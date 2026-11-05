package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	base64 "github.com/segmentio/asm/base64"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agentcfg"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/proc"
)

// Roots are where the two CLIs keep a Unix account's transcripts: the folders under the home, and the ones a
// CLAUDE_CONFIG_DIR or CODEX_HOME moved them to.
type Roots struct {
	Claude string
	Codex  string
	// MoreClaude and MoreCodex are the projects and sessions folders of the other places the CLIs keep their state.
	MoreClaude, MoreCodex []string
}

// TranscriptRoots are the folders under the home.
func TranscriptRoots(home string) Roots {
	return Roots{Claude: filepath.Join(home, ".claude", "projects"), Codex: filepath.Join(home, ".codex", "sessions")}
}

// ClaudeRoots and CodexRoots are every projects and sessions folder, the home's first.
func (r Roots) ClaudeRoots() []string {
	return agentcfg.Unique(append([]string{r.Claude}, r.MoreClaude...)...)
}
func (r Roots) CodexRoots() []string {
	return agentcfg.Unique(append([]string{r.Codex}, r.MoreCodex...)...)
}

// provider names the CLI whose root holds path, or "".
func (r Roots) provider(path string) string {
	for _, root := range r.ClaudeRoots() {
		if strings.HasPrefix(path, root+string(filepath.Separator)) {
			return "claude"
		}
	}
	for _, root := range r.CodexRoots() {
		if strings.HasPrefix(path, root+string(filepath.Separator)) {
			return "codex"
		}
	}
	return ""
}

// rootSet is the agent's roots as they grow: a folder a running CLI was seen to use stays a root after it exits, its
// transcripts are still there.
type rootSet struct {
	mu       sync.Mutex
	roots    Roots
	discover func() (claudeDirs, codexDirs []string)
}

func newRootSet(home string, discover func() (claudeDirs, codexDirs []string)) *rootSet {
	s := &rootSet{roots: TranscriptRoots(home), discover: discover}
	s.Refresh()
	return s
}

// Get is the roots as last refreshed.
func (s *rootSet) Get() Roots {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.roots
	r.MoreClaude, r.MoreCodex = slices.Clone(r.MoreClaude), slices.Clone(r.MoreCodex)
	return r
}

// Refresh adds the folders the CLIs use now and returns the roots.
func (s *rootSet) Refresh() Roots {
	if s.discover != nil {
		claudeDirs, codexDirs := s.discover()
		s.mu.Lock()
		for _, d := range claudeDirs {
			s.roots.MoreClaude = agentcfg.Unique(append(s.roots.MoreClaude, filepath.Join(d, "projects"))...)
		}
		for _, d := range codexDirs {
			s.roots.MoreCodex = agentcfg.Unique(append(s.roots.MoreCodex, filepath.Join(d, "sessions"))...)
		}
		s.mu.Unlock()
	}
	return s.Get()
}

// cliDirs are the folders the CLIs of this account keep their state in: as a CLI the agent starts would see its
// variables (getenv), as each running claude and codex was started (their own environment: the person's shell may
// set CLAUDE_CONFIG_DIR where the agent's service does not), and the defaults under the home.
func cliDirs(home string, getenv func(provider, name string) string, procs proc.Procs) (claudeDirs, codexDirs []string) {
	claudeDirs = append([]string{
		agentcfg.ClaudeDirIn(func(n string) string { return getenv("claude", n) }, home),
	}, agents.LiveDirs(procs, claude.Binary, agentcfg.ClaudeDirEnv)...)
	codexDirs = append([]string{
		agentcfg.CodexDirIn(func(n string) string { return getenv("codex", n) }, home),
	}, agents.LiveDirs(procs, codex.Binary, agentcfg.CodexDirEnv)...)
	return agentcfg.Unique(claudeDirs...), agentcfg.Unique(codexDirs...)
}

func listDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

var yearDir = regexp.MustCompile(`^\d{4}$`)

func describe(provider, file string) (TranscriptFile, bool) {
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return TranscriptFile{}, false
	}
	inode, mtime := fileID(info)
	return TranscriptFile{Provider: provider, Path: file, Size: info.Size(), Inode: inode, MtimeMs: mtime}, true
}

// ListTranscripts is every transcript of the account: Claude — projects/<project>/<session>.jsonl and the subagent
// transcripts under projects/<project>/<session>/subagents/; Codex — sessions/YYYY/MM/DD/*.jsonl.
func ListTranscripts(r Roots) ([]TranscriptFile, error) {
	type candidate struct{ provider, path string }
	var candidates []candidate

	for _, root := range r.ClaudeRoots() {
		projects, err := listDir(root)
		if err != nil {
			return nil, err
		}
		for _, project := range projects {
			if !project.IsDir() {
				continue
			}
			projectDir := filepath.Join(root, project.Name())
			entries, err := listDir(projectDir)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				switch {
				case entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".jsonl"):
					candidates = append(candidates, candidate{"claude", filepath.Join(projectDir, entry.Name())})
				case entry.IsDir():
					subagents := filepath.Join(projectDir, entry.Name(), "subagents")
					subs, err := listDir(subagents)
					if err != nil {
						return nil, err
					}
					for _, sub := range subs {
						if sub.Type().IsRegular() && strings.HasSuffix(sub.Name(), ".jsonl") {
							candidates = append(candidates, candidate{"claude", filepath.Join(subagents, sub.Name())})
						}
					}
				}
			}
		}
	}

	for _, root := range r.CodexRoots() {
		years, err := listDir(root)
		if err != nil {
			return nil, err
		}
		for _, year := range years {
			if !year.IsDir() || !yearDir.MatchString(year.Name()) {
				continue
			}
			months, err := listDir(filepath.Join(root, year.Name()))
			if err != nil {
				return nil, err
			}
			for _, month := range months {
				if !month.IsDir() {
					continue
				}
				days, err := listDir(filepath.Join(root, year.Name(), month.Name()))
				if err != nil {
					return nil, err
				}
				for _, day := range days {
					if !day.IsDir() {
						continue
					}
					dir := filepath.Join(root, year.Name(), month.Name(), day.Name())
					entries, err := listDir(dir)
					if err != nil {
						return nil, err
					}
					for _, entry := range entries {
						if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".jsonl") {
							candidates = append(candidates, candidate{"codex", filepath.Join(dir, entry.Name())})
						}
					}
				}
			}
		}
	}

	files := make([]TranscriptFile, 0, len(candidates))
	for _, c := range candidates {
		if f, ok := describe(c.provider, c.path); ok {
			files = append(files, f)
		}
	}
	return files, nil
}

// IsTranscriptPath says the request names a transcript this agent serves, not an arbitrary file.
func (r Roots) IsTranscriptPath(file string) bool {
	if !filepath.IsAbs(file) || filepath.Clean(file) != file || !strings.HasSuffix(file, ".jsonl") {
		return false
	}
	return r.provider(file) != ""
}

// ReadLines reads whole lines from offset. A trailing line without its newline is still being written and is left
// for the next read, so the caller's offset only ever advances over complete lines.
func ReadLines(file string, offset, maxBytes int64) (ReadChunk, error) {
	chunk, data, release, err := readLines(file, offset, maxBytes)
	if err != nil {
		return ReadChunk{}, err
	}
	defer release()
	if len(data) > 0 {
		chunk.Data = base64.StdEncoding.EncodeToString(data)
	}
	return chunk, nil
}

var readBuffers = sync.Pool{New: func() any { b := make([]byte, 0, 1<<20); return &b }}

// readLines is ReadLines without the encoding: data is the complete lines, valid until release is called.
func readLines(file string, offset, maxBytes int64) (ReadChunk, []byte, func(), error) {
	f, err := os.Open(file)
	if err != nil {
		return ReadChunk{}, nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ReadChunk{}, nil, nil, err
	}
	inode, _ := fileID(info)
	chunk := ReadChunk{Offset: offset, Size: info.Size(), Inode: inode, Next: offset}
	if offset >= info.Size() {
		return chunk, nil, func() {}, nil
	}
	length := min(maxBytes, info.Size()-offset)
	bp := readBuffers.Get().(*[]byte)
	release := func() {
		if cap(*bp) <= 4<<20 {
			readBuffers.Put(bp)
		}
	}
	if int64(cap(*bp)) < length {
		*bp = make([]byte, length)
	}
	buf := (*bp)[:length]
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		release()
		return ReadChunk{}, nil, nil, err
	}
	last := bytes.LastIndexByte(buf[:n], '\n')
	if last < 0 {
		chunk.LineTooLong = int64(n) == maxBytes
		release()
		return chunk, nil, func() {}, nil
	}
	chunk.Next = offset + int64(last) + 1
	return chunk, buf[:last+1], release, nil
}

// encodeReadAnswer writes the whole response frame to a read in one buffer: base64 needs no JSON escaping, and the
// payload is by far the biggest thing the agent sends.
func encodeReadAnswer(id json.RawMessage, c ReadChunk, data []byte) []byte {
	n := base64.StdEncoding.EncodedLen(len(data))
	b := make([]byte, 0, 192+len(id)+n)
	b = append(b, `{"id":`...)
	b = append(b, id...)
	b = append(b, `,"ok":true,"result":{"offset":`...)
	b = strconv.AppendInt(b, c.Offset, 10)
	b = append(b, `,"size":`...)
	b = strconv.AppendInt(b, c.Size, 10)
	b = append(b, `,"inode":`...)
	b = strconv.AppendUint(b, c.Inode, 10)
	b = append(b, `,"data":"`...)
	b = b[:len(b)+n]
	base64.StdEncoding.Encode(b[len(b)-n:], data)
	b = append(b, `","next":`...)
	b = strconv.AppendInt(b, c.Next, 10)
	b = append(b, `,"lineTooLong":`...)
	b = strconv.AppendBool(b, c.LineTooLong)
	return append(b, `}}`...)
}

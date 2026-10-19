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
	"strconv"
	"strings"
	"sync"
)

// Roots are where the two CLIs keep a Unix account's transcripts.
type Roots struct {
	Claude string
	Codex  string
}

func TranscriptRoots(home string) Roots {
	return Roots{Claude: filepath.Join(home, ".claude", "projects"), Codex: filepath.Join(home, ".codex", "sessions")}
}

// provider names the CLI whose root holds path, or "".
func (r Roots) provider(path string) string {
	switch {
	case strings.HasPrefix(path, r.Claude+string(filepath.Separator)):
		return "claude"
	case strings.HasPrefix(path, r.Codex+string(filepath.Separator)):
		return "codex"
	}
	return ""
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

	projects, err := listDir(r.Claude)
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		projectDir := filepath.Join(r.Claude, project.Name())
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

	years, err := listDir(r.Codex)
	if err != nil {
		return nil, err
	}
	for _, year := range years {
		if !year.IsDir() || !yearDir.MatchString(year.Name()) {
			continue
		}
		months, err := listDir(filepath.Join(r.Codex, year.Name()))
		if err != nil {
			return nil, err
		}
		for _, month := range months {
			if !month.IsDir() {
				continue
			}
			days, err := listDir(filepath.Join(r.Codex, year.Name(), month.Name()))
			if err != nil {
				return nil, err
			}
			for _, day := range days {
				if !day.IsDir() {
					continue
				}
				dir := filepath.Join(r.Codex, year.Name(), month.Name(), day.Name())
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

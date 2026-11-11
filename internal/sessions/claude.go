// Package sessions lists the conversations Claude Code and Codex keep on disk for a working folder, as the runtime's
// conversation lists show them.
package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/ly-solutions/harnessly/internal/agentcfg"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

const maxPreview = 200

// ProjectName is the folder Claude Code keeps a working folder's transcripts in: every UTF-16 unit that is not an
// ASCII letter or digit becomes "-", and a name longer than 200 is cut and gets a hash of the path.
func ProjectName(dir string) string {
	units := utf16.Encode([]rune(dir))
	b := make([]byte, len(units))
	var hash int32
	for i, u := range units {
		if u < 128 && (u >= 'a' && u <= 'z' || u >= 'A' && u <= 'Z' || u >= '0' && u <= '9') {
			b[i] = byte(u)
		} else {
			b[i] = '-'
		}
		hash = (hash << 5) - hash + int32(u)
	}
	if len(b) <= maxPreview {
		return string(b)
	}
	h := int64(hash)
	if h < 0 {
		h = -h
	}
	return string(b[:maxPreview]) + "-" + strconv.FormatInt(h, 36)
}

// ClaudeMeta is one conversation in a working folder's list.
type ClaudeMeta struct {
	SessionID      string `json:"sessionId"`
	StartedAt      string `json:"startedAt"`
	LastActivityAt string `json:"lastActivityAt"`
	FirstMessage   string `json:"firstMessage"`
	TurnCount      int    `json:"turnCount"`

	lastMs int64
	// cwd and title are what the other lists do not need: where the session ran and the agent's name for it.
	cwd, title string
}

// scan is what the complete lines of a transcript have given so far: live transcripts grow by tens of megabytes, so
// only what was appended since is read again.
type scan struct {
	offset       int64 // through the last complete line
	firstDone    bool
	startedAt    string
	hasStarted   bool
	firstMessage string
	turns        int
	// cwd is the working folder the first line that names one gave; title the agent's own name for the session, the
	// last one it wrote.
	cwd, title string
}

// ClaudeLister lists the transcripts of a working folder, remembering how far it read each.
type ClaudeLister struct {
	ProjectsDir string
	// Workers bounds the transcripts read at once.
	Workers int
	// Lines, when set, gives what a transcript a conversation is live on gained, read once for all its readers, and
	// a transcript that did not grow is not opened at all; nil opens every transcript at every list.
	Lines *transcriptindex.Follower

	mu    sync.Mutex
	scans map[string]scan
	// reading holds a lock per transcript, so two lists at once do not both read a new file from its start.
	reading map[string]*sync.Mutex
}

// NewClaudeLister lists under the projects folder of $CLAUDE_CONFIG_DIR, else <home>/.claude.
func NewClaudeLister(home string) *ClaudeLister {
	return &ClaudeLister{ProjectsDir: filepath.Join(agentcfg.ClaudeDirIn(os.Getenv, home), "projects"), Workers: 4, scans: map[string]scan{}, reading: map[string]*sync.Mutex{}}
}

// ProjectDir is the transcripts folder of a working folder.
func (l *ClaudeLister) ProjectDir(cwd string) string {
	return filepath.Join(l.ProjectsDir, ProjectName(cwd))
}

// List is every conversation of cwd, the most recently active first. skip leaves out ids that are not conversations
// (forks answering a side question).
func (l *ClaudeLister) List(cwd string, skip func(id string) bool) []ClaudeMeta {
	dir := l.ProjectDir(cwd)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []ClaudeMeta{}
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") && (skip == nil || !skip(jscompat.Basename(name, ".jsonl"))) {
			files = append(files, filepath.Join(dir, name))
		}
	}
	metas := make([]*ClaudeMeta, len(files))
	workers := max(1, l.Workers)
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < min(workers, len(files)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				metas[i] = l.meta(files[i])
			}
		}()
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	out := []ClaudeMeta{}
	live := make(map[string]bool, len(files))
	for i, m := range metas {
		live[files[i]] = true
		if m != nil {
			out = append(out, *m)
		}
	}
	l.forget(dir, live)
	sort.SliceStable(out, func(i, j int) bool { return out[i].lastMs > out[j].lastMs })
	return out
}

// forget drops what is remembered of transcripts of dir that are gone.
func (l *ClaudeLister) forget(dir string, live map[string]bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for p := range l.scans {
		if filepath.Dir(p) == dir && !live[p] {
			delete(l.scans, p)
			delete(l.reading, p)
		}
	}
}

func (l *ClaudeLister) meta(path string) *ClaudeMeta {
	l.mu.Lock()
	lock := l.reading[path]
	if lock == nil {
		lock = &sync.Mutex{}
		l.reading[path] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	size, mtime, birth, _, ok := fileTimes(path)
	if !ok {
		return nil
	}
	l.mu.Lock()
	prev, ok := l.scans[path]
	l.mu.Unlock()
	if !ok || prev.offset > size {
		prev = scan{}
	}
	res, followed := l.scanFollowed(path, prev, ok, size)
	if !followed {
		var err error
		if res, err = scanTranscript(path, prev); err != nil {
			return nil
		}
		l.Lines.Seat(path, res.done.offset)
	}
	l.mu.Lock()
	l.scans[path] = res.done
	l.mu.Unlock()
	whole := res.whole
	lastMs := int64(jscompat.DateMs(jscompat.TimeMs(mtime)))
	last, _ := jscompat.ISOString(float64(lastMs))
	started := whole.startedAt
	if !whole.hasStarted {
		// Node's birthtime: statx's, zero where the file system keeps none.
		started, _ = jscompat.ISOString(jscompat.DateMs(jscompat.TimeMs(birth)))
	}
	return &ClaudeMeta{
		SessionID:      jscompat.Basename(path, ".jsonl"),
		StartedAt:      started,
		LastActivityAt: last,
		FirstMessage:   whole.firstMessage,
		TurnCount:      whole.turns,
		lastMs:         lastMs,
		cwd:            whole.cwd,
		title:          whole.title,
	}
}

type scanResult struct {
	done  scan // through the last complete line, to continue from
	whole scan // with an unfinished last line counted too, as a reader of the whole file sees it
}

// scanFollowed is scanTranscript without the file: a transcript read to its end that did not grow says what it said,
// and one a conversation is live on is moved on by the whole lines the shared read holds. false leaves the read to
// the file: the transcript is new, or it ends in a line still being written.
func (l *ClaudeLister) scanFollowed(path string, prev scan, known bool, size int64) (scanResult, bool) {
	if l.Lines == nil || !known || prev.offset == 0 {
		return scanResult{}, false
	}
	data, partial, ok := l.Lines.Take(path, prev.offset)
	switch {
	case ok && partial:
		return scanResult{}, false
	case !ok:
		// Not a transcript the shared read holds: one that ends where it was read to has nothing new.
		return scanResult{done: prev, whole: prev}, size == prev.offset
	}
	s := prev
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n') // the lines are whole
		s.offset += int64(i) + 1
		for _, part := range splitCR(data[:i]) {
			s.line(part)
		}
		data = data[i+1:]
	}
	return scanResult{done: s, whole: s}, true
}

// scanTranscript reads the transcript from prev.offset on.
func scanTranscript(path string, prev scan) (scanResult, error) {
	f, err := os.Open(path)
	tally.Open()
	if err != nil {
		return scanResult{}, err
	}
	defer f.Close()
	if _, err := f.Seek(prev.offset, io.SeekStart); err != nil {
		return scanResult{}, err
	}
	r := bufio.NewReaderSize(f, 256<<10)
	s := prev
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			s.offset += int64(len(line))
			for _, part := range splitCR(line[:len(line)-1]) {
				s.line(part)
			}
			continue
		}
		if err != nil && err != io.EOF {
			return scanResult{}, err
		}
		whole := s
		if len(line) > 0 {
			for _, part := range splitCR(line) {
				whole.line(part)
			}
		}
		return scanResult{done: s, whole: whole}, nil
	}
}

// splitCR splits a line as Node's readline does: a carriage return ends a line too, one before a newline belongs to it.
func splitCR(line []byte) [][]byte {
	line = bytes.TrimSuffix(line, []byte("\r"))
	if bytes.IndexByte(line, '\r') < 0 {
		return [][]byte{line}
	}
	return bytes.Split(line, []byte("\r"))
}

var (
	humanOrUser = [][]byte{[]byte(`"type":"human"`), []byte(`"type": "human"`), []byte(`"type":"user"`), []byte(`"type": "user"`)}
	notTurn     = [][]byte{[]byte(`"isMeta":true`), []byte(`"isMeta": true`), []byte(`"isCompactSummary":true`)}
)

var (
	cwdKey     = []byte(`"cwd"`)
	aiTitleKey = []byte(`"ai-title"`)
)

func containsAny(line []byte, subs [][]byte) bool {
	for _, s := range subs {
		if bytes.Contains(line, s) {
			return true
		}
	}
	return false
}

func (s *scan) line(line []byte) {
	if !s.firstDone {
		s.firstDone = true
		if jscompat.Trim(string(line)) != "" {
			var entry map[string]any
			if json.Unmarshal(line, &entry) == nil && jscompat.Truthy(entry["timestamp"]) {
				if iso, ok := jscompat.ISOString(jscompat.DateValue(entry["timestamp"])); ok {
					s.startedAt, s.hasStarted = iso, true
				}
			}
		}
	}
	if s.cwd == "" && bytes.Contains(line, cwdKey) {
		var e struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &e) == nil {
			s.cwd = e.Cwd
		}
	}
	if bytes.Contains(line, aiTitleKey) {
		var e struct {
			Type    string `json:"type"`
			AITitle string `json:"aiTitle"`
		}
		if json.Unmarshal(line, &e) == nil && e.Type == "ai-title" && strings.TrimSpace(e.AITitle) != "" {
			s.title = strings.TrimSpace(e.AITitle)
		}
	}
	if !containsAny(line, humanOrUser) || containsAny(line, notTurn) {
		return
	}
	s.turns++
	if s.firstMessage == "" {
		s.firstMessage = firstText(line)
	}
}

// firstText is the text a person typed in a transcript line, "" when the line carries none.
func firstText(line []byte) string {
	var entry map[string]any
	if json.Unmarshal(line, &entry) != nil {
		return ""
	}
	if t := entry["type"]; (t != "human" && t != "user") || jscompat.Truthy(entry["isMeta"]) {
		return ""
	}
	msg := entry["message"]
	if !jscompat.Truthy(msg) {
		return ""
	}
	if text, ok := msg.(string); ok {
		return jscompat.Truncate(text, maxPreview)
	}
	obj, ok := msg.(map[string]any)
	if !ok {
		return ""
	}
	switch content := obj["content"].(type) {
	case []any:
		for _, b := range content {
			if b == nil {
				return "" // reading a field of null throws: the line gives nothing
			}
			block, ok := b.(map[string]any)
			if !ok || block["type"] != "text" || !jscompat.Truthy(block["text"]) {
				continue
			}
			if text, ok := block["text"].(string); ok {
				return jscompat.Truncate(text, maxPreview)
			}
			return ""
		}
	case string:
		return jscompat.Truncate(content, maxPreview)
	}
	return ""
}

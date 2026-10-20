package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

const (
	codexFirstMessageLines = 60
	codexTailBytes         = 1 << 20
)

// CodexDayDir is the folder Codex keeps one local day's sessions in: <root>/YYYY/MM/DD.
func CodexDayDir(root string, day time.Time) string {
	return filepath.Join(root, fmt.Sprintf("%04d", day.Year()), fmt.Sprintf("%02d", int(day.Month())), fmt.Sprintf("%02d", day.Day()))
}

// CodexDays are the day folders of the last n local days, today first.
func CodexDays(root string, now time.Time, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, CodexDayDir(root, now.AddDate(0, 0, -i)))
	}
	return out
}

// readLines reads lines as Node's readline does (a carriage return also ends one), calling fn until it returns false.
func readLines(r io.Reader, fn func(line []byte) bool) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSuffix(line, []byte("\n"))
			for _, part := range splitCR(line) {
				if !fn(part) {
					return nil
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// CodexFirstLine is the session id a Codex transcript's first line names (its session_meta), or nil.
func CodexFirstLine(path string) (id any, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var first []byte
	got := false
	_ = readLines(f, func(line []byte) bool {
		first, got = line, true
		return false
	})
	if !got || len(first) == 0 {
		return nil, false
	}
	var rec map[string]any
	if json.Unmarshal(first, &rec) != nil || rec["type"] != "session_meta" {
		return nil, false
	}
	payload, _ := rec["payload"].(map[string]any)
	if payload == nil || !jscompat.Truthy(payload["id"]) {
		return nil, false
	}
	return payload["id"], true
}

// CodexEntry is one Codex session in a working folder's list.
type CodexEntry struct {
	SessionID        any      `json:"sessionId"`
	JSONLPath        string   `json:"jsonlPath"`
	StartedAt        float64  `json:"startedAt"`
	LastActivityAt   float64  `json:"lastActivityAt"`
	Cwd              any      `json:"cwd"`
	Model            any      `json:"model"`
	FirstUserMessage *string  `json:"firstUserMessage"`
	TurnCount        int      `json:"turnCount"`
	TotalTokens      *float64 `json:"totalTokens"`
}

type codexCached struct {
	mtime time.Time
	size  int64
	entry *CodexEntry
}

// CodexLister lists Codex sessions, keeping what it read of each file until the file changes.
type CodexLister struct {
	Root string
	Now  func() time.Time

	mu    sync.Mutex
	cache map[string]codexCached
}

// NewCodexLister lists under <home>/.codex/sessions.
func NewCodexLister(home string) *CodexLister {
	return &CodexLister{Root: filepath.Join(home, ".codex", "sessions"), Now: time.Now, cache: map[string]codexCached{}}
}

// List is the sessions of the last daysBack days started in cwd, the most recently active first, with how many day
// folders and files were looked at.
func (l *CodexLister) List(cwd string, daysBack int) (sessions []CodexEntry, scannedDirs, scannedFiles int) {
	sessions = []CodexEntry{}
	var files []string
	for _, dir := range CodexDays(l.Root, l.Now(), daysBack) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		scannedDirs++
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonl") {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	scannedFiles = len(files)
	live := make(map[string]bool, len(files))
	for _, p := range files {
		live[p] = true
		e := l.entry(p)
		if e == nil {
			continue
		}
		if c, ok := e.Cwd.(string); !ok || c != cwd {
			continue
		}
		sessions = append(sessions, *e)
	}
	l.mu.Lock()
	for p := range l.cache {
		if !live[p] {
			delete(l.cache, p)
		}
	}
	l.mu.Unlock()
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].LastActivityAt > sessions[j].LastActivityAt })
	return sessions, scannedDirs, scannedFiles
}

func (l *CodexLister) entry(path string) *CodexEntry {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	l.mu.Lock()
	c, ok := l.cache[path]
	l.mu.Unlock()
	if ok && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		return c.entry
	}
	e := codexMeta(path)
	if e != nil {
		e.LastActivityAt = jscompat.TimeMs(st.ModTime())
		if s := codexStats(path, st.Size()); s != nil {
			if e.Model == nil {
				e.Model = s.model
			}
			if s.totalTokens > 0 {
				t := s.totalTokens
				e.TotalTokens = &t
			}
		}
	}
	l.mu.Lock()
	l.cache[path] = codexCached{mtime: st.ModTime(), size: st.Size(), entry: e}
	l.mu.Unlock()
	return e
}

func displayable(text string) bool {
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "<") && (strings.Contains(text, "environment_context") || strings.Contains(text, "user_instructions")) {
		return false
	}
	return true
}

// codexMeta reads a session's first line (its session_meta) and counts the person's messages; nil for a file that is
// not a session.
func codexMeta(path string) *CodexEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var e CodexEntry
	var startedRaw any
	metaOK, failed, n := false, false, 0
	err = readLines(f, func(line []byte) bool {
		n++
		if n == 1 {
			trimmed := jscompat.Trim(string(line))
			if trimmed == "" {
				failed = true // the first line carries no meta: nothing after it can be a session
				return false
			}
			var rec map[string]any
			if json.Unmarshal([]byte(trimmed), &rec) != nil || rec["type"] != "session_meta" || !jscompat.Truthy(rec["payload"]) {
				failed = true
				return false
			}
			payload, _ := rec["payload"].(map[string]any)
			if payload == nil || !jscompat.Truthy(payload["id"]) {
				failed = true
				return false
			}
			e.SessionID = payload["id"]
			startedRaw = payload["timestamp"]
			if startedRaw == nil {
				startedRaw = rec["timestamp"]
			}
			e.Cwd, e.Model = payload["cwd"], payload["model"]
			metaOK = true
			return true
		}
		if !bytes.Contains(line, []byte(`"user_message"`)) {
			return true
		}
		var rec map[string]any
		if json.Unmarshal(line, &rec) != nil || rec["type"] != "event_msg" {
			return true
		}
		payload, _ := rec["payload"].(map[string]any)
		if payload == nil || payload["type"] != "user_message" {
			return true
		}
		text, _ := payload["message"].(string)
		text = jscompat.Trim(text)
		if !displayable(text) {
			return true
		}
		e.TurnCount++
		if e.FirstUserMessage == nil && n <= codexFirstMessageLines {
			m := jscompat.Truncate(text, maxPreview)
			e.FirstUserMessage = &m
		}
		return true
	})
	if err != nil || failed || !metaOK {
		return nil
	}
	started := 0.0
	if jscompat.Truthy(startedRaw) {
		started = jscompat.DateValue(startedRaw)
	}
	if math.IsNaN(started) || started <= 0 {
		return nil
	}
	e.JSONLPath, e.StartedAt, e.LastActivityAt = path, started, started
	return &e
}

type codexStat struct {
	model       any
	totalTokens float64
}

// codexStats is what the end of a session says: the model of its last turn and the tokens it used. nil when the
// session's first line is not its meta.
func codexStats(path string, size int64) *codexStat {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var meta map[string]any
	_ = readLines(f, func(line []byte) bool {
		trimmed := jscompat.Trim(string(line))
		if trimmed == "" {
			return true
		}
		var rec map[string]any
		if json.Unmarshal([]byte(trimmed), &rec) == nil && rec["type"] == "session_meta" && jscompat.Truthy(rec["payload"]) {
			meta = rec
		}
		return false
	})
	if meta == nil {
		return nil
	}
	payload, _ := meta["payload"].(map[string]any)
	if payload == nil || !jscompat.Truthy(payload["id"]) {
		return nil
	}
	started := 0.0
	if ts := payload["timestamp"]; jscompat.Truthy(ts) {
		started = parseDateOf(ts)
	} else if ts := meta["timestamp"]; jscompat.Truthy(ts) {
		started = parseDateOf(ts)
	}
	if !jscompat.Truthy(started) {
		return nil
	}

	start := max(0, size-codexTailBytes)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	var lines [][]byte
	if err := readLines(f, func(line []byte) bool {
		lines = append(lines, append([]byte(nil), line...))
		return true
	}); err != nil {
		return nil
	}
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	var model any
	var info any
	haveInfo := false
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) == 0 {
			continue
		}
		var rec map[string]any
		if json.Unmarshal(lines[i], &rec) != nil {
			continue
		}
		p, isObj := rec["payload"].(map[string]any)
		if !jscompat.Truthy(rec["payload"]) {
			continue
		}
		if !isObj {
			p = map[string]any{}
		}
		if !jscompat.Truthy(model) && rec["type"] == "turn_context" {
			if m, ok := p["model"].(string); ok {
				model = m
			}
		}
		if !haveInfo && rec["type"] == "event_msg" && p["type"] == "token_count" && jscompat.Truthy(p["info"]) {
			info, haveInfo = p["info"], true
		}
		if haveInfo && jscompat.Truthy(model) {
			break
		}
	}
	var usage any
	if obj, ok := info.(map[string]any); ok {
		usage = obj["total_token_usage"]
		if usage == nil {
			usage = obj["last_token_usage"]
		}
	}
	u, _ := usage.(map[string]any)
	input, cached, output := tokenField(u, "input_tokens"), tokenField(u, "cached_input_tokens"), tokenField(u, "output_tokens")
	return &codexStat{model: model, totalTokens: math.Max(0, input-cached) + output}
}

// parseDateOf is Date.parse(String(v)).
func parseDateOf(v any) float64 { return jscompat.ParseDate(jscompat.String(v)) }

// tokenField is `usage?.[k] ?? 0` used as a number.
func tokenField(u map[string]any, k string) float64 {
	switch x := u[k].(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

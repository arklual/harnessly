package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
	"harnessly/internal/proc"
	"harnessly/internal/sessions"
)

const (
	// ScanDays is how many local days of session folders a lookup reads, today first.
	ScanDays = 30
	// processGrace: a session found by folder must have started at most this long before its process.
	processGrace = 60_000
)

// Detector looks for Codex under a pane's shell.
type Detector struct {
	// Dir is Codex's folder (~/.codex), sessions under sessions/YYYY/MM/DD.
	Dir   string
	Procs proc.Procs
	Match agents.Match
	// Installed is asked only when Dir is missing; nil counts as not installed.
	Installed func() bool
	// Now gives today's local day for the session folders.
	Now func() time.Time
}

// NewDetector looks at the machine's processes and <home>/.codex.
func NewDetector(home string) *Detector {
	return &Detector{Dir: filepath.Join(home, ".codex"), Procs: proc.System(), Match: agents.MatchArgv, Now: time.Now}
}

func (d *Detector) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// SessionsRoot holds the session folders.
func (d *Detector) SessionsRoot() string { return filepath.Join(d.Dir, "sessions") }

// IsRunning: one of the shell's children or grandchildren is Codex (its launcher counts).
func (d *Detector) IsRunning(children []int) bool {
	if len(children) == 0 {
		return false
	}
	for _, pid := range d.Procs.WithGrandchildren(children) {
		if d.Match.Is(d.Procs, pid, Binary) {
			return true
		}
	}
	return false
}

var resumeArg = regexp.MustCompile(`(?i)(resume|--resume-session-id)(?:=|` + jscompat.SpaceClass + `+)["']?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

func startsSpace(s string) bool {
	for _, r := range s {
		return jscompat.IsSpace(r)
	}
	return false
}

func endsSpace(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)
	return jscompat.IsSpace(r[len(r)-1])
}

// ResumeID is the session a Codex command line resumes (`resume <id>`, `--resume-session-id=<id>`), standing as a word
// of its own, optionally quoted; "" when none.
func ResumeID(args string) string {
	for from := 0; from < len(args); {
		m := resumeArg.FindStringSubmatchIndex(args[from:])
		if m == nil {
			return ""
		}
		start, idStart, idEnd := from+m[0], from+m[4], from+m[5]
		before, rest := args[:start], args[idEnd:]
		after := rest == "" || startsSpace(rest) || (rest[0] == '"' || rest[0] == '\'') && (len(rest) == 1 || startsSpace(rest[1:]))
		if (before == "" || endsSpace(before)) && after {
			return args[idStart:idEnd]
		}
		from = start + 1
	}
	return ""
}

// SessionMeta is what a rollout's first line (its session_meta) says.
type SessionMeta struct {
	// SessionID as recorded (a JSON value, a string in practice).
	SessionID any
	JSONLPath string
	// Cwd is the session's folder, "" when not a string.
	Cwd       string
	StartedAt *float64
}

func (m SessionMeta) id() string { return jscompat.String(m.SessionID) }

// firstLine is a file's first line as Node's readline gives it (a CR also ends one).
func firstLine(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if err == bufio.ErrBufferFull {
			if i := bytes.IndexByte(line, '\r'); i >= 0 {
				return line[:i], true
			}
			continue
		}
		if err != nil && err != io.EOF {
			return nil, false
		}
		break
	}
	line = bytes.TrimSuffix(line, []byte("\n"))
	if i := bytes.IndexByte(line, '\r'); i >= 0 {
		line = line[:i]
	}
	return line, true
}

// ReadMeta reads a rollout's session_meta; false when the first line is not one or names no id.
func ReadMeta(path string) (SessionMeta, bool) {
	line, ok := firstLine(path)
	if !ok || len(line) == 0 {
		return SessionMeta{}, false
	}
	var rec map[string]any
	if json.Unmarshal(line, &rec) != nil || rec == nil || rec["type"] != "session_meta" {
		return SessionMeta{}, false
	}
	payload, _ := rec["payload"].(map[string]any)
	if payload == nil || !jscompat.Truthy(payload["id"]) {
		return SessionMeta{}, false
	}
	m := SessionMeta{SessionID: payload["id"], JSONLPath: path}
	m.Cwd, _ = payload["cwd"].(string)
	started := payload["timestamp"]
	if started == nil {
		started = rec["timestamp"]
	}
	if jscompat.Truthy(started) {
		if ms := jscompat.DateValue(started); !math.IsNaN(ms) && !math.IsInf(ms, 0) {
			m.StartedAt = &ms
		}
	}
	return m, true
}

func listJSONL(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			out = append(out, e.Name())
		}
	}
	return out
}

// FindSessionByID is the rollout of a session among the last days' folders, by its name and its session_meta.
func FindSessionByID(root, id string, now time.Time, days int) (SessionMeta, bool) {
	for _, dir := range sessions.CodexDays(root, now, days) {
		for _, name := range listJSONL(dir) {
			if !strings.Contains(name, id) {
				continue
			}
			if m, ok := ReadMeta(filepath.Join(dir, name)); ok && m.id() == id {
				if _, isStr := m.SessionID.(string); isStr {
					return m, true
				}
			}
		}
	}
	return SessionMeta{}, false
}

// FindLatestForCwd is the most recently written rollout of the last days whose session ran in cwd.
func FindLatestForCwd(root, cwd string, now time.Time, days int) (SessionMeta, bool) {
	type cand struct {
		path  string
		mtime time.Time
	}
	var cands []cand
	for _, dir := range sessions.CodexDays(root, now, days) {
		for _, name := range listJSONL(dir) {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil {
				cands = append(cands, cand{p, st.ModTime()})
			}
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return b.mtime.Compare(a.mtime) })
	for _, c := range cands {
		if m, ok := ReadMeta(c.path); ok && m.Cwd != "" && m.Cwd == cwd {
			return m, true
		}
	}
	return SessionMeta{}, false
}

func running(pid int, cwd string, m *SessionMeta) agents.SessionInfo {
	info := agents.SessionInfo{Status: agents.Running, PID: pid, Cwd: cwd}
	if m != nil {
		info.RawSessionID = m.SessionID
		info.SessionID = m.id()
		info.JSONLPath = m.JSONLPath
		info.StartedAt = m.StartedAt
		if m.Cwd != "" {
			info.Cwd = m.Cwd
		}
	}
	return info
}

// Detect finds the Codex process among the pane shell's children and grandchildren and the session it resumes. With
// allowCwdFallback (the timeline only) a fresh Codex gets the newest session of its folder that started with it. A
// new session's id normally comes from the hooks, not from here.
func (d *Detector) Detect(children []int, allowCwdFallback bool) agents.SessionInfo {
	if _, err := os.Stat(d.Dir); err != nil {
		if d.Installed != nil && d.Installed() {
			return agents.Info(agents.NotInitialized)
		}
		return agents.Info(agents.NotInstalled)
	}
	if len(children) == 0 {
		return agents.Info(agents.NotRunning)
	}
	pid, args := 0, ""
	for _, p := range d.Procs.WithGrandchildren(children) {
		a, ok := d.Procs.ArgsOf(p)
		if !ok || a == "" || !d.Match.Is(d.Procs, p, Binary) {
			continue
		}
		pid, args = p, a
		break
	}
	if pid == 0 {
		return agents.Info(agents.NotRunning)
	}
	cwd := d.Procs.CwdOf(pid)
	if id := ResumeID(args); id != "" {
		if m, ok := FindSessionByID(d.SessionsRoot(), id, d.now(), ScanDays); ok {
			return running(pid, cwd, &m)
		}
		return running(pid, cwd, &SessionMeta{SessionID: id})
	}
	if allowCwdFallback && cwd != "" {
		if m, ok := FindLatestForCwd(d.SessionsRoot(), cwd, d.now(), ScanDays); ok && d.startedWith(pid, m) {
			return running(pid, cwd, &m)
		}
	}
	return running(pid, cwd, nil)
}

// startedWith: the session started no earlier than a minute before the process (or the process's start is unknown).
func (d *Detector) startedWith(pid int, m SessionMeta) bool {
	if m.StartedAt == nil {
		return false
	}
	start, ok := d.Procs.StartTimeOf(pid)
	if !ok || start.IsZero() {
		return true
	}
	return *m.StartedAt >= float64(start.UnixMilli())-processGrace
}

package devtools

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/tmux"
)

// The dev server's own output for the slot's port: the terminal it runs in, or the file its output goes to (a server an
// agent started in the background writes to one). Problems by default, the plain tail when asked.

type LogSource struct {
	Kind    string
	Session string
	Pane    string
	Path    string
	Command string
}

func (s LogSource) MarshalJSON() ([]byte, error) {
	if s.Kind == "tmux" {
		return marshal(struct {
			Kind    string `json:"kind"`
			Session string `json:"session"`
			Pane    string `json:"pane"`
			Command string `json:"command"`
		}{s.Kind, s.Session, s.Pane, s.Command}), nil
	}
	return marshal(struct {
		Kind    string `json:"kind"`
		Path    string `json:"path"`
		Command string `json:"command"`
	}{s.Kind, s.Path, s.Command}), nil
}

type ServerLog struct {
	Source *LogSource `json:"source"`
	Lines  []LogLine  `json:"lines"`
	Reason *string    `json:"reason"`
}

// Listener is whoever listens on a port; PID 0 for another user's process, which ss does not name.
type Listener struct {
	Port    int
	Host    string
	PID     int
	Command string
	HasPID  bool
}

const (
	defaultProblems = 50
	defaultLines    = 200
	maxLines        = 2000
	problemHistory  = 5000
	tailBytes       = 64 * 1024
	logCacheTTL     = 1500 * time.Millisecond
	// maxAncestors is enough to climb from node through npm, pnpm, turbo and a shell to the pane.
	maxAncestors = 12
)

// ServerLogs reads and briefly remembers the dev server output of the slots' ports.
type ServerLogs struct {
	Store    *Store
	Tmux     *tmux.Tmux
	Listener func(port int) (*Listener, error)
	// Panes and Capture default to the tmux servers': every pane by session, and a pane's text with its history.
	Panes   func() (map[string]tmux.PaneInfo, bool)
	Capture func(session string, lines int) (string, bool)
	// Outside names where a dev server should run for its output to be found (the platform's terminals).
	Outside  string
	ProcRoot string

	mu    sync.Mutex
	cache map[string]cachedLog
}

type cachedLog struct {
	at  time.Time
	log ServerLog
	err error
}

func reason(s string) *string { return &s }

func (l *ServerLogs) proc() string {
	if l.ProcRoot != "" {
		return l.ProcRoot
	}
	return "/proc"
}

func (l *ServerLogs) parentOf(pid int) int {
	raw, err := os.ReadFile(fmt.Sprintf("%s/%d/stat", l.proc(), pid))
	if err != nil {
		return 0
	}
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 > len(s) {
		return 0
	}
	f := strings.Split(s[i+2:], " ")
	if len(f) < 2 {
		return 0
	}
	parent, err := strconv.Atoi(f[1])
	if err != nil || parent <= 1 {
		return 0
	}
	return parent
}

var ttyRe = regexp.MustCompile(`^/dev/(pts/\d+|tty)`)

// outputOf is where a process's stdout (or else stderr) goes: "file" with its path, "terminal" or "pipe".
func (l *ServerLogs) outputOf(pid int) (string, string) {
	terminal := false
	for _, fd := range []int{1, 2} {
		target, err := os.Readlink(fmt.Sprintf("%s/%d/fd/%d", l.proc(), pid, fd))
		if err != nil || target == "" {
			continue
		}
		if ttyRe.MatchString(target) {
			terminal = true
			continue
		}
		if strings.HasPrefix(target, "/") {
			if st, err := os.Stat(target); err == nil && st.Mode().IsRegular() {
				return "file", target
			}
		}
	}
	if terminal {
		return "terminal", ""
	}
	return "pipe", ""
}

func tailOf(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := st.Size()
	length := min(size, tailBytes)
	buf := make([]byte, length)
	if _, err := f.ReadAt(buf, size-length); err != nil {
		return "", err
	}
	text := jscompat.DecodeUTF8(buf)
	if size > length {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			return text[i+1:], nil
		}
	}
	return text, nil
}

type logSource struct {
	kind    string
	session string
	path    string
}

// sourceOf goes up the process tree from the listener: the first stdout that is a file is the log; a terminal is the
// pane above it, if any. A pipe hands the output to the parent, so the walk goes on.
func (l *ServerLogs) sourceOf(pid int) logSource {
	byPid := map[int]string{}
	panes := l.Tmux.ReadAllPanes
	if l.Panes != nil {
		panes = l.Panes
	}
	if panes, ok := panes(); ok {
		for session, info := range panes {
			byPid[info.PID] = session
		}
	}
	terminal := false
	current := pid
	for depth := 0; current != 0 && depth < maxAncestors; depth++ {
		if session, ok := byPid[current]; ok {
			return logSource{kind: "tmux", session: session}
		}
		if !terminal {
			kind, path := l.outputOf(current)
			if kind == "file" {
				return logSource{kind: "file", path: path}
			}
			terminal = kind == "terminal"
		}
		current = l.parentOf(current)
	}
	return logSource{kind: "none"}
}

func (l *ServerLogs) read(port int, all bool, limit int) ServerLog {
	listener, err := l.Listener(port)
	if err != nil || listener == nil {
		return ServerLog{Lines: []LogLine{}, Reason: reason(fmt.Sprintf("На порту %d никто не слушает", port))}
	}
	if !listener.HasPID {
		return ServerLog{Lines: []LogLine{}, Reason: reason(fmt.Sprintf("Порт %d занят процессом другого пользователя", port))}
	}
	pick := func(text string) []LogLine {
		if all {
			return LastLines(text, limit)
		}
		return ProblemLines(text, limit)
	}
	src := l.sourceOf(listener.PID)
	switch src.kind {
	case "file":
		text, err := tailOf(src.path)
		if err != nil {
			return ServerLog{Lines: []LogLine{}, Reason: reason("Не удалось прочитать " + src.path)}
		}
		return ServerLog{Source: &LogSource{Kind: "file", Path: src.path, Command: listener.Command}, Lines: pick(text)}
	case "tmux":
		history := problemHistory
		if all {
			history = limit
		}
		capture := l.Tmux.CaptureHistory
		if l.Capture != nil {
			capture = l.Capture
		}
		text, ok := capture(src.session, history)
		if !ok {
			return ServerLog{Lines: []LogLine{}, Reason: reason("Терминал сервера не ответил")}
		}
		_, pane, _, _ := l.Tmux.ParseSessionName(src.session)
		return ServerLog{Source: &LogSource{Kind: "tmux", Session: src.session, Pane: pane, Command: listener.Command}, Lines: pick(text)}
	}
	return ServerLog{Lines: []LogLine{}, Reason: reason(l.Outside)}
}

func limitOf(lines int, all bool) int {
	if lines > 0 {
		return min(lines, maxLines)
	}
	if all {
		return defaultLines
	}
	return defaultProblems
}

// Get is the slot's dev server output; lines 0 takes the default.
func (l *ServerLogs) Get(slot string, all bool, lines int) (ServerLog, error) {
	target := l.Store.TargetOf(slot)
	if target == nil {
		return ServerLog{Lines: []LogLine{}, Reason: reason("Превью ещё не открывали")}, nil
	}
	if target.Kind == "url" {
		return ServerLog{Lines: []LogLine{}, Reason: reason("Внешний сайт: терминала нет")}, nil
	}
	limit := limitOf(lines, all)
	key := fmt.Sprintf("%d\n%t\n%d", target.Port, all, limit)
	l.mu.Lock()
	if l.cache == nil {
		l.cache = map[string]cachedLog{}
	}
	if c, ok := l.cache[key]; ok && time.Since(c.at) < logCacheTTL {
		l.mu.Unlock()
		return c.log, c.err
	}
	l.mu.Unlock()
	log := l.read(target.Port, all, limit)
	l.mu.Lock()
	l.cache[key] = cachedLog{at: time.Now(), log: log}
	for k, c := range l.cache {
		if time.Since(c.at) >= logCacheTTL {
			delete(l.cache, k)
		}
	}
	l.mu.Unlock()
	return log, nil
}

package tmux

import (
	"regexp"
	"strings"
)

// PaneInfo is one pane of the snapshot every poll judges processes by.
type PaneInfo struct {
	Command        string // pane_current_command
	Path           string // pane_current_path
	PID            int    // pane_pid, 0 when unreadable
	WindowActivity int64  // window_activity (epoch seconds), 0 when unreadable
}

const paneFormat = "#{session_name}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_pid}\t#{window_activity}"

var noServerRe = regexp.MustCompile(`no server running|error connecting to`)

// noServer: nothing listens on the socket (yet, or any more), as opposed to a tmux that did not answer in time.
func noServer(r result) bool { return !r.timedOut && noServerRe.MatchString(r.stderr) }

// readPanesOn is the runtime's panes on one server by session name; ok is false when tmux did not answer.
func (t *Tmux) readPanesOn(socket string) (map[string]PaneInfo, bool) {
	r := t.tmux(socket, "list-panes", "-a", "-F", paneFormat)
	if r.err != nil {
		if noServer(r) {
			return map[string]PaneInfo{}, true
		}
		return nil, false
	}
	panes := map[string]PaneInfo{}
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		for len(f) < 5 {
			f = append(f, "")
		}
		if f[0] == "" || f[1] == "" || !t.onSocket(f[0], socket) {
			continue
		}
		panes[f[0]] = PaneInfo{Command: f[1], Path: f[2], PID: int(leadingInt(f[3])), WindowActivity: leadingInt(f[4])}
	}
	return panes, true
}

// ReadAllPanes is every pane of the runtime's sessions on every server, by session name (for a session with several
// panes, the last one listed). ok is false when any server did not answer — timed out or failed for another reason —
// which a caller must not read as "no panes": a loaded machine can keep list-panes past its timeout, and every working
// agent would look gone at once. A server that is not running is no panes.
func (t *Tmux) ReadAllPanes() (map[string]PaneInfo, bool) {
	sockets := t.Sockets()
	per := make([]map[string]PaneInfo, len(sockets))
	oks := make([]bool, len(sockets))
	t.each(sockets, func(i int, s string) { per[i], oks[i] = t.readPanesOn(s) })
	all := map[string]PaneInfo{}
	for i := range sockets {
		if !oks[i] {
			return nil, false
		}
		for k, v := range per[i] {
			all[k] = v
		}
	}
	return all, true
}

// AllPanes is ReadAllPanes with a silent tmux read as no panes.
func (t *Tmux) AllPanes() map[string]PaneInfo {
	if p, ok := t.ReadAllPanes(); ok {
		return p
	}
	return map[string]PaneInfo{}
}

// Display is `display-message -p` of a format for the session's active pane, trimmed; false when tmux failed.
func (t *Tmux) Display(name, format string) (string, bool) {
	r := t.on(name, "display-message", "-p", "-t", name, format)
	if r.err != nil {
		return "", false
	}
	return strings.TrimSpace(r.stdout), true
}

func (t *Tmux) display(name, format string) string {
	s, _ := t.Display(name, format)
	return s
}

// PanePID is the pid of the session's active pane (its shell), or 0.
func (t *Tmux) PanePID(name string) int { return int(leadingInt(t.display(name, "#{pane_pid}"))) }

// PanePath is tmux's idea of the active pane's folder, or "".
func (t *Tmux) PanePath(name string) string { return t.display(name, "#{pane_current_path}") }

// PaneTitle is the active pane's title, or "".
func (t *Tmux) PaneTitle(name string) string { return t.display(name, "#{pane_title}") }

var paneIDRe = regexp.MustCompile(`^%\d+$`)

// PaneID is the active pane's id (`%12`), the key a program in the pane sees as $TMUX_PANE; "" when unknown.
func (t *Tmux) PaneID(name string) string {
	if id := t.display(name, "#{pane_id}"); paneIDRe.MatchString(id) {
		return id
	}
	return ""
}

var paneRefRe = regexp.MustCompile(`^%\d{1,6}$`)

// SessionsOfPane are the runtime's sessions a pane id names. Each server numbers its own panes, so one id can name a
// pane on each; socket (the last part of the pane's $TMUX path) settles it when the caller knows it, and a socket that
// is not one of the runtime's finds nothing.
func (t *Tmux) SessionsOfPane(paneID, socket string) []string {
	if !paneRefRe.MatchString(paneID) {
		return nil
	}
	sockets := t.Sockets()
	if socket != "" {
		var only []string
		for _, s := range sockets {
			if s == socket {
				only = append(only, s)
			}
		}
		sockets = only
	}
	found := make([]string, len(sockets))
	t.each(sockets, func(i int, s string) {
		r := t.tmux(s, "display-message", "-p", "-t", paneID, "#{session_name}")
		if name := strings.TrimSpace(r.stdout); r.err == nil && t.onSocket(name, s) {
			found[i] = name
		}
	})
	var out []string
	for _, n := range found {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// PaneCurrentCommand is the program in the foreground of the session's pane, or "".
func (t *Tmux) PaneCurrentCommand(name string) string {
	r := t.on(name, "list-panes", "-t", name, "-F", "#{pane_current_command}")
	if r.err != nil {
		return ""
	}
	return strings.TrimSpace(r.stdout)
}

// SafeShells are the foreground programs that mean "nothing runs in the pane but its shell".
var SafeShells = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true}

// CheckTerminalProcess says whether only a shell runs in the session's pane, and the foreground program ("unknown" when
// tmux did not say).
func (t *Tmux) CheckTerminalProcess(name string) (safe bool, process string) {
	cmd := t.PaneCurrentCommand(name)
	if cmd == "" {
		return false, "unknown"
	}
	return SafeShells[cmd], cmd
}

// PaneDetail is what one display-message tells about a session's pane; zero values mean unknown.
type PaneDetail struct {
	Cwd            string
	Command        string
	PID            int
	Width, Height  int
	SessionCreated int64 // epoch seconds
}

// PaneDetail reads the pane's folder, foreground program, pid, size and the session's start in one call.
func (t *Tmux) PaneDetail(name string) PaneDetail {
	r := t.on(name, "display-message", "-p", "-t", name,
		"#{pane_current_path}\t#{pane_current_command}\t#{pane_pid}\t#{pane_width}\t#{pane_height}\t#{session_created}")
	if r.err != nil {
		return PaneDetail{}
	}
	// Only the line end is cut: an empty folder must not shift the other fields.
	f := strings.Split(strings.TrimRight(r.stdout, "\r\n"), "\t")
	for len(f) < 6 {
		f = append(f, "")
	}
	return PaneDetail{
		Cwd: f[0], Command: f[1], PID: int(leadingInt(f[2])), Width: int(leadingInt(f[3])), Height: int(leadingInt(f[4])),
		SessionCreated: leadingInt(f[5]),
	}
}

// leadingInt reads the leading decimal integer of s (after blanks, with a sign), 0 when there is none.
func leadingInt(s string) int64 {
	s = strings.TrimLeft(s, " \t\n\r")
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	var n int64
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int64(s[i]-'0')
	}
	if neg {
		return -n
	}
	return n
}

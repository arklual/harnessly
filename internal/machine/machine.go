// Package machine answers what the runtime reads from the machine itself: processes, the Claude Code session files,
// a tab's working folder.
package machine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"harnessly/internal/proc"
	"harnessly/internal/tmux"
)

// Peer is a live Claude Code session on this machine, under the name agents message each other by.
type Peer struct {
	Name      string   `json:"name"`
	PID       int64    `json:"pid"`
	SessionID *string  `json:"sessionId"`
	TabID     *string  `json:"tabId"`
	Status    *string  `json:"status"`
	UpdatedAt *float64 `json:"updatedAt"`
}

var (
	sessionFile = regexp.MustCompile(`^\d+\.json$`)
	tabOfTmux   = regexp.MustCompile(`-(tab-[\w-]+?)(?::|$)`)
)

// ClaudeDir is where Claude Code keeps its state for this account.
func ClaudeDir(home string) string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home, ".claude")
}

func str(m map[string]any, k string) *string {
	if s, ok := m[k].(string); ok {
		return &s
	}
	return nil
}

// Peers lists sessions/<pid>.json of every live Claude Code session, in name order.
func Peers(home string) []Peer {
	dir := filepath.Join(ClaudeDir(home), "sessions")
	entries, _ := os.ReadDir(dir)
	peers := []Peer{}
	for _, e := range entries {
		if !sessionFile.MatchString(e.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var rec map[string]any
		if json.Unmarshal(raw, &rec) != nil {
			continue
		}
		name, ok := rec["name"].(string)
		pid, okPid := rec["pid"].(float64)
		if !ok || !okPid {
			continue
		}
		p := Peer{Name: name, PID: int64(pid), SessionID: str(rec, "sessionId"), Status: str(rec, "status")}
		if t, ok := rec["tmux"].(string); ok {
			if m := tabOfTmux.FindStringSubmatch(t); m != nil {
				p.TabID = &m[1]
			}
		}
		if u, ok := rec["updatedAt"].(float64); ok {
			p.UpdatedAt = &u
		}
		peers = append(peers, p)
	}
	return peers
}

// processArgs is a process's command line as ps prints it: arguments joined by spaces.
func processArgs(pid int) (string, bool) { return proc.Cmdline(pid) }

// newestChild is what `pgrep -n -P pid` finds: the most recently started child, or 0.
func newestChild(parent int) int { return proc.NewestChild(parent) }

var interpreters = map[string]bool{"node": true, "python": true, "python3": true, "ruby": true, "perl": true, "deno": true, "bun": true}

var scriptExt = regexp.MustCompile(`\.(c|m)?js$`)

// cleanCommandLine shortens a command line for a tab title: the program's base name, an interpreter's script instead.
func cleanCommandLine(raw string) string {
	parts := regexp.MustCompile(`\s+`).Split(raw, -1)
	if len(parts) == 0 {
		return raw
	}
	parts[0] = basename(parts[0])
	if interpreters[parts[0]] && len(parts) > 1 {
		script := scriptExt.ReplaceAllString(basename(parts[1]), "")
		parts = append([]string{script}, parts[2:]...)
	}
	return strings.Join(parts, " ")
}

// basename is Node's path.basename: "" stays "", a trailing slash is dropped.
func basename(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p[strings.LastIndexByte(p, '/')+1:]
}

// SessionCwd is a tab session's working folder (its shell's, else tmux's idea of the pane) and the command running in
// it, or nil.
func SessionCwd(t *tmux.Tmux, session string) (cwd string, lastCommand *string) {
	pid := t.PanePID(session)
	if pid > 0 {
		cwd = proc.Cwd(pid)
	}
	if cwd == "" {
		cwd = t.PanePath(session)
	}
	if pid > 0 {
		if child := newestChild(pid); child > 0 {
			if args, ok := processArgs(child); ok && args != "" {
				cmd := cleanCommandLine(args)
				lastCommand = &cmd
			}
		}
	}
	return cwd, lastCommand
}

package tmux

import (
	"crypto/rand"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/oomguard"
)

// The variables a pane shell keeps from the environment (plus every LC_*); everything else, the runtime's own secrets
// included, is dropped by `env -i`.
var inheritedKeys = map[string]bool{
	"HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "PATH": true, "TERM": true, "COLORTERM": true,
	"LANG": true, "TMPDIR": true, "TMUX_TMPDIR": true, "SSH_AUTH_SOCK": true, "KUBECONFIG": true,
	"TELEPORT_IDENTITY_FILE": true, "TELEPORT_PROXY": true, "SSH_CONNECTION": true, "SSH_TTY": true, "DISPLAY": true,
	"WAYLAND_DISPLAY": true, "XDG_RUNTIME_DIR": true, "TZ": true,
}

// A login shell builds PATH itself from its rc files; it is given only this floor (and the pane bin folder in front),
// so version-manager shims land in the same order as in the person's own terminal. The local install folders are in
// it because non-root Linux profiles may keep the PATH they were given.
const loginShellBasePath = "/usr/local/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Shell is the person's login shell: SHELL of the environment, else the platform's default.
func (t *Tmux) Shell() string {
	if s := lookupEnv(t.cfg.Env, "SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/bash"
}

// shellEnv is the pane shell's environment in the order of Env: the allow-listed variables, a known terminal type, and
// the PATH floor.
func (t *Tmux) shellEnv() [][2]string {
	var keys []string
	vals := map[string]string{}
	set := func(k, v string) {
		if _, seen := vals[k]; !seen {
			keys = append(keys, k)
		}
		vals[k] = v
	}
	for _, kv := range t.cfg.Env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && (inheritedKeys[k] || strings.HasPrefix(k, "LC_")) {
			set(k, v)
		}
	}
	set("TERM", "xterm-256color")
	set("COLORTERM", "truecolor")
	path := loginShellBasePath
	if t.cfg.PaneBinDir != "" {
		path = t.cfg.PaneBinDir + ":" + path
	}
	set("PATH", path)
	out := make([][2]string, len(keys))
	for i, k := range keys {
		out[i] = [2]string{k, vals[k]}
	}
	return out
}

// ShellLaunchCommand is the command a new session runs: the login shell under `env -i` with the allow-listed
// environment, so neither the tmux server's cached environment nor the runtime's own variables reach the pane. TMUX and
// TMUX_PANE, set by tmux for the pane, are expanded by the shell tmux runs the command with and so survive `env -i`.
// With a niceness configured the shell runs under `nice`, so the panes yield the CPU and the server does not.
// Every way of starting a pane shell must go through it.
func (t *Tmux) ShellLaunchCommand() string { return t.shellLaunchCommand(nil) }

// shellLaunchCommand is ShellLaunchCommand with extra KEY=VALUE variables after the allow-listed ones; an entry that
// is not a valid variable name is dropped.
func (t *Tmux) shellLaunchCommand(extra []string) string {
	var b strings.Builder
	if t.cfg.Nice > 0 {
		b.WriteString("nice -n " + strconv.Itoa(t.cfg.Nice) + " ")
	}
	b.WriteString(`env -i TMUX="$TMUX" TMUX_PANE="$TMUX_PANE"`)
	for _, kv := range t.shellEnv() {
		b.WriteString(" " + kv[0] + "=" + shQuote(kv[1]))
	}
	for _, kv := range extra {
		k, v, ok := strings.Cut(kv, "=")
		if ok && envNameRe.MatchString(k) {
			b.WriteString(" " + k + "=" + shQuote(v))
		}
	}
	b.WriteString(" " + shQuote(t.Shell()) + " -l")
	return b.String()
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// paneEnv is what PaneEnv adds for the session's tab.
func (t *Tmux) paneEnv(session string) []string {
	if t.cfg.PaneEnv == nil {
		return nil
	}
	return t.cfg.PaneEnv(t.TabOf(session))
}

// ParseNiceness reads a niceness setting the way the previous runtime did: unset is 10, a whole number is clamped to
// 0..19 (0 turns nice off), anything else is 10. A blank value counts as 0.
func ParseNiceness(raw string, set bool) int {
	if !set {
		return 10
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 10
	}
	return int(math.Min(19, math.Max(0, f)))
}

// CreateSession starts a detached session of cols×rows running the pane shell in cwd (else Home), always on the current
// server; then the config is re-read on that server. The server this may start is not niced (the pane shell is): it
// answers every command of the runtime, each with a few seconds to do it.
func (t *Tmux) CreateSession(name string, cols, rows int, cwd string) error {
	if cwd == "" {
		cwd = t.cfg.Home
	}
	socket := t.cfg.Socket
	args := []string{"-u", "-L", socket}
	if t.cfg.ConfigFile != "" {
		args = append(args, "-f", t.cfg.ConfigFile)
	}
	args = append(args, "new-session", "-d", "-s", name, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows), t.shellLaunchCommand(t.paneEnv(name)))
	program := "tmux"
	// The server this may start, and every pane on it, is what the OOM killer should take before the runtime.
	program, args = oomguard.Unprotected(program, args...)
	if r := t.runWrite(cwd, program, args...); r.timedOut {
		return &SlowError{Command: "new-session", After: r.took}
	} else if r.err != nil {
		return r.err
	}
	if t.legacyName(name) {
		t.remember(name, socket)
	}
	t.ApplyConfig(socket)
	return nil
}

// ApplyConfig re-reads the config file on one server, or on every server when socket is "". A server that is not
// running yet reads it when its first session starts.
func (t *Tmux) ApplyConfig(socket string) {
	if t.cfg.ConfigFile == "" {
		return
	}
	sockets := t.Sockets()
	if socket != "" {
		sockets = []string{socket}
	}
	t.each(sockets, func(_ int, s string) { t.tmux(s, "source-file", t.cfg.ConfigFile) })
}

// each runs f for every socket at once and waits.
func (t *Tmux) each(sockets []string, f func(i int, socket string)) {
	var wg sync.WaitGroup
	for i, s := range sockets {
		wg.Add(1)
		go func() { defer wg.Done(); f(i, s) }()
	}
	wg.Wait()
}

// ListSessions are the runtime's sessions on every server: the current server's first.
func (t *Tmux) ListSessions() []string {
	sockets := t.Sockets()
	lists := make([][]string, len(sockets))
	t.each(sockets, func(i int, s string) {
		r := t.tmux(s, "ls", "-F", "#{session_name}")
		if r.err != nil {
			return
		}
		var names []string
		for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
			if name := strings.TrimSpace(line); t.onSocket(name, s) {
				names = append(names, name)
			}
		}
		lists[i] = names
	})
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// KillSession ends the session: SIGTERM to its pane's process group and kill-session, checking 5 times 200 ms apart;
// then SIGKILL and kill-session again, checking 3 times. A session that still lives is left.
func (t *Tmux) KillSession(name string) {
	if !t.HasSession(name) {
		return
	}
	pid := t.PanePID(name)
	end := func(sig syscall.Signal, checks int) bool {
		if pid > 0 {
			_ = syscall.Kill(-pid, sig)
		}
		t.on(name, "kill-session", "-t", name)
		for i := 0; i < checks; i++ {
			if !t.HasSession(name) {
				return true
			}
			t.sleep(killCheckEvery)
		}
		return false
	}
	if end(syscall.SIGTERM, 5) {
		return
	}
	end(syscall.SIGKILL, 3)
}

// KillServer ends every server of the runtime and every session on them: the reset of all tabs.
func (t *Tmux) KillServer() {
	t.each(t.Sockets(), func(_ int, s string) { t.tmux(s, "kill-server") })
}

// ResolveExistingDir is preferred, or its nearest existing ancestor folder, or Home when none exists or preferred is "".
func (t *Tmux) ResolveExistingDir(preferred string) string {
	if preferred == "" {
		return t.cfg.Home
	}
	candidate := preferred
	for {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return t.cfg.Home
		}
		candidate = parent
	}
}

// nanoid is n random characters of the URL-safe alphabet.
func nanoid(n int) string {
	const alphabet = "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[b[i]&63]
	}
	return string(b)
}

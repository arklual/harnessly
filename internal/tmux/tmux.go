// Package tmux runs the tmux commands the runtime needs. Sessions live on the runtime's own tmux servers (`-L`), so a
// person's terminal tmux is never touched.
//
// Two servers may be in use at once: the current one, where every new session is started, and a legacy one that
// older releases started sessions on. A session named with the current prefix always lives on the current server; a
// session named with the legacy prefix lives wherever it is found (on the legacy server while it lives from before, on
// the current one once it was started again), and the server it was last seen on is remembered.
//
// Every command runs `tmux` directly (no shell) with a 5 s timeout.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Names are the servers and session name prefixes. Sessions named with LegacyPrefix may live on either server; the
// others live on Socket.
type Names struct {
	Socket       string // e.g. the runtime's current server
	LegacySocket string // the server older releases started sessions on
	Prefix       string // sessions of this runtime on Socket; new sessions get it
	LegacyPrefix string // sessions that may be on either server
}

// Config is everything a session is started with on top of the names.
type Config struct {
	Names
	// ConfigFile is the tmux.conf a new server reads (`-f`) and ApplyConfig re-reads; "" leaves tmux's defaults.
	ConfigFile string
	// PaneBinDir comes first on a pane shell's PATH (wrappers such as a guarded tmux); "" adds nothing.
	PaneBinDir string
	// Nice is the niceness `nice -n` gives a server started by CreateSession (every program in it inherits it); 0 runs
	// tmux directly. See ParseNiceness.
	Nice int
	// Env is the environment a pane shell takes its allow-listed variables from; nil means this process's.
	Env []string
	// Home is the folder a session starts in when none is given; "" means HOME of Env, else "/".
	Home string
}

const cmdTimeout = 5 * time.Second

// Tmux runs commands against the runtime's servers.
type Tmux struct {
	cfg   Config
	run   runner
	sleep func(time.Duration)

	mu sync.Mutex
	// legacy remembers which server a legacy-named session was last seen on.
	legacy map[string]string
}

// New runs commands on the named servers; sessions it creates start with tmux's defaults and the process environment.
func New(n Names) *Tmux { return NewConfig(Config{Names: n}) }

// NewConfig runs commands on the named servers and starts sessions as cfg says.
func NewConfig(cfg Config) *Tmux {
	if cfg.Env == nil {
		cfg.Env = os.Environ()
	}
	if cfg.Home == "" {
		cfg.Home = lookupEnv(cfg.Env, "HOME")
	}
	if cfg.Home == "" {
		cfg.Home = "/"
	}
	return &Tmux{cfg: cfg, run: execRunner, sleep: time.Sleep, legacy: map[string]string{}}
}

// Names are the servers and prefixes this Tmux works with.
func (t *Tmux) Names() Names { return t.cfg.Names }

// result of one command: what it printed, and how it failed.
type result struct {
	stdout, stderr string
	err            error
	timedOut       bool // killed at the timeout, as opposed to tmux answering with an error
}

// runner runs program with args in dir ("" = this process's folder).
type runner func(dir, program string, args ...string) result

func execRunner(dir, program string, args ...string) result {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return result{stdout: out.String(), stderr: errOut.String(), err: err, timedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}
}

// tmux runs `tmux -L socket args…`.
func (t *Tmux) tmux(socket string, args ...string) result {
	return t.run("", "tmux", append([]string{"-L", socket}, args...)...)
}

// on runs a command for a session on the server it lives on.
func (t *Tmux) on(name string, args ...string) result { return t.tmux(t.SocketOf(name), args...) }

// Command runs any other tmux command on the server the session lives on and returns what it printed.
func (t *Tmux) Command(session string, args ...string) (string, error) {
	r := t.on(session, args...)
	return r.stdout, r.err
}

// Sockets are the servers the runtime's sessions may be on: the current one first.
func (t *Tmux) Sockets() []string {
	if t.cfg.LegacySocket == "" || t.cfg.LegacySocket == t.cfg.Socket {
		return []string{t.cfg.Socket}
	}
	return []string{t.cfg.Socket, t.cfg.LegacySocket}
}

func (t *Tmux) hasOn(socket, name string) bool {
	return t.tmux(socket, "has-session", "-t", "="+name).err == nil
}

// findLegacy is the server a legacy-named session lives on, probing the last known one first, or "".
func (t *Tmux) findLegacy(name string) string {
	t.mu.Lock()
	known := t.legacy[name]
	t.mu.Unlock()
	order := []string{t.cfg.LegacySocket, t.cfg.Socket}
	if known == t.cfg.Socket {
		order = []string{t.cfg.Socket, t.cfg.LegacySocket}
	}
	for _, s := range order {
		if t.hasOn(s, name) {
			t.remember(name, s)
			return s
		}
	}
	t.mu.Lock()
	delete(t.legacy, name)
	t.mu.Unlock()
	return ""
}

func (t *Tmux) remember(name, socket string) {
	t.mu.Lock()
	t.legacy[name] = socket
	t.mu.Unlock()
}

// hasLegacyPrefix: the name carries the legacy prefix (whether or not a legacy server is configured).
func (t *Tmux) hasLegacyPrefix(name string) bool {
	return t.cfg.LegacyPrefix != "" && strings.HasPrefix(name, t.cfg.LegacyPrefix)
}

// legacyName: a session name that may live on either server.
func (t *Tmux) legacyName(name string) bool {
	return t.hasLegacyPrefix(name) && t.cfg.LegacySocket != "" && t.cfg.LegacySocket != t.cfg.Socket
}

// onSocket says whether name, seen on socket, is one of the runtime's sessions; a legacy one is remembered there. A
// current-prefix name seen on the legacy server is not.
func (t *Tmux) onSocket(name, socket string) bool {
	if name == "" {
		return false
	}
	if t.hasLegacyPrefix(name) {
		if t.legacyName(name) {
			t.remember(name, socket)
		}
		return true
	}
	return strings.HasPrefix(name, t.cfg.Prefix) && socket == t.cfg.Socket
}

// HasSession says whether the session exists (exact name).
func (t *Tmux) HasSession(name string) bool {
	if t.legacyName(name) {
		return t.findLegacy(name) != ""
	}
	return t.hasOn(t.cfg.Socket, name)
}

// SocketOf is the server the session is on, or for one that does not exist, the server it would be started on (the
// current one).
func (t *Tmux) SocketOf(name string) string {
	if !t.legacyName(name) {
		return t.cfg.Socket
	}
	t.mu.Lock()
	known := t.legacy[name]
	t.mu.Unlock()
	if known != "" {
		return known
	}
	if s := t.findLegacy(name); s != "" {
		return s
	}
	return t.cfg.Socket
}

// IsAppSession says whether the name is one of the runtime's sessions (either prefix).
func (t *Tmux) IsAppSession(name string) bool {
	return (t.cfg.Prefix != "" && strings.HasPrefix(name, t.cfg.Prefix)) || t.hasLegacyPrefix(name)
}

// SessionName is the session of a workspace tab: `<prefix><wsID>-<paneID>-<tabID>`.
func (t *Tmux) SessionName(wsID, paneID, tabID string) string {
	return t.cfg.Prefix + wsID + "-" + paneID + "-" + tabID
}

// NewSessionName is an ad hoc session name: the prefix and three random six-character ids.
func (t *Tmux) NewSessionName() string {
	return t.cfg.Prefix + nanoid(6) + "-" + nanoid(6) + "-" + nanoid(6)
}

var tabIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// PaneShellName is the session of a conversation's right-panel shell, `<prefix>shell-<tabID>`, under the legacy prefix
// when legacy is set; false for a tab id that cannot be part of a name. It does not have the workspace form, so the
// layout never adopts it as a tab.
func (t *Tmux) PaneShellName(tabID string, legacy bool) (string, bool) {
	if !tabIDRe.MatchString(tabID) {
		return "", false
	}
	prefix := t.cfg.Prefix
	if legacy {
		if t.cfg.LegacyPrefix == "" {
			return "", false
		}
		prefix = t.cfg.LegacyPrefix
	}
	return prefix + "shell-" + tabID, true
}

var workspaceNameRe = regexp.MustCompile(`^(ws-.*?)-(pane-.*?)-(tab-.+)$`)

// ParseSessionName inverts SessionName for either prefix; false for any other name.
func (t *Tmux) ParseSessionName(name string) (wsID, paneID, tabID string, ok bool) {
	var rest string
	switch {
	case t.cfg.Prefix != "" && strings.HasPrefix(name, t.cfg.Prefix):
		rest = name[len(t.cfg.Prefix):]
	case t.hasLegacyPrefix(name):
		rest = name[len(t.cfg.LegacyPrefix):]
	default:
		return "", "", "", false
	}
	m := workspaceNameRe.FindStringSubmatch(rest)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

func lookupEnv(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && k == key {
			v = val
		}
	}
	return v
}

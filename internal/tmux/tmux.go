// Package tmux runs the tmux commands the runtime needs. Sessions live on the runtime's own tmux servers (`-L`), so a
// person's terminal tmux is never touched.
//
// Two servers may be in use at once: the current one, where every new session is started, and a legacy one that
// older releases started sessions on. A session named with the current prefix always lives on the current server; a
// session named with the legacy prefix lives wherever it is found (on the legacy server while it lives from before, on
// the current one once it was started again), and the server it was last seen on is remembered.
//
// Every command runs `tmux` directly (no shell). A read (the polls) gets 5 s; a command a person waits on — a new
// session, keys typed into a pane — gets 30 s, so a loaded machine starts the conversation late instead of not at all.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	// Nice is the niceness `nice -n` gives every pane shell (and so every agent and build started in it); 0 runs the
	// shell directly. The tmux server itself stays at the runtime's niceness: a niced server on a loaded machine answers
	// the runtime's commands slower than their timeout, and a new conversation dies with "signal: killed". See
	// ParseNiceness.
	Nice int
	// Env is the environment a pane shell takes its allow-listed variables from; nil means this process's.
	Env []string
	// Home is the folder a session starts in when none is given; "" means HOME of Env, else "/".
	Home string
	// PaneEnv adds KEY=VALUE variables to the pane shell of a new session, given the tab the session belongs to ("" for
	// an ad hoc session); nil adds nothing. Integrations hand agents their addresses and tokens this way.
	PaneEnv func(tabID string) []string
}

const (
	cmdTimeout = 5 * time.Second
	// writeTimeout is for a command that changes the server and that a person waits on: the start of a conversation
	// must not fail because a loaded tmux answered in 7 s.
	writeTimeout = 30 * time.Second
)

// SlowError is a command tmux did not answer in its time: the machine is overloaded, nothing is broken.
type SlowError struct {
	Command string        // the tmux command, e.g. new-session
	After   time.Duration // how long it was given before it was killed
}

func (e *SlowError) Error() string {
	return fmt.Sprintf("tmux %s did not answer in %s: the machine is overloaded", e.Command, e.After.Round(100*time.Millisecond))
}

// Tmux runs commands against the runtime's servers.
type Tmux struct {
	cfg Config
	run runner // reads: cmdTimeout
	// runWrite runs the commands a person waits on: writeTimeout.
	runWrite runner
	sleep    func(time.Duration)

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
	return &Tmux{cfg: cfg, run: timedRunner(cmdTimeout), runWrite: timedRunner(writeTimeout), sleep: time.Sleep, legacy: map[string]string{}}
}

// Names are the servers and prefixes this Tmux works with.
func (t *Tmux) Names() Names { return t.cfg.Names }

// result of one command: what it printed, and how it failed.
type result struct {
	stdout, stderr string
	err            error
	timedOut       bool          // killed at the timeout, as opposed to tmux answering with an error
	took           time.Duration // how long the command ran
}

// runner runs program with args in dir ("" = this process's folder).
type runner func(dir, program string, args ...string) result

// timedRunner runs the command and kills it after timeout.
func timedRunner(timeout time.Duration) runner {
	return func(dir, program string, args ...string) result { return execRunner(timeout, dir, program, args...) }
}

func execRunner(timeout time.Duration, dir, program string, args ...string) result {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return result{stdout: out.String(), stderr: errOut.String(), err: err, timedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		took: time.Since(start)}
}

// tmux runs `tmux -u -L socket args…`. Without -u a runtime started under a C locale gets every tab of a -F format
// printed as "_", so no pane line parses and every working agent looks gone.
func (t *Tmux) tmux(socket string, args ...string) result {
	return t.run("", "tmux", append([]string{"-u", "-L", socket}, args...)...)
}

// on runs a command for a session on the server it lives on.
func (t *Tmux) on(name string, args ...string) result { return t.tmux(t.SocketOf(name), args...) }

// onWrite is on for a command a person waits on: it gets writeTimeout.
func (t *Tmux) onWrite(name string, args ...string) result {
	return t.runWrite("", "tmux", append([]string{"-u", "-L", t.SocketOf(name)}, args...)...)
}

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

// hasOn: a tmux that did not answer in time (a loaded machine) said nothing about the session, so it is not gone.
func (t *Tmux) hasOn(socket, name string) bool {
	r := t.tmux(socket, "has-session", "-t", "="+name)
	return r.err == nil || r.timedOut
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

// OwnShellPrefix is what the sessions of a v2 conversation's own terminals begin with: `<prefix>v2t-<convID>-`.
func (t *Tmux) OwnShellPrefix(convID string) string { return t.cfg.Prefix + "v2t-" + convID + "-" }

// OwnShellName is the session of one of a v2 conversation's own terminals. Like the pane shell it does not have the
// workspace form, so the layout never adopts it as a tab.
func (t *Tmux) OwnShellName(convID, id string) string { return t.OwnShellPrefix(convID) + id }

// OwnShellID is the id of an own terminal from its session name, false for any other session of the conversation.
func (t *Tmux) OwnShellID(convID, session string) (string, bool) {
	id, ok := strings.CutPrefix(session, t.OwnShellPrefix(convID))
	return id, ok && id != "" && !strings.Contains(id, "-")
}

var workspaceNameRe = regexp.MustCompile(`^(ws-.*?)-(pane-.*?)-(tab-.+)$`)

// TabOf is the tab a session belongs to: a workspace tab's session or a conversation's right-panel shell; "" for any
// other session.
func (t *Tmux) TabOf(session string) string {
	if _, _, tab, ok := t.ParseSessionName(session); ok {
		return tab
	}
	for _, prefix := range []string{t.cfg.Prefix, t.cfg.LegacyPrefix} {
		if prefix == "" {
			continue
		}
		if tab, ok := strings.CutPrefix(session, prefix+"shell-"); ok && tabIDRe.MatchString(tab) {
			return tab
		}
	}
	return ""
}

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

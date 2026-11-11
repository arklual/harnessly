// Package preflight checks the tools the runtime needs on the machine — tmux, git, Claude Code, Codex — the way the
// person's own login shell finds them.
package preflight

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/cliversion"
	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// cmdTimeout bounds each check; a check stopped by it is uncertain, not missing.
var cmdTimeout = 5 * time.Second

const (
	runtimeTTL     = 30 * time.Second
	codexTTL       = 60 * time.Second
	minTmuxVersion = 2.9
	maxOutput      = 1 << 20
)

// Tool is one tool's state.
type Tool struct {
	Installed bool    `json:"installed"`
	Version   *string `json:"version"`
	// Uncertain: the check ran out of time on a busy machine, the tool was not proven missing.
	Uncertain bool `json:"uncertain,omitempty"`
}

type Tmux struct {
	Tool
	Compatible bool `json:"compatible"`
}

type Claude struct {
	Installed  bool    `json:"installed"`
	Version    *string `json:"version"`
	BinaryPath *string `json:"binaryPath"`
	// LoggedIn is what `claude auth status` says (exit 0); a probe that ran out of time counts as signed in, as it
	// does not prove the opposite. A ~/.claude folder says nothing: the CLI makes it on its first start, signed in or not.
	LoggedIn bool `json:"loggedIn"`
	// Unchecked: the runtime's screen reading was not checked against this version (cliversion's table).
	Unchecked bool `json:"unchecked,omitempty"`
}

type Codex struct {
	Installed  bool    `json:"installed"`
	Version    *string `json:"version"`
	BinaryPath *string `json:"binaryPath"`
	// LoggedIn is what `codex login status` says (exit 0); a probe that ran out of time counts as signed in, as it
	// does not prove the opposite.
	LoggedIn bool `json:"loggedIn"`
}

// Result is the runtime's preflight answer.
type Result struct {
	Tmux   Tmux   `json:"tmux"`
	Git    Tool   `json:"git"`
	Claude Claude `json:"claude"`
	Codex  Codex  `json:"codex"`
}

// Checker runs the checks and keeps the answer for half a minute.
type Checker struct {
	Home string
	// Env is the environment the runtime started with; the login shell gets the person's part of it.
	Env []string
	// Shell is the person's login shell ("" — the account's, else $SHELL, else /bin/bash).
	Shell string
	// AuthDir is the login folder a managed machine keeps Claude's sign-in in ("" — the CLI's own): the chats get it
	// as CLAUDE_SECURESTORAGE_CONFIG_DIR, so the probe asks with it too.
	AuthDir string

	mu       sync.Mutex
	result   *Result
	at       time.Time
	inflight chan struct{}

	codexMu sync.Mutex
	codex   *Codex
	codexAt time.Time

	claudeMu sync.Mutex
	claude   *Claude
	claudeAt time.Time
}

func New(home string) *Checker {
	return &Checker{Home: home, Env: os.Environ(), AuthDir: authDirOf(os.Getenv("RUNTIME_SIDE_CHAT_CONFIG"))}
}

// authDirOf reads `auth_directory` from the machine's runtime config, the folder its chats sign in from.
func authDirOf(file string) string {
	if file == "" {
		return ""
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	var cfg struct {
		AuthDirectory string `json:"auth_directory"`
	}
	if json.Unmarshal(raw, &cfg) != nil || !filepath.IsAbs(cfg.AuthDirectory) {
		return ""
	}
	return cfg.AuthDirectory
}

// Runtime is the cached answer, checked again once it is older than 30 seconds; concurrent callers share one check.
func (c *Checker) Runtime() Result {
	for {
		c.mu.Lock()
		if c.result != nil && time.Since(c.at) < runtimeTTL {
			r := *c.result
			c.mu.Unlock()
			return r
		}
		if wait := c.inflight; wait != nil {
			c.mu.Unlock()
			<-wait
			continue
		}
		done := make(chan struct{})
		c.inflight = done
		c.mu.Unlock()
		r := c.check()
		c.mu.Lock()
		c.result, c.at, c.inflight = &r, time.Now(), nil
		c.mu.Unlock()
		close(done)
		return r
	}
}

// Invalidate drops the cached answer and the Codex probe: the next Runtime checks again, even while a check runs.
func (c *Checker) Invalidate() {
	c.mu.Lock()
	c.result, c.inflight = nil, nil
	c.mu.Unlock()
	c.codexMu.Lock()
	c.codex = nil
	c.codexMu.Unlock()
	c.claudeMu.Lock()
	c.claude = nil
	c.claudeMu.Unlock()
}

func (c *Checker) check() Result {
	path := c.shellPath()
	var wg sync.WaitGroup
	var r Result
	wg.Add(4)
	go func() {
		defer wg.Done()
		t := c.tool(path, "tmux", "-V")
		r.Tmux = Tmux{Tool: t, Compatible: tmuxCompatible(t)}
	}()
	go func() { defer wg.Done(); r.Git = c.tool(path, "git", "--version") }()
	go func() { defer wg.Done(); r.Claude = c.claudeStatus(path) }()
	go func() { defer wg.Done(); r.Codex = c.codexStatus(path) }()
	wg.Wait()
	return r
}

func (c *Checker) getenv(name string) string {
	for _, kv := range c.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return ""
}

// withPath is the runtime's environment with PATH replaced.
func (c *Checker) withPath(path string) []string {
	out := make([]string, 0, len(c.Env)+1)
	for _, kv := range c.Env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+path)
}

var inherited = map[string]bool{"HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "PATH": true, "TERM": true,
	"COLORTERM": true, "LANG": true, "TMPDIR": true, "TMUX_TMPDIR": true, "SSH_AUTH_SOCK": true, "KUBECONFIG": true,
	"TELEPORT_IDENTITY_FILE": true, "TELEPORT_PROXY": true, "SSH_CONNECTION": true, "SSH_TTY": true, "DISPLAY": true,
	"WAYLAND_DISPLAY": true, "XDG_RUNTIME_DIR": true, "TZ": true}

// loginShell is the account's shell from the password file, else $SHELL, else /bin/bash.
func (c *Checker) loginShell() string {
	if c.Shell != "" {
		return c.Shell
	}
	if s := accountShell(os.Getuid()); s != "" {
		return s
	}
	if s := c.getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/bash"
}

func accountShell(uid int) string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	want := strconv.Itoa(uid)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), ":")
		if len(parts) >= 7 && parts[2] == want {
			return parts[6]
		}
	}
	return ""
}

// LoginShell is the account's shell from the password file, else $SHELL, else /bin/bash.
func (c *Checker) LoginShell() string { return c.loginShell() }

// ShellPath is the PATH the person's interactive login shell builds, else the runtime's own (getShellPath).
func (c *Checker) ShellPath() string { return c.shellPath() }

// shellPath is the PATH the person's interactive login shell builds, else the runtime's own.
func (c *Checker) shellPath() string {
	shell := c.loginShell()
	env := []string{}
	for _, kv := range c.Env {
		k, _, _ := strings.Cut(kv, "=")
		if (inherited[k] || strings.HasPrefix(k, "LC_")) && k != "TERM" && k != "COLORTERM" && k != "SHELL" {
			env = append(env, kv)
		}
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "SHELL="+shell, "DISABLE_AUTO_UPDATE=true", "ZSH_TMUX_AUTOSTARTED=true")
	out, err := run(shell, env, c.getenv("PATH"), "-ilc", `echo -n "$PATH"`)
	if err != nil {
		return c.getenv("PATH")
	}
	return jscompat.Trim(out)
}

// lookPath finds a command along path, as a child started with that PATH would.
func lookPath(name, path string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// errTimedOut: the command was stopped for running too long, or died of a signal — the machine is busy, nothing is
// proven missing.
var errTimedOut = errors.New("check timed out")

type capped struct {
	strings.Builder
}

func (b *capped) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxOutput {
		return 0, errors.New("output too large")
	}
	return b.Builder.Write(p)
}

// run starts name, found along lookup, with env and returns its standard output.
func run(name string, env []string, lookup string, args ...string) (string, error) {
	bin, err := lookPath(name, lookup)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Args[0] = name
	cmd.Env = env
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = time.Second
	var out capped
	cmd.Stdout = &out
	err = cmd.Run()
	if ctx.Err() != nil {
		return "", errTimedOut
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				return "", errTimedOut
			}
		}
		return "", err
	}
	return out.String(), nil
}

var versionRe = regexp.MustCompile(`(\d+\.\d+[\d.]*)`)

// Version is the first dotted version number in a tool's output.
func Version(stdout string) *string {
	m := versionRe.FindStringSubmatch(jscompat.Trim(stdout))
	if m == nil {
		return nil
	}
	return &m[1]
}

func (c *Checker) tool(path, name string, args ...string) Tool {
	out, err := run(name, c.withPath(path), path, args...)
	if errors.Is(err, errTimedOut) {
		return Tool{Installed: true, Uncertain: true}
	}
	if err != nil {
		return Tool{}
	}
	return Tool{Installed: true, Version: Version(out)}
}

var leadingNumber = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?`)

func tmuxCompatible(t Tool) bool {
	if !t.Installed {
		return false
	}
	if t.Uncertain {
		return true
	}
	if t.Version == nil {
		return false
	}
	v, err := strconv.ParseFloat(leadingNumber.FindString(*t.Version), 64)
	return err == nil && v >= minTmuxVersion
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// cliVersion is what an agent's CLI prints for --version. The CLI is started once per binary: the answer is kept
// while the file the name resolves to along path stays the same (cliversion.Of), so a preflight — the page asks one
// every half a minute, and every launch reads it — does not start a CLI each time.
func (c *Checker) cliVersion(name, path string) (string, error) {
	bin, err := lookPath(name, path)
	if err != nil {
		return "", err
	}
	return cliversion.Of(bin, func() (string, error) { return run(name, c.withPath(path), path, "--version") })
}

func (c *Checker) claudeProbe(path string) Claude {
	var r Claude
	if out, err := c.cliVersion("claude", path); err == nil {
		r.Installed, r.Version = true, Version(out)
		if r.Version != nil {
			r.Unchecked = cliversion.Note(cliversion.Claude, *r.Version) == cliversion.Unchecked
		}
	}
	if !r.Installed {
		if dir := filepath.Join(c.Home, ".local", "bin"); exists(filepath.Join(dir, "claude")) {
			r.BinaryPath = &dir
		}
	}
	if r.Installed {
		r.LoggedIn = c.claudeLoggedIn(path)
	}
	return r
}

// claudeStatus is kept for a minute on its own, like Codex's: it starts the CLI, which may reach for the network.
func (c *Checker) claudeStatus(path string) Claude {
	c.claudeMu.Lock()
	defer c.claudeMu.Unlock()
	if c.claude != nil && time.Since(c.claudeAt) < codexTTL {
		return *c.claude
	}
	r := c.claudeProbe(path)
	c.claude, c.claudeAt = &r, time.Now()
	return r
}

// claudeLoggedIn asks the CLI itself whether it holds a sign-in (a subscription, a key, a cloud provider): it exits 0
// when it does. A probe that ran out of time proves nothing, so it counts as signed in. So does a home that keeps
// credentials or names its own endpoint or key in the CLI's settings: the runtime gives those to the chat, not to this
// probe, and a machine that worked must not lose its agent to a stricter check.
func (c *Checker) claudeLoggedIn(path string) bool {
	env := c.withPath(path)
	if c.AuthDir != "" {
		env = append(env, "CLAUDE_SECURESTORAGE_CONFIG_DIR="+c.AuthDir)
	}
	_, err := run("claude", env, path, "auth", "status")
	return err == nil || errors.Is(err, errTimedOut) || c.claudeConfigured()
}

// claudeConfigured reports whether the home holds a sign-in of the CLI or settings that bring one.
func (c *Checker) claudeConfigured() bool {
	if c.AuthDir != "" && exists(filepath.Join(c.AuthDir, ".credentials.json")) {
		return true
	}
	dir := filepath.Join(c.Home, ".claude")
	if exists(filepath.Join(dir, ".credentials.json")) {
		return true
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return false
	}
	for _, name := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "apiKeyHelper"} {
		if strings.Contains(string(raw), `"`+name+`"`) {
			return true
		}
	}
	return false
}

// codexStatus is kept for a minute on its own, as the previous runtime kept it.
func (c *Checker) codexStatus(path string) Codex {
	c.codexMu.Lock()
	defer c.codexMu.Unlock()
	if c.codex != nil && time.Since(c.codexAt) < codexTTL {
		return *c.codex
	}
	r := c.probeCodex(path)
	c.codex, c.codexAt = &r, time.Now()
	return r
}

func (c *Checker) probeCodex(path string) Codex {
	env := c.withPath(path)
	out, err := c.cliVersion("codex", path)
	if err != nil {
		return Codex{}
	}
	r := Codex{Installed: true, Version: Version(out), LoggedIn: true}
	if r.Version != nil {
		cliversion.Note(cliversion.Codex, *r.Version)
	}
	if _, err := run("codex", env, path, "login", "status"); err != nil && !errors.Is(err, errTimedOut) {
		r.LoggedIn = false
	}
	if where, err := run("which", env, path, "codex"); err == nil {
		if w := jscompat.Trim(where); w != "" {
			r.BinaryPath = &w
		}
	}
	return r
}

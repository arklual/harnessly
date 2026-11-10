package apps

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
)

// terminal.run and process (HRN-851): an app runs a command in the chat's folder and gets its exit code and output, or
// starts a long one (a dev server, a watcher) and stops it later. Every command runs inside the same wall as the agent
// of the chat (boundary.go: bwrap, then the runtime's sandbox-exec for Landlock and seccomp), in a namespace of
// processes of its own (what it leaves behind dies with it), with no secret in its environment, the person's key
// folders shut, and limits on time, processor time and output. A machine that cannot make the wall runs nothing.

const (
	// DefaultRunTimeout is how long a command runs when the app names no time; MaxRunTimeout is the most it may name.
	DefaultRunTimeout = 2 * time.Minute
	MaxRunTimeout     = 10 * time.Minute
	// MaxRunOutput is how much of a command's output (stdout and stderr together) an answer holds.
	MaxRunOutput = 1 << 20
	// MaxCommand is the longest command an app may pass.
	MaxCommand = 4096
	// RunCPUSeconds is the processor time a command, or a process, may use (RLIMIT_CPU in sandbox-exec).
	RunCPUSeconds = 1800
	// MaxAppProcesses is how many processes an app keeps running in a chat; MaxProcesses on the machine.
	MaxAppProcesses = 4
	MaxProcesses    = 24
	// maxProcessOutput is the tail of a process's output the machine keeps.
	maxProcessOutput = 256 << 10
	// keptProcesses is how many ended processes of an app in a chat stay listed, for their code and output.
	keptProcesses  = 8
	processAliveCh = 5 * time.Second
	// SandboxCPUEnv carries the processor limit to sandbox-exec, which sets it on itself and drops the variable.
	SandboxCPUEnv = "HARNESSLY_SANDBOX_CPU"
)

// shellChars are what make a command more than one program with its arguments: a command an app holds only by a mask
// (terminal.run:npm test*) has none of them, so the mask is all it runs.
const shellChars = ";&|<>()$`\\\"'\n\r{}*?[]~#!"

// homeShut are the folders of the person's home a command never sees (an empty folder in their place), homeHidden the
// files that read as empty: keys, the tools' logins, the history of their shells.
var (
	homeShut   = []string{".ssh", ".config", ".gnupg", ".aws", ".azure", ".kube", ".docker", ".password-store", ".local/share/keyrings"}
	homeHidden = []string{".netrc", ".git-credentials", ".npmrc", ".pypirc", ".claude.json", ".claude/.credentials.json",
		".codex/auth.json", ".bash_history", ".zsh_history", ".python_history", ".node_repl_history", ".psql_history"}
	// keptEnv is what a command keeps of the runtime's environment: the locale and where the toolchains are, nothing
	// that carries a key.
	keptEnv = []string{"PATH", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "USER", "LOGNAME", "SHELL", "TMPDIR",
		"VOLTA_HOME", "NVM_DIR", "NVM_BIN", "PNPM_HOME", "BUN_INSTALL", "DENO_DIR", "GOPATH", "GOROOT", "GOMODCACHE", "GOCACHE",
		"CARGO_HOME", "RUSTUP_HOME", "PYENV_ROOT", "JAVA_HOME", "GEM_HOME", "ASDF_DIR", "ASDF_DATA_DIR", "MISE_DATA_DIR"}
)

// RunResult is what a command did.
type RunResult struct {
	Code     int    `json:"code"`
	Output   string `json:"output"`
	Cut      bool   `json:"cut,omitempty"`
	TimedOut bool   `json:"timedOut,omitempty"`
	Ms       int64  `json:"ms"`
}

// RunRights is what an app may run: any command, or the ones its masks name.
type RunRights struct {
	Any   bool
	Masks []string
}

// RunRightsOf is what the app's right (terminal.run or process) lets it run.
func (m *Manifest) RunRightsOf(right string) RunRights {
	var r RunRights
	for _, p := range m.Permissions {
		if p == right {
			r.Any = true
		}
		if mask, ok := strings.CutPrefix(p, right+":"); ok {
			r.Masks = append(r.Masks, mask)
		}
	}
	return r
}

// CheckCommandMask is "" for a good mask of terminal.run: a command where * stands for any run of characters.
func CheckCommandMask(mask string) string {
	switch {
	case strings.TrimSpace(mask) == "":
		return "is empty"
	case len(mask) > 200:
		return "is longer than 200 characters"
	case strings.ContainsAny(mask, "\n\r\x00"):
		return "has a line break"
	case strings.ContainsAny(strings.ReplaceAll(mask, "*", ""), shellChars):
		return "has a character of the shell (; & | $ quotes…): a mask names one program and its arguments"
	}
	return ""
}

// Argv is what runs for a command: the shell for an app that may run any, the words of the command for one that holds
// a mask it matches. A refusal says why.
func (r RunRights) Argv(command string) ([]string, error) {
	if strings.TrimSpace(command) == "" || len(command) > MaxCommand || strings.ContainsRune(command, 0) {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "command is a line of at most 4096 characters"}
	}
	if r.Any {
		return []string{"sh", "-c", command}, nil
	}
	if strings.ContainsAny(command, shellChars) {
		return nil, denied("the app runs only the commands of its masks, without the shell's ; & | $ or quotes")
	}
	words := strings.Fields(command)
	line := strings.Join(words, " ")
	for _, mask := range r.Masks {
		if commandMatch(strings.Join(strings.Fields(mask), " "), line) {
			return words, nil
		}
	}
	return nil, denied("the command is not one of the app's terminal.run masks")
}

// commandMatch matches a whole command line against a mask where * is any run of characters, except an option: what a
// * stands for holds no word that starts with '-' (npm test* matches npm test:unit, not npm test --registry=…). An
// option the app needs is written in the mask itself (npm test -- --grep *).
func commandMatch(mask, line string) bool {
	parts := strings.Split(mask, "*")
	if !strings.HasPrefix(line, parts[0]) {
		return false
	}
	// at[i]: the line up to i is matched by the parts so far. A * reaches from each such place up to the next option.
	at := make([]bool, len(line)+1)
	at[len(parts[0])] = true
	for _, part := range parts[1:] {
		next := make([]bool, len(line)+1)
		reach := false
		for i := 0; i <= len(line); i++ {
			if i < len(line) && line[i] == '-' && (i == 0 || line[i-1] == ' ') {
				// An option starts here: a * may end before it, not run over it.
				if (reach || at[i]) && strings.HasPrefix(line[i:], part) {
					next[i+len(part)] = true
				}
				reach = false
				continue
			}
			reach = reach || at[i]
			if reach && strings.HasPrefix(line[i:], part) {
				next[i+len(part)] = true
			}
		}
		at = next
	}
	return at[len(line)]
}

// Sandbox is where an app's command runs: the agent's wall, around the chat's folder.
type Sandbox struct {
	Boundary *Boundary
	// Root is the folder the wall's probe runs in (the registry's).
	Root string
	// Protected are the folders bound read-only (the registry, the packages); Data the runtime's data folder (sealed);
	// Home the person's; Hidden the files that read as empty (the cli-token, the agents' key).
	Protected []string
	Data      string
	Home      string
	Hidden    []string
}

// command is the process of argv in dir, inside the wall; it is not started. tty: it runs on a terminal of its own (a
// pty the caller gives it), so it keeps that terminal as its controlling one and gets a terminal's environment.
func (s *Sandbox) command(dir string, argv []string, tty bool) (*exec.Cmd, error) {
	b := s.Boundary
	if b == nil || b.Ready(s.Root) != nil {
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: "this machine cannot run an app's commands inside the agent's wall (bubblewrap and user namespaces)"}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "the chat's folder is not there"}
	}
	args := b.sealedArgs(s.Protected, &Seal{Data: s.Data, Home: s.Home}, s.Hidden...)
	cut := slices.Index(args, "--")
	if cut < 0 {
		return nil, errors.New("apps: the wall has no end of options")
	}
	extra := []string{"--unshare-pid", "--die-with-parent", "--chdir", dir}
	if !tty {
		// No terminal to push keys into: a command with the runtime's own terminal could, with TIOCSTI. A pty is the
		// shell's own, so pushing into it reaches only itself.
		extra = append(extra, "--new-session")
	}
	if s.Home != "" {
		// A link is shut where it leads too: ~/.ssh may be a link to a folder elsewhere, a key a link to a file.
		for _, rel := range homeShut {
			for _, p := range pathAndTarget(filepath.Join(s.Home, rel)) {
				if info, err := os.Stat(p); err == nil && info.IsDir() && !within(p, dir) {
					extra = append(extra, "--tmpfs", p)
				}
			}
		}
		for _, rel := range homeHidden {
			for _, p := range pathAndTarget(filepath.Join(s.Home, rel)) {
				if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
					extra = append(extra, "--ro-bind", os.DevNull, p)
				}
			}
		}
	}
	full := append(append(append([]string{}, args[:cut]...), extra...), args[cut:]...)
	cmd := exec.Command(b.Bwrap, append(full, argv...)...)
	cmd.Dir = dir
	cmd.Env = runEnv(s.Home, tty)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if tty {
		// A session of its own on the pty: its leader's group is the session, so kill reaches all of it.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}

// pathAndTarget is where a path really leads: the path itself, or for a link (or a path under one) its target, which
// is what has to be shut; the link then leads to the shut place. (A mount cannot sit on a link itself.)
func pathAndTarget(p string) []string {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return []string{p}
	}
	return []string{real}
}

// kill ends a started command and everything in its group; the wall's namespace of processes takes the rest with it.
func kill(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// runEnv is a command's environment: the runtime's PATH and locale, the person's HOME, a plain terminal, and nothing
// else (no token, no key the runtime itself was started with).
func runEnv(home string, tty bool) []string {
	env := []string{"TERM=dumb", "NO_COLOR=1", "CI=1", SandboxCPUEnv + "=" + strconv.Itoa(RunCPUSeconds)}
	if tty {
		env = []string{"TERM=xterm-256color", "COLORTERM=truecolor", SandboxCPUEnv + "=" + strconv.Itoa(RunCPUSeconds)}
	}
	if home != "" {
		env = append(env, "HOME="+home)
	}
	for _, k := range keptEnv {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// capped keeps the first max bytes written to it and says whether more came.
type capped struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.max - c.buf.Len(); room < len(p) {
		c.cut = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

// Run runs argv in dir inside the wall and waits for it, at most timeout (DefaultRunTimeout for 0).
func (s *Sandbox) Run(ctx context.Context, dir string, argv []string, timeout time.Duration) (RunResult, error) {
	if timeout <= 0 {
		timeout = DefaultRunTimeout
	}
	timeout = min(timeout, MaxRunTimeout)
	cmd, err := s.command(dir, argv, false)
	if err != nil {
		return RunResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out := &capped{max: MaxRunOutput}
	cmd.Stdout, cmd.Stderr = out, out
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return RunResult{}, &BridgeError{Code: CodeHostUnavailable, Message: "the command did not start", Detail: err.Error()}
	}
	stop := context.AfterFunc(ctx, func() { kill(cmd) })
	err = cmd.Wait()
	stop()
	res := RunResult{Ms: time.Since(start).Milliseconds()}
	res.Output, res.Cut = strings.ToValidUTF8(out.buf.String(), "�"), out.cut
	res.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.Code = exit.ExitCode()
		if res.Code < 0 {
			res.Code = 128 + int(syscall.SIGKILL)
		}
	case res.TimedOut || ctx.Err() != nil:
		res.Code = 128 + int(syscall.SIGKILL)
	default:
		return res, &BridgeError{Code: CodeHostUnavailable, Message: "the command failed to run", Detail: err.Error()}
	}
	return res, nil
}

// Process is a long command an app started.
type Process struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	StartedAt int64  `json:"startedAt"`
	Running   bool   `json:"running"`
	// Code is the exit code of a process that ended.
	Code *int `json:"code,omitempty"`
}

// ProcessOutput is a piece of a process's output: from offset From to Next; Lost says the start of the piece was dropped
// (the machine keeps the last 256 KiB).
type ProcessOutput struct {
	Process
	Text string `json:"text"`
	Next int64  `json:"next"`
	Lost bool   `json:"lost,omitempty"`
}

type proc struct {
	Process
	app, chat string
	cmd       *exec.Cmd
	done      chan struct{}
	ended     time.Time
	mu        sync.Mutex
	tail      []byte
	// start is the offset of tail's first byte in the whole output.
	start int64
}

func (p *proc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tail = append(p.tail, b...)
	if over := len(p.tail) - maxProcessOutput; over > 0 {
		p.tail = append([]byte(nil), p.tail[over:]...)
		p.start += int64(over)
	}
	return len(b), nil
}

// Processes are the processes the machine's apps started.
type Processes struct {
	mu   sync.Mutex
	byID map[string]*proc
	next int
	// starting are the places taken by starts not done yet, by app and chat; startingAll on the machine.
	starting    map[string]int
	startingAll int
	// OnEnd hears a process that ended, for the journal.
	OnEnd func(app, chat string, p Process)
}

// NewProcesses is a machine with no process.
func NewProcesses() *Processes {
	return &Processes{byID: map[string]*proc{}, starting: map[string]int{}}
}

var errNoProcess = &BridgeError{Code: CodeInvalidParams, Message: "no such process of the app in this chat"}

// Start starts argv in dir inside the wall for an app in a chat. alive is asked every few seconds: the process is
// stopped when it says no (the app was turned off in the chat, switched off, or lost the right).
func (ps *Processes) Start(s *Sandbox, app, chat, dir, command string, argv []string, alive func() bool) (Process, error) {
	// The place is taken under the lock, before the start: two starts at once cannot both pass the limit.
	key := app + "\x00" + chat
	ps.mu.Lock()
	n, all := ps.starting[key], ps.startingAll
	for _, p := range ps.byID {
		if p.Running {
			all++
			if p.app == app && p.chat == chat {
				n++
			}
		}
	}
	if n >= MaxAppProcesses {
		ps.mu.Unlock()
		return Process{}, &BridgeError{Code: CodeRateLimited, Message: "the app runs " + strconv.Itoa(MaxAppProcesses) + " processes in this chat already: stop one first"}
	}
	if all >= MaxProcesses {
		ps.mu.Unlock()
		return Process{}, &BridgeError{Code: CodeRateLimited, Message: "too many apps' processes run on this machine"}
	}
	ps.starting[key]++
	ps.startingAll++
	ps.mu.Unlock()
	release := func() {
		ps.starting[key]--
		if ps.starting[key] == 0 {
			delete(ps.starting, key)
		}
		ps.startingAll--
	}
	cmd, err := s.command(dir, argv, false)
	if err == nil {
		p := &proc{app: app, chat: chat, cmd: cmd, done: make(chan struct{})}
		cmd.Stdout, cmd.Stderr = p, p
		if err = cmd.Start(); err != nil {
			err = &BridgeError{Code: CodeHostUnavailable, Message: "the process did not start", Detail: err.Error()}
		} else {
			return ps.started(p, app, chat, command, release, alive), nil
		}
	}
	ps.mu.Lock()
	release()
	ps.mu.Unlock()
	return Process{}, err
}

// started registers a process that has started, in place of its reserved place, and follows it to its end.
func (ps *Processes) started(p *proc, app, chat, command string, release func(), alive func() bool) Process {
	cmd := p.cmd
	ps.mu.Lock()
	release()
	ps.next++
	p.Process = Process{ID: "p" + strconv.Itoa(ps.next), Command: command, StartedAt: time.Now().UnixMilli(), Running: true}
	ps.byID[p.ID] = p
	ps.forget(app, chat)
	shown := p.Process
	ps.mu.Unlock()
	go func() {
		err := cmd.Wait()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
			if code < 0 {
				code = 128 + int(syscall.SIGKILL)
			}
		} else if err != nil {
			code = -1
		}
		ps.mu.Lock()
		p.Running, p.Code, p.ended = false, &code, time.Now()
		ended := p.Process
		ps.mu.Unlock()
		close(p.done)
		if ps.OnEnd != nil {
			ps.OnEnd(app, chat, ended)
		}
	}()
	go func() {
		tick := time.NewTicker(processAliveCh)
		defer tick.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-tick.C:
				if !alive() {
					kill(cmd)
					return
				}
			}
		}
	}()
	return shown
}

// forget drops the oldest ended processes of an app in a chat past keptProcesses; ps.mu is held.
func (ps *Processes) forget(app, chat string) {
	var ended []*proc
	for _, p := range ps.byID {
		if p.app == app && p.chat == chat && !p.Running {
			ended = append(ended, p)
		}
	}
	for len(ended) > keptProcesses {
		oldest := 0
		for i, p := range ended {
			if p.ended.Before(ended[oldest].ended) {
				oldest = i
			}
		}
		delete(ps.byID, ended[oldest].ID)
		ended = append(ended[:oldest], ended[oldest+1:]...)
	}
}

func (ps *Processes) get(app, chat, id string) (*proc, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p := ps.byID[id]
	if p == nil || p.app != app || p.chat != chat {
		return nil, errNoProcess
	}
	return p, nil
}

// Stop ends a process of the app in the chat and waits a moment for it; one that ended already is no error.
func (ps *Processes) Stop(app, chat, id string) (Process, error) {
	p, err := ps.get(app, chat, id)
	if err != nil {
		return Process{}, err
	}
	select {
	case <-p.done:
	default:
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			kill(p.cmd)
			<-p.done
		}
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return p.Process, nil
}

// StopApp ends every process of an app in a chat ("" chat: in every chat).
func (ps *Processes) StopApp(app, chat string) {
	ps.mu.Lock()
	var running []*proc
	for _, p := range ps.byID {
		if p.app == app && (chat == "" || p.chat == chat) && p.Running {
			running = append(running, p)
		}
	}
	ps.mu.Unlock()
	for _, p := range running {
		_, _ = ps.Stop(app, p.chat, p.ID)
	}
}

// List is the processes of an app in a chat, the oldest first.
func (ps *Processes) List(app, chat string) []Process {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	out := []Process{}
	for _, p := range ps.byID {
		if p.app == app && p.chat == chat {
			out = append(out, p.Process)
		}
	}
	slices.SortFunc(out, func(a, b Process) int { return int(a.StartedAt - b.StartedAt) })
	return out
}

// Output is the output of a process from offset since (0: what the machine keeps).
func (ps *Processes) Output(app, chat, id string, since int64) (ProcessOutput, error) {
	p, err := ps.get(app, chat, id)
	if err != nil {
		return ProcessOutput{}, err
	}
	p.mu.Lock()
	end := p.start + int64(len(p.tail))
	from := max(since, p.start)
	lost := since < p.start && since > 0
	if from > end {
		from = end
	}
	text := strings.ToValidUTF8(string(p.tail[from-p.start:]), "�")
	p.mu.Unlock()
	ps.mu.Lock()
	shown := p.Process
	ps.mu.Unlock()
	return ProcessOutput{Process: shown, Text: text, Next: end, Lost: lost}, nil
}

// RunDir is the folder a command runs in: the chat's folder, or a folder in it the app names.
func RunDir(workspace, cwd string) (string, error) {
	if workspace == "" || !filepath.IsAbs(workspace) {
		return "", &BridgeError{Code: CodeInvalidParams, Message: "this view is in no chat folder"}
	}
	if cwd == "" || cwd == "." {
		return workspace, nil
	}
	clean := path.Clean(filepath.ToSlash(cwd))
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", &BridgeError{Code: CodeInvalidParams, Message: "cwd is a folder of the chat's folder"}
	}
	dir := filepath.Join(workspace, filepath.FromSlash(clean))
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", &BridgeError{Code: CodeInvalidParams, Message: "cwd is not a folder of the chat's folder"}
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil || !within(root, real) {
		return "", &BridgeError{Code: CodeInvalidParams, Message: "cwd leads out of the chat's folder"}
	}
	return real, nil
}

// Sandbox is the wall an app's commands run in on this registry's machine: the registry and the packages read-only,
// the data folder sealed, the runtime's own credentials read as empty.
func (r *Registry) Sandbox(b *Boundary) (*Sandbox, error) {
	protected, err := r.ProtectedDirs()
	if err != nil {
		return nil, err
	}
	data := filepath.Dir(r.Root)
	home, _ := os.UserHomeDir()
	return &Sandbox{Boundary: b, Root: r.Root, Protected: protected, Data: data, Home: home,
		Hidden: []string{filepath.Join(data, "cli-token"), filepath.Join(data, gate.AgentTokenFile), filepath.Join(data, "preview-mcp.json")}}, nil
}

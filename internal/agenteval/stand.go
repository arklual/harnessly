package agenteval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentnet"
	"go.yaml.in/yaml/v3"
)

// StandConfig is what every stand of a machine is made from.
type StandConfig struct {
	// Root holds the stands' folders.
	Root string
	// Runtime is the runtime binary; Harnessly the CLI (the agent's `harnessly`, and the fake picture generator).
	Runtime   string
	Harnessly string
	// Tools are folders whose claude and codex the agents run.
	Tools []string
	// Login holds claude-credentials.json, claude-account.json and codex-auth.json: copies without refresh tokens.
	Login string
	// RealImages draws pictures with Codex for real; by default a fake generator answers with a blank picture.
	RealImages bool
	// AgentEnv are variables every agent of the stand gets (a provider's base URL, a model's name).
	AgentEnv map[string]string
	// RuntimeEnv are the runtime's settings (RUNTIME_GITLAB_HOSTS, RUNTIME_TASK_KEY_RE…): the setup under test.
	RuntimeEnv map[string]string
	// ClaudeConfig are keys merged into the agents' ~/.claude.json: a consent given once, as a person would
	// (Code Mode's codeModeCaveatsAcceptedV1 and codeModeAutoModeCaveatAcceptedV1).
	ClaudeConfig map[string]any
	// PromptSuffix is added to the person's words: an arm of a comparison that names the way to work («use subchats»).
	PromptSuffix string
}

// SetupFile is a setup kept as a file: the environment under test, as configuration.
type SetupFile struct {
	Name         string            `yaml:"name"`
	RuntimeEnv   map[string]string `yaml:"runtime_env"`
	AgentEnv     map[string]string `yaml:"agent_env"`
	ClaudeConfig map[string]any    `yaml:"claude_config"`
	PromptSuffix string            `yaml:"prompt_suffix"`
}

var runtimeEnvKey = regexp.MustCompile(`^RUNTIME_[A-Z0-9_]+$`)

// ReadSetup reads a setup file; only RUNTIME_* settings reach the runtime.
func ReadSetup(path string) (SetupFile, error) {
	var s SetupFile
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("%s: %v", path, err)
	}
	for k := range s.RuntimeEnv {
		if !runtimeEnvKey.MatchString(k) {
			return s, fmt.Errorf("%s: runtime_env takes RUNTIME_* settings, not %s", path, k)
		}
	}
	return s, nil
}

func merged(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// Stand is one disposable harnessly: its own home, data, workspace, design library, port and tmux socket.
type Stand struct {
	Dir, Home, Data, Work, Library string
	Port                           int
	Workspace                      string

	name  string
	token string
	proc  *exec.Cmd
	cfg   StandConfig
}

// ErrInfra marks a failure of the stand, not of the agent: such a run is retried and never counted.
var ErrInfra = errors.New("infrastructure")

// sandboxLevel is SandboxAvailable, replaceable in tests.
var sandboxLevel = SandboxAvailable

func infra(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInfra, fmt.Sprintf(format, a...))
}

// Up makes and starts a stand named name on port.
func Up(cfg StandConfig, name string, port int) (*Stand, error) {
	// There is no way around the sandbox: an agent that cannot find what it needs searches the whole machine and writes
	// into what it finds, and nobody is there to refuse it a permission.
	if sandboxLevel() == 0 {
		return nil, infra("no Landlock on this machine: a stand would see every file of the account, so none is started")
	}
	dir := filepath.Join(cfg.Root, name)
	if err := os.RemoveAll(dir); err != nil {
		return nil, infra("clear %s: %v", dir, err)
	}
	// The workspace lives in the home, as a person's does: the runtime serves a chat's files only from there.
	s := &Stand{Dir: dir, Home: filepath.Join(dir, "home"), Data: filepath.Join(dir, "data"), Work: filepath.Join(dir, "home", "work"),
		Library: filepath.Join(dir, "design"), Port: port, name: name, cfg: cfg}
	for _, d := range []string{s.Home, s.Data, s.Work, s.Library, filepath.Join(s.Home, ".claude"), filepath.Join(s.Home, ".codex"),
		filepath.Join(s.Home, ".local", "bin"), filepath.Join(s.Data, "agent-env")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, infra("%v", err)
		}
	}
	if err := s.writeHome(cfg); err != nil {
		return nil, infra("home: %v", err)
	}
	if err := s.start(cfg); err != nil {
		s.Down()
		return nil, err
	}
	return s, nil
}

func (s *Stand) writeHome(cfg StandConfig) error {
	cred, err := os.ReadFile(filepath.Join(cfg.Login, "claude-credentials.json"))
	if err != nil {
		return fmt.Errorf("no Claude login in %s", cfg.Login)
	}
	if err := os.WriteFile(filepath.Join(s.Home, ".claude", ".credentials.json"), cred, 0o600); err != nil {
		return err
	}
	var acct struct {
		OAuthAccount json.RawMessage `json:"oauthAccount"`
	}
	if b, err := os.ReadFile(filepath.Join(cfg.Login, "claude-account.json")); err == nil {
		_ = json.Unmarshal(b, &acct)
	}
	if len(acct.OAuthAccount) == 0 {
		acct.OAuthAccount = json.RawMessage("null")
	}
	conf := map[string]any{}
	for k, v := range cfg.ClaudeConfig {
		conf[k] = v
	}
	// hasSeenAutoDefaultNudge: the one-time offer to make auto mode the default (2.1.32x) would hold the chat at a
	// question nobody answers.
	for k, v := range map[string]any{"hasCompletedOnboarding": true, "bypassPermissionsModeAccepted": true, "hasSeenAutoDefaultNudge": true, "oauthAccount": acct.OAuthAccount,
		"projects": map[string]any{s.Work: map[string]any{"hasTrustDialogAccepted": true}}} {
		conf[k] = v
	}
	cj, _ := json.Marshal(conf)
	if err := os.WriteFile(filepath.Join(s.Home, ".claude.json"), cj, 0o600); err != nil {
		return err
	}
	// The agent works without anyone to grant a permission: the eval judges what it does, not whether it asks to.
	if err := os.WriteFile(filepath.Join(s.Home, ".claude", "settings.json"), []byte(`{"permissions":{"defaultMode":"bypassPermissions"}}`+"\n"), 0o600); err != nil {
		return err
	}
	if b, err := os.ReadFile(filepath.Join(cfg.Login, "codex-auth.json")); err == nil {
		if b, err = codexStandAuth(b, time.Now()); err != nil {
			return fmt.Errorf("codex-auth.json: %v", err)
		}
		if err := os.WriteFile(filepath.Join(s.Home, ".codex", "auth.json"), b, 0o600); err != nil {
			return err
		}
	}
	bin := filepath.Join(s.Home, ".local", "bin")
	links := map[string]string{"harnessly": cfg.Harnessly}
	for _, d := range cfg.Tools {
		for _, n := range []string{"claude", "codex"} {
			if _, ok := links[n]; !ok && exists(filepath.Join(d, n)) {
				links[n] = filepath.Join(d, n)
			}
		}
	}
	for n, target := range links {
		if err := os.Symlink(target, filepath.Join(bin, n)); err != nil {
			return err
		}
	}
	path := "export PATH=" + bin + ":/usr/local/bin:/usr/bin:/bin\n"
	for _, f := range []string{".bashrc", ".profile"} {
		if err := os.WriteFile(filepath.Join(s.Home, f), []byte(path), 0o600); err != nil {
			return err
		}
	}
	// The person's claude.ai connectors are not harnessly's: a clean chat runs without them.
	tmp := filepath.Join(s.Dir, "tmp")
	env := map[string]string{"ENABLE_CLAUDEAI_MCP_SERVERS": "false", "TMPDIR": tmp, "CLAUDE_CODE_TMPDIR": tmp}
	for k, v := range cfg.AgentEnv {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + agentnet.Quote(env[k]) + "\n")
	}
	return os.WriteFile(filepath.Join(s.Data, "agent-env", "all.env"), []byte(b.String()), 0o600)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// standShell is the shell a stand's panes start with: bash where the machine has it, else sh.
func standShell() string {
	if exists("/bin/bash") {
		return "/bin/bash"
	}
	return "/bin/sh"
}

func (s *Stand) start(cfg StandConfig) error {
	// SHELL: tmux starts a pane with it, else with the account's login shell, which on a machine of service accounts is
	// /usr/sbin/nologin: every pane would exit at once and the stand would have no chat.
	env := []string{"HOME=" + s.Home, "USER=" + os.Getenv("USER"), "LANG=C.UTF-8", "TERM=xterm-256color", "SHELL=" + standShell(),
		"PATH=" + filepath.Join(s.Home, ".local", "bin") + ":/usr/local/bin:/usr/bin:/bin",
		"HARNESSLY_DATA_DIR=" + s.Data, "AGENT_DATA_DIR=" + s.Data,
		"RUNTIME_GATE_LISTEN=127.0.0.1:" + strconv.Itoa(s.Port), "RUNTIME_GATE_UPSTREAM=",
		"RUNTIME_TMUX_SOCKET=" + s.socket(), "TMUX_TMPDIR=" + s.Dir,
		"RUNTIME_UPDATE_CHECK=0", "RUNTIME_PREVIEW_LISTEN=off", "RUNTIME_DESIGN_LIBRARY_DIR=" + s.Library,
		// Landlock is the stand's boundary, as a container is: Codex's own bubblewrap sandbox cannot start inside it
		// (nor on machines without user namespaces), and with it every command of Codex fails.
		"RUNTIME_CODEX_SANDBOX=container"}
	keys := make([]string, 0, len(cfg.RuntimeEnv))
	for k := range cfg.RuntimeEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if runtimeEnvKey.MatchString(k) {
			env = append(env, k+"="+cfg.RuntimeEnv[k])
		}
	}
	if !cfg.RealImages {
		fake := filepath.Join(s.Dir, "fake-codex-image")
		script := "#!/bin/sh\nexec " + shellQuote(cfg.Harnessly) + " eval fake-imagegen \"$@\"\n"
		if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
			return infra("%v", err)
		}
		env = append(env, "RUNTIME_IMAGEGEN_BIN="+fake)
	}
	logf, err := os.Create(filepath.Join(s.Dir, "runtime.log"))
	if err != nil {
		return infra("%v", err)
	}
	tmp := filepath.Join(s.Dir, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		logf.Close()
		return infra("%v", err)
	}
	env = append(env, "TMPDIR="+tmp, "CLAUDE_CODE_TMPDIR="+tmp)
	argv := append([]string{cfg.Harnessly, "eval", "sandbox"}, s.sandboxArgs(cfg)...)
	argv = append(argv, "--", cfg.Runtime)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = s.Dir, env, logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logf.Close()
		return infra("runtime: %v", err)
	}
	logf.Close()
	s.proc = cmd
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		tok, err := os.ReadFile(filepath.Join(s.Data, "cli-token"))
		if err == nil && len(bytes.TrimSpace(tok)) > 0 {
			s.token = string(bytes.TrimSpace(tok))
			var ws struct {
				ID string `json:"id"`
			}
			if err := s.api(http.MethodPost, "/api/workspace", map[string]any{"name": "eval", "directory": s.Work}, &ws); err == nil && ws.ID != "" {
				s.Workspace = ws.ID
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return infra("the runtime did not come up on port %d (see %s)", s.Port, filepath.Join(s.Dir, "runtime.log"))
}

func (s *Stand) socket() string { return "ev-" + s.name }

// systemRO are the machine's folders a stand reads: programs, libraries, settings, devices' and processes' views.
var systemRO = []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/proc", "/sys", "/run", "/var/lib/ca-certificates", "/snap"}

// sandboxArgs confine the stand to its own folder (read and write), the system and its tools (read): another account's
// or another stand's files, the machine's homes and its /tmp are out of reach of the runtime and every agent it starts.
func (s *Stand) sandboxArgs(cfg StandConfig) []string {
	args := []string{"--rw", s.Dir, "--rw", "/dev"}
	ro := append([]string{}, systemRO...)
	for _, b := range []string{cfg.Runtime, cfg.Harnessly} {
		if real, err := filepath.EvalSymlinks(b); err == nil {
			ro = append(ro, real)
		}
	}
	for _, d := range cfg.Tools {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			d = real
		}
		ro = append(ro, d)
		if filepath.Base(d) == ".bin" {
			ro = append(ro, filepath.Dir(d))
		}
	}
	for _, p := range ro {
		args = append(args, "--ro", p)
	}
	return args
}

func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }

func (s *Stand) api(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:"+strconv.Itoa(s.Port)+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("x-cli-token", s.token)
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, clip(string(b), 200))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// Chat starts a chat of agent (claude, codex) on model with the person's words, the platform's way.
func (s *Stand) Chat(agent, model, prompt string) (string, error) {
	return s.ChatWithEffort(agent, model, "", prompt)
}

// ChatWithEffort starts a chat without silently using the model's default effort.
func (s *Stand) ChatWithEffort(agent, model, effort, prompt string) (string, error) {
	panel := "claude-code"
	if agent == "codex" {
		panel = "codex-cli"
	}
	body := map[string]any{"workspaceId": s.Workspace, "panelType": panel, "prompt": prompt}
	if model != "" {
		body["model"] = model
	}
	if effort != "" {
		body["effort"] = effort
	}
	var tab struct {
		TabID    string `json:"tabId"`
		Launched bool   `json:"launched"`
	}
	if err := s.api(http.MethodPost, "/api/cli/tabs", body, &tab); err != nil {
		return "", infra("start the chat: %v", err)
	}
	if !tab.Launched {
		return tab.TabID, infra("the agent was not launched")
	}
	return tab.TabID, nil
}

// Turn states a chat ends a turn in.
var turnOver = map[string]bool{"ready-for-review": true, "idle": true, "needs-input": true}

// Wait waits until the chat's turn is over; the answer is the last state ("timeout" when the time ran out).
func (s *Stand) Wait(ctx context.Context, tab string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	last, worked := "", false
	for time.Now().Before(deadline) && ctx.Err() == nil {
		var st struct {
			Alive    bool   `json:"alive"`
			CLIState string `json:"cliState"`
		}
		if err := s.api(http.MethodGet, "/api/cli/tabs/"+tab+"/status?workspaceId="+s.Workspace, nil, &st); err == nil {
			last = st.CLIState
			if last == "busy" {
				worked = true
			}
			if turnOver[last] && (worked || s.transcriptFile() != "") {
				// The pane's intents are written 200 ms after the last one; the transcript's last lines a moment later.
				time.Sleep(1500 * time.Millisecond)
				return last
			}
			if last == "inactive" && worked {
				return last
			}
		}
		time.Sleep(2 * time.Second)
	}
	if ctx.Err() != nil {
		return "stopped"
	}
	return "timeout"
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// transcriptFile is the Claude Code session of the person's chat: the one that started first. A chat the agent
// delegated to works in the same folder and often writes last, so the newest file is not the person's.
func (s *Stand) transcriptFile() string {
	dir := filepath.Join(s.Home, ".claude", "projects", nonAlnum.ReplaceAllString(s.Work, "-"))
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	best, bestT := "", time.Time{}
	for _, f := range files {
		t := sessionStart(f)
		if best == "" || t.Before(bestT) {
			best, bestT = f, t
		}
	}
	return best
}

// sessionStart is the time of a Claude Code session's first timestamped line (its file's time when it has none).
func sessionStart(file string) time.Time {
	fh, err := os.Open(file)
	if err != nil {
		return time.Time{}
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for n := 0; sc.Scan() && n < 50; n++ {
		var line struct {
			Timestamp time.Time `json:"timestamp"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && !line.Timestamp.IsZero() {
			return line.Timestamp
		}
	}
	if st, err := fh.Stat(); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

// Collect reads the facts of a chat and copies its transcript to keep.
func (s *Stand) Collect(agent, tab, keep string) (Facts, error) {
	var f Facts
	if agent == "codex" {
		var err error
		f, err = ReadCodexRollout(s.codexRollout())
		if err != nil {
			return f, infra("no Codex session: %v", err)
		}
		_ = copyFile(s.codexRollout(), keep)
	} else {
		t := s.transcriptFile()
		if t == "" {
			return f, infra("no Claude session in the workspace")
		}
		subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(t, ".jsonl"), "subagents", "*.jsonl"))
		paths := append([]string{t}, subs...)
		for _, o := range s.sessionFiles() {
			if !slices.Contains(paths, o) {
				paths = append(paths, o)
			}
		}
		var err error
		if f, err = ReadClaudeTranscript(paths...); err != nil {
			return f, infra("transcript: %v", err)
		}
		_ = copyFile(t, keep)
	}
	f.Pane = ReadPaneIntents(filepath.Join(s.Data, "pane-intents.json"), tab)
	f.Images = ReadImageJobs(filepath.Join(s.Data, "images"), tab)
	f.Files = append(ReadFiles(s.Work, ""), ReadFiles(s.Library, "design:")...)
	return f, nil
}

// sessionFiles are every Claude Code transcript of the stand, of any folder: the person's chat, its subagents, the
// chats it delegated to (they work in folders of their own) and their subagents, teammates.
func (s *Stand) sessionFiles() []string {
	var out []string
	_ = filepath.WalkDir(filepath.Join(s.Home, ".claude", "projects"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// pendingFor is how long a tool call without its result keeps an agent counted as working: longer than the longest
// Bash (10 min), so a session killed mid-call does not hold the run past it.
const pendingFor = 11 * time.Minute

// working says whether an agent of the stand is in the middle of a tool call (a long Bash, a wait for a chat): its
// transcript is quiet then, and only the call without a result tells it still works.
func (s *Stand) working() bool {
	for _, p := range s.sessionFiles() {
		st, err := os.Stat(p)
		if err != nil || time.Since(st.ModTime()) > pendingFor {
			continue
		}
		if pendingCalls(p) > 0 {
			return true
		}
	}
	return false
}

// pendingCalls counts a transcript's tool calls that have no result yet.
func pendingCalls(file string) int {
	raw, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	open := map[string]bool{}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if !bytes.Contains(line, []byte(`"tool_use`)) && !bytes.Contains(line, []byte(`"tool_result`)) {
			continue
		}
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || (e.Type != "assistant" && e.Type != "user") {
			continue
		}
		var blocks []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			ToolUseID string `json:"tool_use_id"`
		}
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			switch {
			case b.Type == "tool_use" && b.ID != "":
				open[b.ID] = true
			case b.Type == "tool_result":
				delete(open, b.ToolUseID)
			}
		}
	}
	return len(open)
}

// lastActivity is when an agent of the stand last wrote to its transcript.
func (s *Stand) lastActivity() time.Time {
	var last time.Time
	for _, p := range s.sessionFiles() {
		if st, err := os.Stat(p); err == nil && st.ModTime().After(last) {
			last = st.ModTime()
		}
	}
	if st, err := os.Stat(s.codexRollout()); err == nil && st.ModTime().After(last) {
		last = st.ModTime()
	}
	return last
}

func (s *Stand) chatState(tab string) string {
	var st struct {
		CLIState string `json:"cliState"`
	}
	if err := s.api(http.MethodGet, "/api/cli/tabs/"+tab+"/status?workspaceId="+s.Workspace, nil, &st); err != nil {
		return ""
	}
	return st.CLIState
}

// Settle waits, after the person's chat ended its turn, until no agent of the stand wrote for quiet and the chat is not
// working, or until the deadline. The answer is when the agents last did something, and whether they went quiet.
func (s *Stand) Settle(ctx context.Context, tab string, quiet time.Duration, deadline time.Time) (time.Time, bool) {
	for ctx.Err() == nil {
		last := s.lastActivity()
		if time.Since(last) >= quiet && s.chatState(tab) != "busy" && !s.working() {
			return last, true
		}
		if time.Now().After(deadline) {
			return last, false
		}
		time.Sleep(3 * time.Second)
	}
	return s.lastActivity(), false
}

// Send types a message into a chat, as the person would while it works.
func (s *Stand) Send(tab, text string) error {
	return s.api(http.MethodPost, "/api/cli/tabs/"+tab+"/send?workspaceId="+s.Workspace, map[string]any{"content": text}, nil)
}

// Exec runs a shell script in the workspace inside the stand's sandbox, with the stand's home and extra variables.
func (s *Stand) Exec(ctx context.Context, script string, extra []string, timeout time.Duration) (int, string) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := append([]string{s.cfg.Harnessly, "eval", "sandbox"}, s.sandboxArgs(s.cfg)...)
	argv = append(argv, "--", "/bin/sh", "-c", script)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	tmp := filepath.Join(s.Dir, "tmp")
	_ = os.MkdirAll(tmp, 0o700)
	cmd.Dir = s.Work
	cmd.Env = append([]string{"HOME=" + s.Home, "USER=" + os.Getenv("USER"), "LANG=C.UTF-8", "TMPDIR=" + tmp,
		"PATH=" + filepath.Join(s.Home, ".local", "bin") + ":/usr/local/bin:/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1"}, extra...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		if ctx.Err() != nil {
			out = append(out, []byte("\n(timed out)")...)
		}
	}
	return code, clip(string(out), 4000)
}

func (s *Stand) codexRollout() string {
	files, _ := filepath.Glob(filepath.Join(s.Home, ".codex", "sessions", "*", "*", "*", "*.jsonl"))
	sort.Strings(files)
	if len(files) == 0 {
		return ""
	}
	return files[len(files)-1]
}

func copyFile(src, dst string) error {
	if src == "" || dst == "" {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

// Down stops the stand's runtime and agents and removes its folder.
func (s *Stand) Down() {
	if s.proc != nil && s.proc.Process != nil {
		_ = syscall.Kill(-s.proc.Process.Pid, syscall.SIGTERM)
	}
	kill := exec.Command("tmux", "-L", s.socket(), "kill-server")
	kill.Env = []string{"TMUX_TMPDIR=" + s.Dir, "PATH=/usr/local/bin:/usr/bin:/bin"}
	_ = kill.Run()
	killByHome(s.Home)
	time.Sleep(300 * time.Millisecond)
	if s.proc != nil && s.proc.Process != nil {
		_ = syscall.Kill(-s.proc.Process.Pid, syscall.SIGKILL)
	}
	_ = os.RemoveAll(s.Dir)
}

// killByHome ends whatever still runs with the stand's home: an agent the tmux server left behind.
func killByHome(home string) {
	procs, _ := filepath.Glob("/proc/[0-9]*/environ")
	want := []byte("HOME=" + home + "\x00")
	for _, p := range procs {
		b, err := os.ReadFile(p)
		if err != nil || !bytes.Contains(b, want) {
			continue
		}
		if pid, err := strconv.Atoi(filepath.Base(filepath.Dir(p))); err == nil && pid != os.Getpid() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// codexStandAuth makes a login copy without a refresh token one Codex accepts: Codex refuses an auth.json without the
// refresh_token field (and shows its sign-in screen), so the field is there but empty, and last_refresh is now, so Codex
// does not try to refresh an access token that is still good. The real refresh token never reaches a stand: using it
// there would rotate it and sign the person's own Codex out.
func codexStandAuth(raw []byte, now time.Time) ([]byte, error) {
	var auth map[string]any
	if err := json.Unmarshal(raw, &auth); err != nil {
		return nil, err
	}
	if tokens, ok := auth["tokens"].(map[string]any); ok {
		tokens["refresh_token"] = ""
	}
	auth["last_refresh"] = now.UTC().Format(time.RFC3339Nano)
	return json.MarshalIndent(auth, "", "  ")
}

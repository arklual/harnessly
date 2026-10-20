package codex

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/ly-solutions/harnessly/internal/agents"
)

const session = "11111111-2222-4333-8444-555555555555"

func choice(model, effort string) *agents.Launch {
	l := &agents.Launch{}
	if model != "" {
		l.Model = &model
	}
	if effort != "" {
		l.Effort = &effort
	}
	return l
}

var testMCP = MCP{Server: "rt-preview", URL: MCPURL(8022, "/api/cli/preview/mcp"), TokenHeader: "x-rt-token", TokenEnv: "RT_PREVIEW_TOKEN"}

// launch-options: Codex model and effort flags, for the launcher and for codex itself.
func TestLaunchArgsFlags(t *testing.T) {
	if got, _ := LauncherFlags(choice("gpt-5.4", "xhigh")); !slices.Equal(got, []string{"--model", "'gpt-5.4'", "--effort", "'xhigh'"}) {
		t.Fatalf("%q", got)
	}
	if got, _ := RuntimeLaunchArgs(choice("gpt-5.4", "xhigh")); !slices.Equal(got, []string{"-m", "gpt-5.4", "-c", `model_reasoning_effort="xhigh"`}) {
		t.Fatalf("%q", got)
	}
	if got, _ := RuntimeLaunchArgs(choice("", "low")); !slices.Equal(got, []string{"-c", `model_reasoning_effort="low"`}) {
		t.Fatalf("%q", got)
	}
	if _, err := RuntimeLaunchArgs(choice("bad'; id", "")); err == nil {
		t.Fatal("model re-checked")
	}
	if got, err := LauncherFlags(nil); got != nil || err != nil {
		t.Fatal("auto")
	}
}

// launch-options: model and effort are forwarded to codex after resume and the flags.
func TestRuntimeArgsOrder(t *testing.T) {
	dir := t.TempDir()
	c := LaunchConfig{Hooks: &HookConfig{Script: "/data/codex-hook.sh", UserConfig: filepath.Join(dir, "none.toml")}, MCP: testMCP}
	args, parseErr, err := c.RuntimeArgs("", session, choice("gpt-5.4", "xhigh"))
	if err != nil || parseErr {
		t.Fatal(err)
	}
	hook := `[{matcher=".*",hooks=[{type="command",command="/data/codex-hook.sh"}]}]`
	want := []string{"resume", session}
	for _, ev := range HookEvents {
		want = append(want, "-c", "hooks."+ev+"="+hook)
	}
	want = append(want,
		"-c", `mcp_servers.rt-preview.url="http://127.0.0.1:8022/api/cli/preview/mcp"`,
		"-c", `mcp_servers.rt-preview.env_http_headers={"x-rt-token"="RT_PREVIEW_TOKEN"}`,
		"--dangerously-bypass-hook-trust", "-c", "check_for_update_on_startup=false", "-m", "gpt-5.4", "-c", `model_reasoning_effort="xhigh"`)
	if !slices.Equal(args, want) {
		t.Fatalf("got  %q\nwant %q", args, want)
	}
	prompt := filepath.Join(dir, "codex-prompt.md")
	_ = os.WriteFile(prompt, []byte("# Context\nIt has 'quotes' and ''' and \"dq\"\n"), 0o600)
	c.WorkspacePrompt = func(string) string { return prompt }
	c.SkipPermissions = func() bool { return true }
	args, _, _ = c.RuntimeArgs("ws", "", nil)
	i := slices.Index(args, "--yolo")
	if i < 2 || !strings.HasPrefix(args[i-1], "developer_instructions=") || args[0] != "-c" {
		t.Fatalf("%q", args)
	}
	var parsed map[string]string
	if _, err := toml.Decode(args[i-1], &parsed); err != nil || parsed["developer_instructions"] != "# Context\nIt has 'quotes' and ''' and \"dq\"\n" || strings.Contains(args[i-1], "\n") {
		t.Fatalf("one-line TOML value: %v %q", err, parsed)
	}
	if _, _, err := c.RuntimeArgs("", "nope", nil); err == nil || err.Error() != "Invalid codex session ID format: nope" {
		t.Fatal(err)
	}
	c.MCP.URL = MCPURL(0, "/x")
	if _, _, err := c.RuntimeArgs("", "", nil); err == nil {
		t.Fatal("unknown port")
	}
}

// preview-mcp-config: Codex gets the url and the name of a variable for the header, never the token.
func TestMCPArgsAreTOML(t *testing.T) {
	args := MCPArgs(testMCP)
	if len(args) != 4 || args[0] != "-c" || args[2] != "-c" {
		t.Fatalf("%q", args)
	}
	var a, b map[string]map[string]map[string]any
	if _, err := toml.Decode(args[1], &a); err != nil || a["mcp_servers"]["rt-preview"]["url"] != "http://127.0.0.1:8022/api/cli/preview/mcp" {
		t.Fatalf("%v %v", err, a)
	}
	if _, err := toml.Decode(args[3], &b); err != nil {
		t.Fatal(err)
	}
	if h, _ := b["mcp_servers"]["rt-preview"]["env_http_headers"].(map[string]any); h["x-rt-token"] != "RT_PREVIEW_TOKEN" {
		t.Fatalf("%v", b)
	}
}

func TestHookFlagsMergeUserEntries(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(cfg, []byte(`model = "gpt-5.4"
[[hooks.Stop]]
matcher = "x"
hooks = [{ type = "command", command = "notify \"done\"", timeout = 5 }]

[[hooks.Stop]]
hooks = [{ type = "other", command = "skipped" }]

[hooks]
SessionStart = [{ hooks = [{ command = "s", type = "command" }] }]
Unknown = [{ hooks = [] }]
`), 0o600)
	h := &HookConfig{Script: "/d/codex-hook.sh", UserConfig: cfg}
	args, parseErr, err := h.Flags()
	if err != nil || parseErr {
		t.Fatal(err)
	}
	ours := `{matcher=".*",hooks=[{type="command",command="/d/codex-hook.sh"}]}`
	byEvent := map[string]string{}
	for i := 1; i < len(args); i += 2 {
		k, v, _ := strings.Cut(args[i], "=")
		byEvent[k] = v
	}
	if got := byEvent["hooks.Stop"]; got != `[`+ours+`,{matcher="x",hooks=[{type="command",command="notify \"done\"",timeout=5}]}]` {
		t.Fatalf("Stop: %s", got)
	}
	if got := byEvent["hooks.SessionStart"]; got != `[`+ours+`,{hooks=[{command="s",type="command"}]}]` {
		t.Fatalf("SessionStart: %s", got)
	}
	if got := byEvent["hooks.PreToolUse"]; got != `[`+ours+`]` {
		t.Fatalf("PreToolUse: %s", got)
	}
	for k, v := range byEvent {
		var parsed map[string]any
		if _, err := toml.Decode(k+"="+v, &parsed); err != nil {
			t.Errorf("%s does not parse: %v", k, err)
		}
	}
	_ = os.WriteFile(cfg, []byte("[broken"), 0o600)
	future := mustStat(t, cfg).ModTime().Add(1e9)
	_ = os.Chtimes(cfg, future, future)
	args, parseErr, _ = h.Flags()
	if !parseErr || len(args) != 12 || args[1] != "hooks.SessionStart=["+ours+"]" {
		t.Fatalf("a broken config applies only ours: %v %q", parseErr, args)
	}
}

func mustStat(t *testing.T, p string) os.FileInfo {
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func scriptConfig() LauncherScriptConfig {
	return LauncherScriptConfig{
		DataDirs:       []string{".rt-data", ".rt-legacy"},
		PortFile:       "port",
		TokenFile:      "cli-token",
		LaunchArgsPath: "/api/codex/launch-args",
		TokenHeaders:   []string{"x-rt-token", "x-old-token"},
		TokenEnv:       "RT_PREVIEW_TOKEN",
	}
}

// codex-prompt and launch-options: the launcher asks the runtime for args and forwards model and effort.
func TestLauncherScriptContent(t *testing.T) {
	s := string(LauncherScript(scriptConfig()))
	for _, want := range []string{"/api/codex/launch-args", "spawn('codex', args", "item === '--model' && next", "item === '--effort' && next", `headers["x-old-token"] = token;`, `["RT_PREVIEW_TOKEN"]: token`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.HasPrefix(s, "#!/usr/bin/env node\n") {
		t.Fatal("shebang")
	}
}

// codex-launcher-token-env: codex gets the token in the variable the header is read from, not in its arguments.
func TestLauncherPassesTokenThroughEnv(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not here")
	}
	home := t.TempDir()
	token := strings.Repeat("a", 64)
	var body map[string]any
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		headers = r.Header.Clone()
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"args":["--flag"]}`))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	data := filepath.Join(home, ".rt-legacy") // only the legacy folder exists: it is used
	_ = os.MkdirAll(data, 0o700)
	_ = os.WriteFile(filepath.Join(data, "port"), []byte(port), 0o600)
	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte(token+"\n"), 0o600)
	script := filepath.Join(data, "codex-launcher.js")
	if err := EnsureLauncher(script, LauncherScript(scriptConfig())); err != nil {
		t.Fatal(err)
	}
	if mustStat(t, script).Mode().Perm() != 0o700 {
		t.Fatal("mode")
	}
	bin := filepath.Join(home, "bin")
	_ = os.MkdirAll(bin, 0o700)
	_ = os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\necho \"args=$*\"\necho \"token=$RT_PREVIEW_TOKEN\"\n"), 0o755)
	cmd := exec.Command("node", script, "--workspace-id", "w1", "--model", "gpt-5.4", "--prompt", "hello")
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	lines := strings.Split(string(out), "\n")
	if lines[0] != "args=--flag hello" || lines[1] != "token="+token || strings.Contains(lines[0], token) {
		t.Fatalf("%q", out)
	}
	if body["workspaceId"] != "w1" || body["model"] != "gpt-5.4" || body["resumeSessionId"] != nil || body["effort"] != nil {
		t.Fatalf("payload %v", body)
	}
	if _, has := body["prompt"]; has {
		t.Fatal("the prompt is not sent to the runtime")
	}
	if headers.Get("x-rt-token") != token || headers.Get("x-old-token") != token {
		t.Fatalf("headers %v", headers)
	}
	// Codex's exit code is the launcher's.
	_ = os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nexit 7\n"), 0o755)
	cmd = exec.Command("node", script)
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
	if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("exit %v", err)
	}
}

func TestLauncherLine(t *testing.T) {
	dir := t.TempDir()
	c := LaunchConfig{LauncherScript: filepath.Join(dir, "data dir", "codex-launcher.js"), LauncherContent: []byte("x")}
	line, err := c.LauncherLine("ws'1", session, choice("gpt-5.4", "xhigh"))
	want := "node '" + c.LauncherScript + "' --workspace-id 'ws'\\''1' --resume-session-id '" + session + "' --model 'gpt-5.4' --effort 'xhigh'"
	if err != nil || line != want {
		t.Fatalf("%q %v", line, err)
	}
	if raw, _ := os.ReadFile(c.LauncherScript); string(raw) != "x" {
		t.Fatal("script written")
	}
	_ = os.Chmod(c.LauncherScript, 0o755)
	_, _ = c.LauncherLine("", "", nil)
	if mustStat(t, c.LauncherScript).Mode().Perm() != 0o700 {
		t.Fatal("an equal script gets its mode narrowed")
	}
	if line, _ := c.LauncherLine("", "", nil); line != "node '"+c.LauncherScript+"'" {
		t.Fatalf("%q", line)
	}
	if _, err := c.LauncherLine("", "x", nil); err == nil {
		t.Fatal("resume id must be a UUID")
	}
	if TOMLBasicString("a\nb'''") != `"a\nb'''"` {
		t.Fatal("toml string")
	}
}

// The services' tokens must reach the commands Codex runs: by default Codex drops every variable named like a secret.
// The flags are valid TOML, come after the instructions and before the permissions, and keep the CLI token out.
func TestServiceEnvArgs(t *testing.T) {
	dir := t.TempDir()
	c := LaunchConfig{Hooks: &HookConfig{Script: "/data/codex-hook.sh", UserConfig: filepath.Join(dir, "none.toml")}, MCP: testMCP}
	plain, _, err := c.RuntimeArgs("", "", nil)
	if err != nil || slices.ContainsFunc(plain, func(a string) bool { return strings.Contains(a, "shell_environment_policy") }) {
		t.Fatalf("off by default: %v %q", err, plain)
	}
	prompt := filepath.Join(dir, "codex-prompt.md")
	_ = os.WriteFile(prompt, []byte("# Context\n"), 0o600)
	c.WorkspacePrompt = func(string) string { return prompt }
	c.SkipPermissions = func() bool { return true }
	c.PassServiceEnv = true
	c.ShellEnvExclude = []string{"RT_PREVIEW_TOKEN", ""}
	args, _, err := c.RuntimeArgs("ws", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(args, "shell_environment_policy.ignore_default_excludes=true")
	if i < 1 || args[i-1] != "-c" || args[i+1] != "-c" || args[i+3] != "--yolo" {
		t.Fatalf("flags misplaced: %q", args)
	}
	if !strings.HasPrefix(args[i-2], "developer_instructions=") {
		t.Fatalf("after the instructions: %q", args)
	}
	var parsed map[string]map[string]any
	if _, err := toml.Decode(args[i+2], &parsed); err != nil {
		t.Fatalf("%q: %v", args[i+2], err)
	}
	if ex, _ := parsed["shell_environment_policy"]["exclude"].([]any); len(ex) != 1 || ex[0] != "RT_PREVIEW_TOKEN" {
		t.Fatalf("exclude: %v", parsed)
	}
}

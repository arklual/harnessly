package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLauncherClaude(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "review")
	if err := Scaffold(pkg, "acme.review", "Ревью", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "instructions.md"), []byte("  Review only, never push.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "hooks.json")
	own := `{"hooks":{"PreToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"design-guard"}]}],"Stop":[]},"env":{"A":"1"}}`
	if err := os.WriteFile(base, []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	l := &Launcher{Registry: reg, GuardCommand: "/bin/harnessly hook app-guard"}
	got, err := l.Claude([]string{"acme.review"}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PluginDirs) != 1 || got.PluginDirs[0] != pkg {
		t.Fatalf("plugins: %v", got.PluginDirs)
	}
	prompt, _ := os.ReadFile(got.PromptFile)
	if !strings.Contains(string(prompt), "## App: Ревью (acme.review)\n\nReview only, never push.") {
		t.Fatalf("prompt: %q", prompt)
	}
	raw, _ := os.ReadFile(got.Settings)
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
		Env         map[string]string `json:"env"`
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	pre := s.Hooks["PreToolUse"]
	if len(pre) != 2 || pre[0].Matcher != "*" || !strings.HasPrefix(pre[0].Hooks[0].Command, "/bin/harnessly hook app-guard --policy ") || pre[0].Hooks[0].Timeout != GuardTimeoutSeconds || pre[1].Matcher != "Edit" {
		t.Fatalf("PreToolUse: %+v", pre)
	}
	policy := strings.Trim(strings.TrimPrefix(pre[0].Hooks[0].Command, "/bin/harnessly hook app-guard --policy "), "'")
	var snap Snapshot
	rawSnap, _ := os.ReadFile(policy)
	if json.Unmarshal(rawSnap, &snap) != nil || len(snap.Apps) != 1 || snap.Apps[0].Name != "Ревью" || len(snap.Protected) != 2 || snap.Protected[1] != pkg {
		t.Fatalf("snapshot: %s", rawSnap)
	}
	// Claude's own deny rules shut the file tools out of the registry and the package, besides the hook.
	for _, d := range s.Permissions.Deny {
		if strings.HasPrefix(d, "Write(") {
			t.Errorf("Claude matches no Write(…) path rule and warns of it: %q", d)
		}
	}
	for _, want := range []string{"Edit(/" + reg.Root + "/**)", "Edit(/" + pkg + "/**)"} {
		found := false
		for _, d := range s.Permissions.Deny {
			found = found || d == want
		}
		if !found {
			t.Errorf("no deny rule %q in %v", want, s.Permissions.Deny)
		}
	}
	if s.Env["A"] != "1" {
		t.Fatalf("the base settings were lost: %s", raw)
	}
	if again, _ := l.Claude([]string{"acme.review"}, base); again.Settings != got.Settings || again.PromptFile != got.PromptFile {
		t.Fatalf("the same launch wrote new files: %+v %+v", again, got)
	}
	if _, err := l.Claude([]string{"acme.nope"}, base); err == nil {
		t.Fatal("an app not installed launched")
	}
}

// A launch with apps gives the agent the token of that launch in a private file the CLI reads; the person's
// cli-token is not in the launch.
func TestLauncherAgentToken(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "review")
	if err := Scaffold(pkg, "acme.review", "Ревью", "chat"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.WriteFile(base, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var sessions []string
	l := &Launcher{Registry: reg, AgentToken: func(session string, ttl time.Duration) (string, error) {
		if ttl != AgentTokenTTL {
			t.Fatalf("ttl %v", ttl)
		}
		sessions = append(sessions, session)
		return "agent." + session + ".1.mac", nil
	}}
	got, err := l.Claude([]string{"acme.review"}, base)
	if err != nil {
		t.Fatal(err)
	}
	first, rest, _ := strings.Cut(got.Env, " ")
	if !strings.HasPrefix(rest, `HARNESSLY_SESSION="$(tmux display-message`) {
		t.Fatalf("the launch names the tab's session before the wall: %q", got.Env)
	}
	file := strings.Trim(strings.TrimPrefix(first, "HARNESSLY_CLI_TOKEN_FILE="), "'")
	raw, err := os.ReadFile(file)
	info, _ := os.Stat(file)
	if err != nil || strings.TrimSpace(string(raw)) != "agent."+sessions[0]+".1.mac" || info.Mode().Perm() != 0o600 {
		t.Fatalf("%q: %q %v %v", got.Env, raw, err, info)
	}
	again, _ := l.Claude([]string{"acme.review"}, base)
	if len(sessions) != 2 || sessions[0] == sessions[1] || again.Env == got.Env {
		t.Fatalf("every launch has its own token: %v", sessions)
	}
	l.AgentToken = func(string, time.Duration) (string, error) { return "", os.ErrPermission }
	if _, err := l.Claude([]string{"acme.review"}, base); err == nil {
		t.Fatal("a launch whose token could not be made starts with none said")
	}
}

// Both MCP configs of a chat with apps carry the launch's token, not the person's, and the configs written before with
// the person's token go.
func TestLaunchMCPConfigsCarryTheAgentToken(t *testing.T) {
	data := t.TempDir()
	reg := Open(data)
	dir := filepath.Join(reg.Root, "launch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "mcp-0123456789abcdef.json")
	if err := os.WriteFile(stale, []byte(`{"person-token":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(data, "preview-mcp.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{"rt":{"type":"http","url":"http://127.0.0.1:1/mcp","headers":{"x-cli-token":"person-token"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	l := &Launcher{Registry: reg, MCPConfigPath: cfg, AgentToken: func(session string, _ time.Duration) (string, error) { return "agent." + session + ".1.mac", nil }}
	path, token, _, err := l.agentTokenFile()
	if err != nil || path == "" {
		t.Fatal(err)
	}
	host, err := l.hostMCP(dir, token)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := l.runtimeMCP(dir, token)
	if err != nil {
		t.Fatal(err)
	}
	extraSrc := filepath.Join(data, "mcp-servers.json")
	if err := os.WriteFile(extraSrc, []byte(`{"mcpServers":{"mcp-linear":{"type":"http","url":"http://127.0.0.1:1/api/cli/mcp-proxy/a","headers":{"x-cli-token":"person-token"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	extra, err := retokenMCP(dir, "xmcp", extraSrc, token)
	if err != nil || filepath.Dir(extra) != dir {
		t.Fatal(extra, err)
	}
	for _, f := range []string{host, rt, extra} {
		raw, _ := os.ReadFile(f)
		if strings.Contains(string(raw), "person-token") || !strings.Contains(string(raw), token) {
			t.Fatalf("%s: %s", f, raw)
		}
	}
}

func TestLauncherWritesClaudeRulesAndProbesGuard(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := scaffold(t, "acme.review", "chat")
	raw, _ := os.ReadFile(filepath.Join(pkg, ManifestPath))
	raw = []byte(strings.Replace(string(raw), `"instructions"`, `"tools": {"deny": ["Bash(git push*)"], "confirm": ["Bash(npm publish*)"]}, "instructions"`, 1))
	if !strings.Contains(string(raw), `"tools"`) {
		t.Skip("the chat template has no agent.instructions to hang the rules on")
	}
	if err := os.WriteFile(filepath.Join(pkg, ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(base, []byte(`{}`), 0o600)
	l := &Launcher{Registry: reg, GuardCommand: "/bin/harnessly hook app-guard"}
	got, err := l.Claude([]string{"acme.review"}, base)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(got.Settings)
	for _, want := range []string{`"Bash(git push:*)"`, `"ask"`, `"Bash(npm publish:*)"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("settings lack %s: %s", want, body)
		}
	}
	l.GuardProbe = func(string) error { return os.ErrNotExist }
	if _, err := l.Claude([]string{"acme.review"}, base); err == nil || !strings.Contains(err.Error(), "guard does not answer") {
		t.Errorf("a guard that does not answer must stop the launch: %v", err)
	}
}

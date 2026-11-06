package codex

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/ly-solutions/harnessly/internal/agents"
)

type fakeCodexApps struct {
	ids          []string
	conversation string
	apps         agents.CodexApps
}

func (f *fakeCodexApps) Claude([]string, string) (agents.ClaudeApps, error) {
	return agents.ClaudeApps{}, nil
}

func (f *fakeCodexApps) ClaudeFor(string, []string, string) (agents.ClaudeApps, error) {
	return agents.ClaudeApps{}, nil
}

func (f *fakeCodexApps) Codex(conversation string, ids []string) (agents.CodexApps, error) {
	f.conversation = conversation
	f.ids = ids
	return f.apps, nil
}

func withApps(l *agents.Launch, ids ...string) *agents.Launch {
	l.Apps = ids
	return l
}

func appsConfig(t *testing.T, f *fakeCodexApps) LaunchConfig {
	dir := t.TempDir()
	user := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(user, []byte("[[hooks.PreToolUse]]\nmatcher = \"Bash\"\n[[hooks.PreToolUse.hooks]]\ntype = \"command\"\ncommand = \"person-hook\"\n"), 0o600)
	prompt := filepath.Join(dir, "codex-prompt.md")
	_ = os.WriteFile(prompt, []byte("# Workspace\n"), 0o600)
	return LaunchConfig{
		Hooks: &HookConfig{Script: "/data/codex-hook.sh", UserConfig: user}, MCP: testMCP, Apps: f,
		WorkspacePrompt: func(string) string { return prompt },
	}
}

// A launch with apps: the guard is the first PreToolUse hook (before the runtime's and the person's), by `-c`, the apps'
// servers come as mcp_servers flags, the instructions join the workspace's, and the words codex runs behind come apart.
func TestRuntimeStartWithApps(t *testing.T) {
	f := &fakeCodexApps{apps: agents.CodexApps{
		Guard:        "'/bin/hl' hook app-guard --policy '/d/policy-1.json' --agent codex",
		Instructions: "# Apps on in this chat\n\n- skill s: /p/skills/s/SKILL.md\n",
		TokenFile:    "/d/app-launch/s1/token",
		Env:          "HARNESSLY_CLI_TOKEN_FILE='/d/app-launch/s1/token'",
		Wrap:         "'bwrap' '--dev-bind' '/' '/' --",
		Servers: []agents.CodexAppServer{
			{Name: "plugin_acme-cashback_bank", URL: "https://x.test/mcp", Headers: map[string]string{"X-Key": "k"}},
			{Name: "plugin_acme-cashback_secret", URL: "http://127.0.0.1:8022/api/cli/apps/mcp/acme.cashback/secret", TokenHeaders: []string{"x-rt-token"}},
			{Name: "plugin_acme-cashback_local", Command: "node", Args: []string{"srv.js", "--q"}, Env: map[string]string{"A": "1"}},
		},
	}}
	c := appsConfig(t, f)
	l := withApps(choice("gpt-5.4", ""), "acme.cashback")
	l.Conversation = "tab-c1"
	start, parseErr, err := c.RuntimeStart("ws", session, l)
	if err != nil || parseErr {
		t.Fatalf("%v %v", err, parseErr)
	}
	if !slices.Equal(f.ids, []string{"acme.cashback"}) || f.conversation != "tab-c1" {
		t.Fatalf("ids: %v, chat %q", f.ids, f.conversation)
	}
	byKey := map[string]string{}
	for i, a := range start.Args {
		if a == "-c" && i+1 < len(start.Args) {
			k, v, _ := strings.Cut(start.Args[i+1], "=")
			byKey[k] = v
		}
	}
	// Every -c flag is valid TOML.
	for i, a := range start.Args {
		if a == "-c" {
			var out map[string]any
			if _, err := toml.Decode(start.Args[i+1], &out); err != nil {
				t.Errorf("%q: %v", start.Args[i+1], err)
			}
		}
	}
	got := byKey["hooks.PreToolUse"]
	guard, script, person := strings.Index(got, "app-guard"), strings.Index(got, "/data/codex-hook.sh"), strings.Index(got, "person-hook")
	if guard < 0 || guard > script || script > person {
		t.Fatalf("the guard must come first, then the runtime's hook, then the person's: %s", got)
	}
	if !strings.Contains(got, `timeout=15`) || !strings.Contains(got, "--agent codex") {
		t.Fatalf("guard entry: %s", got)
	}
	if strings.Contains(byKey["hooks.UserPromptSubmit"], "app-guard") {
		t.Fatal("the guard is a PreToolUse hook only")
	}
	for _, k := range []string{
		"mcp_servers.plugin_acme-cashback_bank.url", "mcp_servers.plugin_acme-cashback_bank.http_headers",
		"mcp_servers.plugin_acme-cashback_secret.env_http_headers", "mcp_servers.plugin_acme-cashback_local.command",
		"mcp_servers.plugin_acme-cashback_local.args", "mcp_servers.plugin_acme-cashback_local.env",
	} {
		if _, ok := byKey[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if byKey["mcp_servers.plugin_acme-cashback_secret.env_http_headers"] != `{"x-rt-token"="RT_PREVIEW_TOKEN"}` {
		t.Errorf("the token header reads the CLI's environment: %s", byKey["mcp_servers.plugin_acme-cashback_secret.env_http_headers"])
	}
	instr := byKey["developer_instructions"]
	if !strings.Contains(instr, "# Workspace") || !strings.Contains(instr, "SKILL.md") || strings.Index(instr, "Workspace") > strings.Index(instr, "Apps on") {
		t.Errorf("instructions: %s", instr)
	}
	want := `HARNESSLY_CLI_TOKEN_FILE='/d/app-launch/s1/token' RT_PREVIEW_TOKEN="$(cat '/d/app-launch/s1/token')" 'bwrap' '--dev-bind' '/' '/' --`
	if start.Prefix != want {
		t.Errorf("prefix: %s", start.Prefix)
	}
	if start.Args[0] != "resume" {
		t.Errorf("resume first: %q", start.Args[:2])
	}
	// A launch of the arguments alone would drop the wall: it is refused.
	if _, _, err := c.RuntimeArgs("ws", "", withApps(choice("gpt-5.4", ""), "acme.cashback")); !errors.Is(err, ErrNeedsStart) {
		t.Errorf("RuntimeArgs with a wall: %v", err)
	}
	// Without apps nothing of this is added.
	plain, _, err := c.RuntimeStart("", "", nil)
	if err != nil || plain.Prefix != "" || strings.Contains(strings.Join(plain.Args, " "), "app-guard") {
		t.Errorf("plain launch: %v %+v", err, plain)
	}
}

func TestAppsNeedALauncherAndAGuardHook(t *testing.T) {
	c := appsConfig(t, nil)
	c.Apps = nil
	if _, _, err := c.RuntimeStart("", "", withApps(choice("gpt-5.4", ""), "acme.a")); !errors.Is(err, agents.ErrNoApps) {
		t.Fatalf("no launcher: %v", err)
	}
	f := &fakeCodexApps{apps: agents.CodexApps{Guard: "g"}}
	c = appsConfig(t, f)
	c.Hooks = nil
	if _, _, err := c.RuntimeStart("", "", withApps(choice("gpt-5.4", ""), "acme.a")); err == nil {
		t.Fatal("apps with no hooks to hold their rules must not start")
	}
	c = appsConfig(t, f)
	if _, _, err := c.RuntimeStart("", "", withApps(choice("gpt-5.4", ""), "BAD ID")); err == nil {
		t.Fatal("app ids are re-checked")
	}
}

// The launcher line tells the launcher the apps, and the launcher script sends them and runs codex behind the prefix.
func TestLauncherLineCarriesApps(t *testing.T) {
	flags, err := LauncherFlags(withApps(choice("gpt-5.4", ""), "acme.a", "acme.b"))
	if err != nil || !slices.Equal(flags, []string{"--model", "'gpt-5.4'", "--apps", "'acme.a,acme.b'"}) {
		t.Fatalf("%q %v", flags, err)
	}
	l := withApps(choice("gpt-5.4", ""), "acme.a")
	l.Conversation = "tab-c1"
	if flags, err = LauncherFlags(l); err != nil || !slices.Equal(flags, []string{"--model", "'gpt-5.4'", "--apps", "'acme.a'", "--conversation", "'tab-c1'"}) {
		t.Fatalf("with the chat: %q %v", flags, err)
	}
	script := string(LauncherScript(LauncherScriptConfig{DataDirs: []string{".d"}, PortFile: "port", TokenFile: "cli-token", LaunchArgsPath: "/x", TokenHeaders: []string{"h"}, TokenEnv: "E"}))
	for _, want := range []string{"'--apps'", "'--conversation'", "data.prefix", `sh', ['-c', prefix + ' codex "$@"'`} {
		if !strings.Contains(script, want) {
			t.Errorf("launcher script lacks %s", want)
		}
	}
}

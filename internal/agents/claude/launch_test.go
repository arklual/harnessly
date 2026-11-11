package claude

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

const session = "11111111-2222-4333-8444-555555555555"

func testConfig(t *testing.T) LaunchConfig {
	return LaunchConfig{
		HooksPath:       "/tmp/hooks.json",
		MCPConfigPath:   "/tmp/preview-mcp.json",
		WorkspacePrompt: func(string) string { return "/tmp/prompt.md" },
		JoinDir:         filepath.Join(t.TempDir(), "launch-prompts"),
		EditorScript:    func() (string, error) { return "/data/bin/prompt-editor", nil },
		HumanEditorVar:  "HUMAN_EDITOR",
	}
}

func editorPrefix() string { return EditorEnv("HUMAN_EDITOR", "/data/bin/prompt-editor") }

func launch(model, effort string) *agents.Launch {
	l := &agents.Launch{}
	if model != "" {
		l.Model = &model
	}
	if effort != "" {
		l.Effort = &effort
	}
	return l
}

func words(t *testing.T, command string) []string {
	t.Helper()
	out, err := exec.Command("bash", "-c", `printf '%s\n' `+command).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// claude-managed-launcher: the configured absolute launcher for fresh and resumed sessions.
func TestManagedLauncher(t *testing.T) {
	c := testConfig(t)
	c.Launcher = "/opt/personal user's/claude"
	prefix := editorPrefix() + ` DISABLE_AUTOUPDATER=1 '/opt/personal user'\''s/claude'`
	if got, err := c.LaunchCommand("work", nil, nil); err != nil || got != prefix+" --settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json --append-system-prompt-file /tmp/prompt.md" {
		t.Fatalf("%q %v", got, err)
	}
	if got, _ := c.ResumeCommand(session, "", nil); got != prefix+" --resume "+session+" --settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json" {
		t.Fatalf("%q", got)
	}
	c.Launcher = ""
	if got, _ := c.LaunchCommand("", nil, nil); got != editorPrefix()+" DISABLE_AUTOUPDATER=1 claude --settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json" {
		t.Fatalf("plain claude: %q", got)
	}
	if got, _ := c.ResumeCommand(session, "", nil); got != editorPrefix()+" DISABLE_AUTOUPDATER=1 claude --resume "+session+" --settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json" {
		t.Fatalf("plain resume: %q", got)
	}
	c.Launcher = "relative-launcher"
	if _, err := c.ResumeCommand(session, "", nil); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatal(err)
	}
	c.Launcher = "/opt/launcher\nother-command"
	if _, err := c.LaunchCommand("", nil, nil); err == nil || !strings.Contains(err.Error(), "launcher") {
		t.Fatal(err)
	}
	if _, err := testConfig(t).ResumeCommand("not-a-uuid", "", nil); err == nil || err.Error() != "Invalid session ID format: not-a-uuid" {
		t.Fatal(err)
	}
}

// launch-options: quotes the model, checks both values again and keeps "Auto" flagless.
func TestLaunchFlags(t *testing.T) {
	for _, c := range []struct {
		l    *agents.Launch
		want []string
	}{
		{launch("opus", "high"), []string{"--model 'opus'", "--effort high"}},
		{launch("claude-opus-5-5[1m]", ""), []string{"--model 'claude-opus-5-5[1m]'"}},
		{launch("", ""), nil},
		{nil, nil},
		{launch("sonnet", "ultracode"), []string{"--model 'sonnet'", "--effort ultracode"}},
	} {
		if got, err := LaunchFlags(c.l); err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%v: %q %v", c.l, got, err)
		}
	}
	if _, err := LaunchFlags(launch("opus'; rm -rf ~; '", "")); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatal(err)
	}
	if _, err := LaunchFlags(launch("opus", "$(id)")); err == nil || !strings.Contains(err.Error(), "effort") {
		t.Fatal(err)
	}
}

// launch-options: Claude flags go before `--` and the prompt, for launch and resume, bare and managed.
func TestFlagsBeforePrompt(t *testing.T) {
	c := testConfig(t)
	editor := editorPrefix() + " "
	line, err := c.LaunchCommand("ws", launch("fable", "high"), nil)
	if err != nil {
		t.Fatal(err)
	}
	command := agents.WithInitialPrompt("claude", line, "fix it -- now '$HOME'", "")
	if command != editor+`DISABLE_AUTOUPDATER=1 claude --settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json --append-system-prompt-file /tmp/prompt.md --model 'fable' --effort high -- "$(printf %b 'fix it -- now '\''$HOME'\''')"` {
		t.Fatalf("%q", command)
	}
	if got := words(t, strings.TrimPrefix(strings.TrimPrefix(command, editor), "DISABLE_AUTOUPDATER=1 claude ")); !slices.Equal(got, []string{"--settings", "/tmp/hooks.json", "--mcp-config", "/tmp/preview-mcp.json", "--append-system-prompt-file", "/tmp/prompt.md", "--model", "fable", "--effort", "high", "--", "fix it -- now '$HOME'"}) {
		t.Fatalf("%q", got)
	}
	if got, _ := c.ResumeCommand(session, "ws", launch("fable", "high")); got != editor+"DISABLE_AUTOUPDATER=1 claude --resume "+session+" --settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json --append-system-prompt-file /tmp/prompt.md --model 'fable' --effort high" {
		t.Fatalf("%q", got)
	}
	c.Launcher = "/opt/profile/managed-claude"
	if got, _ := c.ResumeCommand(session, "ws", launch("sonnet", "")); got != editor+"DISABLE_AUTOUPDATER=1 '/opt/profile/managed-claude' --resume "+session+" --settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json --append-system-prompt-file /tmp/prompt.md --model 'sonnet'" {
		t.Fatalf("%q", got)
	}
	c.SkipPermissions = func() bool { return true }
	if got, _ := c.Flags("", nil, nil); got != "--settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json --dangerously-skip-permissions" {
		t.Fatalf("%q", got)
	}
	c.EditorScript = func() (string, error) { return "", errors.New("read-only") }
	if got, _ := c.LaunchCommand("", nil, nil); strings.Contains(got, "VISUAL") {
		t.Fatal("no editor when the script cannot be written")
	}
}

// launch-options: joins instruction files into one for bare claude and passes each to the managed launcher.
func TestPromptFiles(t *testing.T) {
	c := testConfig(t)
	c.WorkspacePrompt = nil
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b 'x'.md")
	_ = os.WriteFile(a, []byte("AAA\n"), 0o600)
	_ = os.WriteFile(b, []byte("BBB\n"), 0o600)
	bare, err := c.LaunchCommand("", nil, []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(bare, "--append-system-prompt-file"); n != 1 {
		t.Fatalf("%d flags", n)
	}
	joined := regexp.MustCompile(`--append-system-prompt-file '([^']+)'`).FindStringSubmatch(bare)
	if raw, _ := os.ReadFile(joined[1]); string(raw) != "AAA\n\nBBB\n" {
		t.Fatalf("%q", raw)
	}
	if st, _ := os.Stat(joined[1]); st.Mode().Perm() != 0o600 || filepath.Dir(joined[1]) != c.JoinDir || len(filepath.Base(joined[1])) != 19 {
		t.Fatalf("%v %s", st.Mode(), joined[1])
	}
	c.Launcher = "/opt/launcher"
	managed, _ := c.LaunchCommand("", nil, []string{a, b})
	rest := strings.TrimPrefix(managed, editorPrefix()+" DISABLE_AUTOUPDATER=1 '/opt/launcher' ")
	if got := words(t, rest); !slices.Equal(got, []string{"--settings", "/tmp/hooks.json", "--mcp-config", "/tmp/preview-mcp.json", "--append-system-prompt-file", a, "--append-system-prompt-file", b}) {
		t.Fatalf("%q", got)
	}
	c.WorkspacePrompt = func(id string) string { return "/ws/" + id + "/prompt.md" }
	got, _ := c.Flags("w1", nil, []string{a})
	if !strings.Contains(got, "--append-system-prompt-file '/ws/w1/prompt.md' --append-system-prompt-file '"+a+"'") {
		t.Fatalf("workspace prompt first: %q", got)
	}
}

// The workspace skills ride in a plugin folder loaded with --plugin-dir, on a fresh start and on a resume; no folder, no flag.
func TestPluginDirFlag(t *testing.T) {
	c := testConfig(t)
	base := "--settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json"
	if got, _ := c.Flags("", nil, nil); got != base {
		t.Fatalf("no plugin: %q", got)
	}
	dir := "/data/it's skills"
	c.PluginDir = func() string { return dir }
	want := base + ` --plugin-dir '/data/it'\''s skills'`
	if got, _ := c.Flags("", nil, nil); got != want {
		t.Fatalf("%q", got)
	}
	if got, _ := c.ResumeCommand(session, "", nil); !strings.Contains(got, " --resume "+session+" "+want) {
		t.Fatalf("resume: %q", got)
	}
	if got, _ := c.Flags("work", launch("opus", ""), nil); !strings.HasPrefix(got, want+" --append-system-prompt-file /tmp/prompt.md") || !strings.HasSuffix(got, "--model 'opus'") {
		t.Fatalf("order: %q", got)
	}
	// A plugin folder that is not there yet adds nothing.
	dir = ""
	if got, _ := c.Flags("", nil, nil); got != base {
		t.Fatalf("empty: %q", got)
	}
}

type fakeApps struct {
	ids       []string
	settings  string
	prompt    string
	wrap      string
	retoken   string
	retokened string
}

func (f *fakeApps) Claude(ids []string, settingsPath string) (agents.ClaudeApps, error) {
	return f.ClaudeFor("", ids, settingsPath)
}

func (f *fakeApps) ClaudeFor(conversation string, ids []string, settingsPath string) (agents.ClaudeApps, error) {
	f.ids, f.settings = ids, settingsPath
	out := agents.ClaudeApps{Settings: "/data/apps/launch/settings-1.json", PluginDirs: []string{"/data/apps/a", "/dev/b"},
		PromptFile: f.prompt, Wrap: f.wrap}
	if f.retoken != "" {
		out.Retoken = func(src string) (string, error) { f.retokened = src; return f.retoken, nil }
	}
	return out, nil
}

func (f *fakeApps) Codex(string, []string) (agents.CodexApps, error) { return agents.CodexApps{}, nil }

// The apps of a chat replace --settings with the copy that carries the guard, add their plugins and their instructions.
func TestAppsFlags(t *testing.T) {
	c := testConfig(t)
	c.WorkspacePrompt = nil
	l := launch("opus", "")
	l.Apps = []string{"acme.a", "acme.b"}
	if _, err := c.Flags("", l, nil); !errors.Is(err, agents.ErrNoApps) {
		t.Fatalf("no app launcher: %v", err)
	}
	f := &fakeApps{prompt: filepath.Join(t.TempDir(), "prompt-1.md")}
	_ = os.WriteFile(f.prompt, []byte("# Apps\n"), 0o600)
	c.Apps = f
	got, err := c.Flags("", l, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.ids, l.Apps) || f.settings != "/tmp/hooks.json" {
		t.Fatalf("asked %v over %q", f.ids, f.settings)
	}
	want := "--settings '/data/apps/launch/settings-1.json' --mcp-config /tmp/preview-mcp.json --plugin-dir '/data/apps/a' --plugin-dir '/dev/b' --append-system-prompt-file "
	if !strings.HasPrefix(got, want) || strings.Contains(got, "/tmp/hooks.json") {
		t.Fatalf("%q", got)
	}
	joined := regexp.MustCompile(`--append-system-prompt-file '([^']+)'`).FindStringSubmatch(got)
	if raw, _ := os.ReadFile(joined[1]); string(raw) != "# Apps\n" {
		t.Fatalf("the apps' instructions: %q", raw)
	}
	l.Apps = []string{"Not An Id"}
	if _, err := c.Flags("", l, nil); err == nil {
		t.Fatal("a bad app id launched")
	}
}

// The apps' wall goes between the environment and the CLI, in the new and the resumed launch alike.
func TestAppsWall(t *testing.T) {
	c := testConfig(t)
	c.WorkspacePrompt = nil
	c.Apps = &fakeApps{wrap: "'/usr/bin/bwrap' '--dev-bind' '/' '/' '--' '/rt' 'sandbox-exec' '--'"}
	l := launch("opus", "")
	l.Apps = []string{"acme.a"}
	line, err := c.LaunchCommand("", l, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, LaunchEnv+" '/usr/bin/bwrap' '--dev-bind' '/' '/' '--' '/rt' 'sandbox-exec' '--' "+Binary+" --settings ") {
		t.Fatalf("%q", line)
	}
	if line, err = c.ResumeCommand(id1, "", l); err != nil || !strings.Contains(line, "'sandbox-exec' '--' "+Binary+" --resume "+id1) {
		t.Fatalf("%q %v", line, err)
	}
	l.Apps = nil
	if line, _ = c.LaunchCommand("", l, nil); strings.Contains(line, "bwrap") {
		t.Fatalf("a chat without apps has no wall: %q", line)
	}
}

// The person's MCP servers come as one more --mcp-config at every launch and resume, with their tools allowed in a copy
// of the settings; without servers the line is as it was.
func TestExtraMCP(t *testing.T) {
	dir := t.TempDir()
	c := testConfig(t)
	c.WorkspacePrompt = nil
	c.HooksPath = filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(c.HooksPath, []byte(`{"permissions":{"allow":["mcp__rt"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, "mcp", "servers-1.json")
	servers := []string{}
	c.ExtraMCP = func() (string, []string) {
		if len(servers) == 0 {
			return "", nil
		}
		return extra, servers
	}
	got, err := c.Flags("", nil, nil)
	if err != nil || got != "--settings "+c.HooksPath+" --mcp-config /tmp/preview-mcp.json" {
		t.Fatalf("no servers: %q %v", got, err)
	}
	servers = []string{"mcp__mcp-linear"}
	got, err = c.Flags("", nil, nil)
	if err != nil || !strings.HasSuffix(got, " --mcp-config /tmp/preview-mcp.json --mcp-config '"+extra+"'") {
		t.Fatalf("with servers: %q %v", got, err)
	}
	settings := regexp.MustCompile(`--settings '([^']+)'`).FindStringSubmatch(got)
	if settings == nil || settings[1] == c.HooksPath {
		t.Fatalf("the settings are not the copy: %q", got)
	}
	raw, _ := os.ReadFile(settings[1])
	var s struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &s); err != nil || !slices.Equal(s.Permissions.Allow, []string{"mcp__rt", "mcp__mcp-linear"}) {
		t.Fatalf("allow: %s %v", raw, err)
	}
	if line, err := c.ResumeCommand(id1, "", nil); err != nil || !strings.Contains(line, "--mcp-config '"+extra+"'") {
		t.Fatalf("resume: %q %v", line, err)
	}
	// Under the apps the config goes in with the launch's token, as the copy Retoken gives.
	l := launch("opus", "")
	l.Apps = []string{"acme.a"}
	c.Apps = &fakeApps{retoken: "/data/apps/launch/xmcp-1.json"}
	got, err = c.Flags("", l, nil)
	if err != nil || !strings.Contains(got, "--mcp-config '/data/apps/launch/xmcp-1.json'") || strings.Contains(got, extra) {
		t.Fatalf("apps: %q %v", got, err)
	}
	if f := c.Apps.(*fakeApps); f.settings != settings[1] || f.retokened != extra {
		t.Fatalf("the apps extend %q, retoken %q", f.settings, f.retokened)
	}
}

// The hub's environment plugins ride after the skills plugin, one --plugin-dir each.
func TestEnvPluginDirsFlag(t *testing.T) {
	c := testConfig(t)
	c.PluginDir = func() string { return "/data/skills" }
	var dirs []string
	c.EnvPluginDirs = func() []string { return dirs }
	base := "--settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json --plugin-dir '/data/skills'"
	if got, _ := c.Flags("", nil, nil); got != base {
		t.Fatalf("no environment: %q", got)
	}
	dirs = []string{"/data/environment/claude-plugin", "/data/hub/environment/claude-plugin"}
	want := base + " --plugin-dir '/data/environment/claude-plugin' --plugin-dir '/data/hub/environment/claude-plugin'"
	if got, _ := c.Flags("", nil, nil); got != want {
		t.Fatalf("%q", got)
	}
}

// hostApps is an app launcher that gives a chat without apps the host's quiet app server.
type hostApps struct {
	fakeApps
	host string
}

func (h *hostApps) HostMCP() (string, error) { return h.host, nil }

// A chat that starts with no app on has the apps' server all the same, so an app turned on later reaches its running
// agent; a chat with apps has the launch's own config instead.
func TestHostMCPWithoutApps(t *testing.T) {
	c := testConfig(t)
	c.WorkspacePrompt = nil
	c.Apps = &hostApps{host: "/data/app-mcp.json"}
	got, err := c.Flags("", launch("opus", ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "--mcp-config /tmp/preview-mcp.json --mcp-config '/data/app-mcp.json'") {
		t.Fatalf("without apps: %q", got)
	}
	l := launch("opus", "")
	l.Apps = []string{"acme.a"}
	if got, err = c.Flags("", l, nil); err != nil || strings.Contains(got, "app-mcp.json") {
		t.Fatalf("with apps: %v %q", err, got)
	}
}

// failingApps readies no apps at all (the guard did not answer) and names one of them broken beforehand.
type failingApps struct {
	fakeApps
	broken  string
	fail    error
	dropped map[string]string
}

func (f *failingApps) ClaudeFor(conversation string, ids []string, settingsPath string) (agents.ClaudeApps, error) {
	if f.fail != nil {
		return agents.ClaudeApps{}, f.fail
	}
	return f.fakeApps.ClaudeFor(conversation, ids, settingsPath)
}

func (f *failingApps) Launchable(ids []string) ([]string, map[string]error) {
	var ok []string
	bad := map[string]error{}
	for _, id := range ids {
		if id == f.broken {
			bad[id] = errors.New("not installed")
		} else {
			ok = append(ok, id)
		}
	}
	return ok, bad
}

func (f *failingApps) AppsDropped(_ string, ids []string, why error) {
	for _, id := range ids {
		f.dropped[id] = why.Error()
	}
}

// A chat does not wait on its apps: a broken app is left out with its reason, and when readying the rest
// fails the chat starts plain, without the apps' settings, instead of not at all.
func TestAppsDoNotKeepTheChatFromStarting(t *testing.T) {
	c := testConfig(t)
	c.WorkspacePrompt = nil
	l := launch("opus", "")
	l.Apps = []string{"acme.a", "acme.gone"}
	f := &failingApps{broken: "acme.gone", dropped: map[string]string{}}
	c.Apps = f
	got, err := c.Flags("", l, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.ids, []string{"acme.a"}) || f.dropped["acme.gone"] != "not installed" {
		t.Fatalf("asked for %v, dropped %v", f.ids, f.dropped)
	}
	if !strings.Contains(got, "--plugin-dir '/data/apps/a'") {
		t.Fatalf("the app that works is not in: %q", got)
	}
	f.fail, f.dropped = errors.New("the guard does not answer"), map[string]string{}
	got, err = c.Flags("", l, nil)
	if err != nil {
		t.Fatalf("the chat did not start: %v", err)
	}
	if strings.Contains(got, "settings-1.json") || strings.Contains(got, "--plugin-dir '/data/apps/a'") || !strings.Contains(got, "/tmp/hooks.json") {
		t.Fatalf("the plain chat still has apps: %q", got)
	}
	if f.dropped["acme.a"] != "the guard does not answer" || f.dropped["acme.gone"] != "not installed" {
		t.Fatalf("dropped %v", f.dropped)
	}
}

package claude

import (
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
	if command != editor+`DISABLE_AUTOUPDATER=1 claude --settings /tmp/hooks.json --mcp-config /tmp/preview-mcp.json --append-system-prompt-file /tmp/prompt.md --model 'fable' --effort high -- $'fix it -- now \'$HOME\''` {
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

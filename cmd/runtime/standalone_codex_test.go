package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// bareEnv is a deployment that names nothing of the product: what a Docker, compose, Helm or systemd install has.
func bareEnv(t *testing.T, home string) {
	t.Helper()
	for _, k := range []string{"RUNTIME_SCRIPT_DATA_DIRS", "RUNTIME_MCP_TOKEN_ENV", "RUNTIME_MCP_SERVER", "RUNTIME_MCP_PATH", "RUNTIME_MCP_ALLOW",
		"RUNTIME_CLI_TOKEN_HEADERS", "RUNTIME_CODEX_LAUNCHER_FILE", "RUNTIME_PROMPT_EDITOR", "RUNTIME_PROMPT_EDITOR_NAME", "RUNTIME_LOCK_FILE",
		"RUNTIME_RELEASE_FILE", "RUNTIME_WORKSPACE_PROMPTS_FILE", "AGENT_SECRET_FILE", "AGENT_CONFIG"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
}

func TestScriptDataDirsDefaultToTheDataFolder(t *testing.T) {
	t.Setenv("RUNTIME_SCRIPT_DATA_DIRS", "")
	for _, c := range []struct {
		data, home string
		want       []string
	}{
		{"/home/agent/.harnessly", "/home/agent", []string{".harnessly"}},
		{"/home/agent/state/harnessly", "/home/agent", []string{"state/harnessly"}},
		{"/var/lib/harnessly", "/home/agent", []string{"/var/lib/harnessly"}},
		{"/home/agent", "/home/agent", []string{"/home/agent"}},
		{"/home/agent/.harnessly", "", []string{"/home/agent/.harnessly"}},
	} {
		if got := scriptDataDirs(c.data, c.home); strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s under %q: %v, want %v", c.data, c.home, got, c.want)
		}
	}
	t.Setenv("RUNTIME_SCRIPT_DATA_DIRS", ".acme-ai, .oldmux")
	if got := scriptDataDirs("/home/agent/.harnessly", "/home/agent"); strings.Join(got, "|") != ".acme-ai|.oldmux" {
		t.Errorf("the environment wins: %v", got)
	}
}

// Without RUNTIME_SCRIPT_DATA_DIRS and RUNTIME_MCP_TOKEN_ENV a standalone runtime still writes the Codex launcher at
// its start, and Codex conversations launch.
func TestStandaloneWritesTheCodexLauncherWithoutExtraEnv(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	data := filepath.Join(home, ".harnessly")
	_ = os.MkdirAll(home, 0o700)
	bareEnv(t, home)
	cfg := gate.Config{Listen: "127.0.0.1:8022", DataDir: data}
	s, err := standaloneStart(cfg, configFileOf(cfg), 8022)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	s.files(8022, nil)
	script, err := os.ReadFile(filepath.Join(data, "codex-launcher.js"))
	if err != nil {
		t.Fatalf("the launcher was not written at start: %v", err)
	}
	for _, want := range []string{`[".harnessly"]`, `"` + defaultMCPTokenEnv + `"`, `"x-cli-token"`, "/api/codex/launch-args"} {
		if !strings.Contains(string(script), want) {
			t.Errorf("the launcher lacks %s:\n%s", want, script)
		}
	}
	hook, err := os.ReadFile(filepath.Join(data, "codex-hook.sh"))
	if err != nil || !strings.Contains(string(hook), `$HOME/.harnessly/port`) {
		t.Errorf("the Codex hook script does not find the port file: %v\n%s", err, hook)
	}

	// The lines the starter types: the launcher is there, and also written by the line when it is gone.
	a := newCoreAgents(coreAgentsConfig{DataDir: data, Home: home, ConfigFile: configFileOf(cfg), Port: 8022,
		MCP:            codex.MCP{Server: "harnessly", TokenEnv: mcpTokenEnv()},
		MCPPath:        "/api/cli/preview/mcp",
		ScriptDataDirs: scriptDataDirs(data, home), TokenHeaders: []string{"x-cli-token"}})
	line, err := a.LaunchLine(agents.Codex, "ws-1", nil)
	if err != nil || !strings.HasPrefix(line, "node '"+filepath.Join(data, "codex-launcher.js")+"'") {
		t.Fatalf("codex launch line: %q, %v", line, err)
	}
	if err := os.Remove(filepath.Join(data, "codex-launcher.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResumeLine(agents.Codex, "6f1d2c4e-0a53-4a9e-8f2e-3c1f9a7b5d10", "ws-1", nil); err != nil {
		t.Fatalf("resume line: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "codex-launcher.js")); err != nil {
		t.Fatalf("the line did not write the launcher: %v", err)
	}
}

// A Codex line that cannot be made is an error with its text, not a panic.
func TestCodexLineWithoutALauncherSaysWhy(t *testing.T) {
	root := t.TempDir()
	bareEnv(t, root)
	a := newCoreAgents(coreAgentsConfig{DataDir: filepath.Join(root, "nowhere"), Home: root, ConfigFile: filepath.Join(root, "config.json"), Port: 8022})
	_, err := a.LaunchLine(agents.Codex, "ws-1", nil)
	if err == nil || !strings.Contains(err.Error(), "RUNTIME_SCRIPT_DATA_DIRS") {
		t.Fatalf("%v", err)
	}
}

// RUNTIME_CODEX_SANDBOX=container (the Docker image's default) starts Codex without its own sandbox.
func TestCodexArgsFollowTheSandboxSetting(t *testing.T) {
	root := t.TempDir()
	bareEnv(t, root)
	for _, container := range []bool{false, true} {
		a := newCoreAgents(coreAgentsConfig{DataDir: filepath.Join(root, "data"), Home: root, ConfigFile: filepath.Join(root, "config.json"), Port: 8022,
			CodexContainerSandbox: container})
		args, err := a.CodexArgs("", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if has := strings.Contains(strings.Join(args, " "), `sandbox_mode="danger-full-access" -c approval_policy="never"`); has != container {
			t.Errorf("container=%v: %q", container, args)
		}
	}
}

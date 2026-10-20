package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agentskills"
	"github.com/ly-solutions/harnessly/internal/hooksetup"
)

func clearServices(t *testing.T) {
	t.Helper()
	for _, k := range []string{"RUNTIME_BOARDS_URL", "RUNTIME_ARTIFACTS_URL", "RUNTIME_WORKSPACE_PROMPTS_FILE", "RUNTIME_RELEASE_FILE",
		"RUNTIME_AGENT_SKILLS", "RUNTIME_AGENT_BIN_DIR", "RUNTIME_PANE_BIN_DIR", "CODEX_HOME"} {
		t.Setenv(k, "")
	}
}

// Without a prompts file the agents still learn where they are and what they have: the open install ships no file.
func TestBuiltInWorkspacePrompts(t *testing.T) {
	clearServices(t)
	t.Setenv("RUNTIME_BOARDS_URL", "https://boards.example.com")
	t.Setenv("RUNTIME_ARTIFACTS_URL", "https://docs.example.com")
	p := workspacePrompts()
	for _, provider := range []string{"claude", "codex"} {
		text, ok := p.Text(provider, "ws-1")
		if !ok {
			t.Fatalf("%s has no prompt", provider)
		}
		for _, want := range []string{"`ws-1`", "harnessly open", "harnessly tab create", "`artifacts`", "https://docs.example.com", "`boards`", "https://boards.example.com", "`harnessly`, `boards`, `artifacts`"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s prompt lacks %q:\n%s", provider, want, text)
			}
		}
		if strings.Contains(text, "TOKEN=") || strings.Contains(text, "{{") {
			t.Errorf("%s prompt has a placeholder or a token:\n%s", provider, text)
		}
	}
	if text, _ := p.Text("codex", "ws-1"); !strings.Contains(text, "You are Codex") {
		t.Error("the Codex prompt names Codex")
	}
	if text, _ := p.Text("claude", "ws-1"); !strings.Contains(text, "You are Claude Code") {
		t.Error("the Claude prompt names Claude Code")
	}

	// A service that is not configured is not promised.
	t.Setenv("RUNTIME_BOARDS_URL", "")
	t.Setenv("RUNTIME_ARTIFACTS_URL", "")
	text, _ := workspacePrompts().Text("claude", "ws-1")
	if strings.Contains(text, "`artifacts`") || strings.Contains(text, "`boards`") || strings.Contains(text, "Documents and boards") || !strings.Contains(text, "harnessly open") {
		t.Errorf("no services:\n%s", text)
	}
}

func TestWorkspacePromptsFileWins(t *testing.T) {
	clearServices(t)
	file := filepath.Join(t.TempDir(), "prompts.json")
	_ = os.WriteFile(file, []byte(`{"claude":"mine {{workspaceId}}"}`), 0o600)
	t.Setenv("RUNTIME_WORKSPACE_PROMPTS_FILE", file)
	if text, _ := workspacePrompts().Text("claude", "w"); text != "mine w" {
		t.Errorf("got %q", text)
	}
	// A file that cannot be read falls back to the built-in prompts.
	_ = os.WriteFile(file, []byte(`not json`), 0o600)
	if _, ok := workspacePrompts().Text("codex", "w"); !ok {
		t.Error("no fallback")
	}
}

func TestPromptsAreWrittenForAWorkspace(t *testing.T) {
	clearServices(t)
	data := t.TempDir()
	if err := workspacePrompts().Write(data, "ws-abc"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"claude-prompt.md", "codex-prompt.md"} {
		raw, err := os.ReadFile(filepath.Join(data, "workspaces", "ws-abc", f))
		if err != nil || !strings.Contains(string(raw), "`ws-abc`") {
			t.Errorf("%s: %v", f, err)
		}
	}
	var _ hooksetup.Prompts = workspacePrompts()
}

func TestProvisionFollowsTheConfiguredServices(t *testing.T) {
	clearServices(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	data := filepath.Join(home, ".harnessly")
	t.Setenv("RUNTIME_BOARDS_URL", "https://boards.example.com")
	provisionAgentSkills(data)
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if !exists(filepath.Join(home, ".codex", "skills", "harnessly", "boards", "SKILL.md")) ||
		!exists(filepath.Join(home, ".codex", "skills", "harnessly", "harnessly", "SKILL.md")) ||
		!exists(filepath.Join(home, ".local", "bin", "boards")) {
		t.Fatal("boards and the workspace skill are installed")
	}
	if exists(filepath.Join(home, ".codex", "skills", "harnessly", "artifacts")) || exists(filepath.Join(home, ".local", "bin", "artifacts")) {
		t.Error("artifacts is installed without a catalog")
	}
	if got := skillsPluginDir(data); got != agentskills.ClaudePluginDir(data) {
		t.Errorf("plugin dir %q", got)
	}
	if got := paneBinDir(home); got != filepath.Join(home, ".local", "bin") {
		t.Errorf("pane bin dir %q", got)
	}
}

func TestSkillsOffInstallsNothing(t *testing.T) {
	clearServices(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RUNTIME_AGENT_SKILLS", "off")
	t.Setenv("RUNTIME_BOARDS_URL", "https://boards.example.com")
	t.Setenv("RUNTIME_ARTIFACTS_URL", "https://docs.example.com")
	provisionAgentSkills(filepath.Join(home, ".harnessly"))
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Errorf("wrote into the home: %v", entries)
	}
	if skillsPluginDir(home) != "" || paneBinDir(home) != "" {
		t.Error("off leaves no plugin folder and no pane bin dir")
	}
	// An explicit pane bin dir is the deployment's.
	t.Setenv("RUNTIME_PANE_BIN_DIR", "/opt/bin")
	if paneBinDir(home) != "/opt/bin" {
		t.Error("RUNTIME_PANE_BIN_DIR")
	}
}

func TestCodexHomeAndBinDirOverrides(t *testing.T) {
	clearServices(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "cx"))
	t.Setenv("RUNTIME_AGENT_BIN_DIR", filepath.Join(home, "tools"))
	t.Setenv("RUNTIME_ARTIFACTS_URL", "https://docs.example.com")
	provisionAgentSkills(filepath.Join(home, "data"))
	for _, f := range []string{filepath.Join(home, "cx", "skills", "harnessly", "artifacts", "SKILL.md"), filepath.Join(home, "tools", "artifacts")} {
		if _, err := os.Stat(f); err != nil {
			t.Error(err)
		}
	}
}

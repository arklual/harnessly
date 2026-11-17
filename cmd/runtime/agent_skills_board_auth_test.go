package main

import (
	"os"
	"strings"
	"testing"
)

func TestBoardSkillExplainsMissingCredentials(t *testing.T) {
	raw, err := os.ReadFile("../../skills/upstream/agent-skills/plugins/boards/skills/boards/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"harnessly secret list", "harnessly secret ask", "harnessly secret run", "403", "MCP", "--wait 0", "Do not ask for a link key"} {
		if !strings.Contains(text, want) {
			t.Errorf("board skill lacks %q", want)
		}
	}
	if strings.Contains(text, "link alone is enough to read it") {
		t.Error("a link does not grant access to a private board")
	}
}

// A server URL does not imply credentials: FromEnv/AgentEnv deliberately accept
// URL-only installations. The prompt must teach recovery, not invent a token.
func TestBoardPromptExplainsMissingCredentials(t *testing.T) {
	clearServices(t)
	for _, k := range []string{"RUNTIME_BOARDS_TOKEN", "RUNTIME_BOARDS_AGENT_TOKEN", "RUNTIME_ARTIFACTS_TOKEN"} {
		t.Setenv(k, "")
	}
	t.Setenv("RUNTIME_BOARDS_URL", "https://boards.example.com")
	for _, provider := range []string{"claude", "codex"} {
		text, _ := workspacePrompts().Text(provider, "ws-auth")
		if strings.Contains(text, "tokens are already in your environment") {
			t.Errorf("%s promises missing credentials", provider)
		}
		for _, want := range []string{"harnessly secret list", "harnessly secret ask", "harnessly secret run", "401", "403", "MCP", "Never ask for a token in the chat"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks auth recovery %q", provider, want)
			}
		}
	}
}

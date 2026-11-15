package main

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/integrations"
)

// Agents hand a Sinopia Design page over with `harnessly design export`, not by rewriting its HTML.
func TestWorkspacePromptSaysHowToExportDesign(t *testing.T) {
	for _, agent := range []string{"Claude Code", "Codex"} {
		p := workspacePromptText(agent, integrations.Config{})
		for _, want := range []string{"## Sinopia Design", "harnessly design export <project> \"<page>\"", "--zip", "by hand"} {
			if !strings.Contains(p, want) {
				t.Errorf("%s prompt lacks %q", agent, want)
			}
		}
		if strings.Contains(p, "Claude Design") {
			t.Errorf("%s prompt names the editor wrongly", agent)
		}
	}
}

// With a tracker, a task key opens in the pane by `harnessly open <key>`, with the deployment's own example key.
func TestWorkspacePromptOpensTaskByKey(t *testing.T) {
	t.Setenv("RUNTIME_TRACKER_HOSTS", "")
	t.Setenv("RUNTIME_TRACKER_URL", "")
	if p := workspacePromptText("Codex", integrations.Config{}); strings.Contains(p, "harnessly open PRJ") {
		t.Error("no tracker, yet the prompt tells to open a task")
	}
	t.Setenv("RUNTIME_TRACKER_HOSTS", "tracker.example.test")
	t.Setenv("RUNTIME_TASK_EXAMPLES", "ABC-7, ABC-8")
	p := workspacePromptText("Claude Code", integrations.Config{})
	for _, want := range []string{"`harnessly open ABC-7`", "no tracker connection"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}

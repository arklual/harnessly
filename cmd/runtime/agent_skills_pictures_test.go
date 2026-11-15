package main

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/integrations"
)

// every agent is told it draws with generate_images, in the same turn, and not with code; and its full name:
// Haiku called the bare `generate_images` and got «No such tool available».
func TestWorkspacePromptSaysHowToDraw(t *testing.T) {
	for _, agent := range []string{"Claude Code", "Codex"} {
		p := workspacePromptText(agent, integrations.Config{})
		for _, want := range []string{"## Pictures", "`generate_images` in that same turn", "Never draw with code", "deliver_file", "`mcp__harnessly__generate_images`"} {
			if !strings.Contains(p, want) {
				t.Errorf("%s prompt lacks %q", agent, want)
			}
		}
	}
}

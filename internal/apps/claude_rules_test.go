package apps

import (
	"slices"
	"testing"
)

func TestClaudeRules(t *testing.T) {
	deny, ask := ClaudeRules([]SnapshotApp{{ID: "acme.review", Tools: &ToolPolicy{
		Deny:    []string{"Bash(git push*)", "Bash(rm -rf /)", "Edit(/etc/*)", "Edit(*/docs/*)", "acme.drop", "app.*", "app.data.*", "*", "WebFetch", "mcp__x__y"},
		Confirm: []string{"Bash(git*)", "Bash(npm publish*)", "other.*"},
		Allow:   []string{"Bash(git status*)"},
	}}})
	for _, want := range []string{"Bash(git push:*)", "Bash(rm -rf /)", "Edit(//etc/*)", "mcp__plugin_acme-review_acme__drop", "mcp__acme__drop", "mcp__harnessly-app", "WebFetch"} {
		if !slices.Contains(deny, want) {
			t.Errorf("deny lacks %s: %v", want, deny)
		}
	}
	if len(deny) != 7 {
		t.Errorf("deny has what Claude cannot say: %v", deny)
	}
	// Bash(git*) is overridden by the narrower allow in the hook, so Claude must not ask about it.
	if !slices.Equal(ask, []string{"Bash(npm publish:*)", "mcp__plugin_acme-review_other", "mcp__other"}) {
		t.Errorf("ask = %v", ask)
	}
}

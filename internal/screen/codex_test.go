package screen

import (
	"reflect"
	"strings"
	"testing"
)

func TestIsCodexComposerReady(t *testing.T) {
	cases := []struct {
		name string
		pane string
		want bool
	}{
		{"default composer", `
╭────────────────────────╮
│ >_ OpenAI Codex        │
╰────────────────────────╯

› Ask Codex to do anything

  gpt-5.5 high · ~/repo
`, true},
		{"composer with text", `
╭────────────────────────────────────────────────────────╮
│ >_ OpenAI Codex (v0.128.0)                             │
╰────────────────────────────────────────────────────────╯

› Implement {feature}

  gpt-5.5 high · ~/Workspace/github.com/acme/app
`, true},
		{"any placeholder text", `
╭────────────────────────╮
│ >_ OpenAI Codex        │
╰────────────────────────╯

› Explain this codebase

  100% context left
`, true},
		{"disabled input", `
╭────────────────────────╮
│ >_ OpenAI Codex        │
╰────────────────────────╯

› Input disabled.

  100% context left
`, false},
		{"update prompt menu", "\n  ✨ Update available! 0.125.0 -> 0.128.0\n\n› 1. Update now (runs `npm install -g @openai/codex`)\n  2. Skip\n  3. Skip until next version\n", false},
		{"numbered popup", `
╭────────────────────────╮
│ >_ OpenAI Codex        │
╰────────────────────────╯

› 1. Yes, proceed (y)
  2. No
`, false},
		{"resumed session, banner scrolled off", `
› Implement the new feature

  Sure, I'll start by reviewing the existing code.

› Run the tests

  All tests pass. Here is the summary of changes:
  - Added validation for input
  - Updated tests

›

  gpt-5.5 high · ~/Workspace/github.com/acme/app
`, true},
		{"launch screen with pane padding", `
╭───────────────────────────────────────────────╮
│ >_ OpenAI Codex (v0.128.0)                    │
│                                               │
│ model:       gpt-5.5 high   /model to change  │
│ directory:   ~/Workspace/…/acme/app           │
│ permissions: YOLO mode                        │
╰───────────────────────────────────────────────╯

  Tip: NEW: Prevent sleep while running is now available
  in /experimental.


› Explain this codebase

  gpt-5.5 high · ~/Workspace/github.com/acme/ap…
` + strings.Repeat("\n", 20), true},
		{"stale shell content", `
$ codex resume 12345678-aaaa-bbbb-cccc-1234567890ab
Error: session not found
$
`, false},
		{"checklist popup", "› [x] Run tests\n  [ ] Lint\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := IsCodexComposerReady(c.pane); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func str(s string) *string { return &s }

func TestMatchCodexUpdatePrompt(t *testing.T) {
	prompt := &CodexUpdate{Status: CodexUpdatePrompt, CurrentVersion: str("0.125.0"), LatestVersion: str("0.128.0"), UpdateCommand: str("npm install -g @openai/codex")}
	cases := []struct {
		name string
		text string
		want *CodexUpdate
	}{
		{"prompt", lines(
			"  ✨ Update available! 0.125.0 -> 0.128.0", "",
			"  Release notes: https://github.com/openai/codex/releases/latest", "",
			"› 1. Update now (runs `npm install -g @openai/codex`)", "  2. Skip", "  3. Skip until next version",
		), prompt},
		{"updating", "Updating Codex via `npm install -g @openai/codex`...", &CodexUpdate{Status: CodexUpdateUpdating}},
		{"success", "🎉 Update ran successfully! Please restart Codex.", &CodexUpdate{Status: CodexUpdateSuccess}},
		{"URL cut by a column wrap", lines(
			"  ✨ Update available! 0.125.0 -> 0.128.0", "",
			"  Release notes: https://github.com/openai/codex/re", "",
			"› 1. Update now (runs `npm install -g", "     @openai/codex`)", "  2. Skip", "  3. Skip until next version",
		), prompt},
		{"URL wrapped on a narrow screen", lines(
			"  ✨ Update available! 0.125.0 -> 0.128.0", "",
			"  Release notes:", "  https://github.com/openai/codex/releases/latest", "",
			"› 1. Update now (runs `npm install -g @openai/codex`)", "  2. Skip", "  3. Skip until next version",
		), prompt},
		{"no Skip until next version", lines(
			"  ✨ Update available! 0.125.0 -> 0.128.0",
			"  Release notes: https://github.com/openai/codex/releases/latest",
		), nil},
		{"no Release notes", lines(
			"  ✨ Update available! 0.125.0 -> 0.128.0",
			"› 1. Update now (runs `npm install -g @openai/codex`)",
			"  3. Skip until next version",
		), nil},
		{"empty", "", nil},
		{"unrelated", "codex shell prompt> ", nil},
	}
	for _, c := range cases {
		if got := MatchCodexUpdatePrompt(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}

func TestCodexStillInBox(t *testing.T) {
	box := func(input string) string {
		return lines("• Working (8s • esc to interrupt)", "", "› "+input, "  tab to queue message")
	}
	placeholder := lines("• Working (9s • esc to interrupt)", "", "› Find and fix a bug in @filename", "  gpt-5.6-sol low")
	if !CodexStillInBox(box("Also add the word DELTA."), "Also add the word DELTA.") {
		t.Error("the waiting message is not seen")
	}
	if CodexStillInBox(placeholder, "Also add the word DELTA.") {
		t.Error("the placeholder reads as the message")
	}
	if !CodexStillInBox(box("[Pasted Content 1204 chars]"), "long text") {
		t.Error("a pasted token is not the message")
	}
	if CodexStillInBox("no box here", "x") || CodexStillInBox(box("x"), "   ") {
		t.Error("no box, or an empty message, reads as waiting")
	}
}

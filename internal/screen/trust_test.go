package screen

import (
	"reflect"
	"strings"
	"testing"
)

func TestMatchTrustPrompt(t *testing.T) {
	cases := []struct {
		name string
		text string
		want *TrustPrompt
	}{
		{"claude, folder above the safety paragraph", lines(
			"╭───────────────────────────────────────────────────╮",
			"│ Do you trust the files in this folder?            │",
			"│                                                   │",
			"│ /Users/dev/Workspace/github.com/acme/foo          │",
			"│                                                   │",
			"│ Quick safety check: Claude Code may read files    │",
			"│ in this folder. Reading untrusted files may lead  │",
			"│ Claude Code to behave in unexpected ways.         │",
			"│                                                   │",
			"│ ❯ 1. Yes, I trust this folder                     │",
			"│   2. No, exit                                     │",
			"╰───────────────────────────────────────────────────╯",
		), &TrustPrompt{Agent: "claude", FolderPath: "/Users/dev/Workspace/github.com/acme/foo"}},
		{"codex, the folder it is in", lines(
			"> You are in /Users/dev/Workspace/github.com/acme/nextjs-template",
			"",
			"  Do you trust the contents of this directory? Working with untrusted contents comes with higher risk of prompt injection. Trusting the directory allows project-local config, hooks, and exec policies to load.",
			"",
			"› 1. Yes, continue",
			"  2. No, quit",
			"",
			"  Press enter to continue",
		), &TrustPrompt{Agent: "codex", FolderPath: "/Users/dev/Workspace/github.com/acme/nextjs-template"}},
		{"codex, the repository root first", lines(
			"> You are in /Users/dev/Workspace/github.com/acme/foo/packages/web",
			"",
			"  Note: You’re in a subdirectory of a Git project. Trusting will apply to the repository root: /Users/dev/Workspace/github.com/acme/foo",
			"",
			"  Do you trust the contents of this directory? Working with untrusted contents comes with higher risk of prompt injection.",
			"",
			"› 1. Yes, continue",
			"  2. No, quit",
			"",
			"  Press enter to continue",
		), &TrustPrompt{Agent: "codex", FolderPath: "/Users/dev/Workspace/github.com/acme/foo"}},
		{"shell prompt", "just a regular shell prompt", nil},
		{"anchor without options", "Quick safety check\n(no options shown)", nil},
		{"question without options", "Do you trust the contents of this directory?", nil},
		{"claude without a path", lines("Quick safety check", "1. Yes, I trust this folder", "2. No, exit"), nil},
		{"codex without a path", lines("Do you trust the contents of this directory?", "1. Yes, continue", "2. No, quit"), nil},
	}
	for _, c := range cases {
		if got := MatchTrustPrompt(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}

func TestMatchTrustPromptCaptures(t *testing.T) {
	// Claude Code 2.1.282 and Codex 0.152 first starts in a new folder.
	for name, want := range map[string]struct{ agent, suffix string }{
		"claude-trust-folder.txt":   {"claude", "/scratchpad/pre/work"},
		"codex-trust-directory.txt": {"codex", "/scratchpad/pre/cwork"},
	} {
		got := MatchTrustPrompt(fixture(t, name))
		if got == nil || got.Agent != want.agent || !strings.HasPrefix(got.FolderPath, "/tmp/") || !strings.HasSuffix(got.FolderPath, want.suffix) {
			t.Errorf("%s: got %+v", name, got)
		}
	}
}

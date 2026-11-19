package typist

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Codex marks a menu's focused option with the › its input line starts with: a dialog in a turn is a menu with no
// input line, or the chat takes it for the agent at its prompt and asks the person nothing.
func TestCodexApprovalIsNoInputLine(t *testing.T) {
	for _, name := range []string{"codex-exec-approval.ansi.txt", "codex-patch-approval.ansi.txt"} {
		s := Read(tui(t, name), wire.ProviderCodex)
		if s.Menu != MenuPermission || len(s.Options) != 3 {
			t.Fatalf("%s: menu %v %q", name, s.Menu, s.Options)
		}
		if s.InputLine || s.Input != "" || s.PromptReady() {
			t.Fatalf("%s: the focused option read as the input line (%q)", name, s.Input)
		}
	}
	idle := Read(tui(t, "codex-idle-placeholder.ansi.txt"), wire.ProviderCodex)
	if !idle.InputLine || idle.Menu != NoMenu {
		t.Fatalf("idle Codex: input line %v, menu %v", idle.InputLine, idle.Menu)
	}
	draft := Read(tui(t, "codex-input-draft.ansi.txt"), wire.ProviderCodex)
	if !draft.InputLine || draft.Input == "" {
		t.Fatalf("a draft: %+v", draft.Input)
	}
}

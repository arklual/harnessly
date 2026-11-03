package typist

import (
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// growingPane is a pane whose window grows: the next capture shows the screen the dialog is redrawn on.
type growingPane struct {
	*fakePane
	after string
	grows int
	grow  bool
}

func (p *growingPane) GrowForDialog() bool {
	p.grows++
	if p.grow {
		p.set(p.after)
	}
	return p.grow
}

func longCommand(t *testing.T) PickWant {
	t.Helper()
	raw, err := os.ReadFile("testdata/tui/claude-2.1.316-bash-long.command.txt")
	if err != nil {
		t.Fatal(err)
	}
	return PickWant{N: 1, Label: "Yes", Command: "$ " + strings.TrimSpace(string(raw))}
}

// a chat's pane runs at 80×24 with no terminal attached. Claude 2.1.316 draws a 1.3 KB command past the top of
// that window, and the alternate screen keeps nothing in the scrollback. The window grows and the dialog is whole.
func TestLookGrowsAWindowTheDialogDoesNotFit(t *testing.T) {
	want := longCommand(t)
	cut, whole := tui(t, "claude-2.1.316-bash-long-cut-80x24.ansi.txt"), tui(t, "claude-2.1.316-bash-long-grown-200x60.ansi.txt")

	if Shows(Read(cut, wire.ProviderClaude), want) == nil {
		t.Fatal("the cut screen must not match the command")
	}
	p := &growingPane{fakePane: newPane(cut), after: whole, grow: true}
	s := Look(p, wire.ProviderClaude)
	if p.grows != 1 {
		t.Fatalf("grows: %d", p.grows)
	}
	if err := Shows(s, want); err != nil {
		t.Fatalf("the dialog as redrawn: %v\n%s", err, s.Plain)
	}

	// The whole dialog asks nothing more of the window.
	if Look(p, wire.ProviderClaude); p.grows != 1 {
		t.Fatalf("a whole dialog grew the window: %d", p.grows)
	}

	// A grown window does not loosen the check: another call with the same tail still gets no key.
	other := want
	other.Command = "$ rm -rf /" + strings.TrimPrefix(want.Command, "$ false")
	if Shows(s, other) == nil {
		t.Fatal("another command matched")
	}
}

// An attached terminal keeps its size: the look stays what the screen shows, and the card offers the terminal.
func TestLookWithAWindowThatCannotGrow(t *testing.T) {
	p := &growingPane{fakePane: newPane(tui(t, "claude-2.1.316-bash-long-cut-80x24.ansi.txt"))}
	s := Look(p, wire.ProviderClaude)
	if p.grows != 1 || s.Menu != MenuPermission || Shows(s, longCommand(t)) == nil {
		t.Fatalf("grows %d, menu %q", p.grows, s.Menu)
	}
}

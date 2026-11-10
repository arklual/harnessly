package typist

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func tuiFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tui", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// tallPane: a terminal is attached, the window grows only for a moment.
type tallPane struct {
	*fakePane
	tall  string
	calls int
}

func (p *tallPane) CaptureTall(string) (string, bool) { p.calls++; return p.tall, p.tall != "" }

// HRN-647: Claude 2.1.316's plan approval as it really is, whole and at six lines.
func TestPlanDialogOfClaude2316(t *testing.T) {
	whole := Read(tuiFile(t, "claude-2.1.316-plan.ansi.txt"), wire.ProviderClaude)
	if whole.Menu != MenuPlan || len(whole.Options) != 3 {
		t.Fatalf("whole: %s %q", whole.Menu, whole.Options)
	}
	if got := DrawnPlanFile(whole.Plain); got != "plan-foo-bar.md" {
		t.Fatalf("file: %q", got)
	}
	cut := Read(tuiFile(t, "claude-2.1.316-plan-cut.ansi.txt"), wire.ProviderClaude)
	if cut.Menu != MenuPlan || !cut.cut || cut.InputLine {
		t.Fatalf("cut: %+v", cut)
	}
	if Deliverable(cut) != DecideBlockedMenu {
		t.Fatalf("a message waits forever on a menu nobody sees: %s", Deliverable(cut))
	}
}

func TestLookReadsACutDialogAtItsTallSize(t *testing.T) {
	p := &tallPane{fakePane: newPane(tuiFile(t, "claude-2.1.316-plan-cut.ansi.txt")), tall: tuiFile(t, "claude-2.1.316-plan.ansi.txt")}
	s := Look(p, wire.ProviderClaude)
	if p.calls != 1 || s.Menu != MenuPlan || len(s.Options) != 3 {
		t.Fatalf("calls %d: %s %q", p.calls, s.Menu, s.Options)
	}
	p = &tallPane{fakePane: newPane(tuiFile(t, "claude-2.1.316-plan-cut.ansi.txt"))}
	if s := Look(p, wire.ProviderClaude); s.Menu != MenuPlan || len(s.Options) != 1 {
		t.Fatalf("no tall look: %s %q", s.Menu, s.Options)
	}
}

func TestDrawnPlan(t *testing.T) {
	const rule = "╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌"
	if got := DrawnPlan(" Ready\n " + rule + "\n Plan: a\n\n 1. b\n " + rule + "\n Proceed?\n"); got != "Plan: a\n\n1. b" {
		t.Fatalf("%q", got)
	}
	if got := DrawnPlan(" Proceed?\n"); got != "" {
		t.Fatalf("%q", got)
	}
}

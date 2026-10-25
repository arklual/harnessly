package typist

import (
	"errors"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const limitNotice = "You've used 75% of your weekly limit · resets Oct 28, 5am (UTC)"

// Claude's usage notice, right-aligned on the row above the box, in a short pane whose footer has no «esc to
// interrupt»: the agent works (its spinner says so) and the box is empty. The notice is no text in the box.
func TestAUsageNoticeAboveTheBoxIsNotInput(t *testing.T) {
	s := Read(tui(t, "claude-2.1.310-limit-notice-busy-short-pane.ansi.txt"), wire.ProviderClaude)
	if !strings.Contains(s.Plain, limitNotice) {
		t.Fatal("the capture lost its notice")
	}
	if !s.Busy || s.Input != "" || s.Box != "" || !s.InputLine || s.Menu != NoMenu {
		t.Fatalf("busy=%v input=%q box=%q line=%v menu=%q", s.Busy, s.Input, s.Box, s.InputLine, s.Menu)
	}
	if d := Deliverable(s); d != DecideWait {
		t.Fatalf("decide: %s", d)
	}
	// The same notice over an idle prompt: the box is free, a command goes in.
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	rule := strings.Index(idle, "\x1b[38;5;244m─")
	if rule < 0 {
		t.Fatal("no box rule in the idle capture")
	}
	withNotice := idle[:rule] + strings.Repeat(" ", 40) + "\x1b[38;5;246m" + limitNotice + "\x1b[39m\n" + idle[rule:]
	s = Read(withNotice, wire.ProviderClaude)
	if s.Busy || s.Input != "" || s.Box != "" || !s.PromptReady() {
		t.Fatalf("idle: busy=%v input=%q box=%q ready=%v", s.Busy, s.Input, s.Box, s.PromptReady())
	}
	if err := SetModel(newPane(tui(t, "claude-2.1.310-limit-notice-busy-short-pane.ansi.txt")), newClock(), "opus"); !errors.Is(err, ErrNotAtPrompt) {
		t.Fatalf("a command into a working agent: %v", err)
	}
}

// A box that holds text says what it holds: the journal shows what stood in the way of a command.
func TestAnOccupiedBoxIsNamedInTheError(t *testing.T) {
	err := SetModel(newPane(withInput(t, "a draft of the person")), newClock(), "opus")
	if !errors.Is(err, ErrInputOccupied) || !strings.Contains(err.Error(), "a draft of the person") {
		t.Fatalf("%v", err)
	}
}

package typist

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const modelConfirm = `
────────────────────────────────────────────────────────────────────────────────
   Switch model?
   Your next response will be slower and use more tokens

   This conversation is cached for the current model. Switching to Sonnet 5 means the full history gets re-read on
   your next message.

   ❯ 1. Yes, switch to Sonnet 5
     2. No, go back
`

// afterLine is the idle screen with a line of Claude's own below the prompt it came from.
func afterLine(t *testing.T, line string) string {
	t.Helper()
	return tui(t, "claude-idle-placeholder.ansi.txt") + "\n  ⎿  " + line + "\n"
}

func TestAModelIsSetAndTheConfirmationAnswered(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) {
		switch call {
		case "line:/model sonnet":
			p.set(modelConfirm)
		case "keys:Enter":
			p.set(afterLine(t, "Set model to Sonnet 5"))
		}
	}
	if err := SetModel(p, newClock(), "sonnet"); err != nil {
		t.Fatal(err)
	}
	if got := p.sent(); !reflect.DeepEqual(got, []string{"line:/model sonnet", "keys:Enter"}) {
		t.Fatalf("%v", got)
	}
}

func TestAModelThatNeedsNoConfirmationIsSetByTheLineAlone(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) { p.set(afterLine(t, "Set model to Haiku 4.5")) }
	if err := SetModel(p, newClock(), "haiku"); err != nil {
		t.Fatal(err)
	}
	if got := p.sent(); !reflect.DeepEqual(got, []string{"line:/model haiku"}) {
		t.Fatalf("%v", got)
	}
}

func TestAnEffortIsSet(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) { p.set(afterLine(t, "Set effort level to low")) }
	if err := SetEffort(p, newClock(), wire.EffortLow); err != nil {
		t.Fatal(err)
	}
	if got := p.sent(); !reflect.DeepEqual(got, []string{"line:/effort low"}) {
		t.Fatalf("%v", got)
	}
}

// The new line replaces the old one on the pane (it scrolled off, or the screen was redrawn): the level in the line is
// what tells the effort is set.
func TestAnEffortIsSetWhereAnotherLevelWasSetBefore(t *testing.T) {
	p := newPane(afterLine(t, "Set effort level to high"))
	p.onSend = func(call string) { p.set(afterLine(t, "Set effort level to xhigh")) }
	if err := SetEffort(p, newClock(), wire.EffortXHigh); err != nil {
		t.Fatal(err)
	}
	// «high» is not found in «xhigh»: the old line is not the answer to the new command.
	q := newPane(afterLine(t, "Set effort level to xhigh"))
	q.onSend = func(call string) {}
	if err := SetEffort(q, newClock(), wire.EffortHigh); !errors.Is(err, ErrCommandNotApplied) {
		t.Fatalf("%v", err)
	}
}

func TestAModelTheAgentKeptIsDeclined(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) {
		switch call {
		case "line:/model sonnet":
			p.set(modelConfirm)
		case "keys:Enter":
			p.set(afterLine(t, "Kept model as Haiku 4.5"))
		}
	}
	if err := SetModel(p, newClock(), "sonnet"); !errors.Is(err, ErrCommandDeclined) {
		t.Fatalf("%v", err)
	}
}

func TestAModelThatNeverShowsIsNotApplied(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	clock := newClock()
	if err := SetModel(p, clock, "nope"); !errors.Is(err, ErrCommandNotApplied) {
		t.Fatalf("%v", err)
	}
	if clock.slept > commandLimit+commandPoll {
		t.Fatalf("waited %v", clock.slept)
	}
}

func TestACommandIsNotTypedWhereItDoesNotBelong(t *testing.T) {
	for name, c := range map[string]struct {
		screen string
		want   error
	}{
		"busy":       {tui(t, "claude-busy-spinner.ansi.txt"), ErrNotAtPrompt},
		"a menu":     {permissionScreen, ErrNotAtPrompt},
		"a draft":    {withInput(t, "a draft of the person"), ErrInputOccupied},
		"unreadable": {"", ErrNotAtPrompt},
	} {
		p := newPane(c.screen)
		p.readable = c.screen != ""
		if err := SetModel(p, newClock(), "opus"); !errors.Is(err, c.want) || len(p.sent()) != 0 {
			t.Errorf("%s: %v %v", name, err, p.sent())
		}
	}
}

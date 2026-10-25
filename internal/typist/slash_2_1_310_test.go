package typist

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Claude Code 2.1.310 in a short pane (the terminal docked under the chat): «Switch model?» is cut at the bottom and
// only its focused «1. Yes, switch to …» is drawn. The runtime used to read no menu there, press Escape and get
// «Kept model as Opus 5.5» — the model never changed. The focused Yes is enough to answer.
func TestASwitchQuestionCutAtTheBottomIsAnsweredYes(t *testing.T) {
	clipped := tui(t, "claude-2.1.310-switch-model-clipped.ansi.txt")
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) {
		switch call {
		case "line:/model opus":
			p.set(clipped)
		case "keys:Enter":
			p.set(afterLine(t, "Set model to Opus 5.5 and saved as your default for new sessions"))
		}
	}
	if err := SetModel(p, newClock(), "opus"); err != nil {
		t.Fatal(err)
	}
	if got := p.sent(); !reflect.DeepEqual(got, []string{"line:/model opus", "keys:Enter"}) {
		t.Fatalf("%v", got)
	}
}

// The question drawn up to its title for a moment is waited on, not closed.
func TestASwitchQuestionDrawnHalfIsWaitedOn(t *testing.T) {
	clipped := tui(t, "claude-2.1.310-switch-model-clipped.ansi.txt")
	half := clipped[:strings.Index(clipped, "Switch model?")+len("Switch model?")]
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	clock := newClock()
	p.onSend = func(call string) {
		switch call {
		case "line:/model opus":
			p.set(half)
			clock.onSleep = func(total time.Duration) {
				if total == 3*commandPoll {
					p.set(clipped)
				}
			}
		case "keys:Enter":
			p.set(afterLine(t, "Set model to Opus 5.5"))
		}
	}
	if err := SetModel(p, clock, "opus"); err != nil {
		t.Fatal(err)
	}
	if got := p.sent(); !reflect.DeepEqual(got, []string{"line:/model opus", "keys:Enter"}) {
		t.Fatalf("%v", got)
	}
}

// «Kept model as …» drawn after the line, whoever closed the question (the person in the terminal), is the agent
// saying no: the change is declined, not left to time out.
func TestKeptModelIsDeclinedEvenWhenTheRuntimeDidNotAnswer(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) { p.set(tui(t, "claude-2.1.310-kept-model.ansi.txt")) }
	if err := SetModel(p, newClock(), "opus"); !errors.Is(err, ErrCommandDeclined) {
		t.Fatalf("%v", err)
	}
}

// While the question closes in a short pane, Claude draws its older history again for a few frames: a «Kept model as»
// of before shows, then the screen settles on this command's «Set model to». The change took.
func TestAKeptLineOfBeforeDrawnAgainDoesNotEndTheSwitch(t *testing.T) {
	clipped := tui(t, "claude-2.1.310-switch-model-clipped.ansi.txt")
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	clock := newClock()
	p.onSend = func(call string) {
		switch call {
		case "line:/model opus":
			p.set(clipped)
		case "keys:Enter":
			p.set(afterLine(t, "Kept model as Sonnet 5.5"))
			at := clock.slept
			clock.onSleep = func(total time.Duration) {
				if total == at+4*commandPoll {
					p.set(afterLine(t, "Set model to Opus 5.5"))
				}
			}
		}
	}
	if err := SetModel(p, clock, "opus"); err != nil {
		t.Fatal(err)
	}
}

// The /effort slider of 2.1.310 open over the box: nothing is typed into it.
func TestNoCommandIsTypedIntoTheEffortDialog(t *testing.T) {
	p := newPane(tui(t, "claude-2.1.310-effort-dialog.ansi.txt"))
	if err := SetEffort(p, newClock(), wire.EffortHigh); !errors.Is(err, ErrNotAtPrompt) || len(p.sent()) != 0 {
		t.Fatalf("%v %v", err, p.sent())
	}
}

func TestUltracodeIsTurnedOnAndOff(t *testing.T) {
	for on, c := range map[bool]struct{ line, said string }{
		true:  {"line:/effort ultracode on", "Ultracode on (this session only): dynamic workflows on every task. Effort stays medium."},
		false: {"line:/effort ultracode off", "Ultracode off. Effort stays medium."},
	} {
		p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
		p.onSend = func(call string) { p.set(afterLine(t, c.said)) }
		if err := SetUltracode(p, newClock(), on); err != nil {
			t.Fatalf("%v: %v", on, err)
		}
		if got := p.sent(); !reflect.DeepEqual(got, []string{c.line}) {
			t.Fatalf("%v", got)
		}
	}
	// An «Ultracode on» above, from before, is not the answer to turning it on again.
	p := newPane(afterLine(t, "Ultracode on (this session only): dynamic workflows on every task. Effort stays xhigh."))
	p.onSend = func(call string) {}
	if err := SetUltracode(p, newClock(), true); !errors.Is(err, ErrCommandNotApplied) {
		t.Fatalf("%v", err)
	}
}

func TestUltracodeOnAModelWithoutItIsRefused(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) {
		p.set(afterLine(t, "Ultracode isn't available on claude-haiku-4-5. Valid options are: low, medium, high, auto"))
	}
	if err := SetUltracode(p, newClock(), true); !errors.Is(err, ErrCommandRefused) {
		t.Fatalf("%v", err)
	}
}

// A level leaves ultracode on (2.1.310): «Set effort level to medium» is its answer, the status line keeps
// «medium · ultracode».
func TestALevelIsSetWithUltracodeOn(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) {
		p.set(afterLine(t, "Set effort level to medium (this session only): Balanced approach with standard implementation and testing"))
	}
	if err := SetEffort(p, newClock(), wire.EffortMedium); err != nil {
		t.Fatal(err)
	}
}

// A short pane: the «Set model to Opus 5.5» of the switch before scrolls off as this switch's answer comes, so the
// screen holds one such line before and after. The answer under this command's echo says it took.
func TestTheAnswerUnderTheEchoCountsWhereAnOlderOneScrolledOff(t *testing.T) {
	p := newPane(afterLine(t, "Set model to Haiku 4.5 and saved as your default for new sessions"))
	p.onSend = func(call string) {
		p.set("❯ /model opus\n  ⎿  Set model to Opus 5.5 and saved as your default for new sessions\n\n" + tui(t, "claude-idle-placeholder.ansi.txt"))
	}
	if err := SetModel(p, newClock(), "opus"); err != nil {
		t.Fatal(err)
	}
	// Under the echo the agent kept its model: declined.
	q := newPane(afterLine(t, "Set model to Haiku 4.5"))
	q.onSend = func(call string) {
		q.set("❯ /model opus\n  ⎿  Kept model as Haiku 4.5\n\n" + tui(t, "claude-idle-placeholder.ansi.txt"))
	}
	if err := SetModel(q, newClock(), "opus"); !errors.Is(err, ErrCommandDeclined) {
		t.Fatalf("%v", err)
	}
}

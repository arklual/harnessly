package typist

import (
	"errors"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/screen"
)

// claudeFooters are the footers of Claude's modes in the order shift+tab goes round them (2.1.319, launched with
// --allow-dangerously-skip-permissions; no auto mode on this account).
var claudeFooters = []struct{ mode, footer string }{
	{screen.ClaudeModeAcceptEdits, "⏵⏵ accept edits on\x1b[38;5;246m (shift+tab to cycle)"},
	{screen.ClaudeModePlan, "⏸ plan mode on\x1b[38;5;246m (shift+tab to cycle)"},
	{screen.ClaudeModeBypass, "⏵⏵ bypass permissions on\x1b[38;5;246m (shift+tab to cycle)"},
	{screen.ClaudeModeDefault, "? for shortcuts"},
}

const fixtureFooter = "⏵⏵ accept edits on\x1b[38;5;246m (shift+tab to cycle)"

// modesPane is the idle Claude screen whose footer goes round claudeFooters with each shift+tab.
func modesPane(t *testing.T, at int, cycle []struct{ mode, footer string }) *fakePane {
	t.Helper()
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	if !strings.Contains(idle, fixtureFooter) {
		t.Fatal("fixture has no mode footer")
	}
	p := newPane(strings.Replace(idle, fixtureFooter, cycle[at].footer, 1))
	p.onSend = func(call string) {
		if call == "keys:BTab" {
			at = (at + 1) % len(cycle)
			p.set(strings.Replace(idle, fixtureFooter, cycle[at].footer, 1))
		}
	}
	return p
}

func TestClaudePermissionModeIsReadOffTheFooter(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	for _, f := range claudeFooters {
		got, ok := screen.ClaudePermissionMode(screen.StripANSI(strings.Replace(idle, fixtureFooter, f.footer, 1)))
		if !ok || got != f.mode {
			t.Fatalf("%s: %q %v", f.mode, got, ok)
		}
	}
	// The history may say the words: only the lines under the box count.
	quoted := "● plan mode on is what you asked for\n" + strings.Replace(idle, fixtureFooter, "? for shortcuts", 1)
	if got, _ := screen.ClaudePermissionMode(screen.StripANSI(quoted)); got != screen.ClaudeModeDefault {
		t.Fatalf("history read as the footer: %q", got)
	}
	if _, ok := screen.ClaudePermissionMode("● working…\n"); ok {
		t.Fatal("a screen with no box has no mode")
	}
}

func TestSetPermissionModeGoesRoundToTheMode(t *testing.T) {
	p := modesPane(t, 0, claudeFooters)
	if err := SetPermissionMode(p, newClock(), screen.ClaudeModeDefault); err != nil {
		t.Fatal(err)
	}
	if got := p.sent(); len(got) != 3 {
		t.Fatalf("three presses from accept edits to default: %q", got)
	}
	// Already there: nothing is pressed.
	if err := SetPermissionMode(p, newClock(), screen.ClaudeModeDefault); err != nil || len(p.sent()) != 3 {
		t.Fatalf("%v %q", err, p.sent())
	}
}

func TestSetPermissionModeStopsWhenTheCycleHasNoSuchMode(t *testing.T) {
	// Launched without the flag that allows bypass: shift+tab never reaches it.
	p := modesPane(t, 0, []struct{ mode, footer string }{claudeFooters[0], claudeFooters[1], claudeFooters[3]})
	if err := SetPermissionMode(p, newClock(), screen.ClaudeModeBypass); !errors.Is(err, ErrCommandRefused) {
		t.Fatalf("%v", err)
	}
	// Round once, back to accept edits where it started.
	if n := len(p.sent()); n != 3 {
		t.Fatalf("presses: %d", n)
	}
	if got, _ := PermissionModeNow(p); got != screen.ClaudeModeAcceptEdits {
		t.Fatalf("left in %q", got)
	}
	// A footer that does not move on a press: the agent did not take it.
	still := newPane(strings.Replace(tui(t, "claude-idle-placeholder.ansi.txt"), fixtureFooter, claudeFooters[1].footer, 1))
	if err := SetPermissionMode(still, newClock(), screen.ClaudeModeDefault); !errors.Is(err, ErrCommandNotApplied) {
		t.Fatalf("%v", err)
	}
	// A screen at work is no place for the keys.
	if err := SetPermissionMode(newPane(permissionScreen), newClock(), screen.ClaudeModePlan); !errors.Is(err, ErrNotAtPrompt) {
		t.Fatalf("%v", err)
	}
}

func TestFastAnswersAreCounted(t *testing.T) {
	plain := "❯ /fast on\n  ⎿  Fast mode ON\n❯ /fast off\n  ⎿  Fast mode OFF\n  ⎿  Kept Fast mode OFF\n"
	if screen.CountFast(plain, true) != 1 || screen.CountFast(plain, false) != 2 {
		t.Fatalf("%d %d", screen.CountFast(plain, true), screen.CountFast(plain, false))
	}
	if screen.CountFastRefused("  ⎿  Fast mode is not available on Sonnet") != 1 {
		t.Fatal("refusal")
	}
}

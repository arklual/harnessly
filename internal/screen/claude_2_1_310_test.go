package screen

import (
	"strings"
	"testing"
)

// Claude Code 2.1.310, captured from a real session: ultracode is a mode beside the effort, the /effort dialog has
// its own toggle for it, and in a short pane the «Switch model?» question is cut at the bottom.

func TestTheSwitchQuestionCutInAShortPaneStillShowsItsFocusedYes(t *testing.T) {
	plain := StripANSI(fixture(t, "claude-2.1.310-switch-model-clipped.ansi.txt"))
	if !IsLaunchConfirm(plain) {
		t.Fatal("not read as the question")
	}
	// The whole menu is not there: «2. No, go back» was not drawn. This is what made the runtime press Escape.
	if m := FindMenu(plain); m.YesFocused() {
		t.Fatalf("the menu reader was expected to miss the cut menu: %+v", m)
	}
	if seen, yes := ConfirmFocus(plain); !seen || !yes {
		t.Fatalf("focus seen %v yes %v", seen, yes)
	}
	full := StripANSI(fixture(t, "claude-2.1.310-switch-model-confirm.ansi.txt"))
	if seen, yes := ConfirmFocus(full); !seen || !yes || !FindMenu(full).YesFocused() {
		t.Fatal("the whole question")
	}
	// The same question on the effort, and an answer focused on "No": seen, not yes.
	effort := strings.Replace(plain, "Switch model?", "Change effort level?", 1)
	if seen, yes := ConfirmFocus(effort); !seen || !yes {
		t.Fatal("the effort's question")
	}
	no := strings.Replace(strings.Replace(full, "❯ 1. Yes", "  1. Yes", 1), "  2. No", "❯ 2. No", 1)
	if seen, yes := ConfirmFocus(no); !seen || yes {
		t.Fatalf("a focused No: seen %v yes %v", seen, yes)
	}
	// Drawn up to the title only: no answer seen yet.
	title := plain[:strings.Index(plain, "Switch model?")+len("Switch model?")]
	if seen, _ := ConfirmFocus(title); seen {
		t.Fatal("a title alone has no focus")
	}
	if seen, _ := ConfirmFocus(StripANSI(fixture(t, "claude-idle-placeholder.ansi.txt"))); seen {
		t.Fatal("no question, no focus")
	}
}

func TestKeptModelIsTheRefusalLine(t *testing.T) {
	plain := StripANSI(fixture(t, "claude-2.1.310-kept-model.ansi.txt"))
	if CountKept(plain) != 1 || IsLaunchConfirm(plain) || CountModelSet(plain) != 1 {
		t.Fatal(plain)
	}
	st := Read(fixture(t, "claude-2.1.310-kept-model.ansi.txt"))
	if !st.InputLine || st.Input != "" || st.Menu != nil || st.Busy {
		t.Fatalf("%+v", st)
	}
}

// The /effort dialog of 2.1.310: a slider and «Ultracode on · Tab to toggle». It is a panel, not the prompt: nothing is
// typed while it is open.
func TestTheEffortDialogWithItsUltracodeToggleIsAPanel(t *testing.T) {
	text := fixture(t, "claude-2.1.310-effort-dialog.ansi.txt")
	st := Read(text)
	if !st.Panel || st.Menu != nil {
		t.Fatalf("panel %v menu %+v", st.Panel, st.Menu)
	}
	if !strings.Contains(st.Plain, "Ultracode: dynamic workflows on every task") || !strings.Contains(st.Plain, "Tab to toggle") {
		t.Fatal("not the dialog of 2.1.310")
	}
	cancelled := Read(fixture(t, "claude-2.1.310-effort-cancelled.ansi.txt"))
	if cancelled.Panel || !cancelled.InputLine || cancelled.Input != "" {
		t.Fatalf("%+v", cancelled)
	}
}

func TestTheAnswersOfUltracode(t *testing.T) {
	on := "  ⎿  Ultracode on (this session only): dynamic workflows on every task. Effort stays medium."
	off := "  ⎿  Ultracode off. Effort stays high."
	refused := "  ⎿  Ultracode isn't available on claude-haiku-4-5. Valid options are: low, medium, high, auto"
	if CountUltracode(on, true) != 1 || CountUltracode(on, false) != 0 || CountUltracode(off, false) != 1 || CountUltracode(off, true) != 0 {
		t.Fatal("on and off")
	}
	// The status line under the box says «medium · ultracode»: that is no answer.
	if CountUltracode("◐ medium · ultracode · /effort", true) != 0 {
		t.Fatal("the status line")
	}
	if CountUltracodeRefused(refused) != 1 || CountUltracodeRefused("  ⎿  Ultracode needs dynamic workflows enabled (see /config).") != 1 || CountUltracodeRefused(on) != 0 {
		t.Fatal("refused")
	}
}

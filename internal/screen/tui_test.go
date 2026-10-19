package screen

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Claude Code 2.1.282 and Codex 0.152 captures (testdata/tui).

func TestFindMenu(t *testing.T) {
	confirm := FindMenu(fixture(t, "claude-switch-model-confirm.txt"))
	if confirm == nil || !reflect.DeepEqual(confirm.Options, []string{"1. Yes, switch to Sonnet 5", "2. No, go back"}) ||
		confirm.FocusedIndex != 0 || confirm.Title == nil || *confirm.Title != "Switch model?" {
		t.Errorf("model confirmation: %+v", confirm)
	}
	if !confirm.YesFocused() {
		t.Error("the model confirmation has its Yes focused")
	}

	if effort := FindMenu(fixture(t, "claude-effort-confirm.txt")); effort == nil || effort.Options[0] != "1. Yes, switch to medium" {
		t.Errorf("effort confirmation: %+v", effort)
	}

	step1 := FindMenu(fixture(t, "codex-model-step1.txt"))
	if step1 == nil || step1.Title == nil || *step1.Title != "Select Model and Effort" || len(step1.Options) != 5 {
		t.Errorf("codex step 1: %+v", step1)
	}
	step2 := FindMenu(fixture(t, "codex-model-step2.txt"))
	if step2 == nil || step2.Title == nil || *step2.Title != "Select Reasoning Level for gpt-5.6-sol" || step2.FocusedIndex != 2 {
		t.Errorf("codex step 2: %+v", step2)
	}
	if step2.YesFocused() {
		t.Error("a menu without Yes has it focused")
	}

	for _, name := range []string{"claude-default-start.txt", "claude-effort-applied.txt", "codex-model-esc-closed.txt", "codex-status.txt", "claude-status.txt"} {
		if m := FindMenu(fixture(t, name)); m != nil {
			t.Errorf("%s: menu %+v on a screen without one", name, m)
		}
	}
}

func TestCountPanelHints(t *testing.T) {
	if CountPanelHints(fixture(t, "claude-status.txt")) == 0 {
		t.Error("the /status panel has no footer")
	}
	for _, name := range []string{"claude-default-start.txt", "codex-status.txt"} {
		if n := CountPanelHints(fixture(t, name)); n != 0 {
			t.Errorf("%s: %d panel hints", name, n)
		}
	}
}

func TestPanelSnapshot(t *testing.T) {
	// The command runner answers a panel with this snapshot (Claude /status).
	snap := PanelSnapshot(fixture(t, "claude-status.txt"))
	if !strings.Contains(snap, "Esc to cancel") {
		t.Errorf("snapshot without the footer:\n%s", snap)
	}
	if len(strings.Split(snap, "\n")) > 30 {
		t.Error("snapshot longer than 30 lines")
	}
	if got := PanelSnapshot("history\n▔▔▔▔▔▔▔▔▔▔\n Status\n\n ───\n Esc to cancel\n\n"); got != " Status\n Esc to cancel" {
		t.Errorf("got %q", got)
	}
}

func TestIsBusy(t *testing.T) {
	cases := []struct {
		name  string
		plain string
		want  bool
	}{
		{"claude spinner", StripANSI(fixture(t, "claude-busy-spinner.ansi.txt")), true},
		{"effort applied", fixture(t, "claude-effort-applied.txt"), false},
		{"model confirmation", fixture(t, "claude-switch-model-confirm.txt"), false},
		{"codex working", "• Working (3s • esc to interrupt)", true},
	}
	for _, c := range cases {
		if got := IsBusy(c.plain); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestIsBusyAnywhere(t *testing.T) {
	var todos []string
	for i := range 20 {
		todos = append(todos, fmt.Sprintf("  ☐ step %d", i))
	}
	screen := "✽ Contemplating… (12s · ↓ 20 tokens)\n" + strings.Join(todos, "\n") + "\n\n╭────╮\n│ > │\n╰────╯\n  ? for shortcuts"
	if IsBusy(screen) {
		t.Error("the spinner above a long todo list is within the last lines")
	}
	if !IsBusyAnywhere(screen) {
		t.Error("the spinner pushed up by a todo list is not seen")
	}
	if IsBusyAnywhere("✻ Baked for 5s\n\n╭────╮\n│ > │\n╰────╯\n  ? for shortcuts") {
		t.Error("a finished turn reads as busy")
	}
}

func TestReadInputLine(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
		ok   bool
	}{
		{"claude dim placeholder", fixture(t, "claude-idle-placeholder.ansi.txt"), "", true},
		{"claude draft", fixture(t, "claude-input-draft.ansi.txt"), "half-typed text", true},
		{"codex dim placeholder", fixture(t, "codex-idle-placeholder.ansi.txt"), "", true},
		{"codex draft", fixture(t, "codex-input-draft.ansi.txt"), "half-typed", true},
		{"no prompt", "plain output\nwithout a prompt", "", false},
		{"reset before the marker, dim after", "\x1b[0m❯ \x1b[2mTry something\x1b[0m", "", true},
		{"dim cleared before the text", "❯ \x1b[2m\x1b[22mtyped", "typed", true},
		{"marker glued to text is no input line", "❯typed", "", false},
		{"bare marker", "❯", "", true},
	}
	for _, c := range cases {
		got, ok := ReadInputLine(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %q %v, want %q %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// What the command runner reads before typing a line and while polling (runCommandLine in the previous runtime).
func TestReadForCommandRunner(t *testing.T) {
	idle := Read(fixture(t, "claude-idle-placeholder.ansi.txt"))
	if idle.Menu != nil || idle.Busy || !idle.InputLine || idle.Input != "" {
		t.Errorf("idle Claude: %+v", idle)
	}
	if s := Read(fixture(t, "claude-switch-model-confirm.txt")); s.Menu == nil || !IsLaunchConfirm(s.Plain) {
		t.Error("a menu is open on the model confirmation")
	}
	if s := Read(fixture(t, "claude-input-draft.ansi.txt")); s.Input == "" {
		t.Error("the draft is not seen")
	}
	if s := Read(fixture(t, "codex-model-step1.txt")); s.Menu == nil || *s.Menu.Title != "Select Model and Effort" {
		t.Errorf("codex /model menu: %+v", s.Menu)
	}
	status := Read(fixture(t, "claude-status.txt"))
	if !status.Panel || CountPanelHints(status.Plain) <= CountPanelHints(idle.Plain) {
		t.Error("the /status panel adds no footer to the idle screen")
	}

	// A change is applied when its "Set … to" count grows compared with the screen before typing.
	applied := fixture(t, "claude-effort-applied.txt")
	if CountEffortSet(applied) <= CountEffortSet(idle.Plain) {
		t.Error("the applied effort is not counted")
	}
	// The idle capture itself already shows an earlier "Set model to", so it is no baseline for the model count.
	if CountModelSet(idle.Plain) != 1 {
		t.Error("the idle capture's earlier model change is not counted")
	}
	model := strings.Replace(applied, "Set effort level to medium", "Set model to Sonnet 5", 1)
	if CountModelSet(model) <= CountModelSet(applied) || IsLaunchConfirm(model) {
		t.Error("the applied model is not counted")
	}
	if CountKept(fixture(t, "claude-switch-model-cancelled.txt")) == 0 || CountKept(applied) != 0 {
		t.Error("a declined change is not counted")
	}
	if CountEffortSet("Ultracode on (this session only): dynamic workflows on every task.") != 1 {
		t.Error("ultracode is an applied effort")
	}
}

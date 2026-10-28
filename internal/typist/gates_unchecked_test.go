package typist

import (
	"errors"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// uncheckedScreen is the dialog of a command Claude's checker could not read: the command's box is closed by a solid
// rule, the reason stands under it, two options and no «don't ask again» (a background agent's prompt).
const (
	uncheckedScreen  = "claude-bash-shell-c-unchecked.txt"
	uncheckedCommand = `S=/tmp/claude-995/-var-lib-example-ai-profiles-person-projects/37e6e6ec-765c-4a13-bd6b-d7b85c6c4365/scratchpad; cd $S/qt; for sh in "$S/fish/fish --no-config" "bash --norc" "dash"; do rm -f got.txt; $sh -c "$(cat line.txt)"; cmp -s got.txt want.txt && echo "OK $sh" || { echo "FAIL $sh"; od -c got.txt | head -5; }; done`
)

// The three-option dialog of an ordinary command: the command under the last rule, as before.
const threeOptionScreen = `● Bash(rm -rf build)

────────────────────────────────────────────────────────────────────────────────
 Bash command

 │ rm -rf build
 │ Remove the build directory

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands in /w
   3. No, and tell Claude what to do differently (esc)

 Esc to cancel · Tab to amend · ctrl+e to explain
`

func TestUncheckedShellPromptIsAPermission(t *testing.T) {
	s := Read(tui(t, uncheckedScreen), wire.ProviderClaude)
	if s.Menu != MenuPermission || strings.Join(s.Options, "|") != "1. Yes|2. No" {
		t.Fatalf("menu %q %q", s.Menu, s.Options)
	}
	drawn := DrawnCommand(s.Plain)
	if strings.Count(drawn, "\n") != 2 || !strings.HasPrefix(drawn, "S=/tmp/") || !strings.HasSuffix(drawn, "done") {
		t.Fatalf("drawn %q", drawn)
	}
	for _, c := range []struct {
		name, command string
		ok            bool
	}{
		{"the summary of the call", "$ " + uncheckedCommand, true},
		{"the command as the screen draws it", "$ " + drawn, true},
		{"another command", "$ rm -rf /", false},
		{"the same command with more after it", "$ " + uncheckedCommand + "; rm -rf ~", false},
	} {
		err := Shows(s, PickWant{N: 1, Label: "Yes", Command: c.command})
		if c.ok != (err == nil) || err != nil && !errors.Is(err, ErrMenuChanged) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestThreeOptionShellPromptIsStillRead(t *testing.T) {
	s := Read(threeOptionScreen, wire.ProviderClaude)
	if s.Menu != MenuPermission || len(s.Options) != 3 || !strings.HasPrefix(s.Options[1], "2. Yes, and don't ask again") {
		t.Fatalf("menu %q %q", s.Menu, s.Options)
	}
	if got := DrawnCommand(s.Plain); got != "rm -rf build\nRemove the build directory" {
		t.Fatalf("drawn %q", got)
	}
}

// A gutter run above the last rule counts only when the rule closes it: text between them is not the command's box.
func TestCommandBlockAboveTheRuleMustEndAtIt(t *testing.T) {
	screen := strings.Replace(tui(t, uncheckedScreen), "; done\n─", "; done\nstray line\n─", 1)
	if !strings.Contains(screen, "stray line") {
		t.Fatal("the screen did not change")
	}
	if got := commandBlock(screen); got != nil {
		t.Fatalf("block %q", got)
	}
}

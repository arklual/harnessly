package screen

import (
	"strings"
	"testing"
)

// HRN-647: a terminal docked under the chat leaves Claude a few lines, and its dialog shows one option or two.
func TestFindMenuInALowWindow(t *testing.T) {
	for name, c := range map[string]struct {
		pane    string
		options []string
		cut     bool
	}{
		"one option of a plan": {
			pane:    " Claude has written up a plan and is ready to execute. Would you like to proceed?\n\n ❯ 1. Yes, and use auto mode\n\n ctrl+g to edit in Vim · ~/.claude/plans/p.md\n",
			options: []string{"1. Yes, and use auto mode"}, cut: true,
		},
		"one option of a permission": {
			pane:    " Do you want to proceed?\n ❯ 1. Yes\n\n Esc to cancel · Tab to amend\n",
			options: []string{"1. Yes"}, cut: true,
		},
		"a wrapped option": {
			pane:    " Proceed?\n ❯ 1. Yes, and switch to BYPASS PERMISSIONS\n      for this session\n\n Esc to cancel\n",
			options: []string{"1. Yes, and switch to BYPASS PERMISSIONS for this session"}, cut: true,
		},
		"two options and an arrow": {
			pane:    " Proceed?\n ❯ 1. Yes, and use auto mode\n ↓ 2. Yes, manually approve edits\n\n ctrl+g to edit\n",
			options: []string{"1. Yes, and use auto mode", "2. Yes, manually approve edits"}, cut: true,
		},
		"a whole menu": {
			pane:    " Proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel\n",
			options: []string{"1. Yes", "2. No"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := FindMenu(c.pane)
			if m == nil {
				t.Fatal("no menu")
			}
			if strings.Join(m.Options, "|") != strings.Join(c.options, "|") || m.Cut != c.cut {
				t.Fatalf("got %q cut=%v", m.Options, m.Cut)
			}
		})
	}
}

// What the person typed, a list in the answer: no dialog.
func TestFindMenuNotInTheBox(t *testing.T) {
	for _, pane := range []string{
		"❯ 1. fix the build\n─────────\n  ? for shortcuts\n",
		"● Next steps:\n  1. Build it\n\n❯ \n─────────\n  esc to interrupt\n",
		"> 1. first\n\nsome words\n",
	} {
		if m := FindMenu(pane); m != nil {
			t.Fatalf("%q: %+v", pane, m)
		}
	}
}

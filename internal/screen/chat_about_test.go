package screen

import "testing"

func TestChatAboutRow(t *testing.T) {
	numbered := "❯ Call the tool\n☐ Color\nPick a color\n❯ 1. Red\n  2. Green\n  3. Blue\n  4. Type something.\n────────\n  5. Chat about this\nEnter to select · ↑/↓ to navigate · Esc to cancel\n"
	onChat := "☐ Layout\nPick a layout\n  1. A        │ box │\n  2. B\n  3. C\n           Notes: press n to add notes\n────────\n❯ Chat about this\nEnter to select · ↑/↓ to navigate · n to add notes · Esc to cancel\n"
	permission := "Do you want to proceed?\n❯ 1. Yes\n  2. No, and tell Claude what to do differently (esc)\n"
	quoted := "● Pick «Chat about this» if you want to talk\n\n❯ \n────────\n  ⏵⏵ accept edits on\n"
	cases := []struct {
		name              string
		screen            string
		present, selected bool
	}{
		{"numbered dialog, cursor on an option", numbered, true, false},
		{"preview dialog, cursor on the row", onChat, true, true},
		{"a permission prompt has no such row", permission, false, false},
		{"the words quoted in the conversation are not the dialog", quoted, false, false},
	}
	for _, c := range cases {
		if present, selected := ChatAboutRow(c.screen); present != c.present || selected != c.selected {
			t.Errorf("%s: present %v selected %v", c.name, present, selected)
		}
	}
}

package screen

import "testing"

func TestAutoDenySeconds(t *testing.T) {
	for _, c := range []struct {
		name, plain string
		secs        int
		ok          bool
	}{
		{"minutes and seconds", "⚠ Claude Code will automatically deny this request in 1:17, to\n avoid blocking progress on an unattended session\n", 77, true},
		{"wrapped between the words", "⚠ Claude Code will automatically\n deny this request in 0:09, to avoid", 9, true},
		{"seconds alone", "will automatically deny this request in 45s", 45, true},
		{"no warning", " Do you want to proceed?\n ❯ 1. Yes\n   2. No\n", 0, false},
	} {
		secs, ok := AutoDenySeconds(c.plain)
		if secs != c.secs || ok != c.ok {
			t.Errorf("%s: %d %v", c.name, secs, ok)
		}
	}
}

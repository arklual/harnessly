package proc

import (
	"os"
	"testing"
)

func TestStateOfStat(t *testing.T) {
	for _, c := range []struct {
		in   string
		want byte
		ok   bool
	}{
		{"123 (claude) S 1 2 3", 'S', true},
		{"123 (my (odd) name) D 1 2 3", 'D', true},
		{"123 (x)", 0, false},
		{"garbage", 0, false},
	} {
		got, ok := stateOfStat(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %c %v", c.in, got, ok)
		}
	}
}

// a process in an uninterruptible wait, or alive with an empty command line, cannot be told apart; a zombie
// or a readable command line can.
func TestUnreadableState(t *testing.T) {
	read := func(s string, ok bool) func() (string, bool) { return func() (string, bool) { return s, ok } }
	cases := []struct {
		st   byte
		cmd  func() (string, bool)
		want bool
	}{
		{'D', read("claude\x00", true), true},
		{'S', read("", true), true},
		{'S', read("", false), true},
		{'S', read("claude\x00", true), false},
		{'Z', read("", true), false},
	}
	for _, c := range cases {
		if got := unreadableState(c.st, c.cmd); got != c.want {
			t.Errorf("%c: %v, want %v", c.st, got, c.want)
		}
	}
	if Unreadable(os.Getpid()) {
		t.Error("this test's own process reads as unreadable")
	}
}

package screen

import "testing"

func TestJoinedIn(t *testing.T) {
	a := "Первое сообщение человека, длинное и своё."
	b := "Второе сообщение,\nв две строки."
	for _, c := range []struct {
		entry, text string
		want        bool
	}{
		{a + "\n" + b, a, true},
		{a + "\n" + b, b, true},
		{b + "\n" + a, a, true},
		{a + "\n\n " + b, b, true},
		{a + "\n" + b, a + "\n" + b, false},       // the whole entry: SameMessage's case
		{a, a, false},                             // one message
		{a + "\n" + b, "Первое сообщение", false}, // a part of a line
		{a + "\n" + b, "в две строки.", true},     // a whole line is a whole message
		{a + "\n" + b, "", false},
	} {
		if got := JoinedIn(c.entry, c.text); got != c.want {
			t.Errorf("JoinedIn(%q, %q) = %v, want %v", c.entry, c.text, got, c.want)
		}
	}
}

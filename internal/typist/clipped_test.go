package typist

import (
	"reflect"
	"strings"
	"testing"
)

// labText is the message of the claude-2.1.310-input-long-* captures (a real Claude Code 2.1.310 in its own tmux): a
// single line of 685 characters, typed as keys with an Enter that Claude took as part of the burst.
func labText() string {
	rs := []rune(strings.Repeat("Это проверочный текст для замера поля ввода, в нём нет просьб и задач. ", 12))[:660]
	return strings.TrimRight(string(rs), " ") + " Ответь одним словом: ок."
}

// A recorded case, tab-dBSEBV: a long line typed with its Enter right behind it; Claude read the burst as a paste and the
// Enter as a newline, and the pane, short, shows only the box's last rows. The text did not leave the box: it must not
// count as sent, and the Enter is pressed again until it goes (the second Enter after a swallowed one submits).
func TestASwallowedEnterUnderAClippedBoxIsPressedAgain(t *testing.T) {
	clipped := tui(t, "claude-2.1.310-input-long-clipped-enter-swallowed.ansi.txt")
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	if s := Read(clipped, claude); !StillInBox(s, claude, labText()) {
		t.Fatalf("own text in a clipped box not seen: %q", s.Box)
	}
	p := newPane(idle)
	enters := 0
	p.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "line:"):
			p.set(clipped)
		case call == "keys:Enter":
			if enters++; enters == 2 {
				p.set(idle)
			}
		}
	}
	res := Type(Message{Text: labText()}, claude, p, newClock())
	if res.Outcome != Sent || !reflect.DeepEqual(p.sent(), []string{"line:" + labText(), "keys:Enter", "keys:Enter"}) {
		t.Fatalf("%+v %v", res.Outcome, p.sent())
	}
}

// The same message found in the box on a later look (the try before counted it lost): it is ours, only its Enter is
// pressed — never typed again — whether the box shows all of it or only its tail.
func TestOurLongTextInTheBoxIsEnteredNotRetyped(t *testing.T) {
	for _, f := range []string{"claude-2.1.310-input-long-clipped-enter-swallowed.ansi.txt", "claude-2.1.310-input-long-enter-swallowed.ansi.txt"} {
		p := newPane(tui(t, f))
		plan := Decide(Message{Text: labText()}, Read(tui(t, f), claude), claude)
		if plan.Decision != DecideSend || !plan.OwnText {
			t.Fatalf("%s: %+v", f, plan)
		}
		leaves(t, p)
		res := Type(Message{Text: labText()}, claude, p, newClock())
		if res.Outcome != Sent || !res.Own || !reflect.DeepEqual(p.sent(), []string{"keys:Enter"}) {
			t.Fatalf("%s: %+v %v", f, res.Outcome, p.sent())
		}
	}
	// Another message waiting behind it finds the box taken: someone else's text for it, nothing typed.
	p := newPane(tui(t, "claude-2.1.310-input-long-clipped-enter-swallowed.ansi.txt"))
	if res := Type(Message{Text: "Ещё момент, который мы совершенно не проработали"}, claude, p, newClock()); res.Outcome != BlockedText || len(p.sent()) != 0 {
		t.Fatalf("%+v %v", res.Outcome, p.sent())
	}
}

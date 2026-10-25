package typist

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// A recorded case, tab-dBSEBV 21:31–21:37: four big pictures on a loaded machine; Claude reads the paste («Pasting…», a real
// 2.1.310 capture) for longer than the old 9.5 s, the try gave up, pressed ctrl+u into a box Claude was still filling
// (it takes nothing then) and the references landed after it: the box was left dirty, the message retried, the
// pictures doubled. Now a paste Claude still reads is waited for, and no key goes in meanwhile.
func TestAPasteClaudeStillReadsIsWaitedFor(t *testing.T) {
	pasting := tui(t, "claude-2.1.310-pasting-pictures.ansi.txt")
	four := tui(t, "claude-2.1.310-four-pictures-in-box.ansi.txt")
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	if !Pasting(Read(pasting, claude).Plain) || Pasting(Read(four, claude).Plain) {
		t.Fatal("the «Pasting…» footer misread")
	}
	if s := Read(four, claude); s.Pictures != 4 || !s.OnlyPictures {
		t.Fatalf("%+v", s)
	}
	p := newPane(idle)
	clock := newClock()
	var pastedAt time.Duration
	p.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "paste:/u/"):
			p.set(pasting)
			pastedAt = clock.slept
		case call == "keys:Enter":
			p.set(idle)
		}
	}
	clock.onSleep = func(total time.Duration) {
		if total-pastedAt >= 20*time.Second && Pasting(Read(p.screen, claude).Plain) {
			p.set(four) // 20 s of reading, far past the old limit
		}
	}
	imgs := []string{"/u/a.png", "/u/b.png", "/u/c.png", "/u/d.png"}
	res := Type(Message{Text: "смотри", Images: imgs}, claude, p, clock)
	want := []string{"paste:/u/a.png /u/b.png /u/c.png /u/d.png", "paste: смотри", "keys:Enter"}
	if res.Outcome != Sent || !reflect.DeepEqual(p.sent(), want) {
		t.Fatalf("%s %q", res.Outcome, p.sent())
	}
}

// References that land after the wait gave up (no «Pasting…» seen, a slow screen) are ours: the paths are not pasted
// again, and the text goes with them.
func TestLateReferencesAreTakenOut(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	refs := []string{"[Image #7]", "[Image #8]"}
	box := func() string {
		if len(refs) == 0 {
			return idle
		}
		return withInput(t, strings.Join(refs, " "))
	}
	p := newPane(idle)
	clock := newClock()
	pasted := false
	p.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "paste:/u/"):
			pasted = true
		case call == "keys:BSpace":
			refs = refs[:len(refs)-1]
			p.set(box())
		}
	}
	clock.onSleep = func(total time.Duration) {
		if pasted && total >= imageConfirmBase+imageConfirmPerImage-imagePoll && len(refs) == 2 && p.screen == idle {
			p.set(withInput(t, "[Image #7]")) // one of two, just as the try gives up
		}
	}
	res := Type(Message{Text: "x", Images: []string{"/u/a.png", "/u/b.png"}}, claude, p, clock)
	want := []string{"paste:/u/a.png /u/b.png", "paste: x", "keys:Enter"}
	if res.Outcome != Sent || !res.AsPath || !reflect.DeepEqual(p.sent(), want) {
		t.Fatalf("%s %q box=%q", res.Outcome, p.sent(), Read(p.screen, claude).Box)
	}
}

// A box Claude is still filling with pictures takes no message: the look says wait.
func TestNothingIsTypedWhileClaudeReadsAPaste(t *testing.T) {
	p := newPane(tui(t, "claude-2.1.310-pasting-pictures.ansi.txt"))
	for _, steer := range []bool{false, true} {
		if res := Type(Message{Text: "hello", Steer: steer}, claude, p, newClock()); res.Outcome != Wait || len(p.sent()) != 0 {
			t.Fatalf("%s %v", res.Outcome, p.sent())
		}
	}
}

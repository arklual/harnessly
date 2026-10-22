package prompts

import (
	"strings"
	"testing"
)

// The live screen of a tab whose fifth picture reference the box wrapped in the middle: "[Image" ends the first line,
// "#104]" starts the second.
const wrappedRefFixture = "claude-pictures-wrapped-mid-ref.ansi.txt"

const wrappedRefs = "[Image #100] [Image #101] [Image #102] [Image #103] [Image\n  #104]"

// wrappedRefScreens are that screen before the pictures went in (an empty box) and after.
func wrappedRefScreens(t *testing.T) (before, after string) {
	t.Helper()
	after = tui(t, wrappedRefFixture)
	if !strings.Contains(after, wrappedRefs) {
		t.Fatal("fixture has no wrapped references")
	}
	return strings.Replace(after, wrappedRefs, "", 1), after
}

func TestAReferenceWrappedMidwayCounts(t *testing.T) {
	before, after := wrappedRefScreens(t)
	if got := CountImageRefs(stripForTest(after)); got != 5 {
		t.Errorf("counted %d", got)
	}
	if got := NewImageRefs(stripForTest(before), stripForTest(after)); got != 5 {
		t.Errorf("new %d", got)
	}
	screens := []string{stripForTest(before), stripForTest(after)}
	n := 0
	capture := func() (string, bool) { s := screens[min(n, 1)]; n++; return s, true }
	if res := SendImagePaths(picturePaths(5), true, func(string) error { return nil }, capture, newFakeClock()); !res.Confirmed || res.Attached != 5 {
		t.Fatalf("%+v", res)
	}
}

// The live case end to end: five pictures go in and the box wraps the last reference; the first try confirms them and
// types the text, it does not time out and leave the queue blocked behind its own pictures.
func TestFivePicturesAndALongTextGoInOneTryOnTheLiveScreen(t *testing.T) {
	before, after := wrappedRefScreens(t)
	long := "длинный текст\n" + strings.Repeat("x", 2300)
	r := newRig(t, before, Tab{})
	r.panes.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "paste:/uploads/"):
			r.panes.setScreen(after)
		case call == "keys:Enter":
			r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
		}
	}
	paths := []string{"/uploads/a.png", "/uploads/b.png", "/uploads/c.png", "/uploads/d.png", "/uploads/e.png"}
	_, _ = r.svc.Queue().Add("tab-1", long, paths)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if got := queueTexts(r, "tab-1"); len(got) != 0 {
		t.Fatalf("left %d, sent %d calls", len(got), len(r.panes.sent()))
	}
	sent := r.panes.sent()
	if len(sent) < 3 || !strings.HasPrefix(sent[0], "paste:/uploads/") || sent[len(sent)-1] != "keys:Enter" {
		t.Fatalf("sent %d calls", len(sent))
	}
}

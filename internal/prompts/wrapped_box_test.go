package prompts

import (
	"reflect"
	"strings"
	"testing"
)

func TestPicturesWrappedMidReferenceAreItsOwn(t *testing.T) {
	_, after := wrappedRefScreens(t)
	long := "длинный текст\n" + strings.Repeat("x", 2300)
	d := newDriver(after)
	paths := []string{"/u/a.png", "/u/b.png", "/u/c.png", "/u/d.png", "/u/e.png"}
	got := Deliver(item(func(i *Item) { i.Text, i.Images = long, paths }), d, NewRegistry(newFakeClock(), nil), true, "")
	if got != OutcomeSent || !reflect.DeepEqual(d.calls, []string{"text: " + long, "enter"}) {
		t.Fatalf("%v %v", got, d.calls)
	}
}

// After a restart the runtime finds that screen: only the text and its Enter go, no picture twice.
func TestARestartFinishesTheLiveScreensMessage(t *testing.T) {
	_, after := wrappedRefScreens(t)
	long := "длинный текст\n" + strings.Repeat("x", 2300)
	r := newRig(t, after, Tab{})
	r.panes.onSend = func(call string) {
		if call == "keys:Enter" {
			r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
		}
	}
	_, _ = r.svc.Queue().Add("tab-1", long, []string{"/uploads/a.png", "/uploads/b.png", "/uploads/c.png", "/uploads/d.png", "/uploads/e.png"})
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	sent := r.panes.sent()
	if len(sent) < 2 || sent[0] != "paste: "+long || sent[len(sent)-1] != "keys:Enter" {
		t.Fatalf("sent %d calls, first %.40q", len(sent), sent)
	}
	if got := queueTexts(r, "tab-1"); len(got) != 0 {
		t.Fatalf("left %d", len(got))
	}
}

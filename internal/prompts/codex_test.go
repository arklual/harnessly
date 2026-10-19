package prompts

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func codexBox(input string) string {
	return strings.Join([]string{"• Working (8s • esc to interrupt)", "", "› " + input, "  tab to queue message"}, "\n")
}

const codexPlaceholder = "• Working (9s • esc to interrupt)\n\n› Find and fix a bug in @filename\n  gpt-5.6-sol low"

// codexWorld shows screens[min(enters-1, last)]: each Enter moves Codex on one screen.
type codexWorld struct {
	clock   *fakeClock
	start   time.Time
	screens []string
	enters  []time.Duration
	pasted  []string
}

func (w *codexWorld) Paste(text string) error { w.pasted = append(w.pasted, text); return nil }
func (w *codexWorld) Enter() error {
	w.enters = append(w.enters, w.clock.Now().Sub(w.start))
	return nil
}
func (w *codexWorld) Capture() (string, bool) {
	i := min(len(w.enters)-1, len(w.screens)-1)
	if i < 0 {
		return "", false
	}
	return w.screens[i], true
}

func TestSubmitCodex(t *testing.T) {
	const text = "Also add the word DELTA."
	c1 := newFakeClock()
	dropped := &codexWorld{clock: c1, start: c1.Now(), screens: []string{codexBox(text), codexPlaceholder}}
	if !SubmitCodex(text, false, dropped, dropped.clock) || len(dropped.enters) != 2 {
		t.Fatalf("dropped Enter: %d enters", len(dropped.enters))
	}
	if gap := dropped.enters[1] - dropped.enters[0]; gap < codexRetryAfter {
		t.Errorf("second Enter after %v", gap)
	}
	taken := &codexWorld{clock: newFakeClock(), screens: []string{codexPlaceholder}}
	if !SubmitCodex(text, false, taken, taken.clock) || len(taken.enters) != 1 || !reflect.DeepEqual(taken.pasted, []string{text}) {
		t.Fatalf("taken: %d enters", len(taken.enters))
	}
	stuck := &codexWorld{clock: newFakeClock(), screens: []string{codexBox(text)}}
	if SubmitCodex(text, false, stuck, stuck.clock) || len(stuck.enters) != 3 {
		t.Fatalf("stuck: %d enters", len(stuck.enters))
	}
	typed := &codexWorld{clock: newFakeClock(), screens: []string{codexPlaceholder}}
	if !SubmitCodex(text, true, typed, typed.clock) || len(typed.pasted) != 0 {
		t.Fatal("text already in the box was pasted again")
	}
}

// A Codex tab's queue is delivered like Claude's: pasted and submitted with its box watched (TS never delivered it).
func TestCodexQueueDelivered(t *testing.T) {
	idle := tui(t, "codex-idle-placeholder.ansi.txt")
	r := newRig(t, idle, Tab{Provider: ProviderCodex})
	r.panes.onSend = func(call string) {
		if call == "keys:Enter" {
			r.panes.setScreen(idle)
		}
	}
	item, _ := r.svc.Queue().Add("tab-1", "Run the tests", nil)
	if got := r.svc.TakeNewPrompt("tab-1", item.ID); got != DeliveryQueued {
		t.Fatalf("delivery %s", got)
	}
	r.clock.Advance(0)
	if !reflect.DeepEqual(r.panes.sent(), []string{"paste:Run the tests", "keys:Enter"}) || len(queueTexts(r, "tab-1")) != 0 {
		t.Fatalf("sent %v, queue %v", r.panes.sent(), queueTexts(r, "tab-1"))
	}
	if _, ok := r.svc.EnterClock().Take(testSession); !ok {
		t.Error("the Enter is not on the clock")
	}
}

// Codex kept the text in its box: the message goes back to the queue, its id is freed, and the next try finds its own
// text in the box and only submits it.
func TestCodexQueueRetryFindsItsText(t *testing.T) {
	idle := tui(t, "codex-idle-placeholder.ansi.txt")
	r := newRig(t, idle, Tab{Provider: ProviderCodex})
	held := "\n› Run the tests\n\n  gpt-5.6-sol low · ~/work\n"
	r.panes.onSend = func(call string) {
		if strings.HasPrefix(call, "paste:") {
			r.panes.setScreen(held)
		}
	}
	_, _ = r.svc.Queue().Add("tab-1", "Run the tests", nil)
	if got := r.svc.Pump("tab-1", true, r.svc.deliveryDriver(Tab{Provider: ProviderCodex, Session: testSession})); got != PumpFailed {
		t.Fatalf("first: %s", got)
	}
	if len(queueTexts(r, "tab-1")) != 1 {
		t.Fatal("the message was lost")
	}
	r.panes.onSend = func(call string) {
		if call == "keys:Enter" {
			r.panes.setScreen(idle)
		}
	}
	before := len(r.panes.sent())
	if got := r.svc.Pump("tab-1", true, r.svc.deliveryDriver(Tab{Provider: ProviderCodex, Session: testSession})); got != PumpSent {
		t.Fatalf("retry: %s", got)
	}
	if again := r.panes.sent()[before:]; !reflect.DeepEqual(again, []string{"keys:Enter"}) {
		t.Fatalf("retry typed %v", again)
	}
}

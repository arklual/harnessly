package outbox

import (
	"reflect"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A message that is being typed, waits for the agent's hook, or waits out its window after a try nobody confirmed
// cannot be taken back: the agent may have it. One that was never typed is cancelled and never reaches the agent.
func TestCancelIsTooLateOnceTheMessageWasTyped(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.send(made1, "первое", wire.ModeQueue)
	r.send(made2, "второе", wire.ModeQueue)
	job := r.try()
	if _, code := r.box.Cancel(made1, r.clock.Now()); code != wire.RefuseTooLate {
		t.Fatalf("cancel while typing: %q", code)
	}
	// The screen shows the text in the box after every Enter: the try ends stuck, and the window opens.
	r.neverLeaves("первое")
	r.record(r.box.Typed(job.ClientID, typist.Type(job.Message, r.agent.Provider, r.pane, r.clock), r.clock.Now()))
	if st := r.state(made1); st.K != wire.MsgQueued {
		t.Fatalf("%+v", st)
	}
	if _, code := r.box.Cancel(made1, r.clock.Now()); code != wire.RefuseTooLate {
		t.Fatalf("cancel inside the window: %q", code)
	}
	// The line keeps the order the messages were written in, and the retype is the first one's.
	if got := r.box.Queue().Items; !reflect.DeepEqual(got, []string{made1, made2}) {
		t.Fatalf("queue %v", got)
	}
	// The second was never typed: it is taken back, and stays out of the pane for good.
	ch, code := r.box.Cancel(made2, r.clock.Now())
	r.record(ch)
	if code != "" || r.state(made2).K != wire.MsgCancelled {
		t.Fatalf("cancel before typing: %q %+v", code, r.state(made2))
	}
	r.pane.view = nil
	r.leaves()
	r.advance(ConfirmWindow)
	if st := r.state(made1); st.K != wire.MsgInInput || r.lines("первое") != 2 {
		t.Fatalf("the retype: %+v, typed %v", st, r.typed())
	}
	if _, code := r.box.Cancel(made1, r.clock.Now()); code != wire.RefuseTooLate {
		t.Fatalf("cancel while it waits for the hook: %q", code)
	}
	r.hook("первое")
	r.advance(24 * time.Hour)
	if r.lines("второе") != 0 {
		t.Fatalf("a cancelled message reached the agent: %v", r.typed())
	}
}

// The window a typed message waits in holds back the typing of the next one only: new messages are taken into the
// queue and shown in their places all the while.
func TestTheConfirmWindowDoesNotHoldBackNewMessages(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.neverLeaves("первое")
	r.send(made1, "первое", wire.ModeQueue)
	r.pump()
	if st := r.state(made1); st.K != wire.MsgQueued || r.lines("первое") != 1 {
		t.Fatalf("%+v", st)
	}
	r.advance(time.Second)
	if code := r.send(made3, "третье", wire.ModeQueue); code != "" {
		t.Fatalf("a send inside the window: %q", code)
	}
	if code := r.send(made2, "второе", wire.ModeQueue); code != "" {
		t.Fatalf("a send inside the window: %q", code)
	}
	r.pump()
	if got := r.box.Queue().Items; !reflect.DeepEqual(got, []string{made1, made2, made3}) {
		t.Fatalf("queue %v", got)
	}
	if got := listed(r.box, r.clock.Now()); !reflect.DeepEqual(got, []string{made1, made2, made3}) {
		t.Fatalf("messages %v", got)
	}
	if st := r.state(made2); st.K != wire.MsgQueued || st.Position != 2 {
		t.Fatalf("%+v", st)
	}
	if st := r.state(made3); st.K != wire.MsgQueued || st.Position != 3 {
		t.Fatalf("%+v", st)
	}
	if r.lines("второе") != 0 || r.lines("третье") != 0 {
		t.Fatalf("typed inside the window: %v", r.typed())
	}
	// The agent's word ends the window at once: the next message goes without waiting it out.
	r.hook("первое")
	r.pane.view = nil
	r.leaves()
	r.record(r.box.TurnStarted("c:aa#0", r.clock.Now()))
	r.record(r.box.TurnEnded("c:aa#0", r.clock.Now()))
	r.pump()
	if r.lines("второе") != 1 || r.lines("третье") != 0 {
		t.Fatalf("after the hook: %v", r.typed())
	}
}

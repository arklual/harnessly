package outbox

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// tab-as454dx7m, 28.10: someone's text held the box, and between looks the head waited on the agent («wait»: the box
// not drawn): every wait reset the term, and the block went on for 2165 looks. Its term runs from the first look that
// found the text, and ends it: the message fails, retryable, instead of holding the line for good.
func TestABlockBySomeonesTextIsTimeBoxed(t *testing.T) {
	draft := withInput(t, "a draft the person is writing")
	r := newRig(t, draft, wire.StateIdle)
	r.send("m1", "hello", wire.ModeSteer)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Reason != wire.QueuedBlockedText || st.Wait == nil {
		t.Fatalf("%+v", st)
	}
	deadline := r.state("m1").Wait.DeadlineAt
	for i := 0; i < 40 && r.state("m1").K == wire.MsgQueued; i++ {
		r.pane.screen = "" // the box not drawn: the screen says wait
		r.advance(10 * time.Second)
		r.pane.screen = draft
		r.advance(10 * time.Second)
		if st := r.state("m1"); st.K == wire.MsgQueued && st.Reason == wire.QueuedBlockedText && st.Wait.DeadlineAt != deadline {
			t.Fatalf("the term moved: %d → %d", deadline, st.Wait.DeadlineAt)
		}
	}
	if st := r.state("m1"); st.K != wire.MsgFailed || !st.Retryable {
		t.Fatalf("%s", r.trail("m1"))
	}
}

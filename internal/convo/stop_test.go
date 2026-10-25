package convo

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The page's Stop on an agent the record took for done — its turn ended by silence and a summary on screen while the
// agent went on, or a short pane hid the spinner — while the screen says it works, with text in the box: Escape
// goes in all the same, once. The box is never asked to be free for it, and a queue no interrupt will end is not held.
func TestStopIsNeverRefusedForTheRecordOrTheBox(t *testing.T) {
	r := newRig(t)
	r.user("long job")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.done()
	for i := 0; i < 40 && r.state().K == wire.StateWorking; i++ {
		r.tick(time.Second)
	}
	if k := r.state().K; k == wire.StateWorking {
		t.Fatalf("the silence never ended the turn: %s", k)
	}
	r.pane.screen = withInput(t, "a draft the person is writing")
	res := r.do(wire.InterruptCmd{OpID: "i1", ConversationID: r.id})
	if res.R != wire.ResultAccepted {
		t.Fatalf("stop refused: %+v", res)
	}
	r.tick(time.Second)
	escapes := 0
	for _, c := range r.pane.calls {
		if c == "keys:Escape" {
			escapes++
		}
	}
	if escapes != 1 {
		t.Fatalf("%d escapes: %v", escapes, r.pane.calls)
	}
	if p := r.snap().Queue.Paused; p != nil {
		t.Fatalf("the queue waits for an interrupt nothing will end: %+v", p)
	}
}

package outbox

import (
	"reflect"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// try starts the head's try and gives its job, as the actor does before it types.
func (r *rig) try() *Job {
	r.t.Helper()
	look := typist.Look(r.pane, r.agent.Provider)
	r.agent.Screen = &look
	job, ch := r.box.Pump(r.agent, r.size, r.clock.Now())
	r.record(ch)
	if job == nil {
		r.t.Fatal("no try")
	}
	return job
}

var lost = typist.Result{Outcome: typist.Failed, Lost: true, Err: typist.ErrPreempted}

// The person's Stop took the pane before the message's first key: the message is not lost — it stays first in the
// queue, not delivered, not failed — and is typed once when its turn comes again.
func TestAMessageCutShortByStopIsNeitherLostNorTypedTwice(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.leaves()
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.send("m2", "и ещё одно", wire.ModeQueue)
	job := r.try()
	r.record(r.box.Typed(job.ClientID, lost, r.clock.Now()))
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Position != 1 {
		t.Fatalf("after Stop: %+v (%s)", st, r.trail("m1"))
	}
	if rec := r.box.recs["m1"]; rec.Tries != 0 || rec.Message.Attempts != 0 || rec.Send != 0 {
		t.Fatalf("a cut try counted as a failed one: tries %d attempts %d send %d", rec.Tries, rec.Message.Attempts, rec.Send)
	}
	if got := r.box.Queue().Items; !reflect.DeepEqual(got, []string{"m1", "m2"}) {
		t.Fatalf("queue %v", got)
	}
	r.advance(retryBase)
	r.hook("проверь логи")
	r.advance(24 * time.Hour)
	if r.lines("проверь логи") != 1 || r.state("m1").K != wire.MsgAccepted {
		t.Fatalf("typed %d times, %s", r.lines("проверь логи"), r.trail("m1"))
	}
}

// Stop came after the Enter: whether the agent took the message is its hook's to say. It did — the message is
// accepted where it stands and is not typed a second time.
func TestAMessageCutShortAfterItsEnterIsNotTypedAgainOnceConfirmed(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.leaves()
	r.send("m1", "проверь логи", wire.ModeQueue)
	job := r.try()
	res := lost
	res.Typed = true
	r.record(r.box.Typed(job.ClientID, res, r.clock.Now()))
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Position != 1 {
		t.Fatalf("after Stop: %+v", st)
	}
	r.hook("проверь логи")
	if st := r.state("m1"); st.K != wire.MsgAccepted {
		t.Fatalf("after its hook: %+v", st)
	}
	r.advance(24 * time.Hour)
	if n := len(r.typed()); n != 0 {
		t.Fatalf("typed again: %v", r.typed())
	}
}

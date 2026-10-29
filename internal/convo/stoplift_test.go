package convo

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// tab-as454dx7m, 29.10: a Stop paused the queue (interrupted), the agent ran turns after it and went idle, and the
// queue stood for good. A turn begun after the stop and ended idle lifts the pause: the queue goes on by itself.
func TestAStopsPauseEndsWithTheNextTurn(t *testing.T) {
	r := newRig(t)
	r.user("long job")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.send("m1", "after that", wire.ModeQueue)
	if res := r.do(wire.InterruptCmd{OpID: "i1", ConversationID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("interrupt: %+v", res)
	}
	r.interruptLine()
	r.idle()
	r.tick(time.Second)
	if p := r.snap().Queue.Paused; p == nil || p.Reason != wire.PausedInterrupted {
		t.Fatalf("queue: %+v", r.snap().Queue)
	}
	// The person types into the terminal: a turn runs and ends.
	r.tick(time.Second)
	r.user("typed in the terminal")
	r.busy()
	r.settle(wire.StateWorking)
	r.agentTakes()
	r.say("done")
	r.turnEnd()
	r.idle()
	for i := 0; i < 10 && r.message("m1").State.K == wire.MsgQueued; i++ {
		r.tick(time.Second)
	}
	if p := r.snap().Queue.Paused; p != nil {
		t.Fatalf("still paused: %+v", p)
	}
	if m := r.message("m1"); m.State.K == wire.MsgQueued {
		t.Fatalf("m1: %s", r.trail("m1"))
	}
}

// A message sent after a Stop is the person's word to go on: the pause goes with it.
func TestAMessageSentAfterAStopLiftsItsPause(t *testing.T) {
	r := newRig(t)
	r.user("long job")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	if res := r.do(wire.InterruptCmd{OpID: "i1", ConversationID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("interrupt: %+v", res)
	}
	r.interruptLine()
	r.idle()
	r.tick(time.Second)
	if p := r.snap().Queue.Paused; p == nil {
		t.Fatal("not paused")
	}
	r.agentTakes()
	r.send("m1", "now this", wire.ModeQueue)
	r.tick(time.Second)
	if p := r.snap().Queue.Paused; p != nil {
		t.Fatalf("still paused: %+v", p)
	}
	if m := r.message("m1"); m.State.K == wire.MsgQueued {
		t.Fatalf("m1: %s", r.trail("m1"))
	}
}

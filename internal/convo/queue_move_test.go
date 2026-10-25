package convo

import (
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func before(id string) *string { return &id }

// queue.move puts a queued message before another or at the end; the answer is on the conversation's channel and the
// new order shows in the queue the page reads.
func TestQueueMoveReordersTheQueuedMessages(t *testing.T) {
	r := newRig(t)
	r.user("long job")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.agentTakes()
	for _, id := range []string{"m1", "m2", "m3"} {
		r.send(id, "text "+id, wire.ModeQueue)
	}

	res := r.do(wire.QueueMoveCmd{OpID: "mv1", ConversationID: r.id, ClientID: "m3", BeforeClientID: before("m1")})
	if res.R != wire.ResultAccepted || res.Ch != wire.ConvChannel(r.id) {
		t.Fatalf("%+v", res)
	}
	if got := r.snap().Queue.Items; !reflect.DeepEqual(got, []string{"m3", "m1", "m2"}) {
		t.Fatalf("%v", got)
	}
	if st := r.message("m3").State; st.K != wire.MsgQueued || st.Position != 1 {
		t.Fatalf("m3: %+v", st)
	}
	if st := r.message("m1").State; st.Position != 2 {
		t.Fatalf("m1: %+v", st)
	}

	res = r.do(wire.QueueMoveCmd{OpID: "mv2", ConversationID: r.id, ClientID: "m3"})
	if res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if got := r.snap().Queue.Items; !reflect.DeepEqual(got, []string{"m1", "m2", "m3"}) {
		t.Fatalf("%v", got)
	}
	if res := r.do(wire.QueueMoveCmd{OpID: "mv3", ConversationID: r.id, ClientID: "m1", BeforeClientID: before("gone")}); res.Code != wire.RefuseNotFound {
		t.Fatalf("%+v", res)
	}
	if res := r.do(wire.QueueMoveCmd{OpID: "mv4", ConversationID: r.id, ClientID: "nope"}); res.Code != wire.RefuseNotFound {
		t.Fatalf("%+v", res)
	}
}

// A message the agent has taken is no longer in the line; the order of the rest is not disturbed by the refusal.
func TestQueueMoveRefusesAMessageThatIsPastTheQueue(t *testing.T) {
	r := newRig(t)
	r.user("first")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.agentTakes()
	r.send("m1", "next please", wire.ModeQueue)
	r.send("m2", "after it", wire.ModeQueue)
	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	if st := r.message("m1").State; st.K != wire.MsgInTurn {
		t.Fatalf("m1 went to the agent: %+v", st)
	}
	if res := r.do(wire.QueueMoveCmd{OpID: "mv1", ConversationID: r.id, ClientID: "m1", BeforeClientID: before("m2")}); res.R != wire.ResultRefused || res.Code != wire.RefuseTooLate {
		t.Fatalf("%+v", res)
	}
}

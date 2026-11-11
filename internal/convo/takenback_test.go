package convo

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func (r *rig) status(id string) wire.MessageStatus {
	r.t.Helper()
	st, err := r.rt.MessageStatus(context.Background(), r.id, []string{id})
	if err != nil || len(st.Messages) != 1 {
		r.t.Fatalf("status of %s: %+v %v", id, st, err)
	}
	return st.Messages[0]
}

// The cancel of a message the runtime has not seen is kept with the conversation, through a restart: the send that
// comes after it is answered, shown as cancelled, and never queued.
func TestACancelBeforeItsSendOutlivesARestart(t *testing.T) {
	r := newRig(t)
	r.open()
	if res := r.do(wire.CancelCmd{OpID: "c1", ConversationID: r.id, ClientID: "m1"}); res.Code != wire.RefuseNotFound {
		t.Fatalf("cancel of an unknown message: %+v", res)
	}
	r.rt.Stop()
	r.rt = r.runtime()
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res := r.send("m1", "taken back", wire.ModeQueue); res.R != wire.ResultAccepted {
		t.Fatalf("the send after its cancel: %+v", res)
	}
	st := r.status("m1")
	if st.K != wire.StatusKnown || st.Message.State.K != wire.MsgCancelled {
		t.Fatalf("%+v", st)
	}
	if q := r.snap().Queue.Items; len(q) != 0 {
		t.Fatalf("queued: %v", q)
	}
}

// A send answered «I have it» whose message the conversation lost is done again when it comes with the same clientId;
// while the conversation knows the message, the same send changes nothing.
func TestASendWhoseMessageWasLostIsDoneAgain(t *testing.T) {
	r := newRig(t)
	r.open()
	r.busy()
	first := r.send("m1", "keep me", wire.ModeQueue)
	if first.R != wire.ResultQueued {
		t.Fatalf("send: %+v", first)
	}
	if again := r.send("m1", "keep me", wire.ModeQueue); again.Seq != first.Seq {
		t.Fatalf("the same send again: %+v, first %+v", again, first)
	}
	if _, err := live(r.rt, context.Background(), r.id, func(c *conv, _ time.Time) (struct{}, error) {
		c.box.Restore(outbox.State{})
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if st := r.status("m1"); st.K != wire.StatusUnknown {
		t.Fatalf("%+v", st)
	}
	if again := r.send("m1", "keep me", wire.ModeQueue); again.R != wire.ResultQueued {
		t.Fatalf("the send of a lost message: %+v", again)
	}
	if st := r.status("m1"); st.K != wire.StatusKnown || st.Message.Text != "keep me" {
		t.Fatalf("%+v", st)
	}
}

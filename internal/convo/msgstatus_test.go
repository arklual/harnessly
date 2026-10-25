package convo

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The page asks where its messages are by their client ids: one the agent took is known with its entry, one the
// runtime never had is unknown, one finished long ago is expired. A conversation that is not here is not-found.
func TestTheStatusOfMessagesByClientID(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()
	if res := r.send("m1", "как дела", wire.ModeQueue); res.R != wire.ResultQueued {
		t.Fatalf("%+v", res)
	}
	r.tick(time.Second)
	got, err := r.rt.MessageStatus(context.Background(), r.id, []string{"m1", "never"})
	if err != nil {
		t.Fatal(err)
	}
	m1, never := got.Messages[0], got.Messages[1]
	if m1.K != wire.StatusKnown || m1.Message.Text != "как дела" || m1.Handed == nil || m1.Handed.Where != wire.HandedTranscript {
		t.Fatalf("m1: %+v %+v", m1, m1.Handed)
	}
	if never.K != wire.StatusUnknown {
		t.Fatalf("never: %+v", never)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}

	// The turn ends; a day later the record is let go and the id is expired.
	r.hook("prompt-submit")
	r.say("Хорошо!")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	r.tick(outbox.FinalStored + time.Minute)
	got, err = r.rt.MessageStatus(context.Background(), r.id, []string{"m1"})
	if err != nil || got.Messages[0].K != wire.StatusExpired {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := r.rt.MessageStatus(context.Background(), "tab-nothere1", []string{"m1"}); err != ErrNotFound {
		t.Fatalf("%v", err)
	}
}

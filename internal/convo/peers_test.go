package convo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/peers"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A message from another agent goes through the conversation's outbox like the person's — typed, accepted, read in the
// turn — but it is the outbox's peer message (its client id, its device), and its user entry is the envelope the feed
// draws as another agent's card, never the person's own words.
func TestPeerMessageGoesThroughTheOutbox(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()

	text := peers.Wrap(peers.Envelope{ID: "pm_abcdef", Person: "Иван", Machine: "dev-department",
		Chat: "Модель по умолчанию", Ref: "dev-department/tab-rsk", Via: "agent", Text: "Проверь модель по умолчанию."})
	res := r.rt.DeliverPeer(context.Background(), r.id, "peer-pm_abcdef", text, true)
	if res.R != wire.ResultQueued {
		t.Fatalf("deliver: %+v", res)
	}
	m := r.message("peer-pm_abcdef")
	if !IsPeerClientID(m.ClientID) || m.Mode != wire.ModeNow || m.Kind != wire.KindPrompt {
		t.Fatalf("an idle agent gets it at the head of the line as a prompt: %+v", m)
	}
	r.tick(time.Second)
	r.hook("prompt-submit")
	var user *wire.Entry
	for i, e := range r.snap().Entries {
		if e.T == wire.EntryUser {
			user = &r.snap().Entries[i]
		}
	}
	if user == nil {
		t.Fatal("no user entry for the peer message")
	}
	env, ok := peers.Parse(user.Text)
	if !ok || env.Person != "Иван" || env.Machine != "dev-department" || env.Ref != "dev-department/tab-rsk" ||
		!strings.Contains(env.Text, "модель по умолчанию") {
		t.Fatalf("the entry is not the envelope: %q", user.Text)
	}
	if user.ClientID == nil || *user.ClientID != "peer-pm_abcdef" {
		t.Fatalf("the entry does not carry the peer message: %+v", user.ClientID)
	}
}

// Delivery «at the next step»: a working agent gets the message steered into its turn; «after the turn» it waits.
func TestPeerMessageToAWorkingAgent(t *testing.T) {
	r := newRig(t)
	r.user("first")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)

	if res := r.rt.DeliverPeer(context.Background(), r.id, "peer-pm_steer1", "<peer-message>x</peer-message>", true); res.R != wire.ResultQueued {
		t.Fatalf("steer: %+v", res)
	}
	if m := r.message("peer-pm_steer1"); m.Mode != wire.ModeSteer {
		t.Fatalf("at the next step means steered: %s", m.Mode)
	}
	if res := r.rt.DeliverPeer(context.Background(), r.id, "peer-pm_turn01", "<peer-message>y</peer-message>", false); res.R != wire.ResultQueued {
		t.Fatalf("turn: %+v", res)
	}
	if m := r.message("peer-pm_turn01"); m.Mode != wire.ModeNow {
		t.Fatalf("after the turn means the head of the line: %s", m.Mode)
	}
}

func TestPeerMessageRefusals(t *testing.T) {
	r := newRig(t)
	r.open()
	if res := r.rt.DeliverPeer(context.Background(), "tab-nope", "peer-pm_000001", "x", true); res.Code != wire.RefuseNotFound {
		t.Fatalf("unknown conversation: %+v", res)
	}
	if res := r.rt.DeliverPeer(context.Background(), r.id, "m1", "x", true); res.Code != wire.RefuseBadRequest {
		t.Fatalf("a person's client id is not a peer message: %+v", res)
	}
}

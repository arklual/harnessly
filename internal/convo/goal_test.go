package convo

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestAGoalQueuesTheGoalCommandAndClearQueuesClear(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	if res := r.do(wire.GoalCmd{OpID: "g1", ID: r.id, Text: "  ship the contract  "}); res.R != wire.ResultQueued || res.Ch != wire.ConvChannel(r.id) {
		t.Fatalf("%+v", res)
	}
	if m := r.message("g1"); m.Text != "/goal ship the contract" || m.Kind != wire.KindCommand {
		t.Fatalf("%+v", m)
	}
	if res := r.do(wire.GoalCmd{OpID: "g2", ID: r.id, Text: ""}); res.R == wire.ResultRefused {
		t.Fatalf("%+v", res)
	}
	if m := r.message("g2"); m.Text != "/goal clear" || m.Kind != wire.KindCommand {
		t.Fatalf("%+v", m)
	}
}

func TestACodexConversationHasNoGoalCommand(t *testing.T) {
	r := newRig(t)
	r.rt.Import([]ImportTab{{ID: "tab-cx", Session: "s1", Title: "x", Provider: wire.ProviderCodex, Cwd: "/w", TranscriptPath: r.path}}, nil)
	if res := r.do(wire.GoalCmd{OpID: "g1", ID: "tab-cx", Text: "x"}); res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported {
		t.Fatalf("%+v", res)
	}
}

func TestAnImportedConversationStartsRead(t *testing.T) {
	r := newRig(t)
	r.open()
	cs, err := r.rt.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs.Conversations {
		if c.ID == r.id && c.SeenAt == 0 {
			t.Fatalf("an imported chat is unread: %+v", c)
		}
	}
}

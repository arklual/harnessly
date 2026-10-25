package wire_test

import (
	"os"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A chat's parent goes only to a page that reads it; another page gets the same event, seq for seq, without it.
func TestParentGoesOnlyToAPageThatReadsIt(t *testing.T) {
	g := newRig(t, 0)
	pages := map[string]*sock{"delegation": g.dialWith("delegation"), "": g.dialWith("voice")}
	seqs := map[string]int64{}
	for caps, s := range pages {
		s.hello()
		s.send(wire.SubFrame{Ch: wire.ChannelConvs})
		seqs[caps] = s.snapshot(wire.ChannelConvs).Seq
	}
	parent := g.cmd(wire.OpConversationCreate, wire.ConversationCreateCmd{OpID: "p", Cwd: "/tmp", Provider: wire.ProviderClaude}).ID
	res := g.cmd(wire.OpConversationCreate, wire.ConversationCreateCmd{OpID: "k", Cwd: "/tmp", Provider: wire.ProviderClaude, ParentID: *parent})
	kid := *res.ID
	for caps, s := range pages {
		var got *wire.Conversation
		evs := s.until(wire.ChannelConvs, seqs[caps], func(e wire.Event) bool {
			up, ok := e.(wire.ConversationUpsertEv)
			if ok && up.Conv.ID == kid {
				got = &up.Conv
			}
			return got != nil
		})
		seqs[caps] = evs[len(evs)-1].Seq
		if (got.ParentID != nil) != (caps == "delegation") || (got.ParentID != nil && *got.ParentID != *parent) {
			t.Fatalf("page with caps %q: parentId %v", caps, got.ParentID)
		}
	}
	if seqs["delegation"] != seqs[""] {
		t.Fatalf("both pages are at the same seq: %v", seqs)
	}
	// The snapshot of a page that comes later is cut the same way.
	for caps, want := range map[string]bool{"delegation": true, "": false} {
		s := g.dialWith(caps)
		s.hello()
		s.send(wire.SubFrame{Ch: wire.ChannelConvs})
		for _, c := range s.snapshot(wire.ChannelConvs).Convs.Conversations {
			if c.ID == kid && (c.ParentID != nil) != want {
				t.Fatalf("snapshot for caps %q: parentId %v", caps, c.ParentID)
			}
		}
	}
}

// fixtureEvent is the event of a recorded ev frame of testdata.
func fixtureEvent(t *testing.T, name string) wire.Event {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	f, err := wire.DecodeServerFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	return f.(wire.EvFrame).E
}

// A page built before workflows (one that reads delegation) never gets a field or value it would refuse: the workflow's upsert reaches it as the
// removal of an id it does not hold, with the same seq, and a conversation that waits for a workflow does not count it.
// A page that names the cap gets them whole, live and from the ring alike.
func TestWorkflowsGoOnlyToAPageThatReadsThem(t *testing.T) {
	g := newRig(t, time.Hour)
	old, now := g.dialWith("delegation"), g.dialWith("delegation,workflows")
	conv := wire.ConvChannel("tab-k3Jx9QmZ")
	for _, s := range []*sock{old, now} {
		s.hello()
		s.send(wire.SubFrame{Ch: conv})
		s.snapshot(conv)
		s.send(wire.SubFrame{Ch: wire.ChannelConvs})
		s.snapshot(wire.ChannelConvs)
	}
	upsert := fixtureEvent(t, "ev.background_upsert-workflow.json").(wire.BackgroundUpsertEv)
	waiting := fixtureEvent(t, "ev.conversation_upsert-waiting.json").(wire.ConversationUpsertEv)
	seq := g.hub.Publish(conv, upsert)
	g.hub.Publish(wire.ChannelConvs, waiting)

	if got := old.until(conv, seq-1, func(wire.Event) bool { return true })[0].E; got != (wire.BackgroundRemovedEv{ID: upsert.Task.ID}) {
		t.Fatalf("a page without the cap got %#v", got)
	}
	if got := now.until(conv, seq-1, func(wire.Event) bool { return true })[0].E.(wire.BackgroundUpsertEv); got.Task.Workflow == nil {
		t.Fatal("a page with the cap lost the run")
	}
	oldConv := old.until(wire.ChannelConvs, 0, func(wire.Event) bool { return true })[0].E.(wire.ConversationUpsertEv).Conv
	if oldConv.BgWait == nil || oldConv.BgWait.Workflows != 0 || oldConv.BgAgents != waiting.Conv.BgAgents-1 {
		t.Fatalf("a page without the cap: bgAgents %d, wait %+v", oldConv.BgAgents, oldConv.BgWait)
	}
	if c := now.until(wire.ChannelConvs, 0, func(wire.Event) bool { return true })[0].E.(wire.ConversationUpsertEv).Conv; c.BgWait.Workflows != 1 || c.BgAgents != waiting.Conv.BgAgents {
		t.Fatalf("a page with the cap: bgAgents %d, wait %+v", c.BgAgents, c.BgWait)
	}

	// Back from the ring, each page gets the event as it went to it.
	epoch, after := g.hub.Epoch(), seq-1
	for _, s := range []*sock{old, now} {
		s.send(wire.SubFrame{Ch: conv, Epoch: epoch, AfterSeq: &after})
	}
	if _, ok := old.until(conv, after, func(wire.Event) bool { return true })[0].E.(wire.BackgroundRemovedEv); !ok {
		t.Fatal("the ring replays the workflow to a page without the cap")
	}
	if _, ok := now.until(conv, after, func(wire.Event) bool { return true })[0].E.(wire.BackgroundUpsertEv); !ok {
		t.Fatal("the ring lost the workflow for a page with the cap")
	}
}

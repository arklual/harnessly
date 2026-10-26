package convo

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// twoTurns plays two finished turns of the rig's Claude and returns their prompts' lines.
func (r *rig) twoTurns() (string, string) {
	r.t.Helper()
	r.open()
	u1 := r.user("first question")
	r.say("first answer")
	r.turnEnd()
	u2 := r.user("second question")
	r.say("second answer")
	r.turnEnd()
	r.idle()
	r.settle(wire.StateReady)
	return u1, u2
}

func (r *rig) recordOf(id string) wire.Conversation {
	r.t.Helper()
	rec, ok := r.rt.Record(id)
	if !ok {
		r.t.Fatalf("no conversation %s", id)
	}
	return rec
}

// A Claude fork is a session of its own: the transcript up to the turn, copied under a new session id, which the agent
// resumes; the new chat shows that history and says where it came from, and the chat it came from is not touched.
func TestForkResumesACopyOfTheSessionCutAfterTheTurn(t *testing.T) {
	r := newRig(t)
	u1, u2 := r.twoTurns()
	before, _ := os.ReadFile(r.path)
	first := wire.ClaudeEntryID(u1, 0)
	res := r.do(wire.ConversationForkCmd{OpID: "f1", ID: r.id, AtTurn: &first})
	if res.R != wire.ResultAccepted || res.ID == nil || res.Ch != wire.ChannelConvs {
		t.Fatalf("fork: %+v", res)
	}
	got := r.launcher.launched[len(r.launcher.launched)-1]
	if !strings.HasSuffix(got.TranscriptPath, got.ResumeSessionID+".jsonl") || got.Fork || got.ID != *res.ID {
		t.Fatalf("launch: %+v", got)
	}
	raw, err := os.ReadFile(got.TranscriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), u1) || strings.Contains(string(raw), u2) {
		t.Fatalf("the copy is not cut after the first turn:\n%s", raw)
	}
	if after, _ := os.ReadFile(r.path); string(after) != string(before) {
		t.Fatal("the source transcript changed")
	}
	rec := r.recordOf(*res.ID)
	o := rec.Origin
	if o == nil || o.K != wire.OriginFork || o.FromID != r.id || o.FromTitle != "work" || o.Via != wire.ViaSession ||
		o.AtTurn == nil || *o.AtTurn != first || rec.Title != "work" || rec.Cwd != "/w" {
		t.Fatalf("record: %+v origin %+v", rec, o)
	}
	snap, err := r.rt.Conversation(context.Background(), *res.ID)
	if err != nil || len(snap.Turns) != 1 || snap.Turns[0].ID != first {
		t.Fatalf("the fork's history: %v %+v", err, snap.Turns)
	}
	if r.recordOf(r.id).Origin != nil {
		t.Fatal("the source got an origin")
	}

	latest := r.do(wire.ConversationForkCmd{OpID: "f2", ID: r.id})
	whole := r.launcher.launched[len(r.launcher.launched)-1]
	raw, _ = os.ReadFile(whole.TranscriptPath)
	if latest.R != wire.ResultAccepted || !strings.Contains(string(raw), u2) || *r.recordOf(*latest.ID).Origin.AtTurn != wire.ClaudeEntryID(u2, 0) {
		t.Fatalf("a fork at the last turn: %+v\n%s", latest, raw)
	}
}

// A turn still running is no place to fork; a fork without a turn then cuts before it.
func TestForkOfAWorkingAgentCutsBeforeTheLiveTurn(t *testing.T) {
	r := newRig(t)
	_, u2 := r.twoTurns()
	r.busy()
	u3 := r.user("third question")
	r.settle(wire.StateWorking)
	live := wire.ClaudeEntryID(u3, 0)
	if res := r.do(wire.ConversationForkCmd{OpID: "f1", ID: r.id, AtTurn: &live}); res.R != wire.ResultRefused || res.Code != wire.RefuseAgentWorking {
		t.Fatalf("a live turn: %+v", res)
	}
	res := r.do(wire.ConversationForkCmd{OpID: "f2", ID: r.id})
	if res.R != wire.ResultAccepted {
		t.Fatalf("fork: %+v", res)
	}
	raw, _ := os.ReadFile(r.launcher.launched[len(r.launcher.launched)-1].TranscriptPath)
	if !strings.Contains(string(raw), u2) || strings.Contains(string(raw), u3) {
		t.Fatalf("the copy holds the live turn:\n%s", raw)
	}
	unknown := wire.ClaudeEntryID("00000000-0000-4000-8000-999999999999", 0)
	if res := r.do(wire.ConversationForkCmd{OpID: "f3", ID: r.id, AtTurn: &unknown}); res.Code != wire.RefuseNotFound {
		t.Fatalf("an unknown turn: %+v", res)
	}
}

// A handoff opens a conversation on the other agent whose first message is the approved text in a block that says
// where it comes from; it goes with the launch.
func TestHandoffStartsTheOtherAgentWithTheContext(t *testing.T) {
	r := newRig(t)
	r.twoTurns()
	model := &wire.Model{ID: "gpt-5.5-codex"}
	res := r.do(wire.ConversationHandoffCmd{OpID: "h1", ID: r.id, Provider: wire.ProviderCodex, Model: model,
		Mode: wire.HandoffNote, Text: "## Goal\nShip it"})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("handoff: %+v", res)
	}
	got := r.launcher.launched[len(r.launcher.launched)-1]
	if got.Provider != wire.ProviderCodex || got.Model == nil || got.Model.ID != model.ID || got.ResumeSessionID != "" ||
		!strings.HasPrefix(got.First, `<handoff from="`+r.id+`" title="work" agent="claude"`) || !strings.Contains(got.First, "## Goal\nShip it\n</handoff>") {
		t.Fatalf("launch: %+v", got)
	}
	o := r.recordOf(*res.ID).Origin
	if o == nil || o.K != wire.OriginHandoff || o.Via != wire.ViaNote || o.FromProvider != wire.ProviderClaude {
		t.Fatalf("origin: %+v", o)
	}
}

// «Влить обратно»: the fork's result goes into the queue of the conversation it came from, wrapped with where it is
// from; a conversation that came from none has nothing to merge into.
func TestMergeQueuesTheResultInTheParent(t *testing.T) {
	r := newRig(t)
	r.twoTurns()
	fork := r.do(wire.ConversationForkCmd{OpID: "f1", ID: r.id})
	if fork.R != wire.ResultAccepted {
		t.Fatalf("fork: %+v", fork)
	}
	res := r.do(wire.ConversationMergeCmd{OpID: "m1", ID: *fork.ID, Text: "Done: the branch fixed it."})
	if res.R == wire.ResultRefused || res.Ch != wire.ConvChannel(r.id) || res.ID == nil || *res.ID != r.id {
		t.Fatalf("merge: %+v", res)
	}
	m := r.message("m1")
	if !strings.HasPrefix(m.Text, `<fork-result from="`+*fork.ID+`"`) || !strings.Contains(m.Text, "Done: the branch fixed it.\n</fork-result>") {
		t.Fatalf("message: %q", m.Text)
	}
	if res := r.do(wire.ConversationMergeCmd{OpID: "m2", ID: r.id, Text: "x"}); res.Code != wire.RefuseUnsupported {
		t.Fatalf("no origin: %+v", res)
	}
}

func TestExcerptDraftHoldsTheTurns(t *testing.T) {
	r := newRig(t)
	r.twoTurns()
	d, err := r.rt.Excerpt(r.id, wire.ProviderCodex, "", "")
	if err != nil || d.Mode != wire.DraftExcerpt || d.Turns != 2 || d.Tokens == 0 || d.Validate() != nil {
		t.Fatalf("draft: %+v %v", d, err)
	}
	if !strings.Contains(d.Text, "first question") || !strings.Contains(d.Text, "### Claude\nsecond answer") {
		t.Fatalf("text:\n%s", d.Text)
	}
}

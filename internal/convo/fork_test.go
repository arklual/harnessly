package convo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/handoff"
	"github.com/ly-solutions/harnessly/internal/outbox"
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

func handoffRig(t *testing.T) (*rig, *handoff.ArtifactStore) {
	t.Helper()
	r := newRig(t)
	store, err := handoff.NewArtifactStore(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	r.deps = func(d *Deps) { d.Handoffs = store }
	r.rt = r.runtime()
	return r, store
}

func claimedArtifact(t *testing.T, root string) []byte {
	t.Helper()
	var files []string
	err := filepath.Walk(filepath.Join(root, "v2", "handoffs", "claimed"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("claimed artifacts = %v", files)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func claimedArtifactPath(t *testing.T, root string) string {
	t.Helper()
	var files []string
	err := filepath.Walk(filepath.Join(root, "v2", "handoffs", "claimed"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("claimed artifacts = %v", files)
	}
	return files[0]
}

func finishCarriedFirst(t *testing.T, r *rig, target string) {
	t.Helper()
	if err := r.rt.call(context.Background(), target, func(c *conv, now time.Time) {
		turn := "x:1#0"
		c.change(c.box.PromptSubmitted(&turn, false, now), now)
		c.change(c.box.TurnStarted(turn, now), now)
		c.change(c.box.TurnEnded(turn, now), now)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryHandoffCarriesAShortBootstrapAndTheCompleteTranscript(t *testing.T) {
	r, _ := handoffRig(t)
	r.twoTurns()
	large := strings.Repeat("m", wire.HandoffTextMax+100)
	r.say(large)
	if f, err := os.OpenFile(r.path, os.O_APPEND|os.O_WRONLY, 0); err != nil {
		t.Fatal(err)
	} else {
		_, _ = f.WriteString(`{"partial":"still-writing"}`)
		_ = f.Close()
	}
	draft, err := r.rt.History(r.id)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Mode != wire.DraftHistory || draft.Text != "" || draft.Artifact == nil || draft.Artifact.Bytes <= 0 {
		t.Fatalf("draft: %+v", draft)
	}
	res := r.do(wire.ConversationHandoffCmd{OpID: "history-1", ID: r.id, Provider: wire.ProviderCodex,
		Mode: wire.HandoffHistory, ArtifactID: draft.Artifact.ID})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("handoff: %+v", res)
	}
	launch := r.launcher.launched[len(r.launcher.launched)-1]
	if launch.First == "" || len(launch.First) >= outbox.MaxCarriedText || !strings.Contains(launch.First, `mode="history"`) ||
		strings.Contains(launch.First, strings.Repeat("m", 1000)) {
		t.Fatalf("first prompt (%d): %q", len(launch.First), launch.First)
	}
	if got := claimedArtifact(t, r.dir); !bytes.Contains(got, []byte(large)) || bytes.Contains(got, []byte("still-writing")) ||
		bytes.Contains(got, []byte(`"sessionId"`)) || bytes.Contains(got, []byte(`"model"`)) {
		t.Fatalf("canonical claimed transcript is wrong (%d bytes)", len(got))
	}
	if o := r.recordOf(*res.ID).Origin; o == nil || o.Via != wire.ViaHistory {
		t.Fatalf("origin: %+v", o)
	}
}

func TestClaimedHistoryIsReleasedOnlyAfterItsFirstTurnFinishes(t *testing.T) {
	r, _ := handoffRig(t)
	r.twoTurns()
	draft, err := r.rt.History(r.id)
	if err != nil {
		t.Fatal(err)
	}
	res := r.do(wire.ConversationHandoffCmd{OpID: "history-cleanup", ID: r.id, Provider: wire.ProviderCodex,
		Mode: wire.HandoffHistory, ArtifactID: draft.Artifact.ID})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("handoff: %+v", res)
	}
	path := claimedArtifactPath(t, r.dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("artifact disappeared before the first turn: %v", err)
	}
	finishCarriedFirst(t, r, *res.ID)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact remains after the first turn: %v", err)
	}
}

func TestLongEditableHandoffsUseAnArtifactBootstrap(t *testing.T) {
	for _, mode := range []wire.HandoffMode{wire.HandoffExcerpt, wire.HandoffNote} {
		t.Run(string(mode), func(t *testing.T) {
			r, _ := handoffRig(t)
			r.twoTurns()
			text := strings.Repeat("approved context ", 1800)
			res := r.do(wire.ConversationHandoffCmd{OpID: "long-1", ID: r.id, Provider: wire.ProviderCodex,
				Mode: mode, Text: text})
			if res.R != wire.ResultAccepted {
				t.Fatalf("handoff: %+v", res)
			}
			first := r.launcher.launched[len(r.launcher.launched)-1].First
			if first == "" || len(first) >= outbox.MaxCarriedText || strings.Contains(first, text[:1000]) {
				t.Fatalf("first prompt (%d): %q", len(first), first)
			}
			if got := string(claimedArtifact(t, r.dir)); got != text {
				t.Fatalf("artifact length = %d, want %d", len(got), len(text))
			}
		})
	}
}

func TestRequiredFirstPromptRefusesCreationInsteadOfQueueing(t *testing.T) {
	r := newRig(t)
	now := r.clock.Now()
	rec := wire.Conversation{ID: "tab-target_456", MachineID: "m1", Cwd: "/w", Provider: wire.ProviderClaude,
		CreatedAt: ms(now), LastActivityAt: ms(now), LastPromptAt: ms(now), SeenAt: ms(now),
		State: wire.AgentState{K: wire.StateUnknown, Since: ms(now)}, Rev: 1}
	first := &wire.FirstMessage{ClientID: "first", Text: strings.Repeat("x", outbox.MaxCarriedText+1), AttachmentIDs: []string{}}
	res := r.rt.openConv(rec, LaunchRequest{Cwd: "/w", Provider: wire.ProviderClaude}, first, true, "")
	if res.R != wire.ResultRefused || len(r.launcher.launched) != 0 {
		t.Fatalf("result %+v; launches %+v", res, r.launcher.launched)
	}
}

func TestFailedHandoffLaunchKeepsBootstrapFirstOnRetry(t *testing.T) {
	r, _ := handoffRig(t)
	r.twoTurns()
	r.launcher.err = errors.New("launcher down")
	text := strings.Repeat("approved context ", 1800)
	res := r.do(wire.ConversationHandoffCmd{OpID: "retry-handoff", ID: r.id, Provider: wire.ProviderCodex,
		Mode: wire.HandoffNote, Text: text})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("handoff: %+v", res)
	}
	r.id = *res.ID
	first := r.launcher.launched[len(r.launcher.launched)-1].First
	if message := r.message("retry-handoff"); message.Mode != wire.ModeQueue {
		t.Fatalf("the carried first prompt is exposed as %q, want an ordinary queued prompt", message.Mode)
	}
	path := claimedArtifactPath(t, r.dir)
	if got := string(claimedArtifact(t, r.dir)); got != text {
		t.Fatalf("claimed artifact lost after launch failure: %d bytes", len(got))
	}
	r.launcher.err = nil
	if sent := r.send("urgent", "new message", wire.ModeSteer); sent.R == wire.ResultRefused {
		t.Fatalf("send: %+v", sent)
	}
	if len(r.launcher.launched) != 2 || r.launcher.launched[1].First != first {
		t.Fatalf("retry launches: %+v", r.launcher.launched)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("retry lost its artifact: %v", err)
	}
	finishCarriedFirst(t, r, r.id)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retried artifact remains after success: %v", err)
	}
}

func TestFailedHandoffKeepsArtifactAndFirstPromptAcrossRuntimeRestart(t *testing.T) {
	r, _ := handoffRig(t)
	r.twoTurns()
	r.launcher.err = errors.New("launcher down")
	text := strings.Repeat("restart context ", 1800)
	res := r.do(wire.ConversationHandoffCmd{OpID: "restart-handoff", ID: r.id, Provider: wire.ProviderCodex,
		Mode: wire.HandoffNote, Text: text})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("handoff: %+v", res)
	}
	target := *res.ID
	first := r.launcher.launched[len(r.launcher.launched)-1].First
	path := claimedArtifactPath(t, r.dir)
	launches := len(r.launcher.launched)
	r.rt.Stop()
	r.launcher.err = nil
	r.restart()
	r.id = target
	if res := r.do(wire.QueueResumeCmd{OpID: "resume-handoff", ConversationID: target}); res.R != wire.ResultAccepted {
		t.Fatalf("resume after restart: %+v", res)
	}
	if got := len(r.launcher.launched); got != launches+1 {
		t.Fatalf("launches after restart retry = %d, want %d", got, launches+1)
	}
	if got := r.launcher.launched[len(r.launcher.launched)-1].First; got != first {
		t.Fatalf("first prompt changed across restart:\n%s\nwant:\n%s", got, first)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("restart lost the artifact before retry: %v", err)
	}
	if err := r.rt.call(context.Background(), target, func(c *conv, _ time.Time) {
		if c.handoffArtifact != path || c.handoffArtifactClientID != "restart-handoff" {
			t.Fatalf("restart lost artifact ownership: path=%q client=%q", c.handoffArtifact, c.handoffArtifactClientID)
		}
	}); err != nil {
		t.Fatal(err)
	}
	finishCarriedFirst(t, r, target)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restarted artifact remains after success: %v", err)
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

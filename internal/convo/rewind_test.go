package convo

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Rewinds on the rig: a Claude transcript named by its session, a fake terminal and a fake keeper of the folder's
// snapshots.

const rewindSession = "6f1c2a90-5b3e-4c1d-9e8f-7a6b5c4d3e2f"

type fakeRewinder struct {
	bound     []string // session ids, in order
	snaps     []Snapshot
	restored  []string
	changes   []wire.RewindChange
	failBind  bool
	snapAsked [][2]wire.Ms
}

func (f *fakeRewinder) Bind(_ wire.Conversation, sessionID, transcript string) error {
	if f.failBind {
		return os.ErrPermission
	}
	f.bound = append(f.bound, sessionID+" "+filepath.Base(transcript))
	return nil
}

func (f *fakeRewinder) Snapshot(_ wire.Conversation, from, to wire.Ms) (Snapshot, bool) {
	f.snapAsked = append(f.snapAsked, [2]wire.Ms{from, to})
	for i := len(f.snaps) - 1; i >= 0; i-- {
		if s := f.snaps[i]; s.At >= from && s.At <= to {
			return s, true
		}
	}
	return Snapshot{}, false
}

func (f *fakeRewinder) Changes(Snapshot) ([]wire.RewindChange, int, error) { return f.changes, 0, nil }

func (f *fakeRewinder) Restore(s Snapshot) (Snapshot, error) {
	f.restored = append(f.restored, s.ID)
	return Snapshot{Tab: s.Tab, Cwd: s.Cwd, ID: "before-" + s.ID, At: s.At + 1}, nil
}

type rewindRig struct {
	*reapRig
	rw *fakeRewinder
}

func newRewindRig(t *testing.T) *rewindRig {
	t.Helper()
	r := &rewindRig{reapRig: &reapRig{rig: newRig(t), terms: &fakeTerms{owned: true, tree: idleTree()}, log: &fakeLog{}},
		rw: &fakeRewinder{}}
	r.path = filepath.Join(r.dir, rewindSession+".jsonl")
	if err := os.WriteFile(r.path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.terms.onStop = func() { r.machine.probes["s1"] = Probe{} }
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Log: r.log, Terminals: r.terms,
		Rewinder: r.rw})
	r.open()
	return r
}

// turn writes one finished turn and returns its id.
func (r *rewindRig) turn(prompt, answer string) string {
	id := r.user(prompt)
	r.tick(time.Second)
	r.say(answer)
	r.turnEnd()
	r.tick(time.Second)
	return "c:" + id + "#0"
}

func (r *rewindRig) rewind(entry, text string, files bool) wire.CmdResult {
	r.t.Helper()
	return r.do(wire.RewindCmd{OpID: "rw-" + text, ID: r.id, EntryID: entry, Text: text, Files: files})
}

func turnIDs(s wire.ConvSnapshot) []string {
	var out []string
	for _, t := range s.Turns {
		out = append(out, t.ID)
	}
	return out
}

func TestRewindGoesOnFromBeforeTheMessage(t *testing.T) {
	r := newRewindRig(t)
	first := r.turn("make a migration", "done: migrate.go")
	second := r.turn("now the tests", "tests added")
	r.turn("and the docs", "docs added")
	r.settle(wire.StateReady)
	head, _ := os.ReadFile(r.path)

	res := r.rewind(second, "now the tests, but table-driven", false)
	if res.R != wire.ResultQueued {
		t.Fatalf("rewind = %+v", res)
	}
	if !slices.Equal(r.terms.stopped, []string{"s1"}) {
		t.Fatalf("stopped %v: the agent must not run on while its transcript is forked", r.terms.stopped)
	}
	rec := r.record()
	if rec.TranscriptPath == r.path || rec.AgentSessionID == rewindSession || len(rec.AgentSessionID) != 36 {
		t.Fatalf("the conversation stayed on its transcript: %s %s", rec.TranscriptPath, rec.AgentSessionID)
	}
	if len(r.rw.bound) != 1 || !strings.HasPrefix(r.rw.bound[0], rec.AgentSessionID+" ") {
		t.Fatalf("bound %v, want the new session", r.rw.bound)
	}
	forked, _ := os.ReadFile(rec.TranscriptPath)
	if !strings.Contains(string(forked), "make a migration") || strings.Contains(string(forked), "now the tests") {
		t.Fatalf("fork holds %q", forked)
	}
	if now, _ := os.ReadFile(r.path); string(now) != string(head) {
		t.Fatal("the old transcript changed")
	}
	want := wire.RewindMark{At: ms(r.clock.Now()), AfterTurnID: &first, Turns: 2, UndoUntil: ms(r.clock.Now()) + wire.RewindUndoMs}
	if len(rec.Rewinds) != 1 || !sameMark(rec.Rewinds[0], want) {
		t.Fatalf("rewinds %+v, want %+v", rec.Rewinds, want)
	}
	resets := r.events(wire.EvFeedReset)
	if len(resets) != 1 {
		t.Fatalf("%d feed_reset events", len(resets))
	}
	if got := resets[0].e.(wire.FeedResetEv).Turns; len(got) != 1 || got[0].ID != first {
		t.Fatalf("feed_reset turns %v, want only the first", got)
	}
	if ids := turnIDs(r.snap()); !slices.Equal(ids, []string{first}) {
		t.Fatalf("snapshot turns %v", ids)
	}
	// The edited message is in the queue, and the queue brings the agent back on the new session.
	if m := r.message("rw-now the tests, but table-driven"); m.Text != "now the tests, but table-driven" {
		t.Fatalf("queued %+v", m)
	}
	if !slices.Contains(r.launcher.resumed, r.id) {
		t.Fatalf("resumed %v", r.launcher.resumed)
	}
	if held := r.rt.AttachedSessions(); held[rewindSession] != r.id {
		t.Fatalf("the session left behind is not held by the chat: %v", held)
	}
	// What was left behind stays readable, from the message on.
	page, err := r.rt.Rewound(context.Background(), r.id, 0)
	if err != nil || len(page.Turns) != 2 || page.Turns[0].ID != second {
		t.Fatalf("rewound = %+v, %v", page.Turns, err)
	}
}

func sameMark(a, b wire.RewindMark) bool {
	return a.At == b.At && a.Turns == b.Turns && a.Files == b.Files && a.UndoUntil == b.UndoUntil &&
		(a.AfterTurnID == nil) == (b.AfterTurnID == nil) && (a.AfterTurnID == nil || *a.AfterTurnID == *b.AfterTurnID)
}

func TestRewindIsRefusedWhileTheAgentWorks(t *testing.T) {
	r := newRewindRig(t)
	r.turn("one", "1")
	second := r.turn("two", "2")
	r.settle(wire.StateReady)
	r.busy()
	r.user("three")
	r.settle(wire.StateWorking)
	if res := r.rewind(second, "two again", false); res.R != wire.ResultRefused || res.Code != wire.RefuseAgentWorking {
		t.Fatalf("rewind of a working agent = %+v", res)
	}
	if len(r.terms.stopped) != 0 || len(r.rw.bound) != 0 {
		t.Fatalf("touched: stopped %v, bound %v", r.terms.stopped, r.rw.bound)
	}
}

func TestRewindToTheFirstMessageIsRefused(t *testing.T) {
	r := newRewindRig(t)
	first := r.turn("one", "1")
	r.turn("two", "2")
	r.settle(wire.StateReady)
	// Nothing would be left before it: an agent cannot resume a session without a message.
	if res := r.rewind(first, "one again", false); res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported {
		t.Fatalf("rewind = %+v", res)
	}
	if res := r.rewind("c:00000000-0000-4000-8000-999999999999#0", "x", false); res.Code != wire.RefuseNotFound {
		t.Fatalf("unknown message = %+v", res)
	}
}

func TestRewindWithFilesAndItsUndo(t *testing.T) {
	r := newRewindRig(t)
	r.turn("one", "1")
	endOfFirst := ms(r.clock.Now())
	r.rw.snaps = []Snapshot{{Tab: r.id, Cwd: "/w", ID: "100-turn", At: endOfFirst}}
	r.rw.changes = []wire.RewindChange{{Path: "migrate.go", Change: wire.FileRemoved}}
	r.tick(5 * time.Second)
	second := r.turn("two", "2")
	r.settle(wire.StateReady)

	plan, err := r.rt.Plan(context.Background(), r.id, second)
	if err != nil || plan.Turns != 1 || plan.Files == nil || plan.Files.At != endOfFirst || len(plan.Files.Changes) != 1 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if res := r.rewind(second, "two, differently", true); res.R != wire.ResultQueued {
		t.Fatalf("rewind = %+v", res)
	}
	if !slices.Equal(r.rw.restored, []string{"100-turn"}) {
		t.Fatalf("restored %v", r.rw.restored)
	}
	rec := r.record()
	if len(rec.Rewinds) != 1 || !rec.Rewinds[0].Files {
		t.Fatalf("rewinds %+v", rec.Rewinds)
	}

	r.tick(3 * time.Second)
	if res := r.do(wire.RewindUndoCmd{OpID: "undo-1", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("undo = %+v", res)
	}
	if !slices.Equal(r.rw.restored, []string{"100-turn", "before-100-turn"}) {
		t.Fatalf("restored %v: the undo returns the folder as the rewind found it", r.rw.restored)
	}
	rec = r.record()
	if rec.TranscriptPath != r.path || rec.AgentSessionID != rewindSession || len(rec.Rewinds) != 0 {
		t.Fatalf("after undo: %s %s %+v", rec.TranscriptPath, rec.AgentSessionID, rec.Rewinds)
	}
	if last := r.rw.bound[len(r.rw.bound)-1]; last != rewindSession+" "+filepath.Base(r.path) {
		t.Fatalf("bound back to %q", last)
	}
	if k := r.message("rw-two, differently").State.K; k != wire.MsgCancelled {
		t.Fatalf("the rewind's message is %s, want cancelled", k)
	}
	if ids := turnIDs(r.snap()); len(ids) != 2 || ids[1] != second {
		t.Fatalf("snapshot after undo %v", ids)
	}
}

func TestRewindUndoComesTooLate(t *testing.T) {
	r := newRewindRig(t)
	r.turn("one", "1")
	second := r.turn("two", "2")
	r.settle(wire.StateReady)
	if res := r.rewind(second, "two again", false); res.R != wire.ResultQueued {
		t.Fatalf("rewind = %+v", res)
	}
	r.clock.Sleep(wire.RewindUndoMs*time.Millisecond + time.Second)
	if res := r.do(wire.RewindUndoCmd{OpID: "undo-late", ID: r.id}); res.Code != wire.RefuseTooLate {
		t.Fatalf("late undo = %+v", res)
	}
	if res := r.rewind(second, "with files", true); res.R != wire.ResultRefused {
		t.Fatalf("a rewind with no snapshot of the folder = %+v", res)
	}
}

func TestRewindsSurviveARestart(t *testing.T) {
	r := newRewindRig(t)
	r.turn("one", "1")
	second := r.turn("two", "2")
	r.settle(wire.StateReady)
	if res := r.rewind(second, "two again", false); res.R != wire.ResultQueued {
		t.Fatalf("rewind = %+v", res)
	}
	moved := r.record().TranscriptPath
	r.rt.Stop()
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Log: r.log, Terminals: r.terms,
		Rewinder: r.rw})
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := r.record()
	if rec.TranscriptPath != moved || len(rec.Rewinds) != 1 {
		t.Fatalf("after a restart: %s %+v", rec.TranscriptPath, rec.Rewinds)
	}
	if page, err := r.rt.Rewound(context.Background(), r.id, 0); err != nil || len(page.Turns) != 1 {
		t.Fatalf("rewound after a restart = %+v, %v", page, err)
	}
}

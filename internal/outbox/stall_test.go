package outbox

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// lines counts how many times a text was typed into the pane.
func (r *rig) lines(text string) int {
	n := 0
	for _, c := range r.typed() {
		if c == "line:"+text {
			n++
		}
	}
	return n
}

// A typed message the agent does not take within 30 s is not left waiting for ever: it goes back to the head of the
// queue, stalled, its text is taken out of the agent's input box, and it waits for the person — it is not typed again
// by itself and the messages behind it wait too. A retry types it once more.
func TestAHungMessageGoesBackToTheQueueAndTheBoxIsCleared(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	r.pane.onSend = func(call string) {
		if call == "keys:Enter" || call == "keys:C-u" || strings.HasPrefix(call, "line:") {
			r.pane.screen = idle
		}
	}
	r.size = 100
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.send("m2", "и ещё одно", wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput || st.Wait.DeadlineAt-r.clock.Now().UnixMilli() != wire.ConfirmDeadline {
		t.Fatalf("typed: %+v", st)
	}
	r.advance(wire.ConfirmDeadline*time.Millisecond - time.Second)
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("stalled early: %s", r.trail("m1"))
	}
	// The terminal draws the text back in the box (its Enter was lost): the stall takes it out.
	r.pane.screen = withInput(t, "проверь логи")
	r.advance(time.Second)
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Reason != wire.QueuedStalled || st.Position != 1 || st.Wait != nil {
		t.Fatalf("not stalled: %+v (%s)", st, r.trail("m1"))
	}
	if q := r.box.Queue(); !slices.Equal(q.Items, []string{"m1", "m2"}) || !slices.Equal(q.Stalled, []string{"m1"}) {
		t.Fatalf("queue: %+v", q)
	}
	if !slices.Contains(r.typed(), "keys:C-u") || r.box.NeedsLook(r.clock.Now()) {
		t.Fatalf("box not cleared: %v", r.typed())
	}
	for range 10 {
		r.advance(time.Minute)
	}
	if r.lines("проверь логи") != 1 || r.lines("и ещё одно") != 0 || r.state("m1").Reason != wire.QueuedStalled {
		t.Fatalf("typed while stalled: %v %s", r.typed(), r.trail("m1"))
	}
	if st := r.state("m2"); st.K != wire.MsgQueued || st.Position != 2 || st.Reason == wire.QueuedStalled {
		t.Fatalf("m2: %+v", st)
	}

	r.record(must(r.box.Retry("m1", nil, r.clock.Now())))
	r.pump()
	if r.lines("проверь логи") != 2 || r.state("m1").K != wire.MsgInInput {
		t.Fatalf("retry: %v %s", r.typed(), r.trail("m1"))
	}
	r.record(r.box.PromptSubmitted(nil, false, r.clock.Now()))
	r.pump()
	if r.state("m1").K != wire.MsgAccepted || r.lines("и ещё одно") != 1 {
		t.Fatalf("after: %s / %v", r.trail("m1"), r.typed())
	}
	if got := r.trail("m1"); got != "typing → in-input → queued{stalled} → queued{behind} → typing → in-input → accepted" {
		t.Fatal(got)
	}
	if m, _ := r.box.Message("m1"); m.Attempts != 1 {
		t.Fatalf("attempts %d", m.Attempts)
	}
}

// Its entry, read late, takes a stalled message out of the line: the agent had it, it is not typed again.
func TestAStalledMessageTheAgentTookAfterAllIsAccepted(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.size = 100
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.send("m2", "и ещё одно", wire.ModeQueue)
	r.pump()
	r.advance(wire.ConfirmDeadline * time.Millisecond)
	if r.state("m1").Reason != wire.QueuedStalled {
		t.Fatal(r.trail("m1"))
	}
	id, ch := r.box.UserEntryText("c:aa#0", "проверь логи", 150, nil, r.clock.Now())
	r.record(ch)
	if id != "m1" || r.state("m1").K != wire.MsgAccepted || slices.Contains(r.box.Queue().Items, "m1") {
		t.Fatalf("%s %+v %v", id, r.state("m1"), r.box.Queue().Items)
	}
	if st := r.state("m2"); st.Position != 1 || st.Reason == wire.QueuedStalled {
		t.Fatalf("m2: %+v", st)
	}
	if got := r.box.Status([]string{"m1"})[0]; got.Handed == nil || got.Handed.Where != wire.HandedTranscript || *got.Handed.EntryID != "c:aa#0" {
		t.Fatalf("status: %+v", got)
	}
}

// A steered message waits in Claude's own queue for minutes: that is not a stall.
func TestASteeredMessageDoesNotStall(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "ещё учти тесты", wire.ModeSteer)
	r.pump()
	r.advance(5 * time.Minute)
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("%s", r.trail("m1"))
	}
	if got := r.box.Status([]string{"m1"})[0]; got.Handed == nil || got.Handed.Where != wire.HandedAgentQueue {
		t.Fatalf("status: %+v", got.Handed)
	}
}

// The status of a client id: known with where it went, unknown, or expired once its record was let go — and a page
// that sends an expired id again is told too-late, it is not typed a second time. The same id sent again while the
// record is kept gives the record back.
func TestTheStatusOfAMessage(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.size = 100
	r.send("m1", "первое", wire.ModeQueue)
	r.pump()
	got := r.box.Status([]string{"m1", "nope"})
	if got[0].K != wire.StatusKnown || got[0].Message.State.K != wire.MsgInInput || got[0].Handed.Where != wire.HandedInputBox ||
		got[0].Handed.At != r.clock.Now().UnixMilli() {
		t.Fatalf("in-input: %+v %+v", got[0], got[0].Handed)
	}
	if got[1].K != wire.StatusUnknown || got[1].Message != nil {
		t.Fatalf("unknown: %+v", got[1])
	}
	_, ch := r.box.UserEntry("c:aa#0", 120, nil, r.clock.Now())
	r.record(ch)
	r.record(r.box.TurnStarted("c:aa#0", r.clock.Now()))
	r.record(r.box.TurnEnded("c:aa#0", r.clock.Now()))
	ended := r.clock.Now().UnixMilli()
	if got := r.box.Status([]string{"m1"})[0]; got.Message.State.K != wire.MsgDone || got.Handed.Where != wire.HandedTranscript {
		t.Fatalf("done: %+v", got)
	}
	again, code := r.box.Send(Draft{ClientID: "m1", Text: "первое", Mode: wire.ModeQueue, Kind: wire.KindPrompt}, r.agent.State, r.clock.Now())
	if code != "" || len(again.Events) != 1 || again.Events[0].(wire.MessageUpsertEv).Message.State.K != wire.MsgDone {
		t.Fatalf("send again: %q %+v", code, again)
	}
	r.advance(FinalStored + time.Minute)
	if got := r.box.Status([]string{"m1"})[0]; got.K != wire.StatusExpired || got.At != ended {
		t.Fatalf("expired: %+v", got)
	}
	for _, s := range r.box.Status([]string{"m1", "nope"}) {
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if code := r.send("m1", "первое", wire.ModeQueue); code != wire.RefuseTooLate || r.lines("первое") != 1 {
		t.Fatalf("expired sent again: %q %v", code, r.typed())
	}

	// A restart keeps the ids let go.
	raw, err := json.Marshal(r.box.State())
	if err != nil {
		t.Fatal(err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	back := New("tab-1", wire.DefaultLimits, nil)
	back.Restore(st)
	if got := back.Status([]string{"m1"})[0]; got.K != wire.StatusExpired {
		t.Fatalf("restored: %+v", got)
	}
	r.advance(GoneKept)
	if got := r.box.Status([]string{"m1"})[0]; got.K != wire.StatusUnknown {
		t.Fatalf("past GoneKept: %+v", got)
	}
}

// A restart keeps a stalled message stalled at the head of the line.
func TestAStallOutlivesARestart(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.pump()
	r.advance(wire.ConfirmDeadline * time.Millisecond)
	raw, err := json.Marshal(r.box.State())
	if err != nil {
		t.Fatal(err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	back := New("tab-1", wire.DefaultLimits, nil)
	back.Restore(st)
	r.box = back
	r.record(r.box.Restarted(nil, nil, r.clock.Now()))
	r.advance(time.Minute)
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Reason != wire.QueuedStalled || r.lines("проверь логи") != 1 {
		t.Fatalf("%+v %v", st, r.typed())
	}
}

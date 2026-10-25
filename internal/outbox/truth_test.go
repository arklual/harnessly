package outbox

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A recorded case, tab-dBSEBV: the transcript, not a timeout, says whether the agent has a message.

// labText is the message of the claude-2.1.310-input-long-* captures.
func labText() string {
	rs := []rune(strings.Repeat("Это проверочный текст для замера поля ввода, в нём нет просьб и задач. ", 12))[:660]
	return strings.TrimRight(string(rs), " ") + " Ответь одним словом: ок."
}

// transcript is a fake one: user entries with their text at their offsets.
type transcript struct {
	entries []struct {
		id, text string
		off      int64
	}
}

func (tr *transcript) add(id, text string, off int64) {
	tr.entries = append(tr.entries, struct {
		id, text string
		off      int64
	}{id, text, off})
}

func (tr *transcript) find(offset int64, _ wire.Ms, text string) (string, bool) {
	for _, e := range tr.entries {
		if e.off >= offset && screen.SameMessage(e.text, text) {
			return e.id, true
		}
	}
	return "", false
}

// The prod sequence: a long line typed, its Enter taken as a newline, the box clipped to its tail. The typist sees its
// text still in the box and presses Enter again: it goes in on the first try, typed once.
func TestASwallowedEnterIsPressedAgainNotLost(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	clipped := tui(t, "claude-2.1.310-input-long-clipped-enter-swallowed.ansi.txt")
	r := newRig(t, idle, wire.StateIdle)
	enters := 0
	r.pane.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "line:"):
			r.pane.screen = clipped
		case call == "keys:Enter":
			if enters++; enters == 2 {
				r.pane.screen = idle
			}
		}
	}
	r.size = 100
	r.send("m1", labText(), wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("%+v", st)
	}
	if got := r.typed(); len(got) != 3 || got[1] != "keys:Enter" || got[2] != "keys:Enter" {
		t.Fatalf("%v", got)
	}
}

// A message whose try seemed lost (unconfirmed) and went back to the line: its text reaches the agent anyway (an Enter
// from the terminal, a late one). Its entry, read by its text, takes it out of the line — it is never typed again.
func TestAMessageTheAgentTookAfterAllIsNeverTypedTwice(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	tr := &transcript{}
	r.box.Transcript = tr.find
	r.pane.onSend = func(call string) { r.pane.screen = "" } // the pane cannot be read after the keys: no proof either way
	r.size = 1000
	r.send("m1", "Вот. Потом следующую задачу, которую хочется, чтобы ты тоже сделал", wire.ModeQueue)
	r.send("m2", "Ещё момент", wire.ModeQueue)
	r.pump()
	r.clock.Sleep(time.Duration(wire.ConfirmDeadline+1) * time.Millisecond)
	r.record(r.box.Tick(r.clock.Now()))
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Reason != wire.QueuedStalled {
		t.Fatalf("%+v", st)
	}
	// The retry finds no entry yet: back to the head of the line; the agent is busy meanwhile.
	ch, code := r.box.Retry("m1", nil, r.clock.Now())
	r.record(ch)
	r.agent.State = wire.AgentState{K: wire.StateWorking, Since: 2}
	r.pane.screen = tui(t, "claude-busy-spinner.ansi.txt")
	r.pump()
	if code != "" || r.state("m1").K != wire.MsgQueued || r.state("m1").Reason != wire.QueuedAgentBusy {
		t.Fatalf("%s %+v", code, r.state("m1"))
	}
	// Someone else's prompt does not take it; its own text does.
	if id, _ := r.box.UserEntryText("c:ff#0", "посмотри логи", 1200, nil, r.clock.Now()); id != "" {
		t.Fatal(id)
	}
	id, ch := r.box.UserEntryText("c:aa#0", "Вот. Потом следующую задачу, которую хочется, чтобы ты тоже сделал", 1500, nil, r.clock.Now())
	r.record(ch)
	if id != "m1" || r.state("m1").K != wire.MsgAccepted || slices.Contains(r.box.Queue().Items, "m1") {
		t.Fatalf("%s %+v %v", id, r.state("m1"), r.box.Queue().Items)
	}
	r.record(r.box.TurnStarted("c:aa#0", r.clock.Now()))
	if st := r.state("m2"); st.K != wire.MsgQueued || st.Position != 1 {
		t.Fatalf("%+v", st)
	}
	lines := 0
	for _, c := range r.typed() {
		if strings.HasPrefix(c, "line:Вот.") {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("typed %d times: %v", lines, r.typed())
	}
}

// Before a message typed once is typed again, the transcript is asked: an entry with its text there is the answer.
func TestATypedMessageIsLookedForBeforeItIsTypedAgain(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	tr := &transcript{}
	r.box.Transcript = tr.find
	r.pane.onSend = func(call string) { r.pane.screen = "" }
	r.size = 1000
	r.send("m1", "a message that went in on a late Enter", wire.ModeQueue)
	r.pump()
	r.clock.Sleep(time.Duration(wire.ConfirmDeadline+1) * time.Millisecond)
	r.record(r.box.Tick(r.clock.Now()))
	r.record(must(r.box.Retry("m1", nil, r.clock.Now())))
	tr.add("c:cc#0", "a message that went in on a late Enter", 1100)
	r.pane.screen = tui(t, "claude-idle-placeholder.ansi.txt")
	jobs := r.jobs
	r.pump()
	if r.jobs != jobs || r.state("m1").K != wire.MsgAccepted || *r.state("m1").EntryID != "c:cc#0" {
		t.Fatalf("jobs %d→%d %+v", jobs, r.jobs, r.state("m1"))
	}
	if got := r.trail("m1"); got != "typing → in-input → queued{stalled} → queued{behind} → accepted" {
		t.Fatal(got)
	}
}

// A retry asks the transcript by the text: someone else's entry past the offset is not the message's.
func TestARetryDoesNotTakeSomeoneElsesEntry(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	tr := &transcript{}
	r.box.Transcript = tr.find
	r.pane.onSend = func(call string) { r.pane.screen = "" }
	r.size = 1000
	r.send("m1", "my message", wire.ModeQueue)
	r.pump()
	r.clock.Sleep(time.Duration(wire.ConfirmDeadline+1) * time.Millisecond)
	r.record(r.box.Tick(r.clock.Now()))
	tr.add("c:dd#0", "typed in the terminal by hand", 1100)
	blind := func(int64, wire.Ms) (string, bool) { return "c:dd#0", true }
	r.record(must(r.box.Retry("m1", blind, r.clock.Now())))
	if st := r.state("m1"); st.K != wire.MsgQueued {
		t.Fatalf("%+v", st)
	}
	tr.add("c:ee#0", "my message", 1200)
	r.record(must(r.box.Retry("m1", blind, r.clock.Now()))) // not failed any more: refused, nothing changes
	if st := r.state("m1"); st.K != wire.MsgQueued {
		t.Fatalf("%+v", st)
	}
}

func must(ch Change, _ wire.RefuseCode) Change { return ch }

// Two messages in Claude's own queue: the entry that comes is the one with its text, whatever the order they went in.
func TestASteeredEntryIsMatchedByItsText(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "first, now", wire.ModeSteer)
	r.send("m2", "second, now", wire.ModeSteer)
	r.pump()
	r.pump()
	if r.state("m1").K != wire.MsgInInput || r.state("m2").K != wire.MsgInInput {
		t.Fatalf("%+v %+v", r.state("m1"), r.state("m2"))
	}
	id, ch := r.box.UserEntryText("c:bb#0", "second, now", 10, turn("c:aa#0"), r.clock.Now())
	r.record(ch)
	if id != "m2" || r.state("m2").K != wire.MsgInTurn || r.state("m1").K != wire.MsgInInput {
		t.Fatalf("%s %s / %s", id, r.trail("m1"), r.trail("m2"))
	}
}

// a normal message typed just as the agent began a turn of its own (a background task's notice) reaches Claude's
// queue, and the hook that fires at the Enter finds the agent already working. It is not accepted: it is in the agent's
// queue, shown as a steered message is (mode steer, in-input, the long term), until the transcript delivers it.
func TestAMessageTypedAsTheAgentBeganItsOwnTurnIsInTheAgentsQueue(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("m1", "look at the build", wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("typed: %+v", st)
	}
	r.record(r.box.PromptSubmitted(turn("c:aa#0"), true, r.clock.Now()))
	st := r.state("m1")
	if st.K != wire.MsgInInput || st.Wait == nil || st.Wait.DeadlineAt-st.Wait.SlowAt < SteerDeadline.Milliseconds()/2 {
		t.Fatalf("the hook accepted a message that is only in Claude's queue: %+v", st)
	}
	m, _ := r.box.Message("m1")
	if m.Mode != wire.ModeSteer {
		t.Fatalf("the pages are not told it was handed over: %s", m.Mode)
	}
	var told *wire.Message
	for _, e := range r.events {
		if up, ok := e.(wire.MessageUpsertEv); ok && up.Message.ClientID == "m1" && up.Message.Mode == wire.ModeSteer {
			told = &up.Message
		}
	}
	if told == nil || told.Rev != m.Rev || told.State.K != wire.MsgInInput {
		t.Fatalf("upsert %+v, message %+v", told, m)
	}
	// A long step: nothing failed, and the line is free.
	r.advance(time.Minute)
	if r.state("m1").K != wire.MsgInInput {
		t.Fatalf("%s", r.trail("m1"))
	}
	// The step ends and Claude reads its queue: the queued_command is the message's entry.
	_, ch := r.box.UserEntryText("c:bb#0", "look at the build", 10, turn("c:aa#0"), r.clock.Now())
	r.record(ch)
	if st := r.state("m1"); st.K != wire.MsgInTurn || st.TurnID != "c:aa#0" {
		t.Fatalf("%s", r.trail("m1"))
	}
}

// The same when the hook comes while the keys are still going in: the outcome of the try then sets the state.
func TestTheHookInTheMiddleOfTypingIntoARunningTurnHandsTheMessageOver(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("m1", "look at the build", wire.ModeQueue)
	look := typist.Look(r.pane, r.agent.Provider)
	r.agent.Screen = &look
	job, ch := r.box.Pump(r.agent, r.size, r.clock.Now())
	r.record(ch)
	if job == nil {
		t.Fatal("not typed")
	}
	r.record(r.box.PromptSubmitted(turn("c:aa#0"), true, r.clock.Now()))
	res := typist.Type(job.Message, r.agent.Provider, r.pane, r.clock)
	r.record(r.box.Typed(job.ClientID, res, r.clock.Now()))
	st := r.state("m1")
	if st.K != wire.MsgInInput || st.Wait == nil || st.Wait.DeadlineAt-st.Wait.SlowAt < SteerDeadline.Milliseconds()/2 {
		t.Fatalf("%+v (%s)", st, r.trail("m1"))
	}
	if m, _ := r.box.Message("m1"); m.Mode != wire.ModeSteer {
		t.Fatalf("mode %s", m.Mode)
	}
}

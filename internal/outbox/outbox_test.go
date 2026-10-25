package outbox

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func turn(id string) *string { return &id }

// §6: a message to an idle agent is typed at once, accepted by the hook, joins its turn and ends with it.
func TestSendToAnIdleAgent(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.size = 4096
	if code := r.send("m1", "hello agent", wire.ModeQueue); code != "" {
		t.Fatal(code)
	}
	r.pump()
	st := r.state("m1")
	if st.K != wire.MsgInInput || st.Wait == nil || st.Wait.DeadlineAt-st.Wait.SlowAt != wire.ConfirmDeadline-wire.WaitSlowMs {
		t.Fatalf("%+v", st)
	}
	r.record(r.box.PromptSubmitted(nil, false, r.clock.Now()))
	if st := r.state("m1"); st.K != wire.MsgAccepted || st.EntryID != nil {
		t.Fatalf("%+v", st)
	}
	id, ch := r.box.UserEntry("c:aa#0", 4200, nil, r.clock.Now())
	r.record(ch)
	if id != "m1" || *r.state("m1").EntryID != "c:aa#0" {
		t.Fatalf("%s %+v", id, r.state("m1"))
	}
	r.record(r.box.TurnStarted("c:aa#0", r.clock.Now()))
	r.record(r.box.TurnEnded("c:aa#0", r.clock.Now()))
	if got := r.trail("m1"); got != "typing → in-input → accepted → in-turn → done" {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(r.typed(), []string{"line:hello agent"}) || r.jobs != 1 {
		t.Fatalf("%v", r.typed())
	}
}

// §6: a message to a working agent waits, without a term, and goes once the turn ends.
func TestSendToABusyAgentWaitsForTheTurn(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "next please", wire.ModeQueue)
	r.send("m2", "and then this", wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Reason != wire.QueuedAgentBusy || st.Wait != nil || st.Position != 1 {
		t.Fatalf("%+v", st)
	}
	if st := r.state("m2"); st.Reason != wire.QueuedBehind || st.Position != 2 {
		t.Fatalf("%+v", st)
	}
	r.advance(10 * time.Minute)
	r.advance(20 * time.Minute)
	if len(r.typed()) != 0 || r.state("m1").K != wire.MsgQueued {
		t.Fatal("a busy agent's queue was typed into or expired")
	}
	r.agent.State = wire.AgentState{K: wire.StateReady, Since: 2}
	r.pane.screen = tui(t, "claude-idle-placeholder.ansi.txt")
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("%+v", st)
	}
	// One message on its way at a time: the second waits for the first to be accepted, on the reason it already had.
	r.pump()
	if st := r.state("m2"); st.K != wire.MsgQueued || st.Reason != wire.QueuedAgentBusy || st.Position != 1 {
		t.Fatalf("%+v", st)
	}
	r.record(r.box.PromptSubmitted(nil, false, r.clock.Now()))
	r.agent.State = wire.AgentState{K: wire.StateWorking, Since: 3}
	r.pump()
	if st := r.state("m2"); st.Reason != wire.QueuedAgentBusy {
		t.Fatalf("%+v", st)
	}
}

// §6: a message "now" goes to the head; steer is typed into the running turn and joins it.
func TestNowAndSteer(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "queued first", wire.ModeQueue)
	r.send("m2", "now", wire.ModeNow)
	r.pump()
	if q := r.box.Queue(); !reflect.DeepEqual(q.Items, []string{"m2", "m1"}) {
		t.Fatalf("%v", q.Items)
	}
	if _, code := r.box.Promote("m1", wire.PromoteToSteer, r.agent.State, r.clock.Now()); code != "" {
		t.Fatal(code)
	}
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput || !reflect.DeepEqual(r.typed(), []string{"line:queued first"}) {
		t.Fatalf("%+v %v", st, r.typed())
	}
	// Claude may send no prompt-submit for a steered message: its user entry accepts it, into the running turn.
	_, ch := r.box.UserEntry("c:bb#0", 10, turn("c:aa#0"), r.clock.Now())
	r.record(ch)
	if st := r.state("m1"); st.K != wire.MsgInTurn || st.TurnID != "c:aa#0" {
		t.Fatalf("%+v", st)
	}
	if _, code := r.box.Promote("m2", wire.PromoteToSteer, wire.AgentState{K: wire.StateIdle}, r.clock.Now()); code != wire.RefuseNotWorking {
		t.Fatal(code)
	}
	if _, code := r.box.Promote("m1", wire.PromoteToNow, r.agent.State, r.clock.Now()); code != wire.RefuseTooLate {
		t.Fatal(code)
	}
}

// §6: a question is open — the message is the answer in words: «Chat about this», then the text.
func TestSendWhileAQuestionIsOpen(t *testing.T) {
	question := tui(t, "claude-ask-preview-question.txt")
	r := newRig(t, question, wire.StateWaitingQuestion)
	r.agent.State.QuestionID = "q:tu1"
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	row := ""
	for _, l := range strings.Split(question, "\n") {
		if strings.Contains(l, "Chat about this") {
			row = l
		}
	}
	r.pane.onSend = func(call string) {
		switch {
		case call == "keys:Down":
			r.pane.screen = strings.Replace(question, row, "❯ "+strings.TrimLeft(row, " "), 1)
		case call == "keys:Enter", strings.HasPrefix(call, "line:"):
			r.pane.screen = idle
		}
	}
	r.send("m1", "none of these, use the cache", wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("%+v %v", st, r.typed())
	}
	if got := r.typed(); got[len(got)-1] != "line:none of these, use the cache" {
		t.Fatalf("%v", got)
	}
}

// §6: a permission menu is never typed into: the message waits blocked-menu, and past its term fails expired.
func TestAPermissionMenuBlocksUntilTheTerm(t *testing.T) {
	r := newRig(t, permissionScreen, wire.StateWaitingPermission)
	r.agent.State.PermissionID = "p:tu2"
	r.send("m1", "go on", wire.ModeQueue)
	r.pump()
	st := r.state("m1")
	if st.Reason != wire.QueuedBlockedMenu || st.Wait == nil || st.Wait.DeadlineAt != st.Wait.SlowAt-wire.WaitSlowMs+QueuedTTL.Milliseconds() {
		t.Fatalf("%+v", st)
	}
	r.advance(QueuedTTL - time.Second)
	if r.state("m1").K != wire.MsgQueued {
		t.Fatal("expired before its term")
	}
	r.advance(time.Second)
	if st := r.state("m1"); st.K != wire.MsgFailed || st.Code != wire.MsgFailedExpired || !st.Retryable {
		t.Fatalf("%+v", st)
	}
	if len(r.typed()) != 0 {
		t.Fatalf("typed into a menu: %v", r.typed())
	}
	// The same screen on an agent the automaton thinks idle: the look before the try still refuses it.
	r2 := newRig(t, permissionScreen, wire.StateIdle)
	r2.send("m1", "go on", wire.ModeQueue)
	r2.pump()
	if st := r2.state("m1"); st.Reason != wire.QueuedBlockedMenu || r2.jobs != 0 {
		t.Fatalf("%+v", st)
	}
}

// §6: someone else's text in the box — blocked-text, looked at again 1.5 → 3 → 6 → 10 s, said once in the journal,
// failed{expired} after 10 minutes; the pages never see it leave the queue and come back.
func TestSomeoneElsesTextBlocks(t *testing.T) {
	r := newRig(t, withInput(t, "a draft the person is writing"), wire.StateIdle)
	r.send("m1", "hello there", wire.ModeQueue)
	r.pump()
	var looks []time.Duration
	start := r.clock.Now()
	for r.state("m1").K == wire.MsgQueued {
		next := r.box.Wake()
		looks = append(looks, next.Sub(start))
		start = next
		r.clock.now = next
		r.pump()
		if len(looks) > 200 {
			t.Fatal("never ends")
		}
	}
	want := []time.Duration{1500 * time.Millisecond, 3 * time.Second, 6 * time.Second, 10 * time.Second, 10 * time.Second}
	if !reflect.DeepEqual(looks[:5], want) {
		t.Fatalf("looks %v", looks[:5])
	}
	if st := r.state("m1"); st.Code != wire.MsgFailedExpired {
		t.Fatalf("%+v", st)
	}
	if got := r.trail("m1"); got != "queued{blocked-text} → failed{expired}" {
		t.Fatal(got)
	}
	waits := 0
	for _, l := range r.log.lines {
		if strings.HasPrefix(l, "message waits") {
			waits++
		}
	}
	if waits != 1 || len(r.typed()) != 0 {
		t.Fatalf("%d waits logged: %v", waits, r.log.lines)
	}
}

// §6: five pictures, the box wrapped the last reference mid-way; a try after a restart finds them as its own and
// types only the text.
func TestPicturesWrappedMidReferenceAreFinished(t *testing.T) {
	after := tui(t, "claude-pictures-wrapped-mid-ref.ansi.txt")
	r := newRig(t, after, wire.StateIdle)
	r.leaves()
	long := "длинный текст\n" + strings.Repeat("x", 2300)
	paths := []string{"/u/a.png", "/u/b.png", "/u/c.png", "/u/d.png", "/u/e.png"}
	atts := make([]wire.AttachmentRef, 5)
	for i := range atts {
		atts[i] = wire.AttachmentRef{ID: paths[i], Name: paths[i], Kind: wire.AttachmentImage, Mime: "image/png"}
	}
	ch, code := r.box.Send(Draft{ClientID: "m1", Text: long, Attachments: atts, Images: paths, Mode: wire.ModeQueue, Kind: wire.KindPrompt}, r.agent.State, r.clock.Now())
	if code != "" {
		t.Fatal(code)
	}
	r.record(ch)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput || !reflect.DeepEqual(r.typed(), []string{"paste: " + long, "keys:Enter"}) {
		t.Fatalf("%+v %q", st, r.typed())
	}
}

// §6: a restart in the middle of typing never types blindly: entry found → accepted; text in the box → only Enter;
// neither → queued at the head and typed once.
func TestARestartMidTypingDoesNotRetype(t *testing.T) {
	saved := func(t *testing.T) State {
		r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
		r.send("m1", "hello there", wire.ModeQueue)
		r.pane.onSend = func(string) { panic("the runtime died while typing") }
		r.agent.Screen = nil
		job, ch := r.box.Pump(r.agent, 777, r.clock.Now())
		r.record(ch)
		if job == nil || r.state("m1").K != wire.MsgTyping {
			t.Fatal("not typing")
		}
		return r.box.State()
	}
	restarted := func(t *testing.T, screen string, find Find) *rig {
		r := newRig(t, screen, wire.StateUnknown)
		r.box.Restore(saved(t))
		inBox := func(text string) bool {
			return strings.Contains(r.pane.screen, idleLine+text)
		}
		r.record(r.box.Restarted(find, inBox, r.clock.Now()))
		r.leaves()
		return r
	}

	r := restarted(t, tui(t, "claude-idle-placeholder.ansi.txt"), func(off int64, _ wire.Ms) (string, bool) { return "c:aa#0", off == 777 })
	if st := r.state("m1"); st.K != wire.MsgAccepted || *st.EntryID != "c:aa#0" {
		t.Fatalf("found: %+v", st)
	}
	r.pump()
	if len(r.typed()) != 0 {
		t.Fatalf("found: typed %v", r.typed())
	}

	r = restarted(t, withInput(t, "hello there"), func(int64, wire.Ms) (string, bool) { return "", false })
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("in box: %+v", st)
	}
	r.pump()
	if !reflect.DeepEqual(r.typed(), []string{"keys:Enter"}) || r.state("m1").K != wire.MsgInInput {
		t.Fatalf("in box: typed %v, %+v", r.typed(), r.state("m1"))
	}

	r = restarted(t, tui(t, "claude-idle-placeholder.ansi.txt"), func(int64, wire.Ms) (string, bool) { return "", false })
	if st := r.state("m1"); st.K != wire.MsgQueued {
		t.Fatalf("neither: %+v", st)
	}
	r.pump()
	if !reflect.DeepEqual(r.typed(), []string{"line:hello there"}) {
		t.Fatalf("neither: typed %v", r.typed())
	}
}

// §6: Stop pauses the queue; nothing goes until queue.resume, even to an idle agent.
func TestStopPausesTheQueue(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "after the stop", wire.ModeQueue)
	r.pump()
	r.record(r.box.Pause(wire.PausedInterrupted, r.clock.Now()))
	r.agent.State = wire.AgentState{K: wire.StateIdle, Since: 5}
	r.advance(time.Second)
	if st := r.state("m1"); st.Reason != wire.QueuedPaused || st.Wait == nil || len(r.typed()) != 0 {
		t.Fatalf("%+v %v", st, r.typed())
	}
	if q := r.box.Queue(); q.Paused == nil || q.Paused.Reason != wire.PausedInterrupted {
		t.Fatalf("%+v", q)
	}
	ch, ok := r.box.Resume(r.clock.Now())
	r.record(ch)
	r.pump()
	if !ok || r.state("m1").K != wire.MsgInInput {
		t.Fatalf("%+v", r.state("m1"))
	}
}

// §6: the limits refuse with their codes, and a client id sent twice queues once.
func TestLimitsAndIdempotency(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	for i := range wire.DefaultLimits.MaxQueue {
		if code := r.send("m"+string(rune('a'+i)), "x", wire.ModeQueue); code != "" {
			t.Fatal(code)
		}
	}
	if code := r.send("over", "x", wire.ModeQueue); code != wire.RefuseQueueFull {
		t.Fatal(code)
	}
	if code := r.send("ma", "x", wire.ModeQueue); code != "" || len(r.box.Queue().Items) != wire.DefaultLimits.MaxQueue {
		t.Fatal("a repeat queued twice")
	}
	r2 := newRig(t, "", wire.StateIdle)
	if code := r2.send("m1", strings.Repeat("я", wire.DefaultLimits.MaxText+1), wire.ModeQueue); code != wire.RefuseTextTooLong {
		t.Fatal(code)
	}
	if code := r2.send("m1", "  ", wire.ModeQueue); code != wire.RefuseBadRequest {
		t.Fatal(code)
	}
}

// §6: the machine's tmux is gone — the queue pauses agent-gone, a send is refused, the queued wait until the TTL.
func TestTheAgentIsGone(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.send("m1", "x", wire.ModeQueue)
	r.agent.State = wire.AgentState{K: wire.StateGone, Since: 2, Reason: string(wire.GoneTmuxLost)}
	r.pump()
	if q := r.box.Queue(); q.Paused == nil || q.Paused.Reason != wire.PausedAgentGone {
		t.Fatalf("%+v", q)
	}
	if st := r.state("m1"); st.Reason != wire.QueuedPaused || st.Wait == nil {
		t.Fatalf("%+v", st)
	}
	// A pane not found may be back (tmux that did not answer): the message waits; a closed tab refuses it.
	if code := r.send("m2", "y", wire.ModeQueue); code != "" {
		t.Fatal(code)
	}
	r.agent.State.Reason = string(wire.GoneTabClosed)
	if code := r.send("m3", "z", wire.ModeQueue); code != wire.RefuseAgentGone {
		t.Fatal(code)
	}
	r.agent.State.Reason = string(wire.GoneTmuxLost)
	r.advance(QueuedTTL)
	if st := r.state("m1"); st.Code != wire.MsgFailedExpired {
		t.Fatalf("%+v", st)
	}
}

// A stopped agent with a session is resumed once, and its message waits resuming.
func TestAStoppedAgentIsResumedOnce(t *testing.T) {
	r := newRig(t, "", wire.StateStopped)
	r.agent.State.Resumable = true
	r.send("m1", "x", wire.ModeQueue)
	job, ch := r.box.Pump(r.agent, 0, r.clock.Now())
	if job != nil || !ch.Resume {
		t.Fatal("no resume")
	}
	_, ch = r.box.Pump(r.agent, 0, r.clock.Now())
	if ch.Resume || r.state("m1").Reason != wire.QueuedResuming {
		t.Fatal("resumed twice")
	}
	r.agent.State = wire.AgentState{K: wire.StateLaunchFailed, Reason: string(wire.LaunchFailedExited)}
	r.pump()
	if q := r.box.Queue(); q.Paused.Reason != wire.PausedLaunchFailed {
		t.Fatalf("%+v", q.Paused)
	}
}

// Unknown is not idle: a message waits until a probe sees the agent in the foreground.
func TestUnknownIsNotIdle(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateUnknown)
	r.agent.Foreground = false
	r.send("m1", "x", wire.ModeQueue)
	r.pump()
	if r.jobs != 0 {
		t.Fatal("typed into an unchecked pane")
	}
	r.agent.Foreground = true
	r.leaves()
	r.pump()
	if r.jobs != 1 {
		t.Fatal("not typed once the agent was seen")
	}
}

// A key that does not go in backs off 1.5 → 3 → 6 → 12 → 24 s; the sixth failed try fails the message.
func TestFailedTriesBackOffAndGiveUp(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.pane.fail = "line:"
	r.send("m1", "hello there", wire.ModeQueue)
	r.pump()
	var gaps []time.Duration
	last := r.clock.Now()
	for r.state("m1").K != wire.MsgFailed {
		next := r.box.Wake()
		gaps = append(gaps, next.Sub(last))
		r.clock.now, last = next, next
		r.pump()
		if len(gaps) > MaxTries+2 {
			t.Fatal("never gives up")
		}
	}
	want := []time.Duration{1500 * time.Millisecond, 3 * time.Second, 6 * time.Second, 12 * time.Second, 24 * time.Second,
		30 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}
	if !reflect.DeepEqual(gaps, want) {
		t.Fatalf("%v", gaps)
	}
	if st := r.state("m1"); st.Code != wire.MsgFailedTypeFailed || !st.Retryable || r.jobs != MaxTries {
		t.Fatalf("%+v after %d", st, r.jobs)
	}
	m, _ := r.box.Message("m1")
	if m.Attempts != MaxTries {
		t.Fatalf("attempts %d", m.Attempts)
	}
	// Nothing claimed it sent on the way: the retry types it.
	r.pane.fail = ""
	r.leaves()
	ch, code := r.box.Retry("m1", nil, r.clock.Now())
	r.record(ch)
	r.pump()
	if code != "" || r.state("m1").K != wire.MsgInInput || !reflect.DeepEqual(r.typed(), []string{"line:hello there"}) {
		t.Fatalf("%s %+v %v", code, r.state("m1"), r.typed())
	}
}

// Text stuck in the box is tried again, backing off, MaxTries in all; then the message fails, retryable, and the retry
// presses only its Enter.
func TestStuckInInputThenRetry(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	typed := withInput(t, "hello there")
	r.pane.onSend = func(string) { r.pane.screen = typed }
	r.send("m1", "hello there", wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgQueued {
		t.Fatalf("failed at the first stuck try: %+v", st)
	}
	for i := 0; i < 4*MaxTries && r.state("m1").K != wire.MsgFailed; i++ {
		r.clock.now = r.box.Wake()
		r.pump()
	}
	if m, _ := r.box.Message("m1"); m.Attempts != MaxTries {
		t.Fatalf("attempts %d", m.Attempts)
	}
	if st := r.state("m1"); st.Code != wire.MsgFailedStuckInInput || !st.Retryable {
		t.Fatalf("%+v", st)
	}
	r.pane.calls = nil
	r.leaves()
	ch, _ := r.box.Retry("m1", nil, r.clock.Now())
	r.record(ch)
	r.pump()
	if !reflect.DeepEqual(r.typed(), []string{"keys:Enter"}) || r.state("m1").K != wire.MsgInInput {
		t.Fatalf("%v %+v", r.typed(), r.state("m1"))
	}
}

// in-input without a sign from the agent in 30 s goes back to the head of the queue, stalled; a retry that finds the
// entry accepts it.
func TestUnconfirmedThenFoundByRetry(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.size = 900
	r.send("m1", "hello there", wire.ModeQueue)
	r.pump()
	r.advance(wire.ConfirmDeadline*time.Millisecond - time.Millisecond)
	if r.state("m1").K != wire.MsgInInput {
		t.Fatal("failed early")
	}
	r.advance(time.Millisecond)
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Reason != wire.QueuedStalled || st.Position != 1 {
		t.Fatalf("%+v", st)
	}
	ch, code := r.box.Retry("m1", func(off int64, _ wire.Ms) (string, bool) { return "c:dd#0", off == 900 }, r.clock.Now())
	r.record(ch)
	if code != "" || r.state("m1").K != wire.MsgAccepted || r.jobs != 1 {
		t.Fatalf("%+v", r.state("m1"))
	}
	if _, code := r.box.Retry("m1", nil, r.clock.Now()); code != wire.RefuseNotFailed {
		t.Fatal(code)
	}
}

// A user entry before the typed offset is not this message's.
func TestAnEarlierEntryIsNotOurs(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.size = 5000
	r.send("m1", "hello there", wire.ModeQueue)
	r.pump()
	if id, _ := r.box.UserEntry("c:old#0", 4000, nil, r.clock.Now()); id != "" {
		t.Fatal("took an older entry")
	}
	if id, _ := r.box.UserEntry("c:new#0", 5000, nil, r.clock.Now()); id != "m1" {
		t.Fatal("missed its entry")
	}
}

func TestCancel(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.send("m1", "x", wire.ModeQueue)
	r.send("m2", "y", wire.ModeQueue)
	ch, code := r.box.Cancel("m1", r.clock.Now())
	r.record(ch)
	if code != "" || r.state("m1").K != wire.MsgCancelled || !reflect.DeepEqual(r.box.Queue().Items, []string{"m2"}) {
		t.Fatalf("%s %+v", code, r.box.Queue())
	}
	r.agent.State = wire.AgentState{K: wire.StateIdle}
	r.pane.screen = tui(t, "claude-idle-placeholder.ansi.txt")
	r.leaves()
	r.pump()
	if _, code := r.box.Cancel("m2", r.clock.Now()); code != wire.RefuseTooLate {
		t.Fatal(code)
	}
	if _, code := r.box.Cancel("nope", r.clock.Now()); code != wire.RefuseNotFound {
		t.Fatal(code)
	}
}

func TestLegacyImport(t *testing.T) {
	r := newRig(t, "", wire.StateUnknown)
	now := r.clock.Now().UnixMilli()
	r.record(r.box.Import([]LegacyItem{
		{ID: "q-1", Text: "fresh", CreatedAt: now - 1000},
		{ID: "q-2", Text: "/compact", CreatedAt: now - 2000},
		{ID: "q-3", Text: "old", CreatedAt: now - LegacyLifetime.Milliseconds() - 1},
		{ID: "q-4", Text: "given up", CreatedAt: now, Failed: true},
	}, r.clock.Now()))
	if q := r.box.Queue(); !reflect.DeepEqual(q.Items, []string{"legacy-q-1", "legacy-q-2"}) {
		t.Fatalf("%v", q.Items)
	}
	m, _ := r.box.Message("legacy-q-2")
	if m.Kind != wire.KindCommand {
		t.Fatal("a slash command imported as a prompt")
	}
	for _, id := range []string{"legacy-q-3", "legacy-q-4"} {
		if st := r.state(id); st.Code != wire.MsgFailedExpired {
			t.Fatalf("%s %+v", id, st)
		}
	}
	// Imported twice (a restart before v2 wrote its own file) is imported once.
	r.box.Import([]LegacyItem{{ID: "q-1", Text: "fresh", CreatedAt: now}}, r.clock.Now())
	if len(r.box.Queue().Items) != 2 {
		t.Fatal("imported twice")
	}
}

func TestAStateSurvivesTheDisk(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.send("m1", "x", wire.ModeQueue)
	r.send("m2", "y", wire.ModeQueue)
	r.record(r.box.Pause(wire.PausedUser, r.clock.Now()))
	b := New("tab-1", wire.DefaultLimits, nil)
	b.Restore(r.box.State())
	if !reflect.DeepEqual(b.Queue(), r.box.Queue()) || !reflect.DeepEqual(b.Messages(r.clock.Now()), r.box.Messages(r.clock.Now())) {
		t.Fatalf("%+v\n%+v", b.Queue(), r.box.Queue())
	}
}

// A TUI still starting drew an empty box when the typist looked, then showed the text with its Enter lost: the text
// sits in the box with nothing submitted. The message is looked for there while it waits in-input, and its Enter is
// pressed again.
func TestAnEnterTheAgentDroppedIsPressedAgain(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	r := newRig(t, idle, wire.StateIdle)
	r.pane.onSend = func(call string) {} // the first line goes nowhere visible: the box reads empty
	r.send("m1", "run the build and tell me", wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("%+v", st)
	}
	r.pane.screen = withInput(t, "run the build and tell me") // the text shows up, unsubmitted
	r.advance(recheckEvery - time.Millisecond)
	if len(r.typed()) != 1 {
		t.Fatalf("pressed too early: %v", r.typed())
	}
	r.pane.onSend = func(call string) {
		if call == "keys:Enter" {
			r.pane.screen = idle
		}
	}
	r.advance(time.Millisecond)
	if got := r.typed(); len(got) != 2 || got[1] != "keys:Enter" {
		t.Fatalf("typed: %v", got)
	}
	if r.state("m1").K != wire.MsgInInput {
		t.Fatalf("%+v", r.state("m1"))
	}
	r.record(r.box.PromptSubmitted(nil, false, r.clock.Now()))
	if r.state("m1").K != wire.MsgAccepted {
		t.Fatalf("%+v", r.state("m1"))
	}
	if got := r.trail("m1"); got != "typing → in-input → accepted" {
		t.Fatal(got)
	}
}

// An Enter that is dropped over and over is pressed maxRechecks times, not forever; the term still ends the wait.
func TestAnEnterPressedAgainIsBounded(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.send("m1", "run the build and tell me", wire.ModeQueue)
	r.pump()
	r.pane.screen = withInput(t, "run the build and tell me")
	r.pane.onSend = func(string) {}
	for i := 0; i < 400 && r.state("m1").K != wire.MsgFailed; i++ {
		r.advance(recheckEvery)
	}
	enters := 0
	for _, c := range r.typed() {
		if c == "keys:Enter" {
			enters++
		}
	}
	if enters > 3*(maxRechecks+MaxTries) { // the typist's own Enters of each try, three at most
		t.Fatalf("%d Enters: %v", enters, r.typed())
	}
	if st := r.state("m1"); st.K != wire.MsgFailed {
		t.Fatalf("%+v", st)
	}
}

// a message typed into a working agent sits in Claude's own queue until the agent's next step. The hook fires
// at the Enter and says nothing of that; only the transcript entry means the agent read it, so the message waits
// in-input for it — past the 15 s of an ordinary confirmation — and does not hold the line for the next steer.
func TestASteeredMessageIsReadWhenTheTranscriptSaysSo(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "first, now", wire.ModeSteer)
	r.send("m2", "second, now", wire.ModeSteer)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput || st.Wait == nil || st.Wait.DeadlineAt-st.Wait.SlowAt < SteerDeadline.Milliseconds()/2 {
		t.Fatalf("typed: %+v", st)
	}
	// The Enter reached Claude's queue and the hook fired: that is not the agent reading it.
	r.record(r.box.PromptSubmitted(turn("c:aa#0"), false, r.clock.Now()))
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("the hook accepted a steered message: %+v", st)
	}
	// The line is free: a second steer goes in while the first still waits for the agent's step.
	r.pump()
	if st := r.state("m2"); st.K != wire.MsgInInput {
		t.Fatalf("the second steer waits behind the first: %+v", st)
	}
	// A long Bash call: past the ordinary confirmation term nothing failed.
	r.advance(time.Minute)
	if r.state("m1").K != wire.MsgInInput || r.state("m2").K != wire.MsgInInput {
		t.Fatalf("failed while the agent was busy: %s / %s", r.trail("m1"), r.trail("m2"))
	}
	// The step ends and Claude reads its queue in order.
	_, ch := r.box.UserEntry("c:bb#0", 10, turn("c:aa#0"), r.clock.Now())
	r.record(ch)
	if st := r.state("m1"); st.K != wire.MsgInTurn || st.TurnID != "c:aa#0" || r.state("m2").K != wire.MsgInInput {
		t.Fatalf("first entry: %s / %s", r.trail("m1"), r.trail("m2"))
	}
	_, ch = r.box.UserEntry("c:cc#0", 20, turn("c:aa#0"), r.clock.Now())
	r.record(ch)
	if r.state("m2").K != wire.MsgInTurn {
		t.Fatalf("second entry: %s", r.trail("m2"))
	}
	if r.trail("m1") != "typing → in-input → accepted → in-turn" {
		t.Fatalf("trail: %s", r.trail("m1"))
	}
}

func TestASteeredMessageNeverReadFailsUnconfirmedAtLast(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "now", wire.ModeSteer)
	r.pump()
	r.advance(SteerDeadline + time.Second)
	if st := r.state("m1"); st.K != wire.MsgFailed || st.Code != wire.MsgFailedUnconfirmed || !st.Retryable {
		t.Fatalf("%+v", st)
	}
}

// A restart that finds a steered message not yet read does not type it again: it may still be in Claude's queue.
func TestARestartKeepsASteeredMessageInInput(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "now", wire.ModeSteer)
	r.pump()
	r.record(r.box.Restarted(func(int64, wire.Ms) (string, bool) { return "", false }, func(string) bool { return false }, r.clock.Now()))
	if st := r.state("m1"); st.K != wire.MsgInInput || len(r.box.Queue().Items) != 0 {
		t.Fatalf("%+v %v", st, r.box.Queue().Items)
	}
	r.pump()
	if !reflect.DeepEqual(r.typed(), []string{"line:now"}) {
		t.Fatalf("typed again: %v", r.typed())
	}
}

// a message's states are one road with no way back. A message that waits on the agent (agent-busy) and is
// pushed down the line by one that jumps ahead («now»), or has a message in flight ahead of it, does not go back to
// «behind» and up again: the pages and the journal saw agent-busy → behind → agent-busy for one client id, a state
// that only seemed to change.
func TestAQueuedMessageNeverSwingsBetweenItsReasons(t *testing.T) {
	// The agent is idle but its screen is busy: nothing is typed, the head waits on the agent.
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("m1", "queued first", wire.ModeQueue)
	r.pump()
	r.send("m2", "now please", wire.ModeNow)
	r.pump()
	r.advance(2 * time.Second)
	if q := r.box.Queue(); !reflect.DeepEqual(q.Items, []string{"m2", "m1"}) {
		t.Fatalf("%v", q.Items)
	}
	// The screen frees: m2 is typed and waits for the agent to take it, m1 stays behind it; the agent works on m2.
	r.pane.screen = tui(t, "claude-idle-placeholder.ansi.txt")
	r.pump()
	r.record(r.box.PromptSubmitted(nil, false, r.clock.Now()))
	r.agent.State = wire.AgentState{K: wire.StateWorking, Since: 3}
	r.advance(5 * time.Second)
	if got, want := r.trail("m1"), "queued{agent-busy}"; got != want {
		t.Fatalf("m1: %s, want %s", got, want)
	}
}

// A message goes the road once whatever the agent does meanwhile: queued{agent-busy} → typing → in-input → accepted →
// in-turn → done, each state one event, none twice (but for the entry's name).
func TestAMessageToABusyAgentGoesTheRoadOnce(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "after the turn", wire.ModeQueue)
	for i := 0; i < 5; i++ {
		r.advance(time.Second)
	}
	r.agent.State = wire.AgentState{K: wire.StateReady, Since: 2}
	r.pane.screen = tui(t, "claude-idle-placeholder.ansi.txt")
	r.pump()
	r.record(r.box.PromptSubmitted(nil, false, r.clock.Now()))
	r.agent.State = wire.AgentState{K: wire.StateWorking, Since: 3}
	for i := 0; i < 3; i++ {
		r.advance(time.Second)
	}
	_, ch := r.box.UserEntry("c:cc#0", 20, nil, r.clock.Now())
	r.record(ch)
	r.record(r.box.TurnStarted("c:cc#0", r.clock.Now()))
	r.record(r.box.TurnEnded("c:cc#0", r.clock.Now()))
	if got, want := r.trail("m1"), "queued{agent-busy} → typing → in-input → accepted → in-turn → done"; got != want {
		t.Fatalf("%s, want %s", got, want)
	}
	// One event per real change: «accepted» comes twice, first from the hook, then naming the transcript's entry.
	events := 0
	for _, e := range r.events {
		if ps, ok := e.(wire.PromptStateEv); ok && ps.ClientID == "m1" {
			events++
		}
	}
	if events != 7 {
		t.Fatalf("%d events for 6 states and the entry's name", events)
	}
}

// the agent is free (its state says idle) but its screen still draws the last turn's spinner for a moment:
// nothing is typed, and the head is looked at again soon — not after seconds. From the moment the screen frees to the
// paste is a few polls, under 300 ms; waiting for a retry of 1.5 s made «send» take 2–3 s on an agent that was free.
func TestAMessageToAFreeAgentIsTypedAsSoonAsItsScreenFrees(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("m1", "hello", wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Reason != wire.QueuedAgentBusy || len(r.typed()) != 0 {
		t.Fatalf("%+v %v", st, r.typed())
	}
	r.advance(400 * time.Millisecond)
	// The screen frees now; the box is looked at on its own wake, a poll at a time.
	r.pane.screen = tui(t, "claude-idle-placeholder.ansi.txt")
	freed := r.clock.Now()
	// The moment the line goes in, not the end of the typist's watch over the box after it.
	var typedAt time.Time
	leave := r.pane.onSend
	r.pane.onSend = func(call string) {
		if typedAt.IsZero() {
			typedAt = r.clock.Now()
		}
		leave(call)
	}
	var delay time.Duration
	for delay = 0; delay <= 3*time.Second && len(r.typed()) == 0; delay += 50 * time.Millisecond {
		r.advance(50 * time.Millisecond)
	}
	if len(r.typed()) == 0 {
		t.Fatal("never typed")
	}
	if took := typedAt.Sub(freed); took > 300*time.Millisecond {
		t.Fatalf("typed %v after the screen freed, want under 300 ms", took)
	}
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("%+v", st)
	}
}

// A screen that stays busy while the agent says idle is not looked at six times a second for ever: the quick looks end
// and the old pace comes back.
func TestAFreeAgentWithABusyScreenIsLookedAtQuicklyOnlyForAWhile(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("m1", "hello", wire.ModeQueue)
	looks := 0
	r.box.log = nil
	for i := 0; i < 400; i++ {
		before := r.box.recs["m1"].NextTry
		r.advance(50 * time.Millisecond)
		if r.box.recs["m1"].NextTry != before {
			looks++
		}
	}
	// 20 s: 40 quick looks, then one every 1.5 s.
	if looks > 60 {
		t.Fatalf("%d looks in 20 s", looks)
	}
}

// A queued message is put before another one or at the end; what is not in the line, or would cross a message of
// another urgency, stays where it is and is refused with its reason.
func TestMoveReordersTheLine(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	for _, id := range []string{"m1", "m2", "m3", "m4"} {
		r.send(id, "text "+id, wire.ModeQueue)
	}
	r.pump()
	move := func(id, before string) wire.RefuseCode {
		t.Helper()
		ch, code := r.box.Move(id, before, r.clock.Now())
		r.record(ch)
		r.pump()
		return code
	}
	items := func() []string { return r.box.Queue().Items }
	rev := r.box.Queue().Rev

	if code := move("m3", "m1"); code != "" || !reflect.DeepEqual(items(), []string{"m3", "m1", "m2", "m4"}) {
		t.Fatalf("%q %v", code, items())
	}
	if r.box.Queue().Rev <= rev {
		t.Fatalf("the order changed and rev did not: %d", r.box.Queue().Rev)
	}
	if st := r.state("m3"); st.Position != 1 {
		t.Fatalf("m3 is at the head: %+v", st)
	}
	if st := r.state("m1"); st.Position != 2 {
		t.Fatalf("m1 is behind: %+v", st)
	}
	if code := move("m3", ""); code != "" || !reflect.DeepEqual(items(), []string{"m1", "m2", "m4", "m3"}) {
		t.Fatalf("%q %v", code, items())
	}
	// Nothing changes: no event, no new rev.
	rev, events := r.box.Queue().Rev, len(r.events)
	if code := move("m3", ""); code != "" || r.box.Queue().Rev != rev || len(r.events) != events {
		t.Fatalf("a move that changes nothing did something: %q rev %d, %d events", code, r.box.Queue().Rev, len(r.events)-events)
	}
	if code := move("m2", "m4"); code != "" || !reflect.DeepEqual(items(), []string{"m1", "m2", "m4", "m3"}) {
		t.Fatalf("already before m4: %q %v", code, items())
	}
	if code := move("nope", "m1"); code != wire.RefuseNotFound {
		t.Fatal(code)
	}
	if code := move("m1", "nope"); code != wire.RefuseNotFound {
		t.Fatal(code)
	}
}

func TestMoveKeepsTheOrderOfUrgency(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.send("m1", "one", wire.ModeQueue)
	r.send("m2", "two", wire.ModeQueue)
	r.send("n1", "now", wire.ModeNow)
	if q := r.box.Queue().Items; !reflect.DeepEqual(q, []string{"n1", "m1", "m2"}) {
		t.Fatalf("%v", q)
	}
	// A plain message does not pass the «now» one, and the «now» one does not sink below the plain ones.
	if _, code := r.box.Move("m2", "n1", r.clock.Now()); code != wire.RefuseConflict {
		t.Fatal(code)
	}
	if _, code := r.box.Move("n1", "", r.clock.Now()); code != wire.RefuseConflict {
		t.Fatal(code)
	}
	if _, code := r.box.Move("m2", "m1", r.clock.Now()); code != "" {
		t.Fatal(code)
	}
	if q := r.box.Queue().Items; !reflect.DeepEqual(q, []string{"n1", "m2", "m1"}) {
		t.Fatalf("%v", q)
	}
}

// What is being typed or waits in the agent's input is not in the line any more: it is too late to move it, and a
// place before it is a place that is gone.
func TestMoveRefusesWhatLeftTheLine(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.send("m1", "goes first", wire.ModeQueue)
	r.send("m2", "second", wire.ModeQueue)
	r.send("m3", "third", wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("%+v", st)
	}
	if _, code := r.box.Move("m1", "m3", r.clock.Now()); code != wire.RefuseTooLate {
		t.Fatalf("%q", code)
	}
	if _, code := r.box.Move("m3", "m1", r.clock.Now()); code != wire.RefuseConflict {
		t.Fatalf("%q", code)
	}
	if _, code := r.box.Move("m3", "m2", r.clock.Now()); code != "" {
		t.Fatalf("%q", code)
	}
	if q := r.box.Queue().Items; !reflect.DeepEqual(q, []string{"m3", "m2"}) {
		t.Fatalf("%v", q)
	}
}

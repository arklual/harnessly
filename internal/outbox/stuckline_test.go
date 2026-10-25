package outbox

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A long line typed under load: Claude draws it late, its Enter taken into the text as a newline. The box reads empty
// for the first looks after the Enter, then holds the text with the cursor on a new row below it. An empty box the
// typist never saw the text in is not taken for sent: the text shows up while it still watches, and its Enter is
// pressed again — the message goes in, typed once.
func TestALongLineDrawnLateIsNotTakenForSent(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	swallowed := tui(t, "claude-2.1.310-input-long-enter-swallowed.ansi.txt")
	r := newRig(t, idle, wire.StateIdle)
	var typedAt time.Time
	submitted := false
	r.pane.view = func() string {
		switch {
		case submitted || typedAt.IsZero():
			return idle
		case r.clock.Now().Sub(typedAt) < 400*time.Millisecond:
			return idle // not drawn yet
		}
		return swallowed
	}
	r.pane.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "line:"):
			typedAt = r.clock.Now()
		case call == "keys:Enter" && !typedAt.IsZero() && r.clock.Now().Sub(typedAt) >= 400*time.Millisecond:
			submitted = true
		}
	}
	r.send("m1", labText(), wire.ModeQueue)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("%+v", st)
	}
	if got := r.typed(); len(got) != 2 || !strings.HasPrefix(got[0], "line:") || got[1] != "keys:Enter" {
		t.Fatalf("typed: %v", got)
	}
	if !submitted {
		t.Fatal("the text was left in the box")
	}
}

// The prod sequence (a voice message of 773 characters, steered into a working agent): the line went in as the
// agent's step was ending, the box read empty while the typist watched, so the message was taken for one in Claude's
// queue («in the agent's queue» on the page). It was not: its text sat in the box, the Enter lost, and the next steered
// message waited on «someone else's text» until it expired. The box is looked at again for a steered message: its own
// text there gets its Enter, once, and the message after it goes in.
func TestASteeredMessageLeftInTheBoxAsTheTurnEndsIsEntered(t *testing.T) {
	busy := tui(t, "claude-busy-spinner.ansi.txt")
	swallowed := tui(t, "claude-2.1.310-input-long-enter-swallowed.ansi.txt")
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	r := newRig(t, busy, wire.StateWorking)
	r.pane.onSend = func(string) {} // the line goes in, nothing shows: the TUI is busy ending its step
	r.send("a", labText(), wire.ModeSteer)
	r.send("b", "and one more thing", wire.ModeSteer)
	r.pump()
	if st := r.state("a"); st.K != wire.MsgInInput {
		t.Fatalf("a: %+v", st)
	}
	// The step ends; Claude never queued the line: it is in the box, unsubmitted.
	r.agent.State = wire.AgentState{K: wire.StateIdle, Since: 2}
	r.pane.screen = swallowed
	r.pane.onSend = func(call string) {
		if call == "keys:Enter" || strings.HasPrefix(call, "line:") {
			r.pane.screen = idle
		}
	}
	r.pump()
	if st := r.state("b"); st.K != wire.MsgQueued || st.Reason != wire.QueuedBlockedText {
		t.Fatalf("b: %+v", st)
	}
	for i := 0; i < 4 && len(r.typed()) < 2; i++ {
		r.advance(recheckEvery)
	}
	if got := r.typed(); len(got) < 2 || got[1] != "keys:Enter" {
		t.Fatalf("a's Enter was not pressed again: %v", got)
	}
	// Claude reads it: the transcript has its entry.
	_, ch := r.box.UserEntryText("c:aa#0", labText(), 10, nil, r.clock.Now())
	r.record(ch)
	if st := r.state("a"); st.K != wire.MsgAccepted {
		t.Fatalf("a: %s", r.trail("a"))
	}
	for i := 0; i < 10 && r.state("b").K == wire.MsgQueued; i++ {
		r.advance(time.Second)
	}
	if st := r.state("b"); st.K != wire.MsgInInput {
		t.Fatalf("b: %s", r.trail("b"))
	}
	lines := 0
	for _, c := range r.typed() {
		if c == "line:"+labText() {
			lines++
		}
	}
	if lines != 1 || r.typed()[len(r.typed())-1] != "line:and one more thing" {
		t.Fatalf("typed: %v", r.typed())
	}
}

// A steered message waiting in Claude's queue does not press anything into a box that holds someone else's text: the
// next message waits on that text, as the page says, and nothing of the person's draft is sent.
func TestASteeredMessageNeverEntersSomeoneElsesText(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("a", "first, now", wire.ModeSteer)
	r.pump()
	r.agent.State = wire.AgentState{K: wire.StateIdle, Since: 2}
	r.pane.screen = withInput(t, "a draft the person is writing")
	r.pane.onSend = func(string) {}
	r.send("b", "second", wire.ModeQueue)
	for i := 0; i < 8; i++ {
		r.advance(recheckEvery)
	}
	if got := r.typed(); len(got) != 1 {
		t.Fatalf("typed into someone's draft: %v", got)
	}
	if st := r.state("b"); st.K != wire.MsgQueued || st.Reason != wire.QueuedBlockedText {
		t.Fatalf("b: %+v", st)
	}
	if st := r.state("a"); st.K != wire.MsgInInput {
		t.Fatalf("a: %+v", st)
	}
}

// Its Enters pressed again and the text still in the box: the typist takes its text out and it is typed again, backing
// off; a box that never takes it fails it after MaxTries, retryable — never left holding it for the next message.
func TestASteeredMessageStuckInTheBoxFailsAndLeavesItClean(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.pane.onSend = func(string) {}
	r.send("a", labText(), wire.ModeSteer)
	r.pump()
	r.agent.State = wire.AgentState{K: wire.StateIdle, Since: 2}
	swallowed := tui(t, "claude-2.1.310-input-long-enter-swallowed.ansi.txt")
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	r.pane.screen = swallowed
	r.pane.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "line:"):
			r.pane.screen = swallowed // typed again, its Enter swallowed again
		case call != "keys:Enter" && strings.HasPrefix(call, "keys:"):
			r.pane.screen = idle // clearing the box takes it out
		}
	}
	for i := 0; i < 400 && r.state("a").K != wire.MsgFailed; i++ {
		r.advance(recheckEvery)
	}
	if st := r.state("a"); st.K != wire.MsgFailed || st.Code != wire.MsgFailedStuckInInput || !st.Retryable {
		t.Fatalf("a: %s", r.trail("a"))
	}
	if r.pane.screen != idle {
		t.Fatal("the text was left in the box")
	}
	lines := 0
	for _, c := range r.typed() {
		if strings.HasPrefix(c, "line:") {
			lines++
		}
	}
	if m, _ := r.box.Message("a"); lines < MaxTries-1 || m.Attempts != MaxTries {
		t.Fatalf("typed %d times, %d attempts", lines, m.Attempts)
	}
}

// The person stops the turn while a steered message waits in Claude's queue: Claude puts it back into the input box
// to be edited. It is not Entered again by itself — it fails as returned-to-input and is no longer looked for; its
// retry presses only its Enter (the text is in the box, never typed twice), and it can be removed instead.
func TestASteeredMessageReturnedByAnInterruptIsNotEnteredAgain(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("a", labText(), wire.ModeSteer)
	r.pump()
	if st := r.state("a"); st.K != wire.MsgInInput {
		t.Fatalf("a: %+v", st)
	}
	// Escape: the turn ends interrupted, the queued text is back in the box.
	r.agent.State = wire.AgentState{K: wire.StateIdle, Since: 2}
	r.pane.screen = tui(t, "claude-2.1.310-input-long-enter-swallowed.ansi.txt")
	r.record(r.box.Interrupted(r.clock.Now()))
	if st := r.state("a"); st.K != wire.MsgFailed || st.Code != wire.MsgFailedReturned || !st.Retryable {
		t.Fatalf("a: %s", r.trail("a"))
	}
	for i := 0; i < 6; i++ {
		r.advance(steerRecheckEvery)
	}
	if got := r.typed(); len(got) != 1 || r.box.NeedsLook(r.clock.Now()) {
		t.Fatalf("pressed into the person's edit: %v", got)
	}
	// The person retries it: only its Enter goes in.
	ch, code := r.box.Retry("a", nil, r.clock.Now())
	if code != "" {
		t.Fatal(code)
	}
	r.record(ch)
	r.pump()
	if got := r.typed(); len(got) != 2 || got[1] != "keys:Enter" {
		t.Fatalf("retry typed: %v", got)
	}
	// Or removes it.
	r2 := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r2.leaves()
	r2.send("a", "now", wire.ModeSteer)
	r2.pump()
	r2.record(r2.box.Interrupted(r2.clock.Now()))
	if _, code := r2.box.Cancel("a", r2.clock.Now()); code != "" || r2.state("a").K != wire.MsgCancelled {
		t.Fatalf("%s %+v", code, r2.state("a"))
	}
}

// The interrupt is seen on the screen before the transcript says so: Claude's «Interrupted» mark is the newest line
// above the box, and the box holds the steered message's text. It is not Entered: it is returned to the person.
func TestASteeredMessageBackInTheBoxUnderAnInterruptMarkIsNotEntered(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("a", labText(), wire.ModeSteer)
	r.pump()
	r.agent.State = wire.AgentState{K: wire.StateIdle, Since: 2}
	swallowed := tui(t, "claude-2.1.310-input-long-enter-swallowed.ansi.txt")
	r.pane.screen = strings.Replace(swallowed, "\x1b[39m ок\n", "\x1b[39m ок\n  ⎿  Interrupted · What should Claude do instead?\n", 1)
	if r.pane.screen == swallowed {
		t.Fatal("the capture changed: no answer line to put the mark under")
	}
	for i := 0; i < 4; i++ {
		r.advance(recheckEvery)
	}
	if got := r.typed(); len(got) != 1 {
		t.Fatalf("Entered after a stop: %v", got)
	}
	if st := r.state("a"); st.K != wire.MsgFailed || st.Code != wire.MsgFailedReturned {
		t.Fatalf("a: %s", r.trail("a"))
	}
}

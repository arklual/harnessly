package outbox

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// notDue: Wake names no time at or before now — a past time would make the actor that waits on it turn without a pause.
func notDue(t *testing.T, r *rig) {
	t.Helper()
	if w := r.box.Wake(); !w.IsZero() && !w.After(r.clock.Now()) {
		t.Fatalf("wake %v is not after now %v: the actor would turn without a pause", w, r.clock.Now())
	}
}

// HRN-969: a stalled head whose last try's time is long past waits for the person; Wake does not name that time.
func TestStalledHeadDoesNotWakeTheClock(t *testing.T) {
	r := newRig(t, withInput(t, "a draft the person is writing"), wire.StateIdle)
	r.send("m1", "hello there", wire.ModeQueue)
	r.pump() // blocked by someone's text: NextTry is set
	rec := r.box.recs["m1"]
	if rec.NextTry == 0 {
		t.Fatal("no retry time to begin with")
	}
	ch := Change{}
	r.box.stall(&ch, rec, r.clock.Now())
	r.record(ch)
	rec.ClearBox = false // its text was taken out of the box already, as in the chat that turned (tab-7yts788js)
	r.clock.Sleep(time.Hour)
	if st := r.state("m1"); st.Reason != wire.QueuedStalled {
		t.Fatalf("%+v", st)
	}
	notDue(t, r)
	r.pump()
	notDue(t, r)
}

// HRN-969: a retry's time that came while the agent works is not named by Wake over and over: the agent's state holds
// the head, and the try goes when it lets it.
func TestDueRetryWhileAgentBusyDoesNotWakeTheClock(t *testing.T) {
	r := newRig(t, withInput(t, "a draft the person is writing"), wire.StateIdle)
	r.send("m1", "hello there", wire.ModeQueue)
	r.pump()
	if r.box.recs["m1"].NextTry == 0 {
		t.Fatal("no retry time to begin with")
	}
	r.agent.State = wire.AgentState{K: wire.StateWorking, Since: 2}
	r.clock.Sleep(time.Minute)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgQueued {
		t.Fatalf("%+v", st)
	}
	notDue(t, r)
	// The agent is back at its prompt and the box is clear: the head goes at the next pump, with no wait of the clock.
	r.agent.State = wire.AgentState{K: wire.StateIdle, Since: 3}
	r.pane.screen = tui(t, "claude-idle-placeholder.ansi.txt")
	r.leaves()
	r.pump()
	if r.jobs != 1 {
		t.Fatalf("not typed after the agent came back: %+v", r.state("m1"))
	}
}

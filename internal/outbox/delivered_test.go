package outbox

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A message the agent's structured channel names by its client id is accepted on that word alone: no screen is read
// for it, the line is free for the next one, and it is confirmed once — a second word, or one for a message that is
// not on its way, changes nothing.
func TestDeliveredByTheChannelAcceptsTheMessageOnce(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateReady)
	r.send("m1", "first", wire.ModeQueue)
	r.send("m2", "second", wire.ModeQueue)
	if ch := r.box.Delivered("m2", nil, r.clock.Now()); len(ch.Events) != 0 {
		t.Fatalf("a queued message was confirmed: %+v", ch.Events)
	}
	look := typist.Look(r.pane, r.agent.Provider)
	r.agent.Screen = &look
	job, ch := r.box.Pump(r.agent, r.size, r.clock.Now())
	r.record(ch)
	if job == nil || job.ClientID != "m1" {
		t.Fatalf("job %+v", job)
	}
	r.record(r.box.Typed("m1", typist.Result{Outcome: typist.Sent, Typed: true}, r.clock.Now()))
	if k := r.state("m1").K; k != wire.MsgInInput {
		t.Fatalf("m1 is %s", k)
	}
	r.record(r.box.Delivered("m1", nil, r.clock.Now()))
	if k := r.state("m1").K; k != wire.MsgAccepted || r.box.InFlight() != "" {
		t.Fatalf("m1 is %s, in flight %q", k, r.box.InFlight())
	}
	if ch := r.box.Delivered("m1", nil, r.clock.Now()); len(ch.Events) != 0 {
		t.Fatalf("confirmed twice: %+v", ch.Events)
	}
	if ch := r.box.Delivered("nobody", nil, r.clock.Now()); len(ch.Events) != 0 {
		t.Fatalf("%+v", ch.Events)
	}
	// The hook that comes for the same prompt afterwards takes nothing else.
	r.record(r.box.PromptSubmitted(nil, false, r.clock.Now()))
	if k := r.state("m2").K; k != wire.MsgQueued {
		t.Fatalf("m2 is %s", k)
	}
	// A steered message joins the turn that runs.
	r.agent.State = wire.AgentState{K: wire.StateWorking, Since: 1}
	turn := "x:10#1"
	r.record(r.box.TurnStarted(turn, r.clock.Now()))
	r.send("m3", "and this", wire.ModeSteer)
	r.record(r.box.Delivered("m3", &turn, r.clock.Now()))
	if k := r.state("m3").K; k != wire.MsgQueued {
		t.Fatalf("m3, not handed over yet, is %s", k)
	}
}

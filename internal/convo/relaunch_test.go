package convo

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A relaunch asked while the agent works waits for the turn to end, then stops the agent and resumes it on its
// session; the model on record stays.
func TestRelaunchWaitsForTheTurn(t *testing.T) {
	r := newRig(t)
	terms := &fakeTerms{owned: true}
	r.deps = func(d *Deps) { d.Terminals = terms }
	r.rt = r.runtime()
	r.user("first")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.agentTakes()
	before := r.record().Model

	if err := r.rt.Relaunch(context.Background(), r.id); err != nil {
		t.Fatal(err)
	}
	r.tick(time.Second)
	if len(terms.stopped) != 0 || len(r.launcher.resumed) != 0 {
		t.Fatalf("relaunched mid-turn: stopped %v, resumed %v", terms.stopped, r.launcher.resumed)
	}
	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	r.tick(time.Second)
	if len(terms.stopped) != 1 || len(r.launcher.resumed) != 1 {
		t.Fatalf("after the turn: stopped %v, resumed %v", terms.stopped, r.launcher.resumed)
	}
	if after := r.record().Model; (before == nil) != (after == nil) || before != nil && *before != *after {
		t.Fatalf("model %+v → %+v", before, after)
	}
	if err := r.rt.Relaunch(context.Background(), "nope"); err != ErrNotFound {
		t.Fatalf("unknown: %v", err)
	}
}

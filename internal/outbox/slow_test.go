package outbox

import (
	"errors"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A try that ended before its first key because the terminal answers too slowly leaves the message at the head of the
// queue: it is tried again after a pause, not at once and not typed blind.
func TestASlowTerminalKeepsTheMessageQueuedForAPause(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("m1", "проверь логи", wire.ModeQueue)
	look := typist.Look(r.pane, r.agent.Provider)
	r.agent.Screen = &look
	job, ch := r.box.Pump(r.agent, r.size, r.clock.Now())
	r.record(ch)
	if job == nil {
		t.Fatal("no try")
	}
	slow := typist.Result{Outcome: typist.Wait, Slow: true, Err: &typist.SlowError{Cause: errors.New("tmux display-message did not answer in 2.3s")}}
	r.record(r.box.Typed(job.ClientID, slow, r.clock.Now()))
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Position != 1 {
		t.Fatalf("after a slow terminal: %+v", st)
	}
	if wake := r.box.Wake(); wake.Sub(r.clock.Now()) != slowPause {
		t.Fatalf("next try in %s", wake.Sub(r.clock.Now()))
	}
	r.advance(slowPause - time.Second)
	if len(r.typed()) != 0 {
		t.Fatalf("typed before the pause ended: %v", r.typed())
	}
	r.advance(time.Second)
	if r.lines("проверь логи") != 1 || r.state("m1").K != wire.MsgInInput {
		t.Fatalf("not typed after the pause: %v %s", r.typed(), r.trail("m1"))
	}
}

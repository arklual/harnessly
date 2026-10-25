package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A reply the model is slow to start: nothing in the transcript for minutes, the box up, the spinner unread. Silence
// alone is a turn at work — no end, no Stop refused — until Claude's summary says the turn is over.
func TestASilentWaitOnTheModelIsAWorkingTurn(t *testing.T) {
	r := newRig(t)
	r.user("long job")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)

	// Claude 2.1.310 at work under an older turn's summary, its spinner and footer hint not read.
	quiet := strings.Replace(tui(t, "claude-2.1.310-next-turn-under-done.ansi.txt"), "* Philosophizing…", "", 1)
	for _, screen := range []string{tui(t, "claude-idle-placeholder.ansi.txt"), strings.Replace(quiet, "esc to interrupt", "", 1)} {
		r.pane.screen = screen
		for range 120 {
			r.tick(time.Second)
		}
		if k := r.state().K; k != wire.StateWorking {
			t.Fatalf("two minutes into the wait: %s", k)
		}
		if st := r.snap().Turns[0].Status; st != wire.TurnLive {
			t.Fatalf("the turn is %s", st)
		}
	}
	if res := r.do(wire.InterruptCmd{OpID: "i1", ConversationID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("stop refused: %+v", res)
	}
}

// The summary under the turn ends it at the usual silence.
func TestTheSummaryOnScreenEndsAQuietTurn(t *testing.T) {
	r := newRig(t)
	r.user("build it")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.done()
	for range 20 {
		r.tick(time.Second)
	}
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("after the summary and the silence: %s", k)
	}
	if st := r.snap().Turns[0].Status; st != wire.TurnDone {
		t.Fatalf("the turn ends as %s", st)
	}
}

// A turn whose end nothing reports — no Stop hook, no summary read — is not open forever: past QuietUnmarked the silence
// ends it.
func TestALongSilenceEndsATurnPastTheHold(t *testing.T) {
	r := newRig(t)
	r.user("build it")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.idle()
	for range int(QuietUnmarked/time.Minute) - 1 {
		r.tick(time.Minute)
	}
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("within the hold: %s", k)
	}
	for range 3 {
		r.tick(time.Minute)
	}
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("past the hold: %s", k)
	}
}

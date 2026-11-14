package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// the gateway refused the call (403). The turn ends at the CLI's first retry notice, with the reason in the
// feed and one interrupt; the queue is not held and no target is asked for.
func TestRefusedCallEndsTheTurnAtTheFirstRetry(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.pane.screen = "✻ 403 Forbidden · Retrying in 1s · attempt 1/10"
	r.tick(time.Second)

	if got := r.state().K; got != wire.StateReady {
		t.Fatalf("after the refusal: %s", got)
	}
	turns := r.snap().Turns
	last := turns[len(turns)-1]
	if last.Status != wire.TurnFailed || last.Failure == nil || last.Failure.Code != wire.TurnFailedApiError ||
		last.Failure.Detail == nil || !strings.HasPrefix(*last.Failure.Detail, "Нет доступа к модели (403)") {
		t.Fatalf("the turn: %+v %+v", last, last.Failure)
	}
	rec := r.record()
	if rec.TerminalFailure != nil || rec.Queue.Paused != nil {
		t.Fatalf("a refusal neither marks a target choice nor holds the queue: %+v %+v", rec.TerminalFailure, rec.Queue.Paused)
	}
	interrupts := 0
	for _, call := range r.pane.calls {
		if call == "keys:C-c" {
			interrupts++
		}
	}
	if interrupts != 1 {
		t.Fatalf("%d interrupts: %v", interrupts, r.pane.calls)
	}
}

// A throttle or the vendor's own error is retried by the CLI as before: the turn goes on working.
func TestRetryableFailuresKeepTheTurnWorking(t *testing.T) {
	for _, screen := range []string{
		"✻ 429 Too Many Requests · Retrying in 5s · attempt 1/10",
		"  ⎿  API Error (503 Service Unavailable) · Retrying in 2 seconds… (attempt 1/10)",
		"  ⎿  API Error (Connection error.) · Retrying in 2 seconds… (attempt 2/10)",
	} {
		r := newRig(t)
		r.working("big job")
		r.pane.screen = screen
		r.tick(time.Second)
		if got := r.state().K; got != wire.StateWorking {
			t.Fatalf("%q: %s", screen, got)
		}
		for _, call := range r.pane.calls {
			if call == "keys:C-c" {
				t.Fatalf("%q: interrupted", screen)
			}
		}
	}
}

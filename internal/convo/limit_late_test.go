package convo

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// realLimitLines are the lines Claude Code wrote on runner-2 on 2026-11-09 when tab-q58hc55d8 hit its 5-hour limit
// (cut to the fields they are read by): the refusal, Claude's own notice that it waits for the reset, the turn's length.
func (r *rig) realLimitLines(resetsAt int64) {
	r.line(`"type":"assistant","message":{"model":"<synthetic>","role":"assistant","stop_reason":"stop_sequence","type":"message","content":[{"type":"text","text":"You've hit your 5-hour limit · resets 2:20am (UTC)"}]},"apiError":"usage_limit_reached","apiErrorParams":{"rate_limit_info":{"status":"rejected","resetsAt":%d,"rateLimitType":"five_hour"}},"quotaLimits":{"status":"rejected","resetsAt":%d,"unifiedRateLimitFallbackAvailable":false,"rateLimitType":"five_hour","overageStatus":"rejected","isUsingOverage":false},"error":"rate_limit","isApiErrorMessage":true,"apiErrorStatus":429,"failedResponseHadNoToolCall":true`, resetsAt, resetsAt)
	r.line(`"type":"system","subtype":"informational","content":"Usage limit reached · continuing automatically at 2:20am · esc to cancel","isMeta":false,"level":"notice"`)
	r.line(`"type":"system","subtype":"turn_duration","durationMs":210627,"messageCount":569,"isMeta":false`)
}

func (r *rig) assertHeld(reset time.Time) *wire.LimitHold {
	r.t.Helper()
	rec := r.record()
	h := rec.Limited
	if h == nil || h.ResetsAt == nil || *h.ResetsAt != reset.UnixMilli() || h.ContinueAt == nil {
		r.t.Fatalf("the hold: %+v", h)
	}
	if rec.Queue.Paused == nil || rec.Queue.Paused.Reason != wire.PausedLimited {
		r.t.Fatalf("the queue: %+v", rec.Queue)
	}
	turns := r.snap().Turns
	last := turns[len(turns)-1]
	if last.Status != wire.TurnFailed || last.Failure == nil || last.Failure.Code != wire.TurnFailedRateLimit {
		r.t.Fatalf("the turn: %+v %+v", last, last.Failure)
	}
	return h
}

// HRN-916: Claude's StopFailure comes before the refusal is on the transcript (the hook says no more than that the
// turn failed). The turn first reads as failed for no known reason; the refusal read a moment later holds the chat
// until the reset, and it goes on by itself then.
func TestLimitReadAfterTheStopFailureHoldsTheChat(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	reset := r.clock.Now().Add(time.Hour).Truncate(time.Second)
	r.say("working on it")
	r.tick(time.Second)
	r.idle()
	r.hook("stop-failure")

	r.realLimitLines(reset.Unix())
	r.tick(time.Second)
	h := r.assertHeld(reset)

	typed := len(r.pane.calls)
	r.tick(time.Minute)
	if got := r.typedLines(typed); len(got) != 0 {
		t.Fatalf("typed into a held chat: %v", got)
	}
	r.tickTo(time.UnixMilli(*h.ContinueAt).Add(time.Second))
	if got := r.typedLines(typed); len(got) != 1 || got[0] != LimitContinueText {
		t.Fatalf("after the reset typed %v", got)
	}
}

// The real lines read before the hook hold the chat as they always did.
func TestRealLimitLinesHoldTheChat(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	reset := r.clock.Now().Add(time.Hour).Truncate(time.Second)
	r.realLimitLines(reset.Unix())
	r.idle()
	r.hook("stop-failure")
	r.assertHeld(reset)
}

// A turn that fails with no words of a limit while the limits say the plan ran out (the composer's «5ч 0%») is the
// limit: the chat waits for the reset the limits name.
func TestFailureOnAnExhaustedPlanHoldsTheChat(t *testing.T) {
	var reset time.Time
	r := newRig(t)
	r.deps = func(d *Deps) {
		d.LimitReset = func(p wire.Provider, now time.Time) wire.Ms {
			if p != wire.ProviderClaude {
				return 0
			}
			return reset.UnixMilli()
		}
	}
	r.rt.Stop()
	r.rt = r.runtime()
	reset = r.clock.Now().Add(2 * time.Hour).Truncate(time.Second)
	r.working("big job")
	r.say("working on it")
	r.tick(time.Second)
	r.idle()
	r.hook("stop-failure")
	r.tick(time.Second)
	r.assertHeld(reset)
}

// A failure while the plan has room left is no limit.
func TestFailureWithRoomLeftIsNoLimit(t *testing.T) {
	r := newRig(t)
	r.deps = func(d *Deps) {
		d.LimitReset = func(wire.Provider, time.Time) wire.Ms { return 0 }
	}
	r.rt.Stop()
	r.rt = r.runtime()
	r.working("big job")
	r.say("working on it")
	r.tick(time.Second)
	r.idle()
	r.hook("stop-failure")
	r.tick(time.Second)
	if h := r.record().Limited; h != nil {
		t.Fatalf("held: %+v", h)
	}
}

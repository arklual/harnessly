package convo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// limitLine is Claude Code's refusal for the plan's usage limit as 2.1.282 writes it into the transcript (an
// assistant line of its own, model <synthetic>, with the reset in quotaLimits), cut to the fields it is read by.
func (r *rig) limitLine(resetsAt int64) string {
	return r.line(`"type":"assistant","message":{"model":"<synthetic>","role":"assistant","stop_reason":"stop_sequence","type":"message","content":[{"type":"text","text":"You've hit your session limit · resets 11:50pm (UTC)"}]},"quotaLimits":{"status":"rejected","resetsAt":%d,"unifiedRateLimitFallbackAvailable":false,"rateLimitType":"five_hour","overageStatus":"rejected","isUsingOverage":false},"error":"rate_limit","isApiErrorMessage":true,"apiErrorStatus":429`, resetsAt)
}

// working sends the first message and lets the agent take it: the chat is at work on it.
func (r *rig) working(text string) {
	r.t.Helper()
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()
	r.send("m1", text, wire.ModeQueue)
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateWorking {
		r.t.Fatalf("after the message the agent is %s", k)
	}
}

// hitLimit plays the turn ending on the usage limit, resetting in an hour.
func (r *rig) hitLimit() time.Time {
	r.t.Helper()
	reset := r.clock.Now().Add(time.Hour).Truncate(time.Second)
	r.limitLine(reset.Unix())
	r.idle()
	r.hook("stop")
	return reset
}

// typedLines are the texts typed into the pane since from.
func (r *rig) typedLines(from int) []string {
	var out []string
	for _, c := range r.pane.calls[from:] {
		if t, ok := strings.CutPrefix(c, "line:"); ok {
			out = append(out, t)
		} else if t, ok := strings.CutPrefix(c, "paste:"); ok {
			out = append(out, t)
		}
	}
	return out
}

// tickTo moves the fake clock in steps up to at, giving the runtime its turns on the way.
func (r *rig) tickTo(at time.Time) {
	for r.clock.Now().Before(at) {
		step := at.Sub(r.clock.Now())
		if step > time.Minute {
			step = time.Minute
		}
		r.tick(step)
	}
}

// A turn that ends on Claude's usage limit fails on it, the chat is held until the reset (the queue paused), what the
// person sends meanwhile waits, and at the reset (and its jitter) that message goes — no word of the runtime's own.
func TestLimitHoldsTheQueueUntilTheReset(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	reset := r.hitLimit()

	turns := r.snap().Turns
	last := turns[len(turns)-1]
	if last.Status != wire.TurnFailed || last.Failure == nil || last.Failure.Code != wire.TurnFailedRateLimit ||
		last.Failure.RetryAt == nil || *last.Failure.RetryAt != reset.UnixMilli() {
		t.Fatalf("the turn: %+v %+v", last, last.Failure)
	}
	rec := r.record()
	h := rec.Limited
	if h == nil || h.Provider != wire.ProviderClaude || h.Window != wire.LimitSession || h.ResetsAt == nil ||
		*h.ResetsAt != reset.UnixMilli() || h.ContinueAt == nil {
		t.Fatalf("the hold: %+v", h)
	}
	jitter := *h.ContinueAt - *h.ResetsAt
	if jitter <= 0 || jitter > wire.LimitJitterMax.Milliseconds() {
		t.Fatalf("continueAt %d ms past the reset", jitter)
	}
	if rec.Queue.Paused == nil || rec.Queue.Paused.Reason != wire.PausedLimited {
		t.Fatalf("the queue: %+v", rec.Queue)
	}

	typed := len(r.pane.calls)
	r.send("m2", "and then this", wire.ModeQueue)
	r.tick(time.Minute)
	if got := r.typedLines(typed); len(got) != 0 {
		t.Fatalf("typed into a held chat: %v", got)
	}
	if m := r.message("m2"); m.State.K != wire.MsgQueued || m.State.Reason != wire.QueuedPaused {
		t.Fatalf("the message while held: %+v", m.State)
	}

	r.tickTo(time.UnixMilli(*h.ContinueAt).Add(time.Second))
	if got := r.typedLines(typed); len(got) != 1 || got[0] != "and then this" {
		t.Fatalf("after the reset typed %v", got)
	}
	rec = r.record()
	if rec.Limited != nil || rec.Queue.Paused != nil {
		t.Fatalf("still held: %+v %+v", rec.Limited, rec.Queue.Paused)
	}
	if len(rec.AutoContinues) != 1 || rec.AutoContinues[0].Why != wire.AutoAfterLimit || rec.AutoContinues[0].ClientID != "m2" ||
		rec.AutoContinues[0].Own {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
}

// With nothing queued, the runtime goes on with its own word, a message marked as its own.
func TestLimitGoesOnWithItsOwnWord(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.hitLimit()
	h := r.record().Limited
	typed := len(r.pane.calls)
	r.tickTo(time.UnixMilli(*h.ContinueAt).Add(time.Second))
	if got := r.typedLines(typed); len(got) != 1 || got[0] != LimitContinueText {
		t.Fatalf("after the reset typed %v", got)
	}
	rec := r.record()
	if len(rec.AutoContinues) != 1 || !rec.AutoContinues[0].Own {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
	if m := r.message(rec.AutoContinues[0].ClientID); m.Auto != wire.AutoAfterLimit || m.Text != LimitContinueText {
		t.Fatalf("the runtime's message: %+v", m)
	}
}

// «Не продолжать» leaves the chat held without going on; at the reset the hold ends quietly. «auto» brings the going
// on back, «now» goes on at once with the person's own word.
func TestLimitCommands(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	reset := r.hitLimit()

	if res := r.do(wire.LimitCmd{OpID: "l1", ID: r.id, Action: wire.LimitCancel}); res.R != wire.ResultAccepted {
		t.Fatalf("cancel: %+v", res)
	}
	if h := r.record().Limited; h == nil || h.ContinueAt != nil || h.ResetsAt == nil {
		t.Fatalf("cancelled hold: %+v", h)
	}
	if res := r.do(wire.LimitCmd{OpID: "l2", ID: r.id, Action: wire.LimitAuto}); res.R != wire.ResultAccepted {
		t.Fatalf("auto: %+v", res)
	}
	if h := r.record().Limited; h == nil || h.ContinueAt == nil {
		t.Fatalf("hold again: %+v", h)
	}
	r.do(wire.LimitCmd{OpID: "l3", ID: r.id, Action: wire.LimitCancel})

	typed := len(r.pane.calls)
	r.tickTo(reset.Add(wire.LimitJitterMax + time.Minute))
	if got := r.typedLines(typed); len(got) != 0 {
		t.Fatalf("a cancelled hold typed %v", got)
	}
	rec := r.record()
	if rec.Limited != nil || rec.Queue.Paused != nil || len(rec.AutoContinues) != 0 {
		t.Fatalf("after the reset: %+v %+v %+v", rec.Limited, rec.Queue.Paused, rec.AutoContinues)
	}
	if res := r.do(wire.LimitCmd{OpID: "l4", ID: r.id, Action: wire.LimitNow}); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
		t.Fatalf("limit on a chat not held: %+v", res)
	}
}

func TestLimitNowGoesOnAtOnce(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.hitLimit()
	typed := len(r.pane.calls)
	if res := r.do(wire.LimitCmd{OpID: "l1", ID: r.id, Action: wire.LimitNow}); res.R != wire.ResultAccepted {
		t.Fatalf("now: %+v", res)
	}
	r.tick(time.Second)
	if got := r.typedLines(typed); len(got) != 1 || got[0] != LimitContinueText {
		t.Fatalf("now typed %v", got)
	}
	rec := r.record()
	if rec.Limited != nil || len(rec.AutoContinues) != 0 {
		t.Fatalf("after now: %+v %+v", rec.Limited, rec.AutoContinues)
	}
	for _, m := range r.snap().Messages {
		if m.Auto != "" {
			t.Fatalf("the person's «now» is marked the runtime's: %+v", m)
		}
	}
}

// With the setting off the chat is held but does not go on by itself.
func TestLimitSettingOff(t *testing.T) {
	r := newRig(t)
	r.deps = func(d *Deps) { d.AutoContinue = func() (bool, bool) { return false, true } }
	r.rt = r.runtime()
	r.working("big job")
	r.hitLimit()
	if h := r.record().Limited; h == nil || h.ContinueAt != nil {
		t.Fatalf("hold with the setting off: %+v", h)
	}
}

// A reset already past (a stale time) does not send the chat round a loop: it goes on LimitRetryMin later.
func TestLimitStaleResetWaits(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.limitLine(r.clock.Now().Add(-time.Hour).Unix())
	r.idle()
	r.hook("stop")
	h := r.record().Limited
	if h == nil || h.ContinueAt == nil || *h.ContinueAt < r.clock.Now().Add(LimitRetryMin).UnixMilli() {
		t.Fatalf("hold on a stale reset: %+v", h)
	}
}

// A message held through a restart is still held, and goes on at the reset.
func TestLimitSurvivesARestart(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.hitLimit()
	h := r.record().Limited
	r.rt.Stop()
	r.restart()
	for range 10 {
		r.tick(time.Second)
	}
	got := r.record()
	if got.Limited == nil || *got.Limited.ContinueAt != *h.ContinueAt || got.Queue.Paused == nil || got.Queue.Paused.Reason != wire.PausedLimited {
		t.Fatalf("after the restart: %+v %+v", got.Limited, got.Queue.Paused)
	}
	typed := len(r.pane.calls)
	r.tickTo(time.UnixMilli(*h.ContinueAt).Add(time.Second))
	if lines := r.typedLines(typed); len(lines) != 1 || lines[0] != LimitContinueText {
		t.Fatalf("after the reset typed %v", lines)
	}
}

// A restart that finds the agent of a working chat exited (a shell in its pane) sends it the word to go on once; the
// queue resumes the agent for it.
func TestRestartCutTurnGoesOn(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.rt.Stop()
	r.machine.probes["s1"] = Probe{Session: true, Shell: true, Resumable: true}
	r.restart()
	for range 10 {
		r.tick(time.Second)
	}
	rec := r.record()
	if len(rec.AutoContinues) != 1 || rec.AutoContinues[0].Why != wire.AutoAfterRestart {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
	if m := r.message(rec.AutoContinues[0].ClientID); m.Auto != wire.AutoAfterRestart || m.Text != RestartContinueText {
		t.Fatalf("the runtime's message: %+v", m)
	}
	if len(r.launcher.resumed) == 0 {
		t.Fatalf("the agent was not resumed")
	}
	// Back up, it takes the word; the word goes once.
	r.machine.probes["s1"] = Probe{Session: true, Foreground: true}
	r.idle()
	for range 20 {
		r.tick(time.Second)
	}
	n := 0
	for _, m := range r.snap().Messages {
		if m.Auto == wire.AutoAfterRestart {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d words to go on", n)
	}
}

// A restart that finds the agent still running (tmux outlived the runtime) leaves it be.
func TestRestartWithTheAgentRunningPokesNothing(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.rt.Stop()
	r.restart()
	for range 10 {
		r.tick(time.Second)
	}
	if rec := r.record(); len(rec.AutoContinues) != 0 {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
}

// An idle chat is not poked after a restart, whatever its agent.
func TestRestartOfAnIdleChatPokesNothing(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.rt.Stop()
	r.machine.probes["s1"] = Probe{Session: true, Shell: true, Resumable: true}
	r.restart()
	for range 10 {
		r.tick(time.Second)
	}
	if rec := r.record(); len(rec.AutoContinues) != 0 {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
}

// A lost terminal the startup's resume does not bring back is resumed by the chat itself, after RestartResumeAfter.
func TestRestartLostTerminalResumesItself(t *testing.T) {
	r := newRig(t)
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: wire.ProviderClaude, Cwd: "/w",
		TranscriptPath: r.path, AgentSessionID: "sess-1"}}, nil)
	r.settle(wire.StateReady)
	r.agentTakes()
	r.send("m1", "big job", wire.ModeQueue)
	r.tick(time.Second)
	r.rt.Stop()
	r.machine.probes["s1"] = Probe{}
	delete(r.machine.panes, "s1")
	r.restart()
	r.tick(time.Second)
	if k := r.state(); k.K != wire.StateGone {
		t.Fatalf("state %+v", k)
	}
	if rec := r.record(); len(rec.AutoContinues) != 1 {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
	if len(r.launcher.resumed) != 0 {
		t.Fatalf("resumed before the startup had its chance")
	}
	r.tickTo(r.clock.Now().Add(RestartResumeAfter + time.Second))
	if len(r.launcher.resumed) != 1 {
		t.Fatalf("resumed %v", r.launcher.resumed)
	}
}

// With the setting off, a cut turn is left as it is.
func TestRestartSettingOff(t *testing.T) {
	r := newRig(t)
	r.deps = func(d *Deps) { d.AutoContinue = func() (bool, bool) { return true, false } }
	r.rt = r.runtime()
	r.working("big job")
	r.rt.Stop()
	r.machine.probes["s1"] = Probe{Session: true, Shell: true, Resumable: true}
	r.restart()
	for range 10 {
		r.tick(time.Second)
	}
	if rec := r.record(); len(rec.AutoContinues) != 0 {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
}

// A chat on a backend profile reads its usage limit off the vendor's words; the hold waits for the person, since the
// runtime never goes on by itself on a vendor's limit.
func TestProfileChatLimitIsHeldWithoutAutoContinue(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	if err := r.rt.call(context.Background(), r.id, func(c *conv, _ time.Time) {
		c.rec.Model = &wire.Model{ID: "glm-5.3", Profile: "glm"}
		c.openFeed()
	}); err != nil {
		t.Fatal(err)
	}
	r.agentTakes()
	r.send("m1", "big job", wire.ModeQueue)
	r.tick(time.Second)
	reset := r.clock.Now().Add(time.Hour).UTC().Truncate(time.Second)
	// Z.ai tells the reset in Beijing time.
	text := `API Error: 429 {"error":{"code":"1308","message":"Usage limit reached for 5 hour. Your limit will reset at ` +
		reset.In(time.FixedZone("", 8*3600)).Format("2006-01-02 15:04:05") + `"}}`
	r.line(`"type":"assistant","message":{"model":"<synthetic>","role":"assistant","stop_reason":"stop_sequence","type":"message","content":[{"type":"text","text":%q}]},"error":"rate_limit","isApiErrorMessage":true,"apiErrorStatus":429`, text)
	r.idle()
	r.hook("stop")
	h := r.record().Limited
	if h == nil || h.Window != wire.LimitSession || h.ResetsAt == nil || *h.ResetsAt != reset.UnixMilli() {
		t.Fatalf("the hold: %+v", h)
	}
	if h.ContinueAt != nil {
		t.Fatalf("a vendor's limit goes on by itself: %+v", h)
	}
	typed := len(r.pane.calls)
	r.tickTo(reset.Add(wire.LimitJitterMax + time.Minute))
	for _, l := range r.typedLines(typed) {
		if l == LimitContinueText {
			t.Fatalf("typed %q", l)
		}
	}
	if r.record().Limited != nil {
		t.Fatalf("the hold ends quietly at the reset")
	}
}

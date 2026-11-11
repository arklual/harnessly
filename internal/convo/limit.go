package convo

import (
	"crypto/sha256"
	"encoding/binary"
	"time"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The words the runtime writes to an agent to go on by itself: after its usage limit reset, after a restart of the
// runtime cut its turn short.
const (
	LimitContinueText   = "Продолжай с того места, где остановился"
	RestartContinueText = "Продолжай — работа была прервана перезапуском"
)

// LimitRetryMin is how soon at the earliest a chat held on its limit goes on by itself: a reset the agent names that
// is already past (a stale one) must not send it round a loop of refusals.
const LimitRetryMin = 5 * time.Minute

// LimitScreenFresh is how recent the end of the last turn must be for Codex's notice on the screen to hold the chat:
// an old notice still on the screen after a restart is history.
const LimitScreenFresh = 5 * time.Minute

// RestartResumeAfter is how long a chat a restart cut short waits for the startup's resume to bring its terminal back
// before it resumes the agent itself.
const RestartResumeAfter = 45 * time.Second

func (c *conv) autoOn() (limit, restart bool) {
	limit, restart = true, true
	if c.d.AutoContinue != nil {
		limit, restart = c.d.AutoContinue()
	}
	// A vendor's limit has no reset the runtime can trust to go on by itself at: the person goes on.
	return limit && !c.onProfile(), restart
}

// limitEnd turns the end of a turn into a failure on the usage limit when its last line is the agent's notice of it,
// or when it failed while the limits say the plan ran out (the composer's «5ч 0%»): Claude fires StopFailure before
// its refusal is on the transcript, and the hook does not say why the turn failed.
func (c *conv) limitEnd(id string, st wire.TurnStatus, failure *wire.TurnFailure, now time.Time) (wire.TurnStatus, *wire.TurnFailure, *feed.Limit) {
	if st == wire.TurnInterrupted {
		return st, failure, nil
	}
	l, detail, ok := c.fd.LimitEnd(id)
	if !ok {
		if l, ok = c.exhausted(st, failure, now); !ok {
			return st, failure, nil
		}
	}
	return wire.TurnFailed, limitFailure(l, detail), &l
}

// limitFailure is a turn's failure on the usage limit l, with the notice's words.
func limitFailure(l feed.Limit, detail string) *wire.TurnFailure {
	f := &wire.TurnFailure{Code: wire.TurnFailedRateLimit}
	if detail != "" {
		f.Detail = &detail
	}
	if l.ResetsAt > 0 {
		at := l.ResetsAt
		f.RetryAt = &at
	}
	return f
}

// exhausted is the limit a turn that failed for no reason it names ran into: the limits the plan last reported say it
// ran out and reset later. A vendor's limits are not the plan's; a failure with a cause of its own (the context, the
// agent gone) is that cause.
func (c *conv) exhausted(st wire.TurnStatus, failure *wire.TurnFailure, now time.Time) (feed.Limit, bool) {
	if st != wire.TurnFailed || c.d.LimitReset == nil || c.onProfile() {
		return feed.Limit{}, false
	}
	if failure != nil && failure.Code != wire.TurnFailedUnknown && failure.Code != wire.TurnFailedApiError &&
		failure.Code != wire.TurnFailedRateLimit {
		return feed.Limit{}, false
	}
	reset := c.d.LimitReset(c.rec.Provider, now)
	if reset <= ms(now) {
		return feed.Limit{}, false
	}
	return feed.Limit{Window: wire.LimitOther, ResetsAt: reset}, true
}

// lateLimit holds a chat whose failed turn shows its limit only now: the refusal came onto the transcript after the
// StopFailure ended the turn. The turn's failure is named again, and the chat waits for the reset.
func (c *conv) lateLimit(now time.Time) {
	if c.rec.Limited != nil || c.rec.State.K == wire.StateWorking {
		return
	}
	t := c.fd.LastTurn()
	if t == nil || t.Status != wire.TurnFailed || t.ID == c.terminalFailureTurn ||
		t.Failure != nil && t.Failure.Code == wire.TurnFailedRateLimit {
		return
	}
	if t.EndedAt == nil || now.Sub(time.UnixMilli(*t.EndedAt)) > LimitScreenFresh {
		return
	}
	for _, a := range c.rec.AutoContinues {
		if a.At > *t.EndedAt {
			return // the chat went on after that turn
		}
	}
	l, detail, ok := c.fd.LimitEnd(t.ID)
	if !ok {
		return
	}
	if ended, ok := c.fd.Refail(t.ID, limitFailure(l, detail)); ok {
		c.emit(wire.TurnFailedEv{Turn: ended})
	}
	c.hold(l, now)
	c.lastTurn()
}

// hold keeps the chat on its usage limit: the queue waits for the reset, and the runtime goes on by itself then
// (unless the setting is off or no reset time is known).
func (c *conv) hold(l feed.Limit, now time.Time) {
	h := &wire.LimitHold{Provider: c.rec.Provider, Window: l.Window, At: ms(now)}
	reset := l.ResetsAt
	if reset <= ms(now) && c.d.LimitReset != nil && !c.onProfile() {
		if r := c.d.LimitReset(c.rec.Provider, now); r > reset {
			reset = r
		}
	}
	if reset > 0 {
		h.ResetsAt = &reset
	}
	if on, _ := c.autoOn(); on {
		h.ContinueAt = c.continueAt(h, now)
	}
	c.rec.Limited = h
	c.touch()
	c.change(c.box.Pause(wire.PausedLimited, now), now)
}

// terminalLimit is the plan's usage limit behind a terminal failure, when its words name the reset: a chat stopped on it
// has no other model to wait for, it waits for the reset as any chat held on its limit does. A vendor's quota and a
// failure that names no reset stay with the person's choice.
func (c *conv) terminalLimit() (feed.Limit, bool) {
	f, p := c.rec.TerminalFailure, c.box.Paused()
	if f == nil || p == nil || p.Reason != wire.PausedNeedsTarget || c.rec.Provider != wire.ProviderClaude || c.onProfile() {
		return feed.Limit{}, false
	}
	l, ok := feed.ClaudeLimitText(f.Detail, time.UnixMilli(f.At))
	return l, ok && l.ResetsAt > 0
}

// continueAt is when a hold goes on by itself: the reset and a jitter of the conversation's own, so the chats held on
// one limit do not all start at once; LimitRetryMin from now at the earliest.
func (c *conv) continueAt(h *wire.LimitHold, now time.Time) *wire.Ms {
	if h.ResetsAt == nil {
		return nil
	}
	sum := sha256.Sum256([]byte(c.rec.ID))
	spread := wire.LimitJitterMax.Milliseconds() / 4 * 3
	at := *h.ResetsAt + wire.LimitJitterMax.Milliseconds()/4 + int64(binary.BigEndian.Uint32(sum[:4]))%spread
	if earliest := ms(now) + LimitRetryMin.Milliseconds(); at < earliest && *h.ResetsAt <= ms(now) {
		at = earliest
	}
	return &at
}

// limitDue goes on with a held chat whose time came: by itself at ContinueAt; at the reset without a word of its own
// when it is not to go on by itself, so what the person queued meanwhile goes.
func (c *conv) limitDue(now time.Time) {
	if l, ok := c.terminalLimit(); ok {
		c.rec.TerminalFailure = nil
		c.hold(l, now)
	}
	h := c.rec.Limited
	switch {
	case h == nil:
	case h.ContinueAt != nil && ms(now) >= *h.ContinueAt:
		c.goOn(true, now)
	case h.ContinueAt == nil && h.ResetsAt != nil && ms(now) >= *h.ResetsAt+wire.LimitJitterMax.Milliseconds():
		c.release(now)
	}
}

// limitWake is when the hold is next due, zero for none.
func (c *conv) limitWake() time.Time {
	if _, ok := c.terminalLimit(); ok {
		return time.UnixMilli(c.rec.TerminalFailure.At)
	}
	h := c.rec.Limited
	switch {
	case h == nil:
	case h.ContinueAt != nil:
		return time.UnixMilli(*h.ContinueAt)
	case h.ResetsAt != nil:
		return time.UnixMilli(*h.ResetsAt + wire.LimitJitterMax.Milliseconds())
	}
	return time.Time{}
}

// goOn ends the hold and sends the work on: the person's queued message when one waits, else the runtime's word to go
// on. auto: the runtime does it by itself (the feed marks it); otherwise the person asked, and the word is theirs.
func (c *conv) goOn(auto bool, now time.Time) {
	head := c.release(now)
	if head == "" {
		why := wire.AutoContinueWhy("")
		if auto {
			why = wire.AutoAfterLimit
		}
		head = c.autoSend(LimitContinueText, why, now)
	}
	if auto && head != "" {
		c.logAuto(wire.AutoAfterLimit, head, now)
	}
	c.pump(now)
}

// release ends the hold: the queue paused for it goes on. The head of the queue then, "" for none.
func (c *conv) release(now time.Time) string {
	if c.rec.Limited == nil {
		return ""
	}
	c.rec.Limited = nil
	c.touch()
	if p := c.box.Paused(); p != nil && p.Reason == wire.PausedLimited {
		ch, _ := c.box.Resume(now)
		c.change(ch, now)
	}
	head := c.box.Head()
	c.pump(now)
	return head
}

// autoSend queues the runtime's own message; its client id, "" when the queue refused it.
func (c *conv) autoSend(text string, why wire.AutoContinueWhy, now time.Time) string {
	d := outbox.Draft{ClientID: "auto-" + newID(), Text: text, Mode: wire.ModeQueue, Kind: wire.KindPrompt,
		Attachments: []wire.AttachmentRef{}, Auto: why}
	ch, code := c.box.Send(d, c.rec.State, now)
	if code != "" {
		c.d.Log.Warn("the word to go on was not queued", "conv", c.rec.ID, "code", code)
		return ""
	}
	c.change(ch, now)
	return d.ClientID
}

// logAuto notes a time the runtime went on by itself, the newest AutoContinueMax kept.
func (c *conv) logAuto(why wire.AutoContinueWhy, clientID string, now time.Time) {
	m, _ := c.box.Message(clientID)
	c.rec.AutoContinues = append(c.rec.AutoContinues, wire.AutoContinue{At: ms(now), Why: why, ClientID: clientID, Own: m.Auto != ""})
	if n := len(c.rec.AutoContinues); n > wire.AutoContinueMax {
		c.rec.AutoContinues = append([]wire.AutoContinue(nil), c.rec.AutoContinues[n-wire.AutoContinueMax:]...)
	}
	c.touch()
}

// limitCmd is the person's word on a held chat.
func (c *conv) limitCmd(m wire.LimitCmd, now time.Time) wire.CmdResult {
	h := c.rec.Limited
	if h == nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	switch m.Action {
	case wire.LimitNow:
		c.goOn(false, now)
	case wire.LimitCancel:
		if h.ContinueAt == nil {
			return c.recResult(now)
		}
		held := *h
		held.ContinueAt = nil
		c.rec.Limited = &held
	case wire.LimitAuto:
		held := *h
		held.ContinueAt = c.continueAt(&held, now)
		c.rec.Limited = &held
	}
	return c.recResult(now)
}

// limitOnScreen holds a Codex chat whose turn just ended on the usage limit by its screen: Codex may keep its notice
// off the transcript.
func (c *conv) limitOnScreen(plain string, now time.Time) {
	if c.rec.Provider != wire.ProviderCodex || c.rec.Limited != nil || c.onProfile() {
		return
	}
	k := c.rec.State.K
	if k != wire.StateReady && k != wire.StateIdle {
		return
	}
	line := screen.CodexLimitLine(plain)
	if line == "" || line == c.limitLine {
		return
	}
	t := c.rec.LastTurn
	if t == nil || t.EndedAt == nil || now.Sub(time.UnixMilli(*t.EndedAt)) > LimitScreenFresh {
		return
	}
	for _, a := range c.rec.AutoContinues {
		if a.At > *t.EndedAt {
			return // the chat went on after that turn: the notice is older than the work since
		}
	}
	c.limitLine = line
	c.hold(feed.CodexLimitText(line, time.UnixMilli(*t.EndedAt)), now)
}

// --- a restart that cut a turn short ---

// cutAtRestart: the runtime came back to a conversation that was at work when it went down. Its agent still running
// (tmux outlived the runtime) goes on by itself; one that is not was cut short, and goes on once it is back.
func (c *conv) cutAtRestart(was wire.AgentState, now time.Time) {
	// A chat that was stopped is not cut short; it is remembered as stopped for the look that finds it without a
	// session (see seeing).
	c.stoppedAtRestart, c.resumableAtRestart = was.K == wire.StateStopped, was.K == wire.StateStopped && was.Resumable
	if !hotAtStop(was.K) || c.rec.Session == "" || c.d.Machine == nil {
		c.ownAtRestart(was, true)
		return
	}
	pr := c.d.Machine.Probe(c.rec.Session)
	running := pr.Session && pr.Foreground
	c.ownAtRestart(was, running)
	if was.K != wire.StateWorking || running {
		return
	}
	c.restartCut, c.restartSent, c.restartAt = true, false, now
}

// restartDue is what a start owes a chat whose agent was hot when the runtime went down, once the agent has been
// looked at: the word to go on, and the agent itself (life.go).
func (c *conv) restartDue(now time.Time) {
	c.restartWord(now)
	c.startUpDue(now)
}

// restartWord sends the word to go on to a chat a restart cut short: the queue brings the agent back (a stopped one
// is resumed; with Deps.LegacyLife a lost terminal comes back with the tabs' startup resume, or after
// RestartResumeAfter by the chat's own).
func (c *conv) restartWord(now time.Time) {
	if !c.restartCut {
		return
	}
	k := c.rec.State.K
	if k == wire.StateUnknown || k == wire.StateStarting {
		return // not looked at yet, or on its way back
	}
	if _, on := c.autoOn(); !on || k == wire.StateGone && !c.lostTmux() || k == wire.StateLaunchFailed {
		c.restartCut = false
		return
	}
	if !c.restartSent {
		c.restartSent = true
		if id := c.autoSend(RestartContinueText, wire.AutoAfterRestart, now); id != "" {
			c.logAuto(wire.AutoAfterRestart, id, now)
		}
	}
	if k != wire.StateGone {
		c.restartCut = false
		c.pump(now)
		return
	}
	if now.Sub(c.restartAt) >= RestartResumeAfter {
		// The terminal is still not back: the chat resumes its agent itself.
		c.restartCut = false
		if c.rec.AgentSessionID != "" || c.rec.TranscriptPath != "" {
			c.resume(now)
		}
	}
}

// restartWake is when a chat a restart cut short next looks at its lost terminal, zero for none.
func (c *conv) restartWake() time.Time {
	if !c.restartCut {
		return time.Time{}
	}
	return c.restartAt.Add(RestartResumeAfter)
}

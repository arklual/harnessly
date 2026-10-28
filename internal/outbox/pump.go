package outbox

import (
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// headPlan is what the agent lets the head of the queue do now.
type headPlan struct {
	reason wire.QueueReason
	typing bool
	steer  bool
	pause  wire.PauseReason
	resume bool
}

// plan is §4.3's table: what the agent's state makes of the head of the queue.
func (b *Box) plan(a Agent, head *Record) headPlan {
	if head.Stalled {
		// The agent did not take it once: the person says when to try again.
		return headPlan{reason: wire.QueuedStalled}
	}
	if b.paused != nil {
		return headPlan{reason: wire.QueuedPaused}
	}
	switch st := a.State; st.K {
	case wire.StateStarting:
		return headPlan{reason: wire.QueuedLaunching}
	case wire.StateStopped:
		if st.Resumable {
			return headPlan{reason: wire.QueuedResuming, resume: true}
		}
		return headPlan{reason: wire.QueuedPaused, pause: wire.PausedAgentGone}
	case wire.StateGone:
		return headPlan{reason: wire.QueuedPaused, pause: wire.PausedAgentGone}
	case wire.StateLaunchFailed:
		return headPlan{reason: wire.QueuedPaused, pause: wire.PausedLaunchFailed}
	case wire.StateWorking:
		if head.Message.Mode == wire.ModeSteer {
			return headPlan{reason: wire.QueuedAgentBusy, typing: true, steer: true}
		}
		return headPlan{reason: wire.QueuedAgentBusy}
	case wire.StateWaitingPermission:
		// A permission menu takes keys as its answer: nothing is ever typed into it.
		return headPlan{reason: wire.QueuedBlockedMenu}
	case wire.StateWaitingQuestion, wire.StateIdle, wire.StateReady:
		// Text written into an open question is the person's answer in words: the typist chats about it first.
		return headPlan{reason: wire.QueuedAgentBusy, typing: true}
	case wire.StateUnknown:
		// Not checked yet is not idle: only an agent seen in the foreground (not a shell) is typed into.
		return headPlan{reason: wire.QueuedAgentBusy, typing: a.Foreground}
	}
	return headPlan{reason: wire.QueuedAgentBusy}
}

// Pump looks at the head of the queue on any change — the agent's state, a new message, the clock — and returns the
// try to type when the agent can take it. size is the transcript's length now: the message's user entry, if it
// takes, is written after it. One message is on its way at a time.
func (b *Box) Pump(a Agent, size int64, now time.Time) (*Job, Change) {
	ch := b.Tick(now)
	if job := b.clearJob(a, now); job != nil {
		return job, ch
	}
	if r := b.recs[b.flight]; r != nil && r.EnterOnly && !b.typing && b.paused == nil {
		b.typing = true
		return &Job{ClientID: r.Message.ClientID, Attempt: r.Message.Attempts,
			Message: typist.Message{Text: r.Message.Text, Images: r.Images, EnterOnly: true, Leftovers: b.pasted()}}, ch
	}
	if r := b.rechecked(now); r != nil && a.Screen != nil {
		// A steered message is Entered into a working agent too: Claude queues it, as it did not when it was typed.
		msg := typist.Message{Text: r.Message.Text, Images: r.Images, EnterOnly: true, Steer: r.Steered, Leftovers: b.pasted()}
		p := typist.Decide(msg, *a.Screen, a.Provider)
		if r.Steered && p.OwnText && screen.InterruptedLast(a.Screen.Plain) {
			// The screen says the turn was stopped and the text is back in the box: the person's to edit, not to send.
			ch.Events = append(ch.Events, b.Interrupted(now).Events...)
			return nil, ch
		}
		if p.Decision == typist.DecideSend && p.OwnText {
			r.Rechecks++
			r.RecheckAt = ms(now) + recheckEvery.Milliseconds()
			r.recheck, b.typing, b.flight = true, true, r.Message.ClientID
			if b.log != nil {
				b.log.Info("message found in the input box; Enter pressed again", "conversationId", b.conv,
					"clientId", r.Message.ClientID, "steered", r.Steered, "recheck", r.Rechecks)
			}
			return &Job{ClientID: r.Message.ClientID, Attempt: r.Message.Attempts, Message: msg}, ch
		}
		r.RecheckAt = ms(now) + r.recheckPace().Milliseconds()
	}
	if len(b.items) == 0 {
		return nil, ch
	}
	head := b.recs[b.items[0]]
	p := b.plan(a, head)
	if p.pause != "" {
		pc := b.Pause(p.pause, now)
		ch.Events = append(ch.Events, pc.Events...)
	}
	if p.resume && !b.resumed {
		b.resumed, ch.Resume = true, true
	}
	if !p.resume {
		b.resumed = false
	}
	switch {
	case b.flight != "":
		// One message on its way at a time: the rest wait for it, and that is no new reason — the head keeps the one
		// the agent's state gives it, so a queued message never swings between «agent busy» and «behind» (position 1
		// is the head; «behind» is only for those with a message of the queue ahead of them).
		b.positions(&ch, p.reason, now)
		return nil, ch
	case !p.typing:
		b.positions(&ch, p.reason, now)
		return nil, ch
	case head.NextTry > ms(now):
		b.positions(&ch, head.Reason, now) // looked at again later, on the reason of the last look
		return nil, ch
	}
	if head.TypedAt > 0 && b.Transcript != nil {
		// Typed before and back in the line (a try that seemed lost, a retry): the agent may have it after all — an Enter
		// that came late, a key pressed in the terminal. Its entry in the transcript says so, and it is never typed twice.
		if id, ok := b.Transcript(head.TypedOffset, head.TypedAt, head.Message.Text); ok {
			b.dropItem(head.Message.ClientID)
			b.accept(&ch, head, &id, now)
			ch.add(wire.QueueChangedEv{Queue: b.Queue()})
			b.positions(&ch, p.reason, now)
			return nil, ch
		}
	}
	msg := typist.Message{Text: head.Message.Text, Images: head.Images, Steer: p.steer, HandOver: p.steer && a.HandOver,
		Leftovers: b.pasted()}
	if a.Screen != nil {
		switch typist.Decide(msg, *a.Screen, a.Provider).Decision {
		case typist.DecideBlockedMenu:
			b.blocked(&ch, head, wire.QueuedBlockedMenu, now)
			return nil, ch
		case typist.DecideBlockedText:
			b.blocked(&ch, head, wire.QueuedBlockedText, now)
			return nil, ch
		case typist.DecideWait:
			head.NextTry = ms(now) + head.waitFor().Milliseconds()
			b.positions(&ch, wire.QueuedAgentBusy, now)
			return nil, ch
		}
	}
	b.items = b.items[1:]
	b.rev++
	b.flight, b.typing = head.Message.ClientID, true
	head.TypedOffset, head.TypedAt, head.Submitted, head.Steered = size, ms(now), false, p.steer
	deadline := wire.TypingDeadline + int64(len(head.Images))*1500
	b.set(&ch, head, wire.MessageState{K: wire.MsgTyping, Wait: &wire.Wait{SlowAt: ms(now) + wire.WaitSlowMs, DeadlineAt: ms(now) + deadline}}, now)
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	b.positions(&ch, p.reason, now)
	return &Job{ClientID: head.Message.ClientID, Attempt: head.Message.Attempts, Message: msg}, ch
}

// pasted is every picture path a try of this box pasted, of the messages it still keeps: a paste Claude read too late
// for its try lands as path text afterwards, and is the harness's to clear, whichever message it was for.
func (b *Box) pasted() []string {
	var paths []string
	for _, r := range b.sorted() {
		if r.TypedAt > 0 || r.Message.Attempts > 0 {
			paths = append(paths, r.Images...)
		}
	}
	return paths
}

// recheckDue: the message waits in-input and its box is to be looked at for it now.
func (b *Box) recheckDue(r *Record, now time.Time) bool {
	return !b.typing && b.paused == nil && r.Message.State.K == wire.MsgInInput && r.Rechecks < maxRechecks &&
		r.RecheckAt > 0 && ms(now) >= r.RecheckAt
}

// rechecked is the in-input message whose box is to be looked at now: the one on its way, or — the line free — the
// oldest steered one. A steered message left the line once its Enter seemed to go in, but Claude may not have
// queued it (the Enter came as the turn ended, or went into the text as a newline): its text then sits in the box,
// unsubmitted, «in the agent's queue» on the page and in the way of every message after it.
func (b *Box) rechecked(now time.Time) *Record {
	if r := b.recs[b.flight]; r != nil {
		if b.recheckDue(r, now) {
			return r
		}
		return nil
	}
	for _, r := range b.sorted() {
		if r.Steered && b.recheckDue(r, now) {
			return r
		}
	}
	return nil
}

// recheckPace: a steered message waits in Claude's queue for minutes, its box empty; it is looked for less often.
func (r *Record) recheckPace() time.Duration {
	if r.Steered {
		return steerRecheckEvery
	}
	return recheckEvery
}

// NeedsLook says the box wants a look at the pane even with nothing queued: a message in-input is due its recheck.
func (b *Box) NeedsLook(now time.Time) bool {
	if b.rechecked(now) != nil {
		return true
	}
	for _, r := range b.recs {
		if r.ClearBox {
			return true
		}
	}
	return false
}

// blocked: a menu or someone's text stops the head; it is looked at again 1.5 → 3 → 6 → 10 s later.
func (b *Box) blocked(ch *Change, r *Record, why wire.QueueReason, now time.Time) {
	r.Blocked++
	r.NextTry = ms(now) + min(retryBase<<(r.Blocked-1), blockedMax).Milliseconds()
	b.positions(ch, why, now)
}

// positions gives every queued message its place: the head waits on reason, the rest behind it.
func (b *Box) positions(ch *Change, reason wire.QueueReason, now time.Time) {
	for i, id := range b.items {
		r := b.recs[id]
		why := wire.QueuedBehind
		if r.Stalled {
			// Wherever a message that jumped ahead put it, a stalled message waits for the person.
			why = wire.QueuedStalled
		} else if i == 0 && reason == wire.QueuedStalled {
			// The head was stalled and is gone: the one now first waits on the agent until the next look says more.
			why = wire.QueuedAgentBusy
		} else if i == 0 {
			why = reason
		} else if r.Reason == wire.QueuedAgentBusy {
			// Pushed down the line by a message that jumped ahead («now»): it still waits on the agent, it does not go
			// back to being «behind» only to come up again.
			why = wire.QueuedAgentBusy
		}
		if r.Reason != why {
			r.Reason, r.WaitSince = why, ms(now)
		}
		term := termOf(why, r.WaitSince)
		if why == wire.QueuedPaused && b.paused != nil && b.paused.Reason == wire.PausedLimited {
			// Held on the usage limit, a message waits for the reset however far off: the person sent it for then.
			term = nil
		}
		st := wire.MessageState{K: wire.MsgQueued, Position: i + 1, Reason: why, Wait: term}
		if why == wire.QueuedStalled {
			st.At = r.Message.State.At // the moment of the stall, for the older form (failed{unconfirmed, at})
			if r.Message.State.Reason != wire.QueuedStalled {
				st.At = ms(now)
			}
		}
		b.set(ch, r, st, now)
		b.waiting(id, string(why))
	}
}

// termOf is the term of a queued reason: the agent's own work and the place behind another have none.
func termOf(why wire.QueueReason, since wire.Ms) *wire.Wait {
	switch why {
	case wire.QueuedAgentBusy, wire.QueuedBehind, wire.QueuedStalled:
		return nil
	case wire.QueuedBlockedText:
		return wait(since, OccupiedTTL)
	}
	return wait(since, QueuedTTL)
}

// Typed takes the outcome of a try. Sent waits in-input for the agent to accept it; a screen that said wait or
// blocked puts the message back at the head to be looked at again (a block backs off); a key that did not go in
// counts a failed try, MaxTries in a row fail the message; text stuck in the box fails it, retryable.
func (b *Box) Typed(clientID string, res typist.Result, now time.Time) Change {
	var ch Change
	r := b.recs[clientID]
	if r != nil && clientID == b.clearing {
		b.clearing, b.typing, r.ClearBox = "", false, false
		if b.log != nil {
			b.log.Info("stalled message taken out of the input box", "conversationId", b.conv, "clientId", clientID,
				"cleared", res.Outcome == typist.Sent)
		}
		return ch
	}
	if r == nil || clientID != b.flight {
		return ch
	}
	b.typing = false
	if res.Cleared != "" && r.Message.Cleared == "" {
		// The chat says under the message that the person's screen in the terminal was closed for it.
		r.Message.Cleared = wire.TerminalAside(res.Cleared)
		r.Message.Rev++
		ch.add(wire.MessageUpsertEv{Message: r.Message})
	}
	if r.recheck {
		// Its Enter again for a message in-input: the message is still waiting for the agent. A try that pressed it
		// restarts the wait; one that did not (the screen changed, a key failed) leaves it be; one whose Enters all
		// left the text in the box is typed again from the head (the typist took the text out, so nothing is left there
		// for the next one), MaxTries in all before it fails.
		r.recheck = false
		if r.Steered {
			b.flight = ""
		}
		switch {
		case res.Outcome == typist.Stuck:
			b.stuck(&ch, r, now)
		case res.Outcome == typist.Sent && res.Typed && r.Steered:
			r.TypedAt = ms(now)
			b.set(&ch, r, wire.MessageState{K: wire.MsgInInput, Wait: wait(r.TypedAt, SteerDeadline)}, now)
		case res.Outcome == typist.Sent && res.Typed:
			// An Enter pressed again does not move the term: a message the agent does not take in ConfirmDeadline from
			// its typing stalls, however many Enters it got.
			r.TypedAt = ms(now)
		}
		return ch
	}
	enterOnly := r.EnterOnly
	r.EnterOnly = false
	switch res.Outcome {
	case typist.Sent:
		if res.AsPath && b.log != nil {
			b.log.Warn("picture sent as a path", "conversationId", b.conv, "clientId", clientID)
		}
		r.Tries, r.Blocked, r.Rechecks, r.Waits = 0, 0, 0, 0
		if !enterOnly || res.Typed {
			r.TypedAt = ms(now)
		}
		r.RecheckAt = ms(now) + recheckEvery.Milliseconds()
		if r.Steered && !r.Submitted {
			// In Claude's queue, not read: the line is free for the next message — the entry that says it was read
			// comes whenever the agent's step ends. The box is still looked at for it now and then: an Enter Claude did
			// not take leaves the text there, and its Enter is pressed again.
			b.flight = ""
			b.set(&ch, r, wire.MessageState{K: wire.MsgInInput, Wait: wait(r.TypedAt, SteerDeadline)}, now)
			return ch
		}
		if r.Submitted {
			b.accept(&ch, r, nil, now)
			return ch
		}
		b.set(&ch, r, wire.MessageState{K: wire.MsgInInput, Wait: wait(r.TypedAt, wire.ConfirmDeadline*time.Millisecond)}, now)
		return ch
	case typist.Stuck:
		b.stuck(&ch, r, now)
		return ch
	case typist.Failed:
		r.Tries++
		r.Message.Attempts++
		if r.Tries >= MaxTries {
			b.flight = ""
			b.fail(&ch, r, wire.MsgFailedTypeFailed, true, now)
			return ch
		}
		r.NextTry = ms(now) + min(retryBase<<(r.Tries-1), retryMax).Milliseconds()
		if b.log != nil {
			b.log.Warn("message did not reach the agent; it stays queued", "conversationId", b.conv, "clientId", clientID,
				"try", r.Tries, "nextMs", r.NextTry-ms(now), "err", errText(res.Err))
		}
	case typist.BlockedMenu, typist.BlockedText:
		r.Blocked++
		r.NextTry = ms(now) + min(retryBase<<(r.Blocked-1), blockedMax).Milliseconds()
	default:
		r.NextTry = ms(now) + r.waitFor().Milliseconds()
	}
	if enterOnly {
		// The restart's own text is no longer in the box: whether it went in is the transcript's to say; a try that
		// failed outright starts over at the head.
		if res.Outcome == typist.Wait || res.Outcome == typist.BlockedMenu {
			r.EnterOnly = true
			return ch
		}
	}
	b.flight = ""
	b.items = append([]string{clientID}, b.items...)
	b.rev++
	reason := wire.QueuedAgentBusy
	switch res.Outcome {
	case typist.BlockedMenu:
		reason = wire.QueuedBlockedMenu
	case typist.BlockedText:
		reason = wire.QueuedBlockedText
	}
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	b.positions(&ch, reason, now)
	return ch
}

// stuck: a try's Enters all left the text in the box, and the typist took it out. It is typed again from the head of
// the line, backing off like a failed try; MaxTries in a row fail it, retryable.
func (b *Box) stuck(ch *Change, r *Record, now time.Time) {
	b.flight = ""
	r.Tries++
	r.Message.Attempts++
	if r.Tries >= MaxTries {
		b.fail(ch, r, wire.MsgFailedStuckInInput, true, now)
		return
	}
	r.NextTry = ms(now) + min(retryBase<<(r.Tries-1), retryMax).Milliseconds()
	if b.log != nil {
		b.log.Warn("message stuck in the input box; it is typed again", "conversationId", b.conv,
			"clientId", r.Message.ClientID, "try", r.Tries, "nextMs", r.NextTry-ms(now))
	}
	if !slices.Contains(b.items, r.Message.ClientID) {
		b.items = append([]string{r.Message.ClientID}, b.items...)
		b.rev++
	}
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	b.positions(ch, wire.QueuedAgentBusy, now)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// PromptSubmitted is the agent's prompt-submit hook: the message in input is accepted (its entry not read yet). turn
// is the running turn, if any: a steered message joins it. A steered message is not: the hook fires when the Enter
// reaches Claude's queue, and the agent reads the message only at its next step. working says the agent was already
// in a turn when the hook came: a normal message typed just as the agent began a turn of its own (a background task's
// notice, a timer) is in Claude's queue the same way, not read, and is told so — see handOver.
func (b *Box) PromptSubmitted(turn *string, working bool, now time.Time) Change {
	var ch Change
	r := b.recs[b.flight]
	switch {
	case r == nil:
	case r.Steered:
	case working && (b.typing || r.Message.State.K == wire.MsgInInput):
		b.handOver(&ch, r, now)
	case b.typing:
		r.Submitted = true
	case r.Message.State.K == wire.MsgInInput:
		b.accept(&ch, r, nil, now)
		b.join(&ch, r, turn, now)
	}
	return ch
}

// handOver makes a normal message what a steered one is: typed into a turn that was already running, it sits in
// Claude's own queue and is read at the agent's next step, so it is not accepted by the hook but waits in-input for
// its entry (the queued_command attachment of the transcript, or the turn that Claude starts for it), past the term
// of an ordinary confirmation, and does not hold the line. The pages are told by the whole message, for its mode
// changed; an old page knows a steer already.
func (b *Box) handOver(ch *Change, r *Record, now time.Time) {
	r.Steered = true
	if r.RecheckAt == 0 {
		r.RecheckAt = ms(now) + recheckEvery.Milliseconds()
	}
	r.Message.Mode = wire.ModeSteer
	if r.Message.State.K == wire.MsgInInput { // still typing: the outcome of the try sets the state
		r.Message.State = wire.MessageState{K: wire.MsgInInput, Wait: wait(r.TypedAt, SteerDeadline)}
		if b.flight == r.Message.ClientID {
			b.flight = ""
		}
	}
	r.Message.Rev++
	ch.add(wire.MessageUpsertEv{Message: r.Message})
}

// UserEntry is UserEntryText for an entry whose text is not known: only the message on its way can take it.
func (b *Box) UserEntry(entryID string, offset int64, turn *string, now time.Time) (string, Change) {
	return b.UserEntryText(entryID, "", offset, turn, now)
}

// UserEntryText is a user entry read off the transcript at offset: the first one at or after the typed offset of the
// message on its way is that message's — it is accepted with the entry's id, and its client id is the answer, for
// the entry to carry. A message the hook accepted first gets its entry now. An entry no message on its way takes is
// looked at by its text: a message typed before that went back to the line or failed (its Enter seemed lost, and came
// later — Claude took the text after all) is accepted, not left to be typed a second time.
func (b *Box) UserEntryText(entryID, text string, offset int64, turn *string, now time.Time) (string, Change) {
	var ch Change
	for _, r := range b.sorted() {
		st := r.Message.State
		ours := r.Message.ClientID == b.flight && (st.K == wire.MsgInInput || st.K == wire.MsgTyping) ||
			// Claude reads its queue in order: the oldest steered message first — the one with this text, when it is known.
			st.K == wire.MsgInInput && r.Steered && (text == "" || takes(text, r.Message.Text)) ||
			st.K == wire.MsgAccepted && st.EntryID == nil
		if !ours || offset < r.TypedOffset {
			continue
		}
		if prefixed(text) && !takes(text, r.Message.Text) {
			// A command line in the transcript («/compact», «! ls») is the message that was typed as it, nobody else's.
			continue
		}
		if st.K == wire.MsgTyping {
			r.Submitted = true
			b.typing = false
		}
		b.cut(r, text)
		b.accept(&ch, r, &entryID, now)
		b.join(&ch, r, turn, now)
		return r.Message.ClientID, ch
	}
	if text == "" {
		return "", ch
	}
	for _, r := range b.sorted() {
		st := r.Message.State
		if st.K != wire.MsgQueued && st.K != wire.MsgFailed || r.TypedAt == 0 || offset < r.TypedOffset ||
			!takes(text, r.Message.Text) {
			continue
		}
		if b.log != nil {
			b.log.Info("message found in the transcript after all", "conversationId", b.conv, "clientId", r.Message.ClientID,
				"state", string(st.K))
		}
		b.cut(r, text)
		queued := slices.Contains(b.items, r.Message.ClientID)
		reason := r.Reason
		b.dropItem(r.Message.ClientID)
		b.accept(&ch, r, &entryID, now)
		b.join(&ch, r, turn, now)
		if queued {
			ch.add(wire.QueueChangedEv{Queue: b.Queue()})
			b.positions(&ch, reason, now) // the ones behind it move up, the head waits on what it waited on
		}
		return r.Message.ClientID, ch
	}
	return "", ch
}

// takes: a user entry with this text is this message's — all of it, or its start when Claude cut the paste short.
func takes(entry, text string) bool {
	return screen.SameMessage(entry, text) || screen.CutShort(entry, text)
}

// cut logs a message whose entry has only its start: the agent read part of it, the rest was lost in the paste.
func (b *Box) cut(r *Record, text string) {
	if b.log != nil && text != "" && screen.CutShort(text, r.Message.Text) {
		b.log.Warn("the agent took the message cut short", "conversationId", b.conv, "clientId", r.Message.ClientID,
			"took", len([]rune(text)), "of", len([]rune(r.Message.Text)))
	}
}

func (b *Box) accept(ch *Change, r *Record, entryID *string, now time.Time) {
	if b.flight == r.Message.ClientID {
		b.flight, b.typing = "", false
	}
	if entryID != nil {
		r.EntryID = *entryID
	}
	r.Stalled, r.ClearBox = false, false
	b.set(ch, r, wire.MessageState{K: wire.MsgAccepted, EntryID: entryID}, now)
	delete(b.waits, r.Message.ClientID)
}

// join puts a steered message into the running turn at once: no turn_started comes for it.
func (b *Box) join(ch *Change, r *Record, turn *string, now time.Time) {
	if turn != nil && r.Message.Mode == wire.ModeSteer {
		b.set(ch, r, wire.MessageState{K: wire.MsgInTurn, TurnID: *turn}, now)
	}
}

// TurnStarted moves the accepted messages into the turn that opened.
func (b *Box) TurnStarted(turnID string, now time.Time) Change {
	var ch Change
	for _, r := range b.sorted() {
		if r.Message.State.K == wire.MsgAccepted {
			b.set(&ch, r, wire.MessageState{K: wire.MsgInTurn, TurnID: turnID}, now)
		}
	}
	return ch
}

// Interrupted: the person stopped the turn (Escape in the terminal, stop from the page). Claude puts what waited in
// its own queue back into the input box for editing: a steered message not read yet is there now, pulled back on
// purpose, and is never Entered again by itself — it fails as returned-to-input, its retry (which finds the text in the
// box and presses only its Enter) and its removal the person's to choose.
func (b *Box) Interrupted(now time.Time) Change {
	var ch Change
	for _, r := range b.sorted() {
		if !r.Steered || r.Message.State.K != wire.MsgInInput || r.recheck {
			continue
		}
		if b.flight == r.Message.ClientID {
			b.flight = ""
		}
		b.fail(&ch, r, wire.MsgFailedReturned, true, now)
	}
	return ch
}

// TurnEnded ends the messages of a turn that finished, failed or was interrupted.
func (b *Box) TurnEnded(turnID string, now time.Time) Change {
	var ch Change
	for _, r := range b.sorted() {
		st := r.Message.State
		if st.K == wire.MsgInTurn && st.TurnID == turnID || st.K == wire.MsgAccepted && turnID != "" {
			b.set(&ch, r, wire.MessageState{K: wire.MsgDone, TurnID: turnID}, now)
		}
	}
	return ch
}

// Tick ends the waits past their terms: in-input past its term goes back to the line stalled (a steered or carried
// message, which may still be in Claude's queue or its launch, fails unconfirmed), a message left typing that long with
// no try on its way too; a queued message past the TTL of its reason is failed{expired}; final messages older than
// FinalStored are forgotten, their ids kept GoneKept.
func (b *Box) Tick(now time.Time) Change {
	var ch Change
	t := ms(now)
	for _, r := range b.sorted() {
		st := r.Message.State
		mine := b.typing && b.flight == r.Message.ClientID
		switch {
		case st.K == wire.MsgInInput && st.Wait != nil && t >= st.Wait.DeadlineAt && !mine && (r.Steered || r.Carried):
			if b.flight == r.Message.ClientID {
				b.flight = ""
			}
			b.fail(&ch, r, wire.MsgFailedUnconfirmed, true, now)
		case st.K == wire.MsgInInput && st.Wait != nil && t >= st.Wait.DeadlineAt && !mine,
			st.K == wire.MsgTyping && !mine && t-r.TypedAt >= wire.ConfirmDeadline:
			b.stall(&ch, r, now)
		case st.K == wire.MsgQueued && st.Wait != nil && t >= st.Wait.DeadlineAt:
			b.fail(&ch, r, wire.MsgFailedExpired, true, now)
			ch.add(wire.QueueChangedEv{Queue: b.Queue()})
		case final(st.K) && t-r.EndedAt > FinalStored.Milliseconds():
			delete(b.recs, r.Message.ClientID)
			delete(b.waits, r.Message.ClientID)
			b.forget(r.Message.ClientID, r.EndedAt)
		}
	}
	for id, at := range b.gone {
		if t-at > GoneKept.Milliseconds() {
			delete(b.gone, id)
		}
	}
	return ch
}

// forget keeps the id of a message let go, at most GoneMax of them (the oldest go first).
func (b *Box) forget(id string, endedAt wire.Ms) {
	b.gone[id] = endedAt
	for len(b.gone) > GoneMax {
		oldest, at := "", wire.Ms(0)
		for k, v := range b.gone {
			if oldest == "" || v < at || v == at && k < oldest {
				oldest, at = k, v
			}
		}
		delete(b.gone, oldest)
	}
}

// A stalled message's box is looked at for its text every clearAgain, clearLooks times at most, when no look came.
const (
	clearAgain = 1500 * time.Millisecond
	clearLooks = 5
)

// stall: a typed message the agent did not take within its term. The transcript is asked first (an Enter that came
// late is an answer); without its entry the message goes back to the head of the line, stalled — the pages show it in
// the queue to retry, never as a bubble that waits for ever — and its text is taken out of the agent's input box at
// the next look. It is not typed again by itself: something beyond this message is wrong, and the person decides.
func (b *Box) stall(ch *Change, r *Record, now time.Time) {
	if b.flight == r.Message.ClientID {
		b.flight, b.typing = "", false
	}
	if id, ok := b.found(r, nil); ok {
		b.accept(ch, r, &id, now)
		return
	}
	r.Stalled, r.ClearBox, r.ClearAt, r.ClearLooks = true, true, ms(now), 0
	r.EnterOnly, r.Rechecks, r.RecheckAt = false, 0, 0
	r.WaitSince = ms(now)
	if !slices.Contains(b.items, r.Message.ClientID) {
		b.items = append([]string{r.Message.ClientID}, b.items...)
		b.rev++
	}
	b.set(ch, r, wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedStalled, At: ms(now)}, now)
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	b.positions(ch, wire.QueuedStalled, now)
	if b.log != nil {
		b.log.Warn("message not taken by the agent; back in the queue, stalled", "conversationId", b.conv,
			"clientId", r.Message.ClientID, "attempts", r.Message.Attempts)
	}
}

// unstall is the person's word on a stalled message: one the agent took after all (its entry is in the transcript) is
// accepted, any other is typed again from the head of the line, attempts+1.
func (b *Box) unstall(r *Record, find Find, now time.Time) Change {
	var ch Change
	if r.TypedOffset > 0 || r.TypedAt > 0 {
		if id, ok := b.found(r, find); ok {
			b.dropItem(r.Message.ClientID)
			b.accept(&ch, r, &id, now)
			ch.add(wire.QueueChangedEv{Queue: b.Queue()})
			return ch
		}
	}
	r.Stalled, r.ClearBox = false, false
	r.Message.Attempts++
	r.Tries, r.NextTry, r.Blocked, r.Waits, r.WaitSince = 0, 0, 0, 0, ms(now)
	b.rev++
	b.set(&ch, r, wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedBehind}, now)
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	return ch
}

// clearJob is the try that takes a stalled message's text out of the agent's input box, when the look shows it there;
// a look that does not ends the clearing (nothing of it is in the box). Without a look it is asked again a few times,
// then let be: the box is the person's to see in the terminal.
func (b *Box) clearJob(a Agent, now time.Time) *Job {
	if b.typing {
		return nil
	}
	for _, r := range b.sorted() {
		if !r.ClearBox || ms(now) < r.ClearAt {
			continue
		}
		if a.Screen == nil {
			r.ClearLooks++
			r.ClearAt = ms(now) + clearAgain.Milliseconds()
			if r.ClearLooks >= clearLooks {
				r.ClearBox = false
			}
			continue
		}
		msg := typist.Message{Text: r.Message.Text, Images: r.Images, ClearOnly: true}
		if !typist.Decide(typist.Message{Text: r.Message.Text, Images: r.Images, EnterOnly: true}, *a.Screen, a.Provider).OwnText {
			r.ClearBox = false
			continue
		}
		b.clearing, b.typing = r.Message.ClientID, true
		return &Job{ClientID: r.Message.ClientID, Attempt: r.Message.Attempts, Message: msg}
	}
	return nil
}

// Status is the box's word on each client id, in order: the record, and where a typed message went; a forgotten
// id is expired; any other unknown.
func (b *Box) Status(ids []string) []wire.MessageStatus {
	out := make([]wire.MessageStatus, 0, len(ids))
	for _, id := range ids {
		r, ok := b.recs[id]
		if !ok {
			if at, gone := b.gone[id]; gone {
				out = append(out, wire.MessageStatus{ClientID: id, K: wire.StatusExpired, At: max(at, 1)})
				continue
			}
			out = append(out, wire.MessageStatus{ClientID: id, K: wire.StatusUnknown})
			continue
		}
		m := r.Message
		out = append(out, wire.MessageStatus{ClientID: id, K: wire.StatusKnown, Message: &m, Handed: r.handed()})
	}
	return out
}

// handed is when and where a typed message went: in the agent's input box, in its own queue (steered), with the agent
// (its hook said so), or in the transcript (its entry); nil for a message not handed (queued, failed, cancelled).
func (r *Record) handed() *wire.MessageHanded {
	if r.TypedAt <= 0 {
		return nil
	}
	h := &wire.MessageHanded{At: r.TypedAt}
	switch r.Message.State.K {
	case wire.MsgTyping:
		h.Where = wire.HandedInputBox
	case wire.MsgInInput:
		h.Where = wire.HandedInputBox
		if r.Steered {
			h.Where = wire.HandedAgentQueue
		}
	case wire.MsgAccepted, wire.MsgInTurn, wire.MsgDone:
		switch {
		case r.EntryID != "":
			id := r.EntryID
			h.Where, h.EntryID = wire.HandedTranscript, &id
		case r.Steered && r.Message.State.K == wire.MsgAccepted:
			h.Where = wire.HandedAgentQueue
		default:
			h.Where = wire.HandedAgent
		}
	default:
		return nil
	}
	return h
}

// Wake is when the box next needs a look of its own: a retry's time or the end of a wait; zero when nothing waits on
// the clock.
func (b *Box) Wake() time.Time {
	var at wire.Ms
	soon := func(t wire.Ms) {
		if t > 0 && (at == 0 || t < at) {
			at = t
		}
	}
	for _, r := range b.recs {
		st := r.Message.State
		if pending(st.K) && st.Wait != nil && st.K != wire.MsgTyping {
			soon(st.Wait.DeadlineAt)
		}
		if st.K == wire.MsgTyping && r.TypedAt > 0 && !(b.typing && b.flight == r.Message.ClientID) {
			soon(r.TypedAt + wire.ConfirmDeadline)
		}
		if r.ClearBox && b.clearing == "" {
			soon(r.ClearAt)
		}
	}
	if len(b.items) > 0 {
		soon(b.recs[b.items[0]].NextTry)
	}
	for _, r := range b.recs {
		if (r.Message.ClientID == b.flight || r.Steered && b.flight == "") && r.Message.State.K == wire.MsgInInput &&
			r.Rechecks < maxRechecks {
			soon(r.RecheckAt)
		}
	}
	if at == 0 {
		return time.Time{}
	}
	return time.UnixMilli(at)
}

// Restarted settles the messages a restart caught on their way, by the transcript and the box, never blindly: the
// entry is there → accepted; the text is in the box → in-input, and the next try presses only its Enter; neither →
// queued again at the head. find looks for the entry past the typed offset; inBox says whether the box holds a text.
func (b *Box) Restarted(find Find, inBox func(text string) bool, now time.Time) Change {
	var ch Change
	b.flight, b.typing = "", false
	for _, r := range b.sorted() {
		st := r.Message.State
		if st.K != wire.MsgTyping && st.K != wire.MsgInInput {
			continue
		}
		if id, ok := b.found(r, find); ok {
			b.accept(&ch, r, &id, now)
			continue
		}
		if r.Steered && st.K == wire.MsgInInput {
			// Typed into the running turn and not read yet: it may well sit in Claude's queue still, and typing it
			// again would hand the agent two copies. It waits for its entry a short while, then the person decides
			// (the retry looks in the transcript first).
			r.TypedAt = ms(now)
			b.set(&ch, r, wire.MessageState{K: wire.MsgInInput, Wait: wait(r.TypedAt, wire.ConfirmDeadline*time.Millisecond)}, now)
			continue
		}
		if inBox != nil && inBox(r.Message.Text) && b.flight == "" {
			b.flight, r.EnterOnly = r.Message.ClientID, true
			r.TypedAt = ms(now)
			b.set(&ch, r, wire.MessageState{K: wire.MsgInInput, Wait: wait(ms(now), wire.ConfirmDeadline*time.Millisecond)}, now)
			continue
		}
		if !slices.Contains(b.items, r.Message.ClientID) {
			b.items = append([]string{r.Message.ClientID}, b.items...)
			b.rev++
		}
		r.WaitSince = ms(now)
		b.set(&ch, r, wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedBehind}, now)
	}
	if len(ch.Events) > 0 {
		ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	}
	return ch
}

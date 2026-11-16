package delegation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A team is a chat and the chats it delegated: besides the parent writing to its chats, a delegated chat
// writes to its parent and to the chats delegated beside it, in the middle of its turn, and the message reaches the
// other agent at its next step. A chat that writes to its team is held to a pace, so two agents that answer each other
// do not spin: TeamPairMax messages to one chat and TeamSenderMax to all of them in TeamWindow.
const (
	TeamWindow    = 10 * time.Minute
	TeamPairMax   = 6
	TeamSenderMax = 20
	// ParentAlias is the id a delegated chat names its parent by.
	ParentAlias = "parent"
)

// pace keeps the times of the messages each chat sent to its team.
type pace struct {
	mu   sync.Mutex
	sent map[string][]teamSend // by sender
}

type teamSend struct {
	to string
	at time.Time
}

// take records a message from → to at now unless the pace is spent; it says why it is when it is.
func (p *pace) take(from, to string, now time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sent == nil {
		p.sent = map[string][]teamSend{}
	}
	kept := p.sent[from][:0]
	pair := 0
	for _, s := range p.sent[from] {
		if now.Sub(s.at) < TeamWindow {
			kept = append(kept, s)
			if s.to == to {
				pair++
			}
		}
	}
	p.sent[from] = kept
	if pair >= TeamPairMax {
		return refuse("too-often", "this chat sent %s %d messages in the last %d minutes; finish the work in hand instead of answering back and forth, or say it in your final answer", to, pair, int(TeamWindow/time.Minute))
	}
	if len(kept) >= TeamSenderMax {
		return refuse("too-often", "this chat sent %d messages to its team in the last %d minutes; say the rest in your final answer", len(kept), int(TeamWindow/time.Minute))
	}
	p.sent[from] = append(kept, teamSend{to: to, at: now})
	return nil
}

// teammate is a chat the caller may write to as a member of its team: its parent, or a chat delegated by the same
// parent. ok is false for any other.
func (s *Service) teammate(caller wire.Conversation, id string) (wire.Conversation, bool) {
	if caller.ParentID == nil {
		return wire.Conversation{}, false
	}
	if id == ParentAlias {
		id = *caller.ParentID
	}
	rec, found := s.Convs.Record(id)
	if !found || rec.ID == caller.ID {
		return wire.Conversation{}, false
	}
	if rec.ID == *caller.ParentID || rec.ParentID != nil && *rec.ParentID == *caller.ParentID {
		return rec, true
	}
	return wire.Conversation{}, false
}

// replyHint is what a message from a delegated chat to its team ends with: who wrote it, so the other agent can answer.
func replyHint(from wire.Conversation) string {
	return fmt.Sprintf("\n\n(from chat %s; answer it with send_to_conversation id %q only if it asks you something)", from.ID, from.ID)
}

// Team is the caller's team as list_children shows a delegated chat: its parent and the chats beside it.
type Team struct {
	Parent   *wire.Conversation
	Siblings []wire.Conversation
}

// TeamOf is the parent and the siblings of a delegated chat; empty for a chat the person started.
func (s *Service) TeamOf(ctx context.Context, callerID string) (Team, error) {
	caller, err := s.caller(callerID)
	if err != nil || caller.ParentID == nil {
		return Team{}, err
	}
	var t Team
	if p, ok := s.Convs.Record(*caller.ParentID); ok {
		t.Parent = &p
	}
	kids, err := s.children(ctx, *caller.ParentID)
	if err != nil {
		return Team{}, refuse("unavailable", "the machine's chats did not answer: %v", err)
	}
	for _, k := range kids {
		if k.ID != caller.ID {
			t.Siblings = append(t.Siblings, k)
		}
	}
	return t, nil
}

// live tells whether a chat runs: a stopped one (closed, or stopped for standing idle) or one that never started would
// be started again by a message, which a broadcast must not do. One cut in the middle of a turn still counts: the
// platform brings it back.
func live(c wire.Conversation) bool {
	switch c.State.K {
	case wire.StateStopped, wire.StateLaunchFailed:
		return false
	case wire.StateGone:
		return c.LastTurn != nil && c.LastTurn.EndedAt == nil
	}
	return true
}

// Broadcast is one message to every chat the caller delegated that still runs: «stop, main is frozen», «the
// contract changed». A chat at work gets it in its running turn. A free one (its turn ended) is not woken: the message
// waits for its next turn and goes into that turn once it starts (Held), unless the sender asks to wake it. A stopped
// chat is left out. The answer names each.
type Broadcast struct {
	Sent    []Sent
	Held    []wire.Conversation
	Skipped []wire.Conversation
	Failed  map[string]string
}

// free tells whether a chat ended its turn and waits for a message.
func free(c wire.Conversation) bool {
	return c.State.K == wire.StateIdle || c.State.K == wire.StateReady
}

// BroadcastAll sends text to every live chat the caller delegated, as how says; wake starts the free ones on it too.
func (s *Service) BroadcastAll(ctx context.Context, callerID, text string, how Deliver, wake bool) (Broadcast, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return Broadcast{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > PromptMax {
		return Broadcast{}, refuse("bad-request", "text: 1 to %d characters", PromptMax)
	}
	kids, err := s.children(ctx, caller.ID)
	if err != nil {
		return Broadcast{}, refuse("unavailable", "the machine's chats did not answer: %v", err)
	}
	if len(kids) == 0 {
		return Broadcast{}, refuse("no-children", "this chat has delegated nothing")
	}
	out := Broadcast{Failed: map[string]string{}}
	for _, k := range kids {
		switch {
		case !live(k):
			out.Skipped = append(out.Skipped, k)
			continue
		case free(k) && !wake:
			s.Book.Update(k.ID, func(p *Page) { p.Held = append(p.Held, HeldMessage{From: caller.ID, Text: text}) })
			out.Held = append(out.Held, k)
			continue
		}
		// A broadcast «now» is a stop: the running turns are cut, not just told at their next step.
		send := how
		if send == DeliverNow {
			send = DeliverInterrupt
		}
		sent, err := s.send(ctx, callerID, k.ID, text, send)
		if err != nil {
			out.Failed[k.ID] = errText(err)
			continue
		}
		out.Sent = append(out.Sent, sent)
	}
	return out, nil
}

// HeldMessage is a broadcast kept for a free chat's next turn.
type HeldMessage struct {
	From string `json:"from"`
	Text string `json:"text"`
}

// deliverHeld puts the broadcasts kept for a chat into its turn once one runs: what the sweep does.
func (s *Service) deliverHeld(rec wire.Conversation, page Page) {
	if len(page.Held) == 0 || rec.State.K != wire.StateWorking {
		return
	}
	s.Book.Update(rec.ID, func(p *Page) { p.Held = nil })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, h := range page.Held {
		from, ok := s.Convs.Record(h.From)
		if !ok {
			continue
		}
		if _, res := s.deliver(ctx, from, rec.ID, h.Text, deliverStep); res.R == wire.ResultRefused && s.Log != nil {
			s.Log("delegation: a held broadcast did not go", "child", rec.ID, "code", string(res.Code))
		}
	}
}

// BroadcastText is the answer of broadcast_children.
func BroadcastText(b Broadcast) string {
	var lines []string
	for _, s := range b.Sent {
		lines = append(lines, "- "+strings.SplitN(SentText(s), "\n", 2)[0])
	}
	for id, why := range b.Failed {
		lines = append(lines, "- not sent to "+id+": "+why)
	}
	for _, c := range b.Held {
		lines = append(lines, "- kept for the next turn of "+Head(c)+": it is free and was not woken")
	}
	for _, c := range b.Skipped {
		lines = append(lines, "- skipped "+Head(c)+": it does not run")
	}
	head := fmt.Sprintf("sent to %d of %d delegated chats.", len(b.Sent), len(b.Sent)+len(b.Held)+len(b.Failed)+len(b.Skipped))
	return head + "\n" + strings.Join(lines, "\n")
}

// sendTeam delivers a message of a delegated chat to its parent or a sibling: into the running turn at the agent's next
// step (the person's «after the turn» holds unless now is asked for). The receiver's book is not touched: it is news, not
// work its parent gave it.
func (s *Service) sendTeam(ctx context.Context, from, to wire.Conversation, text string, how Deliver) (Sent, error) {
	clientID, res := s.deliver(ctx, from, to.ID, text, how)
	if res.R == wire.ResultRefused {
		return Sent{}, refuse(string(res.Code), "the message was not queued")
	}
	sent := Sent{To: to, How: how, PeerOutcome: convo.PeerOutcome{Where: convo.PeerQueued, Reason: wire.QueuedAgentBusy}}
	if s.steers(how) {
		sent.PeerOutcome = s.Convs.PeerOutcome(ctx, to.ID, clientID, SentWait)
	}
	if now, ok := s.Convs.Record(to.ID); ok {
		sent.To = now
	}
	return sent, nil
}

// WaitAll blocks until every chat the caller delegated that works has finished what it was last given (or needs the
// person, or ended), or the timeout passes: one call instead of a wait per chat, so a lead does not pay for its whole
// context once per chat and per poll. Chats that already finished come back at once with the rest.
func (s *Service) WaitAll(ctx context.Context, callerID string, timeout time.Duration) ([]Result, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return nil, err
	}
	kids, err := s.children(ctx, caller.ID)
	if err != nil {
		return nil, refuse("unavailable", "the machine's chats did not answer: %v", err)
	}
	if len(kids) == 0 {
		return nil, refuse("no-children", "this chat has delegated nothing")
	}
	poll := s.Poll
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		out := make([]Result, 0, len(kids))
		open := 0
		for _, k := range kids {
			rec, ok := s.Convs.Record(k.ID)
			if !ok {
				continue
			}
			var asked int64
			if p, ok := s.Book.Get(rec.ID); ok {
				asked = p.AskedAt
			}
			if o := outcome(rec, asked); o != "" {
				if r, ok := s.waited(ctx, rec, o); ok {
					out = append(out, r)
					continue
				}
			}
			open++
			out = append(out, Result{Conv: rec, Outcome: Timeout})
		}
		if open == 0 {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return out, nil
		case <-deadline.C:
			return out, nil
		case <-tick.C:
		}
	}
}

// AllText is the answer of a wait for all chats.
func AllText(rs []Result) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = ResultText(r)
	}
	return strings.Join(parts, "\n---\n")
}

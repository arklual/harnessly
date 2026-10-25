package feed

import (
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// NowEvery is how often "now" may go to a conversation's subscribers: at most twice a second.
const NowEvery = 500 * time.Millisecond

// nowThought is how much of the latest thinking "now" carries.
const nowThought = 200

// Now is what the agent does in the open turn this moment: its latest thought, the tool call still running, the live
// reply off the screen (word, "" none). Nil outside a turn.
func (f *Feed) Now(word string, at wire.Ms) *wire.Now {
	id := f.b.openID()
	if id == "" {
		return nil
	}
	n := &wire.Now{TurnID: id, At: at}
	if word != "" {
		n.Word = sptr(word)
	}
	t := f.b.turns[f.b.byTurn[id]]
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := f.b.entries[f.b.byID[t.entries[i]]]
		switch {
		case e.T == wire.EntryThink && n.Thought == nil:
			n.Thought = sptr(clip(collapse(e.Text), nowThought))
		case e.T == wire.EntryTool && e.OK == nil && n.Action == nil && n.Thought == nil:
			n.Action = &wire.NowAction{Tool: e.Tool, Summary: e.Summary}
		case e.T == wire.EntrySay || e.T == wire.EntryUser:
			return n
		}
		if n.Thought != nil {
			return n
		}
	}
	return n
}

// NowThrottle lets "now" out at most once per NowEvery: a change inside the window waits for its end, and only the
// latest one goes; the same "now" again goes nowhere.
type NowThrottle struct {
	sent    *wire.Now
	sentAt  time.Time
	pending *wire.Now
}

// Offer is a fresh "now": the event to publish at once, or nil when it waits for Wake.
func (t *NowThrottle) Offer(n *wire.Now, now time.Time) *wire.NowEv {
	if n == nil || same(t.sent, n) {
		t.pending = nil
		return nil
	}
	if !t.sentAt.IsZero() && now.Sub(t.sentAt) < NowEvery {
		t.pending = n
		return nil
	}
	return t.send(n, now)
}

// Due is the "now" kept back, when its window has passed.
func (t *NowThrottle) Due(now time.Time) *wire.NowEv {
	if t.pending == nil || now.Sub(t.sentAt) < NowEvery {
		return nil
	}
	n := t.pending
	t.pending = nil
	return t.send(n, now)
}

// Wake is when Due has something to let out, zero when nothing waits.
func (t *NowThrottle) Wake() time.Time {
	if t.pending == nil {
		return time.Time{}
	}
	return t.sentAt.Add(NowEvery)
}

// Reset forgets what went: the next "now" goes at once (a turn ended, a new one starts).
func (t *NowThrottle) Reset() { *t = NowThrottle{} }

func (t *NowThrottle) send(n *wire.Now, now time.Time) *wire.NowEv {
	t.sent, t.sentAt = n, now
	return &wire.NowEv{Now: *n}
}

// same: the agent does the same thing (only the time moved).
func same(a, b *wire.Now) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.TurnID == b.TurnID && eqStr(a.Thought, b.Thought) && eqStr(a.Word, b.Word) && eqAction(a.Action, b.Action)
}

func eqStr(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func eqAction(a, b *wire.NowAction) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Tool == b.Tool && a.Summary == b.Summary && eqStr(a.Command, b.Command)
}

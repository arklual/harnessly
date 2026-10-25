package outbox

import (
	"maps"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// State is a box as it is written to disk: the messages not final and the final ones of the last FinalStored, the
// queue's order and pause.
type State struct {
	Records []Record         `json:"records"`
	Items   []string         `json:"items"`
	Paused  *wire.QueuePause `json:"paused"`
	Rev     int64            `json:"rev"`
	Flight  string           `json:"flight"`
	// Gone are the ids of the messages let go and when they ended (GoneKept).
	Gone map[string]wire.Ms `json:"gone,omitempty"`
}

// State is the box to write.
func (b *Box) State() State {
	st := State{Items: slices.Clone(b.items), Paused: b.paused, Rev: b.rev, Flight: b.flight, Gone: maps.Clone(b.gone)}
	for _, r := range b.sorted() {
		st.Records = append(st.Records, *r)
	}
	return st
}

// Restore loads a box written before; its messages caught on their way wait for Restarted.
func (b *Box) Restore(st State) {
	b.recs = map[string]*Record{}
	for _, r := range st.Records {
		if r.Message.ClientID == "" {
			continue
		}
		b.recs[r.Message.ClientID] = &r
	}
	b.items = nil
	for _, id := range st.Items {
		if r := b.recs[id]; r != nil && r.Message.State.K == wire.MsgQueued && !slices.Contains(b.items, id) {
			b.items = append(b.items, id)
		}
	}
	b.paused, b.rev, b.flight = st.Paused, max(st.Rev, 1), ""
	b.gone = map[string]wire.Ms{}
	maps.Copy(b.gone, st.Gone)
}

// LegacyItem is a message of Legacy's prompt-queue.json.
type LegacyItem struct {
	ID        string
	Text      string
	Images    []string
	CreatedAt wire.Ms
	// Failed: Legacy delivery gave up on it already.
	Failed bool
}

// LegacyLifetime: a Legacy message older than this is imported as failed{expired}.
const LegacyLifetime = 24 * time.Hour

// Import takes Legacy's queue of the tab: each message becomes one with client id "legacy-<id>", in its order; one
// that is a day old, or that Legacy gave up on, is failed{expired} and waits for the person.
func (b *Box) Import(items []LegacyItem, now time.Time) Change {
	var ch Change
	for _, it := range items {
		id := "legacy-" + it.ID
		if _, ok := b.recs[id]; ok || it.ID == "" {
			continue
		}
		r := &Record{Message: wire.Message{
			ClientID: id, ConversationID: b.conv, Text: it.Text, Attachments: []wire.AttachmentRef{}, Mode: wire.ModeQueue,
			Kind: wire.KindPrompt, CreatedAt: it.CreatedAt, Rev: 1,
			State: wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedBehind},
		}, Images: slices.Clone(it.Images), WaitSince: ms(now)}
		if prefixed(it.Text) {
			r.Message.Kind = wire.KindCommand
		}
		b.recs[id] = r
		if it.Failed || ms(now)-it.CreatedAt > LegacyLifetime.Milliseconds() {
			r.Message.State = wire.MessageState{K: wire.MsgFailed, Code: wire.MsgFailedExpired, Retryable: true, At: ms(now)}
			r.EndedAt = ms(now)
		} else {
			b.items = append(b.items, id)
		}
		ch.add(wire.MessageUpsertEv{Message: r.Message})
	}
	if len(ch.Events) > 0 {
		b.rev++
		ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	}
	return ch
}

// prefixed: a text that opens a mode of the agent's prompt (/command, !shell) is a command.
func prefixed(text string) bool { return len(text) > 0 && (text[0] == '/' || text[0] == '!') }

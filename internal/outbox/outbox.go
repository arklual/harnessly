// Package outbox is a conversation's way to its agent: every message — a prompt, a "/model", a "!cmd" — enters the
// queue, and the head of the queue is typed by the typist when the agent can take it. A message's state moves only
// here, and every move is an event: queued{reason} → typing → in-input → accepted → in-turn → done, or failed{code}
// or cancelled. Every wait has a term (wire.Wait): nothing waits silently forever.
//
// A Box is owned by the conversation's actor and is not safe for concurrent use. It does not type and does not keep
// time: Pump hands the actor a Job to type under the pane lock, the actor answers with Typed, and the actor calls
// Tick at Wake. The tests drive it on a fake clock with no goroutines.
package outbox

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	// QueuedTTL: a message waiting on the launch, a resume, a menu or a pause this long has failed{expired}.
	QueuedTTL = 15 * time.Minute
	// OccupiedTTL: a box that holds someone else's text this long fails the message waiting behind it.
	OccupiedTTL = 10 * time.Minute
	// FinalKept: a finished message stays in snapshots this long; on disk it stays FinalStored.
	FinalKept   = 10 * time.Minute
	FinalStored = 24 * time.Hour
	// GoneKept: a forgotten message's id is remembered this long (at most GoneMax of them), so the status of a message
	// sent long ago is «expired», not «unknown», and a page that sends it again does not have it typed a second time.
	GoneKept = 7 * 24 * time.Hour
	GoneMax  = 500
	// CancelKept: the cancel of a message the box has not seen (its send is still on the road, or waits in a hub) is
	// remembered this long, at most CancelMax of them, so the send that comes after it is not queued.
	CancelKept = 24 * time.Hour
	CancelMax  = 200
	// MaxTries: a message whose typing failed this many times in a row has failed{type-failed}; the tries back off
	// from retryBase, doubling up to retryMax (1.5 → 3 → 6 → 12 → 24 s).
	MaxTries  = 10
	retryBase = 1500 * time.Millisecond
	retryMax  = 30 * time.Second
	// A menu or foreign text is looked at again 1.5 → 3 → 6 → 10 s.
	blockedMax = 10 * time.Second
	// A screen that said "wait" (busy, no input line) while the agent says it is free — the last turn's spinner is still
	// drawn for a moment, the input box is redrawn — is looked at again soon: every waitQuick for the first quickLooks
	// looks, so a send to a free agent is typed a few polls after the screen frees; past them, every waitAgain.
	// slowPause: a terminal that did not answer in time for a try is asked again this much later.
	slowPause  = 5 * time.Second
	waitQuick  = 100 * time.Millisecond
	quickLooks = 40
	waitAgain  = 1500 * time.Millisecond

	// A message the typist saw leave the box but the agent has not taken is looked for in the box again: a TUI that
	// was still starting can drop the Enter, or show the text only after the look. The box holding the message's text
	// with the prompt idle gets its Enter pressed again, every recheckEvery, at most maxRechecks times.
	recheckEvery = 2500 * time.Millisecond
	maxRechecks  = 10
	// A steered message is looked for in the box this often while it waits in Claude's queue.
	steerRecheckEvery = 5 * time.Second

	// A message carried by the launch of the agent (its first prompt) is waited for this long: a start can be slow.
	CarryDeadline = 90 * time.Second
	// GatherMaxText is the longest message the line's gathering makes (UTF-16 units): a long paste is refused by Claude
	// Code without a word, so messages that would make more are typed one after another, each whole.
	GatherMaxText = 8_000

	// MaxCarriedText is the longest text a launch carries (the agent launcher caps a first prompt there).
	MaxCarriedText = 20_000

	// SteerDeadline: a message typed into a working agent waits in Claude's own queue until the agent's next step — a
	// Bash call or a long answer may take minutes — and the transcript says when it was read. Past this it is
	// failed{unconfirmed}, which the person can retry (the retry looks in the transcript first).
	SteerDeadline = 30 * time.Minute
)

// Logger takes a line of the runtime's journal.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

// Agent is what the head of the queue needs to know of the agent.
type Agent struct {
	State wire.AgentState
	// Foreground: the agent, not a shell, is in the pane's foreground (an unknown agent is typed into only then).
	Foreground bool
	// HandOver: the running tool can be handed to the background (Claude: Bash, an MCP call).
	HandOver bool
	Provider wire.Provider
	// Screen is a look at the pane just now, nil when none was taken: a try the screen would refuse is not started,
	// so the pages do not see the message leave the queue and come back on every look.
	Screen *typist.Screen
}

// Record is a message with what only the runtime keeps of it.
type Record struct {
	Message wire.Message `json:"message"`
	// Images are the attachments' paths on the machine, pasted before the text.
	Images      []string `json:"images"`
	TypedAt     wire.Ms  `json:"typedAt"`
	TypedOffset int64    `json:"typedOffset"`
	// Tries counts the failed tries in a row; NextTry is when the next may start; WaitSince is since when it waits on
	// its current reason (the TTL counts from there).
	Tries     int              `json:"tries"`
	NextTry   wire.Ms          `json:"nextTry"`
	WaitSince wire.Ms          `json:"waitSince"`
	Reason    wire.QueueReason `json:"reason"`
	Blocked   int              `json:"blocked"`
	// BlockedSince: the first look that found someone's text in the box, for the term of that block (OccupiedTTL);
	// 0 when the box is not known to be taken.
	BlockedSince wire.Ms `json:"blockedSince,omitempty"`
	// Submitted: the agent's prompt-submit came while the message was being typed.
	Submitted bool `json:"submitted"`
	// EnterOnly: a restart found the message's text in the box; the next try presses only its Enter.
	EnterOnly bool    `json:"enterOnly"`
	EndedAt   wire.Ms `json:"endedAt"`
	// Steered: the message was typed into the running turn. Claude holds it in its own queue until the agent's next
	// step, so the prompt-submit hook (fired by the Enter) does not say it was read: its transcript entry does.
	Steered bool `json:"steered"`
	// Rechecks counts the Enters pressed again for a message waiting in-input; RecheckAt is when the box is looked at
	// for it next. recheck marks the try on its way as such.
	Rechecks int `json:"rechecks"`
	// Waits counts the looks in a row at which the screen said "wait" (waitQuick, then waitAgain).
	Waits     int     `json:"-"`
	RecheckAt wire.Ms `json:"recheckAt"`
	recheck   bool
	// Carried: the message goes with the launch of the agent as its first prompt, never typed.
	Carried bool `json:"carried"`
	// First: the launch chose this message as its first prompt. If that launch fails, it stays ahead of later urgent
	// messages until a retry carries it again; its public mode remains the mode the person sent (normally queue).
	First bool `json:"first,omitempty"`
	// Stalled: typed, and the agent did not take it within wire.ConfirmDeadline. It waits at the head of the line for
	// the person (retry, promote, cancel) and is not typed again by itself. ClearBox: its text may still be in the
	// agent's input box and is to be taken out of it at the next look, so no late Enter sends it behind our back.
	Stalled  bool `json:"stalled,omitempty"`
	ClearBox bool `json:"clearBox,omitempty"`
	// ClearAt is when the box is looked at for it next; ClearLooks counts the times no look came.
	ClearAt    wire.Ms `json:"clearAt,omitempty"`
	ClearLooks int     `json:"clearLooks,omitempty"`
	// EntryID is the transcript entry the message was accepted as, "" while none is known.
	EntryID string `json:"entryId,omitempty"`
	// Send numbers the try that typed the message, in the order the box typed (0: never typed). Two messages with the
	// same words are two sends: each prompt-submit hook confirms one of them, the oldest not yet confirmed. HookSend is
	// the send a hook confirmed.
	Send     int64 `json:"send,omitempty"`
	HookSend int64 `json:"hookSend,omitempty"`
	// Retyped: the one retype the box makes by itself for a send nobody confirmed is spent (HookConfirms).
	Retyped bool `json:"retyped,omitempty"`
}

// Job is one try to type a message, for the actor to run under the pane lock.
type Job struct {
	ClientID string
	Message  typist.Message
	Attempt  int
}

// Change is what a call did: the events to publish on the conversation's channel, in order.
type Change struct {
	Events []wire.Event
	// Resume asks the actor to start the stopped agent again on its session.
	Resume bool
}

func (c *Change) add(e wire.Event) { c.Events = append(c.Events, e) }

// Box is one conversation's queue and messages.
type Box struct {
	conv   string
	limits wire.Limits
	log    Logger

	recs   map[string]*Record
	items  []string // queued client ids, the head first
	paused *wire.QueuePause
	rev    int64
	// flight is the message typed or waiting to be accepted: one at a time.
	flight  string
	typing  bool
	resumed bool
	waits   map[string]string
	// clearing is the stalled message whose text a job is taking out of the box now, "" none.
	clearing string
	// gone are the forgotten messages' ids and when they ended.
	gone map[string]wire.Ms
	// cancelled are the ids of messages cancelled before the box saw them, and when.
	cancelled map[string]wire.Ms

	// Transcript looks for a message's own entry (its text) in the transcript; nil: none to look in. A message that was
	// typed once is looked for there before it is typed again, and a retry or a restart asks it rather than taking the
	// first user entry for the message: the transcript, not a timeout, says whether the agent has it.
	Transcript FindText
	// HookConfirms: the agent's prompt-submit hook names the prompt it took (ConfirmsByHook). A typed message is then
	// delivered only by that hook or by its entry in the transcript; a box that no longer shows it proves nothing, and
	// the box types it again by itself once, and only when neither came within ConfirmWindow. False is the path of an
	// agent without such a hook, as it was: the screen decides, and a text stuck in the box is typed again.
	HookConfirms bool
	// sends is the number of the last send.
	sends int64
}

// ConfirmWindow is how long a typed message waits for the agent's word — its hook or its transcript entry — before
// the box may type it again. The hook is a program the agent starts at the Enter and that delivers within a second or
// leaves its event in a spool read every 5 s; on a machine loaded several times over its cores both take seconds, and a
// retype sooner than they can come is a second copy of the message.
const ConfirmWindow = wire.ConfirmDeadline * time.Millisecond

// ConfirmsByHook says whether a provider's prompt-submit hook names the prompt the agent took. Claude's does; Codex
// has no such hook, and its messages go the way they went before.
func ConfirmsByHook(p wire.Provider) bool { return p == wire.ProviderClaude }

// FindText looks for the user entry of a message with this text at or after offset (or, read without its offset,
// written at or after typedAt).
type FindText func(offset int64, typedAt wire.Ms, text string) (entryID string, ok bool)

// found looks for r's own entry in the transcript, by its text; find (any user entry past the offset) only when there
// is no transcript to read the text in.
func (b *Box) found(r *Record, find Find) (string, bool) {
	if b.Transcript != nil {
		return b.Transcript(r.TypedOffset, r.TypedAt, r.Message.Text)
	}
	if find != nil {
		return find(r.TypedOffset, r.TypedAt)
	}
	return "", false
}

// New is an empty box of conversation conv.
func New(conv string, limits wire.Limits, log Logger) *Box {
	return &Box{conv: conv, limits: limits, log: log, recs: map[string]*Record{}, rev: 1, waits: map[string]string{},
		gone: map[string]wire.Ms{}, cancelled: map[string]wire.Ms{}}
}

// Queue is the conversation's queue as the wire carries it. An empty queue goes out as [], never null: the page
// takes null for a frame outside the protocol.
func (b *Box) Queue() wire.Queue {
	q := wire.Queue{ConversationID: b.conv, Items: append([]string{}, b.items...), Paused: b.paused, Rev: b.rev}
	for _, id := range b.items {
		if b.recs[id].Stalled {
			q.Stalled = append(q.Stalled, id)
		}
	}
	return q
}

// Summary is the queue as the sidebar row needs it.
func (b *Box) Summary() wire.QueueSummary {
	return wire.QueueSummary{Count: len(b.items), Paused: b.paused}
}

// Messages are the messages not final, and the final ones of the last FinalKept, oldest first.
func (b *Box) Messages(now time.Time) []wire.Message {
	out := []wire.Message{}
	for _, r := range b.sorted() {
		if final(r.Message.State.K) && now.UnixMilli()-r.EndedAt > FinalKept.Milliseconds() {
			continue
		}
		out = append(out, r.Message)
	}
	return out
}

// Message is one message by its client id.
func (b *Box) Message(clientID string) (wire.Message, bool) {
	r, ok := b.recs[clientID]
	if !ok {
		return wire.Message{}, false
	}
	return r.Message, true
}

// InFlight is the message typed or waiting to be accepted, "" none.
func (b *Box) InFlight() string { return b.flight }

func (b *Box) sorted() []*Record {
	out := make([]*Record, 0, len(b.recs))
	for _, r := range b.recs {
		out = append(out, r)
	}
	slices.SortFunc(out, func(x, y *Record) int {
		if x.Message.CreatedAt != y.Message.CreatedAt {
			return int(x.Message.CreatedAt - y.Message.CreatedAt)
		}
		if x.Message.ClientID < y.Message.ClientID {
			return -1
		}
		return 1
	})
	return out
}

func final(k wire.MessageStateKind) bool {
	return k == wire.MsgDone || k == wire.MsgFailed || k == wire.MsgCancelled
}

// Pending: the message is still on its way (queued, typed, or waiting to be accepted).
func pending(k wire.MessageStateKind) bool {
	return k == wire.MsgQueued || k == wire.MsgTyping || k == wire.MsgInInput
}

// Draft is a message to queue, checked and resolved by the caller (its attachments uploaded and found).
type Draft struct {
	ClientID    string
	Text        string
	Attachments []wire.AttachmentRef
	Images      []string
	// Refs are the places the message points at; Text already ends with their block.
	Refs []wire.MessageRef
	Mode wire.MessageMode
	Kind wire.MessageKind
	// Auto: the runtime wrote it itself to go on (wire.Message.Auto).
	Auto wire.AutoContinueWhy
}

// Send queues a message. The same client id again gives back the message it queued (its answer, or its events, were
// lost on the way): accepted, nothing new, and its whole record goes out again so the page that sent it takes its true
// state. The id of a message forgotten long ago is too-late: it was delivered, and is never typed a second time. A
// message cancelled before it came is kept as cancelled and never queued. A refusal names its code.
//
// A message of the line takes its place by when the page made it (place), not by when it arrived.
func (b *Box) Send(d Draft, agent wire.AgentState, now time.Time) (Change, wire.RefuseCode) {
	var ch Change
	if r, ok := b.recs[d.ClientID]; ok {
		return resync(r), ""
	}
	if _, ok := b.gone[d.ClientID]; ok {
		return ch, wire.RefuseTooLate
	}
	if ch, ok := b.TakenBack(d, now); ok {
		return ch, ""
	}
	switch {
	case blankText(d.Text) && len(d.Attachments) == 0:
		return ch, wire.RefuseBadRequest
	case jscompat.Len(d.Text) > b.limits.MaxText:
		return ch, wire.RefuseTextTooLong
	case len(d.Attachments) > b.limits.MaxAttachments:
		return ch, wire.RefuseTooManyAttachments
	case len(b.items) >= b.limits.MaxQueue:
		return ch, wire.RefuseQueueFull
	case agent.K == wire.StateGone && agent.Reason != string(wire.GoneTmuxLost):
		return ch, wire.RefuseAgentGone
	}
	ms := now.UnixMilli()
	r := &Record{Message: wire.Message{
		ClientID: d.ClientID, ConversationID: b.conv, Text: d.Text, Attachments: nonNil(d.Attachments), Refs: d.Refs,
		Mode: d.Mode, Kind: d.Kind, Auto: d.Auto, CreatedAt: ms, State: wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedBehind}, Rev: 1,
	}, Images: d.Images, WaitSince: ms}
	b.recs[d.ClientID] = r
	if d.Mode == wire.ModeNow || d.Mode == wire.ModeSteer {
		// «Сразу» while messages wait: they go with it as one message, theirs first, so nothing is held up behind it.
		b.gather(&ch, r, now)
		b.pushUrgent(d.ClientID, d.Mode)
	} else {
		at := len(b.items)
		if d.Auto == "" {
			at = b.place(d.ClientID)
		}
		if at < len(b.items) {
			// The lists of messages are ordered by CreatedAt, then by client id: it stands before the one it goes ahead of.
			r.Message.CreatedAt = min(ms, b.recs[b.items[at]].Message.CreatedAt)
		}
		b.items = slices.Insert(b.items, at, d.ClientID)
	}
	b.rev++
	ch.add(wire.MessageUpsertEv{Message: r.Message})
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	return ch, ""
}

// TakenBack answers the send of a message that was cancelled before the box saw it: the message is kept as cancelled,
// never queued, and its record goes out so every page shows it as taken back. False when no such cancel is remembered.
func (b *Box) TakenBack(d Draft, now time.Time) (Change, bool) {
	var ch Change
	if _, ok := b.cancelled[d.ClientID]; !ok || b.recs[d.ClientID] != nil {
		return ch, false
	}
	delete(b.cancelled, d.ClientID)
	ms := now.UnixMilli()
	r := &Record{Message: wire.Message{
		ClientID: d.ClientID, ConversationID: b.conv, Text: d.Text, Attachments: nonNil(d.Attachments), Refs: d.Refs,
		Mode: d.Mode, Kind: d.Kind, CreatedAt: ms, State: wire.MessageState{K: wire.MsgCancelled, By: wire.CancelledByUser}, Rev: 1,
	}, EndedAt: ms}
	b.recs[d.ClientID] = r
	ch.add(wire.MessageUpsertEv{Message: r.Message})
	if b.log != nil {
		b.log.Info("message cancelled before it came", "conversationId", b.conv, "clientId", d.ClientID)
	}
	return ch, true
}

// madeAt is when the page made a message, by its client id: a UUIDv7 opens with the page's clock in ms (48 bits).
// False for any other id.
func madeAt(id string) (int64, bool) {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[14] != '7' || id[18] != '-' || id[23] != '-' {
		return 0, false
	}
	v, err := strconv.ParseInt(id[:8]+id[9:13], 16, 64)
	return v, err == nil
}

// place is where a new message of the line goes: after everything, or — when it was made before messages that still
// wait their turn and came ahead of it (its own send was late on the road) — before them, so the agent gets the
// person's messages in the order they were written. It never goes ahead of a message that is urgent, the launch's
// first, stalled, sent again by the person, written by the runtime, or whose id says nothing of its time: those keep
// the place they have, and a message with such an id goes last, as it came.
func (b *Box) place(id string) int {
	at := len(b.items)
	made, ok := madeAt(id)
	if !ok {
		return at
	}
	for at > 0 {
		p := b.recs[b.items[at-1]]
		was, ok := madeAt(p.Message.ClientID)
		if !ok || was <= made || p.Message.Mode != wire.ModeQueue || p.Message.Auto != "" || p.Message.Attempts > 0 ||
			p.First || p.Carried || p.Stalled {
			break
		}
		at--
	}
	return at
}

// urgency orders the modes at the head of the line: a steer goes into the running turn, so it is not held up by a "now"
// that waits for the turn to end; both go before what is merely queued.
func urgency(m wire.MessageMode) int {
	switch m {
	case wire.ModeSteer:
		return 0
	case wire.ModeNow:
		return 1
	}
	return 2
}

// pushUrgent puts a "now" or steered message ahead of the ones that wait, behind the ones of its own kind already there:
// two steers go in the order they were sent (Claude reads what is typed into its queue in order, too).
func (b *Box) pushUrgent(id string, mode wire.MessageMode) {
	at := 0
	for at < len(b.items) && b.recs[b.items[at]].First {
		at++
	}
	for at < len(b.items) && urgency(b.recs[b.items[at]].Message.Mode) <= urgency(mode) {
		at++
	}
	b.items = slices.Insert(b.items, at, id)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func blankText(s string) bool { return jscompat.Trim(s) == "" }

// over: the message has left the line for good — the agent answered it or the person took it back. A page that still
// shows it in the queue missed the frames that said so (a chat cached in the browser came back after its
// messages were answered).
func over(k wire.MessageStateKind) bool { return k == wire.MsgDone || k == wire.MsgCancelled }

// resync answers a command on a message that is over with the message as it is, so the page that asked takes its true
// state and the row leaves the queue: Delete, «Сразу», a move or Изменить of a delivered message is not refused, and
// nothing is sent again.
func resync(r *Record) Change {
	var ch Change
	ch.add(wire.MessageUpsertEv{Message: r.Message})
	return ch
}

// Cancel takes a queued message out; one already typed is too late, one that is over is only shown as it is. A
// message the box does not know is not-found, and the cancel is remembered: its send, when it comes, is not queued.
func (b *Box) Cancel(clientID string, now time.Time) (Change, wire.RefuseCode) {
	var ch Change
	r, ok := b.recs[clientID]
	switch {
	case !ok:
		if _, gone := b.gone[clientID]; !gone {
			// Its send may still be on the road, or wait in a hub: the cancel is remembered for it (TakenBack).
			b.takeBack(clientID, now.UnixMilli())
		}
		return ch, wire.RefuseNotFound
	case over(r.Message.State.K):
		return resync(r), ""
	case r.Message.State.K != wire.MsgQueued && r.Message.State.K != wire.MsgFailed:
		return ch, wire.RefuseTooLate
	case b.awaited(r, now):
		// Typed, and the agent's word on it may still come: it cannot be taken back as if it had never been sent.
		return ch, wire.RefuseTooLate
	}
	b.dropItem(clientID)
	b.set(&ch, r, wire.MessageState{K: wire.MsgCancelled, By: wire.CancelledByUser}, now)
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	return ch, ""
}

// awaited: the message was typed, nobody confirmed it, and it stands in the line only for the window in which the
// agent's hook or its transcript entry may still come (HookConfirms) before the one retype.
func (b *Box) awaited(r *Record, now time.Time) bool {
	return b.HookConfirms && r.Message.State.K == wire.MsgQueued && r.Send > 0 && r.Retyped && !r.Stalled &&
		r.NextTry > now.UnixMilli()
}

// takeBack remembers the cancel of a message the box has not seen, at most CancelMax of them (the oldest go first).
func (b *Box) takeBack(id string, at wire.Ms) {
	b.cancelled[id] = at
	for len(b.cancelled) > CancelMax {
		oldest, when := "", wire.Ms(0)
		for k, v := range b.cancelled {
			if oldest == "" || v < when || v == when && k < oldest {
				oldest, when = k, v
			}
		}
		delete(b.cancelled, oldest)
	}
}

// Find looks for a message's user entry in the transcript at or after offset, or (a line read without its offset,
// after a restart) written at or after typedAt: its id when there is one.
type Find func(offset int64, typedAt wire.Ms) (entryID string, ok bool)

// Retry queues a failed message again, attempts+1. One the agent took after all (the transcript has its entry past
// its typed offset) is accepted instead, not typed again.
func (b *Box) Retry(clientID string, find Find, now time.Time) (Change, wire.RefuseCode) {
	var ch Change
	r, ok := b.recs[clientID]
	switch {
	case !ok:
		return ch, wire.RefuseNotFound
	case r.Stalled && r.Message.State.K == wire.MsgQueued:
		return b.unstall(r, find, now), ""
	case r.Message.State.K != wire.MsgFailed:
		return ch, wire.RefuseNotFailed
	}
	if r.TypedOffset > 0 {
		if id, ok := b.found(r, find); ok {
			b.accept(&ch, r, &id, now)
			return ch, ""
		}
	}
	r.Message.Attempts++
	r.Retyped = false
	r.Tries, r.NextTry, r.Blocked, r.BlockedSince, r.WaitSince, r.EndedAt = 0, 0, 0, 0, now.UnixMilli(), 0
	b.items = append([]string{clientID}, b.items...)
	b.rev++
	b.set(&ch, r, wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedBehind}, now)
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	return ch, ""
}

// Promote moves a queued message to now (the head) or to steer (typed into the running turn). Steer needs a working
// agent: otherwise not-working, and the page sends now.
func (b *Box) Promote(clientID string, to wire.PromoteTo, agent wire.AgentState, now time.Time) (Change, wire.RefuseCode) {
	var ch Change
	r, ok := b.recs[clientID]
	switch {
	case !ok:
		return ch, wire.RefuseNotFound
	case over(r.Message.State.K):
		return resync(r), ""
	case r.Message.State.K != wire.MsgQueued:
		return ch, wire.RefuseTooLate
	case to == wire.PromoteToSteer && agent.K != wire.StateWorking:
		return ch, wire.RefuseNotWorking
	}
	if r.Stalled {
		// «Сразу» on a stalled message is the person's word to send it: it goes like a retry, at its new place.
		ch = b.unstall(r, nil, now)
		if r.Message.State.K != wire.MsgQueued {
			return ch, ""
		}
	}
	r.Message.Mode = wire.MessageMode(to)
	// The other messages that wait go with it, in the order of the line: one message now instead of one after another.
	b.gather(&ch, r, now)
	b.dropItem(clientID)
	b.pushUrgent(clientID, r.Message.Mode)
	b.rev++
	r.Message.Rev++
	ch.add(wire.MessageUpsertEv{Message: r.Message})
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	return ch, ""
}

// gatherable: a message of the line that may go inside another — the person's words waiting their turn (one sent «now»
// or into the turn is on its way already, in its own order); not a command
// («/compact», «! ls» is typed alone), not one the runtime wrote itself, not the launch's first prompt, not one stalled
// (the person decides on it).
func (b *Box) gatherable(r *Record) bool {
	return r.Message.State.K == wire.MsgQueued && r.Message.Mode == wire.ModeQueue && r.Message.Kind != wire.KindCommand &&
		r.Message.Auto == "" && !r.First && !r.Carried && !r.Stalled
}

// gather makes into the one message of every gatherable one in the line, in the line's order — into at its own place
// when it is in the line (promoted), after them all when it is new: their words joined by a blank line, their
// attachments, pictures and places with them. Those taken in leave the line, cancelled; one that would make the
// message too long or carry too many attachments stays in the line. A command into takes nobody in.
func (b *Box) gather(ch *Change, into *Record, now time.Time) {
	if into.Message.Kind == wire.KindCommand || into.Message.Auto != "" {
		return
	}
	order := make([]*Record, 0, len(b.items)+1)
	in := false
	for _, id := range b.items {
		r := b.recs[id]
		if r == into {
			in = true
			order = append(order, r)
		} else if r != nil && b.gatherable(r) {
			order = append(order, r)
		}
	}
	if !in {
		order = append(order, into)
	}
	if len(order) < 2 {
		return
	}
	// into keeps its own words whole; the others go in while the message stays within the limits.
	size, count := jscompat.Len(into.Message.Text), len(into.Message.Attachments)
	var taken []*Record
	for _, r := range order {
		if r != into {
			add := jscompat.Len(r.Message.Text) + 2
			if size+add > min(b.limits.MaxText, GatherMaxText) || count+len(r.Message.Attachments) > b.limits.MaxAttachments {
				continue
			}
			size, count = size+add, count+len(r.Message.Attachments)
		}
		taken = append(taken, r)
	}
	var texts []string
	var atts []wire.AttachmentRef
	var images []string
	var refs []wire.MessageRef
	for _, r := range taken {
		m := r.Message
		if !blankText(m.Text) {
			texts = append(texts, m.Text)
		}
		atts = append(atts, m.Attachments...)
		images = append(images, r.Images...)
		for _, ref := range m.Refs {
			if !slices.ContainsFunc(refs, func(x wire.MessageRef) bool { return x.Kind == ref.Kind && x.ID == ref.ID }) {
				refs = append(refs, ref)
			}
		}
	}
	if len(taken) < 2 {
		return
	}
	into.Message.Text = strings.Join(texts, "\n\n")
	into.Message.Attachments = nonNil(atts)
	into.Images = images
	into.Message.Refs = refs
	ids := make([]string, 0, len(taken)-1)
	for _, r := range taken {
		if r == into {
			continue
		}
		ids = append(ids, r.Message.ClientID)
		b.dropItem(r.Message.ClientID)
		b.set(ch, r, wire.MessageState{K: wire.MsgCancelled, By: wire.CancelledByUser}, now)
	}
	if b.log != nil {
		b.log.Info("queued messages sent as one", "conversationId", b.conv, "clientId", into.Message.ClientID,
			"gathered", ids)
	}
}

// Move puts a queued message right before another queued one (before "": at the end of the line). Only a message of the
// line moves: one typed, waiting in the agent's input or past that is too-late, an unknown one not-found. The line keeps
// its order of urgency (steered first, then «now», then the rest, as pushUrgent made it): a place that would put the
// message across a message of another urgency, or before one that has left the line, is a conflict. A move that changes
// nothing changes nothing and is accepted.
func (b *Box) Move(clientID, before string, now time.Time) (Change, wire.RefuseCode) {
	var ch Change
	r, ok := b.recs[clientID]
	if !ok || (before != "" && b.recs[before] == nil) {
		return ch, wire.RefuseNotFound
	}
	if over(r.Message.State.K) {
		return resync(r), ""
	}
	from := slices.Index(b.items, clientID)
	if r.Message.State.K != wire.MsgQueued || from < 0 {
		return ch, wire.RefuseTooLate
	}
	rest := slices.Delete(slices.Clone(b.items), from, from+1)
	at := len(rest)
	if before != "" {
		if at = slices.Index(rest, before); at < 0 {
			return ch, wire.RefuseConflict
		}
	}
	mine := urgency(r.Message.Mode)
	if (at > 0 && urgency(b.recs[rest[at-1]].Message.Mode) > mine) || (at < len(rest) && urgency(b.recs[rest[at]].Message.Mode) < mine) {
		return ch, wire.RefuseConflict
	}
	if at == from {
		return ch, ""
	}
	b.items = slices.Insert(rest, at, clientID)
	b.rev++
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	return ch, ""
}

// Carry takes a queued message out of the line to go with the launch of the agent, as its first prompt: it waits
// in-input for the agent to take it, and is never typed. False when it cannot (not queued, another on its way, images,
// a text the launch would cut).
func (b *Box) Carry(clientID string, now time.Time) (Change, bool) {
	var ch Change
	r := b.recs[clientID]
	if r == nil || b.flight != "" || r.Message.State.K != wire.MsgQueued || len(r.Images) > 0 || len(r.Message.Attachments) > 0 ||
		blankText(r.Message.Text) || jscompat.Len(r.Message.Text) > MaxCarriedText {
		return ch, false
	}
	b.dropItem(clientID)
	b.flight, b.typing = clientID, false
	r.TypedOffset, r.TypedAt, r.Submitted, r.Carried, r.First = 0, now.UnixMilli(), false, true, true
	r.Rechecks, r.RecheckAt = maxRechecks, 0
	b.set(&ch, r, wire.MessageState{K: wire.MsgInInput, Wait: wait(r.TypedAt, CarryDeadline)}, now)
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	b.positions(&ch, wire.QueuedBehind, now)
	return ch, true
}

// Uncarry puts a message the launch was to carry back at the head of the line: the launch did not happen.
func (b *Box) Uncarry(ch *Change, now time.Time) {
	r := b.recs[b.flight]
	if r == nil || !r.Carried || r.Message.State.K != wire.MsgInInput {
		return
	}
	b.flight, r.Carried = "", false
	b.items = append([]string{r.Message.ClientID}, b.items...)
	b.rev++
	r.WaitSince = now.UnixMilli()
	b.set(ch, r, wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedBehind}, now)
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
}

// Pause stops the queue for reason; Resume lifts the pause. Neither touches a message already on its way, but one
// the launch was to carry goes back to the line when the launch failed or the agent is gone.
func (b *Box) Pause(reason wire.PauseReason, now time.Time) Change {
	var ch Change
	if b.paused != nil && b.paused.Reason == reason {
		return ch
	}
	if reason == wire.PausedLaunchFailed || reason == wire.PausedAgentGone {
		b.Uncarry(&ch, now)
	}
	b.paused = &wire.QueuePause{Reason: reason, At: now.UnixMilli()}
	b.rev++
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	if b.flight == "" {
		b.positions(&ch, wire.QueuedPaused, now)
	}
	return ch
}

// Resume lifts the pause; false when the queue was not paused.
func (b *Box) Resume(now time.Time) (Change, bool) {
	var ch Change
	if b.paused == nil {
		return ch, false
	}
	b.paused = nil
	b.rev++
	b.resumed = false
	for _, id := range b.items {
		b.recs[id].WaitSince = now.UnixMilli()
	}
	ch.add(wire.QueueChangedEv{Queue: b.Queue()})
	return ch, true
}

// Paused is the queue's pause, nil none.
func (b *Box) Paused() *wire.QueuePause { return b.paused }

// Head is the client id of the message first in the line, "" for an empty line.
func (b *Box) Head() string {
	if len(b.items) == 0 {
		return ""
	}
	return b.items[0]
}

func (b *Box) dropItem(id string) {
	if i := slices.Index(b.items, id); i >= 0 {
		b.items = slices.Delete(b.items, i, i+1)
		b.rev++
	}
}

// set moves a message to st (its rev up) and tells the pages; a state that equals the one it has changes nothing.
func (b *Box) set(ch *Change, r *Record, st wire.MessageState, now time.Time) {
	if sameState(r.Message.State, st) {
		return
	}
	r.Message.State = st
	r.Message.Rev++
	if final(st.K) {
		r.EndedAt = now.UnixMilli()
		if b.flight == r.Message.ClientID {
			b.flight, b.typing = "", false
		}
		delete(b.waits, r.Message.ClientID)
	}
	ch.add(wire.PromptStateEv{ClientID: r.Message.ClientID, State: st, Rev: r.Message.Rev})
}

func sameState(a, b wire.MessageState) bool {
	eqWait := a.Wait == nil && b.Wait == nil || a.Wait != nil && b.Wait != nil && *a.Wait == *b.Wait
	eqEntry := a.EntryID == nil && b.EntryID == nil || a.EntryID != nil && b.EntryID != nil && *a.EntryID == *b.EntryID
	a.Wait, b.Wait, a.EntryID, b.EntryID = nil, nil, nil, nil
	return a == b && eqWait && eqEntry
}

func (b *Box) fail(ch *Change, r *Record, code wire.MessageFailure, retryable bool, now time.Time) {
	b.dropItem(r.Message.ClientID)
	b.set(ch, r, wire.MessageState{K: wire.MsgFailed, Code: code, Retryable: retryable, At: now.UnixMilli()}, now)
	if b.log != nil {
		b.log.Warn("message failed", "conversationId", b.conv, "clientId", r.Message.ClientID, "code", string(code),
			"attempts", r.Message.Attempts)
	}
}

// waiting logs what a message waits on when that changed since the last look: a queue that does not move says why
// once, not on every try.
func (b *Box) waiting(id, on string) {
	if b.waits[id] == on {
		return
	}
	b.waits[id] = on
	if b.log != nil {
		b.log.Info("message waits", "conversationId", b.conv, "clientId", id, "on", on)
	}
}

func ms(t time.Time) wire.Ms { return t.UnixMilli() }

// waitFor is how late a screen that said "wait" is looked at again.
func (r *Record) waitFor() time.Duration {
	r.Waits++
	if r.Waits <= quickLooks {
		return waitQuick
	}
	return waitAgain
}

func wait(from wire.Ms, deadline time.Duration) *wire.Wait {
	return &wire.Wait{SlowAt: from + wire.WaitSlowMs, DeadlineAt: from + deadline.Milliseconds()}
}

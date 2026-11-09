package hubproto

import "encoding/json"

// The person's chats in the hub (docs/hub/history.md): the agent forwards the events of the runtime's
// conversation channels (`convs`, `conv:<id>`) to the hub, and the hub keeps the chat list and every chat's feed, so
// the list and the history open while the machine is off. The same model as a closed cluster's copy: a channel's events
// carry a seq of the runtime's epoch and the hub says how far it stored them (`history.ack`); what the agent no longer
// has it sends again from the runtime's pages (`history.backfill`), and each chat is walked to its first turn by a
// cursor the hub keeps (`HistoryStart.Pending`).
//
// Nothing is sent until the hub says `history.start{enabled:true}`.
const (
	OpHistoryStart = "history.start"
	OpHistoryAck   = "history.ack"
	// EventHistoryEvents carries live events of one channel: HistoryEvents.
	EventHistoryEvents = "history.events"
	// EventHistoryBackfill carries events made from what the runtime holds now: HistoryBackfill.
	EventHistoryBackfill = "history.backfill"
	// FeatureHistory is in `hello` when the agent reads the runtime's replica socket and answers history.start/ack.
	FeatureHistory = "history"
)

// HistoryCursor is how far the hub stored a channel: the last seq of an epoch.
type HistoryCursor struct {
	Epoch string `json:"epoch"`
	Seq   int64  `json:"seq"`
}

// HistoryStart tells the agent where the hub's copy stands. Known is the revision of every chat the hub has a record
// of; Pending the chats it does not hold whole, each with the turn the next page is read before ("" for the newest).
type HistoryStart struct {
	Enabled bool                     `json:"enabled"`
	Cursors map[string]HistoryCursor `json:"cursors"`
	Known   map[string]int64         `json:"known"`
	Pending map[string]string        `json:"pending"`
}

// HistoryAck: everything of Ch up to AckSeq (of Epoch) is stored; Gap: a frame began after that, resend from AckSeq+1.
type HistoryAck struct {
	Ch     string `json:"ch"`
	Epoch  string `json:"epoch"`
	AckSeq int64  `json:"ackSeq"`
	Gap    bool   `json:"gap,omitempty"`
}

// HistoryEvents is the payload of EventHistoryEvents: events FromSeq.. of one channel of one epoch; Reset says the run
// before them is not continued (a new baseline).
type HistoryEvents struct {
	Ch      string            `json:"ch"`
	Epoch   string            `json:"epoch"`
	FromSeq int64             `json:"fromSeq"`
	Reset   bool              `json:"reset,omitempty"`
	Events  []json.RawMessage `json:"events"`
}

// HistoryBackfill is the payload of EventHistoryBackfill: events without a seq (the chat list, pages of turns).
type HistoryBackfill struct {
	Ch     string            `json:"ch"`
	Events []json.RawMessage `json:"events"`
}

// HistoryFrameMax is the most events one frame may carry.
const HistoryFrameMax = 5000

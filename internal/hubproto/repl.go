package hubproto

import (
	"encoding/json"
	"time"
)

// One way to replicate a machine's data to the hub over the link (docs/hub/replication.md). The agent sends a kind's
// data as batches, each numbered within its kind on this link; the hub stores them in order and acknowledges how far
// it got. The agent keeps at most Window batches of a kind unacknowledged, so the hub's queue never fills (the flow is
// held on the machine, not dropped on the hub); what is not acknowledged in time, or what the hub could not store, is
// sent again from the oldest unacknowledged batch, with a growing pause between tries.
//
// The kind's own cursor (where the hub's copy of a chat channel stands, the epoch and seq of the spend outbox) stays in
// the kind's payload: the batch number only orders and paces the link.
//
// The agent says FeatureRepl in `hello`; the hub that speaks it answers with `repl.start` before anything else of the
// kinds it names. An agent without the feature, or a hub that never says `repl.start`, keeps the events of each kind as
// they were (history.events, history.backfill).
const (
	// FeatureRepl is in `hello` when the agent sends its kinds as repl.batch after a repl.start.
	FeatureRepl = "repl"
	// OpReplStart turns the batches on for the kinds named, with the window: ReplStart.
	OpReplStart = "repl.start"
	// OpReplAck says how far the hub stored a kind: ReplAck.
	OpReplAck = "repl.ack"
	// EventReplBatch carries one batch: {"event": "repl.batch", "kind", "seq", "data"}.
	EventReplBatch = "repl.batch"

	// ReplWindow is the batches of one kind the agent may have unacknowledged. The windows of all the kinds together
	// stay below the hub's queue of them, so a machine that keeps to its window never has a frame dropped.
	ReplWindow = 16
)

// Replicated kinds: the name of a kind is the name of the event it replaced.
const (
	ReplHistoryEvents   = EventHistoryEvents   // data is HistoryEvents
	ReplHistoryBackfill = EventHistoryBackfill // data is HistoryBackfill
)

// ReplStart turns the batches on for Kinds; Window is the most batches of one kind unacknowledged (0: ReplWindow).
// A new repl.start (the hub lost track) starts every kind's numbering again from 1.
type ReplStart struct {
	Window int      `json:"window"`
	Kinds  []string `json:"kinds"`
}

// ReplBatch is one batch of a kind; Seq counts the kind's batches on this link from 1.
type ReplBatch struct {
	Kind string          `json:"kind"`
	Seq  int64           `json:"seq"`
	Data json.RawMessage `json:"data"`
}

// ReplAck: the batches of Kind up to Seq are stored. Retry: the next one was not, send again from Seq+1 after a pause.
type ReplAck struct {
	Kind  string `json:"kind"`
	Seq   int64  `json:"seq"`
	Retry bool   `json:"retry,omitempty"`
}

// Pauses between tries of what did not go through: ReplRetryMin after the first failure, doubled up to ReplRetryMax.
const (
	ReplRetryMin = time.Second
	ReplRetryMax = time.Minute
)

// ReplRetryWait is the pause before the try that follows `failures` failures in a row (1 or more).
func ReplRetryWait(failures int) time.Duration {
	d := ReplRetryMin
	for i := 1; i < failures && d < ReplRetryMax; i++ {
		d *= 2
	}
	return min(d, ReplRetryMax)
}

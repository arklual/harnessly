package repl

import "github.com/ly-solutions/harnessly/internal/hubproto"

// Transport is how a kind of a machine's data reaches the hub.
type Transport string

const (
	// TransportBatches: repl.batch over the link (this package): numbered, acknowledged, windowed, sent again.
	TransportBatches Transport = "batches"
	// TransportPull: the hub reads the machine's outbox by a cursor over the link and acknowledges what it stored.
	TransportPull Transport = "pull"
	// TransportEvent: a single event over the link, applied once; nothing is sent again if it is lost.
	TransportEvent Transport = "event"
	// TransportHTTP: the machine calls the hub's HTTP API, signed with its key, and tries again itself.
	TransportHTTP Transport = "http"
)

// Kind is one kind of data a machine sends to the hub: the data kind of the register (store.DataKinds) it fills ("" for
// a request to the hub rather than a copy of the machine's data: the documents' events), how it travels, and its names
// on the link (the batch kind or the events and operations).
type Kind struct {
	Name      string
	DataKind  string
	Transport Transport
	// Lag says the kind is in harnessly_hub_repl_lag_seconds.
	Lag  bool
	Wire []string
	// Legacy is how an agent without hubproto.FeatureRepl sends it, for a kind that moved to batches.
	Legacy string
}

// Kinds is the map of what machines replicate to the hub (docs/hub/replication.md). Every data kind of the register
// that a machine owns is here (the tests check it); a kind that moves to batches changes its Transport here.
var Kinds = []Kind{
	{Name: hubproto.ReplHistoryEvents, DataKind: "chat history", Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck, hubproto.OpHistoryAck},
		Legacy: "history.events frames on the bulk queue, dropped when it is full; the channel's own ack and resend"},
	{Name: hubproto.ReplHistoryBackfill, DataKind: "chat history", Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck, hubproto.OpHistoryStart},
		Legacy: "history.backfill frames on the bulk queue, dropped when it is full and never acknowledged; history.start again after a drop"},
	{Name: "usage", DataKind: "spend", Transport: TransportPull, Lag: true,
		Wire: []string{hubproto.EventUsageAvailable, hubproto.OpUsageRead, hubproto.OpUsageAck}},
	{Name: hubproto.EventMetricsSample, DataKind: "what a machine reports about itself", Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck},
		Legacy: "metrics.sample on the event queue; a full queue closes the link"},
	{Name: hubproto.EventEnvState, DataKind: "what a machine reports about itself", Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck},
		Legacy: "env.state on the event queue; a full queue closes the link"},
	{Name: hubproto.EventEnvRun, DataKind: "what a machine reports about itself", Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck},
		Legacy: "env.run on the event queue; a full queue closes the link"},
	{Name: hubproto.EventRestart, DataKind: "what a machine reports about itself", Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck},
		Legacy: "restart on the event queue; a full queue closes the link"},
	{Name: hubproto.EventDocPublish, Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck, hubproto.OpDocResult},
		Legacy: "doc.publish on the event queue; the agent waits for doc.result and the person tries again"},
	{Name: hubproto.EventDocACL, Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck, hubproto.OpDocResult},
		Legacy: "doc.acl on the event queue; the agent waits for doc.result"},
	{Name: hubproto.EventDocBound, Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck},
		Legacy: "docs.bound on the event queue, lost when the link is down"},
	{Name: hubproto.EventDocReply, Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck},
		Legacy: "docs.reply on the event queue, lost when the link is down"},
	{Name: hubproto.EventDesign, DataKind: "designs", Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck, hubproto.OpDesignAccess},
		Legacy: "design on the event queue; the agent tries again only when no link took it"},
	{Name: hubproto.EventAutomationResult, DataKind: "automations", Transport: TransportBatches, Lag: true,
		Wire:   []string{hubproto.EventReplBatch, hubproto.OpReplAck},
		Legacy: "automations.result on the event queue; agents without batches send it to cluster links only"},
	{Name: "memory", DataKind: "agent memory", Transport: TransportHTTP,
		Wire: []string{hubproto.MemoryPullPath, hubproto.MemoryPutPath}},
	{Name: "attachments", DataKind: "chat attachments", Transport: TransportHTTP,
		Wire: []string{"PUT <platform api>/uploads/blobs, /uploads/refs: the page puts the bytes into the hub beside the machine"}},
	{Name: "machine hello", DataKind: "what a machine reports about itself", Transport: TransportEvent,
		Wire: []string{hubproto.OpHello}},
}

// Batched are the kinds that travel as repl.batch.
func Batched() []string {
	var out []string
	for _, k := range Kinds {
		if k.Transport == TransportBatches {
			out = append(out, k.Name)
		}
	}
	return out
}

// Measured are the kinds written in harnessly_hub_repl_lag_seconds, at 0 while nothing waits.
func Measured() []string {
	var out []string
	for _, k := range Kinds {
		if k.Lag {
			out = append(out, k.Name)
		}
	}
	return out
}

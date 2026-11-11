package machines

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/repl"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// HistoryBatches is the side of the HistorySink that stores the frames of the chats as repl.batch: it says why a frame
// was not stored, so the agent sends it again (internal/hub/history).
type HistoryBatches interface {
	LiveBatch(ctx context.Context, m store.Machine, raw json.RawMessage) error
	BackfillBatch(ctx context.Context, m store.Machine, raw json.RawMessage) error
}

// replState holds the receivers of the links that send batches (hubproto.FeatureRepl).
type replState struct {
	once      sync.Once
	stats     *repl.Stats
	receivers sync.Map // *link.Link → *repl.Receiver
}

// Repl is the measure of the batches of every link of this replica (harnessly_hub_repl_*).
func (s *Service) Repl() *repl.Stats {
	s.repl.once.Do(func() { s.repl.stats = repl.NewStats() })
	return s.repl.stats
}

// replEvents are the events that come as batches of their own name: the frame of the event is the batch's data, and
// the hub handles it as the event (dispatch). Their senders cannot be told to send again, so they are acknowledged
// once handled; what the batches give them is the window held on the machine instead of a full queue on the hub.
var replEvents = []string{
	hubproto.EventMetricsSample, hubproto.EventEnvState, hubproto.EventEnvRun, hubproto.EventRestart,
	hubproto.EventDocPublish, hubproto.EventDocACL, hubproto.EventDocBound, hubproto.EventDocReply,
	hubproto.EventDesign, hubproto.EventAutomationResult,
}

// replHandlers are the kinds the hub takes as batches from this link; nil for an agent that does not send batches.
func (s *Service) replHandlers(l *link.Link, m store.Machine, features []string) map[string]repl.Handler {
	if !slices.Contains(features, hubproto.FeatureRepl) {
		return nil
	}
	out := map[string]repl.Handler{}
	for _, name := range replEvents {
		out[name] = func(ctx context.Context, data json.RawMessage) error {
			s.dispatch(ctx, l, m, link.Event{Name: name, Raw: data})
			return nil
		}
	}
	if hb, ok := s.history.(HistoryBatches); ok && m.CreatedBy != "" && slices.Contains(features, hubproto.FeatureHistory) {
		out[hubproto.ReplHistoryEvents] = func(ctx context.Context, data json.RawMessage) error {
			return hb.LiveBatch(ctx, m, data)
		}
		out[hubproto.ReplHistoryBackfill] = func(ctx context.Context, data json.RawMessage) error {
			return hb.BackfillBatch(ctx, m, data)
		}
	}
	return out
}

// startRepl tells an agent that sends batches to send its kinds so, before anything of them is asked for. Without
// it (an older agent, or the call failed) the agent sends the old events, which the hub still takes.
func (s *Service) startRepl(ctx context.Context, l *link.Link, m store.Machine, features []string) {
	handlers := s.replHandlers(l, m, features)
	if handlers == nil {
		return
	}
	r := repl.NewReceiver(s.Repl(), m.ID, handlers, func(ctx context.Context, ack hubproto.ReplAck) error {
		_, err := s.reg.Do(ctx, m.ID, hubproto.OpReplAck, ack, callTimeout)
		return err
	}, s.log)
	if _, err := s.reg.Do(ctx, m.ID, hubproto.OpReplStart, r.Start(), callTimeout); err != nil {
		r.Close()
		s.log.Info("repl.start not taken, the machine sends the old events", "machine", m.ID, "error", err)
		return
	}
	s.repl.receivers.Store(l, r)
	safego.Go("repl receiver", func() {
		<-l.Done()
		s.repl.receivers.Delete(l)
		r.Close()
	})
}

// replBatch hands a repl.batch to the receiver of its link; a link the hub did not start batches on has none.
func (s *Service) replBatch(ctx context.Context, l *link.Link, ev link.Event) {
	if r, ok := s.repl.receivers.Load(l); ok {
		r.(*repl.Receiver).Handle(ctx, ev.Raw, ev.At())
	}
}

// sendsBatches says the link's agent was started on batches: what it sends is acknowledged and sent again, so a lost
// frame needs no history.start.
func (s *Service) sendsBatches(l *link.Link) bool {
	_, ok := s.repl.receivers.Load(l)
	return ok
}

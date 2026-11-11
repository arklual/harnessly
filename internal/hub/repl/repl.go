// Package repl is the hub's end of the one way a machine's data is replicated over the link (hubproto.FeatureRepl,
// docs/hub/replication.md): batches of a kind, numbered on the link, stored in order and acknowledged, sent again from
// the oldest one the hub has not stored, and held on the machine while a window of them waits for the hub. It also
// keeps the one measure of how far the hub's copy of each kind is behind (harnessly_hub_repl_lag_seconds{kind}).
package repl

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/metrics"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// ErrBad is what a handler returns for a batch that can never be stored: it is acknowledged and not sent again.
var ErrBad = errors.New("batch can never be stored")

// Handler stores one batch of a kind. An error other than ErrBad has the agent send the batch again after a pause.
type Handler func(ctx context.Context, data json.RawMessage) error

// Acker sends a repl.ack to the agent of the link. It may be slow: the receiver calls it off the handling of batches.
type Acker func(ctx context.Context, ack hubproto.ReplAck) error

// Results of a batch, the label of harnessly_hub_repl_batches_total.
const (
	ResultStored    = "stored"    // stored and acknowledged
	ResultRetry     = "retry"     // not stored; the agent sends it again after a pause
	ResultDuplicate = "duplicate" // stored before: acknowledged again
	ResultGap       = "gap"       // one before it is missing: not stored, the agent sends from the missing one
	ResultBad       = "bad"       // can never be stored: acknowledged and skipped
)

// KindUnknown is the label of the batches of a kind the hub did not name.
const KindUnknown = "unknown"

var results = []string{ResultStored, ResultRetry, ResultDuplicate, ResultGap, ResultBad}

const ackTimeout = 15 * time.Second

// Receiver takes the batches of one link. Batches are handed to it one by one in the order the link read them.
type Receiver struct {
	machineID string
	handlers  map[string]Handler
	ack       Acker
	log       *slog.Logger
	now       func() time.Time
	stats     *Stats

	mu      sync.Mutex
	streams map[string]*stream
	closed  bool
}

// stream is where a kind of one link stands: the last batch stored, and when the oldest batch not stored was read.
type stream struct {
	done  int64
	since time.Time
}

// NewReceiver makes the receiver of one link and counts it in stats until Close. handlers name the kinds it takes.
func NewReceiver(stats *Stats, machineID string, handlers map[string]Handler, ack Acker, log *slog.Logger) *Receiver {
	if log == nil {
		log = slog.Default()
	}
	r := &Receiver{machineID: machineID, handlers: handlers, ack: ack, log: log, now: time.Now, stats: stats,
		streams: map[string]*stream{}}
	stats.add(r)
	return r
}

// Kinds are the kinds the receiver takes, for repl.start.
func (r *Receiver) Kinds() []string {
	out := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Start is the repl.start the agent is told: every kind's numbering begins again.
func (r *Receiver) Start() hubproto.ReplStart {
	r.mu.Lock()
	r.streams = map[string]*stream{}
	r.mu.Unlock()
	return hubproto.ReplStart{Window: hubproto.ReplWindow, Kinds: r.Kinds()}
}

// Close takes the receiver out of the measure: its link is gone.
func (r *Receiver) Close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.stats.remove(r)
}

// Handle stores a repl.batch frame read at `at` and acknowledges it.
func (r *Receiver) Handle(ctx context.Context, raw json.RawMessage, at time.Time) {
	var b hubproto.ReplBatch
	if json.Unmarshal(raw, &b) != nil || b.Kind == "" || b.Seq <= 0 {
		return
	}
	h := r.handlers[b.Kind]
	if h == nil {
		// A kind the hub did not name: it keeps no state and its name goes into no label (the machine chose it). It is
		// acknowledged so the agent does not hold it forever.
		r.count(KindUnknown, ResultBad)
		r.send(hubproto.ReplAck{Kind: b.Kind, Seq: b.Seq})
		return
	}
	r.mu.Lock()
	st := r.streams[b.Kind]
	if st == nil {
		st = &stream{}
		r.streams[b.Kind] = st
	}
	done := st.done
	switch {
	case b.Seq <= done:
		r.mu.Unlock()
		r.count(b.Kind, ResultDuplicate)
		r.send(hubproto.ReplAck{Kind: b.Kind, Seq: done})
		return
	case b.Seq > done+1:
		// One before it was lost on the way (a dropped frame of an agent past its window): nothing after the hole is
		// stored, so the order of the kind holds; the agent sends again from the hole when its wait for an ack ends.
		if st.since.IsZero() {
			st.since = at
		}
		r.mu.Unlock()
		r.count(b.Kind, ResultGap)
		return
	}
	if st.since.IsZero() {
		st.since = at
	}
	r.mu.Unlock()

	err := h(ctx, b.Data)
	switch {
	case err == nil, errors.Is(err, ErrBad):
		r.mu.Lock()
		st.done, st.since = b.Seq, time.Time{}
		r.mu.Unlock()
		if err != nil {
			r.count(b.Kind, ResultBad)
			r.log.Warn("repl: batch can never be stored, skipped", "machine", r.machineID, "kind", b.Kind, "seq", b.Seq, "error", err)
		} else {
			r.count(b.Kind, ResultStored)
		}
		r.send(hubproto.ReplAck{Kind: b.Kind, Seq: b.Seq})
	default:
		r.count(b.Kind, ResultRetry)
		r.log.Warn("repl: batch not stored, the agent sends it again", "machine", r.machineID, "kind", b.Kind, "seq", b.Seq, "error", err)
		r.send(hubproto.ReplAck{Kind: b.Kind, Seq: done, Retry: true})
	}
}

// send acknowledges off the handling of batches: the agent keeps the highest ack, so their order does not matter.
func (r *Receiver) send(ack hubproto.ReplAck) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ackTimeout)
		defer cancel()
		if err := r.ack(ctx, ack); err != nil {
			r.log.Info("repl: ack not taken", "machine", r.machineID, "kind", ack.Kind, "error", err)
		}
	}()
}

func (r *Receiver) count(kind, result string) { r.stats.count(kind, result) }

// lag is, by kind, how long the oldest batch the link read and has not stored has waited.
func (r *Receiver) lag(now time.Time, into map[string]time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	for kind, st := range r.streams {
		if st.since.IsZero() {
			continue
		}
		if d := now.Sub(st.since); d > into[kind] {
			into[kind] = d
		}
	}
}

// Stats is what the receivers of a hub add up to.
type Stats struct {
	mu        sync.Mutex
	receivers map[*Receiver]struct{}
	batches   map[string]map[string]int64 // kind → result → batches
}

// NewStats makes an empty measure.
func NewStats() *Stats {
	return &Stats{receivers: map[*Receiver]struct{}{}, batches: map[string]map[string]int64{}}
}

func (s *Stats) add(r *Receiver) {
	s.mu.Lock()
	s.receivers[r] = struct{}{}
	s.mu.Unlock()
}

func (s *Stats) remove(r *Receiver) {
	s.mu.Lock()
	delete(s.receivers, r)
	s.mu.Unlock()
}

func (s *Stats) count(kind, result string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.batches[kind]
	if m == nil {
		m = map[string]int64{}
		s.batches[kind] = m
	}
	m[result]++
}

// Lag is, by kind, how long the oldest batch read and not stored has waited, among the links of this replica.
func (s *Stats) Lag(now time.Time) map[string]time.Duration {
	s.mu.Lock()
	rs := make([]*Receiver, 0, len(s.receivers))
	for r := range s.receivers {
		rs = append(rs, r)
	}
	s.mu.Unlock()
	out := map[string]time.Duration{}
	for _, r := range rs {
		r.lag(now, out)
	}
	return out
}

// Write writes the measure of every kind: the lag (with what the kinds outside the batches measure themselves, by kind
// in `other`) and the batches by result. Every kind in kinds is written, at 0 while nothing of it waits.
func (s *Stats) Write(w *metrics.Writer, now time.Time, kinds []string, other map[string]time.Duration) {
	lag := s.Lag(now)
	for k, d := range other {
		lag[k] = max(lag[k], d)
	}
	all := slices.Clone(kinds)
	for k := range lag {
		all = append(all, k)
	}
	s.mu.Lock()
	for k := range s.batches {
		all = append(all, k)
	}
	batches := map[string]map[string]int64{}
	for k, m := range s.batches {
		batches[k] = maps.Clone(m)
	}
	s.mu.Unlock()
	slices.Sort(all)
	all = slices.Compact(all)
	const lagHelp = "Age of the oldest data of a machine the hub was sent (or told of) and has not stored yet, by kind, in seconds."
	for _, k := range all {
		w.Gauge("repl_lag_seconds", lagHelp, lag[k].Seconds(), "kind", k)
	}
	const batchHelp = "Batches of machines' data taken over the link, by kind and result."
	for _, k := range all {
		if batches[k] == nil {
			continue
		}
		for _, res := range results {
			w.Counter("repl_batches_total", batchHelp, float64(batches[k][res]), "kind", k, "result", res)
		}
	}
}

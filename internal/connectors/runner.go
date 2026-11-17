package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// What the runner keeps on the machine for each connection, in dir/state/<id>.json, written at once with every batch:
// the cursor after the last batch, the fingerprints already emitted (so a source that reads again after a crash
// doubles nothing) and the outbox of envelopes some destination has not taken yet (so nothing is lost while the hub
// or the inbox is away). A batch is kept before the source moves on; an envelope leaves the outbox only when every
// destination it is for took it.

const (
	// seenMax fingerprints are remembered per connection: far more than one batch of any source.
	seenMax = 5000
	// outboxMax envelopes wait per connection; the oldest are dropped past it.
	outboxMax = 1000
	// giveUp drops a destination of an envelope it has not taken for this long.
	giveUp = 72 * time.Hour
)

// Destination is where envelopes go: the hub's rules, the inbox. Deliver answers the fingerprints it took; an error
// with none taken keeps them all for later.
type Destination interface {
	Name() string
	// Wants says whether an envelope is for it (the inbox takes only connectors with Spec.Inbox).
	Wants(e Envelope) bool
	Deliver(ctx context.Context, batch []Envelope) (taken []string, err error)
}

type outItem struct {
	Envelope Envelope `json:"envelope"`
	// To are the destinations that have not taken it yet.
	To    []string  `json:"to"`
	Since time.Time `json:"since"`
}

type connState struct {
	Cursor Cursor    `json:"cursor"`
	Seen   []string  `json:"seen"`
	Outbox []outItem `json:"outbox"`
}

type runner struct {
	s   *Service
	log *slog.Logger

	mu      sync.Mutex
	ctx     context.Context
	dests   []Destination
	running map[string]context.CancelFunc
	// sources counts the sources' goroutines still running, for WaitStopped.
	sources sync.WaitGroup
	// relays counts the proposals still being offered or settled in a chat, for WaitStopped.
	relays sync.WaitGroup
	// states are serialized per connection by stateMu.
	stateMu sync.Mutex
	kick    chan struct{}
	// restart is how long a source waits after an error before it runs again.
	restart time.Duration
	// every is how often the outboxes are tried even when nothing kicked them.
	every time.Duration
}

func newRunner(s *Service) *runner {
	return &runner{s: s, log: slog.Default().With("part", "connectors"), running: map[string]context.CancelFunc{},
		kick: make(chan struct{}, 1), restart: time.Minute, every: 30 * time.Second}
}

func (r *runner) stateFile(id string) string { return filepath.Join(r.s.dir, "state", id+".json") }

func (r *runner) load(id string) connState {
	var st connState
	raw, err := os.ReadFile(r.stateFile(id))
	if err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	return st
}

func (r *runner) save(id string, st connState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return writeFile(r.stateFile(id), raw)
}

// run starts every kept connection's source and the delivery loop; it returns at once.
func (r *runner) run(ctx context.Context, dests []Destination) {
	r.mu.Lock()
	r.ctx, r.dests = ctx, dests
	r.mu.Unlock()
	list, err := r.s.List()
	if err != nil {
		r.log.Warn("connections not read", "error", err)
	}
	for _, c := range list {
		r.start(c)
	}
	go r.deliverLoop(ctx)
}

// start (re)starts a connection's source when the runner runs and the connector has one.
func (r *runner) start(c Conn) {
	conn, ok := Lookup(c.Connector)
	if !ok {
		return
	}
	src := sourceOf(conn)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx == nil || src == nil {
		return
	}
	if stop := r.running[c.ID]; stop != nil {
		stop()
	}
	ctx, cancel := context.WithCancel(r.ctx)
	r.running[c.ID] = cancel
	r.sources.Add(1)
	go func() {
		defer r.sources.Done()
		r.watch(ctx, c, conn, src)
	}()
}

func (r *runner) stop(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if stop := r.running[id]; stop != nil {
		stop()
		delete(r.running, id)
	}
}

// watch runs a source until ctx ends, again after each error, always from the last kept cursor.
func (r *runner) watch(ctx context.Context, c Conn, conn Connector, src Watcher) {
	sp := conn.Spec()
	for ctx.Err() == nil {
		cur, err := r.s.Get(c.ID)
		if err != nil {
			return
		}
		in := r.s.instance(cur, nil)
		from := r.load(c.ID).Cursor
		err = src.Watch(ctx, in, from, func(events []Event, next Cursor) error {
			return r.commit(sp, in, cur.Account, events, next)
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			r.log.Warn("connector source stopped", "connection", c.ID, "connector", sp.ID, "error", err)
			_, _ = r.s.update(c.ID, func(x *Conn) { x.State, x.Error = StateError, sayOf(err) })
			r.s.changed()
		}
		t := time.NewTimer(r.restart)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// commit keeps a batch and the cursor after it in one write, dropping what was already emitted.
func (r *runner) commit(sp Spec, in Instance, account string, events []Event, next Cursor) error {
	now := r.s.now().UTC()
	r.mu.Lock()
	dests := r.dests
	r.mu.Unlock()
	r.stateMu.Lock()
	st := r.load(in.ID)
	seen := map[string]bool{}
	for _, f := range st.Seen {
		seen[f] = true
	}
	fresh := 0
	for _, ev := range events {
		env, err := seal(sp, in, account, ev, now)
		if err != nil {
			r.log.Warn("connector event refused", "connection", in.ID, "connector", sp.ID, "error", err)
			continue
		}
		if seen[env.Fingerprint] {
			continue
		}
		seen[env.Fingerprint] = true
		st.Seen = append(st.Seen, env.Fingerprint)
		var to []string
		for _, d := range dests {
			if d.Wants(env) {
				to = append(to, d.Name())
			}
		}
		if len(to) > 0 {
			st.Outbox = append(st.Outbox, outItem{Envelope: env, To: to, Since: now})
		}
		fresh++
	}
	if len(st.Seen) > seenMax {
		st.Seen = slices.Clone(st.Seen[len(st.Seen)-seenMax:])
	}
	if len(st.Outbox) > outboxMax {
		r.log.Warn("connector outbox full, oldest dropped", "connection", in.ID, "dropped", len(st.Outbox)-outboxMax)
		st.Outbox = slices.Clone(st.Outbox[len(st.Outbox)-outboxMax:])
	}
	st.Cursor = next
	err := r.save(in.ID, st)
	r.stateMu.Unlock()
	if err != nil {
		return err
	}
	if fresh > 0 {
		_, _ = r.s.update(in.ID, func(x *Conn) { x.LastEventAt, x.State, x.Error = now, StateOK, "" })
		select {
		case r.kick <- struct{}{}:
		default:
		}
	}
	return nil
}

func (r *runner) deliverLoop(ctx context.Context) {
	t := time.NewTicker(r.every)
	defer t.Stop()
	for {
		r.deliverAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.kick:
		}
	}
}

// deliverAll tries every connection's outbox once.
func (r *runner) deliverAll(ctx context.Context) {
	list, err := r.s.List()
	if err != nil {
		return
	}
	for _, c := range list {
		r.deliver(ctx, c.ID)
	}
}

// deliver hands a connection's outbox to its destinations and keeps what is left. The outbox is read and written
// under stateMu, but the destinations are called outside it, so a slow hub never holds a source's commit.
func (r *runner) deliver(ctx context.Context, id string) {
	r.mu.Lock()
	dests := r.dests
	r.mu.Unlock()
	r.stateMu.Lock()
	box := r.load(id).Outbox
	r.stateMu.Unlock()
	if len(box) == 0 {
		return
	}
	taken := map[string]map[string]bool{} // destination → fingerprints
	for _, d := range dests {
		var batch []Envelope
		for _, it := range box {
			if slices.Contains(it.To, d.Name()) {
				batch = append(batch, it.Envelope)
			}
		}
		if len(batch) == 0 {
			continue
		}
		got, err := d.Deliver(ctx, batch)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.log.Info("connector events wait for a destination", "connection", id, "to", d.Name(), "left", len(batch)-len(got), "error", err)
		}
		set := map[string]bool{}
		for _, f := range got {
			set[f] = true
		}
		taken[d.Name()] = set
	}
	now := r.s.now()
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	st := r.load(id)
	keep := st.Outbox[:0]
	for _, it := range st.Outbox {
		left := it.To[:0]
		for _, to := range it.To {
			if taken[to][it.Envelope.Fingerprint] {
				continue
			}
			if now.Sub(it.Since) > giveUp {
				r.log.Warn("connector event dropped: not taken in time", "connection", id, "to", to, "fingerprint", it.Envelope.Fingerprint)
				continue
			}
			left = append(left, to)
		}
		if len(left) > 0 {
			it.To = left
			keep = append(keep, it)
		}
	}
	st.Outbox = keep
	if err := r.save(id, st); err != nil {
		r.log.Warn("connector state not written", "connection", id, "error", err)
	}
}

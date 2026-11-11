package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/metrics"
)

// With several replicas a browser's stream is open on one of them while the thing it should hear of happens on
// another. A Relay passes what this process tells its own streams (the three buses: a space's events, the fleet's
// pokes, an organization's events, and the kicks) to the other replicas, which give it to theirs. It carries over
// whatever Transport it is given; the hub's is Postgres LISTEN/NOTIFY. Nothing is kept: a notification that a replica
// did not hear is lost, which costs the page a refetch on its next event or focus, as a lost stream does.

// Transport sends a short text to every replica (this one included) and hands what the replicas sent to a listener.
type Transport interface {
	// Notify sends payload; it may fail, and a payload longer than Max is never given to it.
	Notify(ctx context.Context, payload string) error
	// Listen calls fn for every payload until ctx ends, and up each time it starts to hear (again).
	Listen(ctx context.Context, up func(), fn func(payload string), failed func(error)) error
}

// RelayMax is the longest message a Relay sends; a longer event stays with the replica it happened on.
const RelayMax = 7900

const relayQueue = 1024

const (
	relaySpace      = "s"
	relaySpaceKick  = "sk"
	relayFleetSpace = "fs"
	relayFleetUser  = "fu"
	relayOrg        = "o"
	relayOrgKick    = "ok"
)

// relayMsg is one thing a replica tells the others.
type relayMsg struct {
	// From is the replica that sent it: a replica drops its own.
	From string `json:"f"`
	Bus  string `json:"b"`
	// Key is the space or the organization.
	Key        string          `json:"k,omitempty"`
	User       string          `json:"u,omitempty"`
	Name       string          `json:"n,omitempty"`
	Data       json.RawMessage `json:"d,omitempty"`
	OwnersOnly bool            `json:"oo,omitempty"`
	Machine    string          `json:"m,omitempty"`
	Aud        *Audience       `json:"a,omitempty"`
}

// Relay joins the buses of this process to those of the other replicas.
type Relay struct {
	id     string
	t      Transport
	log    *slog.Logger
	broker *Broker
	fleet  *FleetBus
	org    *MemoryBus
	queue  chan relayMsg
	// kicks are the ends of a person's streams (they left, or their role changed): sent before anything else, tried
	// again when the send fails, and never dropped for a full queue of plain events.
	kicks  chan relayMsg
	warned sync.Once
}

// NewRelay makes the relay of the replica called id. Any of the buses may be nil. Nothing moves until Run.
func NewRelay(id string, t Transport, broker *Broker, fleet *FleetBus, org *MemoryBus, log *slog.Logger) *Relay {
	if log == nil {
		log = slog.Default()
	}
	r := &Relay{id: id, t: t, log: log, broker: broker, fleet: fleet, org: org, queue: make(chan relayMsg, relayQueue), kicks: make(chan relayMsg, relayQueue)}
	if broker != nil {
		broker.mu.Lock()
		broker.out = r.send
		broker.mu.Unlock()
	}
	if fleet != nil {
		fleet.mu.Lock()
		fleet.out = r.send
		fleet.mu.Unlock()
	}
	if org != nil {
		org.mu.Lock()
		org.out = r.send
		org.mu.Unlock()
	}
	return r
}

// send queues a message for the other replicas. It never waits: publishing must not depend on the database.
func (r *Relay) send(m relayMsg) {
	m.From = r.id
	q := r.queue
	if m.kick() {
		q = r.kicks
	}
	select {
	case q <- m:
	default:
		metrics.EventsRelayDropped.Inc()
		if m.kick() {
			r.log.Error("events relay: a kick was not passed to the other replicas, its queue is full", "user", m.User)
		}
	}
}

func (m relayMsg) kick() bool { return m.Bus == relaySpaceKick || m.Bus == relayOrgKick }

// Run sends and receives until ctx ends.
func (r *Relay) Run(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := r.t.Listen(ctx, func() { r.log.Info("events relay listening") }, r.receive, func(err error) {
			r.log.Warn("events relay lost its connection, streams hear only this replica until it is back", "error", err)
		})
		if err != nil && ctx.Err() == nil {
			r.log.Warn("events relay is off: streams hear only the replica they are open on", "error", err)
		}
	}()
	for {
		select { // a kick waiting goes before any event
		case m := <-r.kicks:
			r.postKick(ctx, m)
			continue
		default:
		}
		select {
		case <-ctx.Done():
			<-done
			return
		case m := <-r.kicks:
			r.postKick(ctx, m)
		case m := <-r.queue:
			r.post(ctx, m)
		}
	}
}

// postKick sends a kick, trying again when the database did not take it.
func (r *Relay) postKick(ctx context.Context, m relayMsg) {
	for attempt := 1; attempt <= 5; attempt++ {
		if r.post(ctx, m) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
		}
	}
	r.log.Error("events relay: a kick was not passed to the other replicas", "user", m.User)
}

func (r *Relay) post(ctx context.Context, m relayMsg) bool {
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > RelayMax {
		metrics.EventsRelayDropped.Inc()
		return true // it will never fit: trying again changes nothing
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := r.t.Notify(ctx, string(raw)); err != nil {
		metrics.EventsRelayDropped.Inc()
		r.warned.Do(func() { r.log.Warn("events relay could not send", "error", err) })
		return false
	}
	metrics.EventsRelayed.Inc()
	return true
}

// receive gives a message of another replica to this process's streams. It does not send it on.
func (r *Relay) receive(payload string) {
	var m relayMsg
	if json.Unmarshal([]byte(payload), &m) != nil || m.From == "" || m.From == r.id {
		return
	}
	metrics.EventsRelayReceived.Inc()
	switch m.Bus {
	case relaySpace:
		if r.broker != nil {
			r.broker.publish(m.Key, Event{Name: m.Name, Data: m.Data, OwnersOnly: m.OwnersOnly, MachineID: m.Machine}, false)
		}
	case relaySpaceKick:
		if r.broker != nil {
			r.broker.kick(m.Key, m.User)
		}
	case relayFleetSpace:
		if r.fleet != nil {
			r.fleet.touchSpace(m.Key)
		}
	case relayFleetUser:
		if r.fleet != nil {
			r.fleet.touchUser(m.User)
		}
	case relayOrg:
		if r.org != nil {
			var aud Audience
			if m.Aud != nil {
				aud = *m.Aud
			}
			r.org.publish(m.Key, m.Name, m.Data, aud)
		}
	case relayOrgKick:
		if r.org != nil {
			r.org.kick(m.Key, m.User)
		}
	}
}

// Package events is the hub's live channel to the browser: one Server-Sent Events stream per open space. Every
// resource an event announces also has a GET, so a lost stream costs a refetch and never a wrong screen: nothing is
// replayed.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/metrics"
)

// Event is one thing that happened in a space.
type Event struct {
	Name string
	Data json.RawMessage
	// OwnersOnly keeps the event from members: install steps and login links are the owners' business.
	OwnersOnly bool
	// MachineID, when set, makes the event carry a machine's metadata: a stream gets it only while the Broker's machine
	// gate lets the stream's person see that machine.
	MachineID string
}

// Sub is one open stream.
type Sub struct {
	spaceID string
	userID  string
	owner   bool
	ch      chan Event
	done    chan struct{}
	once    sync.Once
}

func (s *Sub) close() { s.once.Do(func() { close(s.done) }) }

// queue bounds the events waiting for a slow browser; past it the stream is dropped and the page reconnects, which
// makes it refetch.
const queue = 64

// Broker holds the open streams of every space.
type Broker struct {
	mu     sync.Mutex
	subs   map[string]map[*Sub]struct{}
	closed bool
	gate   func(ctx context.Context, userID, machineID string) bool
	tap    func(spaceID string, e Event)
	// out, when set, gets what this process published, for the other replicas (Relay).
	out func(relayMsg)
}

// SetTap sets a listener that hears every event published to any space, after the streams were given it and outside
// the broker's lock: how another channel of the hub (the fleet stream) follows what the spaces are told.
func (b *Broker) SetTap(tap func(spaceID string, e Event)) {
	b.mu.Lock()
	b.tap = tap
	b.mu.Unlock()
}

// SetMachineGate sets the rule that decides, per event and per stream at delivery time, whether a person may see a
// machine. Without it every machine event is delivered.
func (b *Broker) SetMachineGate(g func(ctx context.Context, userID, machineID string) bool) {
	b.mu.Lock()
	b.gate = g
	b.mu.Unlock()
}

func (b *Broker) machineGate() func(ctx context.Context, userID, machineID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gate
}

func NewBroker() *Broker { return &Broker{subs: map[string]map[*Sub]struct{}{}} }

// Subscribe opens a stream for a member of a space; owner decides which events it gets. It is nil once the broker is
// closed.
func (b *Broker) Subscribe(spaceID, userID string, owner bool) *Sub {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	s := &Sub{spaceID: spaceID, userID: userID, owner: owner, ch: make(chan Event, queue), done: make(chan struct{})}
	if b.subs[spaceID] == nil {
		b.subs[spaceID] = map[*Sub]struct{}{}
	}
	b.subs[spaceID][s] = struct{}{}
	return s
}

// Unsubscribe closes a stream.
func (b *Broker) Unsubscribe(s *Sub) {
	b.mu.Lock()
	if set := b.subs[s.spaceID]; set != nil {
		delete(set, s)
		if len(set) == 0 {
			delete(b.subs, s.spaceID)
		}
	}
	b.mu.Unlock()
	s.close()
}

// Publish sends an event to the space's streams; it never waits for a slow one.
func (b *Broker) Publish(spaceID string, e Event) {
	b.publish(spaceID, e, true)
}

// publish delivers to this process's streams. own says the event happened here: then the tap hears it and the other
// replicas are told; an event taken from another replica only reaches the streams.
func (b *Broker) publish(spaceID string, e Event, own bool) {
	b.mu.Lock()
	for s := range b.subs[spaceID] {
		if e.OwnersOnly && !s.owner {
			continue
		}
		select {
		case s.ch <- e:
		default:
			metrics.StreamsDropped.Inc()
			delete(b.subs[spaceID], s)
			s.close()
		}
	}
	tap, out := b.tap, b.out
	b.mu.Unlock()
	if !own {
		return
	}
	if tap != nil {
		tap(spaceID, e)
	}
	if out != nil {
		out(relayMsg{Bus: relaySpace, Key: spaceID, Name: e.Name, Data: e.Data, OwnersOnly: e.OwnersOnly, Machine: e.MachineID})
	}
}

// PublishJSON marshals data and publishes it.
func (b *Broker) PublishJSON(spaceID, name string, data any, ownersOnly bool) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	b.Publish(spaceID, Event{Name: name, Data: raw, OwnersOnly: ownersOnly})
}

// PublishMachine is PublishJSON for an event about one machine; see Event.MachineID.
func (b *Broker) PublishMachine(spaceID, machineID, name string, data any, ownersOnly bool) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	b.Publish(spaceID, Event{Name: name, Data: raw, OwnersOnly: ownersOnly, MachineID: machineID})
}

// Kick ends the streams of a user in a space (they left, were removed, or their role changed): the page reconnects
// and is told again what it may see.
func (b *Broker) Kick(spaceID, userID string) {
	b.kick(spaceID, userID)
	b.mu.Lock()
	out := b.out
	b.mu.Unlock()
	if out != nil {
		out(relayMsg{Bus: relaySpaceKick, Key: spaceID, User: userID})
	}
}

func (b *Broker) kick(spaceID, userID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs[spaceID] {
		if s.userID == userID {
			delete(b.subs[spaceID], s)
			s.close()
		}
	}
}

// Close ends every stream and refuses new ones; the server calls it on shutdown, which would otherwise wait for them.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for id, set := range b.subs {
		for s := range set {
			s.close()
		}
		delete(b.subs, id)
	}
}

// Count is the number of open streams of a space; for tests.
func (b *Broker) Count(spaceID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[spaceID])
}

// PingEvery is how often an idle stream is written to, so proxies keep it open.
var PingEvery = 20 * time.Second

// Serve writes the stream to the browser until it goes away, is kicked, or the hub stops.
func (b *Broker) Serve(w http.ResponseWriter, r *http.Request, s *Sub) {
	defer b.Unsubscribe(s)
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, ": ok\n\n"); err != nil || rc.Flush() != nil {
		return
	}
	ping := time.NewTicker(PingEvery)
	defer ping.Stop()
	for {
		select {
		case e := <-s.ch:
			if g := b.machineGate(); e.MachineID != "" && g != nil && !g(r.Context(), s.userID, e.MachineID) {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, e.Data); err != nil {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case <-s.done:
			return
		case <-r.Context().Done():
			return
		}
		if rc.Flush() != nil {
			return
		}
	}
}

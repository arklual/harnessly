// Package events is the hub's live channel to the browser: one Server-Sent Events stream per open space. Every
// resource an event announces also has a GET, so a lost stream costs a refetch and never a wrong screen: nothing is
// replayed.
package events

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event is one thing that happened in a space.
type Event struct {
	Name string
	Data json.RawMessage
	// OwnersOnly keeps the event from members: install steps and login links are the owners' business.
	OwnersOnly bool
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
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs[spaceID] {
		if e.OwnersOnly && !s.owner {
			continue
		}
		select {
		case s.ch <- e:
		default:
			delete(b.subs[spaceID], s)
			s.close()
		}
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

// Kick ends the streams of a user in a space (they left, were removed, or their role changed): the page reconnects
// and is told again what it may see.
func (b *Broker) Kick(spaceID, userID string) {
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

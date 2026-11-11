package events

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// FleetBus is the live channel of the workspace page (GET /api/v2/machines/events): one stream per open page of a
// person. It says one thing, «the list of your machines may have changed», and carries nothing else: the page reads
// GET /api/v2/machines again and compares. A lost stream therefore costs a refetch, and a stream can never show a
// machine its person may not see.
//
// A stream is told when a machine of a space it watches changed (TouchSpace) or when its person's own place changed:
// they joined or left a space, or their role did (TouchUser), after which the spaces it watches are asked again.
//
// The bus lives in this process, like Broker and MemoryBus; with several replicas a Relay passes what one of them
// tells its streams to the streams of the others.
type FleetBus struct {
	mu     sync.Mutex
	subs   map[*FleetSub]struct{}
	closed bool
	// out, when set, gets what this process told its streams, for the other replicas (Relay).
	out func(relayMsg)
}

// FleetSub is one open fleet stream.
type FleetSub struct {
	userID string
	// spaces and stale are the bus's, under its lock.
	spaces map[string]struct{}
	stale  bool
	wake   chan struct{}
	done   chan struct{}
	once   sync.Once
}

func (s *FleetSub) close() { s.once.Do(func() { close(s.done) }) }

func (s *FleetSub) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Done is closed when the stream is over.
func (s *FleetSub) Done() <-chan struct{} { return s.done }

func NewFleetBus() *FleetBus { return &FleetBus{subs: map[*FleetSub]struct{}{}} }

func spaceSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// Subscribe opens a stream for a person who may see the machines of spaces. It is nil once the bus is closed.
func (b *FleetBus) Subscribe(userID string, spaces []string) *FleetSub {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	s := &FleetSub{userID: userID, spaces: spaceSet(spaces), wake: make(chan struct{}, 1), done: make(chan struct{})}
	b.subs[s] = struct{}{}
	return s
}

// Unsubscribe closes a stream.
func (b *FleetBus) Unsubscribe(s *FleetSub) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
	s.close()
}

// TouchSpace tells the streams that watch a space that a machine of it was added, removed or changed. It never waits.
func (b *FleetBus) TouchSpace(spaceID string) {
	if out := b.touchSpace(spaceID); out != nil {
		out(relayMsg{Bus: relayFleetSpace, Key: spaceID})
	}
}

func (b *FleetBus) touchSpace(spaceID string) func(relayMsg) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		if _, ok := s.spaces[spaceID]; ok {
			s.poke()
		}
	}
	return b.out
}

// TouchUser tells a person's streams that the spaces they may see changed: the stream asks for them again before it
// tells the page.
func (b *FleetBus) TouchUser(userID string) {
	if out := b.touchUser(userID); out != nil {
		out(relayMsg{Bus: relayFleetUser, User: userID})
	}
}

func (b *FleetBus) touchUser(userID string) func(relayMsg) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		if s.userID == userID {
			s.stale = true
			s.poke()
		}
	}
	return b.out
}

// Close ends every stream and refuses new ones.
func (b *FleetBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for s := range b.subs {
		s.close()
		delete(b.subs, s)
	}
}

// Count is the number of open streams of a person; for tests.
func (b *FleetBus) Count(userID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for s := range b.subs {
		if s.userID == userID {
			n++
		}
	}
	return n
}

func (b *FleetBus) takeStale(s *FleetSub) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	was := s.stale
	s.stale = false
	return was
}

func (b *FleetBus) watch(s *FleetSub, spaces []string) {
	b.mu.Lock()
	s.spaces = spaceSet(spaces)
	b.mu.Unlock()
}

// FleetSettle is how long a stream waits after a change before it tells the page, so a burst of changes (a machine
// that registers, connects and reports its agents) is one refetch and not five.
var FleetSettle = 200 * time.Millisecond

// Serve writes the stream to the browser until it goes away or the hub stops. spaces answers which spaces the person
// may see machines of now; when it fails the stream ends, and the page's reconnect is its refetch.
func (b *FleetBus) Serve(w http.ResponseWriter, r *http.Request, s *FleetSub, spaces func(ctx context.Context) ([]string, error)) {
	defer b.Unsubscribe(s)
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 3000\n: ok\n\n"); err != nil || rc.Flush() != nil {
		return
	}
	ping := time.NewTicker(PingEvery)
	defer ping.Stop()
	for {
		select {
		case <-s.wake:
			settle := time.NewTimer(FleetSettle)
			select {
			case <-settle.C:
			case <-s.done:
				settle.Stop()
				return
			case <-r.Context().Done():
				settle.Stop()
				return
			}
			select {
			case <-s.wake:
			default:
			}
			if b.takeStale(s) {
				ids, err := spaces(r.Context())
				if err != nil {
					return
				}
				b.watch(s, ids)
			}
			if _, err := fmt.Fprint(w, "event: fleet\ndata: {}\n\n"); err != nil {
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

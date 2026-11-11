package events

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// The organization's stream (GET /api/orgs/{org}/events) is the live channel of the hub's screens: a tab keeps one
// connection, and the data layer drops the cache keys an event names. Like the space stream, it never carries the only
// copy of anything: every resource an event announces has a GET, so a lost event costs a refetch and never a wrong
// screen. For the short gaps there is a ring of the last events per organization for Last-Event-ID; past it the
// stream says `resync` and the page refetches.

// Publisher is how the rest of the hub says that something happened in an organization. kind is the event name
// (`machine.status`, `usage.tick`, `signal.new`, …), data anything that marshals to JSON. It never blocks and never
// fails: an event nobody listens to costs a map lookup. data may be wrapped by For to say who may see it.
type Publisher interface {
	Publish(orgID, kind string, data any)
}

// The roles an event can be held back for. An event is shown to a stream whose person has at least the role.
const (
	RoleMember = "member"
	RoleLead   = "lead"
	RoleAdmin  = "admin"
)

func rank(role string) int {
	switch role {
	case RoleAdmin:
		return 3
	case RoleLead:
		return 2
	case RoleMember:
		return 1
	}
	return 0
}

// Audience says who may see an event. The zero value is the safe one: the leads and admins of the organization,
// whatever the tenant.
type Audience struct {
	// Role is the least role that sees the event; "" means RoleLead.
	Role string
	// Space is the tenant (space id) the event belongs to; "" an event of the organization as a whole. A stream of one
	// tenant gets only the events of that tenant; a stream of the organization gets those and the tenants' ones.
	Space string
	// Only keeps a tenant's event from the stream of the whole organization (the tenant's own numbers).
	Only bool
	// User keeps the event for one person's streams, whatever the role.
	User string
}

// Audienced is what an event's data implements to carry its Audience.
type Audienced interface{ EventAudience() Audience }

type withAudience struct {
	aud  Audience
	data any
}

func (w withAudience) EventAudience() Audience      { return w.aud }
func (w withAudience) MarshalJSON() ([]byte, error) { return json.Marshal(w.data) }

// For wraps data with the audience that may see it; the wrapper marshals as data does.
func For(aud Audience, data any) any { return withAudience{aud: aud, data: data} }

// OrgEvent is one event on the stream.
type OrgEvent struct {
	// ID grows with every event of the process (and across restarts: it starts from the clock), so Last-Event-ID
	// from before a restart is known to be from before it.
	ID   int64
	Kind string
	Data json.RawMessage
	Aud  Audience
}

// Filter is what a stream is allowed to see.
type Filter struct {
	UserID string
	Role   string
	// Space is the tenant of a tenant stream (space id); "" the whole organization.
	Space string
}

// Sees says whether the stream may be shown e.
func (f Filter) Sees(e OrgEvent) bool {
	a := e.Aud
	if a.User != "" {
		return a.User == f.UserID
	}
	need := a.Role
	if need == "" {
		need = RoleLead
	}
	if rank(f.Role) < rank(need) {
		return false
	}
	if f.Space != "" {
		return a.Space == f.Space
	}
	return !(a.Only && a.Space != "")
}

// Listener is an organization (and tenant) with at least one open stream.
type Listener struct{ OrgID, Space string }

// OrgBus is the organization's event bus. MemoryBus is the one process's; a bus over Postgres LISTEN/NOTIFY can stand
// in for it when the hub runs on more than one replica, because the rest of the hub speaks only to this interface.
type OrgBus interface {
	Publisher
	// Subscribe opens a stream. With lastID the events after it that the filter may see are queued first, or, when the
	// bus no longer has all of them, resync is set on the stream.
	Subscribe(orgID string, f Filter, lastID *int64) *OrgSub
	// Unsubscribe closes a stream.
	Unsubscribe(s *OrgSub)
	// Active lists the organizations and tenants somebody is listening to, for the emitters that work only then.
	Active() []Listener
	// Kick ends the streams of a person in an organization (they left or their role changed).
	Kick(orgID, userID string)
	// Close ends every stream and refuses new ones.
	Close()
}

// OrgSub is one open organization stream.
type OrgSub struct {
	orgID  string
	filter Filter
	ch     chan OrgEvent
	wake   chan struct{}
	done   chan struct{}
	once   sync.Once
	lost   atomic.Bool
}

// OrgBuffer bounds the events waiting for a slow browser. Past it events are dropped and the stream sends `resync`.
const OrgBuffer = 64

// OrgRing is how many events per organization are kept for Last-Event-ID.
const OrgRing = 500

func (s *OrgSub) close() { s.once.Do(func() { close(s.done) }) }

func (s *OrgSub) markLost() {
	s.lost.Store(true)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Done is closed when the stream is over.
func (s *OrgSub) Done() <-chan struct{} { return s.done }

// Filter is what the stream may see.
func (s *OrgSub) Filter() Filter { return s.filter }

type orgRing struct {
	events []OrgEvent
	// floor is the id of the newest event that fell out of the ring (or the id the bus started from): a client that
	// has seen less than floor has missed something that is gone.
	floor int64
}

// MemoryBus keeps everything in the process.
type MemoryBus struct {
	mu     sync.Mutex
	next   int64
	start  int64
	rings  map[string]*orgRing
	subs   map[string]map[*OrgSub]struct{}
	closed bool
	// out, when set, gets what this process published, for the other replicas (Relay).
	out func(relayMsg)
}

// NewMemoryBus starts a bus whose ids begin at the current time in milliseconds.
func NewMemoryBus(now func() time.Time) *MemoryBus {
	if now == nil {
		now = time.Now
	}
	start := now().UnixMilli() * 1000
	return &MemoryBus{next: start, start: start, rings: map[string]*orgRing{}, subs: map[string]map[*OrgSub]struct{}{}}
}

var _ OrgBus = (*MemoryBus)(nil)

// Publish implements Publisher.
func (b *MemoryBus) Publish(orgID, kind string, data any) {
	var aud Audience
	if a, ok := data.(Audienced); ok {
		aud = a.EventAudience()
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	if out := b.publish(orgID, kind, raw, aud); out != nil {
		out(relayMsg{Bus: relayOrg, Key: orgID, Name: kind, Data: raw, Aud: &aud})
	}
}

// publish gives an event to this process's streams and ring, under an id of this process, and returns where the
// other replicas are told of it.
func (b *MemoryBus) publish(orgID, kind string, raw json.RawMessage, aud Audience) func(relayMsg) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.next++
	e := OrgEvent{ID: b.next, Kind: kind, Data: raw, Aud: aud}
	r := b.rings[orgID]
	if r == nil {
		r = &orgRing{floor: b.start}
		b.rings[orgID] = r
	}
	r.events = append(r.events, e)
	if len(r.events) > OrgRing {
		r.floor = r.events[0].ID
		r.events = append(r.events[:0], r.events[len(r.events)-OrgRing:]...)
	}
	for s := range b.subs[orgID] {
		if !s.filter.Sees(e) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			s.markLost()
		}
	}
	return b.out
}

// Subscribe implements OrgBus. It is nil once the bus is closed.
func (b *MemoryBus) Subscribe(orgID string, f Filter, lastID *int64) *OrgSub {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	s := &OrgSub{orgID: orgID, filter: f, ch: make(chan OrgEvent, OrgBuffer), wake: make(chan struct{}, 1), done: make(chan struct{})}
	if lastID != nil {
		r := b.rings[orgID]
		floor := b.start
		if r != nil {
			floor = r.floor
		}
		switch {
		case *lastID < floor || *lastID > b.next:
			// Events the ring has let go, or ids of another run of the hub.
			s.lost.Store(true)
			s.wake <- struct{}{}
		case r != nil:
			for _, e := range r.events {
				if e.ID <= *lastID || !f.Sees(e) {
					continue
				}
				select {
				case s.ch <- e:
				default:
					s.lost.Store(true)
					s.wake <- struct{}{}
				}
				if s.lost.Load() {
					break
				}
			}
		}
	}
	if b.subs[orgID] == nil {
		b.subs[orgID] = map[*OrgSub]struct{}{}
	}
	b.subs[orgID][s] = struct{}{}
	return s
}

// Unsubscribe implements OrgBus.
func (b *MemoryBus) Unsubscribe(s *OrgSub) {
	b.mu.Lock()
	if set := b.subs[s.orgID]; set != nil {
		delete(set, s)
		if len(set) == 0 {
			delete(b.subs, s.orgID)
		}
	}
	b.mu.Unlock()
	s.close()
}

// Active implements OrgBus; the list is sorted for stable ticks.
func (b *MemoryBus) Active() []Listener {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[Listener]bool{}
	for org, set := range b.subs {
		for s := range set {
			seen[Listener{OrgID: org, Space: s.filter.Space}] = true
		}
	}
	out := make([]Listener, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OrgID != out[j].OrgID {
			return out[i].OrgID < out[j].OrgID
		}
		return out[i].Space < out[j].Space
	})
	return out
}

// Count is the number of open streams of an organization; for tests.
func (b *MemoryBus) Count(orgID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[orgID])
}

// Kick implements OrgBus.
func (b *MemoryBus) Kick(orgID, userID string) {
	if out := b.kick(orgID, userID); out != nil {
		out(relayMsg{Bus: relayOrgKick, Key: orgID, User: userID})
	}
}

func (b *MemoryBus) kick(orgID, userID string) func(relayMsg) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs[orgID] {
		if s.filter.UserID == userID {
			delete(b.subs[orgID], s)
			s.close()
		}
	}
	return b.out
}

// Close implements OrgBus.
func (b *MemoryBus) Close() {
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

// HeartbeatEvery is how often an idle organization stream gets a comment line, so proxies keep it open.
var HeartbeatEvery = 25 * time.Second

// ServeOrg writes the stream to the browser until it goes away, is kicked, or the hub stops. The bus is asked to end
// the subscription on the way out.
func ServeOrg(w http.ResponseWriter, r *http.Request, bus OrgBus, s *OrgSub) {
	defer bus.Unsubscribe(s)
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
	beat := time.NewTicker(HeartbeatEvery)
	defer beat.Stop()
	for {
		// A stream that dropped events says so before it goes on: the page refetches what it shows.
		if s.lost.Swap(false) {
			if _, err := fmt.Fprint(w, "event: resync\ndata: {}\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
		select {
		case e := <-s.ch:
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Kind, e.Data); err != nil {
				return
			}
		case <-s.wake:
			continue
		case <-beat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
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

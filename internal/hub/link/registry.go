package link

import (
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/metrics"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Registry holds the live link of each machine. The hub is one process, so this map is the whole truth about who is
// connected.
type Registry struct {
	mu      sync.Mutex
	links   map[string]*Link
	cluster *Cluster
	// seen are the machines this process has had a link of: a link of one of them is a reconnect.
	seen map[string]struct{}
}

func NewRegistry() *Registry {
	return &Registry{links: map[string]*Link{}, seen: map[string]struct{}{}}
}

// Stats is what the links this process holds carry at one moment.
type Stats struct {
	// Links is the live links; Tunnels the open tunnels; Calls the requests to agents waiting for an answer.
	Links, Tunnels, Calls int
	// Events and Bulk are the events queued for the hub: the ordinary ones and the frames of the chats' history.
	Events, Bulk int
	// OldestEvent and OldestBulk are how long the oldest event of each queue that is not handled yet has waited.
	OldestEvent, OldestBulk time.Duration
}

// Stats counts what the local links hold. It is for a scrape of the metrics: it takes every link's lock in turn.
func (r *Registry) Stats() Stats {
	r.mu.Lock()
	links := make([]*Link, 0, len(r.links))
	for _, l := range r.links {
		if !l.Closed() {
			links = append(links, l)
		}
	}
	r.mu.Unlock()
	st := Stats{Links: len(links)}
	now := time.Now().UnixNano()
	age := func(head int64, oldest *time.Duration) {
		if head != 0 && time.Duration(now-head) > *oldest {
			*oldest = time.Duration(now - head)
		}
	}
	for _, l := range links {
		tunnels, calls, events, bulk, eventHead, bulkHead := l.load()
		st.Tunnels, st.Calls, st.Events, st.Bulk = st.Tunnels+tunnels, st.Calls+calls, st.Events+events, st.Bulk+bulk
		age(eventHead, &st.OldestEvent)
		age(bulkHead, &st.OldestBulk)
	}
	return st
}

// Get is the machine's link, or nil when it is not connected.
func (r *Registry) Get(machineID string) *Link {
	if l := r.local(machineID); l != nil {
		return l
	}
	if r.cluster != nil {
		// A stand-in for a link another replica holds: Call and OpenConn go to the owner.
		return r.cluster.lookup(machineID)
	}
	return nil
}

// local is the live link this process holds.
func (r *Registry) local(machineID string) *Link {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.links[machineID]
	if l == nil || l.Closed() {
		return nil
	}
	return l
}

// takeLocal removes and returns the link this process holds.
func (r *Registry) takeLocal(machineID string) *Link {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.links[machineID]
	delete(r.links, machineID)
	return l
}

// Add makes l the machine's link. A link the machine had is closed with 4011: the agent that dialed twice redials
// normally, a stale one just goes.
func (r *Registry) Add(l *Link) {
	if r.cluster != nil {
		r.cluster.acquire(r, l)
	}
	l.registered.Store(true)
	r.mu.Lock()
	old := r.links[l.MachineID]
	r.links[l.MachineID] = l
	_, again := r.seen[l.MachineID]
	r.seen[l.MachineID] = struct{}{}
	r.mu.Unlock()
	metrics.LinkConnects.Inc()
	if again {
		metrics.LinkReconnects.Inc()
	}
	if old != nil {
		old.Close(websocket.StatusCode(hubproto.CloseReplaced), "replaced by a newer link")
	}
}

// Remove forgets l if it is still the machine's link; it says whether it was.
func (r *Registry) Remove(l *Link) bool {
	r.mu.Lock()
	current := r.links[l.MachineID] == l
	if current {
		delete(r.links, l.MachineID)
	}
	r.mu.Unlock()
	if r.cluster != nil && l.fence != 0 {
		r.cluster.release(l) // guarded by the fence: it does nothing when a newer owner holds the machine
	}
	return current
}

// Revoke closes the machine's link with 4010: the agent stops dialing until it is restarted.
func (r *Registry) Revoke(machineID string) {
	r.mu.Lock()
	l := r.links[machineID]
	delete(r.links, machineID)
	r.mu.Unlock()
	if l == nil && r.cluster != nil {
		r.cluster.closeRemote(machineID, hubproto.CloseRevoked, "machine revoked")
	}
	if l != nil {
		l.Close(websocket.StatusCode(hubproto.CloseRevoked), "machine revoked")
	}
}

// Disable closes the machine's link with 4003 (the pair is switched off): the agent waits a minute and dials again,
// which the dialer refuses while the machine is disabled; its key stays good, so it links again once it is enabled.
func (r *Registry) Disable(machineID string) {
	r.mu.Lock()
	l := r.links[machineID]
	delete(r.links, machineID)
	r.mu.Unlock()
	if l == nil && r.cluster != nil {
		r.cluster.closeRemote(machineID, hubproto.CloseSwitchedOff, "machine disabled")
	}
	if l != nil {
		l.Close(websocket.StatusCode(hubproto.CloseSwitchedOff), "machine disabled")
	}
}

// Reconnect closes the machine's link as a hub that goes away does: the agent dials again at once, and the hub reads the
// machine anew (its owner changed).
func (r *Registry) Reconnect(machineID string) {
	r.mu.Lock()
	l := r.links[machineID]
	delete(r.links, machineID)
	r.mu.Unlock()
	if l == nil && r.cluster != nil {
		r.cluster.closeRemote(machineID, int(websocket.StatusGoingAway), "machine owner changed")
	}
	if l != nil {
		l.Close(websocket.StatusGoingAway, "machine owner changed")
	}
}

// Online lists the machines with a live link, sorted.
func (r *Registry) Online() []string {
	r.mu.Lock()
	var out []string
	for id, l := range r.links {
		if !l.Closed() {
			out = append(out, id)
		}
	}
	r.mu.Unlock()
	if r.cluster != nil {
		for _, id := range r.cluster.remoteOnline() {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// CloseAll ends every link; the hub does it when it stops, so the agents redial the next instance at once.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	links := r.links
	r.links = map[string]*Link{}
	r.mu.Unlock()
	for _, l := range links {
		l.Close(websocket.StatusGoingAway, "hub restarting")
	}
}

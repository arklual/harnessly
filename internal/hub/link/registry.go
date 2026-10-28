package link

import (
	"sort"
	"sync"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Registry holds the live link of each machine. The hub is one process, so this map is the whole truth about who is
// connected.
type Registry struct {
	mu    sync.Mutex
	links map[string]*Link
}

func NewRegistry() *Registry { return &Registry{links: map[string]*Link{}} }

// Get is the machine's link, or nil when it is not connected.
func (r *Registry) Get(machineID string) *Link {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.links[machineID]
	if l == nil || l.Closed() {
		return nil
	}
	return l
}

// Add makes l the machine's link. A link the machine had is closed with 4011: the agent that dialed twice redials
// normally, a stale one just goes.
func (r *Registry) Add(l *Link) {
	r.mu.Lock()
	old := r.links[l.MachineID]
	r.links[l.MachineID] = l
	r.mu.Unlock()
	if old != nil {
		old.Close(websocket.StatusCode(hubproto.CloseReplaced), "replaced by a newer link")
	}
}

// Remove forgets l if it is still the machine's link; it says whether it was.
func (r *Registry) Remove(l *Link) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.links[l.MachineID] == l {
		delete(r.links, l.MachineID)
		return true
	}
	return false
}

// Revoke closes the machine's link with 4010: the agent stops dialing until it is restarted.
func (r *Registry) Revoke(machineID string) {
	r.mu.Lock()
	l := r.links[machineID]
	delete(r.links, machineID)
	r.mu.Unlock()
	if l != nil {
		l.Close(websocket.StatusCode(hubproto.CloseRevoked), "machine revoked")
	}
}

// Online lists the machines with a live link, sorted.
func (r *Registry) Online() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for id, l := range r.links {
		if !l.Closed() {
			out = append(out, id)
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

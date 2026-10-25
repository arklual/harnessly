package wire

import "time"

// SetSocketIdle makes the socket's idle limit d for one test.
func SetSocketIdle(d time.Duration) (restore func()) {
	old := socketIdle.Swap(int64(d))
	return func() { socketIdle.Store(old) }
}

// Subscriptions is how many channel subscriptions the hub's sockets hold, and Clients how many sockets it holds.
func (h *Hub) Subscriptions() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, hc := range h.chans {
		n += len(hc.subs)
	}
	return n
}

func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// MachineDemand is how many machine subscriptions the hub counts.
func (h *Hub) MachineDemand() int {
	h.srcMu.Lock()
	defer h.srcMu.Unlock()
	return len(h.machineSubs)
}

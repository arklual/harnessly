package wire

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// Hub numbers the events of every channel, keeps their rings and hands each event to the sockets subscribed to it.
// It is the Publisher of the conversations (convo.Publisher) and of the machine source.
//
// The order of a subscription is a protocol invariant (README 21): the hub subscribes a socket to the channel before
// the snapshot is taken, holds the events that arrive meanwhile, and sends them after the snapshot, which carries the
// seq the channel had when the socket was subscribed. The page drops events at or below that seq and applies the rest
// idempotently by id and rev.
type Hub struct {
	now func() time.Time

	mu      sync.Mutex
	epoch   string
	chans   map[Channel]*hubChannel
	clients map[*client]struct{}

	// The machine source is subscribed while any socket holds the machine channel, with load while any asked for it.
	machine     MachineSource
	srcMu       sync.Mutex
	srcCancel   func()
	srcLoad     bool
	machineSubs map[*subscription]bool // the machine channel's subscriptions and whether each asked for load
}

type hubChannel struct {
	seq  int64
	ring ring
	subs map[*subscription]struct{}
}

// subscription is one socket on one channel. Until the snapshot is out it holds the channel's events (held, under the
// socket's lock).
type subscription struct {
	c    *client
	ch   Channel
	hold bool
	held [][]byte
}

// NewHub starts a hub with a new epoch; machine may be nil when nothing serves the machine channel.
func NewHub(machine MachineSource) *Hub {
	return &Hub{now: time.Now, epoch: NewEpoch(), chans: map[Channel]*hubChannel{}, clients: map[*client]struct{}{},
		machine: machine, machineSubs: map[*subscription]bool{}}
}

// NewEpoch is a new epoch: "ep-" and 16 random base32 characters.
func NewEpoch() string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return "ep-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

// Epoch is the hub's current epoch.
func (h *Hub) Epoch() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.epoch
}

// Seq is a channel's last seq, 0 before its first event.
func (h *Hub) Seq(ch Channel) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.chans[ch]; c != nil {
		return c.seq
	}
	return 0
}

func (h *Hub) channel(ch Channel) *hubChannel {
	c := h.chans[ch]
	if c == nil {
		c = &hubChannel{subs: map[*subscription]struct{}{}}
		h.chans[ch] = c
	}
	return c
}

// Publish gives e the next seq of ch, keeps it in the channel's ring and sends it to the channel's sockets. An event
// that does not go on ch, or does not encode, is a bug of the caller: it is logged and dropped, and the seq is 0.
func (h *Hub) Publish(ch Channel, e Event) (seq int64) {
	if e == nil || !slices.Contains(ChannelEvents(ch), e.EventType()) {
		agent.Log("error", "v2: event dropped, not of its channel", agent.F{K: "ch", V: string(ch)}, agent.F{K: "event", V: fmt.Sprintf("%T", e)})
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.channel(ch)
	frame, err := Marshal(EvFrame{Ch: ch, Seq: c.seq + 1, E: e})
	if err != nil {
		agent.Log("error", "v2: event dropped, does not encode", agent.F{K: "ch", V: string(ch)}, agent.F{K: "error", V: err.Error()})
		return 0
	}
	var older map[Caps][]byte
	for _, caps := range olderCaps {
		old, changed := eventFor(e, caps)
		if !changed {
			continue
		}
		b, err := Marshal(EvFrame{Ch: ch, Seq: c.seq + 1, E: old})
		if err != nil {
			agent.Log("error", "v2: event dropped, its older form does not encode", agent.F{K: "ch", V: string(ch)}, agent.F{K: "error", V: err.Error()})
			return 0
		}
		if older == nil {
			older = map[Caps][]byte{}
		}
		older[caps] = b
	}
	c.seq++
	it := ringItem{seq: c.seq, at: h.now(), frame: frame, older: older}
	c.ring.push(it)
	for s := range c.subs {
		s.c.deliver(s, it.forCaps(s.c.caps))
	}
	return c.seq
}

// subscribe puts c on ch. When the cursor (same epoch, afterSeq in the ring) covers what c missed, the missed events
// are queued and the subscription is live: replayed is true. Otherwise the subscription holds the events until
// release is called with the snapshot, which must carry seq. A socket the hub does not hold (detached) gets nil.
func (h *Hub) subscribe(c *client, ch Channel, epoch string, afterSeq *int64) (s *subscription, seq int64, replayed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.detached {
		return nil, 0, false
	}
	h.unsubscribeLocked(c, ch)
	hc := h.channel(ch)
	s = &subscription{c: c, ch: ch}
	if epoch == h.epoch && afterSeq != nil {
		if frames, ok := hc.ring.since(*afterSeq, hc.seq, h.now(), c.caps); ok {
			c.subs[ch] = s
			hc.subs[s] = struct{}{}
			c.enqueue(frames...)
			// A conversation's replay ends with a snapshot "after" it (Server.sub): what comes meanwhile waits for it.
			_, s.hold = ch.ConvID()
			return s, hc.seq, true
		}
	}
	s.hold = true
	c.subs[ch] = s
	hc.subs[s] = struct{}{}
	return s, hc.seq, false
}

// release sends a held subscription's snapshot and then the events it held, and makes it live.
func (h *Hub) release(s *subscription, snapshot []byte) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if !s.hold {
		return
	}
	if snapshot != nil {
		s.c.enqueueLocked(snapshot)
	}
	s.c.enqueueLocked(s.held...)
	s.hold, s.held = false, nil
}

// Subscribers is how many sockets hold ch now: a conv:<id> with any is a chat someone has open.
func (h *Hub) Subscribers(ch Channel) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.chans[ch]; c != nil {
		return len(c.subs)
	}
	return 0
}

// unsubscribe takes c off ch; false when c was not on it.
func (h *Hub) unsubscribe(c *client, ch Channel) bool {
	h.mu.Lock()
	s := h.unsubscribeLocked(c, ch)
	h.mu.Unlock()
	if s != nil && ch == ChannelMachine {
		h.machineDemand(s, false, false)
	}
	return s != nil
}

func (h *Hub) unsubscribeLocked(c *client, ch Channel) *subscription {
	s := c.subs[ch]
	if s == nil {
		return nil
	}
	delete(c.subs, ch)
	if hc := h.chans[ch]; hc != nil {
		delete(hc.subs, s)
	}
	return s
}

// attach and detach keep the hub's list of sockets, for Reset and Close.
func (h *Hub) attach(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *Hub) detach(c *client) {
	h.mu.Lock()
	var machine *subscription
	for ch := range c.subs {
		if s := h.unsubscribeLocked(c, ch); ch == ChannelMachine {
			machine = s
		}
	}
	delete(h.clients, c)
	c.detached = true
	h.mu.Unlock()
	if machine != nil {
		h.machineDemand(machine, false, false)
	}
}

// Reset starts a new epoch, as a restart of the runtime does: every channel starts again from seq 0 with an empty
// ring, and every socket is closed so its page comes back with its cursor.
func (h *Hub) Reset() {
	h.mu.Lock()
	h.epoch = NewEpoch()
	h.chans = map[Channel]*hubChannel{}
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.kill(closeRestart, "restart")
	}
}

// Close closes every socket, as the runtime stops.
func (h *Hub) Close() {
	h.mu.Lock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.kill(closeGoingAway, "shutting down")
	}
}

// Drop cuts every socket without a close frame, as a network that goes away does.
func (h *Hub) Drop() {
	h.mu.Lock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.cut()
	}
}

// machineDemand counts a machine subscription in (on) or out, and subscribes the machine source again when the
// demand changed: from none to some, from some to none, or for load.
func (h *Hub) machineDemand(s *subscription, on, load bool) {
	if h.machine == nil {
		return
	}
	h.srcMu.Lock()
	defer h.srcMu.Unlock()
	if on {
		h.machineSubs[s] = load
	} else {
		delete(h.machineSubs, s)
	}
	wantLoad := false
	for _, l := range h.machineSubs {
		wantLoad = wantLoad || l
	}
	switch {
	case len(h.machineSubs) == 0:
		if h.srcCancel != nil {
			h.srcCancel()
			h.srcCancel = nil
		}
	case h.srcCancel == nil || wantLoad != h.srcLoad:
		if h.srcCancel != nil {
			h.srcCancel()
		}
		h.srcLoad = wantLoad
		h.srcCancel = h.machine.Subscribe(wantLoad, func(e Event) { h.Publish(ChannelMachine, e) })
	}
}

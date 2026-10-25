// Package sockets serves the runtime's JSON sockets: /api/status (tab states, background work, the prompt queue, pane
// intents, limits) and /api/sync (layout and workspace changes, toasts and commands to the page). One of the clients is
// usually the machine agent's relay into the cluster stream; the rest are pages.
package sockets

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

const (
	// backpressure: a client with this much unsent is skipped by broadcasts, like a WebSocket's bufferedAmount.
	backpressure   = 1 << 20
	keepaliveEvery = 25 * time.Second
	writeTimeout   = 30 * time.Second
	compressFrom   = 1024 // JSON compresses 5×; the first status sync alone is ~590 KB raw
)

// PingFrame and PongFrame are the page's own heartbeat: a browser cannot see protocol pings, so a socket the network
// dropped silently looks open to it for minutes.
const (
	PingFrame = `{"type":"ping"}`
	PongFrame = `{"type":"pong"}`
)

// Client is one open socket.
type Client struct {
	ws    *websocket.Conn
	Relay bool // the machine agent's socket (?relay=1)

	mu      sync.Mutex
	queue   [][]byte
	queued  int
	wake    chan struct{}
	dropped bool
	done    chan struct{}
	once    sync.Once
	// resync is called once a client that missed messages has drained: it gets a fresh snapshot instead of a gap.
	resync func(*Client)
}

func newClient(ws *websocket.Conn, relay bool) *Client {
	return &Client{ws: ws, Relay: relay, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

// Send queues one message; false when the client is gone or too far behind (the message is dropped).
func (c *Client) Send(raw []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return false
	default:
	}
	if c.queued >= backpressure {
		c.dropped = true
		return false
	}
	c.queue = append(c.queue, raw)
	c.queued += len(raw)
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return true
}

// SendJSON queues a value written as JSON.stringify writes it.
func (c *Client) SendJSON(v any) bool { return c.Send(nodejson.Marshal(v)) }

func (c *Client) writer() {
	for {
		select {
		case <-c.done:
			return
		case <-c.wake:
		}
		for {
			c.mu.Lock()
			if len(c.queue) == 0 {
				resync := c.dropped && c.resync != nil
				c.dropped = false
				c.mu.Unlock()
				if resync {
					c.resync(c)
				}
				break
			}
			raw := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
			err := c.ws.Write(ctx, websocket.MessageText, raw)
			cancel()
			c.mu.Lock()
			c.queued -= len(raw)
			c.mu.Unlock()
			if err != nil {
				c.Close(websocket.StatusGoingAway, "")
				return
			}
		}
	}
}

func (c *Client) keepalive() {
	t := time.NewTicker(keepaliveEvery)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			// No pong within a round means the peer is gone.
			ctx, cancel := context.WithTimeout(context.Background(), keepaliveEvery)
			err := c.ws.Ping(ctx)
			cancel()
			if err != nil {
				c.Close(websocket.StatusGoingAway, "")
				return
			}
		}
	}
}

// Close ends the socket once.
func (c *Client) Close(code websocket.StatusCode, reason string) {
	c.once.Do(func() {
		close(c.done)
		if c.ws != nil {
			go func() { _ = c.ws.Close(code, reason) }()
		}
	})
}

// Done is closed when the socket ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Hub is the set of open sockets of one kind.
type Hub struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[*Client]struct{}{}} }

func (h *Hub) add(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *Hub) snapshot() []*Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		out = append(out, c)
	}
	return out
}

// Broadcast sends v to every client but exclude; a client too far behind misses it.
func (h *Hub) Broadcast(v any, exclude *Client) {
	raw := nodejson.Marshal(v)
	for _, c := range h.snapshot() {
		if c != exclude {
			c.Send(raw)
		}
	}
}

// Count is the number of open sockets, the relay included.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Shutdown closes every socket.
func (h *Hub) Shutdown() {
	for _, c := range h.snapshot() {
		c.Close(websocket.StatusGoingAway, "Server shutting down")
	}
}

// accept upgrades a request and runs the socket until it ends; onMessage gets every text message but the heartbeat.
func (h *Hub) accept(w http.ResponseWriter, r *http.Request, onOpen func(*Client), onMessage func(*Client, []byte), resync func(*Client)) {
	if sameorigin.Refuse(w, r) {
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify:   true,
		CompressionMode:      websocket.CompressionContextTakeover,
		CompressionThreshold: compressFrom,
	})
	if err != nil {
		return
	}
	ws.SetReadLimit(1 << 20)
	c := newClient(ws, r.URL.Query().Get("relay") == "1")
	c.resync = resync
	h.add(c)
	defer h.remove(c)
	go c.writer()
	go c.keepalive()
	if onOpen != nil {
		onOpen(c)
	}
	for {
		typ, data, err := ws.Read(context.Background())
		if err != nil {
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		if string(data) == PingFrame {
			c.Send([]byte(PongFrame))
			continue
		}
		if onMessage != nil {
			onMessage(c, data)
		}
	}
}

// decode reads a client message; ok false for anything but a JSON object.
func decode(data []byte) (map[string]any, bool) {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

package host

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

type client struct {
	conn     net.Conn
	frames   chan []byte
	done     chan struct{}
	once     sync.Once
	sessions map[string]bool // guarded by relay.mu
}

func (c *client) close() { c.once.Do(func() { close(c.done); _ = c.conn.Close() }) }

type request struct {
	client *client
	id     json.RawMessage
	method string
}

type relay struct {
	mu      sync.Mutex
	current *client
	chrome  *cdp.Stream
	next    uint32
	pending map[uint32]request
	closed  bool
}

func (r *relay) detach(sessions []string) {
	for _, session := range sessions {
		// Detaching alone leaves a native modal dialog without a DevTools handler.
		// Cancel it first so a later client can attach and use the renderer again.
		dialog, _ := json.Marshal(map[string]any{"id": 0, "sessionId": session,
			"method": "Page.handleJavaScriptDialog", "params": map[string]bool{"accept": false}})
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		_ = r.chrome.Write(closeCtx, dialog)
		closeCancel()
		raw, _ := json.Marshal(map[string]any{"id": 0, "method": "Target.detachFromTarget",
			"params": map[string]string{"sessionId": session}})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = r.chrome.Write(ctx, raw)
		cancel()
	}
}

func (r *relay) disconnect(c *client) {
	c.close()
	r.mu.Lock()
	if r.current == c {
		r.current = nil
	}
	sessions := make([]string, 0, len(c.sessions))
	for session := range c.sessions {
		sessions = append(sessions, session)
	}
	c.sessions = nil
	for id, req := range r.pending {
		if req.client == c && req.method != "Target.attachToTarget" {
			delete(r.pending, id)
		}
	}
	r.mu.Unlock()
	// Detach stops the old client's screencasts without closing any browser tabs.
	r.detach(sessions)
}

func (r *relay) replace(conn net.Conn) {
	c := &client{conn: conn, frames: make(chan []byte, 32), done: make(chan struct{}), sessions: map[string]bool{}}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = conn.Close()
		return
	}
	if r.pending == nil {
		r.pending = make(map[uint32]request)
	}
	if r.current != nil {
		r.current.close()
	}
	r.current = c
	r.mu.Unlock()
	stream := cdp.NewStream(conn)
	go func() {
		defer c.close()
		for {
			select {
			case <-c.done:
				return
			case raw := <-c.frames:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := stream.Write(ctx, raw)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()
	go func() {
		defer r.disconnect(c)
		for {
			raw, err := stream.Read(context.Background())
			if err != nil {
				return
			}
			var msg map[string]json.RawMessage
			if json.Unmarshal(raw, &msg) != nil || msg["id"] == nil {
				continue
			}
			var method string
			_ = json.Unmarshal(msg["method"], &method)
			r.mu.Lock()
			if r.current != c {
				r.mu.Unlock()
				return
			}
			// Chrome uses signed 32-bit IDs. The relay, not the reconnecting client, owns their namespace.
			for {
				r.next++
				if r.next >= 1<<31 {
					r.next = 1
				}
				if _, used := r.pending[r.next]; !used {
					break
				}
			}
			wireID := r.next
			r.pending[wireID] = request{c, msg["id"], method}
			msg["id"], _ = json.Marshal(wireID)
			raw, _ = json.Marshal(msg)
			r.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = r.chrome.Write(ctx, raw)
			cancel()
			if err != nil {
				return
			}
		}
	}()
}

func (r *relay) read(ctx context.Context) error {
	for {
		raw, err := r.chrome.Read(ctx)
		if err != nil {
			return err
		}
		r.mu.Lock()
		c := r.current
		var msg map[string]json.RawMessage
		if json.Unmarshal(raw, &msg) == nil && msg["id"] != nil {
			var id uint32
			_ = json.Unmarshal(msg["id"], &id)
			req, known := r.pending[id]
			delete(r.pending, id)
			if !known {
				r.mu.Unlock()
				continue
			}
			active := req.client == c
			select {
			case <-req.client.done:
				active = false
			default:
			}
			if req.method == "Target.attachToTarget" {
				var result struct {
					Session string `json:"sessionId"`
				}
				_ = json.Unmarshal(msg["result"], &result)
				if result.Session != "" {
					if active {
						c.sessions[result.Session] = true
					} else {
						go r.detach([]string{result.Session})
					}
				}
			}
			if !active {
				r.mu.Unlock()
				continue
			}
			msg["id"] = req.id
			raw, _ = json.Marshal(msg)
		}
		r.mu.Unlock()
		// Backpressure preserves bursts; replacement and write deadlines can still close this client.
		if c != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.done:
			case c.frames <- raw:
			}
		}
	}
}

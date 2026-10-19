package timeline

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// backpressure is a socket's unsent bytes from which the timeline holds back what it can send later (a WebSocket's
// bufferedAmount in the previous runtime).
const backpressure = 1 << 20

const writeTimeout = 30 * time.Second

// client is one open socket: messages are queued and written in order by its own writer, so no sender waits on the
// network.
type client struct {
	ws *websocket.Conn

	mu      sync.Mutex
	queue   [][]byte
	queued  int
	wake    chan struct{}
	closing bool
	done    chan struct{}
	once    sync.Once
}

func newClient(ws *websocket.Conn) *client {
	return &client{ws: ws, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

// open is readyState === OPEN: not closing, not gone.
func (c *client) open() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closing
}

// buffered is what is queued and not yet written.
func (c *client) buffered() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queued
}

// canSend: open and not too far behind.
func (c *client) canSend() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closing && c.queued < backpressure
}

// send queues a text message on an open socket, however much is queued (ws.send).
func (c *client) send(raw []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return
	}
	c.queue = append(c.queue, raw)
	c.queued += len(raw)
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *client) writer() {
	for {
		select {
		case <-c.done:
			return
		case <-c.wake:
		}
		for {
			c.mu.Lock()
			if len(c.queue) == 0 {
				c.mu.Unlock()
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
				c.terminate()
				return
			}
		}
	}
}

// close ends the socket with a close frame once what is queued has gone out (ws.close).
func (c *client) close(code websocket.StatusCode, reason string) {
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return
	}
	c.closing = true
	c.mu.Unlock()
	go func() {
		deadline := time.Now().Add(writeTimeout)
		for c.buffered() > 0 && time.Now().Before(deadline) {
			select {
			case <-c.done:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
		_ = c.ws.Close(code, reason)
		c.terminate()
	}()
}

// terminate drops the socket at once.
func (c *client) terminate() {
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	c.once.Do(func() {
		close(c.done)
		_ = c.ws.CloseNow()
	})
}

// ping sends a protocol ping; true when the pong came back in time.
func (c *client) ping(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.ws.Ping(ctx) == nil
}

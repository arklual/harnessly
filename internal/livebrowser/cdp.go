// Package livebrowser is the machine's real browser for the pane: a headless Chrome the runtime starts when a browser tab
// of the pane opens an outside site, driven over the DevTools protocol (CDP). The page's picture goes to the pane as a
// screencast, the person's mouse, keys, wheel and paste go back as input events, and the agent's tools act on the page
// through the same protocol. The proxy (internal/preview) keeps the machine's own servers and stands, where the page is
// rewritten, inspected and edited; this browser takes the sites the proxy cannot carry.
//
// The browser is driven over a pipe (--remote-debugging-pipe), not a DevTools port: no process or page on the machine
// can reach the protocol, and so the profile's cookies, except the runtime through the pipe it holds.
package livebrowser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// cdpReadLimit bounds one CDP message: a screencast frame of a large screen or a page's accessibility tree.
const cdpReadLimit = 64 << 20

// Event is what Chrome tells of its own: a method with parameters, for the session it belongs to ("" for the browser).
type Event struct {
	SessionID string
	Method    string
	Params    json.RawMessage
}

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    any             `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("cdp: %s (%d)", e.Message, e.Code) }

// ErrClosed: the browser's connection is gone.
var ErrClosed = errors.New("the browser's connection is closed")

// conn is one connection to the browser's DevTools pipe, shared by every session (a page is a session, attached flat,
// so its messages carry sessionId). On the pipe, a message is JSON followed by a NUL byte, both ways.
type conn struct {
	w    io.WriteCloser
	r    *bufio.Reader
	rc   io.Closer
	wmu  sync.Mutex
	next atomic.Int64

	mu      sync.Mutex
	waiting map[int64]chan cdpMessage
	// listeners get the events of their session; "" is the browser's own.
	listeners map[string][]chan Event
	closed    chan struct{}
	closeOnce sync.Once
	err       error
}

// newConn speaks the protocol over the browser's pipe: w is what the browser reads (its fd 3), r what it writes (fd 4).
func newConn(w io.WriteCloser, r io.ReadCloser) *conn {
	c := &conn{w: w, r: bufio.NewReaderSize(r, 1<<20), rc: r, waiting: map[int64]chan cdpMessage{},
		listeners: map[string][]chan Event{}, closed: make(chan struct{})}
	go c.read()
	return c
}

// readMessage takes one NUL-terminated message; one past cdpReadLimit ends the connection.
func (c *conn) readMessage() ([]byte, error) {
	var data []byte
	for {
		part, err := c.r.ReadSlice(0)
		data = append(data, part...)
		if err == nil {
			return data[:len(data)-1], nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
		if len(data) > cdpReadLimit {
			return nil, errors.New("a message of the browser is too large")
		}
	}
}

func (c *conn) read() {
	for {
		data, err := c.readMessage()
		if err != nil {
			c.shutdown(err)
			return
		}
		var m cdpMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.waiting[m.ID]
			delete(c.waiting, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		if m.Method == "" {
			continue
		}
		params, _ := m.Params.(json.RawMessage)
		if m.Params != nil && params == nil {
			params, _ = json.Marshal(m.Params)
		}
		ev := Event{SessionID: m.SessionID, Method: m.Method, Params: params}
		c.mu.Lock()
		for _, l := range c.listeners[m.SessionID] {
			// A listener that keeps up hears everything; a full one loses the event (a screencast frame is acked by
			// its handler, so a lost frame only delays the next).
			select {
			case l <- ev:
			default:
			}
		}
		c.mu.Unlock()
	}
}

func (c *conn) shutdown(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		for id, ch := range c.waiting {
			delete(c.waiting, id)
			ch <- cdpMessage{Error: &cdpError{Message: "connection closed"}}
		}
		c.mu.Unlock()
		close(c.closed)
		_ = c.w.Close()
		_ = c.rc.Close()
	})
}

// Close ends the connection; every waiting call fails.
func (c *conn) Close() { c.shutdown(ErrClosed) }

func (c *conn) write(raw []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.w.Write(append(raw, 0)); err != nil {
		return err
	}
	return nil
}

// Call sends a method to a session ("" for the browser) and waits for its result.
func (c *conn) Call(ctx context.Context, sessionID, method string, params any, result any) error {
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	id := c.next.Add(1)
	ch := make(chan cdpMessage, 1)
	c.mu.Lock()
	c.waiting[id] = ch
	c.mu.Unlock()
	raw, err := json.Marshal(cdpMessage{ID: id, SessionID: sessionID, Method: method, Params: params})
	if err != nil {
		return err
	}
	if err := c.write(raw); err != nil {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return ctx.Err()
	case <-c.closed:
		return ErrClosed
	}
}

// Listen gives a channel of the session's events; stop removes it.
func (c *conn) Listen(sessionID string, size int) (events <-chan Event, stop func()) {
	ch := make(chan Event, size)
	c.mu.Lock()
	c.listeners[sessionID] = append(c.listeners[sessionID], ch)
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		ls := c.listeners[sessionID]
		for i, l := range ls {
			if l == ch {
				c.listeners[sessionID] = append(ls[:i:i], ls[i+1:]...)
				break
			}
		}
		if len(c.listeners[sessionID]) == 0 {
			delete(c.listeners, sessionID)
		}
	}
}

// Done is closed when the connection ends.
func (c *conn) Done() <-chan struct{} { return c.closed }

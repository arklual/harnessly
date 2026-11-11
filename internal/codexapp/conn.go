// Package codexapp talks to a chat's `codex app-server`: the structured channel a message goes to Codex through
// instead of being typed in its terminal. The server speaks JSON-RPC, one message per line; a message is a record
// with the client's id, and its delivery, the end of the turn and the approvals Codex asks for come as messages of
// the protocol, not as text on a screen. Codex marks the protocol experimental, so every failure here is one the
// caller can answer by typing in the terminal as before.
package codexapp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/ly-solutions/harnessly/internal/safego"
)

// ErrClosed: the connection to the app-server has ended.
var ErrClosed = errors.New("codex app-server connection ended")

// RPCError is the server's refusal of a request: it read the request and said no.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return "codex app-server: " + e.Message }

// frame is a line of the protocol: a request (id and method), a notification (method) or an answer (id).
type frame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// handler takes what the server says unasked: a notification (id nil) or a request of its own, which is answered with
// conn.reply.
type handler func(id json.RawMessage, method string, params json.RawMessage)

// conn is one connection: requests wait for their answers, everything else goes to the handler in the order it came.
type conn struct {
	rw      io.ReadWriteCloser
	handler handler

	wmu sync.Mutex

	mu      sync.Mutex
	next    int
	waiting map[string]chan frame
	closed  bool

	done chan struct{}
}

func newConn(rw io.ReadWriteCloser, h handler) *conn {
	c := &conn{rw: rw, handler: h, waiting: map[string]chan frame{}, done: make(chan struct{})}
	safego.Go("codex app-server reader", c.read)
	return c
}

func (c *conn) read() {
	defer c.close()
	sc := bufio.NewScanner(c.rw)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		var f frame
		// A line that is no message (a banner) is skipped.
		if json.Unmarshal(sc.Bytes(), &f) != nil || f.ID == nil && f.Method == "" {
			continue
		}
		if f.Method != "" {
			if c.handler != nil {
				c.handler(f.ID, f.Method, f.Params)
			}
			continue
		}
		c.mu.Lock()
		ch := c.waiting[string(f.ID)]
		delete(c.waiting, string(f.ID))
		c.mu.Unlock()
		if ch != nil {
			ch <- f
		}
	}
}

// close ends the connection once; every request still waiting gets ErrClosed.
func (c *conn) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.rw.Close()
	close(c.done)
}

func (c *conn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.rw.Write(append(b, '\n'))
	return err
}

// errNotWritten wraps a failure before the request left: the server has not seen it.
type errNotWritten struct{ err error }

func (e errNotWritten) Error() string {
	return "codex app-server: request not written: " + e.err.Error()
}
func (e errNotWritten) Unwrap() error { return e.err }

// call sends a request and waits for its answer. An errNotWritten or an *RPCError says the request did nothing; any
// other error leaves that unknown (the request was written and the answer never came).
func (c *conn) call(ctx context.Context, method string, params, out any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errNotWritten{ErrClosed}
	}
	c.next++
	id := c.next
	key := fmt.Sprint(id)
	ch := make(chan frame, 1)
	c.waiting[key] = ch
	c.mu.Unlock()
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := c.write(req); err != nil {
		c.mu.Lock()
		delete(c.waiting, key)
		c.mu.Unlock()
		return errNotWritten{err}
	}
	select {
	case f := <-ch:
		if f.Error != nil {
			return f.Error
		}
		if out != nil && len(f.Result) > 0 {
			return json.Unmarshal(f.Result, out)
		}
		return nil
	case <-c.done:
		return ErrClosed
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.waiting, key)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// notify writes a notification: a message nobody answers.
func (c *conn) notify(method string) error { return c.write(map[string]any{"method": method}) }

// reply answers a request of the server's.
func (c *conn) reply(id json.RawMessage, result any) error {
	return c.write(map[string]any{"id": id, "result": result})
}

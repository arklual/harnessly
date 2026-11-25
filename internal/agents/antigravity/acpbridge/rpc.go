package acpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// rpcError is an error the agent answered a request with.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("acp error %d: %s", e.Code, e.Message) }

// errClosed: the agent's streams ended.
var errClosed = errors.New("acp: the agent's connection is closed")

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcHandler answers what the agent sends: a notification (id nil) gets no answer; a request's answer is the result,
// or the error.
type rpcHandler func(method string, params json.RawMessage) (any, error)

// rpc is one JSON-RPC 2.0 connection over newline-delimited JSON, as ACP's stdio transport speaks it.
type rpc struct {
	w      io.Writer
	handle rpcHandler

	wmu sync.Mutex

	mu      sync.Mutex
	next    int
	waiting map[string]chan rpcMessage
	done    chan struct{}
	err     error
}

func newRPC(r io.Reader, w io.Writer, handle rpcHandler) *rpc {
	c := &rpc{w: w, handle: handle, waiting: map[string]chan rpcMessage{}, done: make(chan struct{})}
	go c.read(r)
	return c
}

func (c *rpc) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		var m rpcMessage
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch {
		case m.Method != "" && len(m.ID) > 0:
			// A request of the agent: answered off the reader, since an answer may wait for the person.
			go c.answer(m)
		case m.Method != "":
			_, _ = c.handle(m.Method, m.Params)
		case len(m.ID) > 0:
			c.mu.Lock()
			ch := c.waiting[string(m.ID)]
			delete(c.waiting, string(m.ID))
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	err := sc.Err()
	if err == nil {
		err = errClosed
	}
	c.mu.Lock()
	c.err = err
	for id, ch := range c.waiting {
		close(ch)
		delete(c.waiting, id)
	}
	c.mu.Unlock()
	close(c.done)
}

func (c *rpc) answer(m rpcMessage) {
	result, err := c.handle(m.Method, m.Params)
	out := rpcMessage{JSONRPC: "2.0", ID: m.ID}
	if err != nil {
		var re *rpcError
		if !errors.As(err, &re) {
			re = &rpcError{Code: -32603, Message: err.Error()}
		}
		out.Error = re
	} else {
		raw, merr := json.Marshal(result)
		if merr != nil {
			out.Error = &rpcError{Code: -32603, Message: merr.Error()}
		} else {
			out.Result = raw
		}
	}
	_ = c.write(out)
}

func (c *rpc) write(m rpcMessage) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(append(raw, '\n'))
	return err
}

// call sends a request and waits for its answer, decoded into out (which may be nil).
func (c *rpc) call(ctx context.Context, method string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return errClosed
	}
	c.next++
	id := json.RawMessage(strconv.Itoa(c.next))
	ch := make(chan rpcMessage, 1)
	c.waiting[string(id)] = ch
	c.mu.Unlock()
	if err := c.write(rpcMessage{JSONRPC: "2.0", ID: id, Method: method, Params: raw}); err != nil {
		c.mu.Lock()
		delete(c.waiting, string(id))
		c.mu.Unlock()
		return err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return errClosed
		}
		if m.Error != nil {
			return m.Error
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.waiting, string(id))
		c.mu.Unlock()
		return ctx.Err()
	}
}

// notify sends a notification.
func (c *rpc) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(rpcMessage{JSONRPC: "2.0", Method: method, Params: raw})
}

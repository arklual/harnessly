// Package cdp carries flattened Chrome DevTools sessions over interchangeable transports.
package cdp

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrClosed indicates that the transport is no longer available.
var ErrClosed = errors.New("CDP connection closed")

// Transport preserves message boundaries. Close must interrupt Read and Write.
type Transport interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Close() error
}

// EventHandler runs on the reader goroutine; it must never wait for a CDP reply.
type EventHandler func(method, session string, params json.RawMessage)

// Conn multiplexes commands and events on a single transport.
type Conn struct {
	transport Transport
	handler   EventHandler
	mu        sync.Mutex
	next      uint64
	pending   map[uint64]chan reply
	done      chan struct{}
	once      sync.Once
}

type reply struct {
	result json.RawMessage
	err    error
}

// New creates a connection; Run must be started before calling commands.
func New(t Transport, handler EventHandler) *Conn {
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	// A fresh client must not mistake a late reply to its predecessor for its own.
	return &Conn{
		transport: t, handler: handler, next: binary.BigEndian.Uint64(seed[:]) & ((1 << 30) - 1),
		pending: make(map[uint64]chan reply), done: make(chan struct{}),
	}
}

// Done closes when the connection stops.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Close interrupts all pending calls and closes the transport.
func (c *Conn) Close() error {
	c.once.Do(func() {
		close(c.done)
		_ = c.transport.Close()
		c.mu.Lock()
		for id, ch := range c.pending {
			ch <- reply{err: ErrClosed}
			delete(c.pending, id)
		}
		c.mu.Unlock()
	})
	return nil
}

// Run reads replies and dispatches events until the connection closes.
func (c *Conn) Run(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	defer c.Close()
	for {
		raw, err := c.transport.Read(ctx)
		if err != nil {
			return err
		}
		var m struct {
			ID      uint64          `json:"id"`
			Method  string          `json:"method"`
			Session string          `json:"sessionId"`
			Params  json.RawMessage `json:"params"`
			Result  json.RawMessage `json:"result"`
			Error   *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				r := reply{result: m.Result}
				if m.Error != nil {
					r.err = errors.New(m.Error.Message)
				}
				ch <- r
			}
		} else if m.Method != "" && c.handler != nil {
			c.handler(m.Method, m.Session, m.Params)
		}
	}
}

// Call waits for a command reply, bounded by the context or DefaultTimeout.
func (c *Conn) Call(ctx context.Context, method string, params any, session string) (json.RawMessage, error) {
	ctx, cancel := WithTimeout(ctx)
	defer cancel()
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, ErrClosed
	default:
	}
	id := c.nextIDLocked()
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	msg := struct {
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
		Session string `json:"sessionId,omitempty"`
	}{id, method, params, session}
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if err = c.write(ctx, raw); err != nil {
		// A peer can answer and disconnect before the writer goroutine returns.
		// Preserve a reply the reader has already delivered, including a CDP error.
		select {
		case r := <-ch:
			return r.result, r.err
		default:
		}
		if !errors.Is(err, ErrClosed) {
			return nil, err
		}
	}
	// Close completes every pending reply channel. Racing its notification against
	// a completed reply would randomly discard successful commands on disconnect.
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}

}

// DefaultTimeout bounds commands whose caller has not supplied a deadline.
const DefaultTimeout = 10 * time.Second

// WithTimeout preserves explicit deadlines and bounds otherwise unbounded work.
func WithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, DefaultTimeout)
}

// Send writes a command without waiting for its reply, for flow-control acknowledgements.
func (c *Conn) Send(ctx context.Context, method string, params any, session string) error {
	ctx, cancel := WithTimeout(ctx)
	defer cancel()
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return ErrClosed
	default:
	}
	id := c.nextIDLocked()
	c.mu.Unlock()
	raw, err := json.Marshal(struct {
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
		Session string `json:"sessionId,omitempty"`
	}{id, method, params, session})
	if err != nil {
		return err
	}
	return c.write(ctx, raw)
}

func (c *Conn) write(ctx context.Context, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	// A canceled selection must not cut a shared stream's record in half. Finish a
	// started write within its original deadline, while releasing the caller immediately.
	writeCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	done := make(chan error, 1)
	go func() { defer cancel(); done <- c.transport.Write(writeCtx, raw) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	}
}

func (c *Conn) nextIDLocked() uint64 {
	for {
		c.next++
		if c.next >= 1<<31 {
			c.next = 1
		}
		if _, used := c.pending[c.next]; !used {
			return c.next
		}
	}
}

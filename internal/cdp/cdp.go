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

var ErrClosed = errors.New("CDP connection closed")

// Transport preserves message boundaries. Close must interrupt Read and Write.
type Transport interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Close() error
}

// EventHandler runs on the reader goroutine; it must never wait for a CDP reply.
type EventHandler func(method, session string, params json.RawMessage)

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

func New(t Transport, handler EventHandler) *Conn {
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	// A fresh client must not mistake a late reply to its predecessor for its own.
	return &Conn{transport: t, handler: handler, next: binary.BigEndian.Uint64(seed[:]) & ((1 << 30) - 1), pending: make(map[uint64]chan reply), done: make(chan struct{})}
}
func (c *Conn) Done() <-chan struct{} { return c.done }
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
func (c *Conn) Call(ctx context.Context, method string, params any, session string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, ErrClosed
	default:
	}
	for {
		c.next++
		if c.next >= 1<<31 {
			c.next = 1
		}
		if _, used := c.pending[c.next]; !used {
			break
		}
	}
	id := c.next
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
	if err = c.transport.Write(ctx, raw); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	case <-c.done:
		return nil, ErrClosed
	}
}

package design

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

// cdpPipe is the DevTools protocol over --remote-debugging-pipe: the browser reads NUL-terminated JSON on fd 3 and
// writes on fd 4. No TCP port exists, so no other local user can attach to the browser. Events carry the flattened
// session id of the target they belong to.

const cdpCommandTimeout = 15 * time.Second

type cdpEvent struct {
	Method    string
	Params    map[string]any
	SessionID string
}

type cdpReply struct {
	result map[string]any
	err    error
}

type cdpPipe struct {
	in io.WriteCloser

	mu        sync.Mutex
	nextID    int
	pending   map[int]pendingCall
	listeners map[int]func(cdpEvent)
	nextL     int
	closed    bool
	done      chan struct{}
}

type pendingCall struct {
	method string
	ch     chan cdpReply
}

func newCDPPipe(in io.WriteCloser, out io.Reader) *cdpPipe {
	p := &cdpPipe{in: in, nextID: 1, pending: map[int]pendingCall{}, listeners: map[int]func(cdpEvent){}, done: make(chan struct{})}
	go p.read(out)
	return p
}

func (p *cdpPipe) read(out io.Reader) {
	r := bufio.NewReaderSize(out, 1<<20)
	for {
		raw, err := r.ReadBytes(0)
		if len(raw) > 0 && raw[len(raw)-1] == 0 {
			raw = raw[:len(raw)-1]
		}
		if len(raw) > 0 && err == nil {
			p.onMessage(raw)
		}
		if err != nil {
			p.close(errors.New("CDP pipe closed"))
			return
		}
	}
}

func (p *cdpPipe) onMessage(raw []byte) {
	var m struct {
		ID        *float64       `json:"id"`
		Method    string         `json:"method"`
		Params    map[string]any `json:"params"`
		Result    map[string]any `json:"result"`
		Error     *struct{ Message string }
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	if m.ID != nil {
		p.mu.Lock()
		call, ok := p.pending[int(*m.ID)]
		delete(p.pending, int(*m.ID))
		p.mu.Unlock()
		if !ok {
			return
		}
		if m.Error != nil {
			call.ch <- cdpReply{err: errors.New(call.method + ": " + m.Error.Message)}
		} else {
			if m.Result == nil {
				m.Result = map[string]any{}
			}
			call.ch <- cdpReply{result: m.Result}
		}
		return
	}
	if m.Method == "" {
		return
	}
	if m.Params == nil {
		m.Params = map[string]any{}
	}
	ev := cdpEvent{Method: m.Method, Params: m.Params, SessionID: m.SessionID}
	p.mu.Lock()
	fns := make([]func(cdpEvent), 0, len(p.listeners))
	for _, fn := range p.listeners {
		fns = append(fns, fn)
	}
	p.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// on hears every event until the returned func is called.
func (p *cdpPipe) on(fn func(cdpEvent)) func() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextL++
	id := p.nextL
	p.listeners[id] = fn
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.listeners, id)
	}
}

func (p *cdpPipe) send(method string, params map[string]any, session string) (map[string]any, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("CDP closed: " + method)
	}
	id := p.nextID
	p.nextID++
	ch := make(chan cdpReply, 1)
	p.pending[id] = pendingCall{method: method, ch: ch}
	if params == nil {
		params = map[string]any{}
	}
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	_, err := p.in.Write(append(b, 0))
	p.mu.Unlock()
	if err != nil {
		p.close(err)
	}
	t := time.NewTimer(cdpCommandTimeout)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.result, r.err
	case <-t.C:
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, errors.New("CDP timeout: " + method)
	}
}

func (p *cdpPipe) close(err error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	pending := p.pending
	p.pending = map[int]pendingCall{}
	p.mu.Unlock()
	for _, c := range pending {
		c.ch <- cdpReply{err: err}
	}
	_ = p.in.Close()
	close(p.done)
}

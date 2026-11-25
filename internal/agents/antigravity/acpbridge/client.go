package acpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrNoChannel: the chat has no bridge to talk to (it was started without one, or the bridge is gone).
var ErrNoChannel = errors.New("the chat has no Antigravity bridge")

// ErrUncertain: the request was written and no answer came; the bridge may have it.
var ErrUncertain = errors.New("the Antigravity bridge did not say whether it took the message")

// Sink is what the runtime hears from its chats' bridges. Every call names the conversation. Approval may wait for
// the person; it returns false when the card could not stand for the ask, and its ctx ends when the ask went another
// way (answered in the terminal, the turn stopped).
type Sink interface {
	State(conv string, st State)
	TurnStarted(conv string)
	TurnEnded(conv, stop, detail string)
	Approval(ctx context.Context, conv string, p Permission) (decision string, ok bool)
	Lost(conv string)
}

// Manager keeps the connections to the bridges of the chats, one socket per conversation in Dir.
type Manager struct {
	Dir  string
	Sink Sink

	mu    sync.Mutex
	conns map[string]*Conn
}

// maxSocketPath keeps a socket's path inside what a unix socket address holds.
const maxSocketPath = 100

// Socket is where the bridge of a conversation listens; "" when the id or the folder gives no usable path.
func (m *Manager) Socket(conv string) string {
	if conv == "" || m.Dir == "" || strings.ContainsAny(conv, "/\\'\" \t\n") || strings.HasPrefix(conv, ".") {
		return ""
	}
	p := filepath.Join(m.Dir, conv+".sock")
	if len(p) > maxSocketPath {
		return ""
	}
	return p
}

func isSocket(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

// conn is the connection of a conversation, made when its bridge's socket is there; nil when it is not.
func (m *Manager) conn(conv string) *Conn {
	socket := m.Socket(conv)
	if socket == "" || !isSocket(socket) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conns == nil {
		m.conns = map[string]*Conn{}
	}
	c := m.conns[conv]
	if c == nil {
		c = newConn(conv, socket, m.Sink, func() {
			m.mu.Lock()
			delete(m.conns, conv)
			m.mu.Unlock()
		})
		m.conns[conv] = c
	}
	return c
}

// Has says whether the conversation runs beside a bridge now.
func (m *Manager) Has(conv string) bool { return m.conn(conv) != nil }

// Watch connects to every bridge whose socket is in Dir, now and every few seconds until ctx ends: a bridge's asks
// reach the person even before anyone writes to the chat, after a restart of the runtime too.
func (m *Manager) Watch(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		entries, _ := os.ReadDir(m.Dir)
		for _, e := range entries {
			if conv, ok := strings.CutSuffix(e.Name(), ".sock"); ok {
				m.conn(conv)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Message is a message for a chat's agent.
type Message struct {
	ID     string
	Text   string
	Images []string
}

// Send hands a message to the conversation's agent. Antigravity takes a message during a turn into that turn.
func (m *Manager) Send(ctx context.Context, conv string, msg Message) error {
	c := m.conn(conv)
	if c == nil {
		return ErrNoChannel
	}
	_, err := c.do(ctx, Request{Op: "prompt", ID: msg.ID, Text: msg.Text, Images: msg.Images})
	return err
}

// Cancel stops the conversation's running turn.
func (m *Manager) Cancel(ctx context.Context, conv string) error {
	c := m.conn(conv)
	if c == nil {
		return ErrNoChannel
	}
	_, err := c.do(ctx, Request{Op: "cancel"})
	return err
}

// Configure changes the model, the effort and the mode ("" leaves one as it is) and says what the session runs on.
func (m *Manager) Configure(ctx context.Context, conv, model, effort, mode string) (State, error) {
	c := m.conn(conv)
	if c == nil {
		return State{}, ErrNoChannel
	}
	r, err := c.do(ctx, Request{Op: "config", Model: model, Effort: effort, Mode: mode})
	if r.State != nil {
		return *r.State, err
	}
	return State{}, err
}

// Conn is the runtime's connection to one bridge. It connects, says hello, and keeps connecting again while the
// socket is there.
type Conn struct {
	conv, socket string
	sink         Sink
	gone         func()

	mu      sync.Mutex
	w       net.Conn
	next    int
	waiting map[int]chan Reply
	ready   chan struct{} // closed once connected and greeted
	asks    map[string]context.CancelFunc
	lost    bool
}

func newConn(conv, socket string, sink Sink, gone func()) *Conn {
	c := &Conn{conv: conv, socket: socket, sink: sink, gone: gone, waiting: map[int]chan Reply{},
		ready: make(chan struct{}), asks: map[string]context.CancelFunc{}}
	go c.loop()
	return c
}

func (c *Conn) loop() {
	defer c.gone()
	backoff := 500 * time.Millisecond
	for {
		if !isSocket(c.socket) {
			return
		}
		nc, err := net.DialTimeout("unix", c.socket, 2*time.Second)
		if err != nil {
			if errors.Is(err, syscall.ECONNREFUSED) {
				// Nobody listens: the bridge ended without taking its socket away.
				_ = os.Remove(c.socket)
				return
			}
			time.Sleep(backoff)
			backoff = min(backoff*2, 5*time.Second)
			continue
		}
		backoff = 500 * time.Millisecond
		c.run(nc)
		if c.sink != nil {
			c.sink.Lost(c.conv)
		}
		time.Sleep(backoff)
	}
}

func (c *Conn) run(nc net.Conn) {
	c.mu.Lock()
	c.w = nc
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.w = nil
		for id, ch := range c.waiting {
			close(ch)
			delete(c.waiting, id)
		}
		for id, cancel := range c.asks {
			cancel()
			delete(c.asks, id)
		}
		c.ready = make(chan struct{})
		c.mu.Unlock()
		_ = nc.Close()
	}()
	hello := make(chan Reply, 1)
	go func() {
		r, err := c.call(context.Background(), Request{Op: "hello"}, false)
		if err == nil {
			hello <- r
		}
		close(hello)
	}()
	sc := bufio.NewScanner(nc)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	greeted := false
	for sc.Scan() {
		line := sc.Bytes()
		var probe struct {
			Req *int   `json:"req"`
			Ev  string `json:"ev"`
		}
		if json.Unmarshal(line, &probe) != nil {
			continue
		}
		if probe.Req != nil {
			var r Reply
			if json.Unmarshal(line, &r) != nil {
				continue
			}
			c.mu.Lock()
			ch := c.waiting[r.Req]
			delete(c.waiting, r.Req)
			c.mu.Unlock()
			if ch != nil {
				ch <- r
			}
			if !greeted {
				if r, ok := <-hello; ok {
					greeted = true
					if c.sink != nil && r.State != nil {
						c.sink.State(c.conv, *r.State)
						if !r.State.Busy {
							// A turn the runtime still takes for running ended while nobody listened (a restart).
							c.sink.TurnEnded(c.conv, "idle", "")
						}
					}
					c.mu.Lock()
					close(c.ready)
					c.mu.Unlock()
				}
			}
			continue
		}
		var e Event
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		c.event(e)
	}
}

func (c *Conn) event(e Event) {
	if c.sink == nil {
		return
	}
	switch e.Ev {
	case "turn-started":
		c.sink.TurnStarted(c.conv)
	case "turn-ended":
		c.sink.TurnEnded(c.conv, e.Stop, e.Error)
	case "state":
		if e.State != nil {
			c.sink.State(c.conv, *e.State)
		}
	case "permission":
		if e.Permission == nil {
			return
		}
		p := *e.Permission
		c.mu.Lock()
		if _, ok := c.asks[p.ID]; ok {
			c.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		c.asks[p.ID] = cancel
		c.mu.Unlock()
		go func() {
			decision, ok := c.sink.Approval(ctx, c.conv, p)
			c.mu.Lock()
			if c.asks[p.ID] != nil {
				delete(c.asks, p.ID)
			}
			c.mu.Unlock()
			cancel()
			if ok {
				dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, _ = c.do(dctx, Request{Op: "permission", Permission: p.ID, Decision: decision})
				dcancel()
			}
		}()
	case "permission-gone":
		if e.Permission == nil {
			return
		}
		c.mu.Lock()
		cancel := c.asks[e.Permission.ID]
		delete(c.asks, e.Permission.ID)
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

// do sends a request once the bridge is greeted and waits for its answer. A refusal comes back as an error with the
// bridge's words; a request written and not answered is ErrUncertain.
func (c *Conn) do(ctx context.Context, r Request) (Reply, error) {
	c.mu.Lock()
	ready := c.ready
	c.mu.Unlock()
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case <-ready:
	case <-wait.Done():
		return Reply{}, ErrNoChannel
	}
	return c.call(ctx, r, true)
}

func (c *Conn) call(ctx context.Context, r Request, uncertain bool) (Reply, error) {
	c.mu.Lock()
	w := c.w
	if w == nil {
		c.mu.Unlock()
		return Reply{}, ErrNoChannel
	}
	c.next++
	r.Req = c.next
	ch := make(chan Reply, 1)
	c.waiting[r.Req] = ch
	c.mu.Unlock()
	raw, err := json.Marshal(r)
	if err != nil {
		return Reply{}, err
	}
	c.mu.Lock()
	_, err = w.Write(append(raw, '\n'))
	c.mu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.waiting, r.Req)
		c.mu.Unlock()
		return Reply{}, ErrNoChannel
	}
	select {
	case rep, ok := <-ch:
		if !ok {
			if uncertain {
				return Reply{}, ErrUncertain
			}
			return Reply{}, ErrNoChannel
		}
		if !rep.OK {
			return rep, errors.New(rep.Error)
		}
		return rep, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.waiting, r.Req)
		c.mu.Unlock()
		if uncertain {
			return Reply{}, ErrUncertain
		}
		return Reply{}, ctx.Err()
	}
}

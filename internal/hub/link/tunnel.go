package link

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/safego"
)

// How much of a machine's answers the hub holds for readers that have not taken them yet. An agent that knows the
// window sends no more than the tunnel has granted it, and is granted more as the reader takes the bytes: a slow
// reader slows its own tunnel down and nothing else. A window starts small, doubles while its reader keeps the queue
// empty and shrinks back while the hub's memory for tunnels is tight. An older agent sends as fast as it reads; its
// tunnel is counted by what it holds and cut once it holds too much.
const (
	tunnelWindowStart = 64 * 1024
	tunnelWindowMax   = 1 << 20
	// DefaultTunnelMemory is the ceiling of all tunnels of the process together unless SetTunnelMemory says otherwise.
	DefaultTunnelMemory = 256 << 20
)

// ErrTunnelMemory: the tunnels of this hub hold all the memory they may, so no new one is opened.
var ErrTunnelMemory = errors.New("the hub's memory for tunnels is full")

// tunnelMemory is the budget every tunnel of the process draws from: a tunnel with a window holds its window, one
// without holds what it has queued.
type tunnelMemory struct {
	limit, used  atomic.Int64
	refused, cut atomic.Int64
}

var tunnelMem = func() *tunnelMemory {
	m := &tunnelMemory{}
	m.limit.Store(DefaultTunnelMemory)
	return m
}()

func (m *tunnelMemory) take(n int64) bool {
	for {
		used := m.used.Load()
		if used+n > m.limit.Load() {
			return false
		}
		if m.used.CompareAndSwap(used, used+n) {
			return true
		}
	}
}

func (m *tunnelMemory) give(n int64) { m.used.Add(-n) }

// tight: little is left, so windows stop growing and give back what they grew by.
func (m *tunnelMemory) tight() bool { return m.used.Load() >= m.limit.Load()/4*3 }

// SetTunnelMemory sets how many bytes the tunnels of this process may hold together. Call it before serving.
func SetTunnelMemory(bytes int64) { tunnelMem.limit.Store(bytes) }

// TunnelMemoryStats is what the tunnels hold: Used of Limit bytes, Refused tunnels not opened because the memory was
// full, and Cut tunnels ended because their reader fell too far behind.
type TunnelMemoryStats struct{ Used, Limit, Refused, Cut int64 }

// TunnelMemory reads the budget of the tunnels.
func TunnelMemory() TunnelMemoryStats {
	m := tunnelMem
	return TunnelMemoryStats{Used: m.used.Load(), Limit: m.limit.Load(), Refused: m.refused.Load(), Cut: m.cut.Load()}
}

// Whether the agent keeps to the window: not known until it answers the opening, then one or the other.
const (
	flowUnknown = iota
	flowOn
	flowOff
)

// Tunnel is a TCP connection from the agent to the runtime on the machine, carried over the link. It is a net.Conn, so
// an HTTP client or reverse proxy runs over it as over a socket, WebSocket upgrades included.
type Tunnel struct {
	l    *Link
	id   [tunnelID]byte
	key  string
	done chan struct{} // closed by Close

	mu     sync.Mutex
	queue  [][]byte
	queued int
	// flow is whether the agent keeps to the window. With it on, window = credit (granted, not sent yet) + queued +
	// unsent (read here, not granted back yet). held is what the tunnel has taken of the hub's memory for tunnels: the
	// window, or without one what is queued.
	flow                         int
	held, window, credit, unsent int64
	mem                          *tunnelMemory
	eof                          bool  // the agent ended the connection, or the link did
	err                          error // what a Read returns once the queue is empty (io.EOF, io.ErrUnexpectedEOF)
	closed                       bool
	notify                       chan struct{}
	readDL                       time.Time
	writeDL                      time.Time
}

// OpenTunnel asks the agent for a connection to the runtime. The runtime's own port is the agent's business; the
// local policy's route filter sits between (docs/hub/architecture.md).
func (l *Link) OpenTunnel(ctx context.Context) (*Tunnel, error) {
	mem := tunnelMem
	if !mem.take(tunnelWindowStart) {
		mem.refused.Add(1)
		return nil, ErrTunnelMemory
	}
	raw, key := newTunnelID()
	t := &Tunnel{l: l, id: raw, key: key, done: make(chan struct{}), notify: make(chan struct{}, 1), held: tunnelWindowStart, mem: mem}
	l.mu.Lock()
	if l.Closed() {
		l.mu.Unlock()
		mem.give(tunnelWindowStart)
		return nil, ErrClosed
	}
	// Registered before asking: the agent starts sending the moment it answers.
	l.tunnels[key] = t
	l.mu.Unlock()
	// window is the offer: an agent that keeps to it says so in its answer, an older one ignores the field.
	answer, err := l.Call(ctx, "tunnel.open", map[string]any{"spec": map[string]any{
		"tunnelId": key, "target": "runtime", "window": tunnelWindowStart,
	}})
	if err != nil {
		t.Close()
		return nil, err
	}
	var opened struct {
		Flow bool `json:"flow"`
	}
	_ = json.Unmarshal(answer, &opened)
	t.opened(opened.Flow)
	return t, nil
}

// opened settles how the tunnel is counted once the agent answered. Its first bytes may be here already: nobody has
// read any yet.
func (t *Tunnel) opened(flow bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.eof {
		return
	}
	if !flow {
		t.unwindow()
		return
	}
	if int64(t.queued) > tunnelWindowStart {
		t.cut()
		t.sendClose()
		return
	}
	t.flow, t.window = flowOn, tunnelWindowStart
	t.credit = t.window - int64(t.queued)
}

// unwindow leaves the tunnel holding what it has queued and no more. The caller holds t.mu.
func (t *Tunnel) unwindow() {
	if spare := t.held - int64(t.queued); spare > 0 {
		t.mem.give(spare)
		t.held -= spare
	}
	t.flow = flowOff
}

// cut ends the tunnel for its reader and drops what it held. The caller holds t.mu, and tells the agent after.
func (t *Tunnel) cut() {
	t.queue, t.queued = nil, 0
	t.eof, t.err = true, io.ErrUnexpectedEOF
	t.mem.give(t.held)
	t.held, t.flow = 0, flowOff
	t.mem.cut.Add(1)
}

func (t *Tunnel) signal() {
	select {
	case t.notify <- struct{}{}:
	default:
	}
}

// push adds bytes the agent sent. An agent that sends past its window loses the tunnel, and so does a reader that
// falls too far behind an agent without one: the tunnel, not the link.
func (t *Tunnel) push(b []byte) {
	if len(b) == 0 {
		return
	}
	n := int64(len(b))
	t.mu.Lock()
	if t.closed || t.eof {
		t.mu.Unlock()
		return
	}
	var fits bool
	if t.flow == flowOn {
		if fits = n <= t.credit; fits {
			t.credit -= n
		}
	} else if fits = t.queued+len(b) <= maxTunnelQueue; fits {
		if more := int64(t.queued) + n - t.held; more > 0 {
			if fits = t.mem.take(more); fits {
				t.held += more
			}
		}
	}
	if !fits {
		t.cut()
		t.mu.Unlock()
		t.signal()
		t.sendClose()
		return
	}
	t.queue = append(t.queue, b)
	t.queued += len(b)
	t.mu.Unlock()
	t.signal()
}

// remoteClosed: the agent ended the connection. What was sent before it is still read.
func (t *Tunnel) remoteClosed() {
	t.mu.Lock()
	if !t.eof {
		t.eof, t.err = true, io.EOF
		t.unwindow()
	}
	t.mu.Unlock()
	t.signal()
}

// linkGone: the link ended under the tunnel.
func (t *Tunnel) linkGone() {
	t.mu.Lock()
	if !t.eof {
		t.eof, t.err = true, io.ErrUnexpectedEOF
		t.unwindow()
	}
	t.mu.Unlock()
	t.signal()
}

func (t *Tunnel) Read(p []byte) (int, error) {
	for {
		t.mu.Lock()
		switch {
		case t.closed:
			t.mu.Unlock()
			return 0, net.ErrClosed
		case len(p) == 0:
			t.mu.Unlock()
			return 0, nil
		case len(t.queue) > 0:
			n := copy(p, t.queue[0])
			t.queue[0] = t.queue[0][n:]
			if len(t.queue[0]) == 0 {
				t.queue[0] = nil
				t.queue = t.queue[1:]
			}
			t.queued -= n
			grant := t.taken(int64(n))
			t.mu.Unlock()
			if grant > 0 {
				t.sendWindow(grant)
			}
			return n, nil
		case t.eof:
			err := t.err
			t.mu.Unlock()
			return 0, err
		}
		dl := t.readDL
		t.mu.Unlock()
		var expired <-chan time.Time
		var timer *time.Timer
		if !dl.IsZero() {
			d := time.Until(dl)
			if d <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(d)
			expired = timer.C
		}
		var stop error
		select {
		case <-t.notify:
		case <-expired:
			stop = os.ErrDeadlineExceeded
		case <-t.done:
			stop = net.ErrClosed
		}
		if timer != nil {
			timer.Stop()
		}
		if stop != nil {
			return 0, stop
		}
	}
}

// taken counts n bytes the reader took and says how many more the agent may now send (0: nothing to tell it yet).
// The caller holds t.mu.
func (t *Tunnel) taken(n int64) (grant int64) {
	if t.flow != flowOn {
		t.mem.give(n)
		t.held -= n
		return 0
	}
	t.unsent += n
	if t.unsent < t.window/2 {
		return 0
	}
	tight := t.mem.tight()
	switch {
	case tight && t.window > tunnelWindowStart:
		back := min(t.unsent, t.window-tunnelWindowStart)
		t.window, t.held, t.unsent = t.window-back, t.held-back, t.unsent-back
		t.mem.give(back)
	case !tight && t.queued == 0 && t.window < tunnelWindowMax && t.mem.take(t.window):
		// The reader keeps up with the machine: a wider window costs it fewer round trips.
		t.unsent, t.held, t.window = t.unsent+t.window, t.held+t.window, t.window*2
	}
	grant, t.unsent = t.unsent, 0
	t.credit += grant
	return grant
}

// sendWindow tells the agent it may send n more bytes. It does not wait for a busy link: grants add up, so their
// order does not matter.
func (t *Tunnel) sendWindow(n int64) {
	frame := make([]byte, 1+tunnelID+4)
	frame[0] = frameWindow
	copy(frame[1:], t.id[:])
	binary.BigEndian.PutUint32(frame[1+tunnelID:], uint32(n))
	select {
	case t.l.data <- frame:
		return
	case <-t.l.done:
		return
	default:
	}
	safego.Go("link sendWindow", func() { _ = t.l.sendBinary(frame, t.done, nil) })
}

func (t *Tunnel) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		t.mu.Lock()
		closed, dl := t.closed, t.writeDL
		t.mu.Unlock()
		if closed {
			return written, net.ErrClosed
		}
		var expired <-chan time.Time
		var timer *time.Timer
		if !dl.IsZero() {
			d := time.Until(dl)
			if d <= 0 {
				return written, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(d)
			expired = timer.C
		}
		n := min(len(p), tunnelWriteChunk)
		frame := make([]byte, 1+tunnelID+n)
		frame[0] = frameData
		copy(frame[1:], t.id[:])
		copy(frame[1+tunnelID:], p[:n])
		err := t.l.sendBinary(frame, t.done, expired)
		if timer != nil {
			timer.Stop()
		}
		if err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

// sendClose tells the agent to drop its end, once the bytes written before have gone.
func (t *Tunnel) sendClose() {
	frame := make([]byte, 1+tunnelID)
	frame[0] = frameClose
	copy(frame[1:], t.id[:])
	safego.Go("link sendClose", func() {
		timeout := time.NewTimer(5 * time.Second)
		defer timeout.Stop()
		_ = t.l.sendBinary(frame, nil, timeout.C)
	})
}

// Close ends the tunnel and tells the agent. It does not wait.
func (t *Tunnel) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.queue, t.queued = nil, 0
	t.mem.give(t.held)
	t.held = 0
	t.mu.Unlock()
	close(t.done)
	t.l.dropTunnel(t.key)
	if !t.l.Closed() {
		t.sendClose()
	}
	return nil
}

type tunnelAddr string

func (tunnelAddr) Network() string  { return "harnessly-tunnel" }
func (a tunnelAddr) String() string { return string(a) }

func (t *Tunnel) LocalAddr() net.Addr { return tunnelAddr("hub") }
func (t *Tunnel) RemoteAddr() net.Addr {
	return tunnelAddr(t.l.MachineID + "/" + hex.EncodeToString(t.id[:4]))
}

func (t *Tunnel) SetDeadline(d time.Time) error {
	_ = t.SetReadDeadline(d)
	return t.SetWriteDeadline(d)
}

func (t *Tunnel) SetReadDeadline(d time.Time) error {
	t.mu.Lock()
	t.readDL = d
	t.mu.Unlock()
	t.signal()
	return nil
}

func (t *Tunnel) SetWriteDeadline(d time.Time) error {
	t.mu.Lock()
	t.writeDL = d
	t.mu.Unlock()
	return nil
}

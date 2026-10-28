package link

import (
	"context"
	"encoding/hex"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// Tunnel is a TCP connection from the agent to the runtime on the machine, carried over the link. It is a net.Conn, so
// an HTTP client or reverse proxy runs over it as over a socket, WebSocket upgrades included.
type Tunnel struct {
	l    *Link
	id   [tunnelID]byte
	key  string
	done chan struct{} // closed by Close

	mu      sync.Mutex
	queue   [][]byte
	queued  int
	eof     bool  // the agent ended the connection, or the link did
	err     error // what a Read returns once the queue is empty (io.EOF, io.ErrUnexpectedEOF)
	closed  bool
	notify  chan struct{}
	readDL  time.Time
	writeDL time.Time
}

// OpenTunnel asks the agent for a connection to the runtime. The runtime's own port is the agent's business; the
// local policy's route filter sits between (docs/hub/architecture.md).
func (l *Link) OpenTunnel(ctx context.Context) (*Tunnel, error) {
	raw, key := newTunnelID()
	t := &Tunnel{l: l, id: raw, key: key, done: make(chan struct{}), notify: make(chan struct{}, 1)}
	l.mu.Lock()
	if l.Closed() {
		l.mu.Unlock()
		return nil, ErrClosed
	}
	// Registered before asking: the agent starts sending the moment it answers.
	l.tunnels[key] = t
	l.mu.Unlock()
	_, err := l.Call(ctx, "tunnel.open", map[string]any{"spec": map[string]string{"tunnelId": key, "target": "runtime"}})
	if err != nil {
		t.Close()
		return nil, err
	}
	return t, nil
}

func (t *Tunnel) signal() {
	select {
	case t.notify <- struct{}{}:
	default:
	}
}

// push adds bytes the agent sent. A reader that falls too far behind loses the tunnel, not the link.
func (t *Tunnel) push(b []byte) {
	if len(b) == 0 {
		return
	}
	t.mu.Lock()
	if t.closed || t.eof {
		t.mu.Unlock()
		return
	}
	if t.queued+len(b) > maxTunnelQueue {
		t.queue, t.queued = nil, 0
		t.eof, t.err = true, io.ErrUnexpectedEOF
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
	}
	t.mu.Unlock()
	t.signal()
}

// linkGone: the link ended under the tunnel.
func (t *Tunnel) linkGone() {
	t.mu.Lock()
	if !t.eof {
		t.eof, t.err = true, io.ErrUnexpectedEOF
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
			t.mu.Unlock()
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
	go func() {
		timeout := time.NewTimer(5 * time.Second)
		defer timeout.Stop()
		_ = t.l.sendBinary(frame, nil, timeout.C)
	}()
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

package agent

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// A tunnel is a TCP connection to the runtime (or the disk service next to it) on loopback, its bytes carried over
// the cluster link. The gateway speaks HTTP and WebSocket through it; the runtime checks the session cookie as usual.
// Opening is a JSON request on the link; the bytes travel as binary frames: one byte of kind, sixteen of tunnel id,
// then the payload.
//
// A side that opens a tunnel with a window is sent no more than the window until it grants more (FrameWindow): its
// slow reader holds this tunnel's socket back instead of piling the bytes up at the other end. Without a window the
// socket is read as fast as the link takes it.

const (
	FrameData  byte = 1
	FrameClose byte = 2
	// FrameWindow grants a tunnel opened with a window more bytes to send: the payload is their number, four bytes.
	FrameWindow byte = 3
	// maxWindow bounds what a tunnel is allowed to send ahead, whatever it is granted.
	maxWindow = 64 << 20

	tunnelIDBytes = 16
	maxTunnels    = 512
	openTimeout   = 5 * time.Second
	tunnelRead    = 32 * 1024
	// tunnelWrites bounds the frames waiting for a slow local socket before the link itself waits.
	tunnelWrites = 1024
)

var tunnelIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TunnelOpen is the request to open one.
type TunnelOpen struct {
	TunnelID string `json:"tunnelId"`
	Target   string `json:"target"`
	// Window is how many bytes may be sent before the opener grants more; 0: it does not count them.
	Window int `json:"window,omitempty"`
}

// tunnelOpened is the answer to an opening: flow tells the opener its window is kept to.
func tunnelOpened(spec TunnelOpen) map[string]bool {
	if spec.Window > 0 {
		return map[string]bool{"opened": true, "flow": true}
	}
	return map[string]bool{"opened": true}
}

func EncodeFrame(kind byte, tunnelID string, payload []byte) []byte {
	frame := make([]byte, 1+tunnelIDBytes+len(payload))
	frame[0] = kind
	_, _ = hex.Decode(frame[1:1+tunnelIDBytes], []byte(tunnelID))
	copy(frame[1+tunnelIDBytes:], payload)
	return frame
}

func DecodeFrame(frame []byte) (kind byte, tunnelID string, payload []byte, ok bool) {
	if len(frame) < 1+tunnelIDBytes || frame[0] < FrameData || frame[0] > FrameWindow {
		return 0, "", nil, false
	}
	return frame[0], hex.EncodeToString(frame[1 : 1+tunnelIDBytes]), frame[1+tunnelIDBytes:], true
}

type tunnel struct {
	link   *link
	conn   *net.TCPConn
	writes chan []byte // nil payload: the cluster ended the connection
	done   chan struct{}
	once   sync.Once

	// flow: the tunnel was opened with a window. credit is how many bytes may still be sent; more wakes the reader
	// waiting for some.
	flow   bool
	mu     sync.Mutex
	credit int
	more   chan struct{}
}

// grant adds to what the tunnel may send.
func (tn *tunnel) grant(n int) {
	tn.mu.Lock()
	tn.credit = min(tn.credit+n, maxWindow)
	tn.mu.Unlock()
	select {
	case tn.more <- struct{}{}:
	default:
	}
}

// spend waits until the tunnel may send and takes up to limit bytes of it; 0: the tunnel or its link ended.
func (tn *tunnel) spend(limit int) int {
	for {
		tn.mu.Lock()
		n := min(tn.credit, limit)
		tn.credit -= n
		tn.mu.Unlock()
		if n > 0 {
			return n
		}
		select {
		case <-tn.more:
		case <-tn.done:
			return 0
		case <-tn.link.done:
			return 0
		}
	}
}

func (tn *tunnel) close() {
	tn.once.Do(func() {
		close(tn.done)
		tn.conn.Close()
	})
}

type tunnels struct {
	runtimePort, filesPort int
	mu                     sync.Mutex
	open                   map[string]*tunnel
}

func newTunnels(runtimePort, filesPort int) *tunnels {
	return &tunnels{runtimePort: runtimePort, filesPort: filesPort, open: map[string]*tunnel{}}
}

// Open connects the tunnel. The returned start begins carrying the socket's bytes; it is called after the response
// went out, so the cluster learns the tunnel is open before its first bytes arrive.
func (t *tunnels) Open(l *link, spec TunnelOpen) (start func(), err error) {
	return t.OpenAt(l, spec, 0)
}

// OpenAt is Open to the given loopback port instead of the target's (0: the target's).
func (t *tunnels) OpenAt(l *link, spec TunnelOpen, at int) (start func(), err error) {
	if !tunnelIDPattern.MatchString(spec.TunnelID) {
		return nil, errors.New("bad tunnel id")
	}
	t.mu.Lock()
	_, taken := t.open[spec.TunnelID]
	count := len(t.open)
	t.mu.Unlock()
	if taken {
		return nil, errors.New("bad tunnel id")
	}
	if spec.Target != "runtime" && spec.Target != "files" {
		return nil, errors.New("bad tunnel target")
	}
	if count >= maxTunnels {
		return nil, errors.New("too many tunnels")
	}
	port := t.runtimePort
	if spec.Target == "files" {
		port = t.filesPort
	}
	if at != 0 {
		port = at
	}
	if port == 0 {
		return nil, fmt.Errorf("no %s here", spec.Target)
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), openTimeout)
	if err != nil {
		return nil, err
	}
	conn := c.(*net.TCPConn)
	_ = conn.SetNoDelay(true)
	tn := &tunnel{link: l, conn: conn, writes: make(chan []byte, tunnelWrites), done: make(chan struct{})}
	if spec.Window > 0 {
		tn.flow, tn.credit, tn.more = true, min(spec.Window, maxWindow), make(chan struct{}, 1)
	}
	t.mu.Lock()
	if _, raced := t.open[spec.TunnelID]; raced || l.closed() {
		t.mu.Unlock()
		conn.Close()
		return nil, errors.New("bad tunnel id")
	}
	t.open[spec.TunnelID] = tn
	t.mu.Unlock()
	go t.writer(tn)
	return func() { go t.reader(spec.TunnelID, tn) }, nil
}

func (t *tunnels) reader(id string, tn *tunnel) {
	buf := make([]byte, tunnelRead)
	for {
		limit := len(buf)
		if tn.flow {
			if limit = tn.spend(len(buf)); limit == 0 {
				break
			}
		}
		n, err := tn.conn.Read(buf[:limit])
		if tn.flow && n < limit {
			tn.grant(limit - n)
		}
		if n > 0 && !tn.link.sendBinary(EncodeFrame(FrameData, id, buf[:n])) {
			break
		}
		if err != nil {
			break
		}
	}
	tn.close()
	t.mu.Lock()
	mine := t.open[id] == tn
	if mine {
		delete(t.open, id)
	}
	t.mu.Unlock()
	if mine {
		tn.link.sendBinary(EncodeFrame(FrameClose, id, nil))
	}
}

func (t *tunnels) writer(tn *tunnel) {
	for {
		select {
		case <-tn.done:
			return
		case b := <-tn.writes:
			if b == nil {
				_ = tn.conn.CloseWrite()
				return
			}
			if _, err := tn.conn.Write(b); err != nil {
				tn.close()
				return
			}
		}
	}
}

// Frame takes a binary frame from the cluster.
func (t *tunnels) Frame(raw []byte) {
	kind, id, payload, ok := DecodeFrame(raw)
	if !ok {
		return
	}
	t.mu.Lock()
	tn := t.open[id]
	if tn != nil && kind == FrameClose {
		delete(t.open, id)
	}
	t.mu.Unlock()
	if tn == nil {
		return
	}
	if kind == FrameWindow {
		if tn.flow && len(payload) == 4 {
			tn.grant(int(min(binary.BigEndian.Uint32(payload), maxWindow)))
		}
		return
	}
	if kind == FrameData {
		select {
		case tn.writes <- append([]byte(nil), payload...):
		case <-tn.done:
		case <-tn.link.done:
		}
		return
	}
	select {
	case tn.writes <- nil:
	case <-tn.done:
	case <-tn.link.done:
	}
}

// DropLink closes every connection the gone link carried.
func (t *tunnels) DropLink(l *link) int {
	t.mu.Lock()
	var dropped []*tunnel
	for id, tn := range t.open {
		if tn.link == l {
			delete(t.open, id)
			dropped = append(dropped, tn)
		}
	}
	t.mu.Unlock()
	for _, tn := range dropped {
		tn.close()
	}
	return len(dropped)
}

// DropAll closes every connection to the runtime, whichever link carried it: the local policy narrowed, and what was
// open under the old one must not outlive it. Each tunnel's reader then tells its link, as when a socket ends.
func (t *tunnels) DropAll() int {
	t.mu.Lock()
	all := make([]*tunnel, 0, len(t.open))
	for _, tn := range t.open {
		all = append(all, tn)
	}
	t.mu.Unlock()
	for _, tn := range all {
		tn.close()
	}
	return len(all)
}

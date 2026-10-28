package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// MaxPayload bounds one frame: a page's own socket message (a long paste into the terminal) travels whole inside one
// tunnel frame.
const MaxPayload = 32 * 1024 * 1024

const (
	controlQueue = 4096
	// dataQueue × the tunnel read size is how much tunnel data waits for the link before a tunnel stops reading its
	// socket: a big download must not pile up in memory nor starve the statuses and terminals sharing the link.
	dataQueue    = 128
	writeTimeout = 30 * time.Second
)

// link is one cluster session over a WebSocket, whichever side dialled. Control frames (responses, events) go out
// ahead of tunnel data; a link whose control queue overflows is stuck and is closed, the cluster calls again.
type link struct {
	conn    *websocket.Conn
	control chan []byte
	data    chan []byte
	done    chan struct{}
	once    sync.Once

	// hub: the link is a hub's (protocol 2), which gets the events of protocol 2 and no others.
	hub bool
	// events is the set a link.subscribe named; nil means every event.
	events atomic.Pointer[map[string]struct{}]
}

// subscribe limits the link to the named events from now on.
func (l *link) subscribe(names []string) {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	l.events.Store(&set)
}

// wants says the link takes this event.
func (l *link) wants(event string) bool {
	set := l.events.Load()
	if set == nil {
		return true
	}
	_, ok := (*set)[event]
	return ok
}

func newLink(conn *websocket.Conn) *link {
	l := &link{
		conn:    conn,
		control: make(chan []byte, controlQueue),
		data:    make(chan []byte, dataQueue),
		done:    make(chan struct{}),
	}
	go l.writer()
	return l
}

func (l *link) writer() {
	write := func(typ websocket.MessageType, b []byte) bool {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()
		if err := l.conn.Write(ctx, typ, b); err != nil {
			l.close(websocket.StatusInternalError, "write failed")
			return false
		}
		return true
	}
	for {
		select {
		case b := <-l.control:
			if !write(websocket.MessageText, b) {
				return
			}
			continue
		default:
		}
		select {
		case b := <-l.control:
			if !write(websocket.MessageText, b) {
				return
			}
		case b := <-l.data:
			if !write(websocket.MessageBinary, b) {
				return
			}
		case <-l.done:
			return
		}
	}
}

// sendText queues a control frame without waiting.
func (l *link) sendText(b []byte) {
	select {
	case <-l.done:
	case l.control <- b:
	default:
		Log("warn", "cluster link is not taking frames, closing it")
		l.close(websocket.StatusTryAgainLater, "link stuck")
	}
}

// sendBinary queues tunnel data, waiting while the link is behind. False once the link is gone.
func (l *link) sendBinary(b []byte) bool {
	select {
	case <-l.done:
		return false
	case l.data <- b:
		return true
	}
}

func (l *link) close(code websocket.StatusCode, reason string) {
	l.once.Do(func() {
		close(l.done)
		// The close handshake waits for the peer; nobody who closes a link should wait with it.
		go func() { _ = l.conn.Close(code, reason) }()
	})
}

func (l *link) closed() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

// closeAfterFlush closes the link once the frames already queued have gone out (or a second has passed).
func (l *link) closeAfterFlush(code websocket.StatusCode, reason string) {
	deadline := time.Now().Add(time.Second)
	for len(l.control) > 0 && time.Now().Before(deadline) && !l.closed() {
		time.Sleep(10 * time.Millisecond)
	}
	// The last frame may be in the writer's hands; give it a moment.
	time.Sleep(50 * time.Millisecond)
	l.close(code, reason)
}

package agent

import (
	"context"
	"sync"
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

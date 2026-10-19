package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

const (
	relayReadLimit  = 100 * 1024 * 1024
	relayHandshake  = 8 * time.Second
	relayRetry      = 2 * time.Second
	statusResync    = 10 * time.Minute
	statusSendLimit = 10 * time.Second
)

var statusType = regexp.MustCompile(`^(status|session-history|pane-intents):[a-z-]+$`)

// startRelays reads the runtime's status and sync sockets over loopback, as a page would, and relays every message to
// the cluster. relay=1 tells the runtime pages speak through this socket (their dismissals come back), and keeps the
// sync socket out of the count of pages a panel command waits for. A fresh status sync now and then bounds the
// cluster's stream: everything before the previous sync is cut off.
func (a *Agent) startRelays(ctx context.Context) {
	go a.relay(ctx, a.cfg.StatusPath+"?relay=1", "status.message", func(c *websocket.Conn) {
		a.statusMu.Lock()
		a.status = c
		a.statusMu.Unlock()
	})
	go a.relay(ctx, a.cfg.SyncPath+"?relay=1", "sync.message", func(*websocket.Conn) {})
	go every(ctx, statusResync, false, func() {
		_, _ = a.sendStatus(json.RawMessage(`{"type":"status:request-sync"}`))
	})
}

func (a *Agent) relay(ctx context.Context, path, event string, onOpen func(*websocket.Conn)) {
	for ctx.Err() == nil {
		a.relayOnce(ctx, path, event, onOpen)
		onOpen(nil)
		select {
		case <-ctx.Done():
			return
		case <-time.After(relayRetry):
		}
	}
}

func (a *Agent) relayOnce(ctx context.Context, path, event string, onOpen func(*websocket.Conn)) {
	dialCtx, cancel := context.WithTimeout(ctx, relayHandshake)
	defer cancel()
	header := http.Header{}
	header.Set("cookie", a.runtimeCookie())
	conn, _, err := websocket.Dial(dialCtx, "ws://127.0.0.1:"+strconv.Itoa(a.cfg.RuntimePort)+path, &websocket.DialOptions{
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(relayReadLimit)
	onOpen(conn)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if !json.Valid(data) {
			continue
		}
		var probe struct {
			Type any `json:"type"`
		}
		if json.Unmarshal(data, &probe) == nil && probe.Type == "pong" {
			continue
		}
		a.broadcast(evRelay{Event: event, Message: data})
	}
}

// sendStatus hands a page's message (dismiss, ack, sync, history), relayed by the cluster, to the runtime.
func (a *Agent) sendStatus(message json.RawMessage) (any, error) {
	a.statusMu.Lock()
	conn := a.status
	a.statusMu.Unlock()
	if conn == nil {
		return nil, errors.New("the runtime status socket is not open")
	}
	var probe struct {
		Type any `json:"type"`
	}
	if message == nil || json.Unmarshal(message, &probe) != nil {
		return nil, errors.New("not a status message")
	}
	if t, ok := probe.Type.(string); !ok || !statusType.MatchString(t) {
		return nil, errors.New("not a status message")
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusSendLimit)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, message); err != nil {
		return nil, err
	}
	return map[string]bool{"sent": true}, nil
}

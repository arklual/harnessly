// Package link is the hub's end of the agent link: one WebSocket per machine, dialed by the agent. Over it the hub
// asks the agent to do things (Call), hears what the agent reports (events) and reaches the runtime on the machine
// (tunnels, which look like net.Conn so that an ordinary HTTP reverse proxy can run over them).
//
// The frames are protocol 1 of internal/agent as it is: JSON text frames, a request {"op", "id", …} answered by one
// {"id", "ok", "result"|"error"}, events {"event", …} without an id, and binary frames [kind][tunnel id, 16 bytes]
// [payload] for tunnel bytes. Protocol 2 adds operations and events (docs/hub/api.md).
package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	// MaxPayload bounds one frame, as on the agent's side: a long paste into the terminal travels in one frame.
	MaxPayload = 32 * 1024 * 1024

	controlQueue = 4096
	dataQueue    = 128
	writeTimeout = 30 * time.Second

	frameData  byte = 1
	frameClose byte = 2
	tunnelID        = 16
	// tunnelWriteChunk is how much of a write goes into one frame.
	tunnelWriteChunk = 32 * 1024
	// maxTunnelQueue is how much a tunnel holds for a reader that is slow. Past it the tunnel is cut: the link is shared
	// with every other tunnel of the machine, and holding its reading back would hold them all.
	maxTunnelQueue = 16 << 20
)

// How often the hub pings an agent and how long it waits for the answer. Variables so that a test can shorten them.
var (
	pingEvery   = 15 * time.Second
	pingTimeout = 10 * time.Second
)

// ErrClosed is returned by calls on a link that is gone.
var ErrClosed = errors.New("link closed")

// Event is a frame the agent sent without being asked.
type Event struct {
	Name string
	// Raw is the whole frame, for the consumer to read the fields of its event from.
	Raw json.RawMessage
}

type reply struct {
	result json.RawMessage
	err    *hubproto.Error
}

// Link is one connected agent.
type Link struct {
	MachineID   string
	ConnectedAt time.Time
	// AgentVersion and Protocol are what the agent said when it dialed.
	AgentVersion string
	Protocol     int

	conn      *websocket.Conn
	log       *slog.Logger
	control   chan []byte
	data      chan []byte
	done      chan struct{}
	once      sync.Once
	closeCode websocket.StatusCode

	pingEvery, pingTimeout time.Duration

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan reply
	tunnels map[string]*Tunnel

	onEvent chan Event
}

// New wraps an accepted WebSocket. Nothing runs until Serve.
func New(conn *websocket.Conn, machineID string, log *slog.Logger) *Link {
	if log == nil {
		log = slog.Default()
	}
	conn.SetReadLimit(MaxPayload)
	return &Link{
		MachineID: machineID, ConnectedAt: time.Now().UTC(), conn: conn, log: log,
		control: make(chan []byte, controlQueue), data: make(chan []byte, dataQueue), done: make(chan struct{}),
		pending: map[uint64]chan reply{}, tunnels: map[string]*Tunnel{}, onEvent: make(chan Event, 256),
		pingEvery: pingEvery, pingTimeout: pingTimeout,
	}
}

// Done is closed when the link is over.
func (l *Link) Done() <-chan struct{} { return l.done }

// Closed says whether the link is over.
func (l *Link) Closed() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

// CloseCode is the code the hub closed the link with; 0 when the agent or the network ended it.
func (l *Link) CloseCode() websocket.StatusCode {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeCode
}

// Close ends the link with a WebSocket close code (hubproto.CloseRevoked, …). It does not wait for the peer.
func (l *Link) Close(code websocket.StatusCode, reason string) {
	l.once.Do(func() {
		l.mu.Lock()
		l.closeCode = code
		l.mu.Unlock()
		close(l.done)
		go func() { _ = l.conn.Close(code, reason) }()
	})
}

// Serve runs the link: it writes, pings and reads until the link ends, and returns the reason. Events are handed to
// onEvent one by one in the order they came, off the reading goroutine.
func (l *Link) Serve(ctx context.Context, onEvent func(Event)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go l.writer()
	go l.heartbeat(ctx)
	evDone := make(chan struct{})
	go func() {
		defer close(evDone)
		for {
			select {
			case ev := <-l.onEvent:
				if onEvent != nil {
					onEvent(ev)
				}
			case <-l.done:
				return
			}
		}
	}()
	err := l.read(ctx)
	l.Close(websocket.StatusNormalClosure, "")
	l.fail()
	<-evDone
	return err
}

func (l *Link) writer() {
	write := func(typ websocket.MessageType, b []byte) bool {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()
		if err := l.conn.Write(ctx, typ, b); err != nil {
			l.Close(websocket.StatusInternalError, "write failed")
			return false
		}
		return true
	}
	for {
		// Control frames (answers, requests) go ahead of tunnel bytes.
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

// heartbeat pings the agent: a link that does not answer in time is dead, whatever the TCP connection thinks.
func (l *Link) heartbeat(ctx context.Context) {
	t := time.NewTicker(l.pingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.done:
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, l.pingTimeout)
			err := l.conn.Ping(pctx)
			cancel()
			if err != nil && ctx.Err() == nil {
				l.log.Info("agent link does not answer, dropping it", "machine", l.MachineID)
				_ = l.conn.CloseNow()
				l.Close(websocket.StatusGoingAway, "no answer")
				return
			}
		}
	}
}

func (l *Link) read(ctx context.Context) error {
	for {
		typ, data, err := l.conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageBinary {
			l.binary(data)
			continue
		}
		var head struct {
			ID     json.RawMessage `json:"id"`
			OK     bool            `json:"ok"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
			Event  string          `json:"event"`
		}
		if json.Unmarshal(data, &head) != nil {
			continue
		}
		if head.Event != "" && len(head.ID) == 0 {
			select {
			case l.onEvent <- Event{Name: head.Event, Raw: data}:
			default:
				// Nobody is taking events: the link is stuck, and the agent redials into a clean state.
				l.log.Warn("agent events are not being taken, closing the link", "machine", l.MachineID)
				l.Close(websocket.StatusTryAgainLater, "events stuck")
			}
			continue
		}
		id, err := strconv.ParseUint(string(head.ID), 10, 64)
		if err != nil {
			continue
		}
		l.mu.Lock()
		ch := l.pending[id]
		delete(l.pending, id)
		l.mu.Unlock()
		if ch == nil {
			continue
		}
		if head.OK {
			ch <- reply{result: head.Result}
		} else {
			ch <- reply{err: hubproto.ParseError(head.Error)}
		}
	}
}

// fail ends everything waiting on the link.
func (l *Link) fail() {
	l.mu.Lock()
	tunnels := l.tunnels
	l.tunnels = map[string]*Tunnel{}
	l.mu.Unlock()
	for _, t := range tunnels {
		t.linkGone()
	}
}

// sendText queues a control frame without waiting; a link that does not take frames is stuck and is closed.
func (l *Link) sendText(b []byte) bool {
	select {
	case <-l.done:
		return false
	case l.control <- b:
		return true
	default:
		l.log.Warn("agent link is not taking frames, closing it", "machine", l.MachineID)
		l.Close(websocket.StatusTryAgainLater, "link stuck")
		return false
	}
}

// sendBinary queues tunnel bytes, waiting while the link is behind, until cancel fires or the link ends.
func (l *Link) sendBinary(b []byte, cancel <-chan struct{}, deadline <-chan time.Time) error {
	select {
	case <-l.done:
		return ErrClosed
	case l.data <- b:
		return nil
	case <-cancel:
		return net.ErrClosed
	case <-deadline:
		return os.ErrDeadlineExceeded
	}
}

// Call asks the agent to do op and waits for its answer. params is a struct or map that marshals to a JSON object, or
// nil. An agent's refusal is a *hubproto.Error; a link that ends meanwhile is ErrClosed; ctx ends the wait.
func (l *Link) Call(ctx context.Context, op string, params any) (json.RawMessage, error) {
	rest, err := encodeParams(params)
	if err != nil {
		return nil, err
	}
	opJSON, _ := json.Marshal(op)
	ch := make(chan reply, 1)
	l.mu.Lock()
	if l.Closed() {
		l.mu.Unlock()
		return nil, ErrClosed
	}
	l.nextID++
	id := l.nextID
	l.pending[id] = ch
	l.mu.Unlock()
	cleanup := func() {
		l.mu.Lock()
		delete(l.pending, id)
		l.mu.Unlock()
	}
	frame := append([]byte(`{"op":`), opJSON...)
	frame = append(frame, `,"id":`...)
	frame = strconv.AppendUint(frame, id, 10)
	frame = append(frame, rest...)
	if !l.sendText(frame) {
		cleanup()
		return nil, ErrClosed
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.result, nil
	case <-l.done:
		cleanup()
		return nil, ErrClosed
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	}
}

// CallInto is Call that decodes the result into out.
func (l *Link) CallInto(ctx context.Context, op string, params, out any) error {
	raw, err := l.Call(ctx, op, params)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// encodeParams is the rest of a request object after "op" and "id": "}" for no parameters, else ",…}".
func encodeParams(params any) ([]byte, error) {
	if params == nil {
		return []byte("}"), nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if len(b) < 2 || b[0] != '{' {
		return nil, errors.New("link: request parameters must be a JSON object")
	}
	if len(b) == 2 {
		return []byte("}"), nil
	}
	return append([]byte{','}, b[1:]...), nil
}

// binary hands a tunnel frame to its tunnel.
func (l *Link) binary(frame []byte) {
	if len(frame) < 1+tunnelID || (frame[0] != frameData && frame[0] != frameClose) {
		return
	}
	id := hex.EncodeToString(frame[1 : 1+tunnelID])
	l.mu.Lock()
	t := l.tunnels[id]
	l.mu.Unlock()
	if t == nil {
		return
	}
	if frame[0] == frameClose {
		t.remoteClosed()
		return
	}
	t.push(frame[1+tunnelID:])
}

func newTunnelID() (raw [tunnelID]byte, hexID string) {
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return raw, hex.EncodeToString(raw[:])
}

func (l *Link) dropTunnel(id string) {
	l.mu.Lock()
	delete(l.tunnels, id)
	l.mu.Unlock()
}

// Tunnels is how many tunnels are open; for tests and diagnostics.
func (l *Link) Tunnels() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.tunnels)
}

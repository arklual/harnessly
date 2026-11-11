package codexapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/ly-solutions/harnessly/internal/safego"
)

var (
	// ErrNotReady: the app-server answers but has no thread of the chat to send into yet.
	ErrNotReady = errors.New("codex app-server has no thread for the chat yet")
	// ErrUncertain: the message was written and no answer came, and the thread does not show it. It may still be
	// with Codex, so it is neither sent again nor typed: whoever sent it waits for the transcript to say.
	ErrUncertain = errors.New("codex app-server did not say whether it took the message")
)

// Message is one message of the person. ID is the client's id of it: Codex keeps it with the user message, so a
// message sent before is known by it and never starts a second turn.
type Message struct {
	ID     string
	Text   string
	Images []string
	// Steer puts the message into the turn that runs, when one does.
	Steer bool
}

// Approval is what Codex asks the person to approve.
type Approval struct {
	// Command is the shell command, "" for a change of files; Paths are the files a change touches.
	Command string
	Paths   []string
	Cwd     string
	Reason  string
}

// Decision is the answer to an Approval, in the protocol's words.
type Decision string

const (
	Accept  Decision = "accept"
	Decline Decision = "decline" // refused, the turn goes on
	Cancel  Decision = "cancel"  // refused, and the turn is interrupted
)

// Turn statuses of the protocol.
const (
	TurnCompleted   = "completed"
	TurnInterrupted = "interrupted"
	TurnFailed      = "failed"
	turnInProgress  = "inProgress"
)

// Events is what the server says of the chat's thread. The calls come in the server's order, one at a time, and must
// not wait long — except Approval, which runs beside them and may wait for the person: false leaves the request to
// Codex's own dialog, and ctx ends when someone else answered it.
type Events interface {
	TurnStarted(turnID string)
	TurnEnded(turnID, status, detail string)
	Approval(ctx context.Context, a Approval) (Decision, bool)
	// Lost: the connection ended; nothing more comes until the next Send connects again.
	Lost()
}

// Dialer opens a connection to the chat's app-server.
type Dialer func(ctx context.Context) (io.ReadWriteCloser, error)

// Session is one chat's channel to its app-server. It connects when a message is sent and again after a break, and
// each time reads the thread first, so it knows which of its messages Codex already has.
type Session struct {
	dial Dialer
	ev   Events

	// send holds one Send, with its connecting, at a time.
	send sync.Mutex

	mu        sync.Mutex
	c         *conn
	thread    string
	turn      string
	delivered map[string]bool
	uncertain map[string]bool
	files     map[string][]string
	asks      map[string]context.CancelFunc
	closed    bool
}

// NewSession makes a session that is not connected yet. ev may be nil.
func NewSession(dial Dialer, ev Events) *Session {
	return &Session{dial: dial, ev: ev, delivered: map[string]bool{}, uncertain: map[string]bool{},
		files: map[string][]string{}, asks: map[string]context.CancelFunc{}}
}

// Send hands the message to Codex and returns nil once Codex has it: the server answered the request, or the thread
// already holds a message with this id. ErrUncertain is the one error after which the message may be with Codex; after
// any other it is not, and the caller is free to deliver it another way.
func (s *Session) Send(ctx context.Context, m Message) error {
	if m.ID == "" {
		return errors.New("codex app-server: a message needs an id")
	}
	s.send.Lock()
	defer s.send.Unlock()
	if s.has(m.ID) {
		return nil
	}
	c, err := s.connected(ctx)
	if err != nil {
		return err
	}
	if s.has(m.ID) {
		return nil
	}
	if s.unsure(m.ID) {
		return ErrUncertain
	}
	err = s.start(ctx, c, m)
	if err == nil {
		s.mark(m.ID)
		return nil
	}
	var refused *RPCError
	var unwritten errNotWritten
	if errors.As(err, &refused) || errors.As(err, &unwritten) {
		return err
	}
	// Written, and the answer never came: the thread is asked on a fresh connection whether it has the message.
	c.close()
	if _, cerr := s.connected(ctx); cerr == nil && s.has(m.ID) {
		return nil
	}
	s.mu.Lock()
	s.uncertain[m.ID] = true
	s.mu.Unlock()
	return fmt.Errorf("%w: %v", ErrUncertain, err)
}

// Close ends the session's connection; it connects again at the next Send.
func (s *Session) Close() {
	s.mu.Lock()
	c := s.c
	s.closed = true
	s.mu.Unlock()
	if c != nil {
		c.close()
	}
}

func (s *Session) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delivered[id]
}

func (s *Session) unsure(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uncertain[id]
}

func (s *Session) mark(id string) {
	s.mu.Lock()
	s.delivered[id] = true
	delete(s.uncertain, id)
	s.mu.Unlock()
}

type userInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Path string `json:"path,omitempty"`
}

type turnInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	Items []item `json:"items"`
}

type item struct {
	Type     string  `json:"type"`
	ID       string  `json:"id"`
	ClientID *string `json:"clientId"`
	Changes  []struct {
		Path string `json:"path"`
	} `json:"changes"`
}

// start begins a turn with the message, or steers the running one with it.
func (s *Session) start(ctx context.Context, c *conn, m Message) error {
	var input []userInput
	if m.Text != "" {
		input = append(input, userInput{Type: "text", Text: m.Text})
	}
	for _, p := range m.Images {
		input = append(input, userInput{Type: "localImage", Path: p})
	}
	s.mu.Lock()
	thread, turn := s.thread, s.turn
	s.mu.Unlock()
	if m.Steer && turn != "" {
		err := c.call(ctx, "turn/steer", map[string]any{"threadId": thread, "expectedTurnId": turn, "input": input,
			"clientUserMessageId": m.ID}, nil)
		var refused *RPCError
		if !errors.As(err, &refused) {
			return err
		}
		// The turn ended meanwhile: the message starts the next one.
	}
	var out struct {
		Turn turnInfo `json:"turn"`
	}
	err := c.call(ctx, "turn/start", map[string]any{"threadId": thread, "input": input, "clientUserMessageId": m.ID}, &out)
	if err != nil {
		return err
	}
	if out.Turn.Status == turnInProgress {
		s.mu.Lock()
		s.turn = out.Turn.ID
		s.mu.Unlock()
	}
	return nil
}

// connected is the live connection, made and subscribed to the chat's thread when there is none.
func (s *Session) connected(ctx context.Context) (*conn, error) {
	s.mu.Lock()
	c, closed := s.c, s.closed
	s.mu.Unlock()
	if closed {
		return nil, ErrClosed
	}
	if c != nil {
		select {
		case <-c.done:
			if s.dropped(c) && s.ev != nil {
				s.ev.Lost()
			}
		default:
			return c, nil
		}
	}
	rw, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	var nc *conn
	nc = newConn(rw, func(id json.RawMessage, method string, params json.RawMessage) { s.handle(nc, id, method, params) })
	if err := s.subscribe(ctx, nc); err != nil {
		nc.close()
		return nil, err
	}
	s.mu.Lock()
	s.c = nc
	s.mu.Unlock()
	safego.Go("codex app-server watch", func() {
		<-nc.done
		if s.dropped(nc) && s.ev != nil {
			s.ev.Lost()
		}
	})
	return nc, nil
}

// dropped forgets the connection and ends what waited on it; true when it was the session's current one.
func (s *Session) dropped(c *conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != c {
		return false
	}
	s.c, s.turn = nil, ""
	for id, cancel := range s.asks {
		cancel()
		delete(s.asks, id)
	}
	return true
}

type threadInfo struct {
	ID             string     `json:"id"`
	ParentThreadID *string    `json:"parentThreadId"`
	Turns          []turnInfo `json:"turns"`
}

// subscribe greets the server, finds the chat's thread, subscribes to it and reads what it holds: the messages Codex
// already has, and the turn that runs.
func (s *Session) subscribe(ctx context.Context, c *conn) error {
	if err := c.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "harnessly", "title": "Harnessly", "version": "1"}}, nil); err != nil {
		return err
	}
	if err := c.notify("initialized"); err != nil {
		return err
	}
	thread, err := s.findThread(ctx, c)
	if err != nil {
		return err
	}
	if err := c.call(ctx, "thread/resume", map[string]any{"threadId": thread}, nil); err != nil {
		return err
	}
	var read struct {
		Thread threadInfo `json:"thread"`
	}
	if err := c.call(ctx, "thread/read", map[string]any{"threadId": thread, "includeTurns": true}, &read); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thread, s.turn = thread, ""
	for _, t := range read.Thread.Turns {
		for _, it := range t.Items {
			s.noteItem(it)
		}
		if t.Status == turnInProgress {
			s.turn = t.ID
		}
	}
	return nil
}

// findThread is the thread the server runs for the chat: the one it has loaded that is no sub-agent's.
func (s *Session) findThread(ctx context.Context, c *conn) (string, error) {
	var loaded struct {
		Data []string `json:"data"`
	}
	if err := c.call(ctx, "thread/loaded/list", map[string]any{}, &loaded); err != nil {
		return "", err
	}
	s.mu.Lock()
	known := s.thread
	s.mu.Unlock()
	var own []string
	for _, id := range loaded.Data {
		if id == known {
			return id, nil
		}
		if len(loaded.Data) == 1 {
			return id, nil
		}
		var read struct {
			Thread threadInfo `json:"thread"`
		}
		if err := c.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &read); err != nil {
			return "", err
		}
		if read.Thread.ParentThreadID == nil || *read.Thread.ParentThreadID == "" {
			own = append(own, id)
		}
	}
	if len(own) != 1 {
		return "", ErrNotReady
	}
	return own[0], nil
}

// noteItem keeps what an item of the thread tells; the caller holds s.mu.
func (s *Session) noteItem(it item) {
	switch it.Type {
	case "userMessage":
		if it.ClientID != nil && *it.ClientID != "" {
			s.delivered[*it.ClientID] = true
			delete(s.uncertain, *it.ClientID)
		}
	case "fileChange":
		var paths []string
		for _, ch := range it.Changes {
			paths = append(paths, ch.Path)
		}
		s.files[it.ID] = paths
	}
}

// handle takes a notification or a request of the server's, on the connection's reader.
func (s *Session) handle(c *conn, id json.RawMessage, method string, params json.RawMessage) {
	var p struct {
		ThreadID  string          `json:"threadId"`
		Turn      turnInfo        `json:"turn"`
		Item      item            `json:"item"`
		ItemID    string          `json:"itemId"`
		RequestID json.RawMessage `json:"requestId"`
		Command   *string         `json:"command"`
		Cwd       *string         `json:"cwd"`
		Reason    *string         `json:"reason"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	s.mu.Lock()
	thread := s.thread
	s.mu.Unlock()
	if thread != "" && p.ThreadID != "" && p.ThreadID != thread {
		// Another thread of the same server (a sub-agent's): not the chat's.
		return
	}
	switch method {
	case "turn/started":
		s.mu.Lock()
		s.turn = p.Turn.ID
		s.mu.Unlock()
		if s.ev != nil {
			s.ev.TurnStarted(p.Turn.ID)
		}
	case "turn/completed":
		s.mu.Lock()
		if s.turn == p.Turn.ID {
			s.turn = ""
		}
		s.mu.Unlock()
		detail := ""
		if p.Turn.Error != nil {
			detail = p.Turn.Error.Message
		}
		if s.ev != nil {
			s.ev.TurnEnded(p.Turn.ID, p.Turn.Status, detail)
		}
	case "item/started", "item/completed":
		s.mu.Lock()
		s.noteItem(p.Item)
		s.mu.Unlock()
	case "serverRequest/resolved":
		s.mu.Lock()
		cancel := s.asks[string(p.RequestID)]
		delete(s.asks, string(p.RequestID))
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		if id == nil || s.ev == nil {
			return
		}
		a := Approval{Command: deref(p.Command), Cwd: deref(p.Cwd), Reason: deref(p.Reason)}
		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		a.Paths = s.files[p.ItemID]
		s.asks[string(id)] = cancel
		s.mu.Unlock()
		safego.Go("codex app-server approval", func() {
			defer cancel()
			d, ok := s.ev.Approval(ctx, a)
			s.mu.Lock()
			_, open := s.asks[string(id)]
			delete(s.asks, string(id))
			s.mu.Unlock()
			// Answered only while the request still stands: one that was resolved elsewhere takes no second answer.
			if ok && open {
				_ = c.reply(id, map[string]any{"decision": d})
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

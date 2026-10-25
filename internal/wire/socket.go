package wire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// Service is what the protocol asks of the conversations: convo.Service, which imports this package, has exactly
// these methods (see its comments). NotFound and UnknownCursor on Server tell its errors apart.
type Service interface {
	Conversations(ctx context.Context) (ConvsSnapshot, error)
	Conversation(ctx context.Context, id string) (ConvSnapshot, error)
	ConversationAfter(ctx context.Context, id, afterEntryID string) (ConvSnapshot, error)
	Turns(ctx context.Context, id, beforeTurnID string, limit int) (TurnsPage, error)
	TurnEntries(ctx context.Context, id, turnID string) (EntriesPage, error)
	EntryOutput(ctx context.Context, id, entryID string) (OutputPage, error)
	Do(ctx context.Context, device string, cmd Command) CmdResult
}

// RewindSource is the rewinds of the conversations (convo.Runtime).
type RewindSource interface {
	// Plan is what a rewind to the person's message entryID would do.
	Plan(ctx context.Context, conv, entryID string) (RewindPlan, error)
	// Rewound is the turns the conversation's rewind n (an index into its Rewinds) left behind, oldest first.
	Rewound(ctx context.Context, conv string, n int) (TurnsPage, error)
}

// Server serves /api/v2: hello, the socket, the commands, the pages and the uploads, on a Service and a machine
// source. Its fields are set before it serves.
type Server struct {
	Hub     *Hub
	Service Service
	Machine MachineSource
	Ops     *OpLog
	Uploads *Uploads
	// Dirs lists the folders offered for a new project; nil: GET /api/v2/dirs is a 404.
	Dirs func(ctx context.Context) ([]Dir, error)
	// Sessions lists the agent sessions of the machine that no conversation holds, the newest first; nil: GET
	// /api/v2/sessions is a 404.
	Sessions func(ctx context.Context) ([]AgentSession, error)
	// Terminals gives the conversations' terminals; nil: /api/v2/terminal and .../terminals are 404s.
	Terminals TerminalSource
	// Voice recognizes a recording (lang is ru, en or auto) and returns its text; nil: POST /api/v2/voice is a 404.
	// ErrVoiceUnavailable: this machine cannot recognize.
	Voice func(ctx context.Context, audio io.Reader, lang string) (string, error)
	// SideChat answers a question about a conversation with a stream of server-sent frames (delta, done, error) written
	// to w, and the refusals of its own (unsupported, no-session, busy); it returns nil once it has written anything.
	// convo.ErrNotFound: the conversation is not here. nil: POST /api/v2/conversations/{id}/side-chat is a 404.
	SideChat func(w http.ResponseWriter, r *http.Request, conv string) error
	// Rewind says what a rewind to one of the person's messages would do and shows what a rewind left behind; nil:
	// .../rewind and .../rewinds/{n}/turns are 404s.
	Rewind RewindSource
	// HandoffDraft writes the text a handoff or a merge of a conversation carries (an excerpt, a note, a fork's result);
	// ErrDraftUnsupported when there is nothing to make it of. nil: POST …/handoff-draft is a 404.
	HandoffDraft func(ctx context.Context, conv string, req HandoffDraftRequest) (HandoffDraft, error)
	// Tasks is the agent's task list of a conversation; nil: .../tasks is a 404.
	Tasks func(ctx context.Context, conv string) ([]Task, error)
	// Context is how full a conversation's agent window is; nil: .../context is a 404.
	Context func(ctx context.Context, conv string) (ConvContext, error)
	// MessageStatus is the runtime's word on a conversation's messages by their client ids, one per id in order; nil:
	// .../messages is a 404.
	MessageStatus func(ctx context.Context, conv string, ids []string) (MessageStatuses, error)
	// Legacy is the Legacy tab a conversation runs in; nil: .../legacy is a 404.
	Legacy func(ctx context.Context, conv string) (LegacyTab, error)
	// Files reads the folders of the conversations: .../files lists a folder, .../file reads a text file. nil: both are
	// 404s.
	Files FilesSource
	// Shot opens a picture the conversation lists in its snapshot's shots; ErrNoShot for any other id. nil: .../shot is
	// a 404.
	Shot func(ctx context.Context, conv, id string) (ShotFile, error)
	// Designly is the machine's design library; nil: /api/v2/designly/* are 404s.
	Designly DesignlySource
	// MachineTool serves the Machine tool: GET /api/v2/machine/stats and POST .../machine/processes/stop; nil: both are
	// 404s.
	MachineTool MachineToolSource
	// Open is where a chip or a link of a conversation opens in the right pane; ErrNothingToOpen for what opens nothing.
	// nil: .../open is a 404.
	Open func(ctx context.Context, conv, ref string) (OpenTarget, error)
	// OpenAll is Open for many refs at once (GET .../open-refs), one answer per ref, nil for what opens nothing; nil:
	// the refs are asked of Open one by one. Both nil: .../open-refs is a 404.
	OpenAll func(ctx context.Context, conv string, refs []string) ([]*OpenTarget, error)
	// Apps serves the Apps screen: GET /api/v2/apps; nil: it is a 404.
	Apps AppsSource
	// AgentSettings serves the Settings screen: GET and PATCH /api/v2/agent-settings/<provider>; nil: both are 404s.
	AgentSettings AgentSettingsSource
	// Profiles serves the backend profiles: GET /api/v2/providers, PUT and DELETE .../<id>/key, POST .../<id>/test; nil:
	// all are 404s.
	Profiles ProfilesSource
	// RuntimeSettings are the runtime's own settings: GET and PUT /api/v2/runtime-settings; nil: both are 404s.
	RuntimeSettings RuntimeSettingsStore
	// Me is the person of a runtime on its own; nil: /api/v2/me is a 404 (the cluster's gateway answers it itself).
	Me       MeStore
	Version  string
	Features []Feature
	Limits   Limits
	// NotFound and UnknownCursor report whether an error of the Service is convo.ErrNotFound, convo.ErrUnknownCursor.
	NotFound      func(error) bool
	UnknownCursor func(error) bool
}

// The heartbeat (§2): the page pings every 15 s and calls a socket stale after 20 s without a frame; the runtime
// closes one that sent nothing for SocketIdle, and one that did not say hello within HelloWithin.
const (
	PingEvery   = 15 * time.Second
	StaleAfter  = 20 * time.Second
	SocketIdle  = 35 * time.Second
	HelloWithin = 10 * time.Second
)

// socketIdle is SocketIdle; a test makes it shorter.
var socketIdle atomic.Int64

func init() { socketIdle.Store(int64(SocketIdle)) }

// The socket's own limits: subscriptions per socket, the largest frame from the page, how much may wait unsent
// before the socket is closed (the page comes back with its cursor), how long one frame may take to write, and the
// size from which frames are compressed.
const (
	MaxSubs      = 64
	readLimit    = 64 << 10
	maxBacklog   = 8 << 20
	writeTimeout = 10 * time.Second
	compressFrom = 1024
)

// Close codes: a restart (new epoch) and a stop are told apart from a page that was too slow to read.
const (
	closeRestart   = websocket.StatusServiceRestart
	closeGoingAway = websocket.StatusGoingAway
	closeSlow      = websocket.StatusPolicyViolation
)

// client is one open socket.
type client struct {
	ws     *websocket.Conn
	device string
	caps   Caps                      // what the page reads of the optional parts (Cap), from the socket's address
	subs   map[Channel]*subscription // under the hub's lock

	mu     sync.Mutex
	queue  [][]byte
	queued int
	closed bool
	wake   chan struct{}
	done   chan struct{}
	once   sync.Once

	// subs are the subscribe and unsubscribe frames in the order they came: a snapshot can take its time (a busy
	// actor, a loaded disk), and the reader that answers pings never waits for one.
	ops chan func()
	// detached: the hub let the socket go (under the hub's lock); it takes no subscription after.
	detached bool
	// workerDone closes when the worker has returned; nil while no worker was started.
	workerDone chan struct{}
}

func (c *client) enqueue(frames ...[]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enqueueLocked(frames...)
}

func (c *client) enqueueLocked(frames ...[]byte) {
	if c.closed || len(frames) == 0 {
		return
	}
	for _, f := range frames {
		c.queue = append(c.queue, f)
		c.queued += len(f)
	}
	if c.queued > maxBacklog {
		c.closed = true
		go c.kill(closeSlow, "too far behind")
		return
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// deliver is the hub's fan-out: held while the subscription waits for its snapshot, queued after.
func (c *client) deliver(s *subscription, frame []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.hold {
		s.held = append(s.held, frame)
		return
	}
	c.enqueueLocked(frame)
}

func (c *client) send(v any) {
	b, err := Marshal(v)
	if err != nil {
		agent.Log("error", "v2: frame does not encode", agent.F{K: "error", V: err.Error()})
		return
	}
	c.enqueue(b)
}

// kill closes the socket with a close frame; cut drops its connection without one.
func (c *client) kill(code websocket.StatusCode, reason string) {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.done)
		go func() { _ = c.ws.Close(code, reason) }()
	})
}

func (c *client) cut() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.done)
		_ = c.ws.CloseNow()
	})
}

// worker runs the subscription operations one after another.
func (c *client) worker() {
	defer close(c.workerDone)
	for {
		select {
		case <-c.done:
			return
		case f := <-c.ops:
			select {
			case <-c.done: // the socket went while the operation waited its turn
				return
			default:
			}
			f()
		}
	}
}

// later queues a subscription operation behind the earlier ones; a page that sends more than it may is cut off.
func (c *client) later(f func()) {
	select {
	case c.ops <- f:
	default:
		c.kill(closeSlow, "too many frames")
	}
}

func (c *client) writer(ctx context.Context) {
	for {
		select {
		case <-c.done:
			return
		case <-c.wake:
		}
		c.mu.Lock()
		batch := c.queue
		c.queue, c.queued = nil, 0
		c.mu.Unlock()
		for _, f := range batch {
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, f)
			cancel()
			if err != nil {
				c.cut()
				return
			}
		}
	}
}

// Socket serves GET /api/v2/socket: one socket per machine and page, its channels by subscription.
func (s *Server) Socket(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The page's session is checked before (the same as the Legacy sockets'); the cluster's gateway is in front.
		InsecureSkipVerify:   true,
		CompressionMode:      websocket.CompressionContextTakeover,
		CompressionThreshold: compressFrom,
	})
	if err != nil {
		return
	}
	ws.SetReadLimit(readLimit)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	c := &client{ws: ws, caps: CapsOf(r), subs: map[Channel]*subscription{}, wake: make(chan struct{}, 1), done: make(chan struct{}),
		ops: make(chan func(), 4*MaxSubs)}
	go func() {
		<-c.done
		cancel()
	}()
	s.Hub.attach(c)
	defer func() {
		// The socket is closed first, and its worker is waited for: an operation of the queue that ran after the detach
		// would put the socket back on its channels and on the machine's demand for good.
		c.kill(websocket.StatusNormalClosure, "")
		if c.workerDone != nil {
			<-c.workerDone
		}
		s.Hub.detach(c)
	}()
	go c.writer(ctx)

	if !s.hello(ctx, c) {
		return
	}
	c.workerDone = make(chan struct{})
	go c.worker()
	for {
		rctx, rcancel := context.WithTimeout(ctx, time.Duration(socketIdle.Load()))
		typ, b, err := ws.Read(rctx)
		rcancel()
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			c.send(ErrorFrame{Code: FrameErrProto})
			continue
		}
		s.frame(ctx, c, b)
	}
}

// hello waits for the page's hello and answers welcome; false when the socket is done.
func (s *Server) hello(ctx context.Context, c *client) bool {
	rctx, cancel := context.WithTimeout(ctx, HelloWithin)
	defer cancel()
	_, b, err := c.ws.Read(rctx)
	if err != nil {
		return false
	}
	f, err := DecodeClientFrame(b)
	hello, ok := f.(HelloFrame)
	if err != nil || !ok {
		// Written here, not queued: the page reads why before the close.
		b, _ := json.Marshal(ErrorFrame{Code: FrameErrProto})
		wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
		_ = c.ws.Write(wctx, websocket.MessageText, b)
		wcancel()
		c.kill(websocket.StatusProtocolError, "hello first")
		return false
	}
	c.device = hello.Device
	snap, err := s.Machine.Snapshot(ctx, false)
	if err != nil {
		agent.Log("warn", "v2: no machine for welcome", agent.F{K: "error", V: err.Error()})
		c.kill(websocket.StatusInternalError, "no machine")
		return false
	}
	c.send(WelcomeFrame{Proto: Proto, Epoch: s.Hub.Epoch(), ServerTime: time.Now().UnixMilli(), Machine: snap.Machine,
		Features: s.Features, Limits: s.Limits})
	return true
}

// frame answers one frame after hello.
func (s *Server) frame(ctx context.Context, c *client, b []byte) {
	f, err := DecodeClientFrame(b)
	if err != nil {
		var head struct {
			F  FrameKind `json:"f"`
			Ch Channel   `json:"ch"`
		}
		if json.Unmarshal(b, &head) == nil && (head.F == FrameSub || head.F == FrameUnsub) && !head.Ch.Valid() {
			c.send(ErrorFrame{Ch: "", Code: FrameErrBadChannel})
			return
		}
		c.send(ErrorFrame{Code: FrameErrProto})
		return
	}
	switch f := f.(type) {
	case PingFrame:
		c.send(PongFrame(f))
	case UnsubFrame:
		c.later(func() { s.Hub.unsubscribe(c, f.Ch) })
	case SubFrame:
		c.later(func() { s.sub(ctx, c, f) })
	default: // a second hello
		c.send(ErrorFrame{Code: FrameErrProto})
	}
}

// sub subscribes c to a channel: the missed events from the ring when the cursor allows, else a snapshot — "after"
// the entry cursor of a conversation when the transcript has it, else whole. A conversation's replay is followed by a
// snapshot "after" it that adds no turns and no entries: the page learns that it has caught up (a replay of nothing
// is otherwise silent) and gets the state of now with it.
func (s *Server) sub(ctx context.Context, c *client, f SubFrame) {
	s.Hub.unsubscribe(c, f.Ch)
	s.Hub.mu.Lock()
	n := len(c.subs)
	s.Hub.mu.Unlock()
	if n >= MaxSubs {
		c.send(ErrorFrame{Ch: f.Ch, Code: FrameErrTooManySubs})
		return
	}
	sub, seq, replayed := s.Hub.subscribe(c, f.Ch, f.Epoch, f.AfterSeq)
	if sub == nil { // the socket is gone
		return
	}
	load := f.Opts != nil && f.Opts.Load != nil && *f.Opts.Load
	if f.Ch == ChannelMachine {
		s.Hub.machineDemand(sub, true, load)
	}
	if replayed {
		if _, conv := f.Ch.ConvID(); conv {
			s.caughtUp(ctx, sub, f.Ch, seq)
		}
		return
	}
	snap, code := s.snapshot(ctx, f, seq, load)
	if code != "" {
		s.Hub.unsubscribe(c, f.Ch)
		c.send(ErrorFrame{Ch: f.Ch, Code: code})
		return
	}
	b, err := Marshal(snapshotFor(snap, c.caps))
	if err != nil {
		agent.Log("error", "v2: snapshot does not encode", agent.F{K: "ch", V: string(f.Ch)}, agent.F{K: "error", V: err.Error()})
		s.Hub.unsubscribe(c, f.Ch)
		c.send(ErrorFrame{Ch: f.Ch, Code: FrameErrNotFound})
		return
	}
	s.Hub.release(sub, b)
}

// caughtUp ends a conversation's replay with its snapshot "after" the events replayed; when the conversation does not
// answer, the replay stands alone, as before.
func (s *Server) caughtUp(ctx context.Context, sub *subscription, ch Channel, seq int64) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	id, _ := ch.ConvID()
	cv, err := s.Service.Conversation(ctx, id)
	if err != nil {
		s.Hub.release(sub, nil)
		return
	}
	cv.Turns, cv.Entries, cv.BeforeTurnID = []Turn{}, []Entry{}, nil
	cv = FitConv(cv)
	b, err := Marshal(snapshotFor(SnapshotFrame{Ch: ch, Seq: seq, Mode: SnapshotAfter, Conv: &cv}, sub.c.caps))
	if err != nil {
		b = nil
	}
	s.Hub.release(sub, b)
}

func (s *Server) snapshot(ctx context.Context, f SubFrame, seq int64, load bool) (SnapshotFrame, FrameErrorCode) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	snap := SnapshotFrame{Ch: f.Ch, Seq: seq, Mode: SnapshotReplace}
	switch f.Ch {
	case ChannelMachine:
		m, err := s.Machine.Snapshot(ctx, load)
		if err != nil {
			return snap, FrameErrNotFound
		}
		snap.Machine = &m
	case ChannelConvs:
		cs, err := s.Service.Conversations(ctx)
		if err != nil {
			return snap, FrameErrNotFound
		}
		cs = FitConvs(cs)
		snap.Convs = &cs
	default:
		id, _ := f.Ch.ConvID()
		var cv ConvSnapshot
		err := errUnknown
		if f.AfterEntryID != "" {
			cv, err = s.Service.ConversationAfter(ctx, id, f.AfterEntryID)
			if err == nil {
				snap.Mode = SnapshotAfter
			}
		}
		if err != nil && (f.AfterEntryID == "" || s.UnknownCursor(err)) {
			cv, err = s.Service.Conversation(ctx, id)
		}
		if err != nil {
			if !s.NotFound(err) {
				agent.Log("warn", "v2: conversation snapshot failed", agent.F{K: "ch", V: string(f.Ch)}, agent.F{K: "error", V: err.Error()})
			}
			return snap, FrameErrNotFound
		}
		cv = FitConv(cv)
		snap.Conv = &cv
	}
	return snap, ""
}

var errUnknown = errors.New("v2: not asked yet")

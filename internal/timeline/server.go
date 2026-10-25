// Package timeline serves the conversation socket (/api/timeline): which agent session a tab's pane runs, its
// transcript's first screen and what it gains, switches between sessions, the reply on screen not yet written,
// session statistics and the resume of a past session. In production the cluster's timeline proxy is the client: it
// takes the transcript itself from its database and the rest from here.
package timeline

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

const (
	maxWatchers     = 32
	maxConnections  = 32
	maxInitEntries  = 128
	maxWatchRetries = 3
	compressFrom    = 1024
)

// Tmux is what the timeline asks of a tab's pane.
type Tmux interface {
	PanePID(session string) int
	PaneTitle(session string) string
	Capture(session string) (string, bool)
	CheckTerminalProcess(session string) (safe bool, process string)
	SendKeys(session, line string) error
	// SessionCwd is the pane's working folder, "" when unknown.
	SessionCwd(session string) string
	ParseSessionName(session string) (wsID, paneID, tabID string, ok bool)
}

// Keeper is the tab keeper of the running core.
type Keeper interface {
	NotifyLastUserMessage(session, message string)
	MarkAgentLaunch(tabID string, reset bool)
	MarkAgentGone(tabID string, endLaunch bool)
	IsSessionBusy(session string) bool
}

// Layout is the layout store's side of a tab's agent; nil values are written as null.
type Layout interface {
	AgentState(session, provider string, sessionID, jsonlPath, summary any)
	AgentSessionID(session, provider string, sessionID any)
	AgentSummary(session, provider string, summary any)
	LastUserMessage(session, message string)
	// Recover is the session a stored Claude tab stands for (its transcript, its stored id, its history), written back
	// to the tab.
	Recover(session string) (sessionID, jsonlPath string, ok bool)
}

// Provider is one coding agent the timeline follows.
type Provider struct {
	ID          string
	DisplayName string
	// Detect is the session the pane's processes run, given the shell's children.
	Detect func(children []int) (agents.SessionInfo, error)
	// Running: an agent process runs under the shell already, before it recorded a session.
	Running        func(children []int) bool
	ParsePaneTitle func(title string) *string
	// SessionIDFromPath is the session a transcript path names, "" for none.
	SessionIDFromPath func(path string) string
	ValidSessionID    func(id string) bool
	// FindSession is a session's transcript by id, "" when not found (Codex: its dated folders).
	FindSession func(id string) string
	// SessionsDir is the folder whose changes tell a session started or ended (Claude's pid files); "" for none.
	SessionsDir string
}

// Deps is what the timeline runs on.
type Deps struct {
	Tmux  Tmux
	Procs proc.Procs
	// Alive tells whether a process still runs.
	Alive func(pid int) bool
	// Providers by panel type.
	Providers map[string]*Provider
	// Keeper is the running keeper, nil while the core is off.
	Keeper func() Keeper
	Layout Layout
	// ResumeLine is the line that resumes a session in the tab of the tmux session.
	ResumeLine func(provider, sessionID, session string) (string, error)
	// ClaudeProjects holds Claude's transcripts, one folder per working folder; CodexSessions holds Codex's.
	ClaudeProjects string
	CodexSessions  string
	// StatsDir holds the statusline figures of each Claude session.
	StatsDir string
	Codex    transcript.CodexOptions
	Log      func(level, msg string, kv ...any)
	Timing   Timing
}

// Timing are the old runtime's intervals; tests shorten them.
type Timing struct {
	Heartbeat, HeartbeatTimeout, FileDebounce, WatchRetry  time.Duration
	SessionDebounce, PIDPoll, InstallCheck, LiveReplyEvery time.Duration
}

// DefaultTiming are the intervals of the old runtime.
var DefaultTiming = Timing{Heartbeat: 30 * time.Second, HeartbeatTimeout: 90 * time.Second, FileDebounce: 50 * time.Millisecond,
	WatchRetry: time.Second, SessionDebounce: 200 * time.Millisecond, PIDPoll: 10 * time.Second, InstallCheck: time.Minute,
	LiveReplyEvery: time.Second}

// Server holds every timeline socket of the runtime. One lock guards its state; what the layout store and the keeper
// are told goes out in order from one goroutine, as the old runtime awaited each of those writes.
type Server struct {
	d Deps

	mu             sync.Mutex
	conns          map[*client]*conn
	order          []*conn
	files          map[string]*fileWatcher
	sessions       map[string]*sessionWatcher
	pending        map[string]*pendingWatch
	liveStop       chan struct{}
	liveBusy       bool
	effects        chan func()
	effectsStarted sync.Once
	// inlineEffects runs the writes where they are made (tests, whose fakes never block).
	inlineEffects bool
}

func New(d Deps) *Server {
	if d.Timing == (Timing{}) {
		d.Timing = DefaultTiming
	}
	if d.Log == nil {
		d.Log = func(string, string, ...any) {}
	}
	if d.Alive == nil {
		d.Alive = proc.Running
	}
	return &Server{d: d, conns: map[*client]*conn{}, files: map[string]*fileWatcher{}, sessions: map[string]*sessionWatcher{},
		pending: map[string]*pendingWatch{}, effects: make(chan func(), 4096)}
}

// effect queues a write to the layout store or the keeper; they run one at a time in order.
func (s *Server) effect(fn func()) {
	if s.inlineEffects {
		fn()
		return
	}
	s.effectsStarted.Do(func() {
		go func() {
			for f := range s.effects {
				f()
			}
		}()
	})
	s.effects <- fn
}

func (s *Server) keeper() Keeper {
	if s.d.Keeper == nil {
		return nil
	}
	return s.d.Keeper()
}

// conn is one socket's view of a tab.
type conn struct {
	c           *client
	session     string
	panePID     int
	provider    *Provider
	jsonlPath   string
	cleaned     bool
	lastPong    time.Time
	liveSent    string
	stopBeat    chan struct{}
	inbox       chan []byte
	ready       bool
	readyCh     chan struct{}
	handlerDone chan struct{}
}

func marshal(v any) []byte {
	if o, ok := v.(*jsvalue.Object); ok {
		return jsvalue.Stringify(o, "")
	}
	return nodejson.Marshal(v)
}

func msg(pairs ...any) *jsvalue.Object { return jsvalue.NewObject(pairs...) }

// sendJSON sends when the socket can take it now, else drops the message (sendJson).
func sendJSON(c *client, m *jsvalue.Object) {
	if c.canSend() {
		c.send(marshal(m))
	}
}

func emptyInit(sessionID string, starting bool) *jsvalue.Object {
	m := msg("type", "timeline:init", "entries", []any{}, "sessionId", sessionID, "totalEntries", 0.0, "startByteOffset", 0.0, "hasMore", false)
	if starting {
		m.Set("isClaudeStarting", true)
	}
	return m
}

func sessionChanged(id, reason string) *jsvalue.Object {
	return msg("type", "timeline:session-changed", "newSessionId", id, "reason", reason)
}

func timelineError(code, message string) *jsvalue.Object {
	return msg("type", "timeline:error", "code", code, "message", message)
}

// query is URLSearchParams#get: the first value, present or not.
func query(raw, name string) (string, bool) {
	v := jscompat.Query(raw)[name]
	if len(v) == 0 {
		return "", false
	}
	return v[0], true
}

// Count is the number of open timeline sockets.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// ServeHTTP takes an upgraded /api/timeline request (the caller checked the session).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if sameorigin.Refuse(w, r) {
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true,
		CompressionMode: websocket.CompressionContextTakeover, CompressionThreshold: compressFrom})
	if err != nil {
		return
	}
	ws.SetReadLimit(100 << 20)
	c := newClient(ws)
	go c.writer()
	s.mu.Lock()
	full := len(s.conns) >= maxConnections
	s.mu.Unlock()
	if full {
		c.close(1013, "Too many connections")
		s.drain(c)
		return
	}
	session, _ := query(r.URL.RawQuery, "session")
	if session == "" {
		c.close(1008, "Missing session parameter")
		s.drain(c)
		return
	}
	panelType, has := query(r.URL.RawQuery, "panelType")
	if !has {
		panelType = agents.ClaudePanel
	}
	p := s.d.Providers[panelType]
	if p == nil {
		c.close(1008, "Unknown panel type")
		s.drain(c)
		return
	}
	hint, has := query(r.URL.RawQuery, "agentSessionId")
	if !has {
		hint, _ = query(r.URL.RawQuery, "claudeSessionId")
	}
	cn := &conn{c: c, session: session, provider: p, lastPong: time.Now(), stopBeat: make(chan struct{}), inbox: make(chan []byte, 256),
		readyCh: make(chan struct{}), handlerDone: make(chan struct{})}
	go s.setup(cn, hint)
	s.readLoop(cn)
}

// drain reads a refused socket until it closes, so its close handshake completes.
func (s *Server) drain(c *client) {
	for {
		if _, _, err := c.ws.Read(contextBackground()); err != nil {
			c.terminate()
			return
		}
	}
}

// readLoop answers the page's heartbeat at once and hands the rest to the socket's handler, in order.
func (s *Server) readLoop(cn *conn) {
	defer func() {
		cn.c.terminate()
		close(cn.inbox)
		s.mu.Lock()
		s.cleanup(cn)
		s.mu.Unlock()
	}()
	go s.handler(cn)
	for {
		typ, data, err := cn.c.ws.Read(contextBackground())
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		if string(data) == `{"type":"ping"}` {
			if cn.c.open() {
				cn.c.send([]byte(`{"type":"pong"}`))
			}
			continue
		}
		select {
		case cn.inbox <- data:
		default:
		}
	}
}

// handler runs the page's commands once the socket's first look is done; the old runtime lost commands sent before.
func (s *Server) handler(cn *conn) {
	defer close(cn.handlerDone)
	<-cn.readyCh
	for data := range cn.inbox {
		s.onMessage(cn, data)
	}
}

// setup finds what the pane runs and gives the socket its first screen (handleTimelineConnection).
func (s *Server) setup(cn *conn, hint string) {
	defer close(cn.readyCh)
	pid := s.d.Tmux.PanePID(cn.session)
	if pid == 0 {
		sendJSON(cn.c, emptyInit("", false))
		cn.c.close(1000, "Cannot resolve pane pid")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !cn.c.open() {
		return
	}
	cn.panePID = pid
	s.conns[cn.c] = cn
	s.order = append(s.order, cn)
	s.ensureLivePoller()
	go s.heartbeat(cn)
	cn.ready = true

	p := cn.provider
	children := s.d.Procs.ChildrenOf(pid)
	info, err := p.Detect(children)
	if err != nil {
		s.d.Log("warn", "timeline: session detection failed", "session", cn.session, "err", err.Error())
		return
	}
	if cn.cleaned {
		return
	}
	if info.Status == agents.NotInstalled {
		sendJSON(cn.c, timelineError("not-installed", p.DisplayName+" is not installed"))
		sendJSON(cn.c, emptyInit("", false))
		return
	}
	starting := info.Status == agents.NotRunning && hint == "" && p.Running(s.d.Procs.ChildrenOf(pid))
	if info.Status == agents.Running {
		sendJSON(cn.c, sessionChanged(info.SessionID, "session-waiting"))
	} else if starting {
		sendJSON(cn.c, sessionChanged("", "session-waiting"))
	}
	effective := info.SessionID
	if effective == "" {
		effective = hint
	}
	switch {
	case info.JSONLPath != "":
		cn.jsonlPath = info.JSONLPath
		if info.SessionID != "" {
			s.setAgentSessionID(cn.session, p, info.SessionID)
		}
		s.subscribeAndUpdateSummary(cn.c, info.JSONLPath, info.SessionID, cn.session, p)
	case effective != "":
		if info.SessionID != "" {
			s.setAgentSessionID(cn.session, p, info.SessionID)
		}
		if path := s.resolveJSONLPath(cn.session, effective, p); path != "" {
			cn.jsonlPath = path
			s.subscribeAndUpdateSummary(cn.c, path, effective, cn.session, p)
		} else {
			sendJSON(cn.c, emptyInit(effective, false))
		}
	case !starting:
		// No id from the page and no agent: the conversation is still on disk, the transcript the tab kept or the
		// newest session of its history. The timeline shows it whole, with no agent running.
		var id, path string
		ok := false
		if p.ID == agents.Claude {
			id, path, ok = s.d.Layout.Recover(cn.session)
		}
		if cn.cleaned {
			return
		}
		if ok {
			cn.jsonlPath = path
			s.subscribeAndUpdateSummary(cn.c, path, id, cn.session, p)
		} else {
			sendJSON(cn.c, emptyInit("", info.Status == agents.Running))
		}
	}
	if cn.cleaned {
		return
	}
	if s.sessions[cn.session] == nil {
		s.sessions[cn.session] = s.watchSessions(cn.session, pid, p)
	}
	if info.Status == agents.Running && info.JSONLPath == "" && info.SessionID != "" && info.Cwd != "" {
		s.watchForJSONL(cn.session, info.SessionID, info.Cwd, p)
	}
	// The agent may write its pid file between the first look and the watcher's start: a second look covers it.
	if starting {
		re, err := p.Detect(s.d.Procs.ChildrenOf(pid))
		if err != nil || cn.cleaned {
			return
		}
		if re.Status == agents.Running && re.SessionID != "" && cn.jsonlPath == "" {
			s.setAgentSessionID(cn.session, p, re.SessionID)
			if re.JSONLPath != "" {
				cn.jsonlPath = re.JSONLPath
				sendJSON(cn.c, sessionChanged(re.SessionID, "new-session-started"))
				s.subscribeAndUpdateSummary(cn.c, re.JSONLPath, re.SessionID, cn.session, p)
			} else if re.Cwd != "" {
				sendJSON(cn.c, sessionChanged(re.SessionID, "session-waiting"))
				sendJSON(cn.c, emptyInit(re.SessionID, true))
				s.watchForJSONL(cn.session, re.SessionID, re.Cwd, p)
			}
		} else {
			sendJSON(cn.c, emptyInit("", true))
		}
	}
}

func (s *Server) heartbeat(cn *conn) {
	t := time.NewTicker(s.d.Timing.Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-cn.stopBeat:
			return
		case <-cn.c.done:
			return
		case <-t.C:
		}
		s.mu.Lock()
		if time.Since(cn.lastPong) > s.d.Timing.HeartbeatTimeout {
			s.mu.Unlock()
			cn.c.close(1001, "Heartbeat timeout")
			continue
		}
		if !cn.c.open() {
			s.mu.Unlock()
			continue
		}
		go func() {
			if cn.c.ping(s.d.Timing.HeartbeatTimeout) {
				s.mu.Lock()
				cn.lastPong = time.Now()
				s.mu.Unlock()
			}
		}()
		// A ping is invisible to the page; this tells it the socket still carries the timeline.
		cn.c.send([]byte(`{"type":"timeline:heartbeat"}`))
		if fw := s.files[cn.jsonlPath]; cn.jsonlPath != "" && fw != nil {
			if _, behind := fw.behind[cn.c]; behind {
				s.processFileChange(fw)
			}
		}
		s.mu.Unlock()
	}
}

// onMessage handles one page command.
func (s *Server) onMessage(cn *conn, data []byte) {
	v, err := jsvalue.Parse(data)
	if err != nil {
		s.d.Log("error", "timeline: message handler error", "err", err.Error())
		return
	}
	m, ok := v.(*jsvalue.Object)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !cn.ready || cn.cleaned {
		return
	}
	typ, _ := m.Get("type")
	switch typ {
	case "timeline:subscribe":
		raw, _ := m.Get("jsonlPath")
		if !jsvalue.Truthy(raw) {
			return
		}
		path, ok := raw.(string)
		if !ok {
			return
		}
		if !s.allowedPath(path) {
			sendJSON(cn.c, timelineError("forbidden-path", "Not allowed path"))
			return
		}
		if cn.jsonlPath != "" {
			s.unsubscribe(cn.c, cn.jsonlPath)
		}
		cn.jsonlPath = path
		s.subscribeToFile(cn.c, path, "", cn.session, cn.provider)
	case "timeline:unsubscribe":
		if cn.jsonlPath != "" {
			s.unsubscribe(cn.c, cn.jsonlPath)
			cn.jsonlPath = ""
		}
	case "timeline:resume":
		id, _ := m.Get("sessionId")
		tmuxSession, _ := m.Get("tmuxSession")
		if !jsvalue.Truthy(id) || !jsvalue.Truthy(tmuxSession) {
			return
		}
		sid, isStr := id.(string)
		if !isStr || !cn.provider.ValidSessionID(sid) {
			sendJSON(cn.c, msg("type", "timeline:resume-error", "message", "Invalid session ID format"))
			return
		}
		s.resume(cn, sid, jscompat.String(tmuxSession))
	}
}

// allowedPath: a transcript of either agent, by its resolved path.
func (s *Server) allowedPath(p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil || !strings.HasSuffix(abs, ".jsonl") {
		return false
	}
	return s.d.ClaudeProjects != "" && strings.HasPrefix(abs, s.d.ClaudeProjects+string(filepath.Separator)) ||
		s.d.CodexSessions != "" && strings.HasPrefix(abs, s.d.CodexSessions+string(filepath.Separator))
}

func (s *Server) isCodexPath(p string) bool {
	abs, err := filepath.Abs(p)
	return err == nil && s.d.CodexSessions != "" && strings.HasPrefix(abs, s.d.CodexSessions+string(filepath.Separator)) && strings.HasSuffix(abs, ".jsonl")
}

// resolveJSONLPath is a session's transcript: Codex by its id; Claude in the pane's project folder, else wherever it is.
func (s *Server) resolveJSONLPath(session, id string, p *Provider) string {
	if p.ID == agents.Codex {
		return p.FindSession(id)
	}
	if cwd := s.d.Tmux.SessionCwd(session); cwd != "" {
		path := filepath.Join(projectDir(s.d.ClaudeProjects, cwd), id+".jsonl")
		if exists(path) {
			return path
		}
	}
	return p.FindSession(id)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// resume types the provider's resume line into the pane and follows the resumed transcript.
func (s *Server) resume(cn *conn, id, tmuxSession string) {
	c := cn.c
	safe, process := s.d.Tmux.CheckTerminalProcess(tmuxSession)
	if !safe {
		sendJSON(c, msg("type", "timeline:resume-blocked", "reason", "process-running", "processName", process))
		return
	}
	fail := func(err error) {
		sendJSON(c, msg("type", "timeline:resume-error", "message", err.Error()))
	}
	line, err := s.d.ResumeLine(cn.provider.ID, id, tmuxSession)
	if err != nil {
		fail(err)
		return
	}
	if err := s.d.Tmux.SendKeys(tmuxSession, line); err != nil {
		fail(err)
		return
	}
	if _, _, tabID, ok := s.d.Tmux.ParseSessionName(tmuxSession); ok {
		s.effect(func() {
			if k := s.keeper(); k != nil {
				k.MarkAgentLaunch(tabID, false)
			}
		})
	}
	s.setAgentSessionID(cn.session, cn.provider, id)
	path := s.resolveJSONLPath(tmuxSession, id, cn.provider)
	var jsonl any
	if path != "" {
		jsonl = path
	}
	sendJSON(c, msg("type", "timeline:resume-started", "sessionId", id, "jsonlPath", jsonl))
	if path != "" {
		if cn.jsonlPath != "" {
			s.unsubscribe(c, cn.jsonlPath)
		}
		cn.jsonlPath = path
		s.subscribeAndUpdateSummary(c, path, id, cn.session, cn.provider)
		return
	}
	sendJSON(c, emptyInit(id, false))
	if cwd := s.d.Tmux.SessionCwd(tmuxSession); cwd != "" {
		s.watchForJSONL(cn.session, id, cwd, cn.provider)
	}
}

func (s *Server) sessionConns(session string) []*conn {
	var out []*conn
	for _, cn := range s.order {
		if cn.session == session {
			out = append(out, cn)
		}
	}
	return out
}

// cleanup forgets a closed socket; the last one of a tab stops the watchers of its session.
func (s *Server) cleanup(cn *conn) {
	if cn.cleaned {
		return
	}
	cn.cleaned = true
	close(cn.stopBeat)
	if _, ok := s.conns[cn.c]; ok {
		delete(s.conns, cn.c)
		for i, x := range s.order {
			if x == cn {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}
	if cn.jsonlPath != "" {
		s.unsubscribe(cn.c, cn.jsonlPath)
	}
	if len(s.sessionConns(cn.session)) == 0 {
		s.cancelPending(cn.session)
		if sw := s.sessions[cn.session]; sw != nil {
			sw.stop()
			delete(s.sessions, cn.session)
		}
	}
}

// Shutdown closes every socket and stops every watcher.
func (s *Server) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cn := range append([]*conn(nil), s.order...) {
		cn.c.close(1001, "Server shutting down")
		s.cleanup(cn)
	}
	for k, sw := range s.sessions {
		sw.stop()
		delete(s.sessions, k)
	}
	for k := range s.pending {
		s.cancelPending(k)
	}
	for p := range s.files {
		s.removeFileWatcher(p)
	}
}

// BroadcastSessionStats sends a Claude session's statusline figures to the sockets that show its transcript.
func (s *Server) BroadcastSessionStats(stats *jsvalue.Object) {
	id := stats.String("sessionId")
	raw := marshal(msg("type", "timeline:stats-update", "sessionStats", stats))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, fw := range s.files {
		if sessionIDOfPath(fw.path) != id {
			continue
		}
		for _, c := range fw.order {
			if c.canSend() {
				c.send(raw)
			}
		}
	}
}

// CodexSessionInfo is a session a Codex hook named for a tmux session; the session watcher of that tab takes it.
func (s *Server) CodexSessionInfo(session string, info agents.SessionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sw := s.sessions[session]; sw != nil && sw.codexInfo != nil {
		sw.codexInfo(info)
	}
}

// Package terminal serves the terminal socket: a page attaches to a tab's tmux session through a pty and talks a small
// binary protocol. One byte of type, then the payload:
//
//	page → runtime: 0x00 keystrokes, 0x02 resize (uint16 cols, uint16 rows, big endian), 0x03 heartbeat,
//	                0x04 kill the session, 0x05 text from the composer, 0x06 the id of the composer message that the
//	                next 0x05 carries ({"id": "…"})
//	runtime → page: 0x01 output (UTF-8, whole characters), 0x03 heartbeat answer
package terminal

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

const (
	msgStdin      = 0x00
	msgStdout     = 0x01
	msgResize     = 0x02
	msgHeartbeat  = 0x03
	msgKill       = 0x04
	msgWebStdin   = 0x05
	msgPromptMark = 0x06

	heartbeatEvery   = 30 * time.Second
	heartbeatTimeout = 90 * time.Second
	// While tmux reflows a pane to a new size it draws many intermediate frames: output is gathered for a moment and
	// sent in a few batches, so the screen does not flicker.
	throttleWindow = 500 * time.Millisecond
	throttleFlush  = 250 * time.Millisecond
	// A prompt mark names only the composer frame right behind it; a stale one names nothing.
	promptMarkTTL = 5 * time.Second
	// Frames waiting for a slow page: past this the pty is no longer read, and tmux holds the output.
	outQueue   = 64
	readLimit  = 32 * 1024 * 1024
	writeLimit = 30 * time.Second
)

// Bridge is what the terminal tells the rest of the runtime.
type Bridge interface {
	// ClaimPrompt is false when the composer message went into a pane already: it must not be typed again.
	ClaimPrompt(id, session, text string) bool
	// PromptEnter notes that Enter was passed on in the session, for the prompt-submit timing.
	PromptEnter(session string)
	// ReconcileCwd: a tab's session is gone, its saved folder may need fixing.
	ReconcileCwd(session string)
}

type Config struct {
	Tmux   *tmux.Tmux
	Bridge Bridge
	Home   string
	// Env is the environment of the tmux client the pty runs.
	Env            []string
	MaxConnections int
}

// Server holds the open terminals.
type Server struct {
	cfg   Config
	mu    sync.Mutex
	conns []*conn // oldest first
}

func New(cfg Config) *Server {
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = 32
	}
	return &Server{cfg: cfg}
}

type conn struct {
	s        *Server
	ws       *websocket.Conn
	session  string
	clientID string
	cmd      *exec.Cmd
	pty      *os.File

	mu            sync.Mutex
	cols, rows    int
	capturePaused bool
	throttleUntil time.Time
	throttling    bool
	buffered      []byte
	detaching     bool
	mark          *promptMark

	lastHeartbeat atomic.Int64
	out           chan []byte
	web           chan []byte
	done          chan struct{}
	once          sync.Once
}

type promptMark struct {
	id string
	at time.Time
}

func queryInt(r *http.Request, name string) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || n <= 0 || n > 10000 {
		return 0
	}
	return n
}

// Handle upgrades an already authenticated request for `?session=` and serves it until either side closes.
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(readLimit)
	clientID := r.URL.Query().Get("clientId")

	if !s.makeRoom(clientID) {
		agent.Log("warn", "terminal refused: too many connections", agent.F{K: "max", V: s.cfg.MaxConnections})
		_ = ws.Close(websocket.StatusTryAgainLater, "Max connections exceeded")
		return
	}
	cols, rows := queryInt(r, "cols"), queryInt(r, "rows")
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	if session == "" {
		// A socket that names no session gets a new one, as the previous runtime's plain terminal page did.
		session = s.cfg.Tmux.NewSessionName()
		if err := s.cfg.Tmux.CreateSession(session, cols, rows, ""); err != nil {
			agent.Log("error", "terminal: tmux session creation failed", agent.F{K: "error", V: err.Error()})
			_ = ws.Close(websocket.StatusInternalError, "Session create failed")
			return
		}
	} else if !s.cfg.Tmux.HasSession(session) {
		agent.Log("warn", "terminal: session not found", agent.F{K: "session", V: session})
		s.cfg.Bridge.ReconcileCwd(session)
		_ = ws.Close(websocket.StatusInternalError, "Session not found")
		return
	}
	cmd := exec.Command("tmux", "-u", "-L", s.cfg.Tmux.SocketOf(session), "attach-session", "-t", session)
	cmd.Dir = s.cfg.Home
	cmd.Env = s.cfg.Env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		agent.Log("error", "terminal: tmux attach failed", agent.F{K: "session", V: session}, agent.F{K: "error", V: err.Error()})
		_ = ws.Close(websocket.StatusInternalError, "Session attach failed")
		return
	}
	c := &conn{
		s: s, ws: ws, session: session, clientID: clientID, cmd: cmd, pty: f, cols: cols, rows: rows,
		out: make(chan []byte, outQueue), web: make(chan []byte, 64), done: make(chan struct{}),
	}
	c.lastHeartbeat.Store(time.Now().UnixNano())
	s.mu.Lock()
	s.conns = append(s.conns, c)
	s.mu.Unlock()

	go c.writer()
	go c.typer()
	go c.heartbeats()
	c.startThrottle()
	go c.readPty()
	c.readSocket()
}

// makeRoom replaces the page's previous connection and evicts the oldest ones past the limit. False: no room.
func (s *Server) makeRoom(clientID string) bool {
	s.mu.Lock()
	var drop []*conn
	keep := s.conns[:0]
	for _, c := range s.conns {
		if clientID != "" && c.clientID == clientID {
			drop = append(drop, c)
			continue
		}
		keep = append(keep, c)
	}
	s.conns = keep
	for len(s.conns) >= s.cfg.MaxConnections {
		drop = append(drop, s.conns[0])
		s.conns = s.conns[1:]
	}
	room := len(s.conns) < s.cfg.MaxConnections
	s.mu.Unlock()
	for _, c := range drop {
		c.close(websocket.StatusGoingAway, "Replaced by new connection", true)
	}
	return room
}

func (c *conn) send(frame []byte) bool {
	select {
	case <-c.done:
		return false
	case c.out <- frame:
		return true
	}
}

func stdout(data []byte) []byte {
	frame := make([]byte, 1+len(data))
	frame[0] = msgStdout
	copy(frame[1:], data)
	return frame
}

func (c *conn) writer() {
	for {
		select {
		case <-c.done:
			return
		case frame := <-c.out:
			ctx, cancel := context.WithTimeout(context.Background(), writeLimit)
			err := c.ws.Write(ctx, websocket.MessageBinary, frame)
			cancel()
			if err != nil {
				c.close(websocket.StatusGoingAway, "", true)
				return
			}
		}
	}
}

// readPty forwards the tmux client's output in whole UTF-8 characters.
func (c *conn) readPty() {
	buf := make([]byte, 32*1024)
	var carry []byte
	for {
		n, err := c.pty.Read(buf)
		if n > 0 {
			data := append(carry, buf[:n]...)
			cut := completeUTF8(data)
			carry = append([]byte(nil), data[cut:]...)
			if cut > 0 {
				c.deliver(validUTF8(data[:cut]))
			}
		}
		if err != nil {
			// The pty ended on its own: the session exited. A close that came first (the page left, a replacement,
			// shutdown) already ended the connection, and this one is a no-op.
			c.close(websocket.StatusNormalClosure, "Session exited", false)
			return
		}
	}
}

// completeUTF8 is the length of data without a character cut off at its end.
func completeUTF8(data []byte) int {
	for i := 1; i <= 3 && i <= len(data); i++ {
		b := data[len(data)-i]
		if b < 0x80 {
			return len(data)
		}
		if b >= 0xC0 { // a lead byte: is its character whole?
			need := 2
			switch {
			case b >= 0xF0:
				need = 4
			case b >= 0xE0:
				need = 3
			}
			if i < need {
				return len(data) - i
			}
			return len(data)
		}
	}
	return len(data)
}

func validUTF8(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	return []byte(strings.ToValidUTF8(string(b), "�"))
}

func (c *conn) deliver(data []byte) {
	c.mu.Lock()
	if c.capturePaused {
		c.mu.Unlock()
		return
	}
	if time.Now().Before(c.throttleUntil) {
		c.buffered = append(c.buffered, data...)
		c.mu.Unlock()
		return
	}
	// What the last window gathered goes first, so output keeps its order.
	if len(c.buffered) > 0 {
		data = append(c.buffered, data...)
		c.buffered = nil
	}
	c.mu.Unlock()
	c.send(stdout(data))
}

func (c *conn) startThrottle() {
	c.mu.Lock()
	end := time.Now().Add(throttleWindow)
	if !end.After(c.throttleUntil) {
		c.mu.Unlock()
		return
	}
	c.throttleUntil = end
	if c.throttling {
		c.mu.Unlock()
		return
	}
	c.throttling = true
	c.mu.Unlock()
	go func() {
		t := time.NewTicker(throttleFlush)
		defer t.Stop()
		for {
			select {
			case <-c.done:
				return
			case <-t.C:
			}
			c.mu.Lock()
			data := c.buffered
			c.buffered = nil
			over := !time.Now().Before(c.throttleUntil)
			if over {
				c.throttling = false
			}
			c.mu.Unlock()
			if len(data) > 0 {
				c.send(stdout(data))
			}
			if over {
				return
			}
		}
	}()
}

// typer types composer text one message after another: out of copy mode first, so it reaches the program.
func (c *conn) typer() {
	for {
		select {
		case <-c.done:
			return
		case data := <-c.web:
			c.s.cfg.Tmux.ExitCopyMode(c.session)
			if _, err := c.pty.Write(data); err != nil {
				return
			}
			if len(data) > 0 && data[len(data)-1] == '\r' {
				c.s.cfg.Bridge.PromptEnter(c.session)
			}
		}
	}
}

func (c *conn) heartbeats() {
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if time.Since(time.Unix(0, c.lastHeartbeat.Load())) > heartbeatTimeout {
				c.close(websocket.StatusGoingAway, "Heartbeat timeout", true)
				return
			}
		}
	}
}

func (c *conn) readSocket() {
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.close(websocket.StatusNormalClosure, "", true)
			return
		}
		if len(data) == 0 {
			continue
		}
		typ, payload := data[0], data[1:]
		switch typ {
		case msgStdin:
			_, _ = c.pty.Write(payload)
		case msgPromptMark:
			var mark struct {
				ID string `json:"id"`
			}
			c.mu.Lock()
			c.mark = nil
			if json.Unmarshal(payload, &mark) == nil && mark.ID != "" {
				if len(mark.ID) > 64 {
					mark.ID = mark.ID[:64]
				}
				c.mark = &promptMark{id: mark.ID, at: time.Now()}
			}
			c.mu.Unlock()
		case msgWebStdin:
			c.mu.Lock()
			mark := c.mark
			c.mark = nil
			c.mu.Unlock()
			if mark != nil && time.Since(mark.at) < promptMarkTTL && !c.s.cfg.Bridge.ClaimPrompt(mark.id, c.session, string(payload)) {
				continue
			}
			select {
			case c.web <- append([]byte(nil), payload...):
			case <-c.done:
				return
			}
		case msgResize:
			if len(payload) < 4 {
				continue
			}
			cols, rows := int(binary.BigEndian.Uint16(payload)), int(binary.BigEndian.Uint16(payload[2:]))
			if cols == 0 || rows == 0 {
				continue
			}
			c.mu.Lock()
			changed := cols != c.cols || rows != c.rows
			c.cols, c.rows = cols, rows
			paused := c.capturePaused
			c.mu.Unlock()
			if !paused {
				_ = pty.Setsize(c.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
				if changed {
					c.startThrottle()
				}
			}
		case msgHeartbeat:
			c.lastHeartbeat.Store(time.Now().UnixNano())
			c.send([]byte{msgHeartbeat})
		case msgKill:
			go c.s.cfg.Tmux.KillSession(c.session)
		}
	}
}

// close ends the connection once. A detaching close leaves the session; the page is told why only when the session
// itself ended.
func (c *conn) close(code websocket.StatusCode, reason string, detaching bool) {
	c.once.Do(func() {
		c.mu.Lock()
		c.detaching = c.detaching || detaching
		c.mu.Unlock()
		close(c.done)
		c.s.mu.Lock()
		for i, other := range c.s.conns {
			if other == c {
				c.s.conns = append(c.s.conns[:i], c.s.conns[i+1:]...)
				break
			}
		}
		c.s.mu.Unlock()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Signal(syscall.SIGHUP)
		}
		_ = c.pty.Close()
		go func() { _ = c.cmd.Wait() }()
		// The close handshake waits for the page; a page that stopped reading must not hold up whoever closed it
		// (a new connection of the same page waits for its predecessor to be gone).
		if reason != "" {
			go func() { _ = c.ws.Close(code, reason) }()
		} else {
			_ = c.ws.CloseNow()
		}
	})
}

// Shutdown closes every terminal; the sessions stay.
func (s *Server) Shutdown() {
	s.mu.Lock()
	conns := append([]*conn(nil), s.conns...)
	s.mu.Unlock()
	for _, c := range conns {
		c.close(websocket.StatusGoingAway, "Server shutting down", true)
	}
}

func (s *Server) find(session string) *conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		if c.session == session {
			return c
		}
	}
	return nil
}

// Size is the size the page gave the session's terminal.
func (s *Server) Size(session string) (cols, rows int, ok bool) {
	c := s.find(session)
	if c == nil {
		return 0, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cols, c.rows, true
}

// Pause stops the session's output to the page while the pane is captured at another width; false when there is no
// terminal or it is paused already.
func (s *Server) Pause(session string) (cols, rows int, ok bool) {
	c := s.find(session)
	if c == nil {
		return 0, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.capturePaused {
		return 0, 0, false
	}
	c.capturePaused = true
	return c.cols, c.rows, true
}

func (s *Server) Resume(session string) {
	if c := s.find(session); c != nil {
		c.mu.Lock()
		c.capturePaused = false
		c.mu.Unlock()
	}
}

// Resize sets the session's pty size, whatever the page asked.
func (s *Server) Resize(session string, cols, rows int) {
	if c := s.find(session); c != nil && cols > 0 && rows > 0 {
		_ = pty.Setsize(c.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
}

package codexapp

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ErrNoChannel: the chat has no app-server to talk to (it was started without one, or the server is gone).
var ErrNoChannel = errors.New("the chat has no codex app-server")

// maxSocketPath keeps a socket's path inside what a unix socket address holds (108 bytes on Linux, less elsewhere).
const maxSocketPath = 100

// Sink is Events for many chats: every call names its conversation.
type Sink interface {
	TurnStarted(conv, turnID string)
	TurnEnded(conv, turnID, status, detail string)
	Approval(ctx context.Context, conv string, a Approval) (Decision, bool)
	Lost(conv string)
}

// Manager keeps the sessions of the chats that run beside an app-server. A chat has one when the socket its launch
// was given exists: the launch decides, the manager only finds it.
type Manager struct {
	// Dir holds the chats' sockets, one per conversation.
	Dir string
	// Dial connects to a socket.
	Dial func(ctx context.Context, socket string) (io.ReadWriteCloser, error)
	Sink Sink

	mu       sync.Mutex
	sessions map[string]*Session
}

// Socket is where the app-server of a conversation listens; "" when the id or the folder gives no usable path.
func (m *Manager) Socket(conv string) string {
	if conv == "" || m.Dir == "" || strings.ContainsAny(conv, "/\\'\" \t\n") || strings.HasPrefix(conv, ".") {
		return ""
	}
	p := filepath.Join(m.Dir, conv+".sock")
	if len(p) > maxSocketPath {
		return ""
	}
	return p
}

// Send hands the message to the conversation's Codex (Session.Send); ErrNoChannel when the chat has no app-server.
func (m *Manager) Send(ctx context.Context, conv string, msg Message) error {
	socket := m.Socket(conv)
	if st, err := os.Stat(socket); socket == "" || err != nil || st.Mode()&os.ModeSocket == 0 {
		m.Forget(conv)
		return ErrNoChannel
	}
	m.mu.Lock()
	s := m.sessions[conv]
	if s == nil {
		if m.sessions == nil {
			m.sessions = map[string]*Session{}
		}
		var ev Events
		if m.Sink != nil {
			ev = chatEvents{m.Sink, conv}
		}
		s = NewSession(func(ctx context.Context) (io.ReadWriteCloser, error) { return m.Dial(ctx, socket) }, ev)
		m.sessions[conv] = s
	}
	m.mu.Unlock()
	return s.Send(ctx, msg)
}

// Forget ends the session of a conversation that is over or has lost its server.
func (m *Manager) Forget(conv string) {
	m.mu.Lock()
	s := m.sessions[conv]
	delete(m.sessions, conv)
	m.mu.Unlock()
	if s != nil {
		s.Close()
	}
}

type chatEvents struct {
	sink Sink
	conv string
}

func (e chatEvents) TurnStarted(turnID string) { e.sink.TurnStarted(e.conv, turnID) }
func (e chatEvents) TurnEnded(turnID, status, detail string) {
	e.sink.TurnEnded(e.conv, turnID, status, detail)
}
func (e chatEvents) Approval(ctx context.Context, a Approval) (Decision, bool) {
	return e.sink.Approval(ctx, e.conv, a)
}
func (e chatEvents) Lost() { e.sink.Lost(e.conv) }

// ProxyDial connects through `codex app-server proxy`, Codex's own bridge from standard streams to the server's
// socket: the lines of the protocol go over the process's pipes, whatever the socket itself speaks.
func ProxyDial(bin string) func(ctx context.Context, socket string) (io.ReadWriteCloser, error) {
	return func(_ context.Context, socket string) (io.ReadWriteCloser, error) {
		cmd := exec.Command(bin, "app-server", "proxy", "--sock", socket)
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &proxy{cmd: cmd, in: in, out: out}, nil
	}
}

type proxy struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  io.ReadCloser
	once sync.Once
}

func (p *proxy) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *proxy) Write(b []byte) (int, error) { return p.in.Write(b) }
func (p *proxy) Close() error {
	p.once.Do(func() {
		_ = p.in.Close()
		_ = p.cmd.Process.Kill()
		go func() { _ = p.cmd.Wait() }()
	})
	return nil
}

// minVersion is the oldest Codex whose app-server protocol this client was written against.
var minVersion = [3]int{0, 147, 0}

// Supported says whether a Codex of this version (`codex --version`: "codex-cli 0.147.0") has the app-server this
// package talks to. A version that cannot be read is not supported.
func Supported(version string) bool {
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return false
	}
	v, _, _ := strings.Cut(fields[len(fields)-1], "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return false
		}
		if n != minVersion[i] {
			return n > minVersion[i]
		}
	}
	return true
}

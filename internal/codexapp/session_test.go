package codexapp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeServer plays `codex app-server` for one chat: a thread, the turns started in it, and the faults a test asks for.
type fakeServer struct {
	mu sync.Mutex
	// threads are the loaded threads; the first is the chat's, the rest name it as their parent.
	threads []string
	turns   []fakeTurn
	steers  []string
	dials   int
	conns   []net.Conn

	failDial    bool
	dropOnStart int  // connections that end right after reading turn/start, the turn kept
	forgetIDs   bool // the thread does not give the client ids back
	refuseStart bool
	replies     chan frame // the client's answers to the server's requests
}

type fakeTurn struct{ id, clientID, text string }

func newFake() *fakeServer {
	return &fakeServer{threads: []string{"thread-1"}, replies: make(chan frame, 4)}
}

func (f *fakeServer) dial(context.Context) (io.ReadWriteCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dials++
	if f.failDial {
		return nil, errors.New("no server")
	}
	client, server := net.Pipe()
	f.conns = append(f.conns, server)
	go f.serve(server)
	return client, nil
}

func (f *fakeServer) send(v any) {
	f.mu.Lock()
	c := f.conns[len(f.conns)-1]
	f.mu.Unlock()
	b, _ := json.Marshal(v)
	_, _ = c.Write(append(b, '\n'))
}

func (f *fakeServer) started() []fakeTurn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeTurn(nil), f.turns...)
}

func (f *fakeServer) serve(c net.Conn) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	answer := func(id json.RawMessage, result any) {
		b, _ := json.Marshal(map[string]any{"id": id, "result": result})
		_, _ = c.Write(append(b, '\n'))
	}
	for sc.Scan() {
		var m frame
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Method == "" {
			f.replies <- m
			continue
		}
		var p struct {
			ThreadID string `json:"threadId"`
			Input    []struct {
				Text string `json:"text"`
			} `json:"input"`
			ClientID string `json:"clientUserMessageId"`
			Turns    bool   `json:"includeTurns"`
		}
		_ = json.Unmarshal(m.Params, &p)
		switch m.Method {
		case "initialize":
			answer(m.ID, map[string]any{"userAgent": "fake"})
		case "initialized":
		case "thread/loaded/list":
			f.mu.Lock()
			ids := append([]string{}, f.threads...)
			f.mu.Unlock()
			answer(m.ID, map[string]any{"data": ids})
		case "thread/resume":
			answer(m.ID, map[string]any{"thread": map[string]any{"id": p.ThreadID}})
		case "thread/read":
			thread := map[string]any{"id": p.ThreadID}
			f.mu.Lock()
			if p.ThreadID != f.threads[0] {
				thread["parentThreadId"] = f.threads[0]
			} else if p.Turns {
				turns := []any{}
				for _, t := range f.turns {
					msg := map[string]any{"type": "userMessage", "id": "item-" + t.id, "content": []any{}}
					if !f.forgetIDs {
						msg["clientId"] = t.clientID
					}
					turns = append(turns, map[string]any{"id": t.id, "status": "completed", "items": []any{msg}})
				}
				thread["turns"] = turns
			}
			f.mu.Unlock()
			answer(m.ID, map[string]any{"thread": thread})
		case "turn/steer":
			f.mu.Lock()
			f.steers = append(f.steers, p.ClientID)
			f.mu.Unlock()
			answer(m.ID, map[string]any{"turnId": "turn-live"})
		case "turn/start":
			f.mu.Lock()
			if f.refuseStart {
				f.mu.Unlock()
				b, _ := json.Marshal(map[string]any{"id": m.ID, "error": map[string]any{"code": -32600, "message": "no"}})
				_, _ = c.Write(append(b, '\n'))
				continue
			}
			text := ""
			if len(p.Input) > 0 {
				text = p.Input[0].Text
			}
			t := fakeTurn{id: "turn-" + string(rune('a'+len(f.turns))), clientID: p.ClientID, text: text}
			f.turns = append(f.turns, t)
			drop := f.dropOnStart > 0
			if drop {
				f.dropOnStart--
			}
			f.mu.Unlock()
			if drop {
				return
			}
			answer(m.ID, map[string]any{"turn": map[string]any{"id": t.id, "status": "inProgress", "items": []any{}}})
		}
	}
}

// events records what a session tells.
type events struct {
	mu      sync.Mutex
	ended   []string
	lost    int
	decide  Decision
	answers bool
	asked   chan Approval
	gone    chan struct{}
}

func (e *events) TurnStarted(string) {}
func (e *events) TurnEnded(turnID, status, detail string) {
	e.mu.Lock()
	e.ended = append(e.ended, turnID+" "+status+" "+detail)
	e.mu.Unlock()
}
func (e *events) Lost() {
	e.mu.Lock()
	e.lost++
	e.mu.Unlock()
}
func (e *events) Approval(ctx context.Context, a Approval) (Decision, bool) {
	e.asked <- a
	if e.answers {
		return e.decide, true
	}
	<-ctx.Done()
	close(e.gone)
	return "", false
}

func within(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out: %s", what)
}

func TestMessageIsDeliveredOnceAndTheTurnEndComes(t *testing.T) {
	f, ev := newFake(), &events{}
	s := NewSession(f.dial, ev)
	defer s.Close()
	m := Message{ID: "c1", Text: "hello"}
	for i := 0; i < 3; i++ {
		if err := s.Send(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	turns := f.started()
	if len(turns) != 1 || turns[0].clientID != "c1" || turns[0].text != "hello" || f.dials != 1 {
		t.Fatalf("turns %+v, dials %d", turns, f.dials)
	}
	f.send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-1",
		"turn": map[string]any{"id": turns[0].id, "status": "failed", "items": []any{}, "error": map[string]any{"message": "boom"}}}})
	// A turn of a sub-agent's thread is not the chat's.
	f.send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-2",
		"turn": map[string]any{"id": "other", "status": "completed", "items": []any{}}}})
	if err := s.Send(context.Background(), Message{ID: "c2", Text: "next"}); err != nil {
		t.Fatal(err)
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.ended) != 1 || ev.ended[0] != "turn-a failed boom" {
		t.Fatalf("ended %q", ev.ended)
	}
}

func TestSendAfterABreakDoesNotStartASecondTurn(t *testing.T) {
	f, ev := newFake(), &events{}
	f.dropOnStart = 1
	s := NewSession(f.dial, ev)
	defer s.Close()
	// The server took the message and the connection ended before its answer: the thread, read again, has it.
	if err := s.Send(context.Background(), Message{ID: "c1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), Message{ID: "c1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if turns := f.started(); len(turns) != 1 || f.dials != 2 {
		t.Fatalf("turns %+v, dials %d", turns, f.dials)
	}
	within(t, "the break is told", func() bool { ev.mu.Lock(); defer ev.mu.Unlock(); return ev.lost == 1 })

	// A session made anew (the runtime restarted) learns from the thread what was delivered before it.
	again := NewSession(f.dial, nil)
	defer again.Close()
	if err := again.Send(context.Background(), Message{ID: "c1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if turns := f.started(); len(turns) != 1 {
		t.Fatalf("turns %+v", turns)
	}
}

func TestAMessageTheThreadDoesNotShowIsNeverSentAgain(t *testing.T) {
	f := newFake()
	f.dropOnStart, f.forgetIDs = 1, true
	s := NewSession(f.dial, nil)
	defer s.Close()
	for i := 0; i < 2; i++ {
		if err := s.Send(context.Background(), Message{ID: "c1", Text: "hello"}); !errors.Is(err, ErrUncertain) {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if turns := f.started(); len(turns) != 1 {
		t.Fatalf("turns %+v", turns)
	}
}

func TestAFailureBeforeTheServerTookTheMessageIsNotUncertain(t *testing.T) {
	m := Message{ID: "c1", Text: "hello"}
	f := newFake()
	f.failDial = true
	if err := NewSession(f.dial, nil).Send(context.Background(), m); err == nil || errors.Is(err, ErrUncertain) {
		t.Fatalf("no server: %v", err)
	}
	f = newFake()
	f.refuseStart = true
	var refused *RPCError
	if err := NewSession(f.dial, nil).Send(context.Background(), m); !errors.As(err, &refused) {
		t.Fatalf("refused: %v", err)
	}
	f = newFake()
	f.threads = nil
	if err := NewSession(f.dial, nil).Send(context.Background(), m); !errors.Is(err, ErrNotReady) {
		t.Fatalf("no thread: %v", err)
	}
	if len(f.started()) != 0 {
		t.Fatal("nothing was to start")
	}
	// Sub-agents' threads beside the chat's do not hide it.
	f = newFake()
	f.threads = []string{"thread-1", "thread-2"}
	if err := NewSession(f.dial, nil).Send(context.Background(), m); err != nil || len(f.started()) != 1 {
		t.Fatalf("with a sub-agent: %v", err)
	}
}

func TestSteerGoesIntoTheRunningTurn(t *testing.T) {
	f := newFake()
	s := NewSession(f.dial, nil)
	defer s.Close()
	if err := s.Send(context.Background(), Message{ID: "c1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), Message{ID: "c2", Text: "and this", Steer: true}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.turns) != 1 || len(f.steers) != 1 || f.steers[0] != "c2" {
		t.Fatalf("turns %+v steers %q", f.turns, f.steers)
	}
}

func TestApprovalIsAnsweredOverTheChannel(t *testing.T) {
	f := newFake()
	ev := &events{asked: make(chan Approval, 1), answers: true, decide: Accept}
	s := NewSession(f.dial, ev)
	defer s.Close()
	if err := s.Send(context.Background(), Message{ID: "c1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	f.send(map[string]any{"id": 7, "method": "item/commandExecution/requestApproval", "params": map[string]any{
		"threadId": "thread-1", "turnId": "turn-a", "itemId": "i1", "command": "rm -rf build", "cwd": "/work"}})
	if a := <-ev.asked; a.Command != "rm -rf build" || a.Cwd != "/work" {
		t.Fatalf("%+v", a)
	}
	select {
	case r := <-f.replies:
		if string(r.ID) != "7" || string(r.Result) != `{"decision":"accept"}` {
			t.Fatalf("%s %s", r.ID, r.Result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no answer")
	}
}

func TestApprovalAnsweredElsewhereIsLetGo(t *testing.T) {
	f := newFake()
	ev := &events{asked: make(chan Approval, 1), gone: make(chan struct{})}
	s := NewSession(f.dial, ev)
	defer s.Close()
	if err := s.Send(context.Background(), Message{ID: "c1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	f.send(map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-a",
		"item": map[string]any{"type": "fileChange", "id": "i2", "changes": []any{map[string]any{"path": "a.go"}}}}})
	f.send(map[string]any{"id": "r-9", "method": "item/fileChange/requestApproval", "params": map[string]any{
		"threadId": "thread-1", "turnId": "turn-a", "itemId": "i2"}})
	if a := <-ev.asked; len(a.Paths) != 1 || a.Paths[0] != "a.go" {
		t.Fatalf("%+v", a)
	}
	// The person answered in the terminal: the request is over, and the session answers nothing.
	f.send(map[string]any{"method": "serverRequest/resolved", "params": map[string]any{"threadId": "thread-1", "requestId": "r-9"}})
	select {
	case <-ev.gone:
	case <-time.After(3 * time.Second):
		t.Fatal("the approval was not let go")
	}
	select {
	case r := <-f.replies:
		t.Fatalf("answered %s", r.Result)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestManagerFindsAChatByItsSocket(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	m := &Manager{Dir: dir, Dial: func(ctx context.Context, _ string) (io.ReadWriteCloser, error) { return f.dial(ctx) }}
	msg := Message{ID: "c1", Text: "hello"}
	if err := m.Send(context.Background(), "tab-1", msg); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("no socket: %v", err)
	}
	// A file that is no socket is no server.
	_ = os.WriteFile(filepath.Join(dir, "tab-2.sock"), nil, 0o600)
	if err := m.Send(context.Background(), "tab-2", msg); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("a plain file: %v", err)
	}
	l, err := net.Listen("unix", m.Socket("tab-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := m.Send(context.Background(), "tab-1", msg); err != nil || len(f.started()) != 1 {
		t.Fatalf("with a socket: %v", err)
	}
	if m.Socket("../x") != "" || m.Socket("") != "" || (&Manager{Dir: "/" + string(make([]byte, 120))}).Socket("tab-1") != "" {
		t.Fatal("an unusable path names no socket")
	}
	m.Forget("tab-1")
}

func TestSupported(t *testing.T) {
	for v, want := range map[string]bool{"codex-cli 0.147.0": true, "codex-cli 0.148.2": true, "codex-cli 1.0.0": true,
		"codex-cli 0.147.1-beta.2": true, "codex-cli 0.146.9": false, "codex-cli": false, "": false, "codex-cli nightly": false} {
		if Supported(v) != want {
			t.Errorf("%q: want %v", v, want)
		}
	}
}

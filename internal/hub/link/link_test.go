package link

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type testHub struct {
	t       *testing.T
	db      *store.DB
	srv     *httptest.Server
	dialer  *Dialer
	reg     *Registry
	host    string
	machine store.Machine
	priv    ed25519.PrivateKey

	connected    chan *Link
	events       chan Event
	disconnected chan bool
}

func newTestHub(t *testing.T) *testHub {
	t.Helper()
	h := &testHub{t: t, reg: NewRegistry(), connected: make(chan *Link, 8), events: make(chan Event, 64), disconnected: make(chan bool, 8)}
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h.db = db
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h.priv = priv
	encPub := make([]byte, 32)
	h.machine = store.Machine{
		Name: "m", Hostname: "box", Profile: "dir", Via: "command",
		KeyPub: base64.StdEncoding.EncodeToString(pub), EncPub: base64.StdEncoding.EncodeToString(encPub),
	}
	if err := db.S().CreateMachine(context.Background(), &h.machine); err != nil {
		t.Fatal(err)
	}
	h.dialer = &Dialer{
		DB: db, Reg: h.reg, Limiter: ratelimit.New(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ClientIP: func(*http.Request) string { return "203.0.113.7" },
		Hooks: Hooks{
			Connected: func(l *Link, _ store.Machine) { h.connected <- l },
			Event:     func(_ *Link, _ store.Machine, ev Event) { h.events <- ev },
			Disconnected: func(_ *Link, _ store.Machine, current bool) {
				h.disconnected <- current
			},
		},
	}
	h.srv = httptest.NewServer(h.dialer)
	t.Cleanup(h.srv.Close)
	h.host, err = hubproto.DialHost(h.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	h.dialer.Host = h.host
	t.Cleanup(h.reg.CloseAll)
	return h
}

// dialOpts changes what a fake agent says when it dials.
type dialOpts struct {
	priv     ed25519.PrivateKey
	machine  string
	at       time.Time
	host     string
	protocol string
	version  string
	header   string // sent as is when set
}

func (h *testHub) header(o dialOpts) http.Header {
	hd := http.Header{}
	if o.header != "" {
		hd.Set("Authorization", o.header)
	} else {
		priv, id, at, host := o.priv, o.machine, o.at, o.host
		if priv == nil {
			priv = h.priv
		}
		if id == "" {
			id = h.machine.ID
		}
		if at.IsZero() {
			at = time.Now()
		}
		if host == "" {
			host = h.host
		}
		auth, err := hubproto.SignDial(priv, id, at, host)
		if err != nil {
			h.t.Fatal(err)
		}
		hd.Set("Authorization", auth)
	}
	if o.protocol == "" {
		o.protocol = "2"
	}
	hd.Set("X-Harnessly-Protocol", o.protocol)
	if o.version == "" {
		o.version = "1.0.0"
	}
	hd.Set("X-Harnessly-Agent-Version", o.version)
	return hd
}

// dialRaw dials and returns the connection or the HTTP status and body of the refusal.
func (h *testHub) dialRaw(o dialOpts) (*websocket.Conn, int, string) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.srv.URL, "http")+"/agent/v1", &websocket.DialOptions{HTTPHeader: h.header(o), CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		if res == nil {
			h.t.Fatalf("dial: %v", err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return nil, res.StatusCode, string(b)
	}
	conn.SetReadLimit(MaxPayload)
	return conn, 101, ""
}

// agent is a fake agent: it answers the hub's calls and records what it is sent.
type agent struct {
	t      *testing.T
	conn   *websocket.Conn
	mu     sync.Mutex
	ops    []string
	binary chan []byte
	// handle answers an op; a non-empty errText is an error answer.
	handle func(op string, req map[string]json.RawMessage) (result any, errText string)
	// echo sends tunnel data frames back.
	echo   bool
	closed chan websocket.StatusCode
}

func (h *testHub) agent(o dialOpts) (*agent, *Link) {
	h.t.Helper()
	conn, status, body := h.dialRaw(o)
	if conn == nil {
		h.t.Fatalf("dial refused: %d %s", status, body)
	}
	a := &agent{t: h.t, conn: conn, binary: make(chan []byte, 4096), closed: make(chan websocket.StatusCode, 1)}
	h.t.Cleanup(func() { conn.CloseNow() })
	go a.run()
	select {
	case l := <-h.connected:
		return a, l
	case <-time.After(3 * time.Second):
		h.t.Fatal("the hub did not register the link")
		return nil, nil
	}
}

func (a *agent) run() {
	for {
		typ, data, err := a.conn.Read(context.Background())
		if err != nil {
			a.closed <- websocket.CloseStatus(err)
			return
		}
		if typ == websocket.MessageBinary {
			if a.echo && len(data) > 17 && data[0] == frameData {
				_ = a.conn.Write(context.Background(), websocket.MessageBinary, data)
			}
			select {
			case a.binary <- data:
			default:
			}
			continue
		}
		var req map[string]json.RawMessage
		if json.Unmarshal(data, &req) != nil {
			continue
		}
		var op string
		_ = json.Unmarshal(req["op"], &op)
		a.mu.Lock()
		a.ops = append(a.ops, op)
		a.mu.Unlock()
		go a.answer(op, req)
	}
}

func (a *agent) answer(op string, req map[string]json.RawMessage) {
	var result any
	var errText string
	switch {
	case a.handle != nil:
		result, errText = a.handle(op, req)
	case op == "hello":
		result = map[string]any{"protocol": 2, "agentVersion": "1.0.0"}
	case op == "tunnel.open":
		result = map[string]bool{"opened": true}
	default:
		errText = "unsupported: " + op
	}
	var frame []byte
	if errText != "" {
		frame, _ = json.Marshal(map[string]any{"id": json.RawMessage(req["id"]), "ok": false, "error": errText})
	} else {
		frame, _ = json.Marshal(map[string]any{"id": json.RawMessage(req["id"]), "ok": true, "result": result})
	}
	_ = a.conn.Write(context.Background(), websocket.MessageText, frame)
}

func (a *agent) event(v any) {
	b, _ := json.Marshal(v)
	if err := a.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		a.t.Fatal(err)
	}
}

func (a *agent) expectClose(want websocket.StatusCode) {
	a.t.Helper()
	select {
	case got := <-a.closed:
		if got != want {
			a.t.Errorf("closed with %d, want %d", got, want)
		}
	case <-time.After(3 * time.Second):
		a.t.Fatalf("the link was not closed (want %d)", want)
	}
}

func TestDialAcceptsAndCalls(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	if got := h.reg.Get(h.machine.ID); got != l {
		t.Fatal("the registry does not hold the link")
	}
	if l.AgentVersion != "1.0.0" || l.Protocol != 2 {
		t.Errorf("link facts: %q %d", l.AgentVersion, l.Protocol)
	}
	var hello struct {
		Protocol     int
		AgentVersion string
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := l.CallInto(ctx, "hello", nil, &hello); err != nil || hello.Protocol != 2 {
		t.Fatalf("hello: %v %+v", err, hello)
	}
	// Parameters are merged into the request, next to op and id.
	var seen map[string]json.RawMessage
	a.handle = func(op string, req map[string]json.RawMessage) (any, string) {
		seen = req
		return map[string]string{"ok": "yes"}, ""
	}
	if _, err := l.Call(ctx, "login.code", map[string]string{"loginId": "x", "code": "a\"b"}); err != nil {
		t.Fatal(err)
	}
	if string(seen["op"]) != `"login.code"` || string(seen["loginId"]) != `"x"` || string(seen["code"]) != `"a\"b"` || len(seen["id"]) == 0 {
		t.Errorf("request: %v", seen)
	}
	if _, err := l.Call(ctx, "x", []int{1}); err == nil {
		t.Error("array parameters accepted")
	}
}

func TestCallErrorsAreTyped(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	a.handle = func(op string, _ map[string]json.RawMessage) (any, string) {
		switch op {
		case "a":
			return nil, "policy_denied: terminal is off here"
		case "b":
			return nil, "busy: a login is running"
		case "never":
			time.Sleep(time.Second)
			return nil, ""
		}
		return nil, "something odd happened"
	}
	ctx := context.Background()
	for op, want := range map[string]string{"a": hubproto.CodePolicyDenied, "b": hubproto.CodeBusy, "c": hubproto.CodeFailed} {
		_, err := l.Call(ctx, op, nil)
		var he *hubproto.Error
		if !errors.As(err, &he) || he.Code != want {
			t.Errorf("%s: %v", op, err)
		}
	}
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := l.Call(short, "never", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("slow call: %v", err)
	}
	l.mu.Lock()
	n := len(l.pending)
	l.mu.Unlock()
	if n > 1 { // the slow call may still be answered; nothing else may leak
		t.Errorf("%d calls pending", n)
	}
	l.Close(websocket.StatusNormalClosure, "")
	if _, err := l.Call(ctx, "a", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("call on a closed link: %v", err)
	}
}

func TestCallEndsWhenTheLinkDrops(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	a.handle = func(string, map[string]json.RawMessage) (any, string) { time.Sleep(5 * time.Second); return nil, "" }
	errc := make(chan error, 1)
	go func() { _, err := l.Call(context.Background(), "slow", nil); errc <- err }()
	time.Sleep(50 * time.Millisecond)
	a.conn.CloseNow()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the call hangs after the agent went away")
	}
	if current := <-h.disconnected; !current {
		t.Error("a dropped link was not the current one")
	}
	if h.reg.Get(h.machine.ID) != nil {
		t.Error("a dropped link is still in the registry")
	}
}

func TestEventsInOrder(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.agent(dialOpts{})
	for i := 0; i < 20; i++ {
		a.event(map[string]any{"event": "job", "n": i})
	}
	a.event(map[string]any{"event": "policy", "policy": map[string]bool{"terminal": false}})
	for i := 0; i < 20; i++ {
		select {
		case ev := <-h.events:
			var body struct{ N int }
			_ = json.Unmarshal(ev.Raw, &body)
			if ev.Name != "job" || body.N != i {
				t.Fatalf("event %d: %s %s", i, ev.Name, ev.Raw)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("event %d never came", i)
		}
	}
	if ev := <-h.events; ev.Name != "policy" || !strings.Contains(string(ev.Raw), `"terminal":false`) {
		t.Errorf("last event: %s", ev.Raw)
	}
}

func TestDialRefusals(t *testing.T) {
	h := newTestHub(t)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	for _, c := range []struct {
		name string
		o    dialOpts
		want int
	}{
		{"wrong key", dialOpts{priv: otherPriv}, 401},
		{"unknown machine", dialOpts{machine: "aaaaaaaaaaaaaaaaaaaa"}, 401},
		{"clock ten minutes ahead", dialOpts{at: time.Now().Add(10 * time.Minute)}, 401},
		{"clock ten minutes behind", dialOpts{at: time.Now().Add(-10 * time.Minute)}, 401},
		{"signed for another host", dialOpts{host: "evil.example.com"}, 401},
		{"no header", dialOpts{header: "x"}, 401},
		{"bearer", dialOpts{header: "Bearer abc.def.ghi"}, 401},
	} {
		if conn, status, body := h.dialRaw(c.o); conn != nil || status != c.want || !strings.Contains(body, `"unauthenticated"`) {
			t.Errorf("%s: %d %s", c.name, status, body)
		}
	}
	if h.reg.Get(h.machine.ID) != nil {
		t.Error("a refused dial registered a link")
	}
	// Nothing in the answer tells which part was wrong.
	_, _, a := h.dialRaw(dialOpts{priv: otherPriv})
	_, _, b := h.dialRaw(dialOpts{machine: "aaaaaaaaaaaaaaaaaaaa"})
	if a != b {
		t.Errorf("answers differ: %q vs %q", a, b)
	}
}

func TestReplayedDialIsRefused(t *testing.T) {
	h := newTestHub(t)
	header := h.header(dialOpts{}).Get("Authorization")
	h.agentWithHeader(header)
	if conn, status, _ := h.dialRaw(dialOpts{header: header}); conn != nil || status != 401 {
		t.Errorf("replay: %d", status)
	}
	// After the memory is over the same dial would be refused by the clock, not by the nonce; a fresh nonce is fine.
	h.agent(dialOpts{})
}

func (h *testHub) agentWithHeader(header string) *agent {
	a, _ := h.agent(dialOpts{header: header})
	return a
}

func TestRevokedMachine(t *testing.T) {
	h := newTestHub(t)
	if _, err := h.db.S().RevokeMachine(context.Background(), h.machine.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if conn, status, body := h.dialRaw(dialOpts{}); conn != nil || status != 403 || !strings.Contains(body, "revoked") {
		t.Errorf("revoked: %d %s", status, body)
	}
	// A revoked machine's unsigned dial still gets the neutral answer: revocation is told to the key's holder only.
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	if conn, status, _ := h.dialRaw(dialOpts{priv: otherPriv}); conn != nil || status != 401 {
		t.Errorf("revoked, wrong key: %d", status)
	}
}

func TestOldAgentVersionAndProtocol(t *testing.T) {
	h := newTestHub(t)
	h.dialer.MinAgentVersion = "1.2.0"
	if conn, status, body := h.dialRaw(dialOpts{version: "1.1.9"}); conn != nil || status != 426 || !strings.Contains(body, "upgrade_required") {
		t.Errorf("old agent: %d %s", status, body)
	}
	h.agent(dialOpts{version: "v1.2.0-rc1"})
	h.agent(dialOpts{version: "dev"})

	h2 := newTestHub(t)
	conn, status, _ := h2.dialRaw(dialOpts{protocol: "1"})
	if conn == nil {
		t.Fatalf("protocol 1 dial: %d", status)
	}
	_, _, err := conn.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusCode(hubproto.CloseProtocolOld) {
		t.Errorf("protocol 1: %v", err)
	}
	if h2.reg.Get(h2.machine.ID) != nil {
		t.Error("an old protocol was registered")
	}
}

func TestFailedDialsAreLimited(t *testing.T) {
	h := newTestHub(t)
	for i := 0; i < failedDials; i++ {
		if _, status, _ := h.dialRaw(dialOpts{header: "x"}); status != 401 {
			t.Fatalf("dial %d: %d", i, status)
		}
	}
	if _, status, body := h.dialRaw(dialOpts{header: "x"}); status != 429 || !strings.Contains(body, "rate_limited") {
		t.Errorf("flood: %d %s", status, body)
	}
	// A good dial from the same address is held back too while the window lasts; that is the price of a flood.
	if _, status, _ := h.dialRaw(dialOpts{}); status != 429 {
		t.Errorf("good dial during a flood: %d", status)
	}
}

func TestSecondLinkReplacesTheFirst(t *testing.T) {
	h := newTestHub(t)
	first, l1 := h.agent(dialOpts{})
	second, l2 := h.agent(dialOpts{})
	first.expectClose(websocket.StatusCode(hubproto.CloseReplaced))
	if h.reg.Get(h.machine.ID) != l2 || l1 == l2 {
		t.Error("the newer link is not the machine's")
	}
	if current := <-h.disconnected; current {
		t.Error("the replaced link reported itself as current")
	}
	if got := h.reg.Online(); len(got) != 1 || got[0] != h.machine.ID {
		t.Errorf("online: %v", got)
	}
	_ = second
}

func TestRevokeClosesWith4010(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	h.reg.Revoke(h.machine.ID)
	a.expectClose(websocket.StatusCode(hubproto.CloseRevoked))
	if h.reg.Get(h.machine.ID) != nil || !l.Closed() || l.CloseCode() != websocket.StatusCode(hubproto.CloseRevoked) {
		t.Error("revoked link is still there")
	}
}

func frameOf(kind byte, id string, payload []byte) []byte {
	raw, _ := hex.DecodeString(id)
	return append(append([]byte{kind}, raw...), payload...)
}

func TestTunnelEcho(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	a.echo = true
	var opened json.RawMessage
	a.handle = func(op string, req map[string]json.RawMessage) (any, string) {
		if op == "tunnel.open" {
			opened = req["spec"]
		}
		return map[string]bool{"opened": true}, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tn, err := l.OpenTunnel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct{ TunnelID, Target string }
	_ = json.Unmarshal(opened, &spec)
	if len(spec.TunnelID) != 32 || spec.Target != "runtime" || spec.TunnelID != tn.key {
		t.Errorf("spec: %s", opened)
	}
	if l.Tunnels() != 1 {
		t.Errorf("tunnels: %d", l.Tunnels())
	}

	// A small write and a big one (several frames) come back whole and in order.
	big := make([]byte, 3<<20+17)
	_, _ = rand.Read(big)
	for _, payload := range [][]byte{[]byte("hello"), big} {
		go func(p []byte) {
			if _, err := tn.Write(p); err != nil {
				t.Error(err)
			}
		}(payload)
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(tn, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("echo differs")
		}
	}

	// Closing tells the agent, once, and the tunnel is forgotten.
	tn.Close()
	tn.Close()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case f := <-a.binary:
			if f[0] == frameClose && hex.EncodeToString(f[1:17]) == spec.TunnelID {
				if l.Tunnels() != 0 {
					t.Error("closed tunnel is still registered")
				}
				if _, err := tn.Read(make([]byte, 1)); !errors.Is(err, net.ErrClosed) {
					t.Errorf("read after close: %v", err)
				}
				return
			}
		case <-deadline:
			t.Fatal("the agent was not told to close the tunnel")
		}
	}
}

func TestTunnelRemoteCloseKeepsTheBytesSentBefore(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	tn, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = a.conn.Write(context.Background(), websocket.MessageBinary, frameOf(frameData, tn.key, []byte("last words")))
	_ = a.conn.Write(context.Background(), websocket.MessageBinary, frameOf(frameClose, tn.key, nil))
	got, err := io.ReadAll(tn)
	if err != nil || string(got) != "last words" {
		t.Errorf("ReadAll: %q %v", got, err)
	}
	// Frames for an id nobody has are ignored, not fatal.
	_ = a.conn.Write(context.Background(), websocket.MessageBinary, frameOf(frameData, strings.Repeat("ab", 16), []byte("x")))
	_ = a.conn.Write(context.Background(), websocket.MessageBinary, []byte{9, 1, 2})
	if _, err := l.Call(context.Background(), "hello", nil); err != nil {
		t.Errorf("link broke on stray frames: %v", err)
	}
}

func TestTunnelDeadlines(t *testing.T) {
	h := newTestHub(t)
	_, l := h.agent(dialOpts{})
	tn, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = tn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	start := time.Now()
	if _, err := tn.Read(make([]byte, 8)); !errors.Is(err, os.ErrDeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Errorf("read: %v after %s", err, time.Since(start))
	}
	// A deadline moved while a read waits takes effect.
	_ = tn.SetReadDeadline(time.Time{})
	errc := make(chan error, 1)
	go func() { _, err := tn.Read(make([]byte, 8)); errc <- err }()
	time.Sleep(30 * time.Millisecond)
	_ = tn.SetReadDeadline(time.Now().Add(-time.Second))
	select {
	case err := <-errc:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("woken read: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a read does not notice its deadline moving")
	}
	_ = tn.SetWriteDeadline(time.Now().Add(-time.Second))
	if _, err := tn.Write([]byte("x")); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("write: %v", err)
	}
	if tn.LocalAddr().Network() == "" || tn.RemoteAddr().String() == "" {
		t.Error("addresses")
	}
}

func TestTunnelEndsWithTheLink(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	tn, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() { _, err := tn.Read(make([]byte, 8)); errc <- err }()
	time.Sleep(30 * time.Millisecond)
	a.conn.CloseNow()
	select {
	case err := <-errc:
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("read: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a tunnel outlives its link")
	}
	if _, err := l.OpenTunnel(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("open on a dead link: %v", err)
	}
}

func TestTunnelRefusedByTheAgent(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	a.handle = func(op string, _ map[string]json.RawMessage) (any, string) {
		return nil, "policy_denied: tunnels are off"
	}
	_, err := l.OpenTunnel(context.Background())
	var he *hubproto.Error
	if !errors.As(err, &he) || he.Code != hubproto.CodePolicyDenied {
		t.Errorf("err = %v", err)
	}
	if l.Tunnels() != 0 {
		t.Error("a refused tunnel stays registered")
	}
}

func TestSlowTunnelReaderLosesTheTunnelNotTheLink(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	tn, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1<<20)
	for i := 0; i < 20; i++ { // 20 MiB against a 16 MiB queue, nobody reading
		if err := a.conn.Write(context.Background(), websocket.MessageBinary, frameOf(frameData, tn.key, chunk)); err != nil {
			t.Fatal(err)
		}
	}
	buf := make([]byte, 4)
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = tn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, err := tn.Read(buf)
		if errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tunnel was not cut: %v", err)
		}
	}
	sawClose := false
	for !sawClose {
		select {
		case f := <-a.binary:
			sawClose = f[0] == frameClose
		case <-time.After(3 * time.Second):
			t.Fatal("the agent was not told")
		}
	}
	if _, err := l.Call(context.Background(), "hello", nil); err != nil {
		t.Errorf("the link died with the tunnel: %v", err)
	}
}

func TestHubPingDropsASilentAgent(t *testing.T) {
	oldEvery, oldTimeout := pingEvery, pingTimeout
	pingEvery, pingTimeout = 50*time.Millisecond, 100*time.Millisecond
	defer func() { pingEvery, pingTimeout = oldEvery, oldTimeout }()

	// This agent never reads, so it never answers a ping.
	h := newTestHub(t)
	conn, _, _ := h.dialRaw(dialOpts{})
	defer conn.CloseNow()
	var l *Link
	select {
	case l = <-h.connected:
	case <-time.After(3 * time.Second):
		t.Fatal("not connected")
	}
	select {
	case <-l.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the hub keeps a link whose agent does not answer pings")
	}
	if h.reg.Get(h.machine.ID) != nil {
		t.Error("the silent link is still the machine's")
	}
}

func TestOlderThan(t *testing.T) {
	for _, c := range []struct {
		a, min string
		want   bool
	}{
		{"1.1.9", "1.2.0", true}, {"1.2.0", "1.2.0", false}, {"1.10.0", "1.9.9", false}, {"v1.2.0-rc1", "1.2.0", false},
		{"0.9", "1.0.0", true}, {"2", "1.9.9", false}, {"dev", "1.0.0", false}, {"", "1.0.0", false}, {"1.0.0", "garbage", false},
		{"1.2.3.4", "1.0.0", false},
	} {
		if got := OlderThan(c.a, c.min); got != c.want {
			t.Errorf("OlderThan(%q, %q) = %v", c.a, c.min, got)
		}
	}
}

func TestNonceMemory(t *testing.T) {
	d := &Dialer{}
	now := time.Now()
	if !d.remember("m", "n1", now) || d.remember("m", "n1", now.Add(time.Minute)) {
		t.Error("a nonce is remembered for the window")
	}
	if !d.remember("other", "n1", now) {
		t.Error("nonces are per machine")
	}
	if !d.remember("m", "n1", now.Add(hubproto.NonceMemory+time.Second)) {
		t.Error("a nonce is forgotten after the window")
	}
	for i := 0; i < maxNonces; i++ {
		d.remember("busy", string(rune('a'+i%26))+strings.Repeat("x", i), now)
	}
	if d.remember("busy", "one-more", now) {
		t.Error("the memory is bounded")
	}
	d.Now = func() time.Time { return now.Add(time.Hour) }
	d.Sweep()
	if len(d.nonces) != 0 {
		t.Errorf("sweep left %d machines", len(d.nonces))
	}
}

func TestMapError(t *testing.T) {
	for _, c := range []struct {
		err    error
		op     string
		status int
		code   string
	}{
		{ErrClosed, "x", 502, "machine_offline"},
		{context.DeadlineExceeded, "x", 504, "machine_timeout"},
		{hubproto.Errf(hubproto.CodePolicyDenied, "terminal off"), "key.deliver", 403, "machine_policy"},
		{hubproto.Errf(hubproto.CodeUnsupported, "gemini"), "x", 422, "provider_off"},
		{hubproto.Errf(hubproto.CodeBusy, "login running"), "login.start", 409, "conflict"},
		{hubproto.Errf(hubproto.CodeNotFound, "no such login"), "login.cancel", 404, "not_found"},
		{hubproto.Errf(hubproto.CodeInvalid, "bad name"), "env.deliver", 400, "invalid_request"},
		{hubproto.Errf(hubproto.CodeFailed, "npm broke"), "agents.install", 502, "machine_error"},
	} {
		var he *httpx.Error
		if !errors.As(MapError(c.err, c.op), &he) || he.Status != c.status || he.Code != c.code {
			t.Errorf("%v: %+v", c.err, he)
		}
	}
	var he *httpx.Error
	errors.As(MapError(hubproto.Errf(hubproto.CodePolicyDenied, "x"), "key.deliver"), &he)
	if he.Details["op"] != "key.deliver" {
		t.Errorf("details: %v", he.Details)
	}
	errors.As(MapError(hubproto.Errf(hubproto.CodeBusy, "x"), "login.start"), &he)
	if he.Details["reason"] != "login_running" {
		t.Errorf("busy details: %v", he.Details)
	}
}

func TestTimes(t *testing.T) {
	in := `{"at":1761393600000,"checks":[{"id":"a","at":"x"}],"disk":{"free":5},"access":{"at":1761393601000,"hosts":[]},"expiresAt":1761393602000,"big":12345678901234567890,"n":null}`
	var got map[string]any
	if err := json.Unmarshal(Times(json.RawMessage(in)), &got); err != nil {
		t.Fatal(err)
	}
	if got["at"] != "2025-10-25T12:00:00Z" || got["expiresAt"] != "2025-10-25T12:00:02Z" {
		t.Errorf("times: %v", got)
	}
	if got["access"].(map[string]any)["at"] != "2025-10-25T12:00:01Z" {
		t.Errorf("nested: %v", got["access"])
	}
	if got["checks"].([]any)[0].(map[string]any)["at"] != "x" || got["disk"].(map[string]any)["free"].(float64) != 5 {
		t.Errorf("other fields changed: %v", got)
	}
	if string(Times(json.RawMessage(`not json`))) != "not json" {
		t.Error("garbage is passed through")
	}
}

func TestRegistryDo(t *testing.T) {
	h := newTestHub(t)
	var he *httpx.Error
	if _, err := h.reg.Do(context.Background(), h.machine.ID, "hello", nil, time.Second); !errors.As(err, &he) || he.Code != "machine_offline" {
		t.Errorf("offline: %v", err)
	}
	a, _ := h.agent(dialOpts{})
	a.handle = func(op string, _ map[string]json.RawMessage) (any, string) {
		if op == "slow" {
			time.Sleep(time.Second)
		}
		return map[string]int{"n": 7}, ""
	}
	var out struct{ N int }
	if err := h.reg.DoInto(context.Background(), h.machine.ID, "x", nil, &out, time.Second); err != nil || out.N != 7 {
		t.Errorf("do: %v %+v", err, out)
	}
	if _, err := h.reg.Do(context.Background(), h.machine.ID, "slow", nil, 50*time.Millisecond); !errors.As(err, &he) || he.Code != "machine_timeout" {
		t.Errorf("timeout: %v", err)
	}
}

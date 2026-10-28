package hubtest

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Call is one request the hub made of the agent.
type Call struct {
	Op     string
	Params map[string]json.RawMessage
}

// Str reads a string parameter.
func (c Call) Str(key string) string {
	var s string
	_ = json.Unmarshal(c.Params[key], &s)
	return s
}

// Handler answers an operation. An error is sent as the agent's error string: a *hubproto.Error as "code: text".
type Handler func(c Call) (any, error)

// Fail is a handler error with a protocol code.
func Fail(code, text string) error { return &hubproto.Error{Code: code, Text: text} }

// Agent is a fake machine: it holds real keys, registers with a code, dials the hub signed, answers operations and
// carries tunnels to a runtime of its own (a plain HTTP server).
type Agent struct {
	S         *Stack
	MachineID string
	// Pub and Priv sign the dial; Enc is the key an API key is sealed to.
	Pub  ed25519.PublicKey
	Priv ed25519.PrivateKey
	Enc  *ecdh.PrivateKey
	// Version is what the agent says in the dial header.
	Version string
	// Hello is what the agent answers to hello; the machine id is filled in on registration.
	Hello hubproto.Hello
	// Runtime is the server the tunnels lead to.
	Runtime *httptest.Server

	mu       sync.Mutex
	conn     *websocket.Conn
	calls    []Call
	handlers map[string]Handler
	tunnels  map[string]net.Conn
	closed   chan websocket.StatusCode
	sessions int
}

// NewAgent makes an agent with fresh keys. Its runtime answers with a line that says what the request was; pass a
// handler to be the runtime instead.
func (s *Stack) NewAgent(runtime http.Handler) *Agent {
	s.T.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		s.T.Fatal(err)
	}
	enc, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		s.T.Fatal(err)
	}
	if runtime == nil {
		runtime = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("runtime " + r.Method + " " + r.URL.RequestURI() + " host=" + r.Host + " cookie=" + r.Header.Get("Cookie")))
		})
	}
	a := &Agent{
		S: s, Pub: pub, Priv: priv, Enc: enc, Version: "1.0.0",
		Hello: hubproto.Hello{
			Protocol: hubproto.Version, AgentVersion: "1.0.0", User: "dev", Home: "/home/dev", Hostname: "dev-box",
			Features: hubproto.FeaturesV2, OS: "linux", Arch: "amd64", Profile: hubproto.ProfileDir,
			Policy: hubproto.Policy{Terminal: true, AgentLogin: true, Keys: true, Env: true, Install: true, RuntimeSession: true, Source: "default"},
		},
		Runtime:  httptest.NewServer(runtime),
		handlers: map[string]Handler{}, tunnels: map[string]net.Conn{}, closed: make(chan websocket.StatusCode, 4),
	}
	s.T.Cleanup(a.Runtime.Close)
	s.T.Cleanup(a.Close)
	return a
}

// Handle sets the answer to an operation.
func (a *Agent) Handle(op string, h Handler) {
	a.mu.Lock()
	a.handlers[op] = h
	a.mu.Unlock()
}

// Calls are the requests received so far, optionally of one operation.
func (a *Agent) Calls(op string) []Call {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Call
	for _, c := range a.calls {
		if op == "" || c.Op == op {
			out = append(out, c)
		}
	}
	return out
}

// RegisterRequest is what the agent sends to register.
func (a *Agent) RegisterRequest(code string, profile hubproto.Profile, via hubproto.Via) hubproto.RegisterRequest {
	return hubproto.RegisterRequest{
		Code: code, PublicKey: base64.StdEncoding.EncodeToString(a.Pub), EncKey: base64.StdEncoding.EncodeToString(a.Enc.PublicKey().Bytes()),
		Hostname: a.Hello.Hostname, OS: "linux", Arch: "amd64", UnixUser: a.Hello.User, Profile: profile, Via: via, AgentVersion: a.Version,
	}
}

// Register registers with a code and returns the answer; on 201 the agent knows its machine id.
func (a *Agent) Register(code string, profile hubproto.Profile, via hubproto.Via) Resp {
	a.S.T.Helper()
	r := a.S.Browser().Do("POST", "/agent/register", a.RegisterRequest(code, profile, via))
	if r.Code == http.StatusCreated {
		var res hubproto.RegisterResponse
		r.JSON(&res)
		a.MachineID = res.MachineID
		a.Hello.MachineID = res.MachineID
	}
	return r
}

// TryConnect dials the hub. On success it returns 101; on a refusal, the HTTP status and body.
func (a *Agent) TryConnect() (int, string) {
	a.S.T.Helper()
	host, err := hubproto.DialHost(a.S.Cfg.PublicURL)
	if err != nil {
		a.S.T.Fatal(err)
	}
	auth, err := hubproto.SignDial(a.Priv, a.MachineID, a.S.Clock.Now(), host)
	if err != nil {
		a.S.T.Fatal(err)
	}
	hd := http.Header{}
	hd.Set("Authorization", auth)
	hd.Set("X-Harnessly-Protocol", strconv.Itoa(hubproto.Version))
	hd.Set("X-Harnessly-Agent-Version", a.Version)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(a.S.URL, "http")+"/agent/v1", &websocket.DialOptions{HTTPHeader: hd, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		if res == nil {
			a.S.T.Fatalf("dial: %v", err)
		}
		defer res.Body.Close()
		buf := make([]byte, 4096)
		n, _ := res.Body.Read(buf)
		return res.StatusCode, string(buf[:n])
	}
	conn.SetReadLimit(32 << 20)
	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()
	go a.run(conn)
	return http.StatusSwitchingProtocols, ""
}

// Connect dials the hub and waits for the connect routine (hello, subscribe, preflight, agents, proxy) to be saved.
// Unlink asks the hub, signed with the agent's key, to remove the machine (DELETE /agent/v1) and returns the status.
func (a *Agent) Unlink() int {
	a.S.T.Helper()
	host, err := hubproto.DialHost(a.S.Cfg.PublicURL)
	if err != nil {
		a.S.T.Fatal(err)
	}
	auth, err := hubproto.SignUnlink(a.Priv, a.MachineID, a.S.Clock.Now(), host)
	if err != nil {
		a.S.T.Fatal(err)
	}
	return a.unlinkWith(auth)
}

// UnlinkWith is Unlink with an Authorization header made by the caller.
func (a *Agent) UnlinkWith(auth string) int { a.S.T.Helper(); return a.unlinkWith(auth) }

func (a *Agent) unlinkWith(auth string) int {
	a.S.T.Helper()
	req, err := http.NewRequest(http.MethodDelete, a.S.URL+"/agent/v1", nil)
	if err != nil {
		a.S.T.Fatal(err)
	}
	req.Header.Set("Authorization", auth)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.S.T.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

func (a *Agent) Connect() {
	a.S.T.Helper()
	if code, body := a.TryConnect(); code != http.StatusSwitchingProtocols {
		a.S.T.Fatalf("dial refused: %d %s", code, body)
	}
	a.S.Eventually("the connect routine", func() bool {
		m, err := a.S.DB.S().MachineByID(context.Background(), a.MachineID)
		return err == nil && strings.Contains(m.Info, `"proxy"`)
	})
}

// Close drops the link from the agent's side.
func (a *Agent) Close() {
	a.mu.Lock()
	conn := a.conn
	for id, c := range a.tunnels {
		_ = c.Close()
		delete(a.tunnels, id)
	}
	a.mu.Unlock()
	if conn != nil {
		_ = conn.CloseNow()
	}
}

// WaitClosed waits until the hub ends the link and returns its close code.
func (a *Agent) WaitClosed() websocket.StatusCode {
	a.S.T.Helper()
	select {
	case code := <-a.closed:
		return code
	case <-time.After(5 * time.Second):
		a.S.T.Fatal("the link was not closed")
		return 0
	}
}

// Emit sends an event frame.
func (a *Agent) Emit(v any) {
	a.S.T.Helper()
	b, _ := json.Marshal(v)
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if err := conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		a.S.T.Fatal(err)
	}
}

func (a *Agent) run(conn *websocket.Conn) {
	for {
		typ, data, err := conn.Read(context.Background())
		if err != nil {
			select {
			case a.closed <- websocket.CloseStatus(err):
			default:
			}
			return
		}
		if typ == websocket.MessageBinary {
			a.tunnelFrame(data)
			continue
		}
		var req map[string]json.RawMessage
		if json.Unmarshal(data, &req) != nil {
			continue
		}
		var op string
		_ = json.Unmarshal(req["op"], &op)
		c := Call{Op: op, Params: req}
		a.mu.Lock()
		a.calls = append(a.calls, c)
		a.mu.Unlock()
		go a.answer(conn, c, req["id"])
	}
}

func (a *Agent) answer(conn *websocket.Conn, c Call, id json.RawMessage) {
	a.mu.Lock()
	h := a.handlers[c.Op]
	a.mu.Unlock()
	var (
		result any
		err    error
	)
	if h != nil {
		result, err = h(c)
	} else {
		result, err = a.defaults(c)
	}
	frame := map[string]any{"id": id, "ok": err == nil}
	if err != nil {
		frame["error"] = err.Error()
	} else {
		frame["result"] = result
	}
	b, _ := json.Marshal(frame)
	_ = conn.Write(context.Background(), websocket.MessageText, b)
}

// defaults answer what the hub asks of every machine when it connects.
func (a *Agent) defaults(c Call) (any, error) {
	now := a.S.Clock.Now()
	switch c.Op {
	case hubproto.OpHello:
		return a.Hello, nil
	case hubproto.OpLinkSubscribe:
		return map[string]bool{"subscribed": true}, nil
	case hubproto.OpPreflightRun:
		return Preflight(now), nil
	case hubproto.OpAgentsStatus:
		return map[string]any{"agents": []hubproto.AgentStatus{
			{Provider: "claude", Installed: true, Version: ptr("2.1.0"), Login: hubproto.LoggedIn, AuthMode: ptr(hubproto.AuthSubscription)},
			{Provider: "codex", Login: hubproto.LoggedOut},
		}}, nil
	case hubproto.OpProxyGet:
		return hubproto.ProxySummary{}, nil
	case hubproto.OpRuntimeSess:
		a.mu.Lock()
		a.sessions++
		n := a.sessions
		a.mu.Unlock()
		return hubproto.SessionGrant{CookieName: "hl_runtime", Token: "runtime-token-" + strconv.Itoa(n), ExpiresAt: now.Add(time.Hour).UnixMilli()}, nil
	case hubproto.OpTunnelOpen:
		return a.openTunnel(c)
	}
	return nil, Fail(hubproto.CodeUnsupported, c.Op+" is not handled by the fake agent")
}

func ptr[T any](v T) *T { return &v }

// Preflight is a report of a healthy machine.
func Preflight(at time.Time) hubproto.PreflightReport {
	return hubproto.PreflightReport{
		At: at.UnixMilli(), OSName: "Ubuntu 24.04", OS: "linux", Arch: "amd64", CPUs: 4, MemTotal: 8 << 30, MemAvailable: 4 << 30,
		Disk:   hubproto.Disk{Path: "/home/dev", Total: 100 << 30, Free: 50 << 30},
		Tools:  hubproto.Tools{Tmux: hubproto.ToolState{Installed: true, Version: ptr("3.4"), Compatible: ptr(true)}, Git: hubproto.ToolState{Installed: true, Version: ptr("2.43")}},
		Checks: []hubproto.Check{{ID: "disk", Level: hubproto.LevelOK, Message: "Места достаточно"}},
	}
}

// openTunnel dials the runtime and starts carrying its bytes.
func (a *Agent) openTunnel(c Call) (any, error) {
	var spec struct {
		TunnelID string `json:"tunnelId"`
		Target   string `json:"target"`
	}
	if err := json.Unmarshal(c.Params["spec"], &spec); err != nil || spec.Target != "runtime" {
		return nil, Fail(hubproto.CodeInvalid, "bad spec")
	}
	raw, err := hex.DecodeString(spec.TunnelID)
	if err != nil || len(raw) != 16 {
		return nil, Fail(hubproto.CodeInvalid, "bad tunnel id")
	}
	conn, err := net.Dial("tcp", strings.TrimPrefix(a.Runtime.URL, "http://"))
	if err != nil {
		return nil, Fail(hubproto.CodeFailed, "runtime is down")
	}
	a.mu.Lock()
	a.tunnels[spec.TunnelID] = conn
	ws := a.conn
	a.mu.Unlock()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				frame := append(append([]byte{1}, raw...), buf[:n]...)
				if ws.Write(context.Background(), websocket.MessageBinary, frame) != nil {
					return
				}
			}
			if err != nil {
				_ = ws.Write(context.Background(), websocket.MessageBinary, append([]byte{2}, raw...))
				return
			}
		}
	}()
	return map[string]bool{"opened": true}, nil
}

func (a *Agent) tunnelFrame(data []byte) {
	if len(data) < 17 {
		return
	}
	id := hex.EncodeToString(data[1:17])
	a.mu.Lock()
	c := a.tunnels[id]
	if data[0] == 2 {
		delete(a.tunnels, id)
	}
	a.mu.Unlock()
	if c == nil {
		return
	}
	if data[0] == 2 {
		_ = c.Close()
		return
	}
	_, _ = c.Write(data[17:])
}

// OpenTunnels is how many tunnels the agent holds open.
func (a *Agent) OpenTunnels() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.tunnels)
}

// Eventually waits for a condition, polling.
func (s *Stack) Eventually(what string, cond func() bool) {
	s.T.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.T.Fatalf("timed out waiting for %s", what)
}

// NewCode makes a registration code in a space the person owns.
func (p *Person) NewCode(spaceID, profile string) string {
	p.S.T.Helper()
	r := p.Do("POST", "/api/spaces/"+spaceID+"/machines/codes", map[string]any{"profile": profile})
	if r.Code != http.StatusCreated {
		p.S.T.Fatalf("code: %s", r)
	}
	var out struct{ Code string }
	r.JSON(&out)
	return out.Code
}

// Machine registers a new agent in the space with the dir profile and connects it.
func (p *Person) Machine(spaceID string, runtime http.Handler) *Agent {
	p.S.T.Helper()
	a := p.S.NewAgent(runtime)
	if r := a.Register(p.NewCode(spaceID, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		p.S.T.Fatalf("register: %s", r)
	}
	a.Connect()
	return a
}

// ErrUnexpected is for tests that want a handler to fail without a protocol code.
var ErrUnexpected = errors.New("boom")

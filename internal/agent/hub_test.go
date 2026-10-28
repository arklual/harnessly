package agent

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/agentnet"
	"github.com/ly-solutions/harnessly/internal/agents/agentstest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A fake hub: the server end of the signed link, which checks the dial as the real one will and then asks the agent
// things. The agent under test is the real one, with fake CLIs behind its login shell and a fake runtime next door.

type hubConn struct {
	t    *testing.T
	conn *websocket.Conn
	ctx  context.Context

	mu      sync.Mutex
	next    int
	pending map[int]chan map[string]json.RawMessage
	events  []map[string]json.RawMessage
	binary  [][]byte
	closed  error
	done    chan struct{}
}

func newHubConn(t *testing.T, ctx context.Context, conn *websocket.Conn) *hubConn {
	c := &hubConn{t: t, conn: conn, ctx: ctx, pending: map[int]chan map[string]json.RawMessage{}, done: make(chan struct{})}
	conn.SetReadLimit(MaxPayload)
	go func() {
		defer close(c.done)
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				c.mu.Lock()
				c.closed = err
				c.mu.Unlock()
				return
			}
			if typ == websocket.MessageBinary {
				c.mu.Lock()
				c.binary = append(c.binary, data)
				c.mu.Unlock()
				continue
			}
			var m map[string]json.RawMessage
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			if raw, ok := m["event"]; ok && raw != nil {
				c.mu.Lock()
				c.events = append(c.events, m)
				c.mu.Unlock()
				continue
			}
			var id int
			_ = json.Unmarshal(m["id"], &id)
			c.mu.Lock()
			ch := c.pending[id]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}()
	return c
}

// call asks the agent and returns the result, or the error text it answered with.
func (c *hubConn) call(op string, params map[string]any) (json.RawMessage, string) {
	c.t.Helper()
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan map[string]json.RawMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	req := map[string]any{"op": op, "id": id}
	for k, v := range params {
		req[k] = v
	}
	raw, _ := json.Marshal(req)
	if err := c.conn.Write(c.ctx, websocket.MessageText, raw); err != nil {
		c.t.Fatalf("%s: %v", op, err)
	}
	select {
	case m := <-ch:
		var ok bool
		_ = json.Unmarshal(m["ok"], &ok)
		if !ok {
			var e string
			_ = json.Unmarshal(m["error"], &e)
			return nil, e
		}
		return m["result"], ""
	case <-time.After(30 * time.Second):
		c.t.Fatalf("%s: no answer", op)
		return nil, ""
	}
}

func (c *hubConn) mustCall(op string, params map[string]any) json.RawMessage {
	c.t.Helper()
	res, errText := c.call(op, params)
	if errText != "" {
		c.t.Fatalf("%s: %s", op, errText)
	}
	return res
}

// waitEvent returns the first event of that name after the n-th seen.
func (c *hubConn) waitEvent(name string, after int, within time.Duration) (map[string]json.RawMessage, int) {
	c.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for i := after; i < len(c.events); i++ {
			var n string
			_ = json.Unmarshal(c.events[i]["event"], &n)
			if n == name {
				ev := c.events[i]
				c.mu.Unlock()
				return ev, i + 1
			}
		}
		c.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	return nil, after
}

func (c *hubConn) closeError() error {
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		c.t.Fatal("the link did not close")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type fakeHub struct {
	t         *testing.T
	srv       *httptest.Server
	pub       ed25519.PublicKey
	machineID string

	mu      sync.Mutex
	nonces  map[string]bool
	headers []http.Header
	// reject answers the dial with this status instead of upgrading; 0 upgrades.
	reject atomic.Int32
	dials  atomic.Int32
	conns  chan *hubConn
	ctx    context.Context
}

func newFakeHub(t *testing.T, pub ed25519.PublicKey, machineID string) *fakeHub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f := &fakeHub{t: t, pub: pub, machineID: machineID, nonces: map[string]bool{}, conns: make(chan *hubConn, 8), ctx: ctx}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/v1" {
			http.NotFound(w, r)
			return
		}
		f.dials.Add(1)
		f.mu.Lock()
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		if code := int(f.reject.Load()); code != 0 {
			w.WriteHeader(code)
			return
		}
		auth, err := hubproto.ParseDialHeader(r.Header.Get("Authorization"))
		if err != nil || auth.MachineID != f.machineID || hubproto.VerifyDial(f.pub, auth, strings.ToLower(r.Host), time.Now()) != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		replay := f.nonces[auth.Nonce]
		f.nonces[auth.Nonce] = true
		f.mu.Unlock()
		if replay {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		f.conns <- newHubConn(t, ctx, conn)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeHub) dialURL() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/agent/v1" }

func (f *fakeHub) waitConn(within time.Duration) *hubConn {
	f.t.Helper()
	select {
	case c := <-f.conns:
		return c
	case <-time.After(within):
		f.t.Fatal("the agent did not dial")
		return nil
	}
}

// hubRig is a registered machine running as an agent against a fake hub and a fake runtime.
type hubRig struct {
	t       *testing.T
	rig     *agentstest.Rig
	hub     *fakeHub
	agent   *Agent
	machine *MachineFile
	runtime *fakeRuntime
	secret  string
	cancel  context.CancelFunc
	done    chan struct{}
}

const hubTestSecret = "0123456789abcdef0123456789abcdef-test-secret"

type rigOptions struct {
	policy       string // the policy file's text; "" means none
	noSecretYet  bool
	runtimeRoute func(w http.ResponseWriter, r *http.Request) bool
	// before runs on the hub before the agent starts.
	before func(h *fakeHub)
}

func startHubAgent(t *testing.T, o rigOptions) *hubRig {
	t.Helper()
	rig := agentstest.New(t)
	keys, err := newMachineKeys()
	if err != nil {
		t.Fatal(err)
	}
	id := "abcdefghijklmnopqrst"
	hub := newFakeHub(t, ed25519.NewKeyFromSeed(keys.seed).Public().(ed25519.PublicKey), id)
	m := keys.file(hubproto.RegisterResponse{MachineID: id, DialURL: hub.dialURL(), RuntimeBasePath: "/m/" + id, HubURL: hub.srv.URL}, hubproto.ProfileDir, time.Now())

	rt := newFakeRuntimeWith(t, o.runtimeRoute)
	dataDir := filepath.Join(rig.Home, ".harnessly")
	secretFile := filepath.Join(dataDir, "secret")
	if !o.noSecretYet {
		writeFile(t, secretFile, hubTestSecret)
	}
	policyFile := filepath.Join(rig.Home, "policy.toml")
	if o.policy != "" {
		writeFile(t, policyFile, o.policy)
	}
	if o.before != nil {
		o.before(hub)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	base := m.RuntimeBasePath
	cfg := Config{
		Home: rig.Home, Host: "127.0.0.1", Port: port, Version: "9.9.9-test", ScanInterval: time.Hour,
		DataDir: dataDir, ConfigFile: filepath.Join(dataDir, "config.json"), SecretFile: secretFile,
		RuntimePort: rt.port, ListenAudience: "agent", DialAudience: "ingest",
		RuntimeAPI: base + "/api/agent", StateChangedPath: base + "/api/agent/state-changed", StatusPath: base + "/api/status", SyncPath: base + "/api/sync",
		RuntimeCookie: "session-token", LocalTokenHeader: "x-agent-local-token", ReportPortEnv: "AGENT_REPORT_PORT",
		Auth: AuthEd25519, Machine: m, MachineFile: filepath.Join(dataDir, "machine.json"), PolicyFile: policyFile,
		EnvDir: rig.EnvDir, WorkDir: rig.Home,
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a.hub.shell = rig.Checker
	a.hub.check = fakeProviderChecker(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := &hubRig{t: t, rig: rig, hub: hub, agent: a, machine: m, runtime: rt, secret: secretFile, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		_ = a.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(15 * time.Second):
			t.Error("the agent did not stop")
		}
	})
	return r
}

func (r *hubRig) connect() *hubConn { return r.hub.waitConn(20 * time.Second) }

// waitLinkState waits for the state file to satisfy ok.
func (r *hubRig) waitLinkState(ok func(LinkState) bool) LinkState {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, _ := ReadLinkState(LinkStatePath(filepath.Join(r.rig.Home, ".harnessly")))
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("link state never got there: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (r *hubRig) audit() string {
	raw, _ := os.ReadFile(filepath.Join(r.rig.Home, ".harnessly", "agent-audit.log"))
	return string(raw)
}

func newFakeRuntimeWith(t *testing.T, route func(w http.ResponseWriter, r *http.Request) bool) *fakeRuntime {
	t.Helper()
	rt := &fakeRuntime{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if route != nil && route(w, r) {
			return
		}
		b := new(strings.Builder)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		s := b.String()
		rt.body.Store(&s)
		rt.last.Store(r.Clone(r.Context()))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.RequestURI(), "host": r.Host})
	})
	rt.srv = httptest.NewServer(mux)
	t.Cleanup(rt.srv.Close)
	rt.port = rt.srv.Listener.Addr().(*net.TCPAddr).Port
	return rt
}

// fakeProviderChecker checks "provider hosts" that are one local TLS server, so the verdicts are real without the network.
func fakeProviderChecker(t *testing.T) *agentnet.Checker {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		if key == "" {
			key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		w.Header().Set("Content-Type", "application/json")
		switch key {
		case "":
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"type":"authentication_error"}}`))
		case "sk-good-key":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"type":"authentication_error"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	addr := srv.Listener.Addr().String()
	return &agentnet.Checker{
		TLS: &tls.Config{RootCAs: pool},
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		LookupHost: func(context.Context, string) ([]string, error) { return []string{"127.0.0.1"}, nil },
		Hosts:      map[string][]string{"claude": {"example.com"}, "codex": {"example.com"}},
		Requests:   map[string]string{"claude": "https://example.com/v1/models", "codex": "https://example.com/v1/models"},
	}
}

func seal(t *testing.T, m *MachineFile, purpose, plain string) hubproto.Envelope {
	t.Helper()
	priv, err := ecdh.X25519().NewPrivateKey(m.SealingKey())
	if err != nil {
		t.Fatal(err)
	}
	env, err := hubproto.Seal(priv.PublicKey().Bytes(), m.MachineID, purpose, []byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return v
}

func TestHubDialIsSignedAndHelloAnswers(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()

	r.hub.mu.Lock()
	h := r.hub.headers[0]
	r.hub.mu.Unlock()
	if h.Get("X-Harnessly-Protocol") != "2" || h.Get("X-Harnessly-Agent-Version") != "9.9.9-test" || !strings.HasPrefix(h.Get("Authorization"), "Harnessly-Ed25519 m=abcdefghijklmnopqrst,") {
		t.Fatalf("headers %v", h)
	}

	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if hello.Protocol != 2 || hello.MachineID != "abcdefghijklmnopqrst" || hello.AgentVersion != "9.9.9-test" || hello.Profile != "dir" ||
		hello.Home != r.rig.Home || hello.User == "" || hello.OS == "" || hello.Arch == "" || !hello.Policy.Terminal || hello.Policy.Source != "default" {
		t.Fatalf("%+v", hello)
	}
	have := map[string]bool{}
	for _, f := range hello.Features {
		have[f] = true
	}
	for _, f := range append([]string{"tunnel"}, hubproto.FeaturesV2...) {
		if !have[f] {
			t.Errorf("feature %s missing from %v", f, hello.Features)
		}
	}
	if have["transcripts"] || have["state"] {
		t.Errorf("a hub is told about features it cannot use: %v", hello.Features)
	}

	// After connect: a preflight and the agents, unasked.
	if ev, _ := c.waitEvent("preflight", 0, 20*time.Second); ev == nil {
		t.Error("no preflight event after connect")
	} else if rep := decode[hubproto.PreflightReport](t, ev["report"]); rep.OS == "" || rep.Disk.Path != r.rig.Home {
		t.Errorf("%+v", rep)
	}
	if ev, _ := c.waitEvent("agents", 0, 30*time.Second); ev == nil {
		t.Error("no agents event after connect")
	} else if list := decode[[]hubproto.AgentStatus](t, ev["agents"]); len(list) != 2 || list[0].Provider != "claude" || list[1].Provider != "codex" {
		t.Errorf("%+v", list)
	}
	if !strings.Contains(r.audit(), `"op":"link.up"`) {
		t.Errorf("audit %s", r.audit())
	}
	// `status` and the installer learn from the state file that the machine is linked.
	st := r.waitLinkState(func(s LinkState) bool { return s.Linked })
	if st.MachineID != "abcdefghijklmnopqrst" || st.PID != os.Getpid() || st.HubURL != r.hub.srv.URL || st.Since == 0 || !st.Fresh(time.Now()) || st.Version != "9.9.9-test" {
		t.Errorf("%+v", st)
	}
	if fi, err := os.Stat(LinkStatePath(filepath.Join(r.rig.Home, ".harnessly"))); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("%v %v", err, fi)
	}
}

func TestHubV1OperationsAreDeniedAndUnknownOnesUnsupported(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	for _, op := range []string{"state.list", "transcripts.list", "transcripts.read", "conversations.start", "docs.deliver", "eval.start", "status.send", "design.access"} {
		if _, e := c.call(op, nil); !strings.HasPrefix(e, "policy_denied: ") {
			t.Errorf("%s: %q", op, e)
		}
	}
	if _, e := c.call("nonsense", nil); !strings.HasPrefix(e, "unsupported: ") {
		t.Errorf("%q", e)
	}
	// Nothing of the closed cluster's reached the hub as an event.
	time.Sleep(200 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ev := range c.events {
		var name string
		_ = json.Unmarshal(ev["event"], &name)
		switch name {
		case "preflight", "agents", "job", "login", "policy":
		default:
			t.Errorf("event %s on a hub link", name)
		}
	}
}

func TestHubSubscribeFiltersEvents(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	c.mustCall("link.subscribe", map[string]any{"events": []string{"policy"}})
	r.agent.hub.emit("job", "job", hubproto.Job{ID: "j"})
	r.agent.hub.emit("policy", "policy", hubproto.Policy{Source: "p"})
	if ev, _ := c.waitEvent("policy", 0, 5*time.Second); ev == nil {
		t.Fatal("the subscribed event did not come")
	}
	// An event sent after the subscription and not asked for does not arrive.
	r.agent.hub.emit("job", "job", hubproto.Job{ID: "j2"})
	time.Sleep(300 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, ev := range c.events {
		if string(ev["event"]) == `"job"` {
			n++
		}
	}
	if n > 0 {
		t.Errorf("%d job events got through a subscription to policy", n)
	}
}

func TestHubEnvKeysAndProxy(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	m := r.machine
	// The files begin with a comment line saying who wrote them; what follows is the variables.
	envFile := func(scope string) string {
		b, _ := os.ReadFile(filepath.Join(r.rig.EnvDir, scope+".env"))
		var keep []string
		for _, line := range strings.SplitAfter(string(b), "\n") {
			if line != "" && !strings.HasPrefix(line, "#") {
				keep = append(keep, line)
			}
		}
		return strings.Join(keep, "")
	}

	// A secret sealed for this machine and this name.
	res := c.mustCall("env.deliver", map[string]any{"name": "MY_TOKEN", "envelope": seal(t, m, "env:MY_TOKEN", "it's a 'secret'")})
	if names := decode[map[string][]string](t, res)["env"]; len(names) != 1 || names[0] != "MY_TOKEN" {
		t.Fatalf("%s", res)
	}
	if got := envFile("all"); got != "MY_TOKEN='it'\\''s a '\\''secret'\\'''\n" {
		t.Errorf("all.env %q", got)
	}
	if st, _ := os.Stat(filepath.Join(r.rig.EnvDir, "all.env")); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	// Sealed for another name or another machine does not open; names off the list are refused.
	if _, e := c.call("env.deliver", map[string]any{"name": "OTHER", "envelope": seal(t, m, "env:MY_TOKEN", "x")}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("wrong purpose: %q", e)
	}
	other := *m
	other.MachineID = "zzzzzzzzzzzzzzzzzzzz"
	if _, e := c.call("env.deliver", map[string]any{"name": "MY_TOKEN", "envelope": seal(t, &other, "env:MY_TOKEN", "x")}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("wrong machine: %q", e)
	}
	for _, name := range []string{"PATH", "HOME", "ANTHROPIC_API_KEY", "OPENAI_X", "HTTPS_PROXY", "AGENT_PORT", "GIT_SSH_COMMAND", "PYTHONPATH", "PLAIN", "lower", "1BAD", ""} {
		if _, e := c.call("env.deliver", map[string]any{"name": name, "envelope": seal(t, m, "env:"+name, "x")}); !strings.HasPrefix(e, "invalid: ") {
			t.Errorf("%q: %q", name, e)
		}
	}
	if _, e := c.call("env.deliver", map[string]any{"name": "MY_TOKEN"}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("no envelope: %q", e)
	}

	// A proxy is listed without its login, and the names list hides proxy variables.
	sum := decode[hubproto.ProxySummary](t, c.mustCall("proxy.set", map[string]any{"scope": "all", "url": "http://user:pw@proxy.example.com:3128"}))
	if sum.All == nil || sum.All.Host != "proxy.example.com:3128" || sum.All.Scheme != "http" || sum.Claude != nil {
		t.Fatalf("%+v", sum)
	}
	if strings.Contains(string(c.mustCall("proxy.get", nil)), "pw") {
		t.Error("the proxy's password went to the hub")
	}
	if !strings.Contains(envFile("all"), "HTTPS_PROXY='http://user:pw@proxy.example.com:3128'") {
		t.Errorf("all.env %q", envFile("all"))
	}
	if names := decode[map[string][]string](t, c.mustCall("env.remove", map[string]any{"name": "NOT_THERE"}))["env"]; len(names) != 1 || names[0] != "MY_TOKEN" {
		t.Errorf("%v", names)
	}
	if _, e := c.call("proxy.set", map[string]any{"scope": "everything", "url": "http://a:1"}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("%q", e)
	}
	if _, e := c.call("proxy.set", map[string]any{"scope": "claude", "url": "ftp://a:1"}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("%q", e)
	}
	c.mustCall("proxy.clear", map[string]any{"scope": "all"})
	if strings.Contains(envFile("all"), "PROXY") {
		t.Errorf("all.env %q", envFile("all"))
	}
	if names := decode[map[string][]string](t, c.mustCall("env.remove", map[string]any{"name": "MY_TOKEN"}))["env"]; len(names) != 0 || names == nil {
		t.Errorf("%v", names)
	}
	if _, err := os.Stat(filepath.Join(r.rig.EnvDir, "all.env")); !os.IsNotExist(err) {
		t.Errorf("an empty env file is left: %v", err)
	}

	// Keys.
	r.rig.Script("claude", `printf '%s' "$ANTHROPIC_API_KEY" > "$HOME/claude-saw"`)
	kc := decode[hubproto.KeyCheck](t, c.mustCall("key.deliver", map[string]any{"provider": "claude", "envelope": seal(t, m, "key:claude", "sk-good-key")}))
	if kc.Provider != "claude" || !kc.Present || kc.Verdict != "accepted" || !kc.Saved || kc.Status == nil || *kc.Status != 200 {
		t.Fatalf("%+v", kc)
	}
	if got := envFile("claude"); got != "ANTHROPIC_API_KEY='sk-good-key'\n" {
		t.Errorf("claude.env %q", got)
	}
	kc = decode[hubproto.KeyCheck](t, c.mustCall("key.verify", map[string]any{"provider": "claude"}))
	if kc.Verdict != "accepted" || !kc.Present || kc.Saved {
		t.Errorf("%+v", kc)
	}
	bad := decode[hubproto.KeyCheck](t, c.mustCall("key.deliver", map[string]any{"provider": "claude", "envelope": seal(t, m, "key:claude", "sk-wrong")}))
	if bad.Verdict != "rejected" || !bad.Saved {
		t.Errorf("%+v", bad)
	}
	// A key sealed for another provider is not this provider's.
	if _, e := c.call("key.deliver", map[string]any{"provider": "codex", "envelope": seal(t, m, "key:claude", "sk-good-key")}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("%q", e)
	}
	if _, e := c.call("key.deliver", map[string]any{"provider": "claude", "envelope": seal(t, m, "key:claude", "two words")}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("a key with a space: %q", e)
	}
	if _, e := c.call("key.deliver", map[string]any{"provider": "gemini", "envelope": seal(t, m, "key:gemini", "k")}); !strings.HasPrefix(e, "unsupported: ") {
		t.Errorf("gemini: %q", e)
	}
	c.mustCall("env.deliver", map[string]any{"name": "X_TOKEN", "envelope": seal(t, m, "env:X_TOKEN", "v")})
	c.mustCall("env.remove", map[string]any{"name": "X_TOKEN"})
	if err := os.Remove(filepath.Join(r.rig.EnvDir, "claude.env")); err != nil {
		t.Fatal(err)
	}
	kc = decode[hubproto.KeyCheck](t, c.mustCall("key.verify", map[string]any{"provider": "claude"}))
	if kc.Verdict != "absent" || kc.Present || kc.Status != nil {
		t.Errorf("%+v", kc)
	}

	// Codex is also given its key to sign in with.
	r.rig.Script("codex", `case "$1 $2" in "login --with-api-key") cat > "$HOME/codex-stdin" ;; esac`)
	kc = decode[hubproto.KeyCheck](t, c.mustCall("key.deliver", map[string]any{"provider": "codex", "envelope": seal(t, m, "key:codex", "sk-good-key")}))
	if kc.Verdict != "accepted" || !kc.Saved {
		t.Errorf("%+v", kc)
	}
	if got, _ := os.ReadFile(filepath.Join(r.rig.Home, "codex-stdin")); string(got) != "sk-good-key" {
		t.Errorf("codex was given %q", got)
	}
	if got := envFile("codex"); got != "OPENAI_API_KEY='sk-good-key'\n" {
		t.Errorf("codex.env %q", got)
	}

	// Nothing secret went to the audit log, and the changes are in it.
	audit := r.audit()
	for _, secret := range []string{"sk-good-key", "sk-wrong", "it's a", "pw@", "user:pw"} {
		if strings.Contains(audit, secret) {
			t.Errorf("the audit log holds %q:\n%s", secret, audit)
		}
	}
	for _, want := range []string{`"op":"env.deliver"`, `"name":"MY_TOKEN"`, `"op":"key.deliver"`, `"provider":"claude"`, `"op":"proxy.set"`, `"proxy":"http://proxy.example.com:3128"`, `"op":"env.remove"`} {
		if !strings.Contains(audit, want) {
			t.Errorf("the audit log lacks %s:\n%s", want, audit)
		}
	}
	if st, _ := os.Stat(filepath.Join(r.rig.Home, ".harnessly", "agent-audit.log")); st == nil || st.Mode().Perm() != 0o600 {
		t.Errorf("audit log mode %v", st)
	}
}

func TestHubGitKeyAndProviderCheck(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	pub := decode[map[string]string](t, c.mustCall("gitkey.ensure", nil))["publicKey"]
	if !strings.HasPrefix(pub, "ssh-ed25519 ") || !strings.Contains(pub, " harnessly@") {
		t.Fatalf("%q", pub)
	}
	if again := decode[map[string]string](t, c.mustCall("gitkey.ensure", nil))["publicKey"]; again != pub {
		t.Error("the git key changed")
	}

	chk := decode[hubproto.ProviderCheck](t, c.mustCall("provider.check", map[string]any{"provider": "claude"}))
	if chk.Provider != "claude" || chk.Verdict != "ok" || chk.Via != "direct" || len(chk.Hosts) != 1 {
		t.Fatalf("%+v", chk)
	}
	// A proxy being tried is used and not kept; the machine's own check is what the status shows.
	_, e := c.call("provider.check", map[string]any{"provider": "codex", "proxyUrl": "http://127.0.0.1:1"})
	if e != "" {
		t.Fatalf("%s", e)
	}
	list := decode[map[string][]hubproto.AgentStatus](t, c.mustCall("agents.status", map[string]any{"providers": []string{"claude", "codex", "gemini"}}))["agents"]
	if len(list) != 2 || list[0].Access == nil || list[0].Access.Verdict != "ok" || list[1].Access != nil {
		t.Fatalf("%+v", list)
	}
	if _, e := c.call("provider.check", map[string]any{"provider": "gemini"}); !strings.HasPrefix(e, "unsupported: ") {
		t.Errorf("%q", e)
	}
	if _, e := c.call("provider.check", map[string]any{"provider": "claude", "proxyUrl": "nonsense"}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("%q", e)
	}
}

func TestHubRuntimeSession(t *testing.T) {
	r := startHubAgent(t, rigOptions{noSecretYet: true})
	c := r.connect()
	// The runtime has not written its secret: the agent is up and says so.
	if _, e := c.call("runtime.session", map[string]any{"subject": "a@example.com"}); !strings.HasPrefix(e, "failed: ") || !strings.Contains(e, "secret") {
		t.Fatalf("%q", e)
	}
	writeFile(t, r.secret, hubTestSecret)
	grant := decode[hubproto.SessionGrant](t, c.mustCall("runtime.session", map[string]any{"subject": "a@example.com", "name": "Anna"}))
	if grant.CookieName != "session-token" || grant.Token == "" {
		t.Fatalf("%+v", grant)
	}
	if err := VerifySession([]byte(hubTestSecret), grant.Token, time.Now()); err != nil {
		t.Fatalf("the runtime would not take the token: %v", err)
	}
	if sub, ok := SessionSubject([]byte(hubTestSecret), grant.Token, time.Now()); !ok || sub != "a@example.com" {
		t.Errorf("subject %q %v", sub, ok)
	}
	if left := time.Until(time.UnixMilli(grant.ExpiresAt)); left < 11*time.Hour+50*time.Minute || left > 12*time.Hour+time.Minute {
		t.Errorf("expires in %v", left)
	}
	if exp, ok := SessionExpiry([]byte(hubTestSecret), grant.Token, time.Now()); !ok || time.Until(time.Unix(int64(exp), 0)) < 11*time.Hour {
		t.Errorf("%v %v", exp, ok)
	}
	for _, subject := range []string{"", "  ", "a\nb@example.com", strings.Repeat("x", 300)} {
		if _, e := c.call("runtime.session", map[string]any{"subject": subject}); !strings.HasPrefix(e, "invalid: ") {
			t.Errorf("%q: %q", subject, e)
		}
	}
	if !strings.Contains(r.audit(), `"op":"runtime.session"`) || strings.Contains(r.audit(), grant.Token) {
		t.Errorf("audit %s", r.audit())
	}
}

func TestHubPolicyRefusesAndTellsTheHub(t *testing.T) {
	r := startHubAgent(t, rigOptions{policy: "keys = false\nenv = false\ninstall = false\nagent_login = false\nruntime_session = false\nterminal = false\n"})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if hello.Policy.Keys || hello.Policy.Env || hello.Policy.Terminal || hello.Policy.Source != filepath.Join(r.rig.Home, "policy.toml") {
		t.Fatalf("%+v", hello.Policy)
	}
	m := r.machine
	for op, params := range map[string]map[string]any{
		"key.deliver":     {"provider": "claude", "envelope": seal(t, m, "key:claude", "sk-x")},
		"key.verify":      {"provider": "claude"},
		"env.deliver":     {"name": "A_TOKEN", "envelope": seal(t, m, "env:A_TOKEN", "v")},
		"env.remove":      {"name": "A_TOKEN"},
		"gitkey.ensure":   nil,
		"proxy.set":       {"scope": "all", "url": "http://a:1"},
		"proxy.clear":     {"scope": "all"},
		"agents.install":  {"jobId": strings.Repeat("a", 32), "provider": "claude"},
		"login.start":     {"loginId": "l", "provider": "claude", "method": "link"},
		"login.code":      {"loginId": "l", "code": "c"},
		"login.cancel":    {"loginId": "l"},
		"runtime.session": {"subject": "a@example.com"},
		"tunnel.open":     {"spec": map[string]string{"tunnelId": strings.Repeat("a", 32), "target": "runtime"}},
	} {
		if _, e := c.call(op, params); !strings.HasPrefix(e, "policy_denied: ") {
			t.Errorf("%s: %q", op, e)
		}
	}
	// Reads and checks stay open.
	c.mustCall("proxy.get", nil)
	c.mustCall("agents.status", nil)
	if _, err := os.Stat(filepath.Join(r.rig.Home, ".ssh")); !os.IsNotExist(err) {
		t.Error("the git key was made against the policy")
	}
	if got := strings.Count(r.audit(), `"op":"policy.denied"`); got < 10 {
		t.Errorf("%d denials in the audit log", got)
	}

	// Editing the file applies at once and the hub is told.
	_, seen := c.waitEvent("policy", 0, 100*time.Millisecond)
	writeFile(t, filepath.Join(r.rig.Home, "policy.toml"), "terminal = false\ndeny_routes = [\"/api/fs/write\"]\n")
	r.agent.ReloadPolicy()
	ev, _ := c.waitEvent("policy", seen, 10*time.Second)
	if ev == nil {
		t.Fatal("no policy event")
	}
	if p := decode[hubproto.Policy](t, ev["policy"]); !p.Keys || p.Terminal || len(p.DenyRoutes) != 1 {
		t.Errorf("%+v", p)
	}
	pub := decode[map[string]string](t, c.mustCall("gitkey.ensure", nil))["publicKey"]
	if pub == "" {
		t.Error("no key after the policy opened")
	}
	// A console login is a terminal tab: with the terminal off it is denied while the link login is not.
	if _, e := c.call("login.start", map[string]any{"loginId": "k", "provider": "claude", "method": "console"}); !strings.HasPrefix(e, "policy_denied: ") {
		t.Errorf("console login without the terminal: %q", e)
	}
}

// httpOverTunnel sends a request through an open tunnel and returns what came back.
func httpOverTunnel(t *testing.T, c *hubConn, id, request string) string {
	t.Helper()
	if err := c.conn.Write(c.ctx, websocket.MessageBinary, EncodeFrame(FrameData, id, []byte(request))); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		frames := c.binary
		c.binary = nil
		c.mu.Unlock()
		for _, f := range frames {
			kind, fid, payload, ok := DecodeFrame(f)
			if ok && fid == id && kind == FrameData {
				out.Write(payload)
			}
		}
		if s := out.String(); strings.Contains(s, "\r\n\r\n") && (strings.HasSuffix(s, "}\n") || strings.Contains(s, "Content-Length: 0")) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no response through the tunnel: %q", out.String())
	return ""
}

func TestHubTunnelGoesThroughTheFilter(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	tid := strings.Repeat("ab", 16)
	c.mustCall("tunnel.open", map[string]any{"spec": map[string]string{"tunnelId": tid, "target": "runtime"}})
	req := func(path string) string {
		return "GET " + path + " HTTP/1.1\r\nHost: hub.example.com\r\nX-Forwarded-Proto: https\r\nConnection: keep-alive\r\n\r\n"
	}
	got := httpOverTunnel(t, c, tid, req("/m/abcdefghijklmnopqrst/api/status"))
	if !strings.HasPrefix(got, "HTTP/1.1 200") || !strings.Contains(got, `"host":"hub.example.com"`) {
		t.Fatalf("%q", got)
	}
	if h := r.runtime.last.Load(); h == nil || h.Header.Get("X-Forwarded-Proto") != "https" {
		t.Errorf("the runtime saw %v", h)
	}

	// The policy narrows while the tunnel is open: the next request is refused, and the open tunnels are closed.
	writeFile(t, filepath.Join(r.rig.Home, "policy.toml"), "terminal = false\n")
	r.agent.ReloadPolicy()
	deadline := time.Now().Add(10 * time.Second)
	closed := false
	for time.Now().Before(deadline) && !closed {
		c.mu.Lock()
		for _, f := range c.binary {
			if kind, fid, _, ok := DecodeFrame(f); ok && fid == tid && kind == FrameClose {
				closed = true
			}
		}
		c.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if !closed {
		t.Fatal("the tunnel outlived the narrower policy")
	}
	tid2 := strings.Repeat("cd", 16)
	c.mustCall("tunnel.open", map[string]any{"spec": map[string]string{"tunnelId": tid2, "target": "runtime"}})
	denied := httpOverTunnel(t, c, tid2, req("/m/abcdefghijklmnopqrst/api/terminal"))
	if !strings.HasPrefix(denied, "HTTP/1.1 403") || !strings.Contains(denied, "machine_policy") {
		t.Fatalf("%q", denied)
	}
	if !strings.Contains(r.audit(), `"op":"route.denied"`) {
		t.Errorf("audit %s", r.audit())
	}

	// Only the runtime is a hub's to reach.
	if _, e := c.call("tunnel.open", map[string]any{"spec": map[string]string{"tunnelId": strings.Repeat("ef", 16), "target": "files"}}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("files: %q", e)
	}
}

func TestHubKillSwitch(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	c.mustCall("hello", nil)
	_, seen := c.waitEvent("policy", 0, 50*time.Millisecond)
	off := filepath.Join(r.rig.Home, ".harnessly", "disabled")
	writeFile(t, off, "")
	r.agent.ReloadPolicy()
	if ev, _ := c.waitEvent("policy", seen, 10*time.Second); ev == nil {
		t.Error("the hub was not told")
	}
	if err := c.closeError(); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("the link did not close cleanly: %v", err)
	}
	if st := r.waitLinkState(func(s LinkState) bool { return !s.Linked && s.Note != "" }); st.Note != "switched off on this machine" {
		t.Errorf("%+v", st)
	}
	// Off, it does not call again.
	before := r.hub.dials.Load()
	time.Sleep(2500 * time.Millisecond)
	if after := r.hub.dials.Load(); after != before {
		t.Fatalf("dialled %d more times while switched off", after-before)
	}
	if !strings.Contains(r.audit(), `"op":"killswitch"`) {
		t.Errorf("audit %s", r.audit())
	}
	// On again, it does.
	if err := os.Remove(off); err != nil {
		t.Fatal(err)
	}
	r.agent.ReloadPolicy()
	c2 := r.connect()
	c2.mustCall("hello", nil)
	r.waitLinkState(func(s LinkState) bool { return s.Linked })
}

func TestHubDialRulesOfTheHub(t *testing.T) {
	// 4011: replaced by a newer link; the agent calls again at once.
	t.Run("replaced", func(t *testing.T) {
		r := startHubAgent(t, rigOptions{})
		c := r.connect()
		_ = c.conn.Close(websocket.StatusCode(hubproto.CloseReplaced), "replaced")
		c2 := r.connect()
		c2.mustCall("hello", nil)
	})
	// 4010 and a 403: revoked; no more calls.
	t.Run("revoked by close code", func(t *testing.T) {
		r := startHubAgent(t, rigOptions{})
		c := r.connect()
		_ = c.conn.Close(websocket.StatusCode(hubproto.CloseRevoked), "revoked")
		waitStops(t, r)
		if !strings.Contains(r.audit(), `"op":"revoked"`) {
			t.Errorf("audit %s", r.audit())
		}
	})
	t.Run("revoked by 403", func(t *testing.T) {
		r := startHubAgentRejected(t, 403)
		waitStops(t, r)
	})
	// 4012 and 426: too old; an hour's wait.
	t.Run("too old by close code", func(t *testing.T) {
		r := startHubAgent(t, rigOptions{})
		c := r.connect()
		_ = c.conn.Close(websocket.StatusCode(hubproto.CloseProtocolOld), "too old")
		waitStops(t, r)
	})
	t.Run("too old by 426", func(t *testing.T) {
		r := startHubAgentRejected(t, 426)
		waitStops(t, r)
	})
	// A signature the hub does not take: a long pause, not a storm.
	t.Run("not accepted", func(t *testing.T) {
		r := startHubAgentRejected(t, 401)
		waitStops(t, r)
	})
	// Switched off by the hub (4003): a minute.
	t.Run("switched off at the hub", func(t *testing.T) {
		r := startHubAgent(t, rigOptions{})
		c := r.connect()
		_ = c.conn.Close(websocket.StatusCode(hubproto.CloseSwitchedOff), "off")
		waitStops(t, r)
	})
}

func startHubAgentRejected(t *testing.T, status int32) *hubRig {
	t.Helper()
	// Registered normally, but the hub's door answers with the status.
	return startHubAgent(t, rigOptions{before: func(h *fakeHub) { h.reject.Store(status) }})
}

// waitStops: after the first call the agent makes no second one for a few seconds.
func waitStops(t *testing.T, r *hubRig) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for r.hub.dials.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	n := r.hub.dials.Load()
	if n != 1 {
		t.Fatalf("%d dials", n)
	}
	time.Sleep(3 * time.Second)
	if got := r.hub.dials.Load(); got != n {
		t.Fatalf("dialled again: %d calls", got)
	}
}

func TestHubInstallAndConsoleLoginThroughTheLink(t *testing.T) {
	var gotCommand atomic.Pointer[string]
	r := startHubAgent(t, rigOptions{runtimeRoute: func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path != "/m/abcdefghijklmnopqrst/api/agent/terminal-tab" {
			return false
		}
		cookie, _ := req.Cookie("session-token")
		if cookie == nil || VerifySession([]byte(hubTestSecret), cookie.Value, time.Now()) != nil {
			w.WriteHeader(401)
			return true
		}
		var body struct{ Command string }
		_ = json.NewDecoder(req.Body).Decode(&body)
		gotCommand.Store(&body.Command)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tabId":"t7","url":"/?tab=t7"}`))
		return true
	}})
	c := r.connect()
	c.mustCall("link.subscribe", map[string]any{"events": []string{"job", "login", "agents"}})

	// An install is answered at once and followed by job events until it ends.
	r.rig.Script("curl", `
out=""
while [ $# -gt 0 ]; do if [ "$1" = "-o" ]; then out="$2"; shift; fi; shift; done
cat > "$out" <<'INSTALLER'
mkdir -p "$HOME/.local/bin"
printf '#!/bin/sh\necho "2.1.310 (Claude Code)"\n' > "$HOME/.local/bin/claude"
chmod +x "$HOME/.local/bin/claude"
INSTALLER`)
	r.rig.Link("bash")
	job := strings.Repeat("0f", 16)
	res := decode[map[string]string](t, c.mustCall("agents.install", map[string]any{"jobId": job, "provider": "claude", "from": "provider"}))
	if res["jobId"] != job {
		t.Fatalf("%v", res)
	}
	if _, e := c.call("agents.install", map[string]any{"jobId": strings.Repeat("1a", 16), "provider": "claude"}); !strings.HasPrefix(e, "busy: ") && e != "" {
		t.Errorf("a second install: %q", e)
	}
	if _, e := c.call("agents.install", map[string]any{"jobId": "short", "provider": "claude"}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("a bad job id: %q", e)
	}
	if _, e := c.call("agents.install", map[string]any{"jobId": strings.Repeat("2b", 16), "provider": "gemini"}); !strings.HasPrefix(e, "unsupported: ") {
		t.Errorf("gemini: %q", e)
	}
	var last hubproto.Job
	seen := 0
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && last.State != "done" && last.State != "failed" {
		var ev map[string]json.RawMessage
		if ev, seen = c.waitEvent("job", seen, 200*time.Millisecond); ev != nil {
			last = decode[hubproto.Job](t, ev["job"])
		}
	}
	if last.ID != job || last.State != "done" {
		t.Fatalf("the job ended as %+v", last)
	}
	// And the hub is told the agent is there now.
	var agentsEv map[string]json.RawMessage
	for i, n := 0, 0; i < 100 && agentsEv == nil; i++ {
		agentsEv, n = c.waitEvent("agents", n, 300*time.Millisecond)
		if agentsEv != nil {
			list := decode[[]hubproto.AgentStatus](t, agentsEv["agents"])
			if !list[0].Installed {
				agentsEv = nil
			}
		}
	}
	if agentsEv == nil {
		t.Error("no agents event with Claude installed")
	}

	// A login in the console: the runtime is asked for a tab and the hub is told where it is.
	login := decode[hubproto.Login](t, c.mustCall("login.start", map[string]any{"loginId": "k1", "provider": "claude", "method": "console"}))
	if login.State != "starting" || login.ID != "k1" || login.Method != "console" {
		t.Fatalf("%+v", login)
	}
	if _, e := c.call("login.start", map[string]any{"loginId": "k2", "provider": "codex", "method": "console"}); !strings.HasPrefix(e, "busy: ") {
		t.Errorf("a second login: %q", e)
	}
	var waiting hubproto.Login
	for i, n := 0, 0; i < 50 && waiting.State != "waiting"; i++ {
		var ev map[string]json.RawMessage
		if ev, n = c.waitEvent("login", n, 300*time.Millisecond); ev != nil {
			waiting = decode[hubproto.Login](t, ev["login"])
		}
	}
	if waiting.State != "waiting" || waiting.ConsoleURL == nil || *waiting.ConsoleURL != "/m/abcdefghijklmnopqrst/?tab=t7" {
		t.Fatalf("%+v", waiting)
	}
	if cmd := gotCommand.Load(); cmd == nil || !strings.Contains(*cmd, "exec env claude auth login --claudeai") {
		t.Errorf("the tab was asked to run %v", cmd)
	}
	if _, e := c.call("login.cancel", map[string]any{"loginId": "nope"}); !strings.HasPrefix(e, "not_found: ") {
		t.Errorf("%q", e)
	}
	c.mustCall("login.cancel", map[string]any{"loginId": "k1"})
	var cancelled hubproto.Login
	for i, n := 0, 0; i < 50 && cancelled.State != "cancelled"; i++ {
		var ev map[string]json.RawMessage
		if ev, n = c.waitEvent("login", n, 300*time.Millisecond); ev != nil {
			cancelled = decode[hubproto.Login](t, ev["login"])
		}
	}
	if cancelled.State != "cancelled" {
		t.Errorf("%+v", cancelled)
	}
	audit := r.audit()
	for _, want := range []string{`"op":"agents.install"`, `"op":"login.start"`, `"op":"login.cancel"`} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit lacks %s", want)
		}
	}
}

func TestHubPreflightRunAnswers(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	rep := decode[hubproto.PreflightReport](t, c.mustCall("preflight.run", nil))
	if rep.OS == "" || rep.CPUs == 0 || len(rep.Checks) == 0 || rep.Disk.Total == 0 {
		t.Fatalf("%+v", rep)
	}
}

// Hub mode runs none of the closed cluster's loops: no transcripts are read.
func TestHubModeReadsNoTranscripts(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	writeFile(t, filepath.Join(r.rig.Home, ".claude", "projects", "p", "s.jsonl"), "{\"type\":\"user\"}\n")
	time.Sleep(500 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ev := range c.events {
		if strings.Contains(string(ev["event"]), "transcripts") {
			t.Errorf("a transcript event on a hub link: %s", ev)
		}
	}
}

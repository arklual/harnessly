package appmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// agent is the client's end of the proxy: what it writes to the proxy's stdin and reads from its stdout.
type agent struct {
	t     *testing.T
	in    *io.PipeWriter
	lines chan string
	done  chan error
}

func start(t *testing.T, p *Proxy) *agent {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	p.In, p.Out = inR, outW
	if p.Retry == 0 {
		p.Retry = 10 * time.Millisecond
	}
	if p.MaxPause == 0 {
		p.MaxPause = 40 * time.Millisecond
	}
	a := &agent{t: t, in: inW, lines: make(chan string, 64), done: make(chan error, 1)}
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			a.lines <- sc.Text()
		}
	}()
	go func() { a.done <- p.Run(context.Background()) }()
	t.Cleanup(func() {
		_ = inW.Close()
		select {
		case err := <-a.done:
			if err != nil {
				t.Errorf("the proxy ended with %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("the proxy did not end when its input closed")
		}
		_ = outW.Close()
	})
	return a
}

func (a *agent) send(line string) {
	a.t.Helper()
	if _, err := io.WriteString(a.in, line+"\n"); err != nil {
		a.t.Fatal(err)
	}
}

func (a *agent) next() map[string]any {
	a.t.Helper()
	select {
	case line := <-a.lines:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			a.t.Fatalf("not one JSON line: %q", line)
		}
		return m
	case <-time.After(5 * time.Second):
		a.t.Fatal("no line from the proxy")
		return nil
	}
}

func (a *agent) quiet(d time.Duration) {
	a.t.Helper()
	select {
	case line := <-a.lines:
		a.t.Fatalf("unexpected line: %s", line)
	case <-time.After(d):
	}
}

// runtime is a stand-in for the runtime's endpoint: it answers POSTs and, on GET, does what stream says.
type runtime struct {
	mu     sync.Mutex
	posts  []string
	gets   atomic.Int32
	stop   chan struct{}
	down   func() // stops the server, as the runtime's exit does
	stream func(n int32, w http.ResponseWriter, r *http.Request)
}

func (rt *runtime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-token") != "tok" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"Forbidden"}`)
		return
	}
	if r.Method == http.MethodGet {
		n := rt.gets.Add(1)
		if rt.stream == nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		rt.stream(n, w, r)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
	}
	_ = json.Unmarshal(raw, &req)
	rt.mu.Lock()
	rt.posts = append(rt.posts, req.Method)
	rt.mu.Unlock()
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// Indented on purpose: the proxy must put it on one line.
	_ = json.NewEncoder(indent{w}).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"echo": req.Method}})
}

// hold keeps a stream open until the client leaves or the test ends.
func (rt *runtime) hold(r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-rt.stop:
	}
}

// serve starts rt on l (nil: any port); the streams it holds end with the test, so the server can close.
func serve(t *testing.T, rt *runtime, l net.Listener) *httptest.Server {
	t.Helper()
	rt.stop = make(chan struct{})
	srv := httptest.NewUnstartedServer(rt)
	if l != nil {
		srv.Listener = l
	}
	srv.Start()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(rt.stop)
			srv.CloseClientConnections()
			srv.Close()
		})
	}
	rt.down = stop
	t.Cleanup(stop)
	return srv
}

type indent struct{ w io.Writer }

func (i indent) Write(p []byte) (int, error) {
	_, err := i.w.Write([]byte(strings.ReplaceAll(string(p), ",", ",\n  ")))
	return len(p), err
}

func openStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": ok\n\n")
	w.(http.Flusher).Flush()
}

func proxyTo(url string) *Proxy {
	return &Proxy{URL: url, Headers: map[string]string{"x-token": "tok"}}
}

const initialized = `{"jsonrpc":"2.0","method":"notifications/initialized"}`

func TestRequestsPassWithTheToken(t *testing.T) {
	rt := &runtime{}
	srv := serve(t, rt, nil)
	a := start(t, proxyTo(srv.URL+"/mcp?server=harnessly-app"))
	a.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	got := a.next()
	if got["id"] != float64(1) || got["result"].(map[string]any)["echo"] != "initialize" {
		t.Fatalf("answer: %v", got)
	}
	// A notification has no answer.
	a.send(initialized)
	a.send(`{"jsonrpc":"2.0","id":"x","method":"tools/list"}`)
	if got := a.next(); got["id"] != "x" {
		t.Fatalf("answer: %v", got)
	}
}

// The runtime's own refusal (not JSON-RPC) comes to the agent as an error in words, not as a line it cannot read.
func TestGateRefusalIsAnError(t *testing.T) {
	srv := serve(t, &runtime{}, nil)
	p := proxyTo(srv.URL)
	p.Headers = map[string]string{"x-token": "stale"}
	a := start(t, p)
	a.send(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	got := a.next()
	if e, _ := got["error"].(map[string]any); e == nil || !strings.Contains(e["message"].(string), "403") {
		t.Fatalf("answer: %v", got)
	}
}

// The stream ends when the runtime restarts. The proxy opens it again and tells the client to ask tools/list anew;
// what the runtime says on the new stream goes on reaching the client.
func TestStreamReopensAndSaysListChanged(t *testing.T) {
	rt := &runtime{}
	rt.stream = func(n int32, w http.ResponseWriter, r *http.Request) {
		openStream(w)
		if n == 1 {
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\",\"params\":{\"n\":1}}\n\n")
			w.(http.Flusher).Flush()
			return // the stream breaks
		}
		if n == 2 {
			_, _ = io.WriteString(w, ": ping\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\",\"params\":{\"n\":2}}\n\n")
			w.(http.Flusher).Flush()
		}
		rt.hold(r)
	}
	srv := serve(t, rt, nil)
	a := start(t, proxyTo(srv.URL))
	a.quiet(50 * time.Millisecond) // no stream before the client is ready
	if rt.gets.Load() != 0 {
		t.Fatal("the stream opened before notifications/initialized")
	}
	a.send(initialized)
	if got := a.next(); got["params"].(map[string]any)["n"] != float64(1) {
		t.Fatalf("the runtime's own notification: %v", got)
	}
	// Reopened: the proxy's own list_changed (no params), then the runtime's.
	if got := a.next(); got["method"] != "notifications/tools/list_changed" || got["params"] != nil {
		t.Fatalf("after the stream reopened: %v", got)
	}
	if got := a.next(); got["params"].(map[string]any)["n"] != float64(2) {
		t.Fatalf("the new stream's notification: %v", got)
	}
}

// A runtime that just started answers 405 to the stream (it cannot place the caller yet). An HTTP client stops asking
// there; the proxy asks again, and says list_changed once the stream opens.
func TestStreamRefusedAtStartIsAskedAgain(t *testing.T) {
	rt := &runtime{}
	rt.stream = func(n int32, w http.ResponseWriter, r *http.Request) {
		if n < 3 {
			w.Header().Set("Allow", "POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		openStream(w)
		rt.hold(r)
	}
	srv := serve(t, rt, nil)
	a := start(t, proxyTo(srv.URL))
	a.send(initialized)
	if got := a.next(); got["method"] != "notifications/tools/list_changed" {
		t.Fatalf("after the refused stream opened: %v", got)
	}
	if n := rt.gets.Load(); n != 3 {
		t.Fatalf("stream requests: %d", n)
	}
}

// A stream that opens at the first try says nothing of its own: the client has just listed the tools.
func TestFirstStreamIsQuiet(t *testing.T) {
	rt := &runtime{}
	rt.stream = func(_ int32, w http.ResponseWriter, r *http.Request) {
		openStream(w)
		rt.hold(r)
	}
	srv := serve(t, rt, nil)
	a := start(t, proxyTo(srv.URL))
	a.send(initialized)
	a.quiet(100 * time.Millisecond)
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// The runtime restarts: the stream breaks, a call made meanwhile waits and gets the new runtime's answer, and the
// stream, opened again, says list_changed.
func TestRuntimeRestart(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	stream := func(_ int32, w http.ResponseWriter, r *http.Request) { openStream(w) }
	before := &runtime{}
	before.stream = func(n int32, w http.ResponseWriter, r *http.Request) { stream(n, w, r); before.hold(r) }
	serve(t, before, l)
	p := proxyTo("http://" + addr + "/mcp")
	p.Wait = 5 * time.Second
	a := start(t, p)
	a.send(initialized)
	for i := 0; before.gets.Load() == 0; i++ {
		if i > 500 {
			t.Fatal("the stream did not open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	before.down()
	a.send(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"x"}}`)
	a.quiet(80 * time.Millisecond)
	if l, err = net.Listen("tcp", addr); err != nil {
		t.Skip("the port was taken meanwhile:", err)
	}
	after := &runtime{}
	after.stream = func(n int32, w http.ResponseWriter, r *http.Request) { stream(n, w, r); after.hold(r) }
	serve(t, after, l)
	var answered, told bool
	for i := 0; i < 2; i++ {
		got := a.next()
		switch {
		case got["id"] == float64(7) && got["result"].(map[string]any)["echo"] == "tools/call":
			answered = true
		case got["method"] == "notifications/tools/list_changed":
			told = true
		default:
			t.Fatalf("unexpected: %v", got)
		}
	}
	if !answered || !told {
		t.Fatalf("answered %v, told %v", answered, told)
	}
	after.mu.Lock()
	defer after.mu.Unlock()
	if strings.Join(after.posts, ",") != "tools/call" {
		t.Fatalf("posts of the new runtime: %v", after.posts)
	}
}

// With the runtime away for longer than the wait, a tool call ends as the tool's error in words, any other request as
// a JSON-RPC error; the proxy itself stays.
func TestRuntimeAwayIsSaidInWords(t *testing.T) {
	p := proxyTo("http://" + freeAddr(t) + "/mcp")
	p.Wait = 120 * time.Millisecond
	a := start(t, p)
	a.send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`)
	got := a.next()
	res, _ := got["result"].(map[string]any)
	if res == nil || res["isError"] != true {
		t.Fatalf("answer: %v", got)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "did not answer") || !strings.Contains(text, "repeat it") {
		t.Fatalf("words: %s", text)
	}
	a.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	got = a.next()
	if e, _ := got["error"].(map[string]any); e == nil || e["code"] != float64(rpcServerError) || got["id"] != float64(2) {
		t.Fatalf("answer: %v", got)
	}
	// A notification that could not be passed has no answer.
	a.send(`{"jsonrpc":"2.0","method":"notifications/cancelled"}`)
	a.quiet(200 * time.Millisecond)
}

// dropper is a runtime that takes the first drops tool calls and ends their connections with no answer, as a runtime
// that restarts under a call does; keys says whether it knows the calls' keys.
type dropper struct {
	mu     sync.Mutex
	drops  int
	keys   bool
	calls  []http.Header
	probes int
}

func (d *dropper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
	}
	_ = json.Unmarshal(raw, &req)
	d.mu.Lock()
	drop := false
	if req.Method == "tools/call" {
		d.calls = append(d.calls, r.Header.Clone())
		drop = len(d.calls) <= d.drops
	} else {
		d.probes++
	}
	keys := d.keys
	d.mu.Unlock()
	if drop {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
		return
	}
	if keys {
		w.Header().Set(KeysHeader, "1")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"echo": req.Method}})
}

const toolCall = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`

// A call the runtime took and did not answer (it restarted meanwhile) is made again with the same key, marked as a
// repeat, once the runtime that is back says it knows the keys: the runtime finds the command and runs nothing twice.
func TestLostCallIsRepeatedWithItsKey(t *testing.T) {
	d := &dropper{drops: 1, keys: true}
	srv := httptest.NewServer(d)
	defer srv.Close()
	p := proxyTo(srv.URL)
	p.Wait = 2 * time.Second
	a := start(t, p)
	a.send(toolCall)
	got := a.next()
	if res, _ := got["result"].(map[string]any); res == nil || res["echo"] != "tools/call" {
		t.Fatalf("answer: %v", got)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) != 2 || d.probes != 1 {
		t.Fatalf("calls %d, probes %d", len(d.calls), d.probes)
	}
	key := d.calls[0].Get(KeyHeader)
	if len(key) != 32 || d.calls[1].Get(KeyHeader) != key || d.calls[0].Get(RepeatHeader) != "" || d.calls[1].Get(RepeatHeader) != "1" {
		t.Fatalf("headers: %v / %v", d.calls[0], d.calls[1])
	}
}

// Two calls have two keys.
func TestEveryCallHasItsOwnKey(t *testing.T) {
	d := &dropper{keys: true}
	srv := httptest.NewServer(d)
	defer srv.Close()
	a := start(t, proxyTo(srv.URL))
	a.send(toolCall)
	a.next()
	a.send(toolCall)
	a.next()
	a.send(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	a.next()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) != 2 || d.calls[0].Get(KeyHeader) == d.calls[1].Get(KeyHeader) || d.probes != 1 {
		t.Fatalf("calls: %v, others %d", d.calls, d.probes)
	}
}

// A runtime that does not know the keys (an older one) is not asked twice: the agent is told the call's result is
// unknown. So is it when the runtime does not come back within the wait.
func TestLostCallIsNotRepeatedWithoutKeys(t *testing.T) {
	for _, d := range []*dropper{{drops: 1, keys: false}, {drops: 1000, keys: true}} {
		srv := httptest.NewServer(d)
		p := proxyTo(srv.URL)
		p.Wait = 300 * time.Millisecond
		a := start(t, p)
		a.send(toolCall)
		got := a.next()
		res, _ := got["result"].(map[string]any)
		if res == nil || res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "may or may not have been done") {
			t.Fatalf("answer: %v", got)
		}
		d.mu.Lock()
		if !d.keys && len(d.calls) != 1 {
			t.Fatalf("the call was sent %d times to a runtime that knows no keys", len(d.calls))
		}
		for _, h := range d.calls[1:] {
			if h.Get(RepeatHeader) != "1" {
				t.Fatalf("a repeat without its mark: %v", h)
			}
		}
		d.mu.Unlock()
		srv.Close()
	}
}

func TestFromEnv(t *testing.T) {
	env := map[string]string{EnvURL: "http://127.0.0.1:1/mcp", EnvHeaders: `{"x-token":"tok"}`}
	p, err := FromEnv(func(k string) string { return env[k] }, nil, nil)
	if err != nil || p.URL != env[EnvURL] || p.Headers["x-token"] != "tok" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := FromEnv(func(string) string { return "" }, nil, nil); err == nil {
		t.Fatal("no address must be an error")
	}
}

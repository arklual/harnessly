package gate

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func newGate(t *testing.T, upstream string, routes ...Route) (*Gate, string) {
	t.Helper()
	dir := t.TempDir()
	f, err := New(Config{Listen: "127.0.0.1:0", Upstream: upstream, DataDir: dir, CountEvery: time.Hour}, routes...)
	if err != nil {
		t.Fatal(err)
	}
	return f, dir
}

func TestProxiesRequestsKeepingTheHost(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Set-Cookie", "a=b")
		w.WriteHeader(201)
		fmt.Fprintf(w, "%s %s host=%s cookie=%s body=%s", r.Method, r.URL.RequestURI(), r.Host, r.Header.Get("Cookie"), body)
	}))
	defer old.Close()
	f, _ := newGate(t, strings.TrimPrefix(old.URL, "http://"))
	srv := httptest.NewServer(f)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/agents/api/x?y=1&c=/a;b&d=%zz", strings.NewReader("hi"))
	req.Host = "slot-1.preview.example"
	req.Header.Set("Cookie", "session-token=t")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	want := "POST /agents/api/x?y=1&c=/a;b&d=%zz host=slot-1.preview.example cookie=session-token=t body=hi"
	if res.StatusCode != 201 || string(body) != want || res.Header.Get("Set-Cookie") != "a=b" {
		t.Fatalf("%d %q %v", res.StatusCode, body, res.Header)
	}
}

func TestStreamsAsTheyAreWritten(t *testing.T) {
	release := make(chan struct{})
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "data: two\n\n")
	}))
	defer old.Close()
	defer close(release)
	f, _ := newGate(t, strings.TrimPrefix(old.URL, "http://"))
	srv := httptest.NewServer(f)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(res.Body).ReadString('\n')
		line <- l
	}()
	select {
	case l := <-line:
		if l != "data: one\n" {
			t.Fatalf("first event %q", l)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first event was held back until the stream ended")
	}
}

func TestCarriesWebSockets(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			_ = c.Write(context.Background(), typ, append([]byte("echo:"), data...))
		}
	}))
	defer old.Close()
	f, _ := newGate(t, strings.TrimPrefix(old.URL, "http://"))
	srv := httptest.NewServer(f)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	for _, msg := range []string{"a", string([]byte{0, 1, 2})} {
		if err := c.Write(ctx, websocket.MessageBinary, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		_, got, err := c.Read(ctx)
		if err != nil || string(got) != "echo:"+msg {
			t.Fatalf("%q %v", got, err)
		}
	}
}

func TestADownRuntimeLooksDown(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // nothing listens there now
	f, _ := newGate(t, addr)
	srv := httptest.NewServer(f)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/agents/api/health")
	if err == nil {
		t.Fatalf("a down runtime answered %d instead of dropping the connection", res.StatusCode)
	}
}

func TestSliceAnswersOnlyWhileOn(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "old") }))
	defer old.Close()
	route := Route{
		Slice:   "health",
		Match:   func(r *http.Request) bool { return r.URL.Path == "/agents/api/health" },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "go") }),
	}
	f, dir := newGate(t, strings.TrimPrefix(old.URL, "http://"), route)
	srv := httptest.NewServer(f)
	defer srv.Close()
	get := func() string {
		res, err := http.Get(srv.URL + "/agents/api/health")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	if got := get(); got != "old" {
		t.Fatalf("slice off: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"owned":true,"headless":true,"goSlices":["health"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.slices.at = time.Time{} // skip the one-second cache
	if got := get(); got != "go" {
		t.Fatalf("slice on: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.slices.at = time.Time{}
	if got := get(); got != "old" {
		t.Fatalf("slice off again: %q", got)
	}
}

func TestPathKey(t *testing.T) {
	for in, want := range map[string]string{
		"/agents/api/health":                              "/api/health",
		"/agents/api/layout/pane/abc123def456/tabs":       "/api/layout/pane/*/tabs",
		"/api/platform/subagents/0b1f7c9e-2a3d-4f5e-8a6b": "/api/platform/subagents/*",
		"/api/a/b/c/d/e/f":                                "/api/a/b/c/d/…",
	} {
		if got := pathKey(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// Standalone: every slice on but the shadows, whatever state-mode.json says, and what no route takes is the fallback's.
func TestStandaloneGate(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":[]}`), 0o600)
	t.Setenv("RUNTIME_GATE_LISTEN", "127.0.0.1:0")
	t.Setenv("RUNTIME_GATE_UPSTREAM", "")
	cfg, err := LoadConfig()
	if err != nil || !cfg.Standalone() {
		t.Fatalf("%+v %v", cfg, err)
	}
	cfg.DataDir = dir
	answer := func(body string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
	}
	f, err := New(cfg,
		Route{Slice: "x", Match: func(r *http.Request) bool { return r.URL.Path == "/x" }, Handler: answer("x")},
		Route{Slice: "x-shadow", Match: func(r *http.Request) bool { return r.URL.Path == "/s" }, Handler: answer("shadow")})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Standalone() || !f.SliceOn("anything") || f.SliceOn("x-shadow") {
		t.Fatal("slices")
	}
	get := func(p string) (int, string) {
		rec := httptest.NewRecorder()
		f.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec.Code, rec.Body.String()
	}
	if code, body := get("/x"); code != 200 || body != "x" {
		t.Fatalf("%d %s", code, body)
	}
	if code, body := get("/s"); code != 404 || body != `{"error":"Not found"}` {
		t.Fatalf("a shadow does not run alone: %d %s", code, body)
	}
	f.SetFallback(answer("fallback"))
	if code, body := get("/elsewhere"); code != 200 || body != "fallback" {
		t.Fatalf("%d %s", code, body)
	}
	rec := httptest.NewRecorder()
	f.Proxy(rec, httptest.NewRequest("GET", "/y", nil))
	if rec.Body.String() != "fallback" {
		t.Fatal("Proxy goes to the fallback")
	}
}

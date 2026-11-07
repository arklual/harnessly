package ext

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testProto = "harnessly.test/v1"

func describeServer(t *testing.T, proto string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/describe", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Description{Protocol: proto, Name: "t", Version: "1.2", Capabilities: []string{"a"}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDescribeChecksProtocol(t *testing.T) {
	c, err := NewClient(Config{BaseURL: describeServer(t, testProto).URL})
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.Describe(context.Background(), testProto)
	if err != nil || !d.Has("a") || d.Has("b") {
		t.Fatalf("describe: %+v %v", d, err)
	}
	_, err = c.Describe(context.Background(), "harnessly.test/v2")
	if e, ok := err.(*Error); !ok || e.Code != "protocol_unsupported" {
		t.Fatalf("another protocol: %v", err)
	}
}

func TestRetriesKeepTheIdempotencyKey(t *testing.T) {
	var n atomic.Int32
	keys := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys <- r.Header.Get("Idempotency-Key")
		if n.Add(1) < 3 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{BaseURL: srv.URL, Backoff: time.Millisecond})
	if err := c.Do(context.Background(), http.MethodPost, "/x", struct{}{}, true, nil); err != nil {
		t.Fatal(err)
	}
	a, b, d := <-keys, <-keys, <-keys
	if a == "" || a != b || b != d {
		t.Fatalf("keys %q %q %q", a, b, d)
	}
	// a 4xx is not retried
	n.Store(0)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv2.Close()
	c2, _ := NewClient(Config{BaseURL: srv2.URL, Backoff: time.Millisecond})
	if err := c2.Do(context.Background(), http.MethodGet, "/x", nil, false, nil); err == nil || n.Load() != 1 {
		t.Fatalf("4xx: %v after %d calls", err, n.Load())
	}
}

func TestTimeoutIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	c, _ := NewClient(Config{BaseURL: srv.URL, Timeout: 30 * time.Millisecond, Attempts: 1})
	if err := c.Do(context.Background(), http.MethodGet, "/x", nil, false, nil); err == nil {
		t.Fatal("no error on timeout")
	}
}

func TestBreakerOpensAndRecovers(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, _ := NewClient(Config{BaseURL: srv.URL, Attempts: 1, BreakerFailures: 2, BreakerPause: time.Hour})
	now := time.Now()
	c.Breaker().now = func() time.Time { return now }
	do := func() error { return c.Do(context.Background(), http.MethodGet, "/x", nil, false, nil) }
	_ = do()
	_ = do()
	if c.Breaker().State() != StateOpen {
		t.Fatalf("state %s", c.Breaker().State())
	}
	before := calls.Load()
	if err := do(); err != ErrCircuitOpen || calls.Load() != before {
		t.Fatalf("open circuit: %v, %d calls", err, calls.Load()-before)
	}
	now = now.Add(2 * time.Hour)
	fail.Store(false)
	if c.Breaker().State() != StateHalfOpen {
		t.Fatalf("after the pause: %s", c.Breaker().State())
	}
	if err := do(); err != nil || c.Breaker().State() != StateClosed {
		t.Fatalf("probe: %v %s", err, c.Breaker().State())
	}
	// a 4xx does not count
	fail.Store(false)
	srv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(400) }))
	defer srv4.Close()
	c4, _ := NewClient(Config{BaseURL: srv4.URL, Attempts: 1, BreakerFailures: 1})
	_ = c4.Do(context.Background(), http.MethodGet, "/x", nil, false, nil)
	if c4.Breaker().State() != StateClosed {
		t.Fatal("a 4xx opened the circuit")
	}
}

func TestRegistryStates(t *testing.T) {
	dir := t.TempDir()
	good := describeServer(t, testProto)
	other := describeServer(t, "harnessly.test/v2")
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	tok := filepath.Join(dir, "tok")
	_ = os.WriteFile(tok, []byte("s3cret\n"), 0o600)
	file := filepath.Join(dir, "ext.json")
	spec, _ := json.Marshal([]Spec{
		{Name: "good", Kind: "test", URL: good.URL, TokenFile: tok, Protocol: testProto},
		{Name: "other", Kind: "test", URL: other.URL, Protocol: testProto},
		{Name: "down", Kind: "test", URL: downURL, Protocol: testProto},
	})
	_ = os.WriteFile(file, spec, 0o600)
	specs, err := LoadSpecs(file)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(specs, Config{Attempts: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if st := r.Statuses(); st[0].Health != HealthPending {
		t.Fatalf("before check: %+v", st)
	}
	r.Check(context.Background())
	got := map[string]Status{}
	for _, s := range r.Statuses() {
		got[s.Name] = s
	}
	if got["good"].Health != HealthOK || got["good"].Description.Version != "1.2" ||
		got["other"].Health != HealthMismatch || got["down"].Health != HealthDown || got["down"].Error == "" {
		t.Fatalf("states: %+v", got)
	}
	if Summary(r.Statuses()) == "" {
		t.Fatal("empty summary")
	}
}

func TestSpecsAreChecked(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"bad name": `[{"name":"Bad","kind":"k","url":"http://x","protocol":"harnessly.k/v1"}]`,
		"no proto": `[{"name":"a","kind":"k","url":"http://x","protocol":"v1"}]`,
		"twice":    `[{"name":"a","url":"http://x","protocol":"harnessly.k/v1"},{"name":"a","url":"http://y","protocol":"harnessly.k/v1"}]`,
		"not json": `{`,
		"no url":   `[{"name":"a","protocol":"harnessly.k/v1"}]`,
		"huge max": `[{"name":"a","url":"http://x","protocol":"harnessly.k/v1","maxAnswerBytes":999999999999}]`,
		"neg max":  `[{"name":"a","url":"http://x","protocol":"harnessly.k/v1","maxAnswerBytes":-1}]`,
	} {
		f := filepath.Join(dir, "f.json")
		_ = os.WriteFile(f, []byte(body), 0o600)
		if _, err := LoadSpecs(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewRegistry([]Spec{{Name: "a", URL: "ftp://x", Protocol: "harnessly.k/v1"}}, Config{}); err == nil {
		t.Error("an ftp address was accepted")
	}
}

func TestAnAnswerOverTheLimitIsTooLargeNotCut(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		switch r.URL.Path {
		case "/big":
			_, _ = w.Write([]byte(`{"content":"` + strings.Repeat("a", 2048) + `"}`))
		case "/413":
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		case "/said":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"too_large"}`))
		default:
			_, _ = w.Write([]byte(`{"content":"ok"}`))
		}
	}))
	defer srv.Close()
	c, _ := NewClient(Config{BaseURL: srv.URL, MaxAnswer: 1024, Backoff: time.Millisecond})
	if c.MaxAnswer() != 1024 {
		t.Fatalf("max %d", c.MaxAnswer())
	}
	for _, path := range []string{"/big", "/413", "/said"} {
		n.Store(0)
		var out struct{ Content string }
		if err := c.Do(context.Background(), http.MethodPost, path, struct{}{}, false, &out); !errors.Is(err, ErrTooLarge) || out.Content != "" {
			t.Fatalf("%s: %v %q", path, err, out.Content)
		}
		if n.Load() != 1 {
			t.Fatalf("%s: too large was retried %d times", path, n.Load())
		}
	}
	var out struct{ Content string }
	if err := c.Do(context.Background(), http.MethodPost, "/small", struct{}{}, false, &out); err != nil || out.Content != "ok" {
		t.Fatalf("small: %v %q", err, out.Content)
	}
	d, _ := NewClient(Config{BaseURL: srv.URL})
	if d.MaxAnswer() != DefaultMaxAnswer {
		t.Fatalf("default max %d", d.MaxAnswer())
	}
}

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer is a provider service as docs/hub/provider.md describes it.
type fakeServer struct {
	t     *testing.T
	token string
	mu    sync.Mutex
	keys  map[string]string // Idempotency-Key -> opId
	ops   map[string]string // opId -> state
	next  int
	// failures makes the first n state-changing requests answer 503.
	failures atomic.Int32
	hits     atomic.Int32
}

func newFake(t *testing.T) (*fakeServer, *httptest.Server) {
	f := &fakeServer{t: t, token: "s3cret", keys: map[string]string{}, ops: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /machines/{id}/plans", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "gone" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, map[string]any{"plans": []Plan{
			{ID: "s", Name: "S", CPU: 2, RAMGB: 4, DiskGB: 40, Price: &Price{Amount: 50000, Currency: "RUB", Period: "month"}, Current: true},
			{ID: "m", Name: "M", CPU: 4, RAMGB: 8, DiskGB: 80, Price: &Price{Amount: 90000, Currency: "RUB", Period: "month"}},
		}})
	})
	mux.HandleFunc("POST /machines/{id}/quote", func(w http.ResponseWriter, r *http.Request) {
		var want Resources
		_ = json.NewDecoder(r.Body).Decode(&want)
		if want.DiskGB > 1000 {
			writeJSON(w, 422, map[string]any{"error": "too_big", "message": "disk over 1000 GB"})
			return
		}
		writeJSON(w, 200, map[string]any{"price": Price{Amount: int64(want.CPU)*10000 + int64(want.RAMGB)*1000, Currency: "RUB", Period: "month"}})
	})
	op := func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if f.failures.Add(-1) >= 0 {
			writeJSON(w, 503, map[string]any{"error": "busy"})
			return
		}
		key := r.Header.Get("Idempotency-Key")
		f.mu.Lock()
		defer f.mu.Unlock()
		id, ok := f.keys[key]
		if !ok {
			f.next++
			id = "o" + string(rune('0'+f.next))
			f.keys[key] = id
			f.ops[id] = StateRunning
		}
		writeJSON(w, 202, map[string]any{"opId": id})
	}
	mux.HandleFunc("POST /machines/{id}/resize", op)
	mux.HandleFunc("POST /machines/{id}/reboot", op)
	mux.HandleFunc("POST /machines/{id}/power-off", op)
	mux.HandleFunc("POST /machines/{id}/power-on", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 501, map[string]any{"error": "not_supported"})
	})
	mux.HandleFunc("GET /ops/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		st, ok := f.ops[r.PathValue("id")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, OpStatus{State: st})
	})
	mux.HandleFunc("GET /machines/{id}/console", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"url": "https://console.example.org/" + r.PathValue("id")})
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			writeJSON(w, 401, map[string]any{"error": "unauthorized"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newHTTP(t *testing.T, srv *httptest.Server, token string) *HTTP {
	t.Helper()
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHTTP(HTTPConfig{BaseURL: srv.URL + "/", TokenFile: file, Backoff: time.Millisecond, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHTTPPlansQuoteConsole(t *testing.T) {
	_, srv := newFake(t)
	h := newHTTP(t, srv, "s3cret")
	ctx := context.Background()
	m := Machine{ID: "m1", ExternalID: "vm-7"}
	plans, err := h.Plans(ctx, m)
	if err != nil || len(plans) != 2 || !plans[0].Current || plans[1].Price.Amount != 90000 || plans[1].Resources() != (Resources{4, 8, 80}) {
		t.Fatalf("plans %+v err %v", plans, err)
	}
	price, err := h.Quote(ctx, m, Resources{CPU: 6, RAMGB: 12, DiskGB: 100})
	if err != nil || price != (Price{Amount: 72000, Currency: "RUB", Period: "month"}) {
		t.Fatalf("price %+v err %v", price, err)
	}
	var pe *Error
	if _, err := h.Quote(ctx, m, Resources{CPU: 1, RAMGB: 1, DiskGB: 5000}); !errors.As(err, &pe) || pe.Code != "too_big" || pe.Status != 422 {
		t.Fatalf("want a coded refusal, got %v", err)
	}
	if u, err := h.ConsoleURL(ctx, m); err != nil || u != "https://console.example.org/vm-7" {
		t.Fatalf("console %q %v", u, err)
	}
	if _, err := h.Plans(ctx, Machine{ID: "gone"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestHTTPBadToken(t *testing.T) {
	_, srv := newFake(t)
	h := newHTTP(t, srv, "wrong")
	var pe *Error
	if _, err := h.Plans(context.Background(), Machine{ID: "m1"}); !errors.As(err, &pe) || pe.Status != 401 {
		t.Fatalf("want 401, got %v", err)
	}
}

func TestHTTPRetriesWithTheSameKey(t *testing.T) {
	f, srv := newFake(t)
	h := newHTTP(t, srv, "s3cret")
	f.failures.Store(2)
	ref, err := h.Reboot(WithIdempotencyKey(context.Background(), "op-123"), Machine{ID: "m1"})
	if err != nil || ref.ID == "" || f.hits.Load() != 3 {
		t.Fatalf("ref %+v err %v hits %d", ref, err, f.hits.Load())
	}
	// The same key again is the same operation, not a second reboot.
	again, err := h.Reboot(WithIdempotencyKey(context.Background(), "op-123"), Machine{ID: "m1"})
	if err != nil || again != ref {
		t.Fatalf("again %+v err %v", again, err)
	}
	st, err := h.Op(context.Background(), ref)
	if err != nil || st.State != StateRunning || st.Finished() {
		t.Fatalf("status %+v err %v", st, err)
	}
	// Out of attempts: the 5xx is the answer.
	f.failures.Store(5)
	var pe *Error
	if _, err := h.Resize(context.Background(), Machine{ID: "m1"}, Resources{CPU: 4, RAMGB: 8, DiskGB: 80}); !errors.As(err, &pe) || pe.Status != 503 {
		t.Fatalf("want 503, got %v", err)
	}
}

func TestHTTPNotSupported(t *testing.T) {
	_, srv := newFake(t)
	h := newHTTP(t, srv, "s3cret")
	if _, err := h.PowerOn(context.Background(), Machine{ID: "m1"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestHTTPTokenRotates(t *testing.T) {
	f, srv := newFake(t)
	file := filepath.Join(t.TempDir(), "token")
	_ = os.WriteFile(file, []byte("old"), 0o600)
	h, err := NewHTTP(HTTPConfig{BaseURL: srv.URL, TokenFile: file, Backoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Plans(context.Background(), Machine{ID: "m1"}); err == nil {
		t.Fatal("the old token must be refused")
	}
	_ = os.WriteFile(file, []byte(f.token), 0o600)
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(file, later, later)
	if _, err := h.Plans(context.Background(), Machine{ID: "m1"}); err != nil {
		t.Fatalf("the rotated token must be read: %v", err)
	}
}

func TestNewHTTPRejectsBadAddress(t *testing.T) {
	for _, u := range []string{"", "ftp://x", "http://"} {
		if _, err := NewHTTP(HTTPConfig{BaseURL: u}); err == nil {
			t.Errorf("%q must be refused", u)
		}
	}
	if _, err := NewHTTP(HTTPConfig{BaseURL: "http://x", TokenFile: "/nonexistent/token"}); err == nil {
		t.Error("a missing token file must be refused at start")
	}
}

func TestNone(t *testing.T) {
	var p Provider = None{}
	ctx := context.Background()
	if _, err := p.Plans(ctx, Machine{}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := p.Reboot(ctx, Machine{}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if Supported(p) || Supported(nil) || !Supported(NewMemory()) {
		t.Fatal("Supported is wrong")
	}
}

func TestMemory(t *testing.T) {
	p := NewMemory()
	p.Catalog = []Plan{{ID: "s", CPU: 2, RAMGB: 4, DiskGB: 40, Current: true}, {ID: "m", CPU: 4, RAMGB: 8, DiskGB: 80}}
	p.PriceFn = func(r Resources) Price { return Price{Amount: int64(r.CPU) * 100, Currency: "EUR", Period: "month"} }
	ctx := context.Background()
	m := Machine{ID: "m1"}
	ref, err := p.Resize(ctx, m, Resources{CPU: 4, RAMGB: 8, DiskGB: 80})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := p.Op(ctx, ref); st.State != StateRunning {
		t.Fatalf("status %+v", st)
	}
	p.Complete(ref.ID, "")
	if st, _ := p.Op(ctx, ref); st.State != StateDone {
		t.Fatalf("status %+v", st)
	}
	plans, _ := p.Plans(ctx, m)
	if plans[0].Current || !plans[1].Current {
		t.Fatalf("plans %+v", plans)
	}
	p.FailNext(errors.New("boom"))
	if _, err := p.Reboot(ctx, m); err == nil {
		t.Fatal("want the injected failure")
	}
	if got := p.Calls(); len(got) != 1 || got[0] != "resize:m1:4/8/80" {
		t.Fatalf("calls %v", got)
	}
}

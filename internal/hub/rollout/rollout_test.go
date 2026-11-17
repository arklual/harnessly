package rollout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
)

const token = "0123456789abcdef0123456789abcdef" // gitleaks:allow (a test value, not a secret)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db := storetest.Open(t)
	return db
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func mark(s *Service, auth, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/rollout", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if auth != "" {
		r.Header.Set("Authorization", "Bearer "+auth)
	}
	w := httptest.NewRecorder()
	httpx.Handler(s.ServeMark).ServeHTTP(w, r)
	return w
}

func TestPipelineStartAndEnd(t *testing.T) {
	db := openDB(t)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a := &Service{DB: db, Token: token, Now: c.now, Replica: "a"}
	b := &Service{DB: db, Now: c.now, Replica: "b"}
	ctx := context.Background()
	if a.Active(ctx) || b.Active(ctx) {
		t.Fatal("active with no mark")
	}
	if w := mark(a, token, `{"state":"start","what":"runtime","minutes":5}`); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	c.t = c.t.Add(3 * time.Second) // past b's cache
	if !b.Active(ctx) {
		t.Fatal("the other replica does not see the start")
	}
	if w := mark(a, token, `{"state":"end","what":"runtime"}`); w.Code != http.StatusOK {
		t.Fatalf("end: %d", w.Code)
	}
	c.t = c.t.Add(3 * time.Second)
	if b.Active(ctx) {
		t.Fatal("still active after the end")
	}
	// A start that is never ended runs out.
	mark(a, token, `{"state":"start","what":"hub","minutes":1}`)
	c.t = c.t.Add(61 * time.Second)
	if b.Active(ctx) {
		t.Fatal("a start without an end did not run out")
	}
}

func TestMarkRefusals(t *testing.T) {
	db := openDB(t)
	shut := &Service{DB: db}
	if w := mark(shut, token, `{"state":"start","what":"hub"}`); w.Code != http.StatusNotFound {
		t.Fatalf("no token: %d", w.Code)
	}
	s := &Service{DB: db, Token: token}
	for _, tc := range []struct {
		auth, body string
		code       int
	}{
		{"", `{"state":"start","what":"hub"}`, http.StatusUnauthorized},
		{"wrong-wrong-wrong-wrong-wrong-wrong", `{"state":"start","what":"hub"}`, http.StatusUnauthorized},
		{token, `{"state":"start","what":"db"}`, http.StatusBadRequest},
		{token, `{"state":"later","what":"hub"}`, http.StatusBadRequest},
		{token, `{"state":"start","what":"hub","minutes":999}`, http.StatusBadRequest},
	} {
		if w := mark(s, tc.auth, tc.body); w.Code != tc.code {
			t.Errorf("%s %s: %d, want %d", tc.auth, tc.body, w.Code, tc.code)
		}
	}
}

func TestStoppingReplica(t *testing.T) {
	db := openDB(t)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a := &Service{DB: db, Now: c.now, Replica: "a"}
	b := &Service{DB: db, Now: c.now, Replica: "b"}
	ctx := context.Background()
	a.Stopped(ctx, time.Second) // not stopping: nothing to shorten
	a.Stopping(ctx, 40*time.Second)
	if !a.Active(ctx) || !b.Active(ctx) {
		t.Fatal("a stop is not seen as a rollout")
	}
	a.Stopped(ctx, 20*time.Second)
	c.t = c.t.Add(19 * time.Second)
	if !b.Active(ctx) {
		t.Fatal("the tail ended early")
	}
	c.t = c.t.Add(2 * time.Second)
	if b.Active(ctx) {
		t.Fatal("the tail did not end")
	}

	w := httptest.NewRecorder()
	b.ServeState(w, httptest.NewRequest(http.MethodGet, "/api/v2/rollout", nil))
	if w.Body.String() != "{\"active\":false}\n" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("state: %q %q", w.Body.String(), w.Header().Get("Cache-Control"))
	}
}

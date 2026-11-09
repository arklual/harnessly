package automations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func newSvc(t *testing.T, now time.Time) *Service {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(Deps{Config: &config.Config{Automations: true, AutomationsSecret: "s3cret"}, DB: db, Now: func() time.Time { return now }})
}

func rule(t *testing.T, s *Service, name string, limits Limits) Rule {
	t.Helper()
	r, ok, err := s.addRule(context.Background(), Rule{OwnerID: "u1", Name: name, Trigger: Trigger{Source: "alert"}, PromptTemplate: "fix it", Model: "sonnet", Limits: limits, Enabled: true})
	if err != nil || !ok {
		t.Fatalf("addRule: %v %v", ok, err)
	}
	return r
}

func noon() time.Time { return time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC) } // 12:00 Moscow

func TestRuleNameUnique(t *testing.T) {
	s := newSvc(t, noon())
	rule(t, s, "a", Limits{})
	if _, ok, err := s.addRule(context.Background(), Rule{OwnerID: "u1", Name: "a", Trigger: Trigger{Source: "x"}, PromptTemplate: "p", Model: "sonnet"}); err != nil || ok {
		t.Fatalf("duplicate name: ok=%v err=%v", ok, err)
	}
}

func TestQueueDedupAndLimit(t *testing.T) {
	s := newSvc(t, noon())
	two := 2
	r := rule(t, s, "a", Limits{MaxRunsPerDay: &two})
	ctx := context.Background()
	if o, _ := s.queueRun(ctx, r, "f1", json.RawMessage(`{}`)); !o.Queued {
		t.Fatalf("first: %+v", o)
	}
	if o, _ := s.queueRun(ctx, r, "f1", json.RawMessage(`{}`)); o.Reason != "duplicate" {
		t.Fatalf("dup: %+v", o)
	}
	if o, _ := s.queueRun(ctx, r, "f2", json.RawMessage(`{}`)); !o.Queued {
		t.Fatalf("second: %+v", o)
	}
	if o, _ := s.queueRun(ctx, r, "f3", json.RawMessage(`{}`)); o.Reason != "rate_limited" {
		t.Fatalf("limit: %+v", o)
	}
}

func TestQuietHours(t *testing.T) {
	r := Rule{}
	if !quietNow(r, time.Date(2026, 1, 5, 22, 0, 0, 0, time.UTC)) { // 01:00 Moscow
		t.Error("01:00 MSK should be quiet by default")
	}
	if quietNow(r, noon()) {
		t.Error("12:00 MSK should not be quiet")
	}
	a, b := 22, 6
	r.Limits = Limits{QuietHoursStartMsk: &a, QuietHoursEndMsk: &b}
	if !quietNow(r, time.Date(2026, 1, 5, 20, 0, 0, 0, time.UTC)) { // 23:00 MSK
		t.Error("23:00 MSK is inside a wrapping window")
	}
}

func TestClaimAndResult(t *testing.T) {
	s := newSvc(t, noon())
	ctx := context.Background()
	r := rule(t, s, "a", Limits{})
	out, _ := s.queueRun(ctx, r, "f1", json.RawMessage(`{"k":1}`))
	run, _, token, err := s.claim(ctx)
	if err != nil || run.ID != out.RunID || token == "" {
		t.Fatalf("claim: %v %+v", err, run)
	}
	if _, _, _, err := s.claim(ctx); err != store.ErrNotFound {
		t.Fatalf("second claim should find nothing: %v", err)
	}
	if err := s.recordResult(ctx, run.ID, "wrong", "x", "", ""); err == nil {
		t.Fatal("wrong token accepted")
	}
	if err := s.recordResult(ctx, run.ID, token, "done", "HRN-1", "https://example.com/mr/1"); err != nil {
		t.Fatal(err)
	}
	if err := s.recordResult(ctx, run.ID, token, "again", "", ""); err == nil {
		t.Fatal("result accepted twice")
	}
	got, _ := s.runByID(ctx, run.ID)
	if got.Status != StatusAwaitingApproval || got.Summary != "done" {
		t.Fatalf("run: %+v", got)
	}
	if _, ok, err := s.decide(ctx, run.ID, "u1", StatusApproved); err != nil || !ok {
		t.Fatalf("decide: %v %v", ok, err)
	}
	if _, ok, _ := s.decide(ctx, run.ID, "u1", StatusRejected); ok {
		t.Fatal("decided twice")
	}
}

func TestClaimSkipsQuietRule(t *testing.T) {
	s := newSvc(t, time.Date(2026, 1, 5, 22, 0, 0, 0, time.UTC))
	r := rule(t, s, "a", Limits{})
	if _, err := s.queueRun(context.Background(), r, "f", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.claim(context.Background()); err != store.ErrNotFound {
		t.Fatalf("quiet rule claimed: %v", err)
	}
}

func post(s *Service, secret, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	s.Mount(mux)
	req := httptest.NewRequest("POST", s.prefix+"/hook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Automation-Secret", secret)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestHook(t *testing.T) {
	s := newSvc(t, noon())
	r := rule(t, s, "a", Limits{})
	r.Trigger.Filter = map[string]any{"severity": "critical"}
	if err := s.saveRule(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	body := `{"source":"alert","fingerprint":"f1","event":{"severity":"critical"}}`
	if w := post(s, "", body); w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
		t.Fatalf("no secret: %d", w.Code)
	}
	if w := post(s, "nope", body); w.Code == http.StatusAccepted {
		t.Fatal("wrong secret accepted")
	}
	w := post(s, "s3cret", body)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"queued":true`) {
		t.Fatalf("hook: %d %s", w.Code, w.Body)
	}
	w = post(s, "s3cret", `{"source":"alert","fingerprint":"f2","event":{"severity":"low"}}`)
	if !strings.Contains(w.Body.String(), `"matched":0`) {
		t.Fatalf("filter ignored: %s", w.Body)
	}
	s.cfg.Automations = false
	if w := post(s, "s3cret", body); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("off: %d", w.Code)
	}
}

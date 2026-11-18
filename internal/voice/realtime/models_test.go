package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseRealtimeModels(t *testing.T) {
	raw := `{"data":[{"id":"gpt-realtime"},{"id":"gpt-realtime-mini"},{"id":"gpt-realtime-2.2"},{"id":"gpt-realtime-2.2-mini"},` +
		`{"id":"gpt-realtime-2025-08-28"},{"id":"gpt-4o-realtime-preview"},{"id":"gpt-realtime-2"},{"id":"gpt-6.2-sol"},{"id":"gpt-realtime-transcribe-2"}]}`
	got := parseRealtimeModels([]byte(raw))
	if strings.Join(got, ",") != "gpt-realtime-2.2,gpt-realtime-2.2-mini,gpt-realtime-2,gpt-realtime,gpt-realtime-mini" {
		t.Fatalf("got %v", got)
	}
}

func TestMergeModelsNewestFirst(t *testing.T) {
	got := mergeModels([]string{"gpt-live-1-codex"}, []string{"gpt-live-1.1-codex"})
	if strings.Join(got, ",") != "gpt-live-1.1-codex,gpt-live-1-codex" {
		t.Fatalf("got %v", got)
	}
	got = mergeModels([]string{"gpt-realtime-3"}, Models[RouteKey])
	if got[0] != "gpt-realtime-3" || len(got) != len(Models[RouteKey])+1 {
		t.Fatalf("got %v", got)
	}
}

func TestSubscriptionModelFollowsCodex(t *testing.T) {
	s := New(Config{Dir: t.TempDir(), LiveModels: func() []string { return []string{"gpt-live-1-codex", "gpt-live-1.4-codex", "other"} }})
	if got := s.modelsOf(RouteSubscription); len(got) != 1 || got[0] != "gpt-live-1.4-codex" {
		t.Fatalf("got %v", got)
	}
	s = New(Config{Dir: t.TempDir(), LiveModels: func() []string { return []string{"gpt-live-1-codex"} }})
	if got := s.modelsOf(RouteSubscription); got[0] != Model {
		t.Fatalf("an older Codex took the model down to %v", got)
	}
}

func TestKeyRouteModelsFromTheAPI(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-realtime-3"},{"id":"gpt-realtime-2.1"}]}`))
	}))
	defer srv.Close()
	s := New(Config{Dir: t.TempDir(), Key: func() (string, bool) { return "sk-test", true },
		Endpoints: Endpoints{KeyCall: srv.URL + "/v1/realtime/calls"}})
	_ = s.modelsOf(RouteKey) // starts the refresh
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := s.modelsOf(RouteKey)
		if got[0] == "gpt-realtime-3" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("auth %q", auth)
	}
	if st := s.State(); st.Route == RouteKey && st.Models[0] != "gpt-realtime-3" {
		t.Fatalf("state models %v", st.Models)
	}
}

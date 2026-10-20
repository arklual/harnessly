package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestFromEnvTrimsAddressesToOrigins(t *testing.T) {
	c := FromEnv(envOf(map[string]string{
		"RUNTIME_BOARDS_URL":    "https://boards.example.com/b/",
		"RUNTIME_ARTIFACTS_URL": " http://localhost:8091/ ",
		"RUNTIME_BOARDS_TOKEN":  " b-token ",
	}))
	if c.BoardsURL != "https://boards.example.com" || c.ArtifactsURL != "http://localhost:8091" || c.BoardsToken != "b-token" {
		t.Fatalf("%+v", c)
	}
	for _, bad := range []string{"", "boards.example.com", "ftp://x", "javascript:alert(1)", "https://"} {
		if got := origin(bad); got != "" {
			t.Errorf("origin(%q) = %q", bad, got)
		}
	}
	if c.Author != "" {
		t.Fatalf("author %q", c.Author)
	}
	if a := FromEnv(envOf(map[string]string{"RUNTIME_OWNER_NAME": "Ada"})).Author; a != "Ada (agent)" {
		t.Fatalf("author from the owner's name: %q", a)
	}
	if a := FromEnv(envOf(map[string]string{"RUNTIME_OWNER_NAME": "Ada", "RUNTIME_ARTIFACTS_AUTHOR": "Bot"})).Author; a != "Bot" {
		t.Fatalf("an explicit author: %q", a)
	}
	if (Config{}).Enabled() || !c.Enabled() {
		t.Fatal("Enabled")
	}
}

func TestAgentEnv(t *testing.T) {
	if env := (Config{}).AgentEnv("tab-a"); env != nil {
		t.Fatalf("nothing configured adds nothing: %q", env)
	}
	c := Config{BoardsURL: "https://boards.example.com", BoardsToken: "runtime", ArtifactsURL: "https://docs.example.com",
		ArtifactsToken: "editor", ArtifactsReadToken: "viewer", Author: "Ada (agent)"}
	want := []string{"BOARDS_URL=https://boards.example.com", "BOARDS_TOKEN=runtime", "ARTIFACTS_URL=https://docs.example.com",
		"ARTIFACTS_TOKEN=editor", "ARTIFACTS_SESSION=tab-a", "ARTIFACTS_AUTHOR=Ada (agent)"}
	if got := c.AgentEnv("tab-a"); !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	c.BoardsAgentToken = "agents"
	if got := c.AgentEnv(""); !reflect.DeepEqual(got, []string{"BOARDS_URL=https://boards.example.com", "BOARDS_TOKEN=agents",
		"ARTIFACTS_URL=https://docs.example.com", "ARTIFACTS_TOKEN=editor", "ARTIFACTS_AUTHOR=Ada (agent)"}) {
		t.Fatalf("an agents' token wins, no conversation leaves the session out: %q", got)
	}
	only := Config{ArtifactsURL: "https://docs.example.com"}
	if got := only.AgentEnv("tab-b"); !reflect.DeepEqual(got, []string{"ARTIFACTS_URL=https://docs.example.com", "ARTIFACTS_SESSION=tab-b"}) {
		t.Fatalf("%q", got)
	}
}

func TestPublicHasNoTokens(t *testing.T) {
	c := Config{BoardsURL: "https://boards.example.com", BoardsToken: "secret", ArtifactsToken: "secret"}
	raw, _ := json.Marshal(c.Public())
	if string(raw) != `{"boards":{"url":"https://boards.example.com"},"artifacts":null}` {
		t.Fatal(string(raw))
	}
}

func TestBoardPath(t *testing.T) {
	for in, want := range map[string]string{"/b/plan-abc123def456?k=key": "/b/plan-abc123def456?k=key", "/b/x": "/b/x"} {
		if got, err := BoardPath(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"https://evil.example/b/x", "//evil.example/b/x", "/a/x", "/b/../admin", "b/x", ""} {
		if _, err := BoardPath(bad); !errors.Is(err, ErrBadPath) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBoardSignIn(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/b/api/handoff" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"url":"` + "http://" + r.Host + `/b/auth/handoff?t=signed"}`))
	}))
	defer srv.Close()
	c := Config{BoardsURL: srv.URL, BoardsToken: "runtime-token"}
	u, err := c.BoardSignIn(context.Background(), User{ID: "u-1", Name: "Ada", Email: "ada@example.com"}, "/b/plan-abc?k=key")
	if err != nil || u != srv.URL+"/b/auth/handoff?t=signed" {
		t.Fatalf("%q %v", u, err)
	}
	if auth != "Bearer runtime-token" || got["next"] != "/b/plan-abc?k=key" || got["ttl"] != float64(60) ||
		!reflect.DeepEqual(got["user"], map[string]any{"id": "u-1", "name": "Ada", "email": "ada@example.com"}) {
		t.Fatalf("%q %v", auth, got)
	}

	if u, err := (Config{BoardsURL: srv.URL}).BoardSignIn(context.Background(), User{ID: "u"}, "/b/x?k=1"); err != nil || u != srv.URL+"/b/x?k=1" {
		t.Fatalf("without a token the board itself: %q %v", u, err)
	}
	if _, err := (Config{}).BoardSignIn(context.Background(), User{}, "/b/x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}

	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer refusing.Close()
	_, err = Config{BoardsURL: refusing.URL, BoardsToken: "wrong"}.BoardSignIn(context.Background(), User{ID: "u"}, "/b/x")
	var ue *UpstreamError
	if !errors.As(err, &ue) || ue.Status != 401 || ue.Text != "unauthorized" {
		t.Fatalf("%v", err)
	}

	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"url":"https://evil.example/b/auth/handoff?t=x"}`))
	}))
	defer elsewhere.Close()
	if _, err := (Config{BoardsURL: elsewhere.URL, BoardsToken: "t"}).BoardSignIn(context.Background(), User{ID: "u"}, "/b/x"); err == nil {
		t.Fatal("a hand-off link off the boards server is refused")
	}
}

func TestDocuments(t *testing.T) {
	var query, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, auth = r.URL.RawQuery, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"total":1,"items":[{"slug":"plan-4k2m9x","title":"Plan","updatedAt":"2026-10-19T12:00:00.000Z",
			"versionCount":3,"currentVersion":{"n":3},"viewUrl":"https://docs.example.com/a/plan-4k2m9x",
			"embedUrl":"https://docs.example.com/embed/plan-4k2m9x","session":"tab-a","author":"x"}]}`))
	}))
	defer srv.Close()
	docs, err := Config{ArtifactsURL: srv.URL, ArtifactsToken: "editor", ArtifactsReadToken: "viewer"}.Documents(context.Background(), "tab-a")
	if err != nil {
		t.Fatal(err)
	}
	want := []Document{{Slug: "plan-4k2m9x", Title: "Plan", UpdatedAt: "2026-10-19T12:00:00.000Z", Version: 3,
		ViewURL: "https://docs.example.com/a/plan-4k2m9x", EmbedURL: "https://docs.example.com/embed/plan-4k2m9x"}}
	if !reflect.DeepEqual(docs, want) || query != "limit=100&session=tab-a" || auth != "Bearer viewer" {
		t.Fatalf("%+v %q %q", docs, query, auth)
	}
	if _, err := (Config{ArtifactsURL: srv.URL, ArtifactsToken: "editor"}).Documents(context.Background(), "tab-a"); err != nil || auth != "Bearer editor" {
		t.Fatalf("the editor token stands in for a missing viewer token: %q %v", auth, err)
	}
	if _, err := (Config{}).Documents(context.Background(), "x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}

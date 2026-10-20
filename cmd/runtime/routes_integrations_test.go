package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

func integrationDeps(standalone bool) deps {
	return deps{
		Standalone: standalone,
		SessionOK:  func(r *http.Request) bool { return r.Header.Get("Cookie") == "session=ok" },
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if r.Header.Get("Cookie") == "session=ok" || r.Header.Get("x-cli-token") == "tok" {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		},
		Claims: func(r *http.Request) map[string]any { return map[string]any{"sub": "ada@example.com"} },
	}
}

func serveRoutes(routes []gate.Route, method, target, body string, h http.Header) (int, string) {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range h {
		r.Header[k] = v
	}
	w := httptest.NewRecorder()
	for _, rt := range routes {
		if rt.Match(r) {
			rt.Handler.ServeHTTP(w, r)
			return w.Code, strings.TrimSpace(w.Body.String())
		}
	}
	return -1, ""
}

func TestIntegrationRoutes(t *testing.T) {
	if routes := integrationRoutes(integrationDeps(false), integrations.Config{}); routes != nil {
		t.Fatal("in front of the previous runtime with no service configured, its answers stay its own")
	}
	var handoff string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b/api/handoff":
			raw, _ := io.ReadAll(r.Body)
			handoff = string(raw)
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"url":"http://`+r.Host+`/b/auth/handoff?t=x"}`)
		case "/api/v1/artifacts":
			w.WriteHeader(503)
			_, _ = io.WriteString(w, `{"error":"storage_unavailable"}`)
		}
	}))
	defer srv.Close()
	t.Setenv("RUNTIME_OWNER_NAME", "Ada")
	routes := integrationRoutes(integrationDeps(false), integrations.Config{BoardsURL: srv.URL, BoardsToken: "t", ArtifactsURL: srv.URL})
	page := http.Header{"Cookie": {"session=ok"}}
	token := http.Header{"X-Cli-Token": {"tok"}}

	if code, body := serveRoutes(routes, "GET", "/api/integrations", "", nil); code != 401 || body != `{"error":"Unauthorized"}` {
		t.Fatalf("anonymous: %d %s", code, body)
	}
	if code, body := serveRoutes(routes, "GET", "/agents/api/integrations", "", token); code != 200 ||
		body != `{"boards":{"url":"`+srv.URL+`"},"artifacts":{"url":"`+srv.URL+`"}}` {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := serveRoutes(routes, "POST", "/api/integrations", "{}", page); code != 405 {
		t.Fatal(code)
	}
	if code, body := serveRoutes(routes, "POST", "/api/integrations/boards/sign-in", `{"next":"/b/x?k=1"}`, page); code != 200 ||
		body != `{"url":"`+srv.URL+`/b/auth/handoff?t=x"}` {
		t.Fatalf("%d %s", code, body)
	}
	if handoff != `{"next":"/b/x?k=1","ttl":60,"user":{"id":"ada@example.com","name":"Ada","email":"ada@example.com"}}` {
		t.Fatalf("hand-off body %s", handoff)
	}
	if code, _ := serveRoutes(routes, "POST", "/api/integrations/boards/sign-in", `{"next":"/b/x"}`, token); code != 403 {
		t.Fatalf("cli-token: %d", code)
	}
	if code, body := serveRoutes(routes, "POST", "/api/integrations/boards/sign-in", `{"next":"https://evil.example/b/x"}`, page); code != 400 ||
		!strings.Contains(body, "boards path") {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := serveRoutes(routes, "POST", "/api/integrations/boards/sign-in", `nope`, page); code != 400 {
		t.Fatal(code)
	}
	if code, _ := serveRoutes(routes, "GET", "/api/integrations/artifacts/documents", "", page); code != 400 {
		t.Fatal("a session is required")
	}
	if code, body := serveRoutes(routes, "GET", "/api/integrations/artifacts/documents?session=tab-a", "", page); code != 502 ||
		body != `{"error":"artifacts: storage_unavailable","status":503}` {
		t.Fatalf("%d %s", code, body)
	}

	alone := integrationRoutes(integrationDeps(true), integrations.Config{})
	if code, body := serveRoutes(alone, "GET", "/api/integrations", "", page); code != 200 || body != `{"boards":null,"artifacts":null}` {
		t.Fatalf("standalone with nothing configured: %d %s", code, body)
	}
	if code, body := serveRoutes(alone, "POST", "/api/integrations/boards/sign-in", `{"next":"/b/x"}`, page); code != 404 ||
		body != `{"error":"boards is not configured"}` {
		t.Fatalf("%d %s", code, body)
	}
}

func TestPersonOf(t *testing.T) {
	t.Setenv("RUNTIME_OWNER_NAME", "")
	t.Setenv("RUNTIME_OWNER_EMAIL", "")
	if u := personOf(map[string]any{"sub": "user"}); u != (integrations.User{ID: "user", Name: "user"}) {
		t.Fatalf("%+v", u)
	}
	t.Setenv("RUNTIME_OWNER_EMAIL", "owner@example.org")
	t.Setenv("RUNTIME_OWNER_NAME", "Owner")
	if u := personOf(map[string]any{"sub": "user"}); u != (integrations.User{ID: "owner@example.org", Name: "Owner", Email: "owner@example.org"}) {
		t.Fatalf("%+v", u)
	}
	if u := personOf(map[string]any{"sub": "u-1", "name": "Bea", "email": "bea@example.org"}); u != (integrations.User{ID: "u-1", Name: "Bea", Email: "bea@example.org"}) {
		t.Fatalf("%+v", u)
	}
	if u := personOf(nil); u.ID != "owner@example.org" {
		t.Fatalf("%+v", u)
	}
}

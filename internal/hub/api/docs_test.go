package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// fakeArtifacts is the documents service in memory: just the calls the hub makes.
type fakeArtifacts struct {
	mu     sync.Mutex
	docs   map[string]map[string]any
	public map[string]bool
	auths  []string
}

func newFakeArtifacts(t *testing.T) (*fakeArtifacts, *httptest.Server) {
	f := &fakeArtifacts{docs: map[string]map[string]any{}, public: map[string]bool{}}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("POST /api/v1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		slug, _ := in["slug"].(string)
		if slug == "" {
			slug = "plan"
		}
		created := f.docs[slug] == nil
		f.docs[slug] = map[string]any{"slug": slug, "title": in["title"], "updatedAt": "2026-01-02T03:04:05Z", "html": in["html"], "markdown": in["markdown"]}
		write(w, map[string]any{"created": created, "version": map[string]any{"n": 1, "id": "v1"},
			"artifact": map[string]any{"slug": slug, "title": in["title"]}})
	})
	mux.HandleFunc("GET /api/v1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := []any{}
		for _, d := range f.docs {
			items = append(items, d)
		}
		write(w, map[string]any{"items": items})
	})
	mux.HandleFunc("GET /a/{slug}/raw", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>Plan</h1></body></html>"))
	})
	mux.HandleFunc("PUT /api/v1/artifacts/{slug}/access", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Public bool }
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.public[r.PathValue("slug")] = in.Public
		f.mu.Unlock()
		write(w, map[string]any{"public": in.Public, "publicUrl": "https://docs.example.com/p/" + r.PathValue("slug")})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

func docsStack(t *testing.T, opts ...hubtest.Option) *hubtest.Stack {
	t.Helper()
	return hubtest.New(t, opts...)
}

// Without the flag every route says why, with a code the page reads; it is never a 404.
func TestDocsNotConfigured(t *testing.T) {
	s := docsStack(t)
	owner := s.SignIn("owner@example.com")
	r := owner.Do("GET", "/api/platform/docs", nil)
	if r.Code != http.StatusServiceUnavailable || !strings.Contains(r.String(), "docs_not_configured") || !strings.Contains(r.String(), "HUB_DOCS") {
		t.Fatalf("flag off: %s", r)
	}

	s = docsStack(t, hubtest.Env("HUB_DOCS", "on"))
	owner = s.SignIn("owner@example.com")
	r = owner.Do("GET", "/api/platform/docs", nil)
	if r.Code != http.StatusServiceUnavailable || !strings.Contains(r.String(), "HUB_ARTIFACTS_URL") {
		t.Fatalf("no address: %s", r)
	}
}

// An agent publishes, the owner and a teammate read and comment within the level, a stranger to the level is refused.
func TestDocsPublishCommentAndRights(t *testing.T) {
	fake, srv := newFakeArtifacts(t)
	s := docsStack(t, hubtest.Env("HUB_DOCS", "on"), hubtest.Env("HUB_ARTIFACTS_URL", srv.URL), hubtest.Env("HUB_ARTIFACTS_TOKEN", "secret-token"))
	owner := s.SignIn("owner@example.com")
	mate := s.SignIn("mate@example.com")
	id, _ := owner.NewSpace("Acme")
	a := owner.Machine(id, nil)
	a.Handle("doc.result", func(hubtest.Call) (any, error) { return map[string]bool{"settled": true}, nil })

	a.Emit(map[string]any{"event": "doc.publish", "requestId": "r1", "title": "Plan", "format": "md", "content": "# Plan", "slug": "plan", "visibility": "private"})
	var res hubtest.Call
	s.Eventually("doc.result for publish", func() bool {
		for _, c := range a.Calls("doc.result") {
			if c.Str("requestId") == "r1" {
				res = c
				return true
			}
		}
		return false
	})
	if string(res.Params["status"]) != "200" || !strings.Contains(string(res.Params["body"]), `"slug":"plan"`) {
		t.Fatalf("publish result: %v", res.Params)
	}
	if len(fake.auths) != 1 || fake.auths[0] != "Bearer secret-token" {
		t.Fatalf("token sent to the documents service: %v", fake.auths)
	}

	// Private: the owner sees it, the teammate does not.
	if r := owner.Do("GET", "/api/platform/docs", nil); r.Code != 200 || !strings.Contains(r.String(), `"slug":"plan"`) {
		t.Fatalf("owner list: %s", r)
	}
	if r := mate.Do("GET", "/api/platform/docs", nil); r.Code != 200 || strings.Contains(r.String(), `"slug":"plan"`) {
		t.Fatalf("teammate list: %s", r)
	}
	if r := mate.Do("GET", "/api/platform/docs/plan", nil); r.Code != http.StatusForbidden {
		t.Fatalf("teammate detail: %s", r)
	}

	// The agent opens it for comments through doc.acl.
	a.Emit(map[string]any{"event": "doc.acl", "requestId": "r2", "slug": "plan", "method": "POST", "route": "/agents/api/example/docs/plan/acl/team", "body": map[string]string{"level": "comment"}})
	s.Eventually("doc.result for acl", func() bool {
		for _, c := range a.Calls("doc.result") {
			if c.Str("requestId") == "r2" {
				return string(c.Params["status"]) == "200"
			}
		}
		return false
	})
	// The team level reaches the people of the author's space, not everyone signed in.
	if r := mate.Do("GET", "/api/platform/docs/plan", nil); r.Code == 200 {
		t.Fatalf("outsider detail after acl: %s", r)
	}
	owner.Join(id, mate)
	if r := mate.Do("GET", "/api/platform/docs/plan", nil); r.Code != 200 {
		t.Fatalf("teammate detail after acl: %s", r)
	}
	r := mate.Do("POST", "/api/platform/docs/plan/threads", map[string]any{"anchor": map[string]string{"quote": "Plan", "prefix": "", "suffix": ""}, "body": "Why?"})
	if r.Code != http.StatusCreated {
		t.Fatalf("new thread: %s", r)
	}
	var th struct{ Thread struct{ ID string } }
	r.JSON(&th)
	s.Clock.Advance(time.Second)
	if r := owner.Do("POST", "/api/platform/docs/plan/threads/"+th.Thread.ID+"/comments", map[string]string{"body": "Because."}); r.Code != 200 || !strings.Contains(r.String(), "Because.") {
		t.Fatalf("reply: %s", r)
	}
	if r := owner.Do("GET", "/api/platform/docs/plan", nil); strings.Index(r.String(), "Why?") > strings.Index(r.String(), "Because.") {
		t.Fatalf("comments out of order: %s", r)
	}
	if r := owner.Do("GET", "/api/platform/docs/plan", nil); !strings.Contains(r.String(), `"openThreads":1`) || !strings.Contains(r.String(), `"unread":1`) {
		t.Fatalf("owner counts: %s", r)
	}
	if r := mate.Do("POST", "/api/platform/docs/plan/threads/"+th.Thread.ID+"/status", map[string]string{"status": "resolved"}); r.Code != 200 || !strings.Contains(r.String(), "resolved") {
		t.Fatalf("resolve: %s", r)
	}
	// Commenting is not editing, and only the owner opens the public link.
	if r := mate.Do("PUT", "/api/platform/docs/plan/source", map[string]any{"markdown": "x", "baseVersion": 1}); r.Code != http.StatusForbidden {
		t.Fatalf("teammate edit: %s", r)
	}
	if r := mate.Do("POST", "/api/platform/docs/plan/access", map[string]bool{"public": true}); r.Code != http.StatusForbidden {
		t.Fatalf("teammate public: %s", r)
	}
	if r := owner.Do("POST", "/api/platform/docs/plan/access", map[string]bool{"public": true}); r.Code != 200 || !strings.Contains(r.String(), "https://docs.example.com/p/plan") {
		t.Fatalf("owner public: %s", r)
	}
	// The page carries the hub's frame script and a sandbox.
	r = owner.Do("GET", "/api/platform/docs/plan/content", nil)
	if r.Code != 200 || !strings.Contains(r.String(), "data-hub-doc-frame") {
		t.Fatalf("content: %s", r)
	}
	// the viewer frames this page on its own origin (the hub's, or a gateway's that proxies it), so the hub's
	// blanket X-Frame-Options: DENY must not reach it: the browser answered the frame with «refused to connect».
	if r.Header.Get("X-Frame-Options") != "" || !strings.Contains(r.Header.Get("Content-Security-Policy"), "frame-ancestors 'self'") ||
		!strings.Contains(r.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("content headers: %v", r.Header)
	}
	if r := owner.Do("PUT", "/api/platform/docs/plan/source/block", map[string]any{}); r.Code != http.StatusNotImplemented {
		t.Fatalf("block: %s", r)
	}
}

// Without the services the agent is told, not left to wait for a timeout.
func TestDocsAgentToldWhenNotConfigured(t *testing.T) {
	s := docsStack(t)
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Acme")
	a := owner.Machine(id, nil)
	a.Emit(map[string]any{"event": "doc.publish", "requestId": "r1", "title": "Plan", "format": "md", "content": "# Plan"})
	s.Eventually("doc.result", func() bool {
		for _, c := range a.Calls("doc.result") {
			return string(c.Params["status"]) == "503"
		}
		return false
	})
}

// A document bound to a conversation: a person's comment reaches that conversation through the machine, the agent's
// answer lands in the thread, and a board's binding is not the hub's.
func TestDocsCommentsReachTheConversation(t *testing.T) {
	_, srv := newFakeArtifacts(t)
	s := docsStack(t, hubtest.Env("HUB_DOCS", "on"), hubtest.Env("HUB_ARTIFACTS_URL", srv.URL), hubtest.Env("HUB_ARTIFACTS_TOKEN", "secret-token"))
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Acme")
	a := owner.Machine(id, nil)
	a.Handle("doc.result", func(hubtest.Call) (any, error) { return map[string]bool{"settled": true}, nil })
	a.Handle("docs.deliver", func(hubtest.Call) (any, error) { return map[string]string{"mode": "typed"}, nil })

	a.Emit(map[string]any{"event": "doc.publish", "requestId": "r1", "title": "Plan", "format": "md", "content": "# Plan", "slug": "plan", "visibility": "private"})
	s.Eventually("doc.result for publish", func() bool { return len(a.Calls("doc.result")) > 0 })

	// A board's binding is not the hub's; the document's binding comes through the event the agent sends after publish.
	a.Emit(map[string]any{"event": "docs.bound", "binding": map[string]any{"slug": "b1", "kind": "board", "title": "Board", "tabId": "tab-9"}})
	a.Emit(map[string]any{"event": "docs.bound", "binding": map[string]any{"slug": "plan", "kind": "artifact", "title": "Plan", "tabId": "tab-1", "cwd": "/work"}})
	// The binding is stored in the background: comment until one reaches the conversation.
	var got hubtest.Call
	s.Eventually("comment delivered", func() bool {
		if r := owner.Do("POST", "/api/platform/docs/plan/threads", map[string]any{"anchor": map[string]string{"quote": "Plan"}, "body": "Why?"}); r.Code != http.StatusCreated {
			t.Fatalf("new thread: %s", r)
		}
		time.Sleep(50 * time.Millisecond)
		for _, c := range a.Calls("docs.deliver") {
			got = c
			return true
		}
		return false
	})
	for _, c := range a.Calls("docs.deliver") {
		if c.Str("tabId") == "tab-9" {
			t.Fatalf("a board's binding reached the hub: %v", c.Params)
		}
	}
	if got.Str("tabId") != "tab-1" || got.Str("cwd") != "/work" || !strings.Contains(got.Str("message"), "Комментарий к твоему документу") ||
		!strings.Contains(got.Str("message"), "/docs/plan") || !strings.Contains(got.Str("message"), "Why?") {
		t.Fatalf("deliver: %s", got.Params["message"])
	}
	var doc struct {
		Threads []struct {
			ID       string
			Comments []struct {
				Body        string
				DeliveredAt *string `json:"deliveredAt"`
			}
		}
	}
	s.Eventually("delivered mark", func() bool {
		owner.Do("GET", "/api/platform/docs/plan", nil).JSON(&doc)
		for _, th := range doc.Threads {
			for _, c := range th.Comments {
				if c.Body == "Why?" && c.DeliveredAt != nil {
					return true
				}
			}
		}
		return false
	})

	// The agent answers in the thread and resolves it.
	thread := ""
	for _, th := range doc.Threads {
		if len(th.Comments) > 0 && th.Comments[0].Body == "Why?" {
			thread = th.ID
		}
	}
	a.Emit(map[string]any{"event": "docs.reply", "threadId": thread, "body": "Fixed it", "resolve": true})
	s.Eventually("agent reply", func() bool {
		r := owner.Do("GET", "/api/platform/docs/plan", nil)
		return strings.Contains(r.String(), "Fixed it") && strings.Contains(r.String(), "resolved")
	})
	// The agent's own answer is not handed back to the conversation.
	time.Sleep(100 * time.Millisecond)
	for _, c := range a.Calls("docs.deliver") {
		if strings.Contains(c.Str("message"), "Fixed it") && !strings.Contains(c.Str("message"), "Раньше") {
			t.Fatalf("the agent's reply went back to it: %v", c.Params)
		}
	}
}

package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// fakeCatalog answers the artifacts routes the reader uses and records the tokens it saw.
func fakeCatalog(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var auth []string
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("GET /api/v1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		reply(w, map[string]any{"items": []map[string]any{
			{"slug": "plan-1", "title": "Plan", "session": "tab-1", "updatedAt": "2026-10-20T10:00:00Z", "viewUrl": "https://docs.example.com/a/plan-1"},
			{"slug": "notes", "title": "Notes", "createdAt": "2026-10-19T10:00:00Z", "viewUrl": "https://docs.example.com/a/notes"},
		}})
	})
	mux.HandleFunc("GET /api/v1/artifacts/plan-1", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"artifact": map[string]any{"slug": "plan-1", "title": "Plan", "session": "tab-1", "viewUrl": "https://docs.example.com/a/plan-1"}})
	})
	mux.HandleFunc("GET /api/v1/artifacts/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		reply(w, map[string]string{"error": "not_found"})
	})
	mux.HandleFunc("GET /api/v1/artifacts/plan-1/versions", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"versions": []map[string]any{
			{"n": 2, "id": "b2b2b2b2", "createdAt": "2026-10-20T10:00:00Z", "author": "ada", "note": "more", "current": true},
			{"n": 1, "id": "a1a1a1a1", "createdAt": "2026-10-19T10:00:00Z", "author": nil, "note": nil, "current": false},
		}})
	})
	mux.HandleFunc("GET /api/v1/artifacts/plan-1/diff", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("from") != "1" || r.URL.Query().Get("to") != "2" {
			t.Errorf("diff query %s", r.URL.RawQuery)
		}
		reply(w, map[string]any{"from": map[string]any{"n": 1, "id": "a1"}, "to": map[string]any{"n": 2, "id": "b2"}, "blocks": []map[string]string{
			{"kind": "same", "text": "a"}, {"kind": "removed", "text": "b"}, {"kind": "added", "text": "c"}, {"kind": "added", "text": "d"},
		}})
	})
	mux.HandleFunc("GET /api/v1/artifacts/plan-1/access", func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		reply(w, map[string]any{"public": false, "readLink": map[string]string{"url": "https://docs.example.com/s/tok", "expiresAt": "2026-10-20T10:15:00Z"}})
	})
	mux.HandleFunc("PUT /api/v1/artifacts/plan-1/access", func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		var body map[string]bool
		_ = json.NewDecoder(r.Body).Decode(&body)
		reply(w, map[string]any{"public": body["public"], "publicUrl": "https://docs.example.com/s/share"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &auth
}

func TestDocsFromTheCatalog(t *testing.T) {
	srv, auth := fakeCatalog(t)
	c := Config{ArtifactsURL: srv.URL, ArtifactsToken: "editor", ArtifactsReadToken: "viewer", ArtifactsShareToken: "sharer"}
	ctx := context.Background()

	docs, err := c.ListDocs(ctx, 50)
	if err != nil || len(docs) != 2 {
		t.Fatalf("list: %v %+v", err, docs)
	}
	if d := docs[0]; d.Slug != "plan-1" || d.Kind != "artifact" || !d.Bound || d.URL != "https://docs.example.com/a/plan-1" || d.LastActivityAt != "2026-10-20T10:00:00Z" {
		t.Fatalf("first %+v", d)
	}
	if d := docs[1]; d.Bound || d.LastActivityAt != "2026-10-19T10:00:00Z" {
		t.Fatalf("an unbound document falls back to its creation time: %+v", d)
	}

	doc, err := c.Doc(ctx, "plan-1")
	if err != nil || doc.Title != "Plan" {
		t.Fatalf("doc: %v %+v", err, doc)
	}
	var ue *UpstreamError
	if _, err := c.Doc(ctx, "missing"); !errors.As(err, &ue) || ue.Status != 404 {
		t.Fatalf("missing: %v", err)
	}

	versions, err := c.DocVersions(ctx, "plan-1")
	if err != nil || len(versions) != 2 || versions[0].SHA != "b2b2b2b2" || !versions[0].Current || versions[1].Author != nil {
		t.Fatalf("versions: %v %+v", err, versions)
	}

	diff, err := c.DocDiff(ctx, "plan-1", "1", "2")
	if err != nil || diff.From != "a1" || diff.To != "b2" || diff.Stats.Added != 2 || diff.Stats.Removed != 1 {
		t.Fatalf("diff: %v %+v", err, diff)
	}

	link, at, err := c.DocReadLink(ctx, "plan-1")
	if err != nil || link != "https://docs.example.com/s/tok" || !at.Equal(time.Date(2026, 10, 20, 10, 15, 0, 0, time.UTC)) {
		t.Fatalf("read link: %v %q %v", err, link, at)
	}

	open := true
	access, err := c.DocAccessOf(ctx, "plan-1", &open)
	if err != nil || !access.Public || access.PublicURL == nil || *access.PublicURL != "https://docs.example.com/s/share" {
		t.Fatalf("access: %v %+v", err, access)
	}
	want := []string{"Bearer viewer", "Bearer viewer", "Bearer sharer"}
	if len(*auth) != len(want) {
		t.Fatalf("tokens %v", *auth)
	}
	for i := range want {
		if (*auth)[i] != want[i] {
			t.Fatalf("token %d: %q, want %q (the viewer reads, the sharer shares)", i, (*auth)[i], want[i])
		}
	}

	if _, err := (Config{}).ListDocs(ctx, 1); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no catalog: %v", err)
	}
}

func TestThreads(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	th := &Threads{Path: filepath.Join(t.TempDir(), "doc-threads.json"), Now: func() time.Time { return now }}
	ada := DocAuthor{Kind: "person", Email: "ada@example.com", Name: "Ada"}
	bob := DocAuthor{Kind: "person", Email: "bob@example.com", Name: "Bob"}

	thread, err := th.Create("plan-1", DocAnchor{Quote: "the plan"}, "Why?", ada)
	if err != nil || thread.Status != "open" || len(thread.Comments) != 1 || len(thread.ID) != 16 {
		t.Fatalf("create: %v %+v", err, thread)
	}
	now = now.Add(time.Minute)
	if _, err := th.Reply("plan-1", thread.ID, "Because.", bob); err != nil {
		t.Fatal(err)
	}
	open, unread, last, err := th.Counts(ada.Email)
	if err != nil || open["plan-1"] != 1 || unread["plan-1"] != 1 || last["plan-1"] != "2026-10-20T09:01:00Z" {
		t.Fatalf("counts for Ada: %v %v %v %v", err, open, unread, last)
	}
	now = now.Add(time.Minute)
	if err := th.MarkRead(ada.Email, "plan-1"); err != nil {
		t.Fatal(err)
	}
	if _, unread, _, _ := th.Counts(ada.Email); unread["plan-1"] != 0 {
		t.Fatalf("read: %v", unread)
	}
	resolved, err := th.SetStatus("plan-1", thread.ID, "resolved")
	if err != nil || resolved.Status != "resolved" || resolved.ResolvedAt == nil {
		t.Fatalf("resolve: %v %+v", err, resolved)
	}
	if open, _, _, _ := th.Counts(ada.Email); open["plan-1"] != 0 {
		t.Fatalf("open after resolve: %v", open)
	}
	if _, err := th.Reply("other", thread.ID, "x", ada); !errors.Is(err, ErrNoThread) {
		t.Fatalf("a thread of another document: %v", err)
	}
	list, err := th.Of("plan-1")
	if err != nil || len(list) != 1 || len(list[0].Comments) != 2 {
		t.Fatalf("of: %v %+v", err, list)
	}
}

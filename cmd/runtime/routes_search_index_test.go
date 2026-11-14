package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
)

// a runtime with no hub answers ⌘K from its own embedded index: a project made after the first build is found
// by the next question, with nothing read from the library at the time of the question beyond what changed.
func TestSearchWithoutHubUsesTheEmbeddedIndex(t *testing.T) {
	lib := design.New(design.Config{LibraryDir: t.TempDir(), DataDir: t.TempDir(), Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	s := newRuntimeSearch(t.TempDir(), lib)
	if s == nil {
		t.Fatal("no index")
	}
	routes := s.routes(http.NotFoundHandler())
	ask := func(q string) []map[string]any {
		req := httptest.NewRequest(http.MethodGet, platformAPI+"/search?q="+url.QueryEscape(q), nil)
		if !routes[0].Match(req) {
			t.Fatal("GET /search is not matched")
		}
		rec := httptest.NewRecorder()
		routes[0].Handler.ServeHTTP(rec, req)
		var out struct{ Hits []map[string]any }
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%v: %s", err, rec.Body)
		}
		return out.Hits
	}
	if len(ask("лендинг")) != 0 {
		t.Fatal("found something in an empty library")
	}
	if _, err := lib.CreateProject("Лендинг кофейни"); err != nil {
		t.Fatal(err)
	}
	var hits []map[string]any
	for i := 0; i < 20 && len(hits) == 0; i++ {
		hits = ask("лендинг")
		time.Sleep(100 * time.Millisecond)
	}
	if len(hits) != 1 || hits[0]["kind"] != "design" {
		t.Fatalf("hits: %v", hits)
	}
}

// A machine of a hub's space is shared: ⌘K shows another person of it only the projects they may open, as the
// library's list does, though the index holds every project of the machine.
func TestSearchShowsAPersonOnlyTheirProjects(t *testing.T) {
	lib := design.New(design.Config{LibraryDir: t.TempDir(), DataDir: t.TempDir(), Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	open, err := lib.CreateProject("Лендинг общий")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := lib.CreateProject("Лендинг личный")
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SetAccess(design.AccessSnapshot{
		Enforce:    true,
		At:         time.Now(),
		Projects:   map[string]design.Grant{open.ID: {Role: "owner"}, closed.ID: {Role: "owner"}},
		People:     map[string]design.PersonSnapshot{"guest@example.com": {Projects: map[string]design.Grant{open.ID: {Role: "view"}}}},
		Unrecorded: design.UnrecordedClosed,
	}); err != nil {
		t.Fatal(err)
	}
	s := newRuntimeSearch(t.TempDir(), lib)
	if s == nil {
		t.Fatal("no index")
	}
	s.person = func(r *http.Request) string { return r.Header.Get("X-Test-Person") }
	routes := s.routes(http.NotFoundHandler())
	ask := func(person string) []string {
		req := httptest.NewRequest(http.MethodGet, platformAPI+"/search?q="+url.QueryEscape("лендинг"), nil)
		req.Header.Set("X-Test-Person", person)
		rec := httptest.NewRecorder()
		routes[0].Handler.ServeHTTP(rec, req)
		var out struct{ Hits []struct{ ID string } }
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%v: %s", err, rec.Body)
		}
		ids := []string{}
		for _, h := range out.Hits {
			ids = append(ids, h.ID)
		}
		return ids
	}
	// The index is built in the background: wait for both projects in it.
	var got []string
	for i := 0; i < 50 && len(got) != 2; i++ {
		if got = ask(""); len(got) != 2 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if len(got) != 2 {
		t.Fatalf("the machine's own person: %v", got)
	}
	if got := ask("guest@example.com"); len(got) != 1 || got[0] != open.ID {
		t.Fatalf("the guest: %v, want only %s", got, open.ID)
	}
	if got := ask("stranger@example.com"); len(got) != 0 {
		t.Fatalf("a person with no role: %v", got)
	}
}

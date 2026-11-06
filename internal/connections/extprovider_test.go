package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

// fakeAdapter is a tracker on example.com as an adapter of the connection protocol.
func fakeAdapter(t *testing.T, caps []string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("GET /v1/describe", func(w http.ResponseWriter, r *http.Request) {
		reply(w, ext.Description{Protocol: ConnectionProtocol, Name: "example-tracker", Version: "1", Capabilities: caps})
	})
	mux.HandleFunc("POST /v1/detect", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ URL string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !strings.Contains(in.URL, "tracker.example.com") {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		reply(w, Detection{BaseURL: "https://tracker.example.com/", Deployment: SelfHosted, Title: "Example tracker",
			Methods:      []Method{{ID: "pat", Fields: []Field{{ID: FieldToken, Secret: true}}}},
			Capabilities: []Capability{Issues}})
	})
	mux.HandleFunc("POST /v1/link", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ URL string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if i := strings.Index(in.URL, "/issue/"); i >= 0 {
			reply(w, map[string]any{"found": true, "kind": "issue", "key": in.URL[i+7:]})
			return
		}
		reply(w, map[string]any{"found": false})
	})
	body := func(r *http.Request) map[string]any {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		seen = append(seen, r.URL.Path)
		return in
	}
	authed := func(in map[string]any) bool {
		c, _ := in["credentials"].(map[string]any)
		return c["token"] == "s3cret"
	}
	mux.HandleFunc("POST /v1/me", func(w http.ResponseWriter, r *http.Request) {
		if !authed(body(r)) {
			http.Error(w, `{"error":"denied"}`, http.StatusUnauthorized)
			return
		}
		reply(w, Person{Login: "ann", Name: "Ann"})
	})
	mux.HandleFunc("POST /v1/projects", func(w http.ResponseWriter, r *http.Request) {
		body(r)
		reply(w, map[string]any{"projects": []Project{{Key: "EX", Name: "Example"}}})
	})
	mux.HandleFunc("POST /v1/issues/search", func(w http.ResponseWriter, r *http.Request) {
		in := body(r)
		q, _ := in["query"].(map[string]any)
		reply(w, map[string]any{"issues": []Issue{{Key: "EX-1", Title: "found " + q["text"].(string), Status: Status{Name: "Open", Category: Todo}}}})
	})
	mux.HandleFunc("POST /v1/issues/get", func(w http.ResponseWriter, r *http.Request) {
		in := body(r)
		if in["key"] != "EX-1" {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		reply(w, Issue{Key: "EX-1", Title: "One", URL: "https://tracker.example.com/issue/EX-1", Description: "text"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &seen
}

func withRegistry(t *testing.T) {
	t.Helper()
	saved := Providers()
	t.Cleanup(func() { registry = saved })
}

func register(t *testing.T, srv *httptest.Server) *ExtProvider {
	t.Helper()
	c, err := ext.NewClient(ext.Config{BaseURL: srv.URL, Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	p, err := RegisterAdapter(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAdapterEndToEnd(t *testing.T) {
	withRegistry(t)
	before := len(Providers())
	srv, seen := fakeAdapter(t, []string{"detect", "link", "issues"})
	p := register(t, srv)
	if len(Providers()) != before+1 || ProviderOf("example-tracker") != Provider(p) {
		t.Fatal("the adapter is not registered after the built-in providers")
	}

	u, _ := url.Parse("https://tracker.example.com/issue/EX-1")
	d, err := p.Detect(context.Background(), nil, u)
	if err != nil || d == nil || d.Kind != "example-tracker" || d.BaseURL == "" {
		t.Fatalf("detect: %+v %v", d, err)
	}
	if d, err := p.Detect(context.Background(), nil, &url.URL{Scheme: "https", Host: "other.example.com"}); d != nil || err != nil {
		t.Fatalf("a foreign link must be (nil, nil): %+v %v", d, err)
	}

	conn := Conn{ID: "c1", Kind: "example-tracker", BaseURL: "https://tracker.example.com"}
	if tg, ok := p.Link(conn, u); !ok || tg.Kind != "issue" || tg.Key != "EX-1" {
		t.Fatalf("link: %+v %v", tg, ok)
	}
	if _, ok := p.Link(conn, &url.URL{Scheme: "https", Host: "tracker.example.com", Path: "/about"}); ok {
		t.Fatal("a page that is no issue must not be a link")
	}

	cl := p.Open(conn, Credentials{"token": "s3cret"}, nil)
	if me, err := cl.Me(context.Background()); err != nil || me.Login != "ann" {
		t.Fatalf("me: %+v %v", me, err)
	}
	src := cl.(IssueSource)
	if ps, err := src.Projects(context.Background()); err != nil || len(ps) != 1 {
		t.Fatalf("projects: %v %v", ps, err)
	}
	if is, err := src.Search(context.Background(), Query{Text: "bug"}); err != nil || len(is) != 1 || is[0].Title != "found bug" {
		t.Fatalf("search: %+v %v", is, err)
	}
	if is, err := src.Issue(context.Background(), "EX-1"); err != nil || is.Description != "text" {
		t.Fatalf("issue: %+v %v", is, err)
	}
	if _, err := src.Issue(context.Background(), "EX-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing issue must be ErrNotFound: %v", err)
	}
	if _, err := p.Open(conn, Credentials{"token": "wrong"}, nil).Me(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatalf("a refusal must be ErrDenied: %v", err)
	}
	if len(*seen) == 0 {
		t.Fatal("the adapter was never called")
	}
}

func TestAdapterWithoutCapabilityIsNotCalled(t *testing.T) {
	withRegistry(t)
	srv, seen := fakeAdapter(t, []string{"detect", "issues"})
	p := register(t, srv)
	conn := Conn{ID: "c1", BaseURL: "https://tracker.example.com"}
	if _, ok := p.Link(conn, &url.URL{Scheme: "https", Host: "tracker.example.com", Path: "/issue/EX-1"}); ok {
		t.Fatal("link is not listed, so it must not answer")
	}
	cl := p.Open(conn, Credentials{}, nil).(PageSource)
	if _, err := cl.SearchPages(context.Background(), Query{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("pages are not listed: %v", err)
	}
	if len(*seen) != 0 {
		t.Fatalf("calls left the process: %v", *seen)
	}
}

func TestAdapterKindOfBuiltinIsRefused(t *testing.T) {
	withRegistry(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/describe", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ext.Description{Protocol: ConnectionProtocol, Name: "gitlab", Capabilities: []string{"detect"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := ext.NewClient(ext.Config{BaseURL: srv.URL, Attempts: 1})
	n := len(Providers())
	if _, err := RegisterAdapter(context.Background(), c); !errors.Is(err, ErrKindTaken) {
		t.Fatalf("want ErrKindTaken, got %v", err)
	}
	if len(Providers()) != n {
		t.Fatal("a refused adapter was registered")
	}
}

func TestAdapterOfAnotherProtocolIsRefused(t *testing.T) {
	withRegistry(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/describe", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ext.Description{Protocol: "harnessly.machine-provider/v1", Name: "x"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := ext.NewClient(ext.Config{BaseURL: srv.URL, Attempts: 1})
	if _, err := RegisterAdapter(context.Background(), c); err == nil {
		t.Fatal("an adapter of another protocol must be refused")
	}
}

func TestLoadAdaptersDeclaredOnly(t *testing.T) {
	withRegistry(t)
	n := len(Providers())
	if k, errs := LoadAdapters(context.Background(), ""); k != nil || errs != nil || len(Providers()) != n {
		t.Fatal("without a file nothing changes")
	}
	srv, _ := fakeAdapter(t, []string{"detect", "issues"})
	dir := t.TempDir()
	file := filepath.Join(dir, "adapters.json")
	spec := `[{"name":"example-tracker","kind":"connection","url":"` + srv.URL + `","protocol":"harnessly.connection/v1"},
	 {"name":"down","kind":"connection","url":"http://127.0.0.1:1","protocol":"harnessly.connection/v1","timeoutSeconds":1},
	 {"name":"machines","kind":"machine-provider","url":"http://127.0.0.1:1","protocol":"harnessly.machine-provider/v1"}]`
	if err := os.WriteFile(file, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	// The manifest has no field a secret could go into.
	if strings.Contains(strings.ToLower(spec), "token\":") {
		t.Fatal("test spec must not carry a token")
	}
	kinds, errs := LoadAdapters(context.Background(), file)
	if len(kinds) != 1 || kinds[0] != "example-tracker" {
		t.Fatalf("kinds %v", kinds)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "down") {
		t.Fatalf("a silent adapter must be one error: %v", errs)
	}
	if len(Providers()) != n+1 {
		t.Fatal("only the working connection adapter is registered")
	}
	if _, errs := LoadAdapters(context.Background(), filepath.Join(dir, "none.json")); len(errs) != 1 {
		t.Fatal("a missing file is one error")
	}
}

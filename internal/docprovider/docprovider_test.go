package docprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

func fakeAdapter(t *testing.T, caps []string) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/describe", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ext.Description{Protocol: Protocol, Name: "example-docs", Version: "1", Capabilities: caps})
	})
	reply := func(path string, status int, body any) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
		})
	}
	reply("/v1/list", 200, map[string]any{"docs": []Summary{{Slug: "plan", Kind: "artifact", Title: "Plan", URL: "https://docs.example.com/plan", OpenThreads: 2}}})
	reply("/v1/get", 200, Summary{Slug: "plan", Title: "Plan"})
	reply("/v1/read", 200, Content{Format: "md", Content: "# Plan", Version: "3"})
	reply("/v1/embed", 200, map[string]any{"url": "https://docs.example.com/embed/plan"})
	reply("/v1/publish", 200, Published{Slug: "plan", Version: 4, URL: "https://docs.example.com/plan"})
	reply("/v1/access", 200, Access{Public: false})
	reply("/v1/access/set", 200, Access{Public: true, Grants: []Grant{{Subject: "team@example.com", Role: "viewer"}}})
	reply("/v1/threads", 200, map[string]any{"threads": []Thread{{ID: "0123456789abcdef", Slug: "plan", Status: "open"}}})
	reply("/v1/threads/status", 200, Thread{ID: "0123456789abcdef", Status: "resolved"})
	reply("/v1/boards", 200, map[string]any{"boards": []Board{{Slug: "arch", Title: "Architecture"}}})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

func register(t *testing.T, caps []string) (*ExtProvider, *[]string) {
	t.Helper()
	srv, calls := fakeAdapter(t, caps)
	c, err := ext.NewClient(ext.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Register(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return p, calls
}

func TestExtProviderWholeContract(t *testing.T) {
	p, _ := register(t, []string{"read", "publish", "access", "comments", "boards"})
	ctx := context.Background()
	docs, err := p.List(ctx, 10)
	if err != nil || len(docs) != 1 || docs[0].OpenThreads != 2 {
		t.Fatalf("list: %+v %v", docs, err)
	}
	if c, err := p.Read(ctx, "plan", ""); err != nil || c.Content != "# Plan" || c.Version != "3" {
		t.Fatalf("read: %+v %v", c, err)
	}
	if e, err := p.Embed(ctx, "plan"); err != nil || e.URL == "" {
		t.Fatalf("embed: %+v %v", e, err)
	}
	if r, err := p.Publish(ctx, PublishRequest{Title: "Plan", Format: "md", Content: "x"}); err != nil || r.Version != 4 {
		t.Fatalf("publish: %+v %v", r, err)
	}
	yes := true
	if a, err := p.SetAccess(ctx, "plan", AccessChange{Public: &yes}); err != nil || !a.Public || len(a.Grants) != 1 {
		t.Fatalf("access: %+v %v", a, err)
	}
	if th, err := p.Threads(ctx, "plan"); err != nil || len(th) != 1 {
		t.Fatalf("threads: %+v %v", th, err)
	}
	if th, err := p.SetStatus(ctx, "plan", "0123456789abcdef", "resolved"); err != nil || th.Status != "resolved" {
		t.Fatalf("close: %+v %v", th, err)
	}
	if b, err := p.Boards(ctx); err != nil || len(b) != 1 {
		t.Fatalf("boards: %+v %v", b, err)
	}
}

func TestExtProviderDoesNotSendWhatItDoesNotList(t *testing.T) {
	p, calls := register(t, []string{"read"})
	ctx := context.Background()
	if _, err := p.Publish(ctx, PublishRequest{Title: "x"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("publish: %v", err)
	}
	if _, err := p.Reply(ctx, "plan", "0123456789abcdef", "hi", Author{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("reply: %v", err)
	}
	if th, err := p.Threads(ctx, "plan"); err != nil || len(th) != 0 {
		t.Fatalf("threads: %+v %v", th, err)
	}
	if err := p.MarkRead(ctx, "me@example.com", "plan"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls leaked: %v", *calls)
	}
}

func TestExtProviderNotFoundIsUpstream404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/describe" {
			_ = json.NewEncoder(w).Encode(ext.Description{Protocol: Protocol, Name: "example-docs", Capabilities: []string{"read"}})
			return
		}
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	}))
	defer srv.Close()
	c, _ := ext.NewClient(ext.Config{BaseURL: srv.URL})
	p, err := Register(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Get(context.Background(), "none")
	var ue *integrations.UpstreamError
	if !errors.As(err, &ue) || ue.Status != 404 {
		t.Fatalf("got %v", err)
	}
}

func TestRegisterNeedsRead(t *testing.T) {
	srv, _ := fakeAdapter(t, []string{"publish"})
	c, _ := ext.NewClient(ext.Config{BaseURL: srv.URL})
	if _, err := Register(context.Background(), c); err == nil {
		t.Fatal("an adapter without read was accepted")
	}
}

func TestLoadAdaptersSkipsOtherProtocols(t *testing.T) {
	srv, _ := fakeAdapter(t, []string{"read"})
	f := filepath.Join(t.TempDir(), "adapters.json")
	spec := `[{"name":"example-docs","kind":"docs","url":"` + srv.URL + `","protocol":"` + Protocol + `"},
	{"name":"other","kind":"memory","url":"http://127.0.0.1:1","protocol":"harnessly.memory/v1"}]`
	if err := os.WriteFile(f, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	got, errs := LoadAdapters(context.Background(), f)
	if len(errs) != 0 || len(got) != 1 || got[0].Name() != "example-docs" {
		t.Fatalf("%v %v", got, errs)
	}
	if got, errs := LoadAdapters(context.Background(), ""); got != nil || errs != nil {
		t.Fatal("empty path must load nothing")
	}
}

func TestBuiltinWithoutCatalogIsAsBefore(t *testing.T) {
	b := Builtin{Store: &integrations.Threads{Path: filepath.Join(t.TempDir(), "t.json")}}
	ctx := context.Background()
	if docs, err := b.List(ctx, 10); err != nil || docs == nil || len(docs) != 0 {
		t.Fatalf("list: %v %v", docs, err)
	}
	if _, err := b.Get(ctx, "x"); !errors.Is(err, integrations.ErrNotConfigured) {
		t.Fatalf("get: %v", err)
	}
	if _, err := b.Boards(ctx); !errors.Is(err, integrations.ErrNotConfigured) {
		t.Fatalf("boards: %v", err)
	}
	if _, err := b.Publish(ctx, PublishRequest{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("publish: %v", err)
	}
	if _, err := b.SetAccess(ctx, "x", AccessChange{Grants: &[]Grant{}}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("grants: %v", err)
	}
	th, err := b.CreateThread(ctx, "plan", Anchor{Quote: "q"}, "hello", Author{Kind: "person", Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := b.Threads(ctx, "plan"); len(list) != 1 || list[0].ID != th.ID {
		t.Fatalf("threads: %+v", list)
	}
	if _, err := b.SetStatus(ctx, "plan", th.ID, "resolved"); err != nil {
		t.Fatal(err)
	}
}

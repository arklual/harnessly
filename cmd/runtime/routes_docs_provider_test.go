package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ly-solutions/harnessly/internal/docprovider"
	"github.com/ly-solutions/harnessly/internal/docshare"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

// pinAdapter declares a documents adapter for the test (GitHub issues 26 and 27): it publishes under case-sensitive
// ids, and its boards only search.
func pinAdapter(t *testing.T, caps []string) *atomic.Int32 {
	t.Helper()
	var publishes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/describe":
			_ = json.NewEncoder(w).Encode(ext.Description{Protocol: docprovider.Protocol, Name: "example-docs", Version: "1", Capabilities: caps})
		case "/v1/publish":
			publishes.Add(1)
			_, _ = io.WriteString(w, `{"slug":"Doc_A1","version":1,"url":"https://docs.example.com/d/Doc_A1"}`)
		case "/v1/get":
			_, _ = io.WriteString(w, `{"slug":"Doc_A1","title":"Plan","url":"https://docs.example.com/d/Doc_A1"}`)
		case "/v1/boards/search":
			_, _ = io.WriteString(w, `{"boards":[{"slug":"Arch_1","title":"Architecture","url":"https://docs.example.com/app/board/Arch_1"}]}`)
		default:
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = io.WriteString(w, `{"error":"not_supported"}`)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := ext.NewClient(ext.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	p, err := docprovider.Register(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docprovider.Pin(p))
	return &publishes
}

func TestConversationDocumentsThroughAnAdapter(t *testing.T) {
	publishes := pinAdapter(t, []string{"read", "publish", "boards-search"})
	d := integrationDeps(false)
	d.DataDir = t.TempDir()
	routes := integrationRoutes(d, integrations.Config{})
	if routes == nil {
		t.Fatal("a declared adapter must serve the routes with nothing else configured")
	}
	page := http.Header{"Cookie": {"session=ok"}}
	doc := `{"session":"conv-1","title":"Plan","format":"md","content":"# Plan","requestId":"req-00000001"}`

	code, body := serveRoutes(routes, "POST", "/api/integrations/artifacts/documents", doc, page)
	alias := docprovider.Alias("Doc_A1")
	if code != 201 || !strings.Contains(body, `"slug":"`+alias+`"`) || !strings.Contains(body, `"externalId":"Doc_A1"`) {
		t.Fatalf("publish: %d %s", code, body)
	}
	if code, body := serveRoutes(routes, "POST", "/api/integrations/artifacts/documents", doc, page); code != 200 || publishes.Load() != 1 {
		t.Fatalf("repeat: %d %s, %d publishes", code, body, publishes.Load())
	}
	if code, body := serveRoutes(routes, "GET", "/api/integrations/artifacts/documents?session=conv-1", "", page); code != 200 ||
		!strings.Contains(body, `"slug":"`+alias+`"`) || !strings.Contains(body, `"provider":"example-docs"`) {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, body := serveRoutes(routes, "GET", "/api/integrations/artifacts/documents?session=conv-2", "", page); code != 200 ||
		body != `{"documents":[]}` {
		t.Fatalf("another conversation: %d %s", code, body)
	}
	if code, _ := serveRoutes(routes, "POST", "/api/integrations/artifacts/documents", `{"session":"c","title":"x","format":"md","content":"x","requestId":"no"}`, page); code != 400 {
		t.Fatalf("a short requestId: %d", code)
	}
	if code, body := serveRoutes(routes, "GET", "/api/integrations/boards", "", page); code != 200 || body != `{"boards":[],"searchRequired":true}` {
		t.Fatalf("boards: %d %s", code, body)
	}
	if code, body := serveRoutes(routes, "GET", "/api/integrations/boards?q=arch", "", page); code != 200 ||
		!strings.Contains(body, `"externalId":"Arch_1"`) || !strings.Contains(body, "/app/board/Arch_1") {
		t.Fatalf("board search: %d %s", code, body)
	}
}

func TestReadOnlyAdapterAnswersUnsupported(t *testing.T) {
	pinAdapter(t, []string{"read"})
	d := integrationDeps(false)
	d.DataDir = t.TempDir()
	routes := integrationRoutes(d, integrations.Config{})
	page := http.Header{"Cookie": {"session=ok"}}
	doc := `{"session":"conv-1","title":"Plan","format":"md","content":"# Plan","requestId":"req-00000001"}`
	if code, body := serveRoutes(routes, "POST", "/api/integrations/artifacts/documents", doc, page); code != 501 || !strings.Contains(body, `"code":"unsupported"`) {
		t.Fatalf("publish: %d %s", code, body)
	}
	if code, body := serveRoutes(routes, "GET", "/api/integrations/boards", "", page); code != 501 || !strings.Contains(body, `"code":"unsupported"`) {
		t.Fatalf("boards: %d %s", code, body)
	}
}

func TestSharedFileIsPublishedThroughTheAdapterOnce(t *testing.T) {
	publishes := pinAdapter(t, []string{"read", "publish"})
	store := docprovider.BindingsAt(t.TempDir())
	pub := providerPublisher(docprovider.For(integrations.Config{}, nil), store)
	x := docshare.Publication{Title: "Plan", Format: "md", Content: "# Plan", Path: "/work/plan.md", Conversation: "conv-1"}
	v, slug, fail := pub(x)
	if fail != nil || v != 1 || slug != docprovider.Alias("Doc_A1") {
		t.Fatalf("%d %q %+v", v, slug, fail)
	}
	if _, _, fail := pub(x); fail != nil || publishes.Load() != 1 {
		t.Fatalf("the same version was published again: %+v, %d", fail, publishes.Load())
	}
	if list, _ := store.Of("conv-1", "example-docs"); len(list) != 1 {
		t.Fatalf("bound: %+v", list)
	}
}

func TestSharedFileThroughAReadOnlyAdapterIs501(t *testing.T) {
	pinAdapter(t, []string{"read"})
	pub := providerPublisher(docprovider.For(integrations.Config{}, nil), docprovider.BindingsAt(t.TempDir()))
	if _, _, fail := pub(docshare.Publication{Title: "x", Format: "md", Content: "x", Conversation: "c"}); fail == nil || fail.Status != 501 {
		t.Fatalf("%+v", fail)
	}
}

package docprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

// adapterOf is an adapter answering each POST path with its handler; anything else is 501.
func adapterOf(t *testing.T, caps []string, max int64, routes map[string]http.HandlerFunc) *ExtProvider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/describe" {
			_ = json.NewEncoder(w).Encode(ext.Description{Protocol: Protocol, Name: "example-docs", Version: "1", Capabilities: caps})
			return
		}
		if h, ok := routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"error":"not_supported"}`))
	}))
	t.Cleanup(srv.Close)
	c, err := ext.NewClient(ext.Config{BaseURL: srv.URL, MaxAnswer: max})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Register(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func answer(body any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}

func slugOf(r *http.Request) string {
	var in struct {
		Slug string `json:"slug"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	return in.Slug
}

func TestAliasRoundTripsExternalIDs(t *testing.T) {
	for _, id := range []string{"plan", "Q3-Plan", "aGVsbG8_d29ybGQ-", "x-plan", "AbC123", "a/b c", "docs:42", "Ünïcode"} {
		a := Alias(id)
		if a == "" || !plainIDRe.MatchString(a) {
			t.Fatalf("%q: alias %q is no slug", id, a)
		}
		if back, ok := ExternalID(a); !ok || back != id {
			t.Fatalf("%q: alias %q comes back as %q %v", id, a, back, ok)
		}
	}
	if Alias("plan") != "plan" {
		t.Fatal("a slug that is an id must be its own alias")
	}
	if Alias(strings.Repeat("A", MaxExternalID+1)) != "" {
		t.Fatal("an id over the limit got an alias")
	}
	for _, s := range []string{"x-", "x-!!", "x-mfrgg===", "Plan", ""} {
		if _, ok := ExternalID(s); ok {
			t.Fatalf("%q was taken for an alias", s)
		}
	}
}

func TestExtProviderKeepsCaseSensitiveIDsAndTheirViewer(t *testing.T) {
	const raw = "Kx9_Zq-PLAN"
	var mu sync.Mutex
	var seen []string
	note := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, slugOf(r))
			mu.Unlock()
			h(w, r)
		}
	}
	view := "https://docs.example.com/app/board/" + raw
	p := adapterOf(t, []string{"read", "boards"}, 0, map[string]http.HandlerFunc{
		"/v1/list":   answer(map[string]any{"docs": []Summary{{Slug: raw, Kind: "board", Title: "Plan", URL: view}}}),
		"/v1/get":    note(answer(Summary{Slug: raw, Kind: "board", Title: "Plan", URL: view})),
		"/v1/read":   note(answer(Content{Format: "md", Content: "# Plan"})),
		"/v1/embed":  note(answer(map[string]any{"url": "https://docs.example.com/embed/" + raw})),
		"/v1/boards": answer(map[string]any{"boards": []Board{{Slug: raw, Title: "Plan", URL: view}}}),
	})
	ctx := context.Background()
	docs, err := p.List(ctx, 10)
	if err != nil || len(docs) != 1 {
		t.Fatalf("list: %+v %v", docs, err)
	}
	d := docs[0]
	if d.Slug != Alias(raw) || d.ExternalID != raw || d.URL != view || !d.ProviderEmbed {
		t.Fatalf("summary: %+v", d)
	}
	if s, err := p.Get(ctx, d.Slug); err != nil || s.Slug != d.Slug || s.URL != view {
		t.Fatalf("get: %+v %v", s, err)
	}
	if _, err := p.Read(ctx, d.Slug, ""); err != nil {
		t.Fatal(err)
	}
	if e, err := p.Embed(ctx, d.Slug); err != nil || !strings.HasSuffix(e.URL, raw) {
		t.Fatalf("embed: %+v %v", e, err)
	}
	for _, s := range seen {
		if s != raw {
			t.Fatalf("the adapter was asked for %q, not its own id", s)
		}
	}
	b, err := p.Boards(ctx)
	if err != nil || len(b) != 1 || b[0].Slug != Alias(raw) || b[0].ExternalID != raw || b[0].URL != view {
		t.Fatalf("boards: %+v %v", b, err)
	}
	if _, err := p.Get(ctx, "Not-A-Slug"); err == nil {
		t.Fatal("a string that is no alias reached the adapter")
	}
}

func TestReadOnlyAdapterRefusesWritesAsUnsupported(t *testing.T) {
	p := adapterOf(t, []string{"read"}, 0, nil)
	ctx := context.Background()
	if c := p.Capabilities(); c.Publish || c.Access || c.Comments || c.Boards {
		t.Fatalf("capabilities: %+v", c)
	}
	if _, err := p.Publish(ctx, PublishRequest{Title: "x", Format: "md", Content: "x"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("publish: %v", err)
	}
	yes := true
	if _, err := p.SetAccess(ctx, "plan", AccessChange{Public: &yes}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("access: %v", err)
	}
	store := BindingsAt(t.TempDir())
	if _, err := PublishToConversation(ctx, p, store, PublishRequest{Title: "x", Conversation: "c1"}, "k"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("publish to conversation: %v", err)
	}
}

func TestAdapterWithoutHistoryHasEmptyVersions(t *testing.T) {
	p := adapterOf(t, []string{"read"}, 0, nil)
	v, err := p.Versions(context.Background(), "plan")
	if err != nil || v == nil || len(v) != 0 {
		t.Fatalf("versions: %#v %v", v, err)
	}
}

func TestTooLargeAnswerIsRefusedNotCut(t *testing.T) {
	big := strings.Repeat("a", 4096)
	p := adapterOf(t, []string{"read"}, 1024, map[string]http.HandlerFunc{
		"/v1/read": answer(Content{Format: "md", Content: big}),
		"/v1/get": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte(`{"error":"too_large"}`))
		},
	})
	ctx := context.Background()
	if c, err := p.Read(ctx, "plan", ""); !errors.Is(err, ErrTooLarge) || c.Content != "" {
		t.Fatalf("read: %d bytes, %v", len(c.Content), err)
	}
	if _, err := p.Get(ctx, "plan"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("get: %v", err)
	}
	if got := p.Capabilities().MaxAnswerBytes; got != 1024 {
		t.Fatalf("max answer: %d", got)
	}
}

func TestSearchOnlyBoardsAdapter(t *testing.T) {
	var query string
	p := adapterOf(t, []string{"read", "boards-search"}, 0, map[string]http.HandlerFunc{
		"/v1/boards/search": func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Query string `json:"query"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			query = in.Query
			answer(map[string]any{"boards": []Board{{Slug: "Arch_1", Title: "Architecture"}}})(w, r)
		},
	})
	ctx := context.Background()
	if _, err := p.Boards(ctx); !errors.Is(err, ErrSearchRequired) {
		t.Fatalf("boards: %v", err)
	}
	b, err := p.SearchBoards(ctx, " arch ")
	if err != nil || len(b) != 1 || b[0].ExternalID != "Arch_1" || query != "arch" {
		t.Fatalf("search: %+v %v %q", b, err, query)
	}
	if c := p.Capabilities(); c.Boards || !c.BoardsSearch {
		t.Fatalf("capabilities: %+v", c)
	}
}

// publishingAdapter publishes under case-sensitive ids and counts the publishes.
func publishingAdapter(t *testing.T) (*ExtProvider, *int, *[]string) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	var keys []string
	p := adapterOf(t, []string{"read", "publish"}, 0, map[string]http.HandlerFunc{
		"/v1/publish": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			n++
			keys = append(keys, r.Header.Get("Idempotency-Key"))
			mu.Unlock()
			answer(Published{Slug: "Doc_A1", Version: 1, URL: "https://docs.example.com/d/Doc_A1"})(w, r)
		},
		"/v1/get": func(w http.ResponseWriter, r *http.Request) {
			if slugOf(r) == "Gone" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"not_found"}`))
				return
			}
			answer(Summary{Slug: "Doc_A1", Title: "Plan (renamed)", URL: "https://docs.example.com/d/Doc_A1"})(w, r)
		},
	})
	return p, &n, &keys
}

func TestConversationBindingsSurviveARestartAndStayInTheirConversation(t *testing.T) {
	dir := t.TempDir()
	p, n, keys := publishingAdapter(t)
	ctx := context.Background()
	req := PublishRequest{Title: "Plan", Format: "md", Content: "# Plan", Conversation: "conv-1"}
	b, err := PublishToConversation(ctx, p, BindingsAt(dir), req, "k1")
	if err != nil || b.Slug != Alias("Doc_A1") || b.ExternalID != "Doc_A1" || b.ID == "" {
		t.Fatalf("publish: %+v %v", b, err)
	}
	if len(*keys) != 1 || !strings.Contains((*keys)[0], "k1") {
		t.Fatalf("idempotency key: %v", *keys)
	}
	// A restart: a new store over the same file.
	again := &Bindings{Path: filepath.Join(dir, BindingsFile)}
	docs, err := ConversationDocuments(ctx, p, again, "conv-1")
	if err != nil || len(docs) != 1 || docs[0].Slug != b.Slug || docs[0].Title != "Plan (renamed)" || docs[0].Provider != "example-docs" {
		t.Fatalf("after restart: %+v %v", docs, err)
	}
	if other, err := ConversationDocuments(ctx, p, again, "conv-2"); err != nil || len(other) != 0 {
		t.Fatalf("another conversation sees %+v %v", other, err)
	}
	// The same key after the restart is the same operation.
	if b2, err := PublishToConversation(ctx, p, again, req, "k1"); err != nil || b2.ID != b.ID || *n != 1 {
		t.Fatalf("repeat: %+v %v, %d publishes", b2, err, *n)
	}
	// A new version of the same document keeps its binding.
	if b3, err := PublishToConversation(ctx, p, again, req, "k2"); err != nil || b3.ID != b.ID || *n != 2 {
		t.Fatalf("next version: %+v %v, %d publishes", b3, err, *n)
	}
}

func TestConversationDocumentsLeaveOutWhatTheAdapterLost(t *testing.T) {
	dir := t.TempDir()
	p, _, _ := publishingAdapter(t)
	store := BindingsAt(dir)
	if _, err := store.bind(Binding{Conversation: "c", Provider: "example-docs", Slug: Alias("Gone"), ExternalID: "Gone", Title: "Old"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	docs, err := ConversationDocuments(context.Background(), p, store, "c")
	if err != nil || len(docs) != 0 {
		t.Fatalf("%+v %v", docs, err)
	}
}

func TestBindFailureIsToldApartAndTheRetryDoesNotRepublish(t *testing.T) {
	dir := t.TempDir()
	p, n, _ := publishingAdapter(t)
	ctx := context.Background()
	store := BindingsAt(dir)
	orig := saveFile
	saveFile = func(string, []Binding) error { return errors.New("disk full") }
	req := PublishRequest{Title: "Plan", Format: "md", Content: "# Plan", Conversation: "conv-1"}
	_, err := PublishToConversation(ctx, p, store, req, "k1")
	saveFile = orig
	var be *BindError
	if !errors.As(err, &be) || be.Published.ExternalID != "Doc_A1" {
		t.Fatalf("bind failure: %v", err)
	}
	var pe *PublishError
	if errors.As(err, &pe) {
		t.Fatal("a bind failure was reported as a publish failure")
	}
	b, err := PublishToConversation(ctx, p, store, req, "k1")
	if err != nil || b.ExternalID != "Doc_A1" || *n != 1 {
		t.Fatalf("retry: %+v %v, %d publishes", b, err, *n)
	}
	if list, _ := store.Of("conv-1", "example-docs"); len(list) != 1 {
		t.Fatalf("bound: %+v", list)
	}
}

func TestPublishFailureIsAPublishError(t *testing.T) {
	p := adapterOf(t, []string{"read", "publish"}, 0, map[string]http.HandlerFunc{
		"/v1/publish": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"invalid","message":"no title"}`))
		},
	})
	store := BindingsAt(t.TempDir())
	_, err := PublishToConversation(context.Background(), p, store, PublishRequest{Title: "x", Conversation: "c"}, "k")
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("got %v", err)
	}
	if list, _ := store.Of("c", "example-docs"); len(list) != 0 {
		t.Fatalf("a failed publish was bound: %+v", list)
	}
	var ue *integrations.UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("the adapter's reason is lost: %v", err)
	}
}

func TestResolveNamesDocumentsAndBoardsByTheAdaptersID(t *testing.T) {
	no := false
	p := adapterOf(t, []string{"read", "resolve"}, 0, map[string]http.HandlerFunc{
		"/v1/resolve": func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				URL string `json:"url"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			switch in.URL {
			case "https://docs.example.com/app/board/Ab-Cd_9":
				answer(Resolved{Kind: "board", ID: "Ab-Cd_9"})(w, r)
			case "https://docs.example.com/wiki/Q3-Plan":
				answer(Resolved{Kind: "document", ID: "Q3-Plan", Embed: &no})(w, r)
			case "https://docs.example.com/private":
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"denied"}`))
			case "https://docs.example.com/junk":
				answer(Resolved{Kind: "folder", ID: "x"})(w, r)
			default:
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"not_found"}`))
			}
		},
	})
	ctx := context.Background()
	r, ok, err := p.Resolve(ctx, "https://docs.example.com/app/board/Ab-Cd_9")
	if err != nil || !ok || r.Kind != "board" || r.ID != "Ab-Cd_9" || !r.Embeddable() {
		t.Fatalf("board: %+v %v %v", r, ok, err)
	}
	if r, ok, err := p.Resolve(ctx, "https://docs.example.com/wiki/Q3-Plan"); err != nil || !ok || r.Kind != "document" || r.ID != "Q3-Plan" || r.Embeddable() {
		t.Fatalf("document that cannot be framed: %+v %v %v", r, ok, err)
	}
	if _, ok, err := p.Resolve(ctx, "https://docs.example.com/elsewhere"); ok || err != nil {
		t.Fatalf("an address the adapter does not claim: %v %v", ok, err)
	}
	if _, ok, err := p.Resolve(ctx, "https://docs.example.com/junk"); ok || err != nil {
		t.Fatalf("an unknown kind was taken: %v %v", ok, err)
	}
	if _, ok, err := p.Resolve(ctx, "https://docs.example.com/private"); ok || err == nil {
		t.Fatalf("denied: %v %v", ok, err)
	}
	// An adapter that does not list the capability is never asked.
	asked := false
	plain := adapterOf(t, []string{"read"}, 0, map[string]http.HandlerFunc{"/v1/resolve": func(http.ResponseWriter, *http.Request) { asked = true }})
	if _, ok, err := plain.Resolve(ctx, "https://docs.example.com/x"); ok || err != nil || asked {
		t.Fatalf("without the capability: %v %v asked=%v", ok, err, asked)
	}
}

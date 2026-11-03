package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func wikiJSServer(t *testing.T, prefix string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(400)
			secWrite(w, `{"errors":[{"message":"Must provide query string."}]}`)
			return
		}
		var b struct {
			Query     string
			Variables map[string]any
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		authed := r.Header.Get("Authorization") == "Bearer key"
		switch {
		case strings.Contains(b.Query, "__type"):
			secWrite(w, `{"data":{"a":{"name":"PageQuery"},"b":{"name":"SiteQuery"}}}`)
		case strings.Contains(b.Query, "site {"):
			secWrite(w, `{"data":{"site":{"config":{"title":"Acme Wiki"}}}}`)
		case !authed:
			secWrite(w, `{"errors":[{"message":"Forbidden","extensions":{"exception":{"code":6013}}}],"data":null}`)
		case strings.Contains(b.Query, "profile"):
			secWrite(w, `{"errors":[{"message":"You are not authorized to run this query."}],"data":{"users":null}}`)
		case strings.Contains(b.Query, "search("):
			secWrite(w, `{"data":{"pages":{"search":{"results":[{"id":"12","title":"Setup","path":"docs/setup","locale":"en","description":"how to"}],"totalHits":1}}}}`)
		case strings.Contains(b.Query, "list("):
			secWrite(w, `{"data":{"pages":{"list":[{"id":12,"path":"docs/setup","locale":"en","title":"Setup","description":"","updatedAt":"2026-10-02T10:00:00.000Z"}]}}}`)
		case strings.Contains(b.Query, "singleByPath"):
			if b.Variables["path"] == "docs/setup" && b.Variables["locale"] == "en" {
				secWrite(w, `{"data":{"pages":{"singleByPath":{"id":12,"path":"docs/setup","locale":"en","title":"Setup","content":"<p>Do it</p>","contentType":"html","authorName":"Ann","updatedAt":"2026-10-02T10:00:00.000Z"}}}}`)
				return
			}
			secWrite(w, `{"data":{"pages":{"singleByPath":null}}}`)
		case strings.Contains(b.Query, "single("):
			if b.Variables["id"] == float64(12) {
				secWrite(w, `{"data":{"pages":{"single":{"id":12,"path":"docs/setup","locale":"en","title":"Setup","content":"# Setup","contentType":"markdown","authorName":"Ann"}}}}`)
				return
			}
			secWrite(w, `{"data":{"pages":{"single":null}}}`)
		default:
			w.WriteHeader(400)
		}
	})
	return httptest.NewServer(mux)
}

func TestWikiJSDetect(t *testing.T) {
	srv := wikiJSServer(t, "/wiki")
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/wiki/en/docs/setup")
	d, err := (&wikiJSProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u)
	if err != nil || d == nil || d.BaseURL != srv.URL+"/wiki" || d.Title != "Acme Wiki" || d.Methods[0].TokenURL != srv.URL+"/wiki/a/api" {
		t.Fatalf("%+v %v", d, err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secWrite(w, `{"data":{"a":null,"b":null}}`) }))
	defer other.Close()
	ou, _ := url.Parse(other.URL)
	if d, _ := (&wikiJSProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), ou); d != nil {
		t.Fatalf("claimed %+v", d)
	}
}

func TestWikiJSClient(t *testing.T) {
	srv := wikiJSServer(t, "")
	defer srv.Close()
	conn := Conn{Kind: WikiJS, BaseURL: srv.URL, Method: "api-key"}
	p := &wikiJSProvider{}
	cl := p.Open(conn, Credentials{FieldToken: "key"}, NewHTTP(nil)).(*wikiJSClient)
	ctx := context.Background()
	if me, err := cl.Me(ctx); err != nil || me.Name != "API key" {
		t.Fatalf("%+v %v", me, err)
	}
	if _, err := p.Open(conn, Credentials{FieldToken: "bad"}, NewHTTP(nil)).Me(ctx); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	pages, err := cl.SearchPages(ctx, Query{Text: "setup"})
	if err != nil || len(pages) != 1 || pages[0].ID != "12" || pages[0].URL != srv.URL+"/en/docs/setup" || pages[0].Excerpt != "how to" {
		t.Fatalf("%+v %v", pages, err)
	}
	pages, err = cl.SearchPages(ctx, Query{})
	if err != nil || len(pages) != 1 || pages[0].Updated.IsZero() {
		t.Fatalf("%+v %v", pages, err)
	}
	pg, err := cl.Page(ctx, "12")
	if err != nil || pg.Body != "# Setup" || pg.Author.Name != "Ann" {
		t.Fatalf("%+v %v", pg, err)
	}
	pg, err = cl.Page(ctx, "en/docs/setup")
	if err != nil || pg.Body != "Do it" {
		t.Fatalf("%+v %v", pg, err)
	}
	if _, err := cl.Page(ctx, "en/nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if _, err := cl.Page(ctx, "99"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestWikiJSLink(t *testing.T) {
	p := &wikiJSProvider{}
	conn := Conn{BaseURL: "https://w.example/wiki"}
	u, _ := url.Parse("https://w.example/wiki/pt-br/docs/setup")
	if got, ok := p.Link(conn, u); !ok || got.Kind != "page" || got.Key != "pt-br/docs/setup" {
		t.Fatalf("%+v %v", got, ok)
	}
	for _, raw := range []string{"https://w.example/wiki/en", "https://w.example/wiki/e/en/docs/setup", "https://w.example/wiki/login/x", "https://w.example/en/docs/setup", "https://w.example/wiki/a/api"} {
		u, _ := url.Parse(raw)
		if _, ok := p.Link(conn, u); ok {
			t.Fatalf("matched %s", raw)
		}
	}
}

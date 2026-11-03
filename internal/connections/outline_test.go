package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func outlineServer(t *testing.T, prefix string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	need := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return false
		}
		if r.Header.Get("Authorization") != "Bearer ol_api_x" {
			w.WriteHeader(401)
			secWrite(w, `{"ok":false,"error":"authentication_required","status":401}`)
			return false
		}
		return true
	}
	mux.HandleFunc(prefix+"/api/auth.config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		secWrite(w, `{"ok":true,"status":200,"data":{"name":"Acme Docs","hostname":"x","providers":[{"id":"slack","name":"Slack","authUrl":"x"}]}}`)
	})
	mux.HandleFunc(prefix+"/api/auth.info", func(w http.ResponseWriter, r *http.Request) {
		if need(w, r) {
			secWrite(w, `{"ok":true,"data":{"user":{"id":"u1","name":"Ann","email":"ann@x.io","avatarUrl":"https://a"},"team":{"name":"Acme"}}}`)
		}
	})
	doc := `{"id":"3f1c","urlId":"AbC123xyz0","title":"Runbook","text":"# Runbook\n\nSteps","url":"/doc/runbook-AbC123xyz0","updatedAt":"2026-10-02T10:00:00.000Z","createdBy":{"id":"u1","name":"Ann"}}`
	mux.HandleFunc(prefix+"/api/documents.search", func(w http.ResponseWriter, r *http.Request) {
		if !need(w, r) {
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["query"] != "runbook" || b["limit"] != float64(25) {
			w.WriteHeader(400)
			return
		}
		secWrite(w, `{"ok":true,"data":[{"ranking":0.9,"context":"the <b>runbook</b> text","document":`+doc+`}]}`)
	})
	mux.HandleFunc(prefix+"/api/documents.info", func(w http.ResponseWriter, r *http.Request) {
		if !need(w, r) {
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["id"] != "AbC123xyz0" {
			w.WriteHeader(404)
			secWrite(w, `{"ok":false,"error":"not_found"}`)
			return
		}
		secWrite(w, `{"ok":true,"data":`+doc+`}`)
	})
	return httptest.NewServer(mux)
}

func TestOutlineDetect(t *testing.T) {
	srv := outlineServer(t, "/kb")
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/kb/doc/runbook-AbC123xyz0")
	d, err := (&outlineProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u)
	if err != nil || d == nil || d.BaseURL != srv.URL+"/kb" || d.Title != "Acme Docs" || d.Deployment != SelfHosted || d.Methods[0].TokenURL != srv.URL+"/kb/settings/tokens" {
		t.Fatalf("%+v %v", d, err)
	}
	// Cloud fast path needs no probing.
	cu, _ := url.Parse("https://acme.getoutline.com/doc/x-1")
	d, err = (&outlineProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), cu)
	if err != nil || d == nil || d.Deployment != Cloud || d.BaseURL != "https://acme.getoutline.com" {
		t.Fatalf("%+v %v", d, err)
	}
	// Another site answering 200 JSON to POST is not Outline.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secWrite(w, `{"ok":true,"data":{}}`) }))
	defer other.Close()
	ou, _ := url.Parse(other.URL)
	if d, _ := (&outlineProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), ou); d != nil {
		t.Fatalf("claimed %+v", d)
	}
}

func TestOutlineClient(t *testing.T) {
	srv := outlineServer(t, "")
	defer srv.Close()
	conn := Conn{Kind: Outline, BaseURL: srv.URL, Method: "api-key"}
	p := &outlineProvider{}
	cl := p.Open(conn, Credentials{FieldToken: "ol_api_x"}, NewHTTP(nil)).(*outlineClient)
	ctx := context.Background()
	me, err := cl.Me(ctx)
	if err != nil || me.Name != "Ann" || me.Email != "ann@x.io" {
		t.Fatalf("%+v %v", me, err)
	}
	if _, err := p.Open(conn, Credentials{FieldToken: "bad"}, NewHTTP(nil)).Me(ctx); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	pages, err := cl.SearchPages(ctx, Query{Text: "runbook"})
	if err != nil || len(pages) != 1 || pages[0].ID != "3f1c" || pages[0].Excerpt != "the runbook text" || pages[0].URL != srv.URL+"/doc/runbook-AbC123xyz0" {
		t.Fatalf("%+v %v", pages, err)
	}
	pg, err := cl.Page(ctx, "AbC123xyz0")
	if err != nil || pg.Title != "Runbook" || pg.Body != "# Runbook\n\nSteps" || pg.Author.Name != "Ann" || pg.Updated.IsZero() {
		t.Fatalf("%+v %v", pg, err)
	}
	if _, err := cl.Page(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestOutlineLink(t *testing.T) {
	p := &outlineProvider{}
	conn := Conn{BaseURL: "https://kb.example/kb"}
	u, _ := url.Parse("https://kb.example/kb/doc/my-runbook-AbC123xyz0?x=1")
	if got, ok := p.Link(conn, u); !ok || got.Kind != "page" || got.Key != "AbC123xyz0" {
		t.Fatalf("%+v %v", got, ok)
	}
	for _, raw := range []string{"https://kb.example/doc/x-1", "https://kb.example/kb/collection/x-1", "https://kb.example/kb/doc", "https://o.example/kb/doc/x-1"} {
		u, _ := url.Parse(raw)
		if _, ok := p.Link(conn, u); ok {
			t.Fatalf("matched %s", raw)
		}
	}
}

package connections

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func bookStackServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Token tid:tsecret" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"error":{"message":"No authorization token found on the request","code":401}}`))
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/bs/api/system", guard(func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `{"version":"v24.10.1","instance_id":"abc","app_name":"Acme Docs","base_url":"https://x"}`)
	}))
	mux.HandleFunc("/bs/api/search", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") != "backup {type:page}" {
			w.WriteHeader(400)
			return
		}
		secWrite(w, `{"data":[{"id":7,"book_id":2,"name":"Backups","slug":"backups","type":"page","url":"https://docs.x/bs/books/ops/page/backups","book":{"name":"Ops","slug":"ops"},"preview_content":{"name":"Backups","content":"nightly <b>backup</b> job"}},{"id":9,"type":"chapter","name":"Skip"}],"total":2}`)
	}))
	mux.HandleFunc("/bs/api/books", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter[slug]") == "ops" {
			secWrite(w, `{"data":[{"id":2,"slug":"ops"}],"total":1}`)
			return
		}
		secWrite(w, `{"data":[],"total":0}`)
	}))
	mux.HandleFunc("/bs/api/pages", guard(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("filter[book_id]") == "2" && q.Get("filter[slug]") == "backups" {
			secWrite(w, `{"data":[{"id":7}],"total":1}`)
			return
		}
		secWrite(w, `{"data":[],"total":0}`)
	}))
	mux.HandleFunc("/bs/api/pages/7", guard(func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `{"id":7,"book_id":2,"name":"Backups","slug":"backups","html":"<p>Nightly</p><ul><li>db</li></ul>","markdown":"","book_slug":"ops","updated_at":"2026-10-02T10:00:00.000000Z","created_by":{"id":1,"name":"Ann"}}`)
	}))
	return httptest.NewServer(mux)
}

func TestBookStackDetect(t *testing.T) {
	srv := bookStackServer(t)
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/bs/books/ops/page/backups")
	d, err := (&bookStackProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u)
	if err != nil || d == nil || d.BaseURL != srv.URL+"/bs" || d.Methods[0].Fields[0].ID != FieldTokenID || d.Methods[0].TokenURL != srv.URL+"/bs/my-account/api-tokens" {
		t.Fatalf("%+v %v", d, err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer other.Close()
	ou, _ := url.Parse(other.URL)
	if d, _ := (&bookStackProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), ou); d != nil {
		t.Fatalf("claimed %+v", d)
	}
}

func TestBookStackClient(t *testing.T) {
	srv := bookStackServer(t)
	defer srv.Close()
	conn := Conn{Kind: BookStack, BaseURL: srv.URL + "/bs", Method: "api-key"}
	p := &bookStackProvider{}
	cl := p.Open(conn, Credentials{FieldTokenID: "tid", FieldToken: "tsecret"}, NewHTTP(nil)).(*bookStackClient)
	ctx := context.Background()
	if me, err := cl.Me(ctx); err != nil || me.ID != "tid" {
		t.Fatalf("%+v %v", me, err)
	}
	if _, err := p.Open(conn, Credentials{FieldTokenID: "tid", FieldToken: "bad"}, NewHTTP(nil)).Me(ctx); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	pages, err := cl.SearchPages(ctx, Query{Text: "backup"})
	if err != nil || len(pages) != 1 || pages[0].ID != "7" || pages[0].Space != "Ops" || pages[0].Excerpt != "nightly backup job" {
		t.Fatalf("%+v %v", pages, err)
	}
	for _, id := range []string{"7", "ops/backups"} {
		pg, err := cl.Page(ctx, id)
		if err != nil || pg.Title != "Backups" || pg.Body != "Nightly\n\n- db" || pg.Author.Name != "Ann" || pg.URL != srv.URL+"/bs/books/ops/page/backups" {
			t.Fatalf("%s: %+v %v", id, pg, err)
		}
	}
	if _, err := cl.Page(ctx, "ops/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if _, err := cl.Page(ctx, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestBookStackLink(t *testing.T) {
	p := &bookStackProvider{}
	conn := Conn{BaseURL: "https://d.example/bs"}
	for raw, want := range map[string]string{"https://d.example/bs/link/7": "7", "https://d.example/bs/books/ops/page/backups": "ops/backups", "https://d.example/bs/books/ops/page/backups/edit": "ops/backups"} {
		u, _ := url.Parse(raw)
		if got, ok := p.Link(conn, u); !ok || got.Kind != "page" || got.Key != want {
			t.Fatalf("%s: %+v %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"https://d.example/bs/books/ops", "https://d.example/books/ops/page/x", "https://d.example/bs/link/abc"} {
		u, _ := url.Parse(raw)
		if _, ok := p.Link(conn, u); ok {
			t.Fatalf("matched %s", raw)
		}
	}
}

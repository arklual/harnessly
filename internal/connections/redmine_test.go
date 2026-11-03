package connections

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func redmineServer(t *testing.T, prefix string, public bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("X-Redmine-API-Key") != "key" {
			w.WriteHeader(401)
			return false
		}
		return true
	}
	mux.HandleFunc(prefix+"/projects.json", func(w http.ResponseWriter, r *http.Request) {
		if !public && !authed(w, r) {
			return
		}
		secWrite(w, `{"projects":[{"id":1,"name":"Site","identifier":"site"}],"total_count":1,"offset":0,"limit":100}`)
	})
	mux.HandleFunc(prefix+"/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta name="description" content="Redmine" /></head><body><div id="footer">Powered by <a href="https://www.redmine.org/">Redmine</a> © 2006-2024</div></body></html>`))
	})
	mux.HandleFunc(prefix+"/users/current.json", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			secWrite(w, `{"user":{"id":3,"login":"ann","firstname":"Ann","lastname":"Lee","mail":"ann@x.io"}}`)
		}
	})
	mux.HandleFunc(prefix+"/issue_statuses.json", func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `{"issue_statuses":[{"id":1,"name":"New","is_closed":false},{"id":5,"name":"Closed","is_closed":true},{"id":2,"name":"In Progress","is_closed":false}]}`)
	})
	issue := `{"id":123,"project":{"id":1,"name":"Site"},"tracker":{"id":1,"name":"Bug"},"status":{"id":2,"name":"In Progress"},"priority":{"id":2,"name":"Normal"},
	"author":{"id":3,"name":"Ann Lee"},"assigned_to":{"id":4,"name":"Bob"},"parent":{"id":100},"subject":"Broken page","description":"h1. Steps",
	"created_on":"2026-10-01T10:00:00Z","updated_on":"2026-10-02T10:00:00Z","journals":[{"id":1,"user":{"id":4,"name":"Bob"},"notes":"checking","created_on":"2026-10-03T10:00:00Z"},{"id":2,"user":{"id":4,"name":"Bob"},"notes":"","created_on":"2026-10-03T11:00:00Z"}]}`
	mux.HandleFunc(prefix+"/issues.json", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		q := r.URL.Query()
		if q.Get("assigned_to_id") != "me" || q.Get("status_id") != "open" || q.Get("subject") != "~broken" || q.Get("project_id") != "site" {
			w.WriteHeader(400)
			return
		}
		secWrite(w, `{"issues":[`+issue+`],"total_count":1}`)
	})
	mux.HandleFunc(prefix+"/issues/123.json", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) && r.URL.Query().Get("include") == "journals" {
			secWrite(w, `{"issue":`+issue+`}`)
		}
	})
	return httptest.NewServer(mux)
}

func TestRedmineDetect(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		public bool
	}{{"", true}, {"/redmine", false}} {
		srv := redmineServer(t, tc.prefix, tc.public)
		u, _ := url.Parse(srv.URL + tc.prefix + "/issues/123")
		d, err := (&redmineProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u)
		srv.Close()
		if err != nil || d == nil || d.Kind != Redmine || d.BaseURL != srv.URL+tc.prefix {
			t.Fatalf("%v: %+v %v", tc, d, err)
		}
		if d.Methods[0].ID != "api-key" || d.Methods[0].TokenURL != srv.URL+tc.prefix+"/my/account" || d.Methods[1].ID != "basic" {
			t.Fatalf("methods %+v", d.Methods)
		}
	}
}

func TestRedmineDetectRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Welcome</body></html>"))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/issues/1")
	if d, _ := (&redmineProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u); d != nil {
		t.Fatalf("claimed %+v", d)
	}
	json := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secWrite(w, `{"projects":[]}`) }))
	defer json.Close()
	u, _ = url.Parse(json.URL)
	if d, _ := (&redmineProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u); d != nil {
		t.Fatalf("claimed %+v", d)
	}
}

func TestRedmineClient(t *testing.T) {
	srv := redmineServer(t, "/redmine", false)
	defer srv.Close()
	conn := Conn{Kind: Redmine, BaseURL: srv.URL + "/redmine", Method: "api-key"}
	cl := (&redmineProvider{}).Open(conn, Credentials{FieldToken: "key"}, NewHTTP(nil)).(*redmineClient)
	ctx := context.Background()
	me, err := cl.Me(ctx)
	if err != nil || me.Login != "ann" || me.Name != "Ann Lee" {
		t.Fatalf("%+v %v", me, err)
	}
	if _, err := (&redmineProvider{}).Open(conn, Credentials{FieldToken: "x"}, NewHTTP(nil)).Me(ctx); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	ps, err := cl.Projects(ctx)
	if err != nil || len(ps) != 1 || ps[0].Key != "site" {
		t.Fatalf("%+v %v", ps, err)
	}
	list, err := cl.Search(ctx, Query{Text: "broken", Mine: true, Open: true, Project: "site"})
	if err != nil || len(list) != 1 || list[0].Key != "123" || list[0].Status.Category != Doing || list[0].URL != srv.URL+"/redmine/issues/123" || list[0].Parent.Key != "100" {
		t.Fatalf("%+v %v", list, err)
	}
	it, err := cl.Issue(ctx, "#123")
	if err != nil || it.Title != "Broken page" || len(it.Comments) != 1 || it.Comments[0].Text != "checking" {
		t.Fatalf("%+v %v", it, err)
	}
	if _, err := cl.Issue(ctx, "abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if _, err := cl.Issue(ctx, "999"); err == nil {
		t.Fatal("expected error for unknown issue")
	}
}

func TestRedmineBasicHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		secWrite(w, `{"user":{"id":1,"login":"u"}}`)
	}))
	defer srv.Close()
	cl := (&redmineProvider{}).Open(Conn{BaseURL: srv.URL, Method: "basic"}, Credentials{FieldUsername: "u", FieldPassword: "p"}, NewHTTP(nil))
	if _, err := cl.Me(context.Background()); err != nil || got != Basic("u", "p") {
		t.Fatalf("%v %q", err, got)
	}
}

func TestRedmineLink(t *testing.T) {
	p := &redmineProvider{}
	conn := Conn{BaseURL: "https://h.example/redmine"}
	u, _ := url.Parse("https://h.example/redmine/issues/123#note-2")
	if got, ok := p.Link(conn, u); !ok || got.Key != "123" || got.Kind != "issue" {
		t.Fatalf("%+v %v", got, ok)
	}
	for _, raw := range []string{"https://h.example/issues/123", "https://h.example/redmine/issues", "https://h.example/redmine/projects/site", "https://h.example/redmine/issues/new"} {
		u, _ := url.Parse(raw)
		if _, ok := p.Link(conn, u); ok {
			t.Fatalf("matched %s", raw)
		}
	}
}

// A deep path that redirects to the root login page must not make the deep path the base.
func TestRedmineIgnoresRedirectToRootLogin(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<meta name="description" content="Redmine" />`))
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/deep/path/here")
	d, _ := (&redmineProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u)
	if d == nil || d.BaseURL != srv.URL {
		t.Fatalf("want root base, got %+v", d)
	}
}

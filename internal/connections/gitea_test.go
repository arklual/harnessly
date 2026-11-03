package connections

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// secRewriteRT sends every request to the test server whatever its host: fixed-host fast paths under test.
type secRewriteRT struct{ target *url.URL }

func (r secRewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	c.URL.Scheme, c.URL.Host = r.target.Scheme, r.target.Host
	return http.DefaultTransport.RoundTrip(c)
}

func secRewriteHTTP(t *testing.T, srv *httptest.Server) *HTTP {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	return NewHTTP(secRewriteRT{u})
}

func secWrite(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func giteaServer(t *testing.T, prefix string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/api/v1/version", func(w http.ResponseWriter, r *http.Request) { secWrite(w, `{"version":"9.0.0+gitea-1.22.0"}`) })
	mux.HandleFunc(prefix+"/api/v1/settings/api", func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `{"max_response_items":50,"default_paging_num":30,"default_git_trees_per_page":1000,"default_max_blob_size":10485760}`)
	})
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "token tok" {
			w.WriteHeader(401)
			return false
		}
		return true
	}
	mux.HandleFunc(prefix+"/api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			secWrite(w, `{"id":7,"login":"ann","full_name":"Ann Lee","email":"ann@x.io","avatar_url":"https://x/a.png"}`)
		}
	})
	mux.HandleFunc(prefix+"/api/v1/user/repos", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			secWrite(w, `[{"full_name":"acme/app","html_url":"https://git.x/acme/app"}]`)
		}
	})
	issue := `{"number":12,"title":"Crash on save","body":"steps","state":"open","html_url":"https://git.x/acme/app/issues/12",
	"user":{"id":7,"login":"ann"},"assignee":{"id":8,"login":"bob","full_name":"Bob"},"labels":[{"name":"in progress"}],
	"created_at":"2026-10-01T10:00:00Z","updated_at":"2026-10-02T10:00:00+03:00","repository":{"full_name":"acme/app"}}`
	mux.HandleFunc(prefix+"/api/v1/repos/issues/search", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		q := r.URL.Query()
		if q.Get("type") != "issues" || q.Get("state") != "open" || q.Get("assigned") != "true" || q.Get("q") != "crash" {
			w.WriteHeader(400)
			return
		}
		secWrite(w, `[`+issue+`,{"number":3,"title":"PR","state":"open","pull_request":{},"repository":{"full_name":"acme/app"}}]`)
	})
	mux.HandleFunc(prefix+"/api/v1/repos/acme/app/issues/12", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			secWrite(w, issue)
		}
	})
	mux.HandleFunc(prefix+"/api/v1/repos/acme/app/issues/12/comments", func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `[{"id":1,"body":"on it","user":{"id":8,"login":"bob"},"created_at":"2026-10-03T10:00:00Z"}]`)
	})
	return httptest.NewServer(mux)
}

func TestGiteaDetect(t *testing.T) {
	for _, prefix := range []string{"", "/gitea"} {
		srv := giteaServer(t, prefix)
		u, _ := url.Parse(srv.URL + prefix + "/acme/app/issues/12")
		d, err := (&giteaProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u)
		srv.Close()
		if err != nil || d == nil {
			t.Fatalf("prefix %q: %v %v", prefix, d, err)
		}
		if d.Kind != Gitea || d.BaseURL != srv.URL+prefix || d.Edition != "forgejo" || d.Version != "9.0.0+gitea-1.22.0" || d.Deployment != SelfHosted {
			t.Fatalf("detection %+v", d)
		}
		if len(d.Methods) != 1 || d.Methods[0].TokenURL != srv.URL+prefix+"/user/settings/applications" {
			t.Fatalf("methods %+v", d.Methods)
		}
	}
}

func TestGiteaDetectRejects(t *testing.T) {
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer html.Close()
	only := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			secWrite(w, `{"version":"1.0"}`)
			return
		}
		secWrite(w, `{}`)
	}))
	defer only.Close()
	for _, s := range []*httptest.Server{html, only} {
		u, _ := url.Parse(s.URL)
		if d, _ := (&giteaProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u); d != nil {
			t.Fatalf("claimed %+v", d)
		}
	}
}

func TestGiteaCodebergFastPath(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	u, _ := url.Parse("https://codeberg.org/forgejo/forgejo/issues/1")
	d, err := (&giteaProvider{}).Detect(context.Background(), NewProber(secRewriteHTTP(t, srv)), u)
	if err != nil || d == nil || d.BaseURL != "https://codeberg.org" || d.Deployment != Cloud || d.Methods[0].TokenURL != "https://codeberg.org/user/settings/applications" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestGiteaClient(t *testing.T) {
	srv := giteaServer(t, "/gitea")
	defer srv.Close()
	conn := Conn{Kind: Gitea, BaseURL: srv.URL + "/gitea"}
	p := &giteaProvider{}
	cl := p.Open(conn, Credentials{FieldToken: "tok"}, NewHTTP(nil)).(*giteaClient)
	me, err := cl.Me(context.Background())
	if err != nil || me.Login != "ann" || me.Name != "Ann Lee" {
		t.Fatalf("%+v %v", me, err)
	}
	bad := p.Open(conn, Credentials{FieldToken: "no"}, NewHTTP(nil))
	if _, err := bad.Me(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	projects, err := cl.Projects(context.Background())
	if err != nil || len(projects) != 1 || projects[0].Key != "acme/app" {
		t.Fatalf("%+v %v", projects, err)
	}
	list, err := cl.Search(context.Background(), Query{Text: "crash", Mine: true, Open: true})
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	if list[0].Key != "acme/app#12" || list[0].Status.Category != Doing || list[0].Assignee.Login != "bob" {
		t.Fatalf("%+v", list[0])
	}
	it, err := cl.Issue(context.Background(), "acme/app#12")
	if err != nil || it.Title != "Crash on save" || it.Description != "steps" || len(it.Comments) != 1 || it.Comments[0].Text != "on it" {
		t.Fatalf("%+v %v", it, err)
	}
	if _, err := cl.Issue(context.Background(), "acme/app#99"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestGiteaLink(t *testing.T) {
	p := &giteaProvider{}
	conn := Conn{BaseURL: "https://h.example/gitea"}
	for raw, want := range map[string]string{
		"https://h.example/gitea/acme/app/issues/12":     "acme/app#12",
		"https://h.example/gitea/acme/app/pulls/5#c":     "acme/app#5",
		"https://h.example/gitea/acme/app/issues/12?x=1": "acme/app#12",
	} {
		u, _ := url.Parse(raw)
		got, ok := p.Link(conn, u)
		if !ok || got.Kind != "issue" || got.Key != want {
			t.Fatalf("%s: %+v %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"https://h.example/acme/app/issues/12", "https://other/gitea/acme/app/issues/12", "https://h.example/gitea/acme/app", "https://h.example/gitea/acme/app/issues/new"} {
		u, _ := url.Parse(raw)
		if _, ok := p.Link(conn, u); ok {
			t.Fatalf("matched %s", raw)
		}
	}
}

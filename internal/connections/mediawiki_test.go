package connections

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func mediaWikiServer(t *testing.T, apiPath string, private bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(apiPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if private && r.Header.Get("Authorization") != "Bearer tok" {
			secWrite(w, `{"error":{"code":"readapidenied","info":"You need read permission to use this module.","*":"x"},"servedby":"mw1"}`)
			return
		}
		switch {
		case q.Get("meta") == "siteinfo":
			secWrite(w, `{"batchcomplete":true,"query":{"general":{"mainpage":"Main Page","base":"https://w.example/wiki/Main_Page","sitename":"Acme Wiki","generator":"MediaWiki 1.42.1","articlepath":"/wiki/$1","scriptpath":"/w"}}}`)
		case q.Get("meta") == "userinfo" && q.Get("uiprop") == "rights":
			secWrite(w, `{"query":{"userinfo":{"id":0,"name":"127.0.0.1","anon":true,"rights":["createaccount","read","edit"]}}}`)
		case q.Get("meta") == "userinfo":
			if r.Header.Get("Authorization") == "Bearer tok" {
				secWrite(w, `{"query":{"userinfo":{"id":42,"name":"Ann"}}}`)
				return
			}
			secWrite(w, `{"query":{"userinfo":{"id":0,"name":"127.0.0.1","anon":true}}}`)
		case q.Get("list") == "search":
			if q.Get("srsearch") != "deploy guide" {
				w.WriteHeader(400)
				return
			}
			secWrite(w, `{"query":{"searchinfo":{"totalhits":1},"search":[{"ns":0,"title":"Deploy guide/Prod","pageid":5,"snippet":"how to <span class=\"searchmatch\">deploy</span> &amp; roll back","timestamp":"2026-10-02T10:00:00Z"}]}}`)
		case q.Get("action") == "parse":
			if q.Get("page") != "Deploy guide/Prod" {
				secWrite(w, `{"error":{"code":"missingtitle","info":"The page you specified doesn't exist."}}`)
				return
			}
			secWrite(w, `{"parse":{"title":"Deploy guide/Prod","pageid":5,"text":"<div class=\"mw-parser-output\"><h2>Steps<span class=\"mw-editsection\"><span class=\"mw-editsection-bracket\">[</span><a href=\"x\">edit</a><span class=\"mw-editsection-bracket\">]</span></span></h2><p>Run it.</p><ul><li>one</li></ul></div>"}}`)
		default:
			w.WriteHeader(400)
		}
	})
	return httptest.NewServer(mux)
}

func TestMediaWikiDetect(t *testing.T) {
	srv := mediaWikiServer(t, "/w/api.php", false)
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/wiki/Deploy_guide")
	d, err := (&mediaWikiProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u)
	if err != nil || d == nil || d.Version != "1.42.1" || d.BaseURL != srv.URL || d.APIURL != srv.URL+"/w/api.php" || d.Title != "Acme Wiki" {
		t.Fatalf("%+v %v", d, err)
	}
	if d.Methods[0].ID != "none" || !d.Methods[0].Recommended || d.Methods[1].ID != "pat" || !strings.Contains(d.Methods[1].TokenURL, "OAuthConsumerRegistration") {
		t.Fatalf("methods %+v", d.Methods)
	}
	if d.Extra["articlepath"] != "/wiki/$1" || len(d.Capabilities) != 1 || d.Capabilities[0] != Pages {
		t.Fatalf("%+v", d)
	}
}

func TestMediaWikiDetectPrivate(t *testing.T) {
	srv := mediaWikiServer(t, "/wiki/api.php", true)
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/wiki/Main_Page")
	d, err := (&mediaWikiProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u)
	if err != nil || d == nil || d.Version != "" || len(d.Methods) != 1 || d.Methods[0].ID != "pat" || !d.Methods[0].Recommended {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestMediaWikiDetectRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "api.php") {
			secWrite(w, `{"query":{"general":{"generator":"Something 1.0"}}}`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/wiki/X")
	if d, _ := (&mediaWikiProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), u); d != nil {
		t.Fatalf("claimed %+v", d)
	}
}

func TestMediaWikiClient(t *testing.T) {
	srv := mediaWikiServer(t, "/w/api.php", false)
	defer srv.Close()
	conn := Conn{Kind: MediaWiki, BaseURL: srv.URL, APIURL: srv.URL + "/w/api.php", Method: "none", Extra: map[string]string{"articlepath": "/wiki/$1"}}
	cl := (&mediaWikiProvider{}).Open(conn, nil, NewHTTP(nil)).(*mediaWikiClient)
	ctx := context.Background()
	me, err := cl.Me(ctx)
	if err != nil || me.Name != "Anonymous" {
		t.Fatalf("%+v %v", me, err)
	}
	pages, err := cl.SearchPages(ctx, Query{Text: "deploy guide"})
	if err != nil || len(pages) != 1 || pages[0].ID != "Deploy guide/Prod" || pages[0].Excerpt != "how to deploy & roll back" || pages[0].URL != srv.URL+"/wiki/Deploy_guide/Prod" {
		t.Fatalf("%+v %v", pages, err)
	}
	pg, err := cl.Page(ctx, "Deploy guide/Prod")
	if err != nil || pg.Body != "Steps\nRun it.\n\n- one" {
		t.Fatalf("%+v %v", pg, err)
	}
	if _, err := cl.Page(ctx, "Nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestMediaWikiToken(t *testing.T) {
	srv := mediaWikiServer(t, "/w/api.php", true)
	defer srv.Close()
	conn := Conn{BaseURL: srv.URL, APIURL: srv.URL + "/w/api.php", Method: "pat"}
	p := &mediaWikiProvider{}
	me, err := p.Open(conn, Credentials{FieldToken: "tok"}, NewHTTP(nil)).Me(context.Background())
	if err != nil || me.Login != "Ann" {
		t.Fatalf("%+v %v", me, err)
	}
	if _, err := p.Open(conn, Credentials{FieldToken: "bad"}, NewHTTP(nil)).Me(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
}

func TestMediaWikiLink(t *testing.T) {
	p := &mediaWikiProvider{}
	conn := Conn{BaseURL: "https://w.example", Extra: map[string]string{"articlepath": "/wiki/$1"}}
	for raw, want := range map[string]string{
		"https://w.example/wiki/Deploy_guide/Prod":      "Deploy guide/Prod",
		"https://w.example/wiki/Caf%C3%A9":              "Café",
		"https://w.example/w/index.php?title=Some_page": "Some page",
	} {
		u, _ := url.Parse(raw)
		if got, ok := p.Link(conn, u); !ok || got.Kind != "page" || got.Key != want {
			t.Fatalf("%s: %+v %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"https://other/wiki/X", "https://w.example/wiki/", "https://w.example/wiki/Special:Search", "https://w.example/foo"} {
		u, _ := url.Parse(raw)
		if _, ok := p.Link(conn, u); ok {
			t.Fatalf("matched %s", raw)
		}
	}
}

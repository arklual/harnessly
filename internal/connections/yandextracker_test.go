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

func yandexServer(t *testing.T, headerName string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "OAuth tok" || r.Header.Get(headerName) != "org-1" {
			w.WriteHeader(401)
			return false
		}
		return true
	}
	mux.HandleFunc("/v2/myself", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			secWrite(w, `{"self":"x","uid":1130000012345,"login":"ann","firstName":"Ann","lastName":"Lee","display":"Ann Lee","email":"ann@x.ru"}`)
		}
	})
	mux.HandleFunc("/v2/queues", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			secWrite(w, `[{"id":1,"key":"ABC","name":"Alpha"}]`)
		}
	})
	issue := `{"key":"ABC-12","id":"5f","summary":"Fix login","description":"details","status":{"key":"inProgress","display":"В работе"},
	"type":{"display":"Баг"},"priority":{"display":"Критичный"},"queue":{"key":"ABC"},"assignee":{"id":"11","display":"Ann Lee"},
	"parent":{"key":"ABC-1","display":"Epic"},"tags":["web"],"createdAt":"2026-10-01T10:00:00.000+0000","updatedAt":"2026-10-02T10:00:00.000+0000"}`
	mux.HandleFunc("/v2/issues/_search", func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r) {
			return
		}
		var b struct{ Query string }
		_ = json.NewDecoder(r.Body).Decode(&b)
		if r.Method != "POST" || b.Query != `Queue: "ABC" AND Assignee: me() AND Resolution: empty() AND "login" "Sort by": Updated DESC` {
			w.WriteHeader(400)
			return
		}
		secWrite(w, `[`+issue+`]`)
	})
	mux.HandleFunc("/v2/issues/ABC-12", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			secWrite(w, issue)
		}
	})
	mux.HandleFunc("/v2/issues/ABC-12/comments", func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `[{"id":9,"text":"look","createdBy":{"id":"12","display":"Bob"},"createdAt":"2026-10-03T10:00:00.000+0000"}]`)
	})
	return httptest.NewServer(mux)
}

func TestYandexTrackerDetect(t *testing.T) {
	p := &yandexTrackerProvider{}
	u, _ := url.Parse("https://tracker.yandex.ru/ABC-12")
	d, err := p.Detect(context.Background(), nil, u)
	if err != nil || d == nil || d.BaseURL != "https://tracker.yandex.ru" || len(d.Methods) != 2 || d.Methods[0].ID != "oauth-360" || d.Methods[1].ID != "oauth-cloud" || !d.Methods[0].Recommended {
		t.Fatalf("%+v %v", d, err)
	}
	if d.Methods[0].Fields[1].ID != FieldOrg {
		t.Fatalf("fields %+v", d.Methods[0].Fields)
	}
	u, _ = url.Parse("https://example.com/ABC-12")
	if d, _ := p.Detect(context.Background(), nil, u); d != nil {
		t.Fatal("claimed foreign host")
	}
}

func TestYandexTrackerClient(t *testing.T) {
	for method, header := range map[string]string{"oauth-360": "X-Org-ID", "oauth-cloud": "X-Cloud-Org-ID"} {
		srv := yandexServer(t, header)
		conn := Conn{Kind: YandexTracker, BaseURL: "https://tracker.yandex.ru", APIURL: srv.URL, Method: method}
		cl := (&yandexTrackerProvider{}).Open(conn, Credentials{FieldToken: "tok", FieldOrg: "org-1"}, NewHTTP(nil)).(*yandexTrackerClient)
		ctx := context.Background()
		me, err := cl.Me(ctx)
		if err != nil || me.Login != "ann" || me.Name != "Ann Lee" {
			t.Fatalf("%s: %+v %v", method, me, err)
		}
		wrong := (&yandexTrackerProvider{}).Open(conn, Credentials{FieldToken: "tok", FieldOrg: "other"}, NewHTTP(nil))
		if _, err := wrong.Me(ctx); !errors.Is(err, ErrDenied) {
			t.Fatalf("want denied, got %v", err)
		}
		ps, err := cl.Projects(ctx)
		if err != nil || len(ps) != 1 || ps[0].URL != "https://tracker.yandex.ru/ABC" {
			t.Fatalf("%+v %v", ps, err)
		}
		list, err := cl.Search(ctx, Query{Text: "login", Mine: true, Open: true, Project: "ABC"})
		if err != nil || len(list) != 1 || list[0].Key != "ABC-12" || list[0].Status.Category != Doing || list[0].Parent.Key != "ABC-1" || list[0].URL != "https://tracker.yandex.ru/ABC-12" {
			t.Fatalf("%+v %v", list, err)
		}
		it, err := cl.Issue(ctx, "abc-12")
		if err != nil || it.Description != "details" || len(it.Comments) != 1 || it.Comments[0].Author.Name != "Bob" || it.Created.IsZero() {
			t.Fatalf("%+v %v", it, err)
		}
		if _, err := cl.Issue(ctx, "ABC-99"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
		srv.Close()
	}
}

func TestYandexTrackerLink(t *testing.T) {
	p := &yandexTrackerProvider{}
	conn := Conn{BaseURL: "https://tracker.yandex.ru"}
	for raw, want := range map[string]string{"https://tracker.yandex.ru/ABC-12": "ABC-12", "https://tracker.yandex.com/abc-7?x=1": "ABC-7", "https://tracker.yandex.ru/issue/ABC-3": "ABC-3"} {
		u, _ := url.Parse(raw)
		if got, ok := p.Link(conn, u); !ok || got.Key != want || got.Kind != "issue" {
			t.Fatalf("%s: %+v %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"https://tracker.yandex.ru/pages/", "https://tracker.yandex.ru/", "https://example.com/ABC-12"} {
		u, _ := url.Parse(raw)
		if _, ok := p.Link(conn, u); ok {
			t.Fatalf("matched %s", raw)
		}
	}
}

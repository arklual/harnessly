package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestLinkMetaOfAStandAndACommit(t *testing.T) {
	var mu sync.Mutex
	asked := map[string]bool{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[r.Host] = true
		mu.Unlock()
		switch {
		case r.Host == "hop.preview.example.com":
			http.Redirect(w, r, "https://stand-2.preview.example.com/login", http.StatusFound)
		case r.Host == "down.preview.example.com":
			w.WriteHeader(http.StatusBadGateway)
		case r.Host == "login.preview.example.com":
			http.Redirect(w, r, "https://sso.example.com/login", http.StatusFound)
		case strings.HasSuffix(r.Host, ".preview.example.com"):
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<head><title>Аналитика</title></head>"))
		case strings.Contains(r.URL.Path, "/repository/commits/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"title":"fix: стенд","short_id":"8ebd61a7","last_pipeline":{"status":"failed","web_url":"https://gitlab.example.com/p/-/pipelines/1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := New(Config{Transport: tlsTo(srv), StandDomain: "preview.example.com", GitLabHosts: []string{"gitlab.example.com"}})
	ctx := context.Background()

	up := s.parseLinkTarget("https://stand-1.preview.example.com/adminpanel/analytics")
	if up == nil || up.kind != "stand" {
		t.Fatalf("stand target: %+v", up)
	}
	if m := s.linkMeta(ctx, up); opt(m, "up") != true || opt(m, "title") != "Аналитика" {
		t.Fatalf("stand up: %v", m)
	}
	if m := s.linkMeta(ctx, s.parseLinkTarget("https://down.preview.example.com/")); opt(m, "up") != false {
		t.Fatalf("stand down: %v", m)
	}
	if m := s.linkMeta(ctx, s.parseLinkTarget("https://login.preview.example.com/")); opt(m, "up") != true {
		t.Fatalf("a stand that asks to sign in is up: %v", m)
	}
	if m := s.linkMeta(ctx, s.parseLinkTarget("https://hop.preview.example.com/")); opt(m, "title") != "Аналитика" {
		t.Fatalf("a redirect to a stand is followed: %v", m)
	}
	mu.Lock()
	if asked["sso.example.com"] {
		t.Error("the probe of a stand followed a redirect off the stand domain")
	}
	mu.Unlock()
	for _, not := range []string{"https://preview.example.com/", "https://a.b.preview.example.com/", "https://x.preview.example.com.evil.org/"} {
		if got := s.parseLinkTarget(not); got != nil && got.kind == "stand" {
			t.Errorf("%s is no stand", not)
		}
	}

	c := s.parseLinkTarget("https://gitlab.example.com/harnessly/harnessly/-/commit/8ebd61a76b6245df")
	if c == nil || c.kind != "commit" || c.project != "harnessly/harnessly" {
		t.Fatalf("commit target: %+v", c)
	}
	m := s.linkMeta(ctx, c)
	if opt(m, "kind") != "commit" || opt(m, "title") != "fix: стенд" || opt(opt(m, "pipeline"), "status") != "failed" {
		t.Fatalf("commit meta: %v", m)
	}
}

package api_test

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

func TestLegacyDocumentAddressRedirectsToThePage(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_DOCS_LEGACY_HOSTS", "old-docs.example.net"))
	p := s.Browser()
	const host = "old-docs.example.net"
	r := p.DoAt(s.URL, host, "GET", "/a/my-doc-x1y2?v=3", nil)
	if r.Code != 301 || !strings.HasSuffix(r.Header.Get("Location"), "/agents/docs/my-doc-x1y2?v=3") {
		t.Fatalf("redirect: %d %q", r.Code, r.Header.Get("Location"))
	}
	if loc := r.Header.Get("Location"); strings.Contains(loc, host) {
		t.Errorf("the redirect stays on the old host: %q", loc)
	}
	if r := p.DoAt(s.URL, host, "GET", "/a/my-doc/raw", nil); r.Code != 301 || !strings.HasSuffix(r.Header.Get("Location"), "/agents/docs/my-doc") {
		t.Errorf("a longer old path: %d %q", r.Code, r.Header.Get("Location"))
	}
	// Nothing is served here: not another path, not another method, not a slug that is not one, not the hub's API.
	for _, c := range []struct{ method, path string }{
		{"GET", "/"}, {"GET", "/api/me"}, {"GET", "/a/"}, {"GET", "/a/..%2fx"}, {"GET", "/a/%2e%2e"}, {"POST", "/a/doc"}, {"GET", "/agents/docs/doc"},
	} {
		if r := p.DoAt(s.URL, host, c.method, c.path, nil); r.Code != 404 {
			t.Errorf("%s %s: %d", c.method, c.path, r.Code)
		}
	}
	// A hub without the setting has no such host.
	s2 := hubtest.New(t)
	if r := s2.Browser().DoAt(s2.URL, host, "GET", "/a/my-doc", nil); r.Code == 301 {
		t.Errorf("redirect without the setting: %d", r.Code)
	}
}

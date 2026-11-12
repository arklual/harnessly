package api_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// a former address of the guest links sends /s/<token> on to the share host and serves nothing else, so
// links already handed out keep opening; the hub's own pages are never there.
func TestDesignShareAliasSendsGuestLinksOn(t *testing.T) {
	const alias = "old-share.example.test"
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost), hubtest.Env("HUB_DESIGN_SHARE_ALIASES", "http://"+alias))
	b := s.Browser()

	r := b.DoAt(s.URL, alias, "GET", "/s/tok123/f/Main.dc.html?v=2", nil)
	if r.Code != 307 {
		t.Fatalf("a link at the former address is sent on: %s", r)
	}
	if got, want := r.Header.Get("Location"), "http://"+shareHost+"/s/tok123/f/Main.dc.html?v=2"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	for _, c := range []struct{ method, path string }{{"GET", "/"}, {"GET", "/api/me"}, {"GET", "/agent/v1"}, {"POST", "/s/tok123/comments"}, {"GET", "/d/tok"}} {
		r := b.DoAt(s.URL, alias, c.method, c.path, nil)
		if r.Code != 404 || r.Header.Get("Location") != "" {
			t.Errorf("%s %s at the former address must be nothing: %s", c.method, c.path, r)
		}
	}
	// The share host itself keeps serving the link, and the hub's origin does not take the alias's place.
	if r := b.DoAt(s.URL, shareHost, "GET", "/s/unknown", nil); r.Code == 307 {
		t.Errorf("the share host is not sent anywhere: %s", r)
	}
}

// Without HUB_DESIGN_SHARE_ALIASES an unknown host is not a share alias, and with the share host alone a /d/ path on it is
// not the hub's document page unless HUB_DESIGN_SHARE_DOCS is on.
func TestDesignShareDocsAreOffByDefault(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost), hubtest.Env("HUB_DOCS", "on"), hubtest.Env("HUB_ORGS", "on"), hubtest.Env("HUB_ACCESS_ENFORCE", "on"), hubtest.Env("HUB_RESOURCE_ACCESS", "on"))
	b := s.Browser()
	hub := b.Do("GET", "/d/unknown", nil)
	if hub.Code != 404 {
		t.Fatalf("the hub's own guest page for an unknown link: %s", hub)
	}
	if r := b.DoAt(s.URL, shareHost, "GET", "/d/unknown", nil); string(r.Body) == string(hub.Body) {
		t.Errorf("a document link must not open on the share host by default: %s", r)
	}
}

// With HUB_DESIGN_SHARE_DOCS=on the share host and its aliases open a document's guest link under their own address;
// the hub's pages are still not served there.
func TestDesignShareDocsOpenOnShareHosts(t *testing.T) {
	const alias = "old-share.example.test"
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost), hubtest.Env("HUB_DESIGN_SHARE_ALIASES", "http://"+alias), hubtest.Env("HUB_DESIGN_SHARE_DOCS", "on"), hubtest.Env("HUB_DOCS", "on"), hubtest.Env("HUB_ORGS", "on"), hubtest.Env("HUB_ACCESS_ENFORCE", "on"), hubtest.Env("HUB_RESOURCE_ACCESS", "on"))
	b := s.Browser()
	for _, host := range []string{shareHost, alias} {
		hub := b.Do("GET", "/d/unknown", nil)
		got := b.DoAt(s.URL, host, "GET", "/d/unknown", nil)
		if got.Code != hub.Code || string(got.Body) != string(hub.Body) {
			t.Errorf("/d/ on %s answers as the hub's guest page does: %s vs %s", host, got, hub)
		}
		if r := b.DoAt(s.URL, host, "GET", "/api/me", nil); r.Code != 404 && r.Code != 307 {
			t.Errorf("the hub's API on %s: %s", host, r)
		}
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every route of a deck is registered once, JSON or body, under the platform's API.
func TestDeckRoutesAreRegistered(t *testing.T) {
	t.Setenv("RUNTIME_WORKSPACE_DIR", t.TempDir())
	t.Setenv("RUNTIME_DESIGN_LIBRARY_DIR", "")
	d := deps{Home: t.TempDir(), DataDir: t.TempDir(), SessionOK: func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	json, body := designDeckRoutes(d), designDeckBodyRoutes(d)
	want := map[string]string{
		"": "json", "/settings": "json", "/notes": "json", "/tidy": "json", "/patch": "json", "/comments": "json",
		"/rec": "json", "/rec/events": "json", "/rec/finish": "json", "/rec/edit": "json", "/export": "json", "/pdf": "json", "/slide": "json",
		"/pptx-style": "body", "/rec/chunk": "body", "/rec/media": "body", "/export/file": "body",
	}
	for p, kind := range want {
		r := httptest.NewRequest(http.MethodGet, deckPath(p)+"?artifact=acme", nil)
		jsonHits, bodyHits := 0, 0
		for _, route := range json {
			if route.Match(r) {
				jsonHits++
			}
		}
		for _, route := range body {
			if route.Match(r) {
				bodyHits++
			}
		}
		if (kind == "json" && (jsonHits != 1 || bodyHits != 0)) || (kind == "body" && (bodyHits != 1 || jsonHits != 0)) {
			t.Errorf("%s: %d JSON and %d body routes match, want one %s route", p, jsonHits, bodyHits, kind)
		}
	}
	// The pictures of a design (design/image, design/image/file) and the kit transfer (design/kit/import, design/kit/export), the pack transfer (design/pack/import, design/pack/export) are registered beside the deck's.
	pictures := map[string]string{platformAPI + "/design/image": "json", platformAPI + "/design/image/file": "body",
		platformAPI + "/design/kit/import": "body", platformAPI + "/design/kit/export": "body",
		platformAPI + "/design/pack/import": "body", platformAPI + "/design/pack/export": "body",
		platformAPI + "/design/kit/layouts": "json", platformAPI + "/design/kit/fonts": "json"}
	for p, kind := range pictures {
		r := httptest.NewRequest(http.MethodGet, p+"?artifact=acme", nil)
		hits := 0
		routes := len(json)
		for i := range routes {
			if kind == "json" && json[i].Match(r) {
				hits++
			}
		}
		for _, route := range body {
			if kind == "body" && route.Match(r) {
				hits++
			}
		}
		if hits != 1 {
			t.Errorf("%s: %d %s routes match, want one", p, hits, kind)
		}
	}
	if len(json)+len(body) != len(want)+len(pictures) {
		t.Errorf("%d routes, want %d", len(json)+len(body), len(want)+len(pictures))
	}
}

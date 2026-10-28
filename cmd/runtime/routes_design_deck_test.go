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
		"/rec": "json", "/rec/events": "json", "/rec/finish": "json", "/rec/edit": "json", "/export": "json", "/pdf": "json",
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
	if len(json)+len(body) != len(want) {
		t.Errorf("%d routes, want %d", len(json)+len(body), len(want))
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVoiceRoutesMatchAndRefuse(t *testing.T) {
	d := deps{DataDir: t.TempDir(), SessionOK: func(r *http.Request) bool { return r.Header.Get("Cookie") == "session-token=good" },
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			return 401, nil, nil, r.Header.Get("Cookie") != "session-token=good"
		}}
	routes := voiceRoutes(d)
	req := func(method, path string, upgrade bool) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		if upgrade {
			r.Header.Set("Upgrade", "websocket")
		}
		return r
	}
	matches := func(r *http.Request) int {
		for i, rt := range routes {
			if rt.Slice == "voice" && rt.Match(r) {
				return i
			}
		}
		return -1
	}
	for _, c := range []struct {
		r    *http.Request
		want int
	}{
		{req("GET", "/api/voice", true), 0},
		{req("GET", "/agents/api/voice", true), 0},
		{req("GET", "/api/voice", false), -1},
		{req("GET", "/api/voice/assets/hf/x", false), 1},
		{req("GET", "/agents/api/voice/assets/ort/1.2.3/f/", false), 1},
		{req("POST", "/api/voice/assets/hf/x", false), -1},
		{req("GET", "/api/voice/assets", false), -1},
	} {
		if got := matches(c.r); got != c.want {
			t.Errorf("%s %s: route %d, want %d", c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
	w := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(w, req("GET", "/api/voice", true))
	if w.Code != 401 {
		t.Fatalf("socket without a session: %d", w.Code)
	}
	w = httptest.NewRecorder()
	routes[1].Handler.ServeHTTP(w, req("GET", "/api/voice/assets/x", false))
	if w.Code != 401 || w.Body.String() != `{"error":"Unauthorized"}` {
		t.Fatalf("assets without a session: %d %s", w.Code, w.Body)
	}
}

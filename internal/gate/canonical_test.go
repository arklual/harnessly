package gate

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A route matches what the old router matched: trailing and doubled slashes, with or without the app's mount point; a
// socket and a path outside the API come as they are.
func TestCanonicalPaths(t *testing.T) {
	dir := t.TempDir()
	g, err := New(Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	var seen string
	g.Add(Route{Match: func(r *http.Request) bool { return r.URL.Path == "/api/x/y" || r.URL.Path == "/agents/api/x/y" },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r.URL.Path + "?" + r.URL.RawQuery
			w.WriteHeader(299)
		})})
	for target, want := range map[string]string{
		"/api/x/y/":           "/api/x/y?",
		"/api//x///y?q=%zz&q": "/api/x/y?q=%zz&q",
		"/agents/api/x/y//":   "/agents/api/x/y?",
		"/api/%78/y":          "/api/x/y?",
		"//api/x/y":           "",
		"/agents//api/x/y":    "",
		"/x/api/x/y/":         "",
	} {
		seen = ""
		req := httptest.NewRequest("GET", target, nil)
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		if seen != want {
			t.Errorf("%s: the route saw %q, want %q", target, seen, want)
		}
	}
	seen = ""
	req := httptest.NewRequest("GET", "/api/x/y/", nil)
	req.Header.Set("Upgrade", "websocket")
	g.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "" {
		t.Errorf("a socket's path was rewritten: %q", seen)
	}
}

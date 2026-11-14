package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// the home asks the runtime for each of its parts with the page's own cookies, side by side, and answers
// once; a part that failed is named with its status, and the others still come as their routes gave them.
func TestDesignHomeIsEveryPartInOneAnswer(t *testing.T) {
	seen := map[string]string{}
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		<-mu
		seen[r.Method+" "+r.URL.Path] = r.Header.Get("Cookie") + "|" + r.URL.Query().Get("machine") + "|" + string(body) + "|" + r.Header.Get("Accept-Encoding")
		mu <- struct{}{}
		switch r.URL.Path {
		case "/api/v2/designly/systems":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"error":"agent away"}`)
		case platformAPI + "/apps/kinds":
			_, _ = io.WriteString(w, "not json")
		default:
			_, _ = io.WriteString(w, `{"path":"`+r.URL.Path+`"}`)
		}
	})
	routes := designHomeRoutes(h)
	req := httptest.NewRequest(http.MethodGet, platformAPI+"/design/home?machine=m1", nil)
	req.Header.Set("Cookie", "session=s")
	req.Header.Set("Accept-Encoding", "gzip")
	if !routes[0].Match(req) {
		t.Fatal("GET /design/home is not matched")
	}
	if routes[0].Match(httptest.NewRequest(http.MethodPost, platformAPI+"/design/home", nil)) {
		t.Fatal("a POST is matched")
	}
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, req)
	var home designHome
	if err := json.Unmarshal(rec.Body.Bytes(), &home); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	for _, name := range []string{"library", "layout", "kits", "kindDocs"} {
		if _, ok := home.Parts[name]; !ok {
			t.Errorf("part %s missing: %s", name, rec.Body)
		}
	}
	if f := home.Failed["systems"]; f.Status != http.StatusBadGateway || !strings.Contains(string(f.Body), "agent away") {
		t.Errorf("systems: %+v", f)
	}
	if f, ok := home.Failed["kinds"]; !ok || f.Body != nil {
		t.Errorf("a part that is not JSON: %+v", f)
	}
	if got := seen["POST "+platformAPI+"/design/library/layout"]; got != "session=s|m1|{}|" {
		t.Errorf("the layout was asked as %q", got)
	}
	if got := seen["GET /api/v2/designly/library"]; got != "session=s|m1||" {
		t.Errorf("the library was asked as %q", got)
	}
	if len(home.TookMs) != len(homeParts()) {
		t.Errorf("took: %v", home.TookMs)
	}
}

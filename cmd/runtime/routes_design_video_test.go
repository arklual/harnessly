package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every route of a video is registered once, JSON or body, and no other design group answers its path.
func TestVideoRoutesAreRegistered(t *testing.T) {
	t.Setenv("RUNTIME_WORKSPACE_DIR", t.TempDir())
	t.Setenv("RUNTIME_DESIGN_LIBRARY_DIR", "")
	d := deps{Home: t.TempDir(), DataDir: t.TempDir(), SessionOK: func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	json, body := designVideoRoutes(d), designVideoBodyRoutes(d)
	json = append(json, designDeckRoutes(d)...)
	json = append(json, designRoutes(d)...)
	body = append(body, designDeckBodyRoutes(d)...)
	want := map[string]string{
		videoPath(""): "json", videoPath("/settings"): "json", videoPath("/view"): "json", videoPath("/scene"): "json", videoPath("/comments"): "json",
		videoPath("/render"): "json", videoPath("/frame"): "body", videoPath("/file"): "body",
		videoCLIPath(""): "json", videoCLIPath("/scene"): "json", videoCLIPath("/frame"): "json",
		videoCLIPath("/check"): "json", videoCLIPath("/render"): "json",
	}
	for p, kind := range want {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch} {
			r := httptest.NewRequest(method, p+"?artifact=acme", nil)
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
				t.Errorf("%s %s: %d JSON and %d body routes match, want one %s route", method, p, jsonHits, bodyHits, kind)
			}
		}
	}
	if n := len(designVideoRoutes(d)) + len(designVideoBodyRoutes(d)); n != len(want) {
		t.Errorf("%d video routes, want %d", n, len(want))
	}
}

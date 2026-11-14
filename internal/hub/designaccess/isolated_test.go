package designaccess

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// A board served from the hub's snapshot is a sandbox of an opaque origin: its own support.js and styles come as
// «cross-site» requests and must load; a sibling host of the same site, which carries the session, keeps
// «same-origin».
func TestIsolatedLetsOnlyTheSandboxLoadItsOwnFiles(t *testing.T) {
	for site, want := range map[string]string{"cross-site": "cross-origin", "same-site": "same-origin", "same-origin": "same-origin", "": "same-origin"} {
		req := httptest.NewRequest("GET", "/support.js", nil)
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		rec := httptest.NewRecorder()
		w := isolated(rec, req)
		w.Header().Set("Content-Type", "text/javascript")
		w.WriteHeader(200)
		h := rec.Header()
		if got := h.Get("Cross-Origin-Resource-Policy"); got != want {
			t.Errorf("Sec-Fetch-Site %q: Cross-Origin-Resource-Policy = %q, want %q", site, got, want)
		}
		if got := h.Get("Referrer-Policy"); got != "strict-origin" {
			t.Errorf("Referrer-Policy = %q: the board's requests must name the hub's origin (HRN-1400)", got)
		}
		if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || strings.Contains(csp, "allow-same-origin") {
			t.Errorf("the sandbox must stay without allow-same-origin, got %q", csp)
		}
	}
}

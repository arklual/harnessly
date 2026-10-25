package design

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The site's DevTools read what the open pages of a design logged, from the same record the agents' `design logs` reads;
// only a session reads it, and only GET, and only of a design that has a library id.
func TestLogRoute(t *testing.T) {
	l := testLibrary(t, t.TempDir())
	prevNow := nowMs
	nowMs = func() int64 { return 1760000000000 }
	t.Cleanup(func() { nowMs = prevNow })
	kind, page, message := "error", "Main.dc.html", "TypeError: x is undefined"
	l.RecordLiveLog("alpha", &kind, &page, &message)
	a := &API{L: l, Session: func(r *http.Request) bool { return r.Header.Get("Cookie") == "ok" },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}

	req := httptest.NewRequest("GET", "/x/design/library/log?artifact=alpha", nil)
	req.Header.Set("Cookie", "ok")
	ans := a.PageLog(req)
	want := `{"artifact":"alpha","entries":[{"at":1760000000000,"kind":"error","page":"Main.dc.html","message":"TypeError: x is undefined"}]}`
	if ans.Status != 200 || string(ans.Body.(json.RawMessage)) != want || ans.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("log GET = %d %s %v", ans.Status, ans.Body, ans.Header)
	}

	req = httptest.NewRequest("GET", "/x?artifact=beta", nil)
	req.Header.Set("Cookie", "ok")
	if ans := a.PageLog(req); ans.Status != 200 || string(ans.Body.(json.RawMessage)) != `{"artifact":"beta","entries":[]}` {
		t.Fatalf("log of a design without entries = %d %s", ans.Status, ans.Body)
	}
	req = httptest.NewRequest("GET", "/x?artifact=Bad%20id", nil)
	req.Header.Set("Cookie", "ok")
	if ans := a.PageLog(req); ans.Status != 400 {
		t.Fatalf("log of a bad id = %d", ans.Status)
	}
	req = httptest.NewRequest("GET", "/x?artifact=alpha", nil)
	if ans := a.PageLog(req); ans.Status != 403 || !strings.Contains(string(ans.Body.(json.RawMessage)), "Forbidden") {
		t.Fatalf("log without a session = %d %s", ans.Status, ans.Body)
	}
	req = httptest.NewRequest("POST", "/x?artifact=alpha", nil)
	req.Header.Set("Cookie", "ok")
	if ans := a.PageLog(req); ans.Status != 405 {
		t.Fatalf("log POST = %d", ans.Status)
	}
}

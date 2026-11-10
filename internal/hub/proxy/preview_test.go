package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var pvKey = strings.Repeat("0a", 16)

func TestHubPassesPreviewPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/m/abc/__pv/1-" + pvKey + "/x": true,
		"/m/abc/__pv/6-" + pvKey:        true,
		"/m/abc/__pv/7-" + pvKey + "/x": false,
		"/m/abc/__pv/1/x":               false,
		"/m/abc/__pv/1-short/x":         false,
		"/m/abd/__pv/1-" + pvKey + "/x": false,
		"/m/abc/api/state":              false,
	} {
		if got := previewPath(path, "abc"); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
}

func TestPreviewCookies(t *testing.T) {
	prefix := "/m/abc/__pv/"
	for line, want := range map[string]bool{
		"__t_x_sid=1; Path=/m/abc/__pv/1-" + pvKey + "/; Secure; SameSite=None": true,
		"__t_x_sid=1; Path=/":                                                false,
		"hl_session=evil; Path=/m/abc/__pv/1-" + pvKey + "/":                 false,
		"__t_x_sid=1; Path=/m/abc/__pv/1-" + pvKey + "/; Domain=example.com": false,
		"__t_x_sid=1; Path=/m/other/__pv/1-" + pvKey + "/":                   false,
	} {
		if got := previewCookieKept(line, prefix); got != want {
			t.Errorf("%s: %v", line, got)
		}
	}
	r := httptest.NewRequest("GET", "/m/abc/__pv/1-"+pvKey+"/x", nil)
	r.Header.Set("Cookie", "__Host-hl_session=secret; __t_x_sid=1; other=2")
	if got := targetCookies(r); got != "__t_x_sid=1" {
		t.Fatalf("only the target's cookies go to the machine: %q", got)
	}
}

func TestReloadAsked(t *testing.T) {
	r := httptest.NewRequest("GET", "/dashboard?x=1", nil)
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if !ReloadAsked(r) {
		t.Fatal("a cross-site frame navigation is a preview's reload")
	}
	w := httptest.NewRecorder()
	ServeReload(w, r)
	if !strings.Contains(w.Body.String(), `"/dashboard?x=1"`) || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("reload page: %s", w.Body.String())
	}
	r.Header.Set("Sec-Fetch-Dest", "document")
	if ReloadAsked(r) {
		t.Fatal("a page navigation is not")
	}
}

func TestPreviewHeadersSandboxAtTheHub(t *testing.T) {
	h := http.Header{}
	h.Set("Clear-Site-Data", `"*"`)
	h.Set("Strict-Transport-Security", "max-age=1; includeSubDomains")
	h.Set("Content-Type", "text/html")
	previewHeaders(h)
	csp := strings.Join(h.Values("Content-Security-Policy"), "; ")
	if !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("csp: %q", csp)
	}
	if h.Get("Clear-Site-Data") != "" || h.Get("Strict-Transport-Security") != "" || h.Get("Content-Type") != "text/html" {
		t.Fatalf("headers: %v", h)
	}
}

func TestPreviewRefusalTellsTheFrameAndLogsTheTrace(t *testing.T) {
	var logged strings.Builder
	s := &Service{log: slog.New(slog.NewTextHandler(&logged, nil))}
	r := httptest.NewRequest(http.MethodGet, "/m/abc/__pv/1-"+pvKey+"/ui/index.html?__hl_trace=v-7", nil)
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	w := httptest.NewRecorder()
	answered := false
	s.previewRefused(w, r, "abc", errPreviewOffline, func() { answered = true })
	if answered || w.Code != http.StatusBadGateway {
		t.Fatalf("a frame: answered %v, status %d", answered, w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "hlViewError") || !strings.Contains(body, "hub-machine_offline") || !strings.Contains(body, "не на связи") {
		t.Errorf("the frame's page: %s", body)
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'sha256-") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("csp: %s", csp)
	}
	if line := logged.String(); !strings.Contains(line, "trace=v-7") || !strings.Contains(line, "code=machine_offline") || strings.Contains(line, pvKey) {
		t.Errorf("log: %s", line)
	}

	plain := httptest.NewRequest(http.MethodGet, "/m/abc/__pv/1-"+pvKey+"/x", nil)
	plain.Header.Set("X-Hl-Trace", "bad trace!")
	s.previewRefused(httptest.NewRecorder(), plain, "abc", errPreviewOffline, func() { answered = true })
	if !answered {
		t.Error("a request that is not a frame got the frame's page")
	}
}

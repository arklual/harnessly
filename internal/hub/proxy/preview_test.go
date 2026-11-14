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
		"/m/abc/__pv/7-" + pvKey + "/x": true,
		"/m/abc/__pv/9-" + pvKey + "/x": false,
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
	previewHeaders(h, false)
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

// A document a slot's page holds in a frame (a Storybook's canvas) has that page, with its opaque origin, among its
// ancestors: no frame-ancestors source admits it, so the answer to a frame of the slot carries none, and stays sandboxed.
func TestPreviewHeadersLetASlotFrameItsOwnDocuments(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/m/m1/__pv/1-0123456789abcdef0123456789abcdef/iframe.html", nil)
	if slotFrame(r) {
		t.Fatal("a request that says nothing of itself is not a slot's frame")
	}
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if slotFrame(r) {
		t.Fatal("the hub's own frame of the slot is not a frame inside the slot")
	}
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if !slotFrame(r) {
		t.Fatal("a frame navigating from the slot's opaque origin")
	}
	r.Header.Set("Sec-Fetch-Dest", "document")
	if slotFrame(r) {
		t.Fatal("a page navigation is not a frame")
	}

	h := http.Header{}
	h.Set("Strict-Transport-Security", "max-age=1")
	previewHeaders(h, true)
	csp := strings.Join(h.Values("Content-Security-Policy"), "; ")
	if !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") {
		t.Fatalf("a nested document left the sandbox: %q", csp)
	}
	if strings.Contains(csp, "frame-ancestors") || h.Get("X-Frame-Options") != "" {
		t.Fatalf("a nested document is refused by its own ancestor: csp %q, x-frame-options %q", csp, h.Get("X-Frame-Options"))
	}
	if h.Get("Strict-Transport-Security") != "" || h.Get("Referrer-Policy") != "no-referrer" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", h)
	}
}

// On a slot host the page's origin is real, and a document it frames of itself has that origin among its ancestors.
func TestSlotHeadersAdmitTheSlotItself(t *testing.T) {
	h := http.Header{}
	h.Set("X-Frame-Options", "DENY")
	slotHeaders(h, []string{"https://hub.example.net"})
	if got := h.Values("Content-Security-Policy"); len(got) != 1 || got[0] != "frame-ancestors 'self' https://hub.example.net" {
		t.Fatalf("csp: %q", got)
	}
	if h.Get("X-Frame-Options") != "" {
		t.Fatal("the older header would refuse the hub's frame")
	}
	h = http.Header{}
	slotHeaders(h, nil)
	if got := h.Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
		t.Fatalf("no hub origin known: %q", got)
	}
}

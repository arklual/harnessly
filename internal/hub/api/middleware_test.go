package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecovererTurnsAPanicInto500AndLogsIt(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	h := recoverer(log, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x?token=SECRETQUERY", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"error":"internal"`) {
		t.Errorf("answer = %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(logs.String(), "handler panicked") || !strings.Contains(logs.String(), "kaboom") || !strings.Contains(logs.String(), "route=/api/x") {
		t.Errorf("log = %s", logs.String())
	}
	if strings.Contains(logs.String(), "SECRETQUERY") {
		t.Error("the query is in the log")
	}
}

// http.ErrAbortHandler is how a handler drops a connection on purpose (the proxy does when a stream breaks): it is
// not an error to answer or to log.
func TestRecovererLetsAnAbortThrough(t *testing.T) {
	var logs bytes.Buffer
	h := recoverer(slog.New(slog.NewTextHandler(&logs, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v", v)
		}
		if logs.Len() != 0 {
			t.Errorf("logged %s", logs.String())
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed int
}

func (f *flushRecorder) Flush() { f.flushed++ }

func TestAccessLogLevelsAndPassThrough(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ip := func(*http.Request) string { return "203.0.113.7" }
	statusOf := map[string]int{"/ok": 200, "/missing": 404, "/boom": 502, "/healthz": 503, "/s/a/events": 401, "/assets/x.js": 404}
	h := accessLog(log, ip, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(statusOf[r.URL.Path]) }))
	for p := range statusOf {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p+"?secret=Q", nil))
	}
	want := map[string]string{
		"route=/ok ":          "level=DEBUG",
		"route=/missing ":     "level=INFO",
		"route=/boom ":        "level=WARN",
		"route=/healthz ":     "level=WARN", // a failing probe is worth a line
		"route=/s/a/events ":  "level=DEBUG",
		"route=/assets/x.js ": "level=DEBUG",
	}
	for sub, level := range want {
		var line string
		for _, l := range strings.Split(logs.String(), "\n") {
			if strings.Contains(l, sub) {
				line = l
			}
		}
		if !strings.Contains(line, level) || !strings.Contains(line, "ip=203.0.113.7") {
			t.Errorf("%q: %q, want %s", sub, line, level)
		}
	}
	if strings.Contains(logs.String(), "secret") {
		t.Error("the query is in the log")
	}

	// A stream must still flush through the wrapper.
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	accessLog(log, ip, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NewResponseController(w).Flush()
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/stream", nil))
	if rec.flushed != 1 {
		t.Errorf("flushed %d times", rec.flushed)
	}
}

func TestSecurityHeadersSkipTheRuntime(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	for path, headers := range map[string]bool{"/": true, "/api/config": true, "/m/abc/": false, "/m/": false} {
		rec := httptest.NewRecorder()
		securityHeaders(true, next).ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		got := rec.Header().Get("X-Frame-Options") != ""
		if got != headers || (headers && rec.Header().Get("Strict-Transport-Security") == "") || (!headers && len(rec.Header()) != 0) {
			t.Errorf("%s: headers %v", path, rec.Header())
		}
	}
}

func TestContentSecurityPolicyHashesOnlyInlineScripts(t *testing.T) {
	index := []byte(`<script>a()</script><script type="module" src="/assets/x.js"></script><SCRIPT SRC=/y.js></SCRIPT><script type="application/json">{"b":1}</script>`)
	csp := contentSecurityPolicy(index)
	if n := strings.Count(csp, "'sha256-"); n != 2 { // a() and the JSON block; the two with src are covered by 'self'
		t.Errorf("%d hashes in %q", n, csp)
	}
	for _, must := range []string{"default-src 'self'", "object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'", "connect-src 'self'"} {
		if !strings.Contains(csp, must) {
			t.Errorf("CSP lacks %q: %s", must, csp)
		}
	}
	// The hash is of the exact text between the tags.
	sum := sha256.Sum256([]byte("a()"))
	if want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"; !strings.Contains(csp, want) {
		t.Errorf("hash of a() (%s) missing: %s", want, csp)
	}
}

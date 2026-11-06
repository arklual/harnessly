package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/preview/devtools"
)

func TestPreviewStorage(t *testing.T) {
	dir := t.TempDir()
	mine := &previewSession{sub: "u1", target: devtools.Target{Kind: "port", Port: 5173}}
	other := &previewSession{sub: "u2", target: devtools.Target{Kind: "port", Port: 5173}}
	w := httptest.NewRecorder()
	servePreviewStorage(w, httptest.NewRequest("POST", "/__tk/storage", strings.NewReader(`{"token":"abc","theme":"dark"}`)), dir, mine)
	if w.Code != 204 {
		t.Fatalf("write: %d", w.Code)
	}
	w = httptest.NewRecorder()
	servePreviewStorage(w, httptest.NewRequest("GET", "/__tk/storage", nil), dir, mine)
	if !strings.Contains(w.Body.String(), `"token":"abc"`) || w.Header().Get("Access-Control-Allow-Origin") != "null" {
		t.Fatalf("read back: %s %v", w.Body.String(), w.Header())
	}
	w = httptest.NewRecorder()
	servePreviewStorage(w, httptest.NewRequest("GET", "/__tk/storage", nil), dir, other)
	if w.Body.String() != "{}" {
		t.Fatalf("another person's storage: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	big := `{"x":"` + strings.Repeat("a", previewStorageMax) + `"}`
	servePreviewStorage(w, httptest.NewRequest("POST", "/__tk/storage", strings.NewReader(big)), dir, mine)
	if w.Code != 413 {
		t.Fatalf("over 5 MB: %d", w.Code)
	}
	w = httptest.NewRecorder()
	servePreviewStorage(w, httptest.NewRequest("POST", "/__tk/storage", strings.NewReader(`{"x":1}`)), dir, mine)
	if w.Code != 400 {
		t.Fatalf("not strings: %d", w.Code)
	}
}

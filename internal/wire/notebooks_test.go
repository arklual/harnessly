package wire

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotebookRoutingAndOriginGuard(t *testing.T) {
	calls := 0
	s := &Server{Notebook: func(w http.ResponseWriter, r *http.Request, conv string) {
		calls++
		if conv != "tab-a" {
			t.Errorf("conversation: %q", conv)
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	for _, tc := range []struct {
		method, path, origin string
		status               int
	}{
		{"GET", "/api/v2/conversations/tab-a/notebook?path=book.ipynb", "", 204},
		{"POST", "/api/v2/conversations/tab-a/notebook?path=book.ipynb", "", 204},
		{"POST", "/agents/api/v2/conversations/tab-a/notebook?path=book.ipynb", "http://example.org", 204},
		{"POST", "/api/v2/conversations/tab-a/notebook", "https://other.example", 403},
		{"GET", "/api/v2/conversations/tab-a/notebook", "https://other.example", 403},
		{"PUT", "/api/v2/conversations/tab-a/notebook", "", 405},
		{"POST", "/api/v2/conversations/not%20an%20id/notebook", "", 400},
	} {
		r := httptest.NewRequest(tc.method, "http://example.org"+tc.path, nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s %s from %s: got %d want %d", tc.method, tc.path, tc.origin, w.Code, tc.status)
		}
	}
	if calls != 3 {
		t.Fatalf("refused route reached kernel handler: %d calls", calls)
	}
	s.Notebook = nil
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v2/conversations/tab-a/notebook", nil))
	if w.Code != 404 {
		t.Fatalf("unavailable backend: %d", w.Code)
	}
}

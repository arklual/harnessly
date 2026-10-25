package gate

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSONIsWrittenAsTheOldRuntimeWroteIt(t *testing.T) {
	got := string(encodeJSON(map[string]string{"a": "<b>&\u2028\\u2028\u2029"}))
	if want := "{\"a\":\"<b>&\u2028\\\\u2028\u2029\"}"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	w := httptest.NewRecorder()
	writeJSON(w, 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}})
	if w.Header().Get("Content-Type") != "application/json" || w.Body.String() != `{"error":"Unauthorized"}` {
		t.Fatalf("%v %q", w.Header(), w.Body)
	}
}

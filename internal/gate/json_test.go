package gate

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteRaw(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, 500, Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, nil)
	if w.Code != 500 || w.Header().Get("Content-Type") != "text/plain" || w.Body.String() != "Internal Server Error" {
		t.Fatalf("%d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	w = httptest.NewRecorder()
	writeJSON(w, 200, map[string]any{"valid": true}, nil)
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" || w.Body.String() != `{"valid":true}` {
		t.Fatalf("%q", w.Body.String())
	}
}

func TestQueryReachesTheOldRuntimeAsWritten(t *testing.T) {
	var got string
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.URL.RawQuery }))
	defer old.Close()
	f, err := New(Config{Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(f)
	defer front.Close()
	res, err := http.Get(front.URL + "/api/x?directory=/a;b&y=%zz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got != "directory=/a;b&y=%zz" {
		t.Fatalf("query %q", got)
	}
}

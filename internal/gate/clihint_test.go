package gate

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func get(t *testing.T, url string) (int, http.Header, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(raw)
}

// A standalone gate says which header a CLI route wants when it refuses; other answers are not touched.
func TestStandaloneCLIForbiddenNamesTheTokenHeader(t *testing.T) {
	forbidden := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Header().Set("ETag", `"x"`)
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, body)
		}
	}
	match := func(prefix string) func(*http.Request) bool {
		return func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, prefix) }
	}
	f, err := New(Config{Listen: "127.0.0.1:0", DataDir: t.TempDir(), CountEvery: time.Hour, TokenHeaders: []string{"x-acme-token"}},
		Route{Match: match("/api/cli/bare"), Handler: forbidden(bareForbidden)},
		Route{Match: match("/agents/api/cli/bare"), Handler: forbidden(bareForbidden + "\n")},
		Route{Match: match("/api/cli/other"), Handler: forbidden(`{"error":"Saving is only available"}`)},
		Route{Match: match("/api/workspace"), Handler: forbidden(bareForbidden)},
		Route{Match: match("/api/cli/ok"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(201)
			_, _ = io.WriteString(w, "fine")
		})})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	defer srv.Close()

	for _, p := range []string{"/api/cli/bare", "/agents/api/cli/bare"} {
		code, h, body := get(t, srv.URL+p)
		if code != 403 || !strings.Contains(body, "x-acme-token request header") || !strings.Contains(body, "session cookie is not accepted") {
			t.Fatalf("%s: %d %s", p, code, body)
		}
		if h.Get("ETag") != "" || h.Get("Content-Length") == strconv.Itoa(len(bareForbidden)) {
			t.Fatalf("%s: stale headers %v", p, h)
		}
	}
	if code, _, body := get(t, srv.URL+"/api/cli/other"); code != 403 || body != `{"error":"Saving is only available"}` {
		t.Fatalf("another 403 is kept: %d %s", code, body)
	}
	if code, _, body := get(t, srv.URL+"/api/workspace"); code != 403 || body != bareForbidden {
		t.Fatalf("outside /api/cli/ the answer is kept: %d %s", code, body)
	}
	if code, _, body := get(t, srv.URL+"/api/cli/ok"); code != 201 || body != "fine" {
		t.Fatalf("%d %s", code, body)
	}
}

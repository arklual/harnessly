package gate

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBodyRouteShadowComparesStatusHeadersAndBytes(t *testing.T) {
	serve := func(body, etag string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(200)
			io.WriteString(w, body)
		}
	}
	old := httptest.NewServer(serve("png", `"a"`))
	defer old.Close()
	routes := []BodyRoute{
		{Slice: "files", Match: func(r *http.Request) bool { return r.URL.Path == "/same" }, Serve: serve("png", `"a"`)},
		{Slice: "files", Match: func(r *http.Request) bool { return r.URL.Path == "/bytes" }, Serve: serve("pnG", `"a"`)},
		{Slice: "files", Match: func(r *http.Request) bool { return r.URL.Path == "/etag" }, Serve: serve("png", `"b"`)},
		{Slice: "files", Match: func(r *http.Request) bool { return r.URL.Path == "/empty" }, Serve: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(403)
		}},
	}
	f, dir := newGate(t, strings.TrimPrefix(old.URL, "http://"))
	f.Add(f.AsBodyRoutes(routes...)...)
	if err := os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["files-shadow"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, p := range []string{"/same", "/bytes", "/etag", "/empty"} {
		res, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(b) != "png" {
			t.Fatalf("%s: the caller gets the old answer, got %q", p, b)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.shadows.mu.Lock()
		m, d := f.shadows.matched["files"], f.shadows.differ["files"]
		f.shadows.mu.Unlock()
		if m+d == 4 {
			if m != 1 || d != 3 {
				t.Fatalf("matched %d, differ %d", m, d)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("compared %d", m+d)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// On, the Go handler answers; other methods never reach a GET-only route.
	if err := os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["files"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.slices.at = time.Time{}
	res, err := http.Get(srv.URL + "/bytes")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "pnG" {
		t.Fatalf("slice on: %q", b)
	}
}

func TestEmptyAnswersCountAsLengthZero(t *testing.T) {
	a := newDigest()
	a.status, a.header = 403, http.Header{"Content-Length": {"0"}}
	b := newDigest()
	b.status, b.header = 403, http.Header{}
	if why := sameBody(a, b); why != "" {
		t.Fatal(why)
	}
}

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

func TestShadowOutlivesTheAnswer(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(200)
		io.WriteString(w, "png")
	}))
	defer old.Close()
	f, dir := newGate(t, strings.TrimPrefix(old.URL, "http://"))
	f.Add(f.AsBodyRoutes(BodyRoute{Slice: "files", Match: func(r *http.Request) bool { return true }, Serve: func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond) // a read of the network, after the old answer went out
		if r.Context().Err() != nil {
			w.WriteHeader(504)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(200)
		io.WriteString(w, "png")
	}})...)
	if err := os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["files-shadow"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		f.shadows.mu.Lock()
		m, d := f.shadows.matched["files"], f.shadows.differ["files"]
		f.shadows.mu.Unlock()
		if m+d == 1 {
			if d != 0 {
				t.Fatal("the shadow ran on the finished request's context")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no comparison")
		}
	}
}

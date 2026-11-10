package design

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLibScriptIsKeptByVersionAndGzipped(t *testing.T) {
	body := strings.Repeat("var a = 1; // the board script\n", 400)
	v := libScriptOf(body).version

	r := httptest.NewRequest("GET", scriptPath+"?"+libScriptParam+"="+v, nil)
	r.Header.Set("Accept-Encoding", "gzip, br")
	w := httptest.NewRecorder()
	serveLibScript(w, r, body, immutable, "public, max-age=300", false)
	if w.Code != 200 || w.Header().Get("Cache-Control") != immutable || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("versioned: %d %v", w.Code, w.Header())
	}
	if n := w.Body.Len(); n == 0 || n > len(body)/10 {
		t.Fatalf("not compressed: %d of %d", n, len(body))
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(zr); string(got) != body {
		t.Fatal("the gzipped body is not the script")
	}

	// An address without the version, or with an old one, is not kept for good.
	for _, q := range []string{"", "?" + libScriptParam + "=0000000000000000"} {
		r = httptest.NewRequest("GET", scriptPath+q, nil)
		r.Header.Set("Accept-Encoding", "identity;q=1, gzip;q=0")
		w = httptest.NewRecorder()
		serveLibScript(w, r, body, immutable, "public, max-age=300", false)
		if w.Header().Get("Cache-Control") != "public, max-age=300" || w.Header().Get("ETag") != `"`+v+`"` {
			t.Fatalf("%q: %v", q, w.Header())
		}
	}

	r = httptest.NewRequest("GET", scriptPath, nil)
	r.Header.Set("If-None-Match", `W/"`+v+`"`)
	w = httptest.NewRecorder()
	serveLibScript(w, r, body, immutable, "public, max-age=300", false)
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatalf("a known ETag: %d, %d bytes", w.Code, w.Body.Len())
	}

	if got := withClientAt("<head><title>x</title>", "p1", false, scriptPath+"?"+libScriptParam+"="+v); !strings.Contains(got, `src="/__lib/client.js?v=`+v+`"`) {
		t.Fatalf("the board names the script without its version: %s", got)
	}
}

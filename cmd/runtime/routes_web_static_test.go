package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func staticTestConfig(t *testing.T) (*webConfig, string) {
	t.Helper()
	dir := webDir(t)
	big := strings.Repeat("export const answer = 42; // padding padding padding\n", 200)
	_ = os.WriteFile(filepath.Join(dir, "assets", "big-9f8e.js"), []byte(big), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "assets", "pic-1.png"), bytes.Repeat([]byte{0x89, 'P', 'N', 'G', 0, 0}, 500), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte(testIndex+strings.Repeat("<!-- padding -->", 200)), 0o644)
	t.Setenv("RUNTIME_WEB_DIR", dir)
	t.Setenv("RUNTIME_WEB", "")
	t.Setenv("RUNTIME_APP_BASE_PATH", "")
	c := webConfigFromEnv(true)
	if c == nil {
		t.Fatal("no page")
	}
	return c, big
}

var acceptGzip = http.Header{"Accept-Encoding": {"gzip, br"}}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestStaticGzipWhenAccepted(t *testing.T) {
	c, big := staticTestConfig(t)
	w := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", acceptGzip)
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if gunzip(t, w.Body.Bytes()) != big || w.Body.Len() >= len(big)/4 {
		t.Fatalf("body is %d bytes of %d", w.Body.Len(), len(big))
	}
	if w.Header().Get("Content-Length") != itoa(w.Body.Len()) || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") ||
		!strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("%v", w.Header())
	}
	if etag := w.Header().Get("ETag"); !strings.HasSuffix(etag, `-gz"`) {
		t.Fatalf("etag %q", etag)
	}
	// Same bytes the second time, from the kept copy.
	if w2 := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", acceptGzip); !bytes.Equal(w2.Body.Bytes(), w.Body.Bytes()) {
		t.Fatal("the gzipped copy changed between requests")
	}
	if w := serveWeb(c, "HEAD", "/agents/assets/big-9f8e.js", acceptGzip); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("head: %d %v", w.Code, w.Header())
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestStaticIdentityWhenNotAccepted(t *testing.T) {
	c, big := staticTestConfig(t)
	for _, h := range []http.Header{nil, {"Accept-Encoding": {"br"}}, {"Accept-Encoding": {"gzip;q=0"}}, {"Accept-Encoding": {"identity"}}} {
		w := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", h)
		if w.Code != 200 || w.Header().Get("Content-Encoding") != "" || w.Body.String() != big {
			t.Fatalf("%v: %d %v", h, w.Code, w.Header())
		}
		if w.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("the plain answer also varies on the encoding: %v", w.Header())
		}
		if etag := w.Header().Get("ETag"); etag == "" || strings.HasSuffix(etag, `-gz"`) {
			t.Fatalf("etag %q", etag)
		}
	}
}

func TestStaticGzipConditionalRequests(t *testing.T) {
	c, _ := staticTestConfig(t)
	gz := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", acceptGzip)
	plain := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", nil)
	gzTag, plainTag := gz.Header().Get("ETag"), plain.Header().Get("ETag")
	if gzTag == plainTag {
		t.Fatalf("both variants share the tag %q", gzTag)
	}
	w := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", http.Header{"Accept-Encoding": {"gzip"}, "If-None-Match": {gzTag}})
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 || w.Header().Get("Vary") != "Accept-Encoding" || w.Header().Get("ETag") != gzTag {
		t.Fatalf("gzip tag: %d %v", w.Code, w.Header())
	}
	w = serveWeb(c, "GET", "/agents/assets/big-9f8e.js", http.Header{"If-None-Match": {plainTag}})
	if w.Code != http.StatusNotModified {
		t.Fatalf("plain tag: %d", w.Code)
	}
	// A tag of the other variant does not match.
	w = serveWeb(c, "GET", "/agents/assets/big-9f8e.js", http.Header{"Accept-Encoding": {"gzip"}, "If-None-Match": {plainTag}})
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("other variant's tag: %d %v", w.Code, w.Header())
	}
}

func TestStaticSmallAndCompressedTypesStayPlain(t *testing.T) {
	c, _ := staticTestConfig(t)
	for _, p := range []string{"/agents/assets/app-1a2b.js", "/agents/logo.svg", "/agents/assets/pic-1.png"} {
		w := serveWeb(c, "GET", p, acceptGzip)
		if w.Code != 200 || w.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%s: %d %v", p, w.Code, w.Header())
		}
	}
	if w := serveWeb(c, "GET", "/agents/assets/app-1a2b.js", acceptGzip); w.Body.String() != "console.log(1)" {
		t.Fatalf("small file: %q", w.Body)
	}
}

func TestStaticRangeIsNotCompressed(t *testing.T) {
	c, big := staticTestConfig(t)
	h := http.Header{"Accept-Encoding": {"gzip"}, "Range": {"bytes=0-9"}}
	w := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", h)
	if w.Code != http.StatusPartialContent || w.Header().Get("Content-Encoding") != "" || w.Body.String() != big[:10] {
		t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body)
	}
}

func TestStaticPageGzipped(t *testing.T) {
	c, _ := staticTestConfig(t)
	w := serveWeb(c, "GET", "/agents/w/ws-a", acceptGzip)
	body := gunzip(t, w.Body.Bytes())
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" ||
		w.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(body, `<base href="/agents/" />`) {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	w = serveWeb(c, "GET", "/agents/", nil)
	if w.Header().Get("Content-Encoding") != "" || !strings.Contains(w.Body.String(), `<base href="/agents/" />`) || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("plain page: %v", w.Header())
	}
}

func TestStaticChangedFileIsServedAgain(t *testing.T) {
	c, _ := staticTestConfig(t)
	dir := os.Getenv("RUNTIME_WEB_DIR")
	p := filepath.Join(dir, "assets", "big-9f8e.js")
	first := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", acceptGzip)
	next := strings.Repeat("let changed = true; // other padding\n", 100)
	_ = os.WriteFile(p, []byte(next), 0o644)
	w := serveWeb(c, "GET", "/agents/assets/big-9f8e.js", acceptGzip)
	if gunzip(t, w.Body.Bytes()) != next || w.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatal("a changed file is served from the old copy")
	}
}

package webstatic

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var big = strings.Repeat("export const answer = 42; // padding padding padding\n", 200)

func testFiles() fstest.MapFS {
	return fstest.MapFS{
		"assets/app-1a2b.js": {Data: []byte(big)},
		"assets/pic-1.png":   {Data: bytes.Repeat([]byte{0x89, 'P', 'N', 'G', 0, 0}, 500)},
		"small.js":           {Data: []byte("console.log(1)")},
	}
}

func get(t *testing.T, c *Cache, name string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/"+name, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	if !c.Serve(w, r, testFiles(), name) {
		t.Fatalf("%s: not served", name)
	}
	return w
}

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

func TestServeGzipsForBrowsersThatTakeIt(t *testing.T) {
	c := &Cache{}
	plain := get(t, c, "assets/app-1a2b.js")
	if plain.Code != 200 || plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != big {
		t.Fatalf("plain: %d %q, %d bytes", plain.Code, plain.Header().Get("Content-Encoding"), plain.Body.Len())
	}
	zipped := get(t, c, "assets/app-1a2b.js", "Accept-Encoding", "gzip, br")
	if zipped.Code != 200 || zipped.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip: %d %q", zipped.Code, zipped.Header().Get("Content-Encoding"))
	}
	if zipped.Body.Len()*3 > len(big) {
		t.Errorf("gzip: %d bytes of %d", zipped.Body.Len(), len(big))
	}
	if gunzip(t, zipped.Body.Bytes()) != big {
		t.Error("gzip: the body does not unpack to the file")
	}
	for _, w := range []*httptest.ResponseRecorder{plain, zipped} {
		if w.Header().Get("Vary") != "Accept-Encoding" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") ||
			!strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
			t.Errorf("headers: %v", w.Header())
		}
	}
	pt, zt := plain.Header().Get("ETag"), zipped.Header().Get("ETag")
	if pt == "" || zt == "" || pt == zt {
		t.Errorf("tags: plain %q, gzip %q", pt, zt)
	}
	if w := get(t, c, "assets/app-1a2b.js", "Accept-Encoding", "gzip", "If-None-Match", zt); w.Code != 304 || w.Body.Len() != 0 {
		t.Errorf("gzip, same tag: %d", w.Code)
	}
	if w := get(t, c, "assets/app-1a2b.js", "Accept-Encoding", "gzip", "If-None-Match", pt); w.Code != 200 {
		t.Errorf("gzip, the plain copy's tag: %d", w.Code)
	}
	if w := get(t, c, "assets/app-1a2b.js", "Accept-Encoding", "gzip;q=0"); w.Header().Get("Content-Encoding") != "" {
		t.Error("gzip refused with q=0 was sent anyway")
	}
}

func TestServeLeavesRangesSmallFilesAndImagesPlain(t *testing.T) {
	c := &Cache{}
	w := get(t, c, "assets/app-1a2b.js", "Accept-Encoding", "gzip", "Range", "bytes=0-9")
	if w.Code != 206 || w.Header().Get("Content-Encoding") != "" || w.Body.String() != big[:10] {
		t.Errorf("range: %d %q %q", w.Code, w.Header().Get("Content-Encoding"), w.Body.String())
	}
	for _, name := range []string{"small.js", "assets/pic-1.png"} {
		w := get(t, c, name, "Accept-Encoding", "gzip")
		if w.Code != 200 || w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "" {
			t.Errorf("%s: %d %v", name, w.Code, w.Header())
		}
	}
	if w := get(t, c, "small.js"); w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("small.js: Cache-Control %q", w.Header().Get("Cache-Control"))
	}
}

func TestServeReportsAMissingFile(t *testing.T) {
	c := &Cache{}
	for _, name := range []string{"assets/none.js", "assets"} {
		w := httptest.NewRecorder()
		if c.Serve(w, httptest.NewRequest("GET", "/"+name, nil), testFiles(), name) {
			t.Errorf("%s: served", name)
		}
		if len(w.Header()) != 0 {
			t.Errorf("%s: headers written: %v", name, w.Header())
		}
	}
}

func TestWriteBody(t *testing.T) {
	page := []byte(strings.Repeat("<!-- padding -->", 200))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	WriteBody(w, r, page, nil)
	if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "Accept-Encoding" || !bytes.Equal(w.Body.Bytes(), page) {
		t.Errorf("plain: %v", w.Header())
	}
	r.Header.Set("Accept-Encoding", "gzip")
	for _, gz := range [][]byte{nil, Gzip(page)} {
		w = httptest.NewRecorder()
		WriteBody(w, r, page, gz)
		if w.Header().Get("Content-Encoding") != "gzip" || gunzip(t, w.Body.Bytes()) != string(page) {
			t.Errorf("gzip: %v", w.Header())
		}
	}
	w = httptest.NewRecorder()
	WriteBody(w, r, []byte("short"), nil)
	if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "" || w.Body.String() != "short" {
		t.Errorf("short: %v", w.Header())
	}
	head := httptest.NewRequest(http.MethodHead, "/", nil)
	w = httptest.NewRecorder()
	WriteBody(w, head, page, nil)
	if w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Errorf("head: %d bytes, %v", w.Body.Len(), w.Header())
	}
}

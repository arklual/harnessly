package design

import (
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func engineConst(t *testing.T, src, name string) string {
	t.Helper()
	m := regexp.MustCompile(`var ` + name + ` = "([^"]+)";`).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("support.js has no %s", name)
	}
	return m[1]
}

// The vendored files are the bytes their SRI names. The engine is not in the tree (see engine.go): with
// HARNESSLY_TEST_ENGINE naming a fetched support.js, the test also holds its URLs and SRIs to the vendored files.
func TestVendorFilesMatchEngineSRI(t *testing.T) {
	if len(vendorFiles) != 3 {
		t.Fatalf("vendorFiles has %d entries", len(vendorFiles))
	}
	for url, f := range vendorFiles {
		sum := sha512.Sum384(f.body())
		if got := "sha384-" + base64.StdEncoding.EncodeToString(sum[:]); got != f.SRI {
			t.Fatalf("%s: file %s, declared %s", url, got, f.SRI)
		}
	}
	engine := os.Getenv("HARNESSLY_TEST_ENGINE")
	if engine == "" {
		t.Skip("HARNESSLY_TEST_ENGINE names no engine: the vendored files are checked against their own SRI only")
	}
	raw, err := os.ReadFile(engine)
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, prefix := range []string{"REACT", "REACT_DOM", "BABEL"} {
		url, sri := engineConst(t, src, prefix+"_URL"), engineConst(t, src, prefix+"_SRI")
		f, ok := vendorFiles[url]
		if !ok {
			t.Fatalf("%s is not vendored", url)
		}
		if f.SRI != sri {
			t.Fatalf("%s: declared %s, engine %s", url, f.SRI, sri)
		}
	}
}

func TestVendorServedImmutable(t *testing.T) {
	l, _ := hashedLibrary(t)
	f := vendorFiles["https://unpkg.com/react@18.3.1/umd/react.production.min.js"]
	w := hashedGet(l, vendorPrefix+f.Name)
	if w.Code != 200 || w.Header().Get("Cache-Control") != immutable {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("type %q", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Content-Encoding") != "" || w.Body.Len() != len(f.body()) {
		t.Fatal("identity expected without Accept-Encoding gzip")
	}
	r := httptest.NewRequest("GET", vendorPrefix+f.Name, nil)
	r.Host = "127.0.0.1:18470"
	r.Header.Set("Accept-Encoding", "gzip")
	gw := httptest.NewRecorder()
	l.serveLibrary(gw, r, 18470)
	if gw.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("not gzipped")
	}
	zr, err := gzip.NewReader(gw.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if string(plain) != string(f.body()) {
		t.Fatal("gzip body differs")
	}
	if nw := hashedGet(l, vendorPrefix+"nope.js"); nw.Code != 404 {
		t.Fatalf("unknown vendor name: %d", nw.Code)
	}
}

func TestBoardHTMLHasResourcesBeforeEngine(t *testing.T) {
	l, root := hashedLibrary(t)
	writeFile(t, filepath.Join(root, "one", "project", "Main.dc.html"),
		`<html><head><title>x</title><script src="./support.js"></script></head><body></body></html>`)
	html := hashedBoard(t, l, "one")
	res, eng := strings.Index(html, `"__resources"`), strings.Index(html, `src="./support.js"`)
	if res < 0 || eng < 0 || res > eng {
		t.Fatalf("resources at %d, engine at %d", res, eng)
	}
	for _, f := range vendorFiles {
		if !strings.Contains(html, `"`+f.URL+`":"`+vendorPrefix+f.Name+`"`) {
			t.Fatalf("no mapping for %s", f.URL)
		}
	}
	if strings.Contains(html, "unpkg.com/react@17") {
		t.Fatal("unexpected mapping")
	}
}

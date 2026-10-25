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

func TestVendorFilesMatchEngineSRI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("newproject", "project", "support.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	want := map[string]string{"REACT": "", "REACT_DOM": "", "BABEL": ""}
	if len(vendorFiles) != len(want) {
		t.Fatalf("vendorFiles has %d entries", len(vendorFiles))
	}
	for prefix := range want {
		url, sri := engineConst(t, src, prefix+"_URL"), engineConst(t, src, prefix+"_SRI")
		f, ok := vendorFiles[url]
		if !ok {
			t.Fatalf("%s is not vendored", url)
		}
		sum := sha512.Sum384(f.body())
		got := "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
		if got != sri || f.SRI != sri {
			t.Fatalf("%s: file %s, declared %s, engine %s", url, got, f.SRI, sri)
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

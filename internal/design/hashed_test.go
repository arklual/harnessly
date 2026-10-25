package design

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var hashedURLRe = regexp.MustCompile(`/__h/[0-9a-f]{32}/[^"'?#]+`)

func heavyBody(seed string) string {
	return strings.Repeat(seed+" console.log(1);\n", 6000) // > 64 KB
}

func hashedLibrary(t *testing.T) (*Library, string) {
	t.Helper()
	root := t.TempDir()
	l := testLibrary(t, root)
	l.srv = &server{}
	return l, root
}

func hashedGet(l *Library, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "127.0.0.1:18470"
	r.Header.Set("Accept-Encoding", "br")
	l.serveLibrary(w, r, 18470)
	return w
}

func hashedBoard(t *testing.T, l *Library, id string) string {
	t.Helper()
	w := hashedGet(l, "/a/"+id+"/p/Main.dc.html")
	if w.Code != 200 {
		t.Fatalf("board %s: %d", id, w.Code)
	}
	return w.Body.String()
}

const hashedHTML = `<html><body><script src="lib/_ds_bundle.js"></script><script src="_ds/x/_ds_bundle.js"></script></body></html>`

func TestHashedSameFileTwoProjectsOneURL(t *testing.T) {
	l, root := hashedLibrary(t)
	body := heavyBody("ds")
	for _, id := range []string{"one", "two"} {
		p := filepath.Join(root, id, "project")
		writeFile(t, filepath.Join(p, "Main.dc.html"), hashedHTML)
		writeFile(t, filepath.Join(p, "lib", "_ds_bundle.js"), body)
		writeFile(t, filepath.Join(p, "_ds", "x", "_ds_bundle.js"), body)
	}
	one, two := hashedURLRe.FindAllString(hashedBoard(t, l, "one"), -1), hashedURLRe.FindAllString(hashedBoard(t, l, "two"), -1)
	if len(one) != 2 || len(two) != 2 || one[0] != one[1] || one[0] != two[0] || two[0] != two[1] {
		t.Fatalf("want one shared URL, got %v %v", one, two)
	}
	if !strings.HasSuffix(one[0], "/_ds_bundle.js") {
		t.Fatalf("base name missing: %s", one[0])
	}
	if strings.Contains(hashedBoard(t, l, "one"), "tkv=") {
		t.Fatal("heavy file kept tkv")
	}
}

func hashOf(t *testing.T, file string) string {
	t.Helper()
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := (&hashIndex{}).sum(file, st)
	return h
}

func TestHashedServesImmutable(t *testing.T) {
	l, root := hashedLibrary(t)
	body := heavyBody("ds")
	p := filepath.Join(root, "one", "project")
	writeFile(t, filepath.Join(p, "Main.dc.html"), hashedHTML)
	writeFile(t, filepath.Join(p, "lib", "_ds_bundle.js"), body)
	writeFile(t, filepath.Join(p, "_ds", "x", "_ds_bundle.js"), body)
	u := hashedURLRe.FindString(hashedBoard(t, l, "one"))
	w := hashedGet(l, u)
	if w.Code != 200 || w.Header().Get("Cache-Control") != immutable || w.Body.String() != body {
		t.Fatalf("got %d %q, body equal %v", w.Code, w.Header().Get("Cache-Control"), w.Body.String() == body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content type %q", ct)
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("no ETag")
	}
	hr := httptest.NewRecorder()
	req := httptest.NewRequest("HEAD", u, nil)
	req.Host = "127.0.0.1:18470"
	req.Header.Set("Accept-Encoding", "br")
	l.serveLibrary(hr, req, 18470)
	if hr.Code != 200 || hr.Body.Len() != 0 || hr.Header().Get("Cache-Control") != immutable {
		t.Fatalf("HEAD: %d body %d", hr.Code, hr.Body.Len())
	}
	gw := httptest.NewRecorder()
	req = httptest.NewRequest("GET", u, nil)
	req.Host = "127.0.0.1:18470"
	req.Header.Set("Accept-Encoding", "gzip")
	l.serveLibrary(gw, req, 18470)
	if gw.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("not gzip: %d %q", gw.Code, gw.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(gw.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if string(plain) != body {
		t.Fatal("gzip body differs")
	}
}

func TestHashedOnlyFitFiles(t *testing.T) {
	l, root := hashedLibrary(t)
	p := filepath.Join(root, "one", "project")
	writeFile(t, filepath.Join(p, "Main.dc.html"), `<link rel="stylesheet" href="a.css"><script src="dyn.js"></script><script src="static.js"></script><script src="mod.js"></script><script src="meta.js"></script><script src="ok.js"></script><script src="computed.js"></script><script src="minimp.js"></script><script src="minexp.js"></script><script src="side.js"></script><script src="side2.js"></script><script src="ws.js"></script><script src="dynsp.js"></script><script src="dq.js"></script><script src="metaq.js"></script><link rel="preload" href="f.woff2"><img src="p.png">`)
	writeFile(t, filepath.Join(p, "a.css"), strings.Repeat("a{background:url(./x.png)}\n", 4000))
	writeFile(t, filepath.Join(p, "dyn.js"), heavyBody("x")+"import('./chunk.js')")
	writeFile(t, filepath.Join(p, "static.js"), heavyBody("x")+`import {a} from "./a.js"`)
	writeFile(t, filepath.Join(p, "mod.js"), heavyBody("x")+`export * from '../b.js'`)
	writeFile(t, filepath.Join(p, "meta.js"), heavyBody("x")+"new URL('./w', import.meta.url)")
	writeFile(t, filepath.Join(p, "minimp.js"), heavyBody("x")+`import{a}from"./x.js"`)
	writeFile(t, filepath.Join(p, "minexp.js"), heavyBody("x")+`export*from"../b.js"`)
	writeFile(t, filepath.Join(p, "side.js"), heavyBody("x")+`import "./x.js"`)
	writeFile(t, filepath.Join(p, "side2.js"), heavyBody("x")+`import"./x.js"`)
	writeFile(t, filepath.Join(p, "ws.js"), heavyBody("x")+`import {a} from  "./x"`)
	writeFile(t, filepath.Join(p, "dq.js"), heavyBody("x")+`import("./x")`)
	writeFile(t, filepath.Join(p, "metaq.js"), heavyBody("x")+`new URL("a.png", import.meta.url)`)
	writeFile(t, filepath.Join(p, "dynsp.js"), heavyBody("x")+`import ( './x')`)
	writeFile(t, filepath.Join(p, "computed.js"), heavyBody("c")+`await import(route.module); "<define:import.meta.env>"`)
	writeFile(t, filepath.Join(p, "ok.js"), heavyBody("ok"))
	writeFile(t, filepath.Join(p, "f.woff2"), heavyBody("font"))
	writeFile(t, filepath.Join(p, "p.png"), heavyBody("png"))
	out := hashedBoard(t, l, "one")
	for _, kept := range []string{"a.css?tkv=", "dyn.js?tkv=", "static.js?tkv=", "mod.js?tkv=", "meta.js?tkv=", "minimp.js?tkv=", "minexp.js?tkv=", "side.js?tkv=", "side2.js?tkv=", "ws.js?tkv=", "dynsp.js?tkv=", "dq.js?tkv=", "metaq.js?tkv="} {
		if !strings.Contains(out, kept) {
			t.Fatalf("%s lost its tkv: %s", kept, out)
		}
	}
	for _, name := range []string{"ok.js", "computed.js", "f.woff2", "p.png"} {
		if !strings.Contains(out, "/"+name+`"`) || !strings.Contains(out, hashedPrefix) || strings.Contains(out, name+"?tkv=") {
			t.Fatalf("%s not hashed: %s", name, out)
		}
	}
	if n := len(hashedURLRe.FindAllString(out, -1)); n != 4 {
		t.Fatalf("hashed urls: %d", n)
	}
}

func TestHashedAddressesTheServedFile(t *testing.T) {
	root := t.TempDir()
	own, engine := filepath.Join(root, "support.js"), filepath.Join(root, "engine.js")
	writeFile(t, own, heavyBody("own"))
	writeFile(t, engine, heavyBody("engine"))
	l := testLibrary(t, root)
	l.srv = &server{}
	swap := func(real string) string {
		if real == own {
			return engine
		}
		return real
	}
	out := withAssetVersions(`<script src="support.js"></script>`, root, root, swap, l.hashed)
	u := hashedURLRe.FindString(out)
	want := hashOf(t, engine)
	if u == "" || !strings.Contains(u, want) || strings.Contains(u, hashOf(t, own)) {
		t.Fatalf("url %q, want hash %s", u, want)
	}
	w := hashedGet(l, u)
	if w.Code != 200 || w.Body.String() != heavyBody("engine") {
		t.Fatalf("served %d, engine bytes %v", w.Code, w.Body.String() == heavyBody("engine"))
	}
}

func TestHashedServeChecksTheVersion(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "a.js")
	writeFile(t, f, heavyBody("a"))
	st, _ := os.Stat(f)
	x := &hashIndex{}
	u, _ := x.urlFor(f, st)
	hash := strings.Split(u, "/")[2]
	writeFile(t, f, heavyBody("b"))
	if _, _, ok := x.find(hash); ok {
		t.Fatal("changed file found under the old hash")
	}
	// the stat urlFor was given is stale: bytes read are of another version, so it is not filed
	writeFile(t, f, heavyBody("c"))
	if _, ok := x.urlFor(f, st); ok {
		t.Fatal("stale stat was filed")
	}
}

func TestHashedChangedFileNewURL(t *testing.T) {
	l, root := hashedLibrary(t)
	p := filepath.Join(root, "one", "project")
	file := filepath.Join(p, "lib", "_ds_bundle.js")
	writeFile(t, filepath.Join(p, "Main.dc.html"), `<script src="lib/_ds_bundle.js"></script>`)
	writeFile(t, file, heavyBody("old"))
	old := hashedURLRe.FindString(hashedBoard(t, l, "one"))
	if hashedGet(l, old).Code != 200 {
		t.Fatal("old url not served")
	}
	writeFile(t, file, heavyBody("new"))
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(file, later, later)
	fresh := hashedURLRe.FindString(hashedBoard(t, l, "one"))
	if fresh == "" || fresh == old {
		t.Fatalf("address did not change: %q %q", old, fresh)
	}
	if hashedGet(l, fresh).Code != 200 {
		t.Fatal("new url not served")
	}
	if code := hashedGet(l, old).Code; code != 404 {
		t.Fatalf("old url: %d", code)
	}
}

func TestHashedUnknownHash404(t *testing.T) {
	l, _ := hashedLibrary(t)
	for _, p := range []string{"/__h/" + strings.Repeat("0", 32) + "/x.js", "/__h/zz/x.js", "/__h/", "/__h/" + strings.Repeat("a", 32)} {
		if code := hashedGet(l, p).Code; code != 404 {
			t.Fatalf("%s: %d", p, code)
		}
	}
}

func TestHashedOnlyIndexedFiles(t *testing.T) {
	l, root := hashedLibrary(t)
	p := filepath.Join(root, "one", "project")
	writeFile(t, filepath.Join(p, "Main.dc.html"), `<script src="lib/a.js"></script>`)
	writeFile(t, filepath.Join(p, "lib", "a.js"), heavyBody("a"))
	writeFile(t, filepath.Join(p, "lib", "secret.js"), heavyBody("secret"))
	hashedBoard(t, l, "one")
	// secret.js was never named by a served page: its hash must not open it
	sum := hashOf(t, filepath.Join(p, "lib", "secret.js"))
	if code := hashedGet(l, hashedPrefix+sum+"/secret.js").Code; code != 404 {
		t.Fatalf("unindexed file served: %d", code)
	}
}

func TestHashedSmallFileKeepsTkv(t *testing.T) {
	l, root := hashedLibrary(t)
	p := filepath.Join(root, "one", "project")
	writeFile(t, filepath.Join(p, "Main.dc.html"), `<script src="lib/small.js"></script>`)
	writeFile(t, filepath.Join(p, "lib", "small.js"), "console.log(1)")
	out := hashedBoard(t, l, "one")
	if !strings.Contains(out, "lib/small.js?tkv=") || strings.Contains(out, hashedPrefix) {
		t.Fatalf("small file: %s", out)
	}
}

func TestHashedIndexEvictsOldest(t *testing.T) {
	root := t.TempDir()
	x := &hashIndex{max: 3}
	var first string
	for i := 0; i < 5; i++ {
		f := filepath.Join(root, "f"+strconv.Itoa(i)+".js")
		writeFile(t, f, heavyBody(f))
		st, _ := os.Stat(f)
		u, ok := x.urlFor(f, st)
		if !ok {
			t.Fatal("not indexed")
		}
		if i == 0 {
			first = u
		}
	}
	if x.size() != 3 {
		t.Fatalf("size %d", x.size())
	}
	if _, ok := x.lookup(strings.Split(first, "/")[2]); ok {
		t.Fatal("oldest entry survived")
	}
}

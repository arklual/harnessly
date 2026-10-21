package design

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func kitRig(t *testing.T) (*Library, func(path string, headers ...string) *httptest.ResponseRecorder) {
	t.Helper()
	root := t.TempDir()
	kits := filepath.Join(root, "kits")
	write := func(rel, body string) {
		p := filepath.Join(kits, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("acme-ds/0.16.0/bundle.r18.js", strings.Repeat("window.AcmeDS={};", 200))
	write("acme-ds/0.16.0/bundle.css", ".t-button{color:red}")
	write("acme-ds/0.16.0/README.md", "# kit")
	write("acme-ds/0.16.0/fonts/inter.woff2", "font")
	write("acme-ds/0.16.0/SHA256SUMS", "sums")
	write("acme-ds/0.6.1/bundle.js", "old")
	write("secret.txt", "do not serve")
	if err := os.MkdirAll(filepath.Join(root, "lib", "x", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := New(Config{LibraryDir: filepath.Join(root, "lib"), KitsDir: kits, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	l.srv = &server{}
	t.Cleanup(l.StopWatching)
	get := func(path string, headers ...string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18470"
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		l.serveLibrary(w, r, 18470)
		return w
	}
	return l, get
}

func TestAKitFileIsServedForGoodFromItsVersionAddress(t *testing.T) {
	_, get := kitRig(t)
	w := get("/kit/acme-ds/0.16.0/bundle.css")
	if w.Code != 200 || w.Body.String() != ".t-button{color:red}" {
		t.Fatalf("css: %d %q", w.Code, w.Body.String())
	}
	h := w.Header()
	if h.Get("Cache-Control") != "public, max-age=31536000, immutable" || h.Get("Content-Type") != "text/css; charset=utf-8" ||
		h.Get("X-Content-Type-Options") != "nosniff" || h.Get("ETag") == "" {
		t.Fatalf("headers: %v", h)
	}
	if w := get("/kit/acme-ds/0.16.0/fonts/inter.woff2"); w.Code != 200 || w.Header().Get("Content-Type") != "font/woff2" {
		t.Fatalf("font: %d %v", w.Code, w.Header())
	}
	if w := get("/kit/acme-ds/0.16.0/README.md"); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("readme: %d %v", w.Code, w.Header())
	}
	if w := get("/kit/acme-ds/0.6.1/bundle.js"); w.Code != 200 || w.Body.String() != "old" {
		t.Fatalf("another version: %d %q", w.Code, w.Body.String())
	}
	etag := get("/kit/acme-ds/0.16.0/bundle.css").Header().Get("ETag")
	if w := get("/kit/acme-ds/0.16.0/bundle.css", "If-None-Match", etag); w.Code != 304 {
		t.Fatalf("revalidation: %d", w.Code)
	}
}

func TestALargeKitScriptIsSentCompressed(t *testing.T) {
	_, get := kitRig(t)
	w := get("/kit/acme-ds/0.16.0/bundle.r18.js", "Accept-Encoding", "gzip")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(raw), "window.AcmeDS") {
		t.Fatalf("body: %.40q", raw)
	}
}

func TestKitPathsCannotLeaveTheVersionFolder(t *testing.T) {
	root, get := kitRig(t)
	if err := os.Symlink(filepath.Join(root.KitsDir(), "secret.txt"), filepath.Join(root.KitsDir(), "acme-ds", "0.16.0", "link.txt")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/kit/acme-ds/0.16.0/../../secret.txt",
		"/kit/acme-ds/0.16.0/%2e%2e/%2e%2e/secret.txt",
		"/kit/acme-ds/0.16.0/..%2f..%2fsecret.txt",
		"/kit/acme-ds/0.16.0/link.txt",
		"/kit/acme-ds/0.16.0/SHA256SUMS", // an extension the library does not serve
		"/kit/acme-ds/0.16.0/",
		"/kit/acme-ds/0.16.0",
		"/kit/acme-ds",
		"/kit/",
		"/kit/acme-ds/9.9.9/bundle.css",
		"/kit/nope/0.16.0/bundle.css",
		"/kit/Acme-DS/0.16.0/bundle.css",
		"/kit/acme-ds/latest/bundle.css",
		"/kit/..%2f/0.16.0/bundle.css",
	} {
		w := get(path)
		if w.Code == 200 && strings.Contains(w.Body.String(), "do not serve") {
			t.Fatalf("%s leaked the file", path)
		}
		if w.Code != 404 && w.Code != 400 && w.Code != 301 && w.Code != 302 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestKitsAreListedForBoardsAndAgents(t *testing.T) {
	_, get := kitRig(t)
	w := get("/__lib/kits")
	var all struct {
		Kits []struct {
			ID       string `json:"id"`
			Versions []struct {
				Version string `json:"version"`
			} `json:"versions"`
		} `json:"kits"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &all) != nil || len(all.Kits) != 1 || all.Kits[0].ID != "acme-ds" ||
		len(all.Kits[0].Versions) != 2 || all.Kits[0].Versions[0].Version != "0.16.0" {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	w = get("/__lib/kits?kit=acme-ds&version=0.16.0")
	var one struct {
		Files []string `json:"files"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &one) != nil || len(one.Files) != 5 || one.Files[0] != "README.md" {
		t.Fatalf("files: %d %s", w.Code, w.Body.String())
	}
	if w := get("/__lib/kits?kit=acme-ds&version=1.2.3"); w.Code != 404 {
		t.Fatalf("a version that is not installed: %d", w.Code)
	}
}

func TestKitServingFollowsTheHostRuleAndTheDefaultFolder(t *testing.T) {
	l, _ := kitRig(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/kit/acme-ds/0.16.0/bundle.css", nil)
	r.Host = "evil.example"
	l.serveLibrary(w, r, 18470)
	if w.Code != 403 {
		t.Fatalf("a foreign host: %d", w.Code)
	}
	if got := New(Config{DataDir: "/data"}).KitsDir(); got != "/data/design-kits" {
		t.Fatalf("default folder: %s", got)
	}
}

func TestKitFilesAreReadableAcrossOriginsOnlyFromAllowedOrigins(t *testing.T) {
	l, get := kitRig(t)
	l.cfg.KitOrigins = []string{"https://design.example.com/", "not an origin", "*"}
	path := "/kit/acme-ds/0.16.0/fonts/inter.woff2"
	acao := func(origin string) (string, string) {
		var w *httptest.ResponseRecorder
		if origin == "" {
			w = get(path)
		} else {
			w = get(path, "Origin", origin)
		}
		if w.Code != 200 {
			t.Fatalf("%q: %d", origin, w.Code)
		}
		return w.Header().Get("Access-Control-Allow-Origin"), strings.Join(w.Header().Values("Vary"), ",")
	}
	for _, allowed := range []string{"http://127.0.0.1:18470", "http://localhost:18470", "http://[::1]:18470", "https://design.example.com"} {
		if got, vary := acao(allowed); got != allowed || !strings.Contains(vary, "Origin") {
			t.Errorf("allowed %s: ACAO %q, Vary %q", allowed, got, vary)
		}
	}
	for _, foreign := range []string{"https://evil.example", "http://127.0.0.1:9999", "http://localhost", "https://design.example.com.evil.example", "http://design.example.com", "null", "*"} {
		if got, vary := acao(foreign); got != "" || !strings.Contains(vary, "Origin") {
			t.Errorf("foreign %s: ACAO %q, Vary %q", foreign, got, vary)
		}
	}
	if got, vary := acao(""); got != "" || !strings.Contains(vary, "Origin") {
		t.Errorf("no origin: ACAO %q, Vary %q", got, vary)
	}
	// A 304 and a compressed answer carry the same rule.
	w := get("/kit/acme-ds/0.16.0/bundle.r18.js", "Origin", "https://evil.example", "Accept-Encoding", "gzip")
	if w.Header().Get("Access-Control-Allow-Origin") != "" || !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Accept-Encoding") {
		t.Errorf("foreign origin on a gzip answer: %v", w.Header())
	}
}

func TestNormalizeOrigin(t *testing.T) {
	for in, want := range map[string]string{"https://A.example:8443/": "https://a.example:8443", "http://x.test": "http://x.test", "https://x.test/path": "", "ftp://x.test": "", "x.test": "", "https://u@x.test": "", "": "", "*": ""} {
		if got := NormalizeOrigin(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

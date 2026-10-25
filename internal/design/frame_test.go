package design

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// The library's pages and its script may be framed only by the library itself, the platform on this machine and the
// configured platform hosts; the script knows those hosts too.
func TestLibraryAnswersNameWhoMayFrameThem(t *testing.T) {
	root := t.TempDir()
	l := New(Config{
		LibraryDir: filepath.Join(root, "lib"),
		KitOrigins: []string{"https://App.Example.com/", "not an origin", "null"},
		Texts:      Texts{Tag: "test-design", CLI: "acme"},
	})
	l.srv = &server{}
	t.Cleanup(l.StopWatching)
	const want = "frame-ancestors 'self' http://localhost:* http://127.0.0.1:* https://app.example.com"
	for _, path := range []string{"/__lib/client.js", "/__lib/version?a=nope", "/a/nope/p/Main.dc.html", "/"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18470"
		l.serveLibrary(w, r, 18470)
		if got := w.Header().Get("Content-Security-Policy"); got != want {
			t.Fatalf("%s: Content-Security-Policy %q, want %q", path, got, want)
		}
		if path == "/__lib/client.js" && !strings.Contains(w.Body.String(), `var PLATFORM_HOSTS = ["https://app.example.com"];`) {
			t.Fatalf("the script does not name the platform hosts")
		}
	}
	bare := New(Config{LibraryDir: filepath.Join(root, "lib2"), Texts: Texts{Tag: "test-design", CLI: "acme"}})
	if got := bare.frameAncestors(); got != "frame-ancestors 'self' http://localhost:* http://127.0.0.1:*" {
		t.Fatalf("without platform hosts: %q", got)
	}
	if !strings.Contains(bare.clientScript(), "var PLATFORM_HOSTS = [];") {
		t.Fatalf("the script of a library without platform hosts names some")
	}
}

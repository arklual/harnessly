package design

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A board an agent renamed or removed does not leave an open tab on «no such design» after a reload: its page leads to
// the project's first board; a missing asset or project is still a 404.
func TestAGoneBoardLeadsToTheProjectsFirstBoard(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "harnessly", "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "Главная.dc.html"), []byte("<html><body>main</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "empty", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := New(Config{LibraryDir: root, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(l.StopWatching)
	get := func(path string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18470"
		l.serveLibrary(w, r, 18470)
		return w.Code, w.Header().Get("Location")
	}
	old := "/a/harnessly/p/" + EncodeURIComponent("Рабочее место.dc.html")
	if code, to := get(old); code != 302 || to != "/a/harnessly/p/"+EncodeURIComponent("Главная.dc.html") {
		t.Fatalf("gone board: %d %q", code, to)
	}
	if code, to := get("/a/empty/p/x.dc.html"); code != 302 || to != "/a/empty/" {
		t.Fatalf("gone board of a project with none: %d %q", code, to)
	}
	if code, _ := get("/a/harnessly/p/" + EncodeURIComponent("Главная.dc.html")); code != 200 {
		t.Fatalf("present board: %d", code)
	}
	if code, _ := get("/a/harnessly/p/lib/missing.js"); code != 404 {
		t.Fatalf("missing asset: %d", code)
	}
	if code, _ := get("/a/nope/p/x.dc.html"); code != 404 {
		t.Fatalf("missing project: %d", code)
	}
}

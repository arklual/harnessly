package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// A board file is renamed only as itself, only next to itself: a link is refused, not followed; a folder reached
// through a link is refused; a file at the new name is never replaced.
func TestRenameInDirNeverFollowsALink(t *testing.T) {
	root := t.TempDir()
	dir, outside := filepath.Join(root, "project"), filepath.Join(root, "outside")
	writeFile(t, filepath.Join(dir, "A.dc.html"), "a")
	writeFile(t, filepath.Join(dir, "B.dc.html"), "b")
	writeFile(t, filepath.Join(outside, "secret.txt"), "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "Link.dc.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	if err := renameInDir(dir, "Link.dc.html", "Moved.dc.html"); err == nil {
		t.Fatal("a link was renamed")
	}
	if exists(filepath.Join(dir, "Moved.dc.html")) || readFileT(t, filepath.Join(outside, "secret.txt")) != "secret" {
		t.Fatal("the link's target was touched")
	}
	if err := renameInDir(filepath.Join(root, "linked"), "secret.txt", "x.txt"); err == nil || !exists(filepath.Join(outside, "secret.txt")) {
		t.Fatalf("a folder reached through a link: %v", err)
	}
	if err := renameInDir(dir, "A.dc.html", "B.dc.html"); !isExist(err) || readFileT(t, filepath.Join(dir, "B.dc.html")) != "b" {
		t.Fatalf("replaced: %v", err)
	}
	for _, to := range []string{"../escaped.dc.html", "sub/x.dc.html", "", ".."} {
		if err := renameInDir(dir, "A.dc.html", to); err == nil {
			t.Fatalf("%q accepted", to)
		}
	}
	if err := renameInDir(dir, "A.dc.html", "C.dc.html"); err != nil || exists(filepath.Join(dir, "A.dc.html")) || readFileT(t, filepath.Join(dir, "C.dc.html")) != "a" {
		t.Fatalf("plain rename: %v", err)
	}
}

func TestRenamePageRefusesALinkedBoard(t *testing.T) {
	l, root := migratedLibrary(t)
	project := filepath.Join(root, "files", ProjectDirname)
	writeFile(t, filepath.Join(root, "elsewhere.html"), "x")
	if err := os.Remove(filepath.Join(project, "B.dc.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere.html"), filepath.Join(project, "B.dc.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RenamePage("files", "B", nil, strPtr("Moved.dc.html")); err == nil {
		t.Fatal("a linked board was renamed")
	}
	if exists(filepath.Join(project, "Moved.dc.html")) || readFileT(t, filepath.Join(root, "elsewhere.html")) != "x" {
		t.Fatal("touched")
	}
}

func TestSetRelease(t *testing.T) {
	l, root := migratedLibrary(t)
	if err := l.SetRelease("post", "1792547779-0491"); err != nil {
		t.Fatal(err)
	}
	if m := l.ReadModel("post"); m.Meta.Release != "1792547779-0491" || m.Meta.Title != "Post" {
		t.Fatalf("meta: %+v", m.Meta)
	}
	canvas, _ := readJSON(filepath.Join(root, "post", ProjectDirname, canvasJSONName)).(*jsvalue.Object)
	if canvas.Object(DefaultCanvasKey).String("release") != "1792547779-0491" {
		t.Fatal("canvas.json not rebuilt")
	}
	if err := l.SetRelease("post", "../x"); err == nil {
		t.Fatal("a bad release")
	}
	writeFile(t, filepath.Join(root, "legacy", "project", "A.dc.html"), "a")
	if err := l.SetRelease("legacy", "x"); err == nil || exists(filepath.Join(root, "legacy", MetaDirname)) {
		t.Fatalf("no meta: %v", err)
	}
}

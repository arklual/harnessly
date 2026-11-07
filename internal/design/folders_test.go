package design

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func post(a *API, h func(*http.Request) Answer, body string) Answer {
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return h(r)
}

func TestFoldersOneLevelRenameMoveAndDelete(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"alpha", "beta"} {
		makeProject(t, root, id, id, `{"v":2,"user":"se","name":"P","person":"p.smirnov@example.com"}`)
	}
	l := accessLibrary(t, root)
	a := pagesAPI(l)
	top, err := l.SaveFolder("", "", "Работа", "", false)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := l.SaveFolder("", "", "Архив", top.ID, true)
	if err != nil || sub.Parent != top.ID {
		t.Fatalf("sub: %v %v", sub, err)
	}
	if _, err := l.SaveFolder("", "", "Глубже", sub.ID, true); err == nil {
		t.Errorf("a third level was made")
	}
	if _, err := l.SaveFolder("", "", "работа", "", false); err == nil {
		t.Errorf("a twin name was made")
	}
	if _, err := l.SaveFolder("", top.ID, "Работа 2", "", false); err != nil {
		t.Errorf("rename: %v", err)
	}
	if err := l.MoveProjects("", []string{"alpha", "beta", "nothing"}, sub.ID); err != nil {
		t.Fatal(err)
	}
	lay, _ := l.LayoutFor("")
	if lay.Placed["alpha"] != sub.ID || lay.Placed["beta"] != sub.ID || len(lay.Placed) != 2 {
		t.Fatalf("placed: %v", lay.Placed)
	}
	// deleting the top folder with "trash" sends the projects of both levels to the trash (the owner file says they are ours)
	ans := post(a, a.FolderDelete, `{"id":"`+top.ID+`","projects":"trash"}`)
	if ans.Status != 200 {
		t.Fatalf("delete: %d %v", ans.Status, ans.Body)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha still there")
	}
	lay, _ = l.LayoutFor("")
	if len(lay.Folders) != 0 || len(lay.Placed) != 0 {
		t.Errorf("layout: %v", lay)
	}
	// the trash lists them, restore brings one back, purge erases the other
	items := l.TrashFor("")
	if len(items) != 2 {
		t.Fatalf("trash: %v", items)
	}
	if err := l.RestoreProject("alpha", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha")); err != nil {
		t.Errorf("restored: %v", err)
	}
	if err := l.PurgeProject("beta", ""); err != nil {
		t.Fatal(err)
	}
	if got := l.TrashFor(""); len(got) != 0 {
		t.Errorf("trash after: %v", got)
	}
}

func TestFolderDeleteKeepLeavesProjects(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "alpha", "alpha", `{"v":2,"user":"se","name":"P","person":"p.smirnov@example.com"}`)
	l := accessLibrary(t, root)
	a := pagesAPI(l)
	f, _ := l.SaveFolder("", "", "Работа", "", false)
	_ = l.MoveProjects("", []string{"alpha"}, f.ID)
	if ans := post(a, a.FolderDelete, `{"id":"`+f.ID+`","projects":"keep"}`); ans.Status != 200 {
		t.Fatal(ans.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha")); err != nil {
		t.Errorf("project gone: %v", err)
	}
}

func TestTrashIsTheOwnersOnly(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "theirs", "theirs", `{"v":2,"user":"other","name":"X","person":"x@example.com"}`)
	l := accessLibrary(t, root)
	_ = os.MkdirAll(filepath.Join(root, ".trash"), 0o777)
	_ = os.Rename(filepath.Join(root, "theirs"), filepath.Join(root, ".trash", "theirs-1700000000000"))
	if got := l.TrashFor(""); len(got) != 0 {
		t.Errorf("someone else's project listed: %v", got)
	}
	if err := l.RestoreProject("theirs", ""); err == nil {
		t.Errorf("restored someone else's")
	}
	if err := l.PurgeProject("theirs", ""); err == nil {
		t.Errorf("purged someone else's")
	}
}

func TestExamplesFolderOnceWithTheDemoProjects(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"relay-site", "relay-deck", "mine"} {
		makeProject(t, root, id, id, `{"v":2,"user":"se","name":"P","person":"p.smirnov@example.com"}`)
	}
	l := accessLibrary(t, root)
	// A person who already laid a demo project somewhere keeps it there.
	work, err := l.SaveFolder("", "", "Работа", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.MoveProjects("", []string{"relay-deck"}, work.ID); err != nil {
		t.Fatal(err)
	}
	lay, err := l.LayoutFor("")
	if err != nil {
		t.Fatal(err)
	}
	if lay.Placed["relay-site"] != ExamplesFolderID || lay.Placed["relay-deck"] != work.ID || lay.Placed["mine"] != "" {
		t.Fatalf("placed: %v", lay.Placed)
	}
	n := 0
	for _, fo := range lay.Folders {
		if fo.ID == ExamplesFolderID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("examples folders: %d in %v", n, lay.Folders)
	}
	// Deleted, it does not come back.
	if err := l.RemoveFolder("", ExamplesFolderID); err != nil {
		t.Fatal(err)
	}
	lay, _ = l.LayoutFor("")
	for _, fo := range lay.Folders {
		if fo.ID == ExamplesFolderID {
			t.Fatalf("the examples folder came back: %v", lay.Folders)
		}
	}
	if _, ok := lay.Placed["relay-site"]; ok {
		t.Fatalf("relay-site still placed: %v", lay.Placed)
	}
}

func TestNoExamplesFolderWithoutDemoProjects(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "mine", "mine", `{"v":2,"user":"se","name":"P","person":"p.smirnov@example.com"}`)
	l := accessLibrary(t, root)
	lay, err := l.LayoutFor("")
	if err != nil || len(lay.Folders) != 0 {
		t.Fatalf("%v %v", lay.Folders, err)
	}
}

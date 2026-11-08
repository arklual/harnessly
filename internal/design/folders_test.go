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

func TestDemoFolderOnceWithTheDemoProjects(t *testing.T) {
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
	if lay.Placed["relay-site"] != DemoFolderID || lay.Placed["relay-deck"] != work.ID || lay.Placed["mine"] != "" {
		t.Fatalf("placed: %v", lay.Placed)
	}
	n := 0
	for _, fo := range lay.Folders {
		if fo.ID == DemoFolderID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("examples folders: %d in %v", n, lay.Folders)
	}
	// Deleted, it does not come back.
	if err := l.RemoveFolder("", DemoFolderID); err != nil {
		t.Fatal(err)
	}
	lay, _ = l.LayoutFor("")
	for _, fo := range lay.Folders {
		if fo.ID == DemoFolderID {
			t.Fatalf("the examples folder came back: %v", lay.Folders)
		}
	}
	if _, ok := lay.Placed["relay-site"]; ok {
		t.Fatalf("relay-site still placed: %v", lay.Placed)
	}
}

func TestNoDemoFolderWithoutDemoProjects(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "mine", "mine", `{"v":2,"user":"se","name":"P","person":"p.smirnov@example.com"}`)
	l := accessLibrary(t, root)
	lay, err := l.LayoutFor("")
	if err != nil || len(lay.Folders) != 0 {
		t.Fatalf("%v %v", lay.Folders, err)
	}
}

func demoNames(lay Layout) []string {
	var out []string
	for _, fo := range lay.Folders {
		out = append(out, fo.Name)
	}
	return out
}

func demoLibrary(t *testing.T, ids ...string) *Library {
	t.Helper()
	root := t.TempDir()
	for _, id := range ids {
		makeProject(t, root, id, id, `{"v":2,"user":"se","name":"P","person":"p.smirnov@example.com"}`)
	}
	return accessLibrary(t, root)
}

func TestDemoFolderIsCalledDemo(t *testing.T) {
	for _, c := range []struct{ locale, want string }{{"en", "Demo"}, {"ru", "Демо"}} {
		t.Setenv("RUNTIME_LOCALE", c.locale)
		l := demoLibrary(t, "relay-site", "relay-deck", "relay-video", "relay-ds", "mine")
		lay, err := l.LayoutFor("")
		if err != nil {
			t.Fatal(err)
		}
		if len(lay.Folders) != 1 || lay.Folders[0].Name != c.want || lay.Folders[0].Parent != "" {
			t.Fatalf("%s: folders %v", c.locale, lay.Folders)
		}
		for _, id := range DemoProjects {
			if lay.Placed[id] != lay.Folders[0].ID {
				t.Errorf("%s: %s placed in %q", c.locale, id, lay.Placed[id])
			}
		}
		if _, ok := lay.Placed["mine"]; ok {
			t.Errorf("%s: a project of the person went to the demo folder", c.locale)
		}
	}
}

// writeOldLayout is a layout of the first release: the folder was "Examples"/"Примеры" and the file had no "named".
func writeOldLayout(t *testing.T, l *Library, name string, placed map[string]string) {
	t.Helper()
	key, err := l.layoutKey("")
	if err != nil {
		t.Fatal(err)
	}
	f := layoutFile{Folders: []Folder{{ID: DemoFolderID, Name: name}}, Placed: placed, Examples: true}
	if err := l.writeLayout(key, f); err != nil {
		t.Fatal(err)
	}
}

func TestDemoFolderMigrationRenamesOnlyTheDefaultName(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "ru")
	// The folder of the first release, untouched: renamed once, the projects stay in it.
	l := demoLibrary(t, "relay-site", "relay-deck")
	writeOldLayout(t, l, "Примеры", map[string]string{"relay-site": DemoFolderID, "relay-deck": DemoFolderID})
	lay, err := l.LayoutFor("")
	if err != nil || len(lay.Folders) != 1 || lay.Folders[0].Name != "Демо" || lay.Placed["relay-site"] != DemoFolderID {
		t.Fatalf("%v %v %v", lay.Folders, lay.Placed, err)
	}
	// Renamed back by the person: the migration does not run twice.
	if _, err := l.SaveFolder("", DemoFolderID, "Примеры", "", false); err != nil {
		t.Fatal(err)
	}
	lay, _ = l.LayoutFor("")
	if lay.Folders[0].Name != "Примеры" {
		t.Fatalf("renamed again: %v", lay.Folders)
	}

	// Renamed by the person before the migration: theirs.
	l = demoLibrary(t, "relay-site")
	writeOldLayout(t, l, "Для обучения", map[string]string{"relay-site": DemoFolderID})
	if lay, _ = l.LayoutFor(""); lay.Folders[0].Name != "Для обучения" {
		t.Fatalf("the person's name lost: %v", lay.Folders)
	}

	// The person took the projects out to the top: they stay there, the library is not laid out again.
	l = demoLibrary(t, "relay-site", "relay-deck")
	writeOldLayout(t, l, "Примеры", map[string]string{})
	lay, _ = l.LayoutFor("")
	if len(lay.Placed) != 0 || len(lay.Folders) != 1 {
		t.Fatalf("laid out again: %v %v", lay.Folders, lay.Placed)
	}

	// A folder of that name exists already: no two folders of one name.
	l = demoLibrary(t, "relay-site")
	own, _ := l.SaveFolder("", "", "Демо", "", false)
	writeOldLayoutWith(t, l, []Folder{{ID: DemoFolderID, Name: "Примеры"}, own}, map[string]string{"relay-site": DemoFolderID})
	lay, _ = l.LayoutFor("")
	if len(lay.Folders) != 2 || lay.Folders[0].Name != "Примеры" {
		t.Fatalf("%v", lay.Folders)
	}
}

func writeOldLayoutWith(t *testing.T, l *Library, folders []Folder, placed map[string]string) {
	t.Helper()
	key, _ := l.layoutKey("")
	if err := l.writeLayout(key, layoutFile{Folders: folders, Placed: placed, Examples: true}); err != nil {
		t.Fatal(err)
	}
}

func TestPlaceDemoUsesTheFolderTheyHaveOnce(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "ru")
	l := demoLibrary(t, "relay-site", "relay-deck", "relay-video", "relay-ds")
	if _, err := l.LayoutFor(""); err != nil {
		t.Fatal(err)
	}
	// Renamed: the restored project goes to the renamed folder, no second "Демо".
	if _, err := l.SaveFolder("", DemoFolderID, "Уроки", "", false); err != nil {
		t.Fatal(err)
	}
	if err := l.MoveProjects("", []string{"relay-site"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.PlaceDemo("", []string{"relay-site", "relay-deck"}); err != nil {
		t.Fatal(err)
	}
	lay, _ := l.LayoutFor("")
	if got := demoNames(lay); len(got) != 1 || got[0] != "Уроки" || lay.Placed["relay-site"] != DemoFolderID {
		t.Fatalf("%v %v", got, lay.Placed)
	}
	// Deleted: one new "Демо" comes, and a second restore adds no second one.
	if err := l.RemoveFolder("", DemoFolderID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := l.PlaceDemo("", []string{"relay-site", "relay-ds"}); err != nil {
			t.Fatal(err)
		}
	}
	lay, _ = l.LayoutFor("")
	if got := demoNames(lay); len(got) != 1 || got[0] != "Демо" || lay.Placed["relay-site"] != lay.Folders[0].ID || lay.Placed["relay-ds"] != lay.Folders[0].ID {
		t.Fatalf("%v %v", got, lay.Placed)
	}
	// A project the person laid elsewhere stays there.
	work, _ := l.SaveFolder("", "", "Работа", "", false)
	_ = l.MoveProjects("", []string{"relay-ds"}, work.ID)
	_ = l.PlaceDemo("", []string{"relay-ds"})
	if lay, _ = l.LayoutFor(""); lay.Placed["relay-ds"] != work.ID {
		t.Fatalf("moved back: %v", lay.Placed)
	}
	// A folder of the person's own called "Демо" is the one used.
	l = demoLibrary(t, "relay-site")
	own, _ := l.SaveFolder("", "", "демо", "", false)
	if err := l.PlaceDemo("", []string{"relay-site"}); err != nil {
		t.Fatal(err)
	}
	if lay, _ = l.LayoutFor(""); len(lay.Folders) != 1 || lay.Placed["relay-site"] != own.ID {
		t.Fatalf("%v %v", lay.Folders, lay.Placed)
	}
}

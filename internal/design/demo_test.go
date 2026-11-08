package design

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestSeedDemoOnceAndRestore(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	fsys := fstest.MapFS{
		"relay-site/artifact.json":           {Data: []byte(`{"v":1,"title":"Relay","boards":[]}`)},
		"relay-site/project/Главная.dc.html": {Data: []byte("<x-dc></x-dc>")},
	}
	ids, err := l.SeedDemo(fsys, false)
	if err != nil || len(ids) != 1 {
		t.Fatalf("first seed: %v %v", ids, err)
	}
	if _, err := os.Stat(filepath.Join(l.ProjectDir("relay-site"), "support.js")); err != nil {
		t.Fatalf("no engine: %v", err)
	}
	if err := os.RemoveAll(l.ArtifactDir("relay-site")); err != nil {
		t.Fatal(err)
	}
	if ids, _ := l.SeedDemo(fsys, false); len(ids) != 0 {
		t.Fatalf("a deleted demo came back on its own: %v", ids)
	}
	if ids, _ := l.SeedDemo(fsys, true); len(ids) != 1 {
		t.Fatalf("restore: %v", ids)
	}
	if err := os.WriteFile(filepath.Join(l.ProjectDir("relay-site"), "Главная.dc.html"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ids, _ := l.SeedDemo(fsys, true); len(ids) != 0 {
		t.Fatalf("restore overwrote an edited project: %v", ids)
	}
}

// The demo lies in one «Демо» folder when the library is seeded, and again when the person asks for it back, even when
// they deleted the folder as well: one folder either way.
func TestSeededDemoLiesInTheDemoFolderAndComesBackThere(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "ru")
	root := t.TempDir()
	l := accessLibrary(t, root)
	fsys := fstest.MapFS{
		"relay-site/artifact.json":           {Data: []byte(`{"v":1,"title":"Relay","boards":[]}`)},
		"relay-site/project/Главная.dc.html": {Data: []byte("<x-dc></x-dc>")},
		"relay-deck/artifact.json":           {Data: []byte(`{"v":1,"title":"Deck","boards":[]}`)},
		"relay-deck/project/Главная.dc.html": {Data: []byte("<x-dc></x-dc>")},
	}
	if ids, err := l.SeedDemo(fsys, false); err != nil || len(ids) != 2 {
		t.Fatalf("seed: %v %v", ids, err)
	}
	lay, err := l.LayoutFor("")
	if err != nil || len(lay.Folders) != 1 || lay.Folders[0].Name != "Демо" {
		t.Fatalf("%v %v", lay.Folders, err)
	}
	folder := lay.Folders[0].ID
	if lay.Placed["relay-site"] != folder || lay.Placed["relay-deck"] != folder {
		t.Fatalf("placed: %v", lay.Placed)
	}

	// One project deleted, the folder kept: it comes back into it.
	if err := os.RemoveAll(l.ArtifactDir("relay-site")); err != nil {
		t.Fatal(err)
	}
	ids, err := l.SeedDemo(fsys, true)
	if err != nil || len(ids) != 1 {
		t.Fatalf("restore: %v %v", ids, err)
	}
	if err := l.PlaceDemo("", ids); err != nil {
		t.Fatal(err)
	}
	lay, _ = l.LayoutFor("")
	if len(lay.Folders) != 1 || lay.Placed["relay-site"] != folder {
		t.Fatalf("after restore: %v %v", lay.Folders, lay.Placed)
	}

	// The folder deleted with the projects in it (they go to the top): the next restore makes it again, once.
	for _, id := range []string{"relay-site", "relay-deck"} {
		if err := os.RemoveAll(l.ArtifactDir(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.RemoveFolder("", folder); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ids, err = l.SeedDemo(fsys, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.PlaceDemo("", ids); err != nil {
			t.Fatal(err)
		}
	}
	lay, _ = l.LayoutFor("")
	if len(lay.Folders) != 1 || lay.Folders[0].Name != "Демо" || lay.Placed["relay-site"] != lay.Folders[0].ID || lay.Placed["relay-deck"] != lay.Folders[0].ID {
		t.Fatalf("after the folder was deleted: %v %v", lay.Folders, lay.Placed)
	}
}

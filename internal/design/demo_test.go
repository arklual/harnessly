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

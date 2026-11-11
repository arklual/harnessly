package designkit

import (
	"os"
	"path/filepath"
	"testing"
)

func writeVersion(t *testing.T, dir, id, v string, files map[string]string) string {
	t.Helper()
	root := filepath.Join(dir, id, v)
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var sealFiles = map[string]string{"design-system.json": `{"v":3}`, "components/bundle.js": "x", "tokens.json": "{}"}

// A version that is arriving file by file (no seal, marker set) is not installed; the seal makes it whole; a missing or
// changed file un-installs it again.
func TestUnsealedVersionIsNotInstalledOnceSealsAreRequired(t *testing.T) {
	dir := t.TempDir()
	root := writeVersion(t, dir, "acme", "1.0.0", sealFiles)
	if Resolve(dir, "acme", "1.0.0", []string{"tokens.json"}) == "" {
		t.Fatal("without the mark a version without a seal is installed as before")
	}
	if err := os.WriteFile(filepath.Join(dir, SealRequired), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if Resolve(dir, "acme", "1.0.0", nil) != "" || len(List(dir)) != 0 {
		t.Fatal("an unsealed version is visible as installed")
	}
	if err := WriteSeal("acme", "1.0.0", root); err != nil {
		t.Fatal(err)
	}
	sealCache = map[string]*sealInfo{}
	if Resolve(dir, "acme", "1.0.0", []string{"tokens.json"}) == "" || len(List(dir)) != 1 || len(Files(dir, "acme", "1.0.0")) != 3 {
		t.Fatalf("a sealed version is not installed: %v", Files(dir, "acme", "1.0.0"))
	}
	if got := List(dir)[0].Versions[0]; got.Files != 3 {
		t.Errorf("a version without a record of its own takes its size from the seal: %+v", got)
	}
	// a file the seal does not list is not part of the version
	if err := os.WriteFile(filepath.Join(root, "extra.js"), []byte("evil"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Resolve(dir, "acme", "1.0.0", []string{"extra.js"}) != "" || len(Files(dir, "acme", "1.0.0")) != 3 {
		t.Error("an unlisted file is served or listed")
	}
	// a listed file that changed or went
	if err := os.WriteFile(filepath.Join(root, "tokens.json"), []byte("{ }"), 0o644); err != nil {
		t.Fatal(err)
	}
	sealCache = map[string]*sealInfo{}
	if Resolve(dir, "acme", "1.0.0", nil) != "" {
		t.Error("a version whose file changed is still installed")
	}
}

// Versions installed before seals are sealed in one pass and stay visible; a version arriving after that is not.
func TestSealLegacyKeepsOldVersionsVisible(t *testing.T) {
	dir := t.TempDir()
	writeVersion(t, dir, "old", "0.1.0", sealFiles)
	n, err := SealLegacy(dir)
	if err != nil || n != 1 {
		t.Fatalf("sealed %d: %v", n, err)
	}
	if Resolve(dir, "old", "0.1.0", []string{"design-system.json"}) == "" || len(List(dir)) != 1 {
		t.Fatal("the old version vanished")
	}
	writeVersion(t, dir, "new", "1.0.0", sealFiles) // arriving
	if len(List(dir)) != 1 {
		t.Fatal("a version without a seal appeared")
	}
	// A later start finds the mark and leaves the folder alone: the arriving version is not sealed as a legacy one.
	if n, err := SealLegacy(dir); n != 0 || err != nil {
		t.Fatalf("a second pass sealed %d: %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new", "1.0.0", SealFile)); err == nil {
		t.Fatal("the arriving version got a seal")
	}
}

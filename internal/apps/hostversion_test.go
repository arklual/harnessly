package apps

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNewerManifestAsksForTheVersion(t *testing.T) {
	raw := []byte(`{"id":"acme.x","harnessly":">=9.4","brandNewField":1}`)
	m, ps := ParseManifest(raw)
	if m != nil || len(ps) != 1 || !strings.Contains(ps[0].Message, "needs harnessly 9.4.0") {
		t.Fatalf("want the version, got %v", ps)
	}
	if _, ps := ParseManifest([]byte(`{"id":"acme.x","brandNewField":1}`)); len(ps) != 1 || !strings.Contains(ps[0].Message, "unknown field") {
		t.Fatalf("no range: the unknown field stays, got %v", ps)
	}
	dir := scaffold(t, "acme.demo", "chat")
	mp := filepath.Join(dir, ManifestPath)
	b, _ := os.ReadFile(mp)
	b = []byte(strings.Replace(string(b), "{", `{"brandNewField": 1, `, 1))
	b = []byte(regexp.MustCompile(`"harnessly": *"[^"]*"`).ReplaceAllString(string(b), `"harnessly": ">=9.4"`))
	_ = os.WriteFile(mp, b, 0o644)
	_, _, err := (&Registry{Root: filepath.Join(t.TempDir(), "apps")}).Install(dir, InstallOptions{Dev: true, HostVersion: "0.17.0"})
	if !errors.Is(err, ErrHostVersion) {
		t.Fatalf("install: want ErrHostVersion, got %v", err)
	}
}

func TestPackageIsCached(t *testing.T) {
	dir := scaffold(t, "acme.demo", "chat")
	before := loads.Load()
	for range 3 {
		if p, _ := CachedPackage(dir); p == nil {
			t.Fatal("no package")
		}
	}
	if n := loads.Load() - before; n != 1 {
		t.Fatalf("the tree was read %d times", n)
	}
	mp := filepath.Join(dir, ManifestPath)
	b, _ := os.ReadFile(mp)
	_ = os.WriteFile(mp, append(b, '\n'), 0o644)
	_ = os.Chtimes(mp, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	_, _ = CachedPackage(dir)
	if n := loads.Load() - before; n != 2 {
		t.Fatalf("a changed manifest must reload: %d", n)
	}
}

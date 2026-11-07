package apps

import (
	"os"
	"path/filepath"
	"testing"
)

// The corpus of old apps (web/app/next/features/app-views/fixtures/compat/README.md): every package installs
// on today's host as it is, and an install made before the consent was asked keeps its rights through an update that
// asks for no more. A red test means an update of the host broke an app that worked: fix the host, never the app.
func corpusDirs(t *testing.T) []string {
	t.Helper()
	dirs := []string{filepath.Join("..", "..", "web", "app", "next", "features", "app-views", "fixtures", "compat", "ptcheck")}
	entries, err := os.ReadDir(filepath.Join("..", "..", "docs", "apps"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		dir := filepath.Join("..", "..", "docs", "apps", e.Name())
		if _, err := os.Stat(filepath.Join(dir, ManifestPath)); e.IsDir() && err == nil {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) < 3 {
		t.Fatalf("the corpus is too small: %v", dirs)
	}
	return dirs
}

func TestCorpusInstallsAndKeepsItsRights(t *testing.T) {
	for _, dir := range corpusDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			r := &Registry{Root: t.TempDir()}
			// As it was installed before consent was asked: by the host, no grant.
			a, ps, err := r.Install(dir, InstallOptions{HostVersion: "0.17.0"})
			if err != nil {
				t.Fatalf("install: %v %v", err, ps.Errors())
			}
			list, _ := r.read()
			list[0].Wants = nil
			if err := r.write(list); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Get(a.ID); err != nil {
				t.Fatalf("an old install is on: %v", err)
			}
			// The same package again through the CLI: nothing new to agree to, it stays on.
			again, _, err := r.Install(dir, InstallOptions{HostVersion: "0.17.0", Consent: ConsentAsk})
			if err != nil {
				t.Fatal(err)
			}
			if again.WaitsConsent() {
				t.Fatalf("an old install asks for consent it already had: %+v", again)
			}
			if _, err := r.Get(a.ID); err != nil {
				t.Fatalf("still on: %v", err)
			}
		})
	}
}

package design

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A library folder reached through a link (a workspace's design-artifacts pointing at a shared folder) is watched
// like a real one: a project that appears after the first listing is listed, not hidden behind the cached list.
func TestListingSeesProjectsAddedBehindALinkedRoot(t *testing.T) {
	tmp := t.TempDir()
	shared := filepath.Join(tmp, "shared")
	link := filepath.Join(tmp, "workspace", "design-artifacts")
	mustMkdir(t, filepath.Join(shared, "first", "project"))
	mustMkdir(t, filepath.Dir(link))
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}
	l := New(Config{LibraryDir: link, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(l.StopWatching)

	if got := ids(l.ListArtifacts()); len(got) != 1 || got[0] != "first" {
		t.Fatalf("first listing = %v, want [first]", got)
	}
	if !l.watching() {
		t.Fatal("the linked root is not watched")
	}
	mustMkdir(t, filepath.Join(shared, "second", "project"))

	deadline := time.Now().Add(5 * time.Second)
	for {
		got := ids(l.ListArtifacts())
		if len(got) == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("listing after a new project = %v, want both projects", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func ids(list []*Artifact) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.ID)
	}
	return out
}

package design

import (
	"os"
	"path/filepath"
	"testing"
)

// A kit without themes mounts whole: the mount folder is the kit's root, and it is served.
func TestMountInsideAllowsTheRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "themes", "dark"), 0o755); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	if got := mountInside(root, root); got != want {
		t.Fatalf("root as the mount = %q, want %q", got, want)
	}
	if got := mountInside(root, filepath.Join(root, "themes", "dark")); got == "" {
		t.Fatal("a theme folder inside the kit must be served")
	}
	if got := mountInside(root, filepath.Dir(root)); got != "" {
		t.Fatalf("a folder outside the kit must not be served, got %q", got)
	}
}

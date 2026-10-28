package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectFilePathStaysInTheProject(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "plan.md"), []byte("# x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("# y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "leak.md")); err != nil {
		t.Fatal(err)
	}
	if got, err := ProjectFilePath(root, "docs/plan.md"); err != nil || got != filepath.Join(root, "docs", "plan.md") {
		t.Errorf("inside: %q %v", got, err)
	}
	for _, rel := range []string{"../x.md", "/etc/passwd", "leak.md", "docs/missing.md"} {
		if got, err := ProjectFilePath(root, rel); err == nil {
			t.Errorf("%s resolved to %q", rel, got)
		}
	}
}

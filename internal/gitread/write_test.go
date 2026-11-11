package gitread

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileOnlyInsideAndOnlyOverWhatWasRead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "user.name", "T")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "first")
	root := resolve(dir)

	if status, _ := WriteFile(root, "a.txt", "two\n", ContentHash("stale\n")); status != 409 {
		t.Errorf("over a newer file: %d", status)
	}
	if status, _ := WriteFile(root, "a.txt", "two\n", ContentHash("one\n")); status != 200 {
		t.Fatalf("write: %d", status)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "two\n" {
		t.Errorf("file %q", b)
	}
	if status, _ := WriteFile(root, "new/b.txt", "x", ContentHash("")); status != 400 {
		t.Errorf("into a folder that is not there: %d", status)
	}
	_ = os.MkdirAll(filepath.Join(dir, "sub", ".git", "hooks"), 0o755)
	for _, bad := range []string{"../out.txt", "/etc/passwd", ".git/config", "", "./.git/hooks/pre-commit", ".GIT/config",
		"sub/.git/hooks/pre-commit", "a/../.git/config", "x\\..\\y"} {
		if status, _ := WriteFile(root, bad, "x", ContentHash("")); status != 400 {
			t.Errorf("%q: %d", bad, status)
		}
	}
	outside := t.TempDir()
	_ = os.Symlink(outside, filepath.Join(dir, "link"))
	if status, _ := WriteFile(root, "link/x.txt", "x", ContentHash("")); status != 400 {
		t.Errorf("through a link out: %d", status)
	}
	if status, _ := WriteFile(filepath.Join(root, "sub"), "a.txt", "x", ContentHash("")); status != 404 {
		t.Errorf("not a repository's top: %d", status)
	}

	if status, v := CommitFiles(root, "edit a", []string{"a.txt"}); status != 200 {
		t.Fatalf("commit: %d %v", status, v)
	}
	if msg := gitIn(t, dir, "log", "-1", "--format=%s"); !strings.Contains(msg, "edit a") {
		t.Errorf("log %q", msg)
	}
	if status, _ := CommitFiles(root, "again", []string{"a.txt"}); status != 409 {
		t.Errorf("nothing to commit: %d", status)
	}
	if status, _ := CommitFiles(root, " ", []string{"a.txt"}); status != 400 {
		t.Errorf("no message: %d", status)
	}
}

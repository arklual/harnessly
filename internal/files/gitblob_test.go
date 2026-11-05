package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitBlobReadsTheCommittedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "src", "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "first")
	if err := os.WriteFile(file, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	side, inRepo, tracked := GitBlob(file, "HEAD")
	if !inRepo || !tracked || side.Content != "one\n" {
		t.Fatalf("committed side = %+v inRepo=%v tracked=%v", side, inRepo, tracked)
	}
	fresh := filepath.Join(repo, "src", "new.txt")
	_ = os.WriteFile(fresh, []byte("x"), 0o644)
	if _, inRepo, tracked := GitBlob(fresh, "HEAD"); !inRepo || tracked {
		t.Fatalf("a new file: inRepo=%v tracked=%v", inRepo, tracked)
	}
	alone := filepath.Join(t.TempDir(), "b.txt")
	_ = os.WriteFile(alone, []byte("x"), 0o644)
	if _, inRepo, _ := GitBlob(alone, "HEAD"); inRepo {
		t.Fatal("a file outside any repository is not in one")
	}
}

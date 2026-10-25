package convo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestAWorktreeIsMadeOffHeadAndKeptOutOfStatus(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "sub", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "first")

	got, err := makeWorktree(filepath.Join(repo, "sub"), "p1-opus")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repo, ".worktrees", "p1-opus", "sub")
	if got != want {
		t.Fatalf("folder %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(got, "a.txt")); err != nil {
		t.Fatalf("the tree is there: %v", err)
	}
	if b := gitIn(t, got, "rev-parse", "--abbrev-ref", "HEAD"); b != "agent/p1-opus" {
		t.Fatalf("branch %q", b)
	}
	if s := gitIn(t, repo, "status", "--porcelain"); s != "" {
		t.Fatalf("worktrees show in status: %q", s)
	}
	// A second one, and the exclude is not written twice.
	if _, err := makeWorktree(repo, "p1-sonnet"); err != nil {
		t.Fatal(err)
	}
	ex, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if n := strings.Count(string(ex), "/.worktrees/"); n != 1 {
		t.Fatalf("exclude lines: %d", n)
	}
}

func TestAFolderThatIsNoRepositoryIsUsedAsItIs(t *testing.T) {
	dir := t.TempDir()
	if got, err := makeWorktree(dir, "x"); err != nil || got != dir {
		t.Fatalf("%q %v", got, err)
	}
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q") // no commit yet
	if got, err := makeWorktree(repo, "x"); err != nil || got != repo {
		t.Fatalf("a repository with no commit: %q %v", got, err)
	}
}

func TestWorktreeNamesAreSlugs(t *testing.T) {
	if got := worktreeName("Ab12", "claude-opus-5-5[1m]"); got != "ab12-claude-opus-5-5-1m" {
		t.Fatal(got)
	}
	if got := worktreeName("", "***"); got != "chat" {
		t.Fatal(got)
	}
}

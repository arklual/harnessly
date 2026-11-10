package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Git's marks (HRN-679): a changed file, a new one, a folder by what is under it; nothing outside a repository.
func TestGitMarksOfAFolder(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	put := func(rel, body string) {
		p := filepath.Join(repo, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	put("app/keep.txt", "1")
	put("app/edit.txt", "1")
	put("app/lib/gone.txt", "1")
	put("app/lib/stay.txt", "1")
	put("top.txt", "1")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	put("app/edit.txt", "2")
	put("app/fresh.txt", "1")
	put("app/newdir/a.txt", "1")
	_ = os.Remove(filepath.Join(repo, "app", "lib", "gone.txt"))
	put("top.txt", "2")

	got := GitMarks(filepath.Join(repo, "app"))
	want := map[string]string{"edit.txt": "changed", "fresh.txt": "new", "newdir": "new", "lib": "changed"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("marks of app = %v, want %v", got, want)
	}
	if got := GitMarks(repo); !reflect.DeepEqual(got, map[string]string{"app": "changed", "top.txt": "changed"}) {
		t.Errorf("marks of the root = %v", got)
	}
	if got := GitMarks(t.TempDir()); len(got) != 0 {
		t.Errorf("outside a repository: %v", got)
	}
}

func TestGitMarksRefuseRepositoryFilters(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if GitMarks(dir)["a.txt"] != GitMarkNew {
		t.Fatalf("plain repository: %v", GitMarks(dir))
	}
	run("config", "filter.evil.clean", "touch "+filepath.Join(dir, "ran"))
	if m := GitMarks(dir); m != nil {
		t.Errorf("a repository with its own filter is read: %v", m)
	}
	run("config", "--unset", "filter.evil.clean")
	run("config", "include.path", "/dev/null")
	if m := GitMarks(dir); m != nil {
		t.Errorf("a repository that includes a config is read: %v", m)
	}
	if !trustedScopes([]byte("system\tfilter.lfs.clean\nglobal\tfilter.lfs.process\ncommand\tcore.fsmonitor\n")) {
		t.Error("the person's own filters are refused")
	}
}

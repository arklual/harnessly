package gitread

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func sidesOf(t *testing.T, kv ...string) (int, map[string]any) {
	t.Helper()
	q := url.Values{}
	for i := 0; i < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	status, v := FileSides(q)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return status, out
}

func TestFileSidesReadsCommitsAndUncommittedWork(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "first")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)
	gitIn(t, dir, "commit", "-q", "-am", "second")
	second := gitIn(t, dir, "rev-parse", "HEAD")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("fresh\n"), 0o644)

	// A commit: its parent against the commit.
	status, v := sidesOf(t, "root", dir, "sha", second, "path", "a.txt")
	if status != 200 || v["oldContent"] != "one\n" || v["newContent"] != "one\ntwo\n" {
		t.Errorf("commit: %d %v", status, v)
	}
	// Uncommitted work: HEAD against the working tree.
	status, v = sidesOf(t, "root", dir, "path", "a.txt")
	if status != 200 || v["oldContent"] != "one\ntwo\n" || v["newContent"] != "one\ntwo\nthree\n" {
		t.Errorf("worktree: %d %v", status, v)
	}
	// An added file has no old side, a deleted one no new side.
	status, v = sidesOf(t, "root", dir, "path", "new.txt", "added", "1")
	if status != 200 || v["oldContent"] != "" || v["newContent"] != "fresh\n" {
		t.Errorf("added: %d %v", status, v)
	}
	status, v = sidesOf(t, "root", dir, "sha", second, "path", "", "oldPath", "a.txt")
	if status != 200 || v["oldContent"] != "one\n" || v["newContent"] != "" {
		t.Errorf("deleted: %d %v", status, v)
	}
	// Paths out of the repository, a folder that is not a repository's top, a bad sha.
	for _, kv := range [][]string{
		{"root", dir, "path", "../x"},
		{"root", dir, "path", "/etc/passwd"},
		{"root", dir, "path", ""},
		{"root", "relative", "path", "a.txt"},
		{"root", dir, "sha", "zz", "path", "a.txt"},
	} {
		if status, _ := sidesOf(t, kv...); status != 400 {
			t.Errorf("%v: %d", kv, status)
		}
	}
	if status, _ := sidesOf(t, "root", filepath.Join(dir, ".git"), "path", "a.txt"); status != 404 {
		t.Errorf("not the top folder: %d", status)
	}
}

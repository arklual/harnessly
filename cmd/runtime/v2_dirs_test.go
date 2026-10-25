package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func gitDir(t *testing.T, dir, head string) {
	t.Helper()
	mkdir(t, dir, ".git")
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(head+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The folders offered: git ones first, then by name; hidden and build folders are not offered; a git folder inside a
// folder of projects is, a plain folder inside one is not.
func TestTheFoldersOfferedForAProject(t *testing.T) {
	root := t.TempDir()
	gitDir(t, mkdir(t, root, "zeta"), "ref: refs/heads/feature/x")
	gitDir(t, mkdir(t, root, "alpha"), "0123456789abcdef0123456789abcdef01234567")
	if err := os.WriteFile(filepath.Join(mkdir(t, root, "notes"), "a.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mkdir(t, root, "empty")
	mkdir(t, root, ".hidden")
	mkdir(t, root, "node_modules")
	gitDir(t, mkdir(t, root, "clients", "acme"), "ref: refs/heads/main")
	mkdir(t, root, "clients", "plain")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A linked worktree: .git is a file naming the real git folder.
	real := mkdir(t, root, "main-repo", ".git", "worktrees", "w")
	if err := os.WriteFile(filepath.Join(real, "HEAD"), []byte("ref: refs/heads/wt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wt := mkdir(t, root, "wt")
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+real+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := v2Dirs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name, branch string
		git, empty   bool
	}
	var rows []row
	for _, d := range got {
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.Name, err)
		}
		b := ""
		if d.Branch != nil {
			b = *d.Branch
		}
		rows = append(rows, row{d.Name, b, d.Git, d.Empty})
	}
	want := []row{
		{"acme", "main", true, false}, {"alpha", "", true, false}, {"main-repo", "", true, false}, {"wt", "wt", true, false},
		{"zeta", "feature/x", true, false},
		{"clients", "", false, false}, {"empty", "", false, true}, {"notes", "", false, false},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows: %+v", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d: %+v, want %+v\nall: %+v", i, rows[i], want[i], rows)
		}
	}
}

func TestNoFoldersWithoutADefaultFolder(t *testing.T) {
	got, err := v2Dirs(context.Background(), "")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

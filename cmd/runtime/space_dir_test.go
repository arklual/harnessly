package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpaceChatDirIsOwnAndEmpty(t *testing.T) {
	home := t.TempDir()
	// The person's last chat worked in a project with its own CLAUDE.md: the spaces' folder is not under it.
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte("another project"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := spaceChatDir(home)
	if dir != filepath.Join(home, spaceDirName) {
		t.Fatalf("dir %q", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("not an empty folder: %v %v", entries, err)
	}
	if again := spaceChatDir(home); again != dir {
		t.Fatalf("second call %q", again)
	}
	if spaceChatDir("") != "" {
		t.Fatal("no home gives no folder")
	}
}

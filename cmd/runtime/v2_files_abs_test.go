package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Abs names the file a document is bound to: the same place File opens, and nothing outside the folder.
func TestV2FilesAbs(t *testing.T) {
	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(cwd, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "docs", "plan.md"), []byte("# a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "x.md"), []byte("# b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "x.md"), filepath.Join(cwd, "leak.md")); err != nil {
		t.Fatal(err)
	}
	f := &v2Files{record: func(id string) (wire.Conversation, bool) {
		if id == "tab-ok" {
			return wire.Conversation{ID: id, Cwd: cwd}, true
		}
		return wire.Conversation{}, false
	}}
	ctx := context.Background()
	if got, err := f.Abs(ctx, "tab-ok", "docs/plan.md"); err != nil || got != filepath.Join(cwd, "docs", "plan.md") {
		t.Errorf("inside: %q %v", got, err)
	}
	for _, c := range [][2]string{{"tab-ok", "leak.md"}, {"tab-ok", "../x.md"}, {"tab-ok", "docs/none.md"}, {"nope", "docs/plan.md"}} {
		if got, err := f.Abs(ctx, c[0], c[1]); err == nil {
			t.Errorf("%v resolved to %q", c, got)
		}
	}
}

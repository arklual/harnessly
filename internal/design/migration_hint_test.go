package design

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyPageErrorNamesScopedMigration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "library with ' quote")
	l := New(Config{LibraryDir: root, Texts: Texts{CLI: "harnessly"}})
	want := "harnessly design migrate --library '" + strings.ReplaceAll(root, "'", "'\"'\"'") + "' shop"
	err := l.noMetaError("shop")
	if !strings.Contains(err.Error(), "`"+want+"`") {
		t.Fatalf("recovery command must work without inherited runtime env and target only shop; want %q in %q", want, err)
	}
}

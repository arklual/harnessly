package agenteval

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestShopFixtureCopiesTheProductBoardEngine(t *testing.T) {
	dst := t.TempDir()
	if err := copyTree("../../evals/agent/fixtures/shop", dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "project", "support.js"))
	if err != nil {
		t.Fatalf("catalogue references a missing board engine: %v", err)
	}
	want, err := os.ReadFile("../design/ownengine/support.js")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("fixture must use the product engine, not a stub or stale copy")
	}
	info, err := os.Lstat(filepath.Join(dst, "project", "support.js"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("copied engine must be a regular file: %v", err)
	}
}

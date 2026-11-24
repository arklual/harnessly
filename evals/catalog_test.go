package evals

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The packaged CLI must retain fixture data (including hidden files and the real
// board engine) and every executable launcher when run away from its checkout.
func TestExtractMatchesCheckout(t *testing.T) {
	dst := t.TempDir()
	if err := Extract(dst); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir("agent", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, err := filepath.Rel("agent", path)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		got, err := os.ReadFile(out)
		if err != nil {
			return err
		}
		if !bytes.Equal(src, got) {
			t.Errorf("%s: packaged content differs", rel)
		}
		original, err := os.Stat(path)
		if err != nil {
			return err
		}
		installed, err := os.Stat(out)
		if err != nil {
			return err
		}
		if (original.Mode()&0111 != 0) != (installed.Mode()&0111 != 0) {
			t.Errorf("%s: executable mode differs", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

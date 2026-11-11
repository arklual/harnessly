package datamove

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// readSame gives the whole file when it fits, one byte past the limit when it does not, and refuses a file that is
// not the one that was looked at.
func TestReadSameReadsTheFileItSawUpToTheLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	body := bytes.Repeat([]byte("0123456789"), 100)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	seen, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readSame(path, seen, 1<<20); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("whole file: %d bytes, %v", len(got), err)
	}
	if got, err := readSame(path, seen, int64(len(body))); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("a file of exactly the limit: %d bytes, %v", len(got), err)
	}
	if got, err := readSame(path, seen, 100); err != nil || len(got) != 101 || !bytes.Equal(got, body[:101]) {
		t.Fatalf("over the limit: %d bytes, %v; want 101", len(got), err)
	}
	other := filepath.Join(dir, "g")
	if err := os.WriteFile(other, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSame(other, seen, 1<<20); err == nil {
		t.Fatal("another file was read as the one seen")
	}
}

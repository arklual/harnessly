package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A new file, a rename and the trash (HRN-679): names that are not one entry, or that look like credentials, are
// refused; a taken name is refused; the trash keeps the file and the note to put it back.
func TestEntryOps(t *testing.T) {
	dir := t.TempDir()
	p, err := NewEmptyFile(dir, "a.txt")
	if err != nil || p != filepath.Join(dir, "a.txt") {
		t.Fatalf("new file: %q %v", p, err)
	}
	if _, err := NewEmptyFile(dir, "a.txt"); err != ErrEntryExists {
		t.Errorf("a taken name: %v", err)
	}
	for _, bad := range []string{"", ".", "..", "x/y", ".env", "id_rsa", "k.pem", strings.Repeat("a", 300)} {
		if _, err := NewEmptyFile(dir, bad); err == nil {
			t.Errorf("%q was made", bad)
		}
		if _, err := RenameEntry(p, bad); err == nil {
			t.Errorf("renamed to %q", bad)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RenameEntry(p, "b.txt"); err != ErrEntryExists {
		t.Errorf("rename over another: %v", err)
	}
	q, err := RenameEntry(p, "c.txt")
	if err != nil || q != filepath.Join(dir, "c.txt") {
		t.Fatalf("rename: %q %v", q, err)
	}
	trash := filepath.Join(dir, "Trash")
	when := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := TrashEntry(filepath.Join(dir, "gone.txt"), trash, when); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"files/gone.txt", "files/gone.2.txt", "info/gone.txt.trashinfo", "info/gone.2.txt.trashinfo"} {
		if _, err := os.Stat(filepath.Join(trash, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	note, _ := os.ReadFile(filepath.Join(trash, "info", "gone.txt.trashinfo"))
	if !strings.Contains(string(note), "Path="+dir+"/gone.txt\n") || !strings.Contains(string(note), "DeletionDate=2026-11-04T12:00:00") {
		t.Errorf("note = %s", note)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("still there: %v", err)
	}
}

func TestHoldsSecret(t *testing.T) {
	a := NewAccess([]string{"/home/me"}, []string{".config/acme/keys"})
	for p, want := range map[string]bool{
		"/home/me/.config":           true,
		"/home/me/.CONFIG/acme":      true,
		"/home/me/.config/acme/keys": false, // a secret itself: Secret refuses it
		"/home/me/config":            false,
		"/home/me/x.config":          false,
	} {
		if got := a.HoldsSecret(p); got != want {
			t.Errorf("HoldsSecret(%s) = %v", p, got)
		}
	}
}

func TestRenameKeepsWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b"), []byte("b"), 0o644)
	if err := renameNoReplace(filepath.Join(dir, "a"), filepath.Join(dir, "b")); !errors.Is(err, ErrEntryExists) {
		t.Fatalf("over a file: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b")); string(b) != "b" {
		t.Error("the file there was replaced")
	}
}

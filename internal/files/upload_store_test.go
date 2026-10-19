package files

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Where the Go uploads part from the previous runtime: links in the uploads folder are never followed, and a type
// name every JavaScript object carries is no image type.

func TestUploadsDoNotFollowLinks(t *testing.T) {
	root := t.TempDir()
	dir, outside := filepath.Join(root, "uploads"), filepath.Join(root, "outside")
	for _, d := range []string{filepath.Join(dir, "ws"), filepath.Join(outside, "tab")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Symlink(outside, filepath.Join(dir, "linked"))
	_ = os.Symlink(filepath.Join(outside, "tab"), filepath.Join(dir, "ws", "tab"))
	u := &Uploads{Dir: dir}
	for _, c := range [][2]string{{"linked", "tab"}, {"ws", "tab"}} {
		if p, err := u.Save([]byte("x"), c[0], c[1], "f.txt"); err == nil {
			t.Fatalf("saved through a link: %s", p)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(outside, "tab")); len(entries) != 0 {
		t.Fatalf("written outside: %v", entries)
	}
	if p, err := u.Save([]byte("x"), "ws", "real", "f.txt"); err != nil || p != filepath.Join(dir, "ws", "real", "f.txt") {
		t.Fatalf("save: %s %v", p, err)
	}
	if _, err := u.Save([]byte("y"), "ws", "real", "f.txt"); err == nil {
		t.Fatal("an existing upload was overwritten")
	}

	old := time.Now().Add(-48 * time.Hour)
	victim := filepath.Join(outside, "tab", "keep.png")
	_ = os.WriteFile(victim, []byte("keep"), 0o644)
	_ = os.Chtimes(victim, old, old)
	_ = os.MkdirAll(filepath.Join(dir, "ws2", "tab"), 0o755)
	_ = os.Symlink(victim, filepath.Join(dir, "ws2", "tab", "link.png"))
	res := u.Clean(true)
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("the clean-up removed a file outside the uploads folder")
	}
	if _, err := os.Lstat(filepath.Join(dir, "ws2", "tab", "link.png")); err != nil {
		t.Fatal("the clean-up removed a link")
	}
	if res.Deleted != 1 || res.FreedBytes != 1 {
		t.Fatalf("clean-up %+v", res)
	}
}

func TestImageTypes(t *testing.T) {
	for _, m := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		if !ImageType(m) {
			t.Errorf("%s refused", m)
		}
	}
	for _, m := range []string{"constructor", "toString", "__proto__", "hasOwnProperty", "image/PNG", "image/svg+xml", ""} {
		if ImageType(m) {
			t.Errorf("%s taken", m)
		}
	}
}

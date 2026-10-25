package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The folder of a conversation: listed folders first, files read as text, and whatever leaves the folder, is not text or
// is too large refused with the wire's code.
func TestV2Files(t *testing.T) {
	cwd, outside := t.TempDir(), t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(cwd, "src", "ui"), 0o755))
	must(os.WriteFile(filepath.Join(cwd, "README.md"), []byte("# hi\n"), 0o644))
	must(os.WriteFile(filepath.Join(cwd, "src", "main.go"), []byte("package main\n"), 0o644))
	must(os.WriteFile(filepath.Join(cwd, "blob.bin"), []byte{0, 1, 2}, 0o644))
	must(os.WriteFile(filepath.Join(cwd, "big.txt"), bytes.Repeat([]byte("a"), 1_000_001), 0o644))
	must(os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o644))
	must(os.Symlink(outside, filepath.Join(cwd, "escape")))
	must(os.Symlink(filepath.Join(outside, "secret"), filepath.Join(cwd, "leak")))
	f := &v2Files{record: func(id string) (wire.Conversation, bool) {
		switch id {
		case "tab-ok":
			return wire.Conversation{ID: id, Cwd: cwd}, true
		case "tab-nocwd":
			return wire.Conversation{ID: id}, true
		}
		return wire.Conversation{}, false
	}}
	ctx := context.Background()

	root, err := f.Dir(ctx, "tab-ok", "")
	if err != nil || root.Path != "" || root.Truncated || root.Validate() != nil {
		t.Fatalf("%+v %v", root, err)
	}
	var names []string
	for _, e := range root.Entries {
		names = append(names, string(e.Kind)+":"+e.Path)
	}
	// Folders first, then by name (without regard to case); the links are not listed.
	if want := []string{"directory:src", "file:big.txt", "file:blob.bin", "file:README.md"}; !slices.Equal(names, want) {
		t.Fatalf("%v", names)
	}
	sub, err := f.Dir(ctx, "tab-ok", "src")
	if err != nil || len(sub.Entries) != 2 || sub.Entries[0].Path != "src/ui" || sub.Entries[1].Path != "src/main.go" || sub.Path != "src" {
		t.Fatalf("%+v %v", sub, err)
	}
	file, err := f.File(ctx, "tab-ok", "src/main.go")
	if err != nil || file.Path != "src/main.go" || file.Content != "package main\n" || file.Bytes != 13 || file.Revision == "" || file.Validate() != nil {
		t.Fatalf("%+v %v", file, err)
	}

	code := func(err error) wire.RefuseCode {
		var fe *wire.FileError
		if !errors.As(err, &fe) {
			return wire.RefuseCode(err.Error())
		}
		return fe.Code
	}
	for path, want := range map[string]wire.RefuseCode{
		"../x": wire.RefuseForbidden, "/etc/passwd": wire.RefuseForbidden, "escape/secret": wire.RefuseForbidden,
		"leak": wire.RefuseForbidden, "nope.txt": wire.RefuseNotFound, "blob.bin": wire.RefuseUnsupported,
		"big.txt": wire.RefuseUnsupported, "src": wire.RefuseUnsupported,
	} {
		if _, err := f.File(ctx, "tab-ok", path); err == nil || code(err) != want {
			t.Errorf("file %q: %v, want %s", path, err, want)
		}
	}
	for path, want := range map[string]wire.RefuseCode{"../x": wire.RefuseForbidden, "escape": wire.RefuseForbidden, "nope": wire.RefuseNotFound, "README.md": wire.RefuseNotFound} {
		if _, err := f.Dir(ctx, "tab-ok", path); err == nil || code(err) != want {
			t.Errorf("dir %q: %v, want %s", path, err, want)
		}
	}
	if _, err := f.Dir(ctx, "tab-none", ""); !errors.Is(err, convo.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := f.Dir(ctx, "tab-nocwd", ""); err == nil || code(err) != wire.RefuseNotFound {
		t.Errorf("no folder: %v", err)
	}
}

// A save: whole and over the revision the page read, keeping the file's mode; a changed file is a conflict, what leaves
// the folder, is not text or is too large is refused as for reading, and no temporary file is left behind.
func TestV2FilesSave(t *testing.T) {
	cwd, outside := t.TempDir(), t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(cwd, "run.sh"), []byte("#!/bin/sh\n"), 0o755))
	must(os.Chmod(filepath.Join(cwd, "run.sh"), 0o755)) // the umask took bits off
	must(os.WriteFile(filepath.Join(cwd, "notes.md"), []byte("# a\n"), 0o644))
	must(os.WriteFile(filepath.Join(cwd, "locked.txt"), []byte("x"), 0o444))
	must(os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o644))
	must(os.Symlink(outside, filepath.Join(cwd, "escape")))
	must(os.Symlink(filepath.Join(outside, "secret"), filepath.Join(cwd, "leak")))
	must(os.Symlink("notes.md", filepath.Join(cwd, "inside")))
	f := &v2Files{record: func(id string) (wire.Conversation, bool) {
		if id == "tab-ok" {
			return wire.Conversation{ID: id, Cwd: cwd}, true
		}
		return wire.Conversation{}, false
	}}
	ctx := context.Background()
	code := func(err error) wire.RefuseCode {
		var fe *wire.FileError
		if !errors.As(err, &fe) {
			return wire.RefuseCode(err.Error())
		}
		return fe.Code
	}

	read, err := f.File(ctx, "tab-ok", "run.sh")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.Save(ctx, "tab-ok", "run.sh", "#!/bin/sh\necho hi\n", read.Revision)
	if err != nil || saved.Content != "#!/bin/sh\necho hi\n" || saved.Bytes != 18 || saved.Revision == read.Revision || saved.Validate() != nil {
		t.Fatalf("%+v %v", saved, err)
	}
	disk, _ := os.ReadFile(filepath.Join(cwd, "run.sh"))
	st, _ := os.Stat(filepath.Join(cwd, "run.sh"))
	if string(disk) != saved.Content || st.Mode().Perm() != 0o755 {
		t.Fatalf("on disk: %q %v", disk, st.Mode())
	}
	if again, err := f.File(ctx, "tab-ok", "run.sh"); err != nil || again.Revision != saved.Revision {
		t.Fatalf("the revision a save answers is the one a read gives: %+v %v", again, err)
	}
	// The old revision is stale now; so is one the disk moved past without the page.
	if _, err := f.Save(ctx, "tab-ok", "run.sh", "mine", read.Revision); code(err) != wire.RefuseConflict {
		t.Errorf("stale: %v", err)
	}
	must(os.WriteFile(filepath.Join(cwd, "run.sh"), []byte("changed by an agent\n"), 0o755))
	if _, err := f.Save(ctx, "tab-ok", "run.sh", "mine", saved.Revision); code(err) != wire.RefuseConflict {
		t.Errorf("changed on disk: %v", err)
	}
	if disk, _ := os.ReadFile(filepath.Join(cwd, "run.sh")); string(disk) != "changed by an agent\n" {
		t.Errorf("a refused save wrote: %q", disk)
	}

	// A link inside the folder is saved through to the file it leads to.
	notes, _ := f.File(ctx, "tab-ok", "inside")
	if _, err := f.Save(ctx, "tab-ok", "inside", "# b\n", notes.Revision); err != nil {
		t.Errorf("inside link: %v", err)
	}
	if disk, _ := os.ReadFile(filepath.Join(cwd, "notes.md")); string(disk) != "# b\n" {
		t.Errorf("link target: %q", disk)
	}
	if li, _ := os.Lstat(filepath.Join(cwd, "inside")); li.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced")
	}

	for path, want := range map[string]wire.RefuseCode{
		"../x": wire.RefuseForbidden, "/etc/passwd": wire.RefuseForbidden, "escape/secret": wire.RefuseForbidden,
		"leak": wire.RefuseForbidden, "nope.txt": wire.RefuseNotFound,
	} {
		if _, err := f.Save(ctx, "tab-ok", path, "x", "any"); err == nil || code(err) != want {
			t.Errorf("save %q: %v, want %s", path, err, want)
		}
	}
	locked, _ := f.File(ctx, "tab-ok", "locked.txt")
	if _, err := f.Save(ctx, "tab-ok", "locked.txt", "y", locked.Revision); code(err) != wire.RefuseForbidden {
		t.Errorf("read-only: %v", err)
	}
	if disk, _ := os.ReadFile(filepath.Join(outside, "secret")); string(disk) != "s" {
		t.Errorf("outside was written: %q", disk)
	}
	guard, _ := f.File(ctx, "tab-ok", "notes.md")
	for name, text := range map[string]string{"nul": "a\x00b", "utf8": "a\xffb", "large": strings.Repeat("a", 1_000_001)} {
		if _, err := f.Save(ctx, "tab-ok", "notes.md", text, guard.Revision); code(err) != wire.RefuseUnsupported {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := f.Save(ctx, "tab-none", "notes.md", "x", "any"); !errors.Is(err, convo.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	entries, _ := os.ReadDir(cwd)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".hx-save-") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}

// A file the agent made outside the chat's folder (a worktree beside it) opens from its card by its absolute path;
// another file there, one the turns never touched, stays refused, and so does a path that is not clean.
func TestV2FilesOpenAFileATurnTouchedOutsideTheFolder(t *testing.T) {
	cwd, worktree := t.TempDir(), t.TempDir()
	made := filepath.Join(worktree, "turn.tsx")
	if err := os.WriteFile(made, []byte("export {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &v2Files{
		record: func(id string) (wire.Conversation, bool) { return wire.Conversation{ID: id, Cwd: cwd}, true },
		touched: func(_ context.Context, _ string, p string) (bool, error) {
			return p == made, nil
		},
	}
	ctx := context.Background()
	got, err := f.File(ctx, "tab-ok", made)
	if err != nil || got.Content != "export {};\n" || got.Path != made {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := f.Save(ctx, "tab-ok", made, "export const a = 1;\n", got.Revision); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(worktree, "other.txt"), worktree + "/./turn.tsx"} {
		var fe *wire.FileError
		if _, err := f.File(ctx, "tab-ok", p); !errors.As(err, &fe) || fe.Code != wire.RefuseForbidden {
			t.Fatalf("%s: %v", p, err)
		}
	}
}

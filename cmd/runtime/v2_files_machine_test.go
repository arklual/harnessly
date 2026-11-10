package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// «Машина» in «Файлы» (HRN-679): "/" lists the workspace's places, a folder in them lists with absolute paths and
// without its secrets; a folder outside them, or a secret folder, is refused.
func TestV2FilesMachineDir(t *testing.T) {
	place, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	for _, d := range []string{"proj/src", ".ssh"} {
		if err := os.MkdirAll(filepath.Join(place, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"proj/a.txt", "proj/.env", "proj/key.pem"} {
		if err := os.WriteFile(filepath.Join(place, p), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := &v2Files{
		record: func(id string) (wire.Conversation, bool) { return wire.Conversation{ID: id, Cwd: place}, true },
		access: files.NewAccess([]string{place}, nil),
	}
	ctx := context.Background()
	roots, err := f.Dir(ctx, "c", "/")
	if err != nil || len(roots.Entries) != 1 || roots.Entries[0].Path != place {
		t.Fatalf("places = %+v, %v", roots, err)
	}
	got, err := f.Dir(ctx, "c", filepath.Join(place, "proj"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got.Entries {
		names = append(names, e.Name)
		if !strings.HasPrefix(e.Path, place+"/proj/") {
			t.Errorf("path %q is not absolute in the folder", e.Path)
		}
	}
	if got.Root != place {
		t.Errorf("root = %q, want %q", got.Root, place)
	}
	if strings.Join(names, ",") != "src,a.txt" {
		t.Errorf("entries = %v", names)
	}
	for _, dir := range []string{outside, filepath.Join(place, ".ssh"), place + "/proj/../proj"} {
		if _, err := f.Dir(ctx, "c", dir); err == nil {
			t.Errorf("%s listed", dir)
		}
	}
}

// «Найти в файлах» (HRN-679): the chat's folder answers paths the files tab opens; a folder outside the places, or one
// leading out of the chat's folder, is refused.
func TestV2FilesFindText(t *testing.T) {
	place, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(place, "proj", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(place, "proj", "src", "a.go"), []byte("x := needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &v2Files{
		record: func(id string) (wire.Conversation, bool) {
			return wire.Conversation{ID: id, Cwd: filepath.Join(place, "proj")}, true
		},
		access: files.NewAccess([]string{place}, nil),
	}
	ask := func(query string) (int, string) {
		w := httptest.NewRecorder()
		f.ServeSearch(w, httptest.NewRequest(http.MethodGet, "/x?"+query, nil), "c")
		return w.Code, w.Body.String()
	}
	if code, body := ask("q=needle"); code != 200 || !strings.Contains(body, `"path":"src/a.go","line":1,"col":6`) {
		t.Errorf("chat folder: %d %s", code, body)
	}
	if code, body := ask("q=needle&path=src"); code != 200 || !strings.Contains(body, `"path":"src/a.go"`) {
		t.Errorf("sub-folder: %d %s", code, body)
	}
	abs := filepath.Join(place, "proj")
	if code, body := ask("q=needle&path=" + abs); code != 200 || !strings.Contains(body, `"path":"`+abs+`/src/a.go"`) {
		t.Errorf("machine folder: %d %s", code, body)
	}
	for _, q := range []string{"q=needle&path=" + outside, "q=needle&path=../..", "q=", "q=%20"} {
		if code, _ := ask(q); code == 200 {
			t.Errorf("%s answered 200", q)
		}
	}
}

func TestV2FilesEntryOp(t *testing.T) {
	place, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("XDG_DATA_HOME", filepath.Join(place, ".data"))
	proj := filepath.Join(place, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(filepath.Join(outside, "keep.txt"), filepath.Join(proj, "ln"))
	_ = os.Symlink(outside, filepath.Join(proj, "away"))
	f := &v2Files{
		record: func(id string) (wire.Conversation, bool) {
			return wire.Conversation{ID: id, Cwd: proj}, true
		},
		access: files.NewAccess([]string{place, filepath.Join(proj, "inner"), filepath.Join(place, "linkplace"), filepath.Join(place, "sub", "p2")}, []string{".config/acme"}),
	}
	for _, d := range []string{"inner", ".config/acme"} {
		if err := os.MkdirAll(filepath.Join(proj, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"sub/p2", "real"} {
		if err := os.MkdirAll(filepath.Join(place, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Symlink(filepath.Join(place, "real"), filepath.Join(place, "linkplace"))
	_ = os.WriteFile(filepath.Join(proj, "cert.p12"), []byte("x"), 0o600)
	do := func(op, path, name string) (int, string) {
		body := `{"op":"` + op + `","path":"` + path + `","name":"` + name + `"}`
		w := httptest.NewRecorder()
		f.ServeEntryOp(w, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)), "c")
		return w.Code, w.Body.String()
	}
	if code, body := do("newFile", "", "n.txt"); code != 200 || !strings.Contains(body, `"path":"n.txt"`) {
		t.Fatalf("new file: %d %s", code, body)
	}
	if code, _ := do("newFile", "", "n.txt"); code != http.StatusConflict {
		t.Errorf("taken name: %d", code)
	}
	if code, body := do("rename", "n.txt", "m.txt"); code != 200 || !strings.Contains(body, `"path":"m.txt"`) {
		t.Errorf("rename: %d %s", code, body)
	}
	if code, body := do("rename", "ln", "ln2"); code != 200 {
		t.Errorf("rename a link: %d %s", code, body)
	}
	if code, body := do("trash", "m.txt", ""); code != 200 {
		t.Errorf("trash: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(place, ".data", "Trash", "files", "m.txt")); err != nil {
		t.Errorf("not in the trash: %v", err)
	}
	if code, _ := do("trash", "ln2", ""); code != 200 {
		t.Errorf("trash a link: %d", code)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Errorf("a link's target went: %v", err)
	}
	for _, c := range [][3]string{
		{"newFile", "", ".env"},
		{"newFile", "", "a/b"},
		{"newFile", "away", "x.txt"},
		{"newFile", outside, "x.txt"},
		{"rename", "", "x"},
		{"trash", "", ""},
		{"trash", place, ""},
		{"trash", outside + "/keep.txt", ""},
		{"trash", "../../etc", ""},
		{"rename", "missing", "x"},
		{"wipe", "m.txt", ""},
		{"rename", proj, "gone"},
		{"trash", proj, ""},
		{"rename", "inner", "x"},
		{"trash", filepath.Join(place, "sub"), ""},
		{"trash", filepath.Join(place, "real"), ""},
		{"trash", filepath.Join(place, "linkplace"), ""},
		{"rename", ".config", "cfg"},
		{"trash", ".config", ""},
		{"rename", "cert.p12", "cert.bin"},
	} {
		if code, body := do(c[0], c[1], c[2]); code == 200 {
			t.Errorf("%v answered 200: %s", c, body)
		}
	}
	// A refusal of the system names no path (root writes into a read-only folder, as in CI: nothing to refuse).
	if os.Geteuid() != 0 {
		ro := filepath.Join(proj, "ro")
		_ = os.MkdirAll(ro, 0o755)
		_ = os.WriteFile(filepath.Join(ro, "f"), nil, 0o644)
		_ = os.Chmod(ro, 0o555)
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if code, body := do("rename", "ro/f", "g"); code == 200 || strings.Contains(body, place) {
			t.Errorf("a read-only folder: %d %s", code, body)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "x.txt")); err == nil {
		t.Error("a file was made outside the places")
	}
	if code, body := do("newFile", proj, "abs.txt"); code != 200 || !strings.Contains(body, `"path":"`+proj+`/abs.txt"`) {
		t.Errorf("absolute folder: %d %s", code, body)
	}
}

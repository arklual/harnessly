package delivery

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/files"
)

func makeZip(t *testing.T, dir string, entries map[string]string) string {
	t.Helper()
	p := filepath.Join(dir, "bundle.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return p
}

func TestARecordTravelsInOneLink(t *testing.T) {
	in := Info{Path: "/w/отчёт (1).zip", Name: "отчёт (1).zip", Title: "Отчёт", Mime: "application/zip", Size: 42, SHA256: "ab"}
	link := Link(in)
	if strings.ContainsAny(link, " ()\"'<>[]{}") {
		t.Fatalf("the link breaks the tool result's link finder: %s", link)
	}
	out, ok := LinkIn([]string{"https://example.com/x", link})
	if !ok || out != in {
		t.Fatalf("got %+v %v", out, ok)
	}
	if _, ok := ParseLink("https://example.com/file?path=/etc/passwd&name=x&size=1"); ok {
		t.Fatal("a link of another host is a record")
	}
	if _, ok := ParseLink("https://" + Host + "/file?path=rel&name=x&size=1"); ok {
		t.Fatal("a relative path is not a record")
	}
}

func TestDescribeReadsTheFileUnderTheAccessRules(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	a := files.NewAccess([]string{root}, nil)
	p := makeZip(t, root, map[string]string{"a.txt": "hello"})
	info, refusal := Describe(a, p, " Bundle ")
	if refusal != nil {
		t.Fatal(refusal)
	}
	raw, _ := os.ReadFile(p)
	sum := sha256.Sum256(raw)
	if info.SHA256 != hex.EncodeToString(sum[:]) || info.Size != int64(len(raw)) || info.Mime != "application/zip" || info.Title != "Bundle" || info.Name != "bundle.zip" {
		t.Fatalf("%+v", info)
	}
	if !info.Opens() {
		t.Fatal("a zip opens by itself")
	}
	if (Info{Name: "x.docx"}).Opens() {
		t.Fatal("a docx is only downloaded")
	}
	// Outside the workspace, through a link out of it, and a secret: refused.
	other := makeZip(t, outside, map[string]string{"a": "b"})
	if _, refusal := Describe(a, other, ""); refusal == nil || refusal.Status != 403 {
		t.Fatalf("outside: %v", refusal)
	}
	if err := os.Symlink(other, filepath.Join(root, "link.zip")); err != nil {
		t.Fatal(err)
	}
	if _, refusal := Describe(a, filepath.Join(root, "link.zip"), ""); refusal == nil || refusal.Status != 403 {
		t.Fatalf("link out: %v", refusal)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("K=v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, refusal := Describe(a, filepath.Join(root, ".env"), ""); refusal == nil {
		t.Fatal("a secret is not handed over")
	}
	if _, refusal := Describe(a, filepath.Join(root, "nope.zip"), ""); refusal == nil || refusal.Status != 404 {
		t.Fatalf("missing: %v", refusal)
	}
}

func serve(a *files.Access, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/platform/file-preview?"+query, nil)
	w := httptest.NewRecorder()
	a.ServePreview(w, r, files.RequestQuery(r))
	return w
}

func TestAZipListsItsFilesAndGivesOneOut(t *testing.T) {
	root := t.TempDir()
	a := files.NewAccess([]string{root}, nil)
	p := makeZip(t, root, map[string]string{"docs/a.txt": "hello", "b.bin": "xyz", "../evil.txt": "no", "/abs.txt": "no"})
	list := serve(a, "zip=list&path="+p)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"docs/a.txt"`) || strings.Contains(list.Body.String(), "evil") || strings.Contains(list.Body.String(), "abs.txt") {
		t.Fatalf("%d %s", list.Code, list.Body.String())
	}
	got := serve(a, "zip=entry&path="+p+"&name=docs/a.txt")
	if got.Code != 200 || got.Body.String() != "hello" || !strings.HasPrefix(got.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("%d %q %v", got.Code, got.Body.String(), got.Header())
	}
	for _, name := range []string{"../evil.txt", "/abs.txt", "..%2F..%2Fetc%2Fpasswd", "missing.txt", ""} {
		if w := serve(a, "zip=entry&path="+p+"&name="+name); w.Code < 400 {
			t.Fatalf("%q came out: %d", name, w.Code)
		}
	}
	// An archive outside the workspace is not opened at all.
	outside := makeZip(t, t.TempDir(), map[string]string{"a": "b"})
	if w := serve(a, "zip=list&path="+outside); w.Code != 403 {
		t.Fatalf("outside: %d", w.Code)
	}
}

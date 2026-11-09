package diskfs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (*Server, string, string) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	outside := filepath.Join(tmp, "outside")
	for _, d := range []string{root, outside, filepath.Join(root, "docs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "docs", "leak.txt")); err != nil {
		t.Fatal(err)
	}
	s := New(Config{Roots: []Root{{ID: "main", Label: "Main", Path: root}}, StateDir: filepath.Join(tmp, "state"), ChunkSize: 4})
	return s, root, outside
}

func call(s *Server, method, target string, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	u, _ := url.Parse(target)
	s.Serve(w, r, strings.Split(strings.TrimPrefix(u.Path, "/api/fs/"), "/"))
	return w
}

func TestNoRootsMeansOff(t *testing.T) {
	if New(Config{}) != nil {
		t.Fatal("a server without roots must not exist")
	}
}

func TestParseRoots(t *testing.T) {
	roots, err := ParseRoots("Home dir=/home/x;Work=/work")
	if err != nil || len(roots) != 2 || roots[0].ID != "home-dir" {
		t.Fatalf("%v %v", roots, err)
	}
	for _, bad := range []string{"x=relative", "=/a", "a=/x;a=/y"} {
		if _, err := ParseRoots(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestNothingLeavesTheRoot(t *testing.T) {
	s, root, _ := setup(t)
	for _, p := range []string{"..", "../outside", "docs/../../outside", "/etc", "escape", "escape/secret.txt", "docs/%00", "a\\..\\b"} {
		for _, route := range []string{"list", "download"} {
			w := call(s, "GET", "/api/fs/"+route+"?root=main&path="+url.QueryEscape(p), "", nil)
			if w.Code == 200 {
				t.Errorf("%s %q answered 200: %s", route, p, w.Body)
			}
		}
	}
	if w := call(s, "GET", "/api/fs/download?root=main&path=docs/leak.txt", "", nil); w.Code != 403 {
		t.Errorf("a link to a file outside: %d", w.Code)
	}
	if w := call(s, "POST", "/api/fs/mkdir", `{"root":"main","path":"escape","name":"x"}`, nil); w.Code == 201 {
		t.Error("mkdir through a link out of the root")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "outside", "x")); err == nil {
		t.Error("a folder appeared outside the root")
	}
	for _, name := range []string{"..", "a/b", ""} {
		b, _ := json.Marshal(map[string]string{"root": "main", "path": "", "name": name})
		if w := call(s, "POST", "/api/fs/mkdir", string(b), nil); w.Code == 201 {
			t.Errorf("mkdir name %q", name)
		}
	}
	if w := call(s, "POST", "/api/fs/uploads", `{"root":"main","path":"escape","name":"x","size":1}`, nil); w.Code == 201 {
		t.Error("upload through a link out of the root")
	}
	if w := call(s, "POST", "/api/fs/favorites", `{"root":"main","path":"../outside"}`, nil); w.Code == 200 {
		t.Error("favorite outside the root")
	}
}

func TestListDownloadMkdirAndFavorites(t *testing.T) {
	s, _, _ := setup(t)
	w := call(s, "GET", "/api/fs/list?root=main&path=docs", "", nil)
	var page struct {
		Entries []entry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(page.Entries) != 2 || page.Entries[0].Name != "a.txt" || page.Entries[1].Kind != "link" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := call(s, "GET", "/api/fs/download?root=main&path=docs/a.txt", "", nil); w.Code != 200 || w.Body.String() != "hello" ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "a.txt") {
		t.Fatalf("download: %d %q", w.Code, w.Body)
	}
	if w := call(s, "POST", "/api/fs/mkdir", `{"root":"main","path":"docs","name":"new"}`, nil); w.Code != 201 {
		t.Fatalf("mkdir %d", w.Code)
	}
	if w := call(s, "POST", "/api/fs/mkdir", `{"root":"main","path":"docs","name":"new"}`, nil); w.Code != 409 {
		t.Fatalf("mkdir again %d", w.Code)
	}
	if w := call(s, "POST", "/api/fs/favorites", `{"root":"main","path":"docs/new"}`, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"new"`) {
		t.Fatalf("pin %d %s", w.Code, w.Body)
	}
	if w := call(s, "DELETE", "/api/fs/favorites?root=main&path=docs/new", "", nil); !strings.Contains(w.Body.String(), `"favorites":[]`) {
		t.Fatalf("unpin %s", w.Body)
	}
}

func TestResumableUpload(t *testing.T) {
	s, root, _ := setup(t)
	w := call(s, "POST", "/api/fs/uploads", `{"root":"main","path":"docs","name":"a.txt","size":6,"onConflict":"ask"}`, nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"exists"`) {
		t.Fatalf("conflict %d %s", w.Code, w.Body)
	}
	w = call(s, "POST", "/api/fs/uploads", `{"root":"main","path":"docs","name":"a.txt","size":6,"onConflict":"keep-both"}`, nil)
	var created struct {
		UploadID string `json:"uploadId"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || created.Name != "a (1).txt" {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	base := "/api/fs/uploads/" + created.UploadID
	if w := call(s, "PUT", base, "abcd", map[string]string{"Upload-Offset": "0"}); w.Code != 200 {
		t.Fatalf("chunk %d %s", w.Code, w.Body)
	}
	if w := call(s, "PUT", base, "ef", map[string]string{"Upload-Offset": "0"}); w.Code != 409 || !strings.Contains(w.Body.String(), `"offset":4`) {
		t.Fatalf("mismatch %d %s", w.Code, w.Body)
	}
	if w := call(s, "HEAD", base, "", nil); w.Header().Get("Upload-Offset") != "4" {
		t.Fatalf("head %v", w.Header())
	}
	if w := call(s, "POST", base+"/complete", "", nil); w.Code != 409 {
		t.Fatalf("early complete %d", w.Code)
	}
	if w := call(s, "PUT", base, "efgh", map[string]string{"Upload-Offset": "4"}); w.Code != 413 {
		t.Fatalf("too much %d", w.Code)
	}
	call(s, "PUT", base, "ef", map[string]string{"Upload-Offset": "4"})
	if w := call(s, "POST", base+"/complete", "", nil); w.Code != 200 {
		t.Fatalf("complete %d %s", w.Code, w.Body)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "docs", "a (1).txt")); string(got) != "abcdef" {
		t.Fatalf("file %q", got)
	}
	if w := call(s, "HEAD", base, "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("session after complete %d", w.Code)
	}
}

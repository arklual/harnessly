package files

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileBytesSavesOverItsRevisionOnly(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.csv")
	if err := os.WriteFile(file, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	// Set apart from the creation: the umask may have cut what WriteFile asked for.
	if err := os.Chmod(file, 0o640); err != nil {
		t.Fatal(err)
	}
	a := NewAccess([]string{dir}, nil)
	call := func(method, path, revision, body string) *httptest.ResponseRecorder {
		q := url.Values{"path": {path}}
		if revision != "" {
			q.Set("revision", revision)
		}
		r := httptest.NewRequest(method, "/api/v2/file-bytes?"+q.Encode(), strings.NewReader(body))
		w := httptest.NewRecorder()
		a.ServeFileBytes(w, r)
		return w
	}
	revisionOf := func(w *httptest.ResponseRecorder) string {
		var doc struct {
			Revision string `json:"revision"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s: %v", w.Body.String(), err)
		}
		return doc.Revision
	}

	first := revisionOf(call(http.MethodGet, file, "", ""))
	saved := call(http.MethodPut, file, first, "new")
	if saved.Code != http.StatusOK {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}
	if got, _ := os.ReadFile(file); string(got) != "new" {
		t.Fatalf("file holds %q", got)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o640 {
		t.Fatalf("the file's mode changed: %v", st.Mode())
	}

	if w := call(http.MethodPut, file, first, "stale"); w.Code != http.StatusConflict {
		t.Fatalf("a save over an old revision: %d", w.Code)
	}
	if got, _ := os.ReadFile(file); string(got) != "new" {
		t.Fatalf("a refused save changed the file: %q", got)
	}
	if w := call(http.MethodPut, file, "", "forced"); w.Code != http.StatusOK {
		t.Fatalf("a save with no revision: %d", w.Code)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("a save left files behind: %v", entries)
	}
}

func TestFileBytesRefusesWhatItMustNotWrite(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"a.png": "x", ".env.csv": "x"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "b.csv"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewAccess([]string{dir}, nil)
	for path, want := range map[string]int{
		filepath.Join(dir, "a.png"):     http.StatusUnsupportedMediaType,
		filepath.Join(outside, "b.csv"): http.StatusForbidden,
		filepath.Join(dir, "gone.csv"):  http.StatusNotFound,
		"relative.csv":                  http.StatusBadRequest,
	} {
		q := url.Values{"path": {path}}
		w := httptest.NewRecorder()
		a.ServeFileBytes(w, httptest.NewRequest(http.MethodPut, "/api/v2/file-bytes?"+q.Encode(), strings.NewReader("y")))
		if w.Code != want {
			t.Errorf("%s: %d, want %d", path, w.Code, want)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(outside, "b.csv")); string(got) != "x" {
		t.Errorf("a file outside was written: %q", got)
	}
}

func TestFileBytesKeepsAHistoryThatCanBeRestored(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.csv")
	if err := os.WriteFile(file, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewAccess([]string{dir}, nil)
	a.VersionsDir = t.TempDir()
	call := func(method, query, body string) *httptest.ResponseRecorder {
		q := url.Values{"path": {file}}.Encode()
		if query != "" {
			q += "&" + query
		}
		w := httptest.NewRecorder()
		a.ServeFileBytes(w, httptest.NewRequest(method, "/api/v2/file-bytes?"+q, strings.NewReader(body)))
		return w
	}
	type version struct {
		ID string `json:"id"`
	}
	versions := func() []version {
		var doc struct {
			Versions []version `json:"versions"`
		}
		if err := json.Unmarshal(call(http.MethodGet, "versions=1", "").Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Versions
	}

	// Being looked at keeps what is there, once.
	call(http.MethodGet, "", "")
	call(http.MethodGet, "", "")
	if got := versions(); len(got) != 1 {
		t.Fatalf("after two looks: %v", got)
	}
	first := versions()[0].ID

	// What the agent wrote while the file stood open has a way back too.
	future := time.Now().Add(time.Hour)
	if err := os.WriteFile(file, []byte("agent's"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, future, future); err != nil {
		t.Fatal(err)
	}
	call(http.MethodGet, "", "")
	if got := versions(); len(got) != 2 {
		t.Fatalf("after the agent's write: %v", got)
	}

	if w := call(http.MethodPost, "restore="+first, ""); w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(file); string(got) != "one" {
		t.Fatalf("restored %q", got)
	}
	// The restore can itself be undone: the state it replaced stays in the list.
	if got := versions(); len(got) < 2 {
		t.Fatalf("after the restore: %v", got)
	}
	if w := call(http.MethodPost, "restore=../../etc", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("a path for a version: %d", w.Code)
	}
	if w := call(http.MethodPost, "restore=1-1", ""); w.Code != http.StatusNotFound {
		t.Fatalf("a version that is not there: %d", w.Code)
	}
}

func TestARevisionIsToldOfAnyFileThePaneOpens(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewAccess([]string{dir}, nil)
	a.VersionsDir = t.TempDir()
	q := url.Values{"path": {file}}.Encode()
	w := httptest.NewRecorder()
	a.ServeFileBytes(w, httptest.NewRequest(http.MethodGet, "/api/v2/file-bytes?"+q, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if entries, _ := os.ReadDir(a.VersionsDir); len(entries) != 0 {
		t.Fatalf("a file it does not edit got a history: %v", entries)
	}
	put := httptest.NewRecorder()
	a.ServeFileBytes(put, httptest.NewRequest(http.MethodPut, "/api/v2/file-bytes?"+q, strings.NewReader("y")))
	if put.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a PUT of it: %d", put.Code)
	}
}

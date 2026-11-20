package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/notebooks"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const notebookFixture = `{"nbformat":4,"nbformat_minor":5,"metadata":{"kernelspec":{"name":"untrusted","argv":["must-not-run"]}},"cells":[]}`

func notebookFiles(t *testing.T) (*v2Files, string, string) {
	t.Helper()
	root, outside := t.TempDir(), t.TempDir()
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "book.ipynb"), notebookFixture)
	write(filepath.Join(root, "invalid.ipynb"), "{}")
	write(filepath.Join(root, "plain.txt"), notebookFixture)
	write(filepath.Join(root, "readonly.ipynb"), notebookFixture)
	if err := os.Chmod(filepath.Join(root, "readonly.ipynb"), 0o400); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(outside, "outside.ipynb"), notebookFixture)
	if err := os.Mkdir(filepath.Join(root, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(root, ".ssh", "secret.ipynb"), notebookFixture)
	for name, target := range map[string]string{"alias.ipynb": filepath.Join(root, "book.ipynb"), "escape.ipynb": filepath.Join(outside, "outside.ipynb"), "hidden.ipynb": filepath.Join(root, ".ssh", "secret.ipynb")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	f := &v2Files{record: func(conv string) (wire.Conversation, bool) {
		return wire.Conversation{ID: conv, Cwd: root}, conv == "tab-a"
	},
		access:    files.NewAccess([]string{root, outside}, nil),
		touched:   func(context.Context, string, string) (bool, error) { return true, nil },
		delivered: func(context.Context, string, string) (string, bool, error) { return "fixture", true, nil },
	}
	return f, root, outside
}

func TestNotebookPathsUseConversationResolverNotViewerHandover(t *testing.T) {
	f, root, outside := notebookFiles(t)
	for _, path := range []string{"book.ipynb", "alias.ipynb", filepath.Join(root, "book.ipynb")} {
		got, err := f.notebookPath(context.Background(), "tab-a", path)
		if err != nil || got != filepath.Join(root, "book.ipynb") {
			t.Errorf("%s: %s %v", path, got, err)
		}
	}
	for _, path := range []string{"../outside.ipynb", "escape.ipynb", "hidden.ipynb", ".ssh/secret.ipynb", "readonly.ipynb", "plain.txt", "invalid.ipynb", filepath.Join(outside, "outside.ipynb")} {
		if _, err := f.notebookPath(context.Background(), "tab-a", path); err == nil {
			t.Errorf("accepted inaccessible notebook %q", path)
		}
	}
	// The file API does allow this touched path. Notebook execution must not.
	if _, err := f.File(context.Background(), "tab-a", filepath.Join(outside, "outside.ipynb")); err != nil {
		t.Fatalf("fixture is not viewer-accessible: %v", err)
	}
	if _, err := f.notebookPath(context.Background(), "tab-missing", "book.ipynb"); err == nil {
		t.Fatal("missing conversation accepted")
	}
}

func TestNotebookHTTPContractAndSession(t *testing.T) {
	f, root, outside := notebookFiles(t)
	t.Setenv("RUNTIME_NOTEBOOK_PYTHON", filepath.Join(t.TempDir(), "missing-python"))
	m := notebooks.New()
	defer m.Close()
	n := &v2Notebooks{files: f, kernels: m}
	srv := &wire.Server{Notebook: n.Serve}
	h := v2Session(deps{SessionOK: func(r *http.Request) bool { return r.Header.Get("Authorization") == "page-session" }, Claims: func(*http.Request) map[string]any { return map[string]any{"sub": "owner@example.org"} }}, srv)
	call := func(method, path, body string, signed bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v2/conversations/tab-a/notebook?path="+url.QueryEscape(path), strings.NewReader(body))
		if signed {
			r.Header.Set("Authorization", "page-session")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "book.ipynb", "", false); w.Code != 401 {
		t.Fatalf("unsigned: %d %s", w.Code, w.Body.String())
	}
	if w := call("GET", "book.ipynb", "", true); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"state":"stopped"}` {
		t.Fatalf("GET started/probed Python: %d %s", w.Code, w.Body.String())
	}
	for _, action := range []string{"interrupt", "shutdown"} {
		if w := call("POST", "book.ipynb", `{"action":"`+action+`"}`, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"stopped"`) {
			t.Errorf("%s: %d %s", action, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{}`, `null`, `{"action":"execute"}`, `{"action":"unknown"}`, `{"action":"shutdown","code":"x"}`, `{"action":"execute","code":"x","kernel":"arbitrary"}`, `{"action":"execute","code":"x"} {}`} {
		if w := call("POST", "book.ipynb", body, true); w.Code != 400 && w.Code != 413 {
			t.Errorf("invalid body %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	if w := call("POST", "book.ipynb", `{"action":"execute","code":"x"}`, true); w.Code != 503 || !strings.Contains(w.Body.String(), "pip install jupyter_client ipykernel") {
		t.Fatalf("dependency error: %d %s", w.Code, w.Body.String())
	}
	if w := call("GET", filepath.Join(outside, "outside.ipynb"), "", true); w.Code != 403 {
		t.Fatalf("outside folder: %d %s", w.Code, w.Body.String())
	}
	if w := call("PUT", "book.ipynb", "{}", true); w.Code != 405 {
		t.Fatalf("method: %d", w.Code)
	}
	after, err := os.ReadFile(filepath.Join(root, "book.ipynb"))
	if err != nil || string(after) != notebookFixture {
		t.Fatal("notebook route persisted output or metadata")
	}
}

func TestNotebookPathRejectsSecretsWithCanonicalConversationRoot(t *testing.T) {
	f, root, _ := notebookFiles(t)
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	f.record = func(conv string) (wire.Conversation, bool) { return wire.Conversation{ID: conv, Cwd: alias}, true }
	got, err := f.notebookPath(context.Background(), "tab-a", filepath.Join(root, "book.ipynb"))
	if err != nil || got != filepath.Join(root, "book.ipynb") {
		t.Fatalf("canonical path through cwd alias: %q %v", got, err)
	}
	_, err = f.notebookPath(context.Background(), "tab-a", "hidden.ipynb")
	var fe *wire.FileError
	if !errors.As(err, &fe) || fe.Code != wire.RefuseForbidden {
		t.Fatalf("canonical secret accepted: %v", err)
	}
}

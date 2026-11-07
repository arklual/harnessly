package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// hl.files over the route: the workspace is the chat's own, the masks are the app's, and nothing else gets through.
func TestAppFilesRoute(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"}
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}

	ws := t.TempDir()
	for name, body := range map[string]string{"docs/a.md": "alpha", ".env": "SECRET=1", "other/x.md": "x"} {
		p := filepath.Join(ws, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(ws, ".env"), filepath.Join(ws, "docs", "link.md")); err != nil {
		t.Fatal(err)
	}
	conv.Cwd = ws

	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(pkg, apps.ManifestPath)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["permissions"] = []string{"storage", "files.read:docs/**", "files.write:docs/out/*.md"}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)

	code, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-1"}`, page)
	var sess struct{ Session string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &sess) != nil {
		t.Fatalf("session: %d %s", code, body)
	}
	files := func(rest string) (int, string) {
		return call("POST", platformAPI+"/apps/files", `{"session":"`+sess.Session+`",`+rest+`}`, page)
	}

	if code, _ := call("POST", platformAPI+"/apps/files", `{"session":"`+sess.Session+`","op":"read","path":"docs/a.md"}`, nil); code != http.StatusUnauthorized {
		t.Errorf("no page session: %d", code)
	}
	if code, body := files(`"op":"read","path":"docs/a.md"`); code != http.StatusOK || !strings.Contains(body, `"text":"alpha"`) {
		t.Errorf("read inside the mask: %d %s", code, body)
	}
	// The body names no workspace: a cwd or a conversation in it is not read.
	if code, body := files(`"op":"read","path":"docs/a.md","conversation":"tab-2","cwd":"/etc"`); code != http.StatusOK || !strings.Contains(body, "alpha") {
		t.Errorf("read with extra fields: %d %s", code, body)
	}
	for _, c := range []struct{ name, rest string }{
		{"outside the mask", `"op":"read","path":"other/x.md"`},
		{"a secret", `"op":"read","path":".env"`},
		{"a link to a secret", `"op":"read","path":"docs/link.md"`},
		{"parent segment", `"op":"read","path":"docs/../.env"`},
		{"absolute path", `"op":"read","path":"` + filepath.Join(ws, "docs", "a.md") + `"`},
		{"a write the read mask does not give", `"op":"write","path":"docs/b.md","text":"x"`},
		{"a write to the registry's folder", `"op":"write","path":"../apps/installed.json","text":"{}"`},
	} {
		code, body := files(c.rest)
		if code != http.StatusForbidden || !strings.Contains(body, "permission_denied") {
			t.Errorf("%s: %d %s", c.name, code, body)
		}
		if strings.Contains(body, ws) || strings.Contains(body, "SECRET") {
			t.Errorf("%s: the refusal leaks: %s", c.name, body)
		}
	}
	if code, body := files(`"op":"write","path":"docs/out/n.md","text":"new"`); code != http.StatusOK {
		t.Errorf("write inside the mask: %d %s", code, body)
	}
	if got, _ := os.ReadFile(filepath.Join(ws, "docs", "out", "n.md")); string(got) != "new" {
		t.Errorf("the file written: %q", got)
	}
	if code, body := files(`"op":"write","path":"docs/out/n.md"`); code != http.StatusBadRequest {
		t.Errorf("a write with no text: %d %s", code, body)
	}
	if code, _ := files(`"op":"delete","path":"docs/a.md"`); code != http.StatusBadRequest {
		t.Errorf("an unknown op: %d", code)
	}

	// a folder and a search, inside the masks; a watch and git need their own rights.
	if code, body := files(`"op":"list","path":""`); code != http.StatusOK || !strings.Contains(body, `"path":"docs"`) ||
		strings.Contains(body, "other") || strings.Contains(body, ".env") {
		t.Errorf("list: %d %s", code, body)
	}
	if code, body := files(`"op":"search","query":"ALPHA"`); code != http.StatusOK || !strings.Contains(body, `"path":"docs/a.md"`) || strings.Contains(body, "SECRET") {
		t.Errorf("search: %d %s", code, body)
	}
	if code, body := files(`"op":"watch"`); code != http.StatusForbidden || !strings.Contains(body, "files.watch") {
		t.Errorf("a watch without files.watch: %d %s", code, body)
	}
	if code, body := call("POST", platformAPI+"/apps/git", `{"session":"`+sess.Session+`","op":"status"}`, page); code != http.StatusForbidden || !strings.Contains(body, "git.read") {
		t.Errorf("git without git.read: %d %s", code, body)
	}

	// The chat turned the app off: its view's session no longer reaches the workspace.
	conv.Apps = nil
	if code, body := files(`"op":"read","path":"docs/a.md"`); code != http.StatusForbidden || !strings.Contains(body, "not-on") {
		t.Errorf("an app turned off in the chat: %d %s", code, body)
	}
}

// An installed package is out of reach whoever owns it: the copy in the registry and a dev install's source folder, even
// when the source lies inside the workspace and a mask covers it.
func TestAppFilesRouteKeepsPackagesOut(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"}
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	ws := t.TempDir()
	conv.Cwd = ws

	mk := func(dir, id string, perms []string) {
		if err := apps.Scaffold(dir, id, "App", "pane-chat"); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, apps.ManifestPath)
		raw, _ := os.ReadFile(p)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if perms != nil {
			m["permissions"] = perms
		}
		raw, _ = json.Marshal(m)
		_ = os.WriteFile(p, raw, 0o644)
	}
	other := filepath.Join(ws, "other-app")
	mk(other, "acme.other", nil)
	mk(filepath.Join(t.TempDir(), "demo"), "acme.demo", nil)
	demo := filepath.Join(t.TempDir(), "demo2")
	mk(demo, "acme.demo", []string{"storage", "files.read:**", "files.write:**"})
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+other+`","dev":true}`, cli); code != http.StatusOK {
		t.Fatalf("install other: %d %s", code, body)
	}
	agreeRig(t)
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+demo+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install demo: %d %s", code, body)
	}
	agreeRig(t)
	_ = os.WriteFile(filepath.Join(ws, "ok.md"), []byte("fine"), 0o644)

	code, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-1"}`, page)
	var sess struct{ Session string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &sess) != nil {
		t.Fatalf("session: %d %s", code, body)
	}
	files := func(rest string) (int, string) {
		return call("POST", platformAPI+"/apps/files", `{"session":"`+sess.Session+`",`+rest+`}`, page)
	}
	if code, _ := files(`"op":"read","path":"ok.md"`); code != http.StatusOK {
		t.Errorf("a plain file: %d", code)
	}
	for _, c := range []string{
		`"op":"read","path":"other-app/harnessly/app.json"`,
		`"op":"write","path":"other-app/harnessly/app.json","text":"{}"`,
		`"op":"write","path":"other-app/new.txt","text":"x"`,
	} {
		if code, body := files(c); code != http.StatusForbidden {
			t.Errorf("%s: %d %s", c, code, body)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(other, "harnessly", "app.json")); string(b) == "{}" {
		t.Error("the other app's source was overwritten")
	}
}

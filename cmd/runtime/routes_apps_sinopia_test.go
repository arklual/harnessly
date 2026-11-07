package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
)

const sinLint = `{
  "id": "harnessly.deck-lint", "name": "Валидатор презентаций", "description": "Находит слайды с лишним текстом",
  "version": "0.1.0", "harnessly": ">=0.1", "trust": "sandboxed",
  "targets": ["sinopia.slides"], "activation": { "on": ["document"] },
  "permissions": ["sinopia.read", "sinopia.write"],
  "views": [{ "id": "panel", "entry": "ui/panel.html", "targets": ["sinopia.slides"] }],
  "sinopia": {
    "panels": [{ "id": "lint", "view": "panel", "kinds": ["slides"] }],
    "bindings": { "inspector": "panel", "lockUnbound": true },
    "checks": [{ "id": "deck", "on": ["save", "share"], "blockShare": "error",
      "rules": [{ "id": "wordy", "select": "p", "maxWords": 3, "level": "error", "message": "Слишком много слов: {n}" }] }]
  }
}`

const (
	sinProject = "deck1"
	sinPage    = "01ARYZ6S41TSV4RRFFQ69G5FAV"
)

func writeAll(t *testing.T, files map[string]string) {
	t.Helper()
	for file, body := range files {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The Sinopia routes with a real library: a deck of one slide, the validator installed. The validator is on in the deck by
// itself; its check marks the wordy paragraph, blocks sharing, and a view bound to the document reads and writes it.
func TestSinopiaRoutes(t *testing.T) {
	lib := t.TempDir()
	t.Setenv("RUNTIME_DESIGN_LIBRARY_DIR", lib)
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes", "Content-Type": "application/json"}

	app := t.TempDir()
	writeAll(t, map[string]string{
		filepath.Join(app, "harnessly", "app.json"): sinLint,
		filepath.Join(app, "ui", "panel.html"):      "<p>panel</p>",
	})
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+app+`","dev":true}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	dir := filepath.Join(lib, sinProject)
	writeAll(t, map[string]string{
		filepath.Join(dir, "meta", "deck.json"):              `{"v":1}`,
		filepath.Join(dir, "meta", "project.json"):           `{"title":"Колода","kind":"slides"}`,
		filepath.Join(dir, "meta", "pages", sinPage+".json"): `{"id":"` + sinPage + `","title":"Слайд","file":"Slide.dc.html","w":1920,"h":1080}`,
		filepath.Join(dir, "project", "Slide.dc.html"): `<!doctype html><html><head><script src="./support.js"></script></head><body><x-dc>
<main><h1 data-hl-field="title">Title</h1><p>one two three four five</p><p>short</p></main>
</x-dc></body></html>`,
	})

	post := func(path string, v any, h map[string]string) (int, map[string]any) {
		raw, _ := json.Marshal(v)
		code, body := call("POST", platformAPI+path, string(raw), h)
		var out map[string]any
		_ = json.Unmarshal([]byte(body), &out)
		return code, out
	}

	// The editor reports the open document: the app is on in it, the save check starts by itself.
	code, st := post("/apps/sinopia/state", map[string]any{"document": sinProject, "conversation": "tab-1"}, page)
	if code != 200 || st["kind"] != "slides" || st["lockUnbound"] != true || len(st["apps"].([]any)) != 1 {
		t.Fatalf("state: %d %v", code, st)
	}
	var marks []any
	for range 100 {
		_, st = post("/apps/sinopia/state", map[string]any{"document": sinProject, "conversation": "tab-1",
			"selection": []map[string]any{{"id": sinPage + ":1", "tag": "h1", "field": "title", "text": "Title"}}}, page)
		if marks, _ = st["marks"].([]any); len(marks) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(marks) != 1 || marks[0].(map[string]any)["node"] != sinPage+":2" || marks[0].(map[string]any)["level"] != "error" {
		t.Fatalf("marks: %v", st)
	}

	// Before sharing: the checks run and block.
	code, out := post("/apps/sinopia/checks", map[string]any{"document": sinProject, "on": "share"}, page)
	if code != 200 || out["blocked"] != true {
		t.Fatalf("share check: %d %v", code, out)
	}
	if code, _ := post("/apps/sinopia/checks", map[string]any{"document": sinProject, "on": "later"}, page); code != 400 {
		t.Errorf("a trigger that is none: %d", code)
	}
	if code, _ := post("/apps/sinopia/state", map[string]any{"document": "nope"}, page); code != 400 {
		t.Errorf("no such document: %d", code)
	}
	if code, _ := post("/apps/sinopia/state", map[string]any{"document": sinProject}, map[string]string{}); code != 401 {
		t.Errorf("not the person's page: %d", code)
	}

	// A view of the panel is shown in the deck: its session names the document, and the library answers it.
	if code, _ := post("/apps/session", map[string]any{"app": "harnessly.deck-lint", "view": "panel", "target": "sinopia.site", "document": sinProject}, page); code == 200 {
		t.Error("a site view for a deck")
	}
	if code, _ := post("/apps/session", map[string]any{"app": "harnessly.deck-lint", "view": "panel", "target": "sinopia.slides"}, page); code == 200 {
		t.Error("a Sinopia view with no document")
	}
	code, sess := post("/apps/session", map[string]any{"app": "harnessly.deck-lint", "view": "panel", "target": "sinopia.slides", "document": sinProject}, page)
	if code != 200 {
		t.Fatalf("session: %d %v", code, sess)
	}
	token := sess["session"].(string)
	code, out = post("/apps/sinopia", map[string]any{"session": token, "method": "query", "params": map[string]any{"selector": "p"}}, page)
	if code != 200 || out["result"].(map[string]any)["count"] != float64(2) {
		t.Fatalf("query: %d %v", code, out)
	}
	// The app holds sinopia.write: the patch lands in the board and is one history entry.
	code, out = post("/apps/sinopia", map[string]any{"session": token, "method": "setFields", "params": map[string]any{"patch": map[string]any{"title": "Новый"}}}, page)
	if code != 200 {
		t.Fatalf("setFields: %d %v", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "project", "Slide.dc.html")); !strings.Contains(string(b), `data-hl-field="title">Новый</h1>`) {
		t.Fatalf("board:\n%s", b)
	}
	// It does not hold sinopia.export.
	code, out = post("/apps/sinopia", map[string]any{"session": token, "method": "render", "params": map[string]any{"board": "x"}}, page)
	if code != http.StatusForbidden || out["error"] != apps.CodePermissionDenied {
		t.Errorf("render: %d %v", code, out)
	}
	if code, out := post("/apps/sinopia", map[string]any{"session": "nope", "method": "document"}, page); code == 200 {
		t.Errorf("no session: %v", out)
	}
}

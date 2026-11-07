package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// A document type added by an app, built from the SDK alone (docs/apps/moodboard): the host makes the project, the app's
// editor is shown in it with the target sinopia.acme.moodboard, and it reads the document and keeps its cards through the SDK.
func TestKindRoutes(t *testing.T) {
	lib := t.TempDir()
	t.Setenv("RUNTIME_DESIGN_LIBRARY_DIR", lib)
	// The library's own new-project template, so a project can be made without the engine.
	writeAll(t, map[string]string{
		filepath.Join(lib, ".template", "artifact.json"):         `{"title":"Шаблон"}`,
		filepath.Join(lib, ".template", "project", "index.html"): "<!doctype html><title>t</title>",
	})
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes", "Content-Type": "application/json"}
	post := func(path string, v any, h map[string]string) (int, map[string]any) {
		raw, _ := json.Marshal(v)
		code, body := call("POST", platformAPI+path, string(raw), h)
		var out map[string]any
		_ = json.Unmarshal([]byte(body), &out)
		return code, out
	}

	// Nothing adds the type yet.
	code, body := call("GET", platformAPI+"/apps/kinds", "", page)
	if code != http.StatusOK || body != "{\"kinds\":[]}\n" && body != "{\"kinds\":[]}" {
		t.Fatalf("no types: %d %q", code, body)
	}
	if code, _ := post("/apps/kinds/create", map[string]any{"kind": "acme.moodboard"}, page); code != http.StatusNotFound {
		t.Errorf("a type nobody adds: %d", code)
	}

	example := filepath.Join("..", "..", "docs", "apps", "moodboard")
	abs, _ := filepath.Abs(example)
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+abs+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	code, body = call("GET", platformAPI+"/apps/kinds", "", page)
	var listed struct {
		Kinds []struct {
			ID, App       string
			SchemaVersion int
		} `json:"kinds"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &listed) != nil || len(listed.Kinds) != 1 || listed.Kinds[0].ID != "acme.moodboard" || listed.Kinds[0].SchemaVersion != 2 {
		t.Fatalf("types: %d %s", code, body)
	}
	if code, _ := call("GET", platformAPI+"/apps/kinds", "", nil); code != http.StatusUnauthorized {
		t.Errorf("not the person's page: %d", code)
	}

	// A new document: a project of the library carrying the type.
	code, made := post("/apps/kinds/create", map[string]any{"kind": "acme.moodboard", "title": "Идеи для лендинга"}, page)
	doc, _ := made["document"].(string)
	if code != http.StatusOK || doc == "" {
		t.Fatalf("create: %d %v", code, made)
	}
	code, body = call("GET", platformAPI+"/apps/kinds/docs", "", page)
	var listedDocs struct {
		Docs []struct {
			Document, Title, Kind, KindTitle string
			NeedsApp                         bool
		} `json:"docs"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &listedDocs) != nil || len(listedDocs.Docs) != 1 || listedDocs.Docs[0].Document != doc ||
		listedDocs.Docs[0].Title != "Идеи для лендинга" || listedDocs.Docs[0].KindTitle != "Мудборд" || listedDocs.Docs[0].NeedsApp {
		t.Fatalf("docs: %d %s", code, body)
	}
	if code, _ := call("GET", platformAPI+"/apps/kinds/docs", "", nil); code != http.StatusUnauthorized {
		t.Errorf("docs, not the person's page: %d", code)
	}
	if code, out := post("/apps/kinds/open", map[string]any{"document": doc}, page); code != http.StatusOK || out["kind"] != "acme.moodboard" || out["needsApp"] != false ||
		out["editor"] != "editor" || out["target"] != "sinopia.acme.moodboard" {
		t.Fatalf("open: %d %v", code, out)
	}
	if code, _ := post("/apps/kinds/open", map[string]any{"document": "nosuchdoc"}, page); code != http.StatusNotFound {
		t.Errorf("a project that is no app's document: %d", code)
	}

	// The editor is shown in the document, and only in a document of its own type.
	if code, _ := post("/apps/session", map[string]any{"app": "acme.moodboard", "view": "editor", "target": "sinopia.acme.moodboard"}, page); code == http.StatusOK {
		t.Error("an editor with no document")
	}
	if code, _ := post("/apps/session", map[string]any{"app": "acme.moodboard", "view": "editor", "target": "sinopia.slides", "document": doc}, page); code == http.StatusOK {
		t.Error("an editor in a deck's target")
	}
	code, sess := post("/apps/session", map[string]any{"app": "acme.moodboard", "view": "editor", "target": "sinopia.acme.moodboard", "document": doc}, page)
	if code != http.StatusOK {
		t.Fatalf("session: %d %v", code, sess)
	}
	token := sess["session"].(string)
	code, out := post("/apps/sinopia", map[string]any{"session": token, "method": "document"}, page)
	res, _ := out["result"].(map[string]any)
	if code != http.StatusOK || res["kind"] != "acme.moodboard" {
		t.Fatalf("hl.sinopia.document: %d %v", code, out)
	}
	if code, _ := post("/apps/storage", map[string]any{"session": token, "op": "set", "scope": "doc", "key": "board", "value": map[string]any{"cards": []any{map[string]any{"text": "идея"}}}}, page); code != http.StatusOK {
		t.Fatalf("hl.storage.doc.set: %d", code)
	}
	code, out = post("/apps/storage", map[string]any{"session": token, "op": "get", "scope": "doc", "key": "board"}, page)
	if code != http.StatusOK || out["value"] == nil {
		t.Fatalf("hl.storage.doc.get: %d %v", code, out)
	}

	// A new document is at the app's version; one recorded at an older version is migrated when it is opened.
	if code, out := post("/apps/kinds/open", map[string]any{"document": doc}, page); code != http.StatusOK || out["readOnly"] != false {
		t.Fatalf("open a current document: %d %v", code, out)
	}
	for k, v := range map[string]any{"board": map[string]any{"notes": []any{map[string]any{"text": "старое"}}}, "@schema": 1} {
		if code, _ := post("/apps/storage", map[string]any{"session": token, "op": "set", "scope": "doc", "key": k, "value": v}, page); code != http.StatusOK {
			t.Fatalf("set %s: %d", k, code)
		}
	}
	code, out = post("/apps/kinds/open", map[string]any{"document": doc}, page)
	if sch, _ := out["schema"].(map[string]any); code != http.StatusOK || out["readOnly"] != false || sch["migrated"] != true || sch["from"] != float64(1) {
		t.Fatalf("open an old document: %d %v", code, out)
	}
	code, out = post("/apps/storage", map[string]any{"session": token, "op": "get", "scope": "doc", "key": "board"}, page)
	if raw, _ := json.Marshal(out["value"]); code != http.StatusOK || string(raw) != `{"cards":[{"text":"старое"}]}` {
		t.Fatalf("the migrated document: %d %s", code, raw)
	}
	if code, _ := post("/apps/kinds/revert", map[string]any{"document": doc, "to": 1}, page); code != http.StatusOK {
		t.Errorf("revert: %d", code)
	}
	if code, _ := post("/apps/kinds/revert", map[string]any{"document": doc, "to": 1}, page); code != http.StatusConflict {
		t.Errorf("revert twice: %d", code)
	}
	// A document that does not fit comes back read-only with the reason, and is left as it was.
	for k, v := range map[string]any{"board": []any{"x"}, "@schema": 1} {
		_, _ = post("/apps/storage", map[string]any{"session": token, "op": "set", "scope": "doc", "key": k, "value": v}, page)
	}
	if code, out := post("/apps/kinds/open", map[string]any{"document": doc}, page); code != http.StatusOK || out["readOnly"] != true || out["reason"] == "" {
		t.Errorf("open a document that does not fit: %d %v", code, out)
	}

	// An app that is not an editor of the type is not in the document: a generic app targeting sinopia.* is not either.
	// Removing the app leaves the document there to read, with the note that it needs the app.
	if code, _ := call("POST", "/api/cli/apps/remove", `{"id":"acme.moodboard"}`, cli); code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	if code, out := post("/apps/kinds/open", map[string]any{"document": doc}, page); code != http.StatusOK || out["needsApp"] != true || out["kind"] != "acme.moodboard" {
		t.Errorf("open without the app: %d %v", code, out)
	}
	code, body = call("GET", platformAPI+"/apps/kinds/docs", "", page)
	if code != http.StatusOK || !strings.Contains(body, `"needsApp":true`) || !strings.Contains(body, `"kindTitle":"acme.moodboard"`) {
		t.Errorf("docs without the app: %d %s", code, body)
	}
}

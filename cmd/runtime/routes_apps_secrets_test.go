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

// a view puts its own secret, one it declared, and the person's secret only by the right of that name; the
// value never comes back and never reaches the journal.
func TestAppSecretsPutRoute(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"secrets:token", "secrets.write", "secrets.write:GITHUB_TOKEN", "secrets.write:SCOPED"}
	m["secrets"] = []map[string]any{{"name": "token", "description": "Ключ сервиса", "optional": true}}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, map[string]string{"x-cli-token": "tok"}); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	_, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-1"}`, page)
	var sess struct{ Session string }
	if err := json.Unmarshal([]byte(body), &sess); err != nil || sess.Session == "" {
		t.Fatalf("session: %s", body)
	}
	put := func(rest string) (int, string) {
		return call("POST", platformAPI+"/apps/secrets", `{"session":"`+sess.Session+`",`+rest+`}`, page)
	}
	store := safeFor(rigDeps).store

	if code, body := put(`"name":"token","value":"own-value-1234"`); code != http.StatusOK || strings.TrimSpace(body) != `{"created":true}` {
		t.Fatalf("own: %d %s", code, body)
	}
	if !store.Local.HasApp("acme.demo", "token") || store.Has("token") {
		t.Fatal("the own secret is not the app's alone")
	}
	if code, body := put(`"name":"token","value":"own-value-5678"`); code != http.StatusOK || strings.TrimSpace(body) != `{"created":false}` {
		t.Fatalf("own again: %d %s", code, body)
	}
	if code, body := put(`"name":"other","value":"x1234567"`); code != http.StatusForbidden || !strings.Contains(body, apps.CodePermissionDenied) {
		t.Fatalf("own, not declared: %d %s", code, body)
	}
	if code, body := put(`"name":"NPM_TOKEN","value":"x1234567","shared":true`); code != http.StatusForbidden || store.Has("NPM_TOKEN") {
		t.Fatalf("shared without its right: %d %s", code, body)
	}
	if code, body := put(`"name":"GITHUB_TOKEN","value":"shared-value-1234","shared":true`); code != http.StatusOK || !store.Has("GITHUB_TOKEN") {
		t.Fatalf("shared: %d %s", code, body)
	}
	// A secret other projects alone may use is not replaced from this chat's.
	if _, _, err := store.Put("SCOPED", "scoped-value-1234"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Metadata("SCOPED", nil, []string{"p-other"}); err != nil {
		t.Fatal(err)
	}
	if code, body := put(`"name":"SCOPED","value":"x1234567","shared":true`); code != http.StatusForbidden || !strings.Contains(body, "other projects") {
		t.Fatalf("scoped: %d %s", code, body)
	}
	if code, body := put(`"name":"GITHUB_TOKEN","value":"","shared":true`); code != http.StatusBadRequest {
		t.Fatalf("empty: %d %s", code, body)
	}
	list, err := apps.Open(decRigDir).ReadJournal("acme.demo", 50)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(list)
	if strings.Contains(string(raw), "value-") || !strings.Contains(string(raw), "secrets.put") || !strings.Contains(string(raw), "shared GITHUB_TOKEN") {
		t.Fatalf("journal: %s", raw)
	}
}

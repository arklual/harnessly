package apps

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// extPkg writes a ui extension with a settings schema, a secret and data at the given version.
func extPkg(t *testing.T, version string, dataVersion int, steps map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pkg")
	for rel, body := range map[string]string{"ui/index.html": "<!doctype html><title>x</title>"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	migrations := map[string]string{}
	for from, patch := range steps {
		file := "migrations/" + from + ".json"
		if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(patch), 0o644); err != nil {
			t.Fatal(err)
		}
		migrations[from] = file
	}
	m := map[string]any{
		"id": "example-corp.crm", "name": "CRM", "description": "A CRM link.", "version": version, "harnessly": ">=0.1.0 <2.0.0",
		"trust": "sandboxed", "targets": []string{"settings"}, "kind": "ui",
		"permissions": []string{"storage", "secrets:token"},
		"secrets":     []map[string]any{{"name": "token", "description": "API token"}},
		"settings": map[string]any{"type": "object", "required": []string{"url"}, "properties": map[string]any{
			"url":   map[string]any{"type": "string", "maxLength": 100},
			"mode":  map[string]any{"type": "string", "enum": []string{"a", "b"}, "default": "a"},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 10},
			"on":    map[string]any{"type": "boolean"},
		}},
		"views": []map[string]any{{"id": "main", "title": "CRM", "entry": "ui/index.html", "targets": []string{"settings"}, "label": "CRM"}},
		"data":  map[string]any{"version": dataVersion, "migrations": migrations},
	}
	raw, _ := json.Marshal(m)
	if err := os.MkdirAll(filepath.Join(dir, "harnessly"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestExtensionKinds(t *testing.T) {
	pkg, ps := LoadPackage(extPkg(t, "1.0.0", 1, nil))
	if pkg == nil || len(ps.Errors()) > 0 {
		t.Fatalf("%v", ps)
	}
	if k := pkg.Manifest.ExtensionKind(); k != KindUI {
		t.Fatalf("kind %q", k)
	}
	m := *pkg.Manifest
	m.Kind = ""
	m.Targets = []string{"chat", "pane"}
	m.Agent = &Agent{}
	if m.ExtensionKind() != KindRuntime {
		t.Fatalf("want runtime, got %s", m.ExtensionKind())
	}
	m.Agent, m.Views, m.Targets = &Agent{Instructions: "x"}, nil, nil
	if m.ExtensionKind() != KindRuntime {
		t.Fatalf("agent profile counts as runtime, got %s", m.ExtensionKind())
	}
	m.Agent = nil
	if m.ExtensionKind() != KindTool {
		t.Fatalf("want tool, got %s", m.ExtensionKind())
	}
	// A ui kind written on a package that is more than slots is refused; a tool with views too.
	bad := *pkg.Manifest
	bad.Targets = []string{"settings", "pane"}
	if ps := bad.Check(); problemAt(ps, "kind") == nil {
		t.Fatalf("want ui + pane refused: %v", ps)
	}
	bad = *pkg.Manifest
	bad.Kind = "widget"
	if ps := bad.Check(); problemAt(ps, "kind") == nil {
		t.Fatalf("want unknown kind refused: %v", ps)
	}
}

func TestExtensionSecrets(t *testing.T) {
	pkg, _ := LoadPackage(extPkg(t, "1.0.0", 1, nil))
	m := pkg.Manifest
	safe := secrets.Open(t.TempDir(), filepath.Join(t.TempDir(), "key"))

	if err := safe.PutApp(m.ID, "token", "s3cret-value"); err != nil {
		t.Fatal(err)
	}
	st := m.SecretStates(func(name string) bool { return safe.HasApp(m.ID, name) })
	raw, _ := json.Marshal(st)
	if !st[0].Set || strings.Contains(string(raw), "s3cret") {
		t.Fatalf("state must say set and hold no value: %s", raw)
	}
	if _, err := RevealSecrets(safe, m, []string{"other"}); !errors.Is(err, ErrSecretUndeclared) {
		t.Fatalf("undeclared: %v", err)
	}
	got, err := RevealSecrets(safe, m, []string{"token"})
	if err != nil || got["token"] != "s3cret-value" {
		t.Fatalf("reveal: %v %v", got, err)
	}
	// No secrets:<name> permission, no value.
	noRight := *m
	noRight.Permissions = []string{"storage"}
	if _, err := RevealSecrets(safe, &noRight, []string{"token"}); !errors.Is(err, ErrSecretRight) {
		t.Fatalf("right: %v", err)
	}
}

func TestLifecycleDisableMigrateRemove(t *testing.T) {
	data := t.TempDir()
	r := &Registry{Root: filepath.Join(data, "apps")}
	st := OpenStorage(data)
	safe := secrets.Open(filepath.Join(data, "safe"), filepath.Join(t.TempDir(), "key"))
	opt := InstallOptions{HostVersion: "0.50.0"}

	if _, _, err := r.Install(extPkg(t, "1.0.0", 1, nil), opt); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("example-corp.crm", ScopeInstall, "", "contact", json.RawMessage(`{"name":"A"}`)); err != nil {
		t.Fatal(err)
	}

	// Off: still installed, out of what is on, kept on an update.
	if err := r.SetDisabled("example-corp.crm", true); err != nil {
		t.Fatal(err)
	}
	if list, _ := r.List(); len(list) != 0 {
		t.Fatalf("disabled app is on: %v", list)
	}
	if a, err := r.Find("example-corp.crm"); err != nil || !a.Off {
		t.Fatalf("find: %v %v", a, err)
	}

	// An update with a failing step keeps the old version and the data.
	bad := extPkg(t, "2.0.0", 2, map[string]string{"1": `[{"op":"test","path":"/missing","value":1}]`})
	if _, _, err := r.Install(bad, opt); !errors.Is(err, ErrMigration) {
		t.Fatalf("want ErrMigration, got %v", err)
	}
	if a, _ := r.Find("example-corp.crm"); a.Version != "1.0.0" || st.DataVersion(a.ID) == 2 {
		t.Fatalf("failed update changed the app: %v", a.Version)
	}

	// A good one moves the data and keeps the app off.
	good := extPkg(t, "2.0.0", 2, map[string]string{"1": `[{"op":"move","from":"/contact","path":"/person"}]`})
	got, _, err := r.Install(good, opt)
	if err != nil || got.Version != "2.0.0" || !got.Off {
		t.Fatalf("update: %+v %v", got, err)
	}
	if _, ok, _ := st.Get(got.ID, ScopeInstall, "", "person"); !ok || st.DataVersion(got.ID) != 2 {
		t.Fatalf("data not migrated, version %d", st.DataVersion(got.ID))
	}

	// Compat: the host is outside the range → the app is a warning while it is off, an error once it is on.
	rep, _ := r.Compat("3.0.0")
	if rep.OK != true || len(rep.Apps) != 1 || len(rep.Apps[0].Issues) == 0 || !rep.Apps[0].Issues[0].Warning {
		t.Fatalf("compat off: %+v", rep)
	}
	_ = r.SetDisabled(got.ID, false)
	if rep, _ := r.Compat("3.0.0"); rep.OK {
		t.Fatalf("compat on must fail: %+v", rep)
	}
	if rep, _ := r.Compat("1.0.0"); !rep.OK {
		t.Fatalf("compat in range: %+v", rep)
	}

	// Remove without data keeps it; a reinstall finds it. Remove with purge forgets all of it.
	m := got.Manifest
	_ = safe.PutApp(m.ID, "token", "v")
	_, _ = st.SaveSettings(m, m.ID, map[string]any{"url": "https://example.com"})
	if err := r.Uninstall(got.ID, false, safe); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Get(got.ID, ScopeInstall, "", "person"); !ok || !safe.HasApp(m.ID, "token") {
		t.Fatal("remove without purge lost the data")
	}
	if _, _, err := r.Install(good, opt); err != nil {
		t.Fatal(err)
	}
	if err := r.Uninstall(got.ID, true, safe); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Get(got.ID, ScopeInstall, "", "person"); ok || safe.HasApp(m.ID, "token") {
		t.Fatal("purge left something")
	}
	if g, _ := st.Settings(m, m.ID); g["url"] != nil {
		t.Fatalf("purge left settings: %v", g)
	}
	if _, err := r.Find(got.ID); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("still installed: %v", err)
	}
	// A secret that was never set is nothing to forget: purge must not fail on it (found in the live pass).
	if _, _, err := r.Install(good, opt); err != nil {
		t.Fatal(err)
	}
	if err := r.Uninstall(got.ID, true, safe); err != nil {
		t.Fatalf("purge with no secret set: %v", err)
	}
}

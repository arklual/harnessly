package apps

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A prototype of a mobile screen made of a Sinopia site: no editor view of its own.
const protoApp = `{
  "id": "acme.proto", "name": "Прототип", "description": "Прототип мобильного экрана", "version": "0.1.0", "harnessly": ">=0.17", "trust": "sandboxed",
  "targets": ["sinopia.acme.proto"],
  "permissions": ["sinopia.kind", "sinopia.read", "storage", "app.extend"],
  "views": [{ "id": "flow", "entry": "ui/flow.html", "targets": ["sinopia.acme.proto"] }],
  "sinopia": { "kinds": [{ "id": "acme.proto", "title": "Прототип", "extends": "site", "schemaVersion": 1 }] }
}`

func withExtendList(t *testing.T, list string) {
	t.Helper()
	old := extendList
	extendList = func() string { return list }
	t.Cleanup(func() { extendList = old })
}

func TestExtendsIsChecked(t *testing.T) {
	m, ps := ParseManifest([]byte(protoApp))
	if m == nil || len(ps.Errors()) > 0 {
		t.Fatalf("a type made of a site needs no editor: %v", ps)
	}
	without := strings.Replace(protoApp, `, "app.extend"`, "", 1)
	if _, ps := ParseManifest([]byte(without)); problemAt(ps, "sinopia.kinds[0].extends") == nil {
		t.Fatalf("app.extend is required: %v", ps)
	}
	deck := strings.Replace(protoApp, `"extends": "site"`, `"extends": "deck"`, 1)
	if _, ps := ParseManifest([]byte(deck)); problemAt(ps, "sinopia.kinds[0].extends") == nil {
		t.Fatalf("only a site is extended: %v", ps)
	}
	noEditor := strings.Replace(protoApp, `, "extends": "site"`, "", 1)
	if _, ps := ParseManifest([]byte(noEditor)); problemAt(ps, "sinopia.kinds[0].editor") == nil {
		t.Fatalf("a type of its own still needs its editor: %v", ps)
	}
}

func TestExtendAllowedByTheDeployment(t *testing.T) {
	withExtendList(t, "")
	if ExtendAllowed("acme.proto") {
		t.Fatal("nothing listed: nobody extends")
	}
	withExtendList(t, " acme.other , acme.proto")
	if !ExtendAllowed("acme.proto") || ExtendAllowed("acme.prot") {
		t.Fatal("the list names apps exactly")
	}
	withExtendList(t, "*")
	if !ExtendAllowed("anything") {
		t.Fatal("* lets every app")
	}
}

func TestExtendingKindIsOfferedOnlyWhereAllowed(t *testing.T) {
	m, _ := ParseManifest([]byte(protoApp))
	list := []Installed{{ID: m.ID, Manifest: m}}
	withExtendList(t, "")
	if kinds := kindsOf(list); len(kinds) != 0 {
		t.Fatalf("not listed by the admin: not offered: %+v", kinds)
	}
	if m.OnInDocument("acme.proto") {
		t.Fatal("not allowed: not on in its documents")
	}
	withExtendList(t, "acme.proto")
	kinds := kindsOf(list)
	if len(kinds) != 1 || kinds[0].Extends != ExtendsSite {
		t.Fatalf("offered with what it extends: %+v", kinds)
	}
	if !m.OnInDocument("acme.proto") || m.OnInDocument("site") {
		t.Fatal("on in its own documents only, never in Sinopia's other sites")
	}
}

func TestKindRefusesAnExtendNotAllowed(t *testing.T) {
	reg := newKindRegistry(t, protoApp)
	withExtendList(t, "")
	if _, _, err := reg.Kind("acme.proto"); !errors.Is(err, ErrExtendNotAllowed) {
		t.Fatalf("not allowed: %v", err)
	}
	withExtendList(t, "acme.proto")
	if info, _, err := reg.Kind("acme.proto"); err != nil || info.Extends != ExtendsSite {
		t.Fatalf("allowed: %+v %v", info, err)
	}
}

func newKindRegistry(t *testing.T, manifest string) *Registry {
	t.Helper()
	reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	dir := filepath.Join(t.TempDir(), "pkg")
	for name, body := range map[string]string{ManifestPath: manifest, "ui/flow.html": "<html></html>"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, ps, err := reg.Install(dir, InstallOptions{HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("install: %v %v", err, ps)
	}
	return reg
}

func TestPinnedTabIsChecked(t *testing.T) {
	pinned := strings.Replace(protoApp, `"targets": ["sinopia.acme.proto"] }]`, `"targets": ["sinopia.acme.proto"], "pinned": true }]`, 1)
	m, ps := ParseManifest([]byte(pinned))
	if m == nil || len(ps.Errors()) > 0 {
		t.Fatalf("a pinned tab of a type made of a site: %v", ps)
	}
	withExtendList(t, "acme.proto")
	if v := m.PinnedView("acme.proto"); v == nil || v.ID != "flow" {
		t.Fatalf("the pinned view: %+v", v)
	}
	if m.PinnedView("site") != nil {
		t.Fatal("never beside Sinopia's other sites")
	}
	withExtendList(t, "")
	if m.PinnedView("acme.proto") != nil {
		t.Fatal("not allowed by the deployment: no tab")
	}
	ownEditor := strings.Replace(pinned, `"extends": "site", `, `"editor": "flow", `, 1)
	if _, ps := ParseManifest([]byte(ownEditor)); problemAt(ps, "views[0].pinned") == nil {
		t.Fatalf("a type with an editor of its own has no pane to pin to: %v", ps)
	}
	two := strings.Replace(pinned, `"pinned": true }]`, `"pinned": true }, { "id": "more", "entry": "ui/flow.html", "targets": ["sinopia.acme.proto"], "pinned": true }]`, 1)
	if _, ps := ParseManifest([]byte(two)); problemAt(ps, "views[1].pinned") == nil {
		t.Fatalf("one pinned tab at most: %v", ps)
	}
}

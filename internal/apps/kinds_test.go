package apps

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const kindApp = `{
  "id": "acme.moodboard", "name": "Мудборд", "description": "Тип документа", "version": "0.1.0", "harnessly": ">=0.17", "trust": "sandboxed",
  "targets": ["sinopia.acme.moodboard"],
  "permissions": ["sinopia.kind", "sinopia.read", "storage"],
  "views": [{ "id": "editor", "entry": "ui/editor.html", "targets": ["sinopia.acme.moodboard"] }],
  "sinopia": { "kinds": [{ "id": "acme.moodboard", "title": "Мудборд", "editor": "editor", "schemaVersion": 2, "dataKey": "board", "migrations": { "1": "m/1.json" } }] }
}`

func TestSinopiaKindCheck(t *testing.T) {
	if m, ps := ParseManifest([]byte(kindApp)); m == nil || len(ps) != 0 {
		t.Fatalf("a good kind app: %v", ps)
	}
	cases := map[string]struct{ from, to, path string }{
		"no permission":        {`"sinopia.kind", `, ``, "sinopia.kinds"},
		"a name not the app's": {`"id": "acme.moodboard", "title"`, `"id": "other.board", "title"`, "sinopia.kinds[0].id"},
		"a built-in name":      {`"id": "acme.moodboard", "title"`, `"id": "site", "title"`, "sinopia.kinds[0].id"},
		"a prefix is not it":   {`"id": "acme.moodboard", "title"`, `"id": "acme.moodboardx", "title"`, "sinopia.kinds[0].id"},
		"no title":             {`"title": "Мудборд", "editor"`, `"editor"`, "sinopia.kinds[0].title"},
		"no editor view":       {`"editor": "editor", "schemaVersion"`, `"editor": "nope", "schemaVersion"`, "sinopia.kinds[0].editor"},
		"no version":           {`"schemaVersion": 2`, `"schemaVersion": 0`, "sinopia.kinds[0].schemaVersion"},
		"no step":              {`"migrations": { "1": "m/1.json" }`, `"migrations": {}`, "sinopia.kinds[0].migrations"},
		"a step from the end":  {`"migrations": { "1": "m/1.json" }`, `"migrations": { "1": "m/1.json", "2": "m/2.json" }`, "sinopia.kinds[0].migrations.2"},
		"a step by a name":     {`"migrations": { "1": "m/1.json" }`, `"migrations": { "one": "m/1.json" }`, "sinopia.kinds[0].migrations"},
		"no target":            {`"targets": ["sinopia.acme.moodboard"],`, `"targets": ["sinopia.slides"],`, "sinopia.kinds[0]"},
		"the view not there":   {`"views": [{ "id": "editor", "entry": "ui/editor.html", "targets": ["sinopia.acme.moodboard"] }]`, `"views": [{ "id": "editor", "entry": "ui/editor.html", "targets": ["sinopia.slides"] }]`, "sinopia.kinds[0].editor"},
		"a guest tool":         {`"schemaVersion": 2,`, `"schemaVersion": 2, "guest": { "render": "srv.render" },`, "sinopia.kinds[0].guest"},
	}
	for name, c := range cases {
		raw := strings.Replace(kindApp, c.from, c.to, 1)
		if raw == kindApp {
			t.Fatalf("%s: the case changes nothing", name)
		}
		m, ps := ParseManifest([]byte(raw))
		if m == nil {
			t.Errorf("%s: %v", name, ps)
			continue
		}
		found := false
		for _, p := range ps.Errors() {
			found = found || p.Path == c.path
		}
		if !found {
			t.Errorf("%s: no error at %s in %v", name, c.path, ps)
		}
	}
}

// sinopia.* is the host's own types: an app that adds one has to be named in the target, and a generic app is not in it.
func TestWildcardLeavesAppKindsOut(t *testing.T) {
	m := &Manifest{Targets: []string{"sinopia.*"}}
	if !m.HasTarget("sinopia.slides") || m.HasTarget("sinopia.acme.moodboard") {
		t.Error("sinopia.* covers the host's types only")
	}
	m = &Manifest{Targets: []string{"sinopia.acme.moodboard"}}
	if !m.HasTarget("sinopia.acme.moodboard") || m.HasTarget("sinopia.slides") {
		t.Error("an app that names its type is in it and in no other")
	}
}

func installKindApp(t *testing.T, reg *Registry, id string) error {
	t.Helper()
	dir := filepath.Join(t.TempDir(), id)
	for name, body := range map[string]string{
		ManifestPath:     strings.NewReplacer("acme.moodboard", id).Replace(kindApp),
		"ui/editor.html": "<html></html>",
		"m/1.json":       `[{"op":"move","from":"/notes","path":"/cards"}]`,
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, ps, err := reg.Install(dir, InstallOptions{HostVersion: "0.17.0"})
	if err != nil && !errors.Is(err, ErrKindTaken) {
		t.Fatalf("%s: %v %v", id, err, ps)
	}
	return err
}

func TestKindsOfInstalledApps(t *testing.T) {
	reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	if err := installKindApp(t, reg, "acme.moodboard"); err != nil {
		t.Fatal(err)
	}
	kinds, err := reg.Kinds()
	if err != nil || len(kinds) != 1 || kinds[0].ID != "acme.moodboard" || kinds[0].App != "acme.moodboard" || kinds[0].SchemaVersion != 2 {
		t.Fatalf("kinds: %+v %v", kinds, err)
	}
	if _, k, err := reg.Kind("acme.moodboard"); err != nil || k.Editor != "editor" {
		t.Fatalf("Kind: %+v %v", k, err)
	}
	if _, _, err := reg.Kind("acme.nothing"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("a type nobody adds: %v", err)
	}
	// The same app again is an update, not a clash.
	if err := installKindApp(t, reg, "acme.moodboard"); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	// Another app that adds a type with the same name cannot be: a type is named like its app, so it needs a manifest that
	// names it without being the app (the check refuses that at validate; the registry refuses it as well).
	other := &Manifest{ID: "acme.other", Name: "x", Sinopia: json.RawMessage(`{"kinds":[{"id":"acme.moodboard"}]}`)}
	list, _ := reg.List()
	if k, app := kindConflict(other, manifestsOf(list)); k != "acme.moodboard" || app != "acme.moodboard" {
		t.Fatalf("conflict: %q %q", k, app)
	}
}

func TestApplyPatch(t *testing.T) {
	cases := []struct {
		name, doc, patch, want string
		fails                  bool
	}{
		{"move a key", `{"notes":[{"text":"a"}]}`, `[{"op":"move","from":"/notes","path":"/cards"}]`, `{"cards":[{"text":"a"}]}`, false},
		{"add a member", `{"cards":[]}`, `[{"op":"add","path":"/version","value":2}]`, `{"cards":[],"version":2}`, false},
		{"add at the end of an array", `{"cards":[1]}`, `[{"op":"add","path":"/cards/-","value":2}]`, `{"cards":[1,2]}`, false},
		{"insert in the middle", `{"c":[1,3]}`, `[{"op":"add","path":"/c/1","value":2}]`, `{"c":[1,2,3]}`, false},
		{"replace", `{"a":1}`, `[{"op":"replace","path":"/a","value":2}]`, `{"a":2}`, false},
		{"remove from an array", `{"c":[1,2,3]}`, `[{"op":"remove","path":"/c/1"}]`, `{"c":[1,3]}`, false},
		{"copy", `{"a":{"x":1}}`, `[{"op":"copy","from":"/a","path":"/b"}]`, `{"a":{"x":1},"b":{"x":1}}`, false},
		{"escapes", `{"a/b":1,"c~d":2}`, `[{"op":"replace","path":"/a~1b","value":9},{"op":"remove","path":"/c~0d"}]`, `{"a/b":9}`, false},
		{"a test that holds", `{"v":1}`, `[{"op":"test","path":"/v","value":1},{"op":"replace","path":"/v","value":2}]`, `{"v":2}`, false},
		{"a test that does not", `{"v":1}`, `[{"op":"test","path":"/v","value":5}]`, ``, true},
		{"replace a missing member", `{}`, `[{"op":"replace","path":"/a","value":1}]`, ``, true},
		{"remove a missing member", `{}`, `[{"op":"remove","path":"/a"}]`, ``, true},
		{"past the end of an array", `{"c":[1]}`, `[{"op":"add","path":"/c/5","value":1}]`, ``, true},
		{"into a number", `{"a":1}`, `[{"op":"add","path":"/a/b","value":1}]`, ``, true},
		{"the second step fails", `{"a":1}`, `[{"op":"replace","path":"/a","value":2},{"op":"remove","path":"/zz"}]`, ``, true},
	}
	for _, c := range cases {
		ops, err := ParsePatch([]byte(c.patch))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		got, err := ApplyPatch(json.RawMessage(c.doc), ops)
		switch {
		case c.fails && !errors.Is(err, ErrMigration):
			t.Errorf("%s: want a failed migration, got %s %v", c.name, got, err)
		case !c.fails && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case !c.fails && string(got) != c.want:
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	for _, bad := range []string{`{}`, `[{"op":"frob","path":"/a"}]`, `[{"op":"add","path":"/a"}]`, `[{"op":"add","path":"a","value":1}]`, `[{"op":"move","path":"/a"}]`, `[{"op":"add","path":"/a","value":1,"extra":1}]`} {
		if _, err := ParsePatch([]byte(bad)); err == nil {
			t.Errorf("%s: parsed", bad)
		}
	}
}

// The example moodboard brings a document of its first version up, and a failed step leaves the document as it was.
func TestMoodboardMigration(t *testing.T) {
	pkg, ps := LoadPackage(filepath.Join("..", "..", "docs", "apps", "moodboard"))
	if pkg == nil || len(ps) != 0 {
		t.Fatalf("moodboard: %v", ps)
	}
	s, err := pkg.Manifest.SinopiaOf()
	if err != nil || len(s.Kinds) != 1 {
		t.Fatalf("kinds: %v", err)
	}
	k := s.Kinds[0]
	doc := json.RawMessage(`{"notes":[{"text":"идея"}]}`)
	got, err := k.Migrate(pkg, doc, 1)
	if err != nil || string(got) != `{"cards":[{"text":"идея"}]}` {
		t.Fatalf("migrate: %s %v", got, err)
	}
	if got, err := k.Migrate(pkg, got, 2); err != nil || string(got) != `{"cards":[{"text":"идея"}]}` {
		t.Fatalf("a document at the current version is left alone: %s %v", got, err)
	}
	if _, err := k.Migrate(pkg, json.RawMessage(`{"cards":[]}`), 1); !errors.Is(err, ErrMigration) {
		t.Fatalf("a step that does not fit: %v", err)
	}
	for _, from := range []int{0, 3} {
		if _, err := k.Migrate(pkg, doc, from); !errors.Is(err, ErrMigration) {
			t.Errorf("version %d: %v", from, err)
		}
	}
}

// A package whose migration is no JSON Patch, or whose files are missing, does not pass the check.
func TestKindFilesAreChecked(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mb")
	for name, body := range map[string]string{ManifestPath: kindApp, "ui/editor.html": "<html></html>", "m/1.json": `{"not":"a patch"}`} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, ps := LoadPackage(dir)
	if len(ps.Errors()) == 0 || ps.Errors()[0].Path != "sinopia.kinds[0].migrations.1" {
		t.Fatalf("a migration that is no patch: %v", ps)
	}
	_ = os.Remove(filepath.Join(dir, "m", "1.json"))
	if _, ps := LoadPackage(dir); len(ps.Errors()) == 0 {
		t.Fatalf("a migration file that is missing: %v", ps)
	}
}

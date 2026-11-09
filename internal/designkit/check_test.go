package designkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyTree copies the fixture kit into a temp folder, so a test can break one file of it.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func edit(t *testing.T, root, rel, old, new string) {
	t.Helper()
	p := filepath.Join(root, rel)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s has no %q", rel, old)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func check(t *testing.T, root string) *CheckReport {
	t.Helper()
	rep, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestCheckPassesTheReferenceKit(t *testing.T) {
	rep := check(t, "testdata/check/ok")
	if len(rep.Problems) != 0 {
		t.Fatalf("the reference kit has problems: %+v", rep.Problems)
	}
	if rep.Components != 2 || len(rep.Themes) != 1 {
		t.Fatalf("themes %v, components %d", rep.Themes, rep.Components)
	}
}

func TestCheckNamesEachBrokenPart(t *testing.T) {
	cases := []struct {
		name, file, old, new, level, want string
	}{
		{"no identity", "design-system.json", `"id": "acme-ui", "version": "1.0.0", `, ``, LevelError, "no kit record"},
		{"bad version", "design-system.json", `"version": "1.0.0"`, `"version": "1.0"`, LevelError, "major.minor.patch"},
		{"bad namespace", "design-system.json", `"namespace": "Acme"`, `"namespace": "acme-ui"`, LevelError, "not a JavaScript name"},
		{"claude.ai groups", "design-system.json", `"groups": [`, `"groups": ["Logos"], "x": [`, LevelError, "list of names"},
		{"no description", "design-system.json", `"description": "Text that leads to another page or place; it navigates, it does not act.",`, ``, LevelError, "no description"},
		{"short description", "design-system.json", `"The main way to act on a page: submits a form or starts a flow."`, `"A button."`, LevelWarning, "too short"},
		{"enum without values", "design-system.json", `"values": ["primary", "secondary"], `, ``, LevelError, "needs its values"},
		{"preview missing", "design-system.json", `"components/Button/preview.html"`, `"components/Button/nope.html"`, LevelError, "not in the kit"},
		{"card summary differs", "components/Button/README.md", "The main way to act on a page", "A clickable thing on a page", LevelWarning, "differs from its description"},
		{"no when not", "components/Link/README.md", "## When not to use", "## Notes", LevelWarning, "When not to use"},
		{"no example", "components/Link/README.md", "component-from-global-scope", "component", LevelWarning, "no example"},
		{"dtcg tokens", "tokens.json", `"radius": { "tokens": [ { "name": "radius-m", "value": "8px", "usage": "Buttons and cards." } ] }`, `"radius": { "m": { "$value": "8px" } }`, LevelError, "W3C/DTCG"},
		{"var colour", "tokens.json", `"#14161a"`, `"var(--bg)"`, LevelError, "dropped by the readers"},
		{"named colour", "tokens.json", `"#ffffff"`, `"white"`, LevelError, "dropped by the readers"},
		{"duplicate token", "tokens.json", `"name": "space-2"`, `"name": "accent"`, LevelError, "used once"},
		{"missing mode", "tokens.json", `"value": { "light": "#ffffff", "dark": "#14161a" }`, `"value": { "light": "#ffffff" }`, LevelWarning, "no dark value"},
		{"global css", "components/bundle.css", ":root {", "html, body { margin: 0 }\n:root {", LevelError, "outside @layer"},
		{"own react", "components/bundle.js", "var h", "var __SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED = {}; var h", LevelError, "its own React"},
		{"module", "components/bundle.js", "(function () {", "import React from 'react';\n(function () {", LevelError, "classic script"},
		{"undeclared sample prop", "design-system.json", `"sample": { "href": "#", "children": "Open the event" }`, `"sample": { "href": "#", "label": "Open" }`, LevelError, "not in props"},
		{"no consuming", "README.md", "## Consuming", "## Usage", LevelWarning, "Consuming"},
		{"element of nothing", "design-system.json", `"sample": { "children": "Save" }`, `"sample": { "children": { "$c": "Nope" } }`, LevelError, "not a component of the kit"},
		{"element prop undeclared", "design-system.json", `"sample": { "children": "Save" }`, `"sample": { "children": { "$c": "Link", "props": { "to": "/x" } } }`, LevelError, `sets "to", which is not in props`},
		{"enum value", "design-system.json", `"sample": { "children": "Save" }`, `"sample": { "children": "Save", "variant": "danger" }`, LevelError, "not one of its values"},
		{"bad function", "design-system.json", `"sample": { "children": "Save" }`, `"sample": { "children": { "$fn": "a.b" } }`, LevelError, "a function is"},
		{"element name not a string", "design-system.json", `"sample": { "children": "Save" }`, `"sample": { "children": { "$c": 1 } }`, LevelError, "must be a component's id"},
		{"required missing", "design-system.json", `"variants": [ { "id": "primary", "name": "Primary", "props": { "variant": "primary", "children": "Save" } } ],
          "sample": { "children": "Save" },`, `"variants": [ { "id": "primary", "name": "Primary", "props": { "variant": "primary" } } ],
          "sample": { "disabled": true },`, LevelError, `required prop "children"`},
		{"handler in sample", "design-system.json", `"sample": { "children": "Save" }`, `"sample": { "children": "Save", "onClick": "x" }`, LevelError, "not in props"},
		{"part of nothing", "design-system.json", `"sample": { "href": "#", "children": "Open the event" } }`, `"sample": { "href": "#", "children": "Open the event" }, "part": "Card" }`, LevelError, "not a component of the kit"},
		{"part never shown", "design-system.json", `"sample": { "href": "#", "children": "Open the event" } }`, `"sample": { "href": "#", "children": "Open the event" }, "part": "Button" }`, LevelWarning, "never use it"},
		{"part bad shape", "design-system.json", `"sample": { "href": "#", "children": "Open the event" } }`, `"sample": { "href": "#", "children": "Open the event" }, "part": 3 }`, LevelError, "part must be"},
		{"keywords not a list", "design-system.json", `"sample": { "href": "#", "children": "Open the event" } }`, `"sample": { "href": "#", "children": "Open the event" }, "keywords": "link url" }`, LevelError, "keywords must be a list"},
		{"thumb missing", "design-system.json", `"sample": { "href": "#", "children": "Open the event" } }`, `"sample": { "href": "#", "children": "Open the event" }, "thumb": "_harnessly/thumbs/components/Link.png" }`, LevelError, "thumb _harnessly"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := copyTree(t, "testdata/check/ok")
			edit(t, root, tc.file, tc.old, tc.new)
			rep := check(t, root)
			for _, p := range rep.Problems {
				if p.Level == tc.level && strings.Contains(p.Message, tc.want) {
					return
				}
			}
			t.Fatalf("no %s with %q; got %+v", tc.level, tc.want, rep.Problems)
		})
	}
}

func TestCheckFindsMissingFiles(t *testing.T) {
	for _, tc := range []struct{ remove, want string }{
		{"components/Link/README.md", "the component's card"},
		{"README.md", "every agent that uses the kit starts there"},
		{"tokens.json", "the foundation"},
	} {
		root := copyTree(t, "testdata/check/ok")
		if err := os.Remove(filepath.Join(root, tc.remove)); err != nil {
			t.Fatal(err)
		}
		rep := check(t, root)
		if rep.Errors() == 0 || !strings.Contains(rep.Problems[0].Message+rep.Problems[len(rep.Problems)-1].Message, "") {
			t.Fatalf("%s removed: no error", tc.remove)
		}
		found := false
		for _, p := range rep.Problems {
			found = found || strings.Contains(p.Message, tc.want)
		}
		if !found {
			t.Fatalf("%s removed: no %q in %+v", tc.remove, tc.want, rep.Problems)
		}
	}
	// The bundle at the root, where the live preview does not look.
	root := copyTree(t, "testdata/check/ok")
	if err := os.Rename(filepath.Join(root, "components/bundle.js"), filepath.Join(root, "bundle.js")); err != nil {
		t.Fatal(err)
	}
	rep := check(t, root)
	if rep.Errors() == 0 || !strings.Contains(rep.Problems[0].Message, "the bundle is at bundle.js") {
		t.Fatalf("bundle at the root: %+v", rep.Problems)
	}
}

func TestCheckReadsThemesOfKitJSON(t *testing.T) {
	root := t.TempDir()
	theme := filepath.Join(root, "themes", "light")
	src := copyTree(t, "testdata/check/ok")
	if err := os.MkdirAll(filepath.Dir(theme), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, theme); err != nil {
		t.Fatal(err)
	}
	kit := `{"title":"Acme","themes":[{"id":"light","folder":"themes/light"},{"id":"dark","folder":"themes/dark"}]}`
	if err := os.WriteFile(filepath.Join(root, "kit.json"), []byte(kit), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := check(t, root)
	if len(rep.Themes) != 1 || rep.Themes[0] != "light" || rep.Errors() != 1 || !strings.Contains(rep.Problems[0].Message, "themes/dark has no") {
		t.Fatalf("got %+v", rep)
	}
}

func TestGlobalRuleKeepsLayeredAndScopedRules(t *testing.T) {
	for css, want := range map[string]string{
		"@layer kit-a { body { margin: 0 } * { box-sizing: border-box } }": "",
		".a > * { x: 1 } .b body-text { y: 2 }":                            "",
		"/* body { } */ :root { --a: 1 }":                                  "",
		"@media (min-width: 1px) { body { margin: 0 } }":                   "body",
		"*, *::before { box-sizing: border-box }":                          "*, *::before",
	} {
		if got := globalRule(css); got != want {
			t.Errorf("%q: got %q, want %q", css, got, want)
		}
	}
}

// The kit `kit init` writes passes the check with nothing to say: an agent starts from a valid kit.
func TestInitPassesTheCheck(t *testing.T) {
	for _, source := range []string{"draft", "library"} {
		dir := filepath.Join(t.TempDir(), "kit")
		if _, err := Init(dir, InitOptions{ID: "acme-ui", Namespace: "Acme", Title: "Acme UI", Source: source}); err != nil {
			t.Fatal(err)
		}
		rep := check(t, dir)
		if len(rep.Problems) != 0 || rep.Components != 5 {
			t.Fatalf("%s: %d components, problems %+v", source, rep.Components, rep.Problems)
		}
		if _, err := Init(dir, InitOptions{ID: "acme-ui", Namespace: "Acme", Source: "draft"}); err == nil {
			t.Fatal("init wrote over a kit")
		}
	}
}

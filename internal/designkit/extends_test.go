package designkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installKit copies a fixture kit into <dir>/<id>/<version> under another id and version, optionally extending a kit.
func installKit(t *testing.T, dir, fixture, id, version, extends string) string {
	t.Helper()
	src := copyTree(t, fixture)
	name := filepath.Base(fixture)
	old := `"id": "` + name + `", "version": "1.0.0"`
	repl := `"id": "` + id + `", "version": "` + version + `"`
	if extends != "" {
		repl += `, "extends": "` + extends + `"`
	}
	raw, err := os.ReadFile(filepath.Join(src, "design-system.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, `"extends"`) {
		s = strings.Replace(s, `, "extends": "acme-ui@^1.0.0"`, "", 1)
	}
	if !strings.Contains(s, old) {
		t.Fatalf("%s: no %q", fixture, old)
	}
	if err := os.WriteFile(filepath.Join(src, "design-system.json"), []byte(strings.Replace(s, old, repl, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, filepath.Join(dir, id, version)); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, id, version)
}

const (
	baseFixture  = "testdata/kit/acme-ui"
	childFixture = "testdata/kit/acme-brand"
)

func brandDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	installKit(t, dir, baseFixture, "acme-ui", "1.0.0", "")
	installKit(t, dir, childFixture, "acme-brand", "1.0.0", "acme-ui@^1.0.0")
	return dir
}

func TestAChildIsAssembledWithItsBase(t *testing.T) {
	dir := brandDir(t)
	c, err := ReadCanvas(dir, "acme-brand", "1.0.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Extends == nil || c.Extends.Resolved != "1.0.0" || c.Extends.Error != "" || len(c.Bases) != 1 || c.Bases[0].Kit != "acme-ui" || c.Bases[0].Namespace != "Acme" {
		t.Fatalf("extends %+v bases %+v", c.Extends, c.Bases)
	}
	colors := map[string]ColorToken{}
	for _, t := range c.Colors {
		colors[t.Name] = t
	}
	if len(c.Colors) != 3 || colors["surface-1"].From != "acme-ui@1.0.0" || colors["accent"].From != "" || colors["accent"].Values["light"] != "#d6336c" || colors["brand-ink"].From != "" {
		t.Errorf("colours: %+v", c.Colors)
	}
	if len(c.Tokens) != 2 || c.Tokens[0].From != "acme-ui@1.0.0" {
		t.Errorf("plain tokens come from the base: %+v", c.Tokens)
	}
	if c.Root != "Root" || c.RootNs != "Acme" || c.TokenPrefix != "--acme-" || c.Namespace != "AcmeBrand" {
		t.Errorf("root %q in %q prefix %q ns %q", c.Root, c.RootNs, c.TokenPrefix, c.Namespace)
	}
	button, card := c.Component("Button"), c.Component("Card")
	if button.From != "" || button.Overrides != "acme-ui@1.0.0" || button.Ns != "Acme" || len(button.Props) != 4 {
		t.Errorf("Button replaces the base's: %+v", button)
	}
	if card.From != "acme-ui@1.0.0" || card.Ns != "Acme" || c.NamespaceOf(card) != "Acme" || c.NamespaceOf(button) != "Acme" {
		t.Errorf("Card is inherited: %+v", card)
	}
	if c.Component("PrimaryButton") == nil || len(c.Templates) != 1 || c.Templates[0].From != "acme-ui@1.0.0" {
		t.Errorf("preset and templates are inherited: %v", c.Templates)
	}
	if ns := c.Namespaces(); len(ns) != 2 || ns[0] != "AcmeBrand" || ns[1] != "Acme" {
		t.Errorf("namespaces: %v", ns)
	}
	if hits := Search(c.Index, "panel", 5); len(hits) == 0 || hits[0].From != "acme-ui@1.0.0" {
		t.Errorf("search finds an inherited component with its source: %+v", hits)
	}
}

func TestViewsMarkWhereAnInheritedComponentComesFrom(t *testing.T) {
	dir := brandDir(t)
	c, _ := ReadCanvas(dir, "acme-brand", "1.0.0", "")
	all := ""
	for _, g := range IndexOf(c).Components {
		all += strings.Join(g.Items, "\n") + "\n"
	}
	if !strings.Contains(all, "Card +2 ←acme-ui") || strings.Contains("\n"+all, "\nButton ←") {
		t.Errorf("index:\n%s", all)
	}
	v := ComponentOf(dir, c, "Card")
	if v.From != "acme-ui@1.0.0" || !v.Card.Found || !strings.Contains(v.Call, `"Acme.Card"`) || len(v.Parts) != 2 {
		t.Errorf("an inherited component keeps its card (read from the base) and its namespace: %+v", v)
	}
	b := ComponentOf(dir, c, "Button")
	if b.Overrides != "acme-ui@1.0.0" || !strings.Contains(b.Call, `"Acme.Button"`) || b.Card.WhenNotToUse == nil {
		t.Errorf("Button: %+v", b)
	}
	if u := b.UsedWith.Layouts; len(u) != 1 || u[0] != "pages/record-page" {
		t.Errorf("the base's layout board is read from the base: %v", u)
	}
	if tv := TemplateOf(dir, c, "record-page"); tv == nil || !strings.Contains(tv.Source, "Acme.Card") {
		t.Errorf("template: %+v", tv)
	}
	tk := TokensOf(c, "color")
	for _, tok := range tk.Groups["color"] {
		if (tok.Name == "surface-1") != (tok.From == "acme-ui@1.0.0") {
			t.Errorf("token source: %+v", tok)
		}
	}
}

func TestAChildPassesItsCheckAssembledAndAlone(t *testing.T) {
	dir := brandDir(t)
	root := Resolve(dir, "acme-brand", "1.0.0", nil)
	for _, d := range []string{dir, ""} {
		rep, err := CheckIn(root, d)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Errors() != 0 {
			t.Errorf("dir %q: %+v", d, rep.Problems)
		}
	}
}

func problems(t *testing.T, dir string) string {
	t.Helper()
	rep, err := CheckIn(Resolve(dir, "acme-brand", "1.0.0", nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range rep.Problems {
		if strings.HasPrefix(p.Rule, "extends.") {
			out = append(out, p.Rule)
		}
	}
	return strings.Join(out, ",")
}

func TestExtendsErrors(t *testing.T) {
	// the base is not installed
	dir := t.TempDir()
	installKit(t, dir, childFixture, "acme-brand", "1.0.0", "acme-ui@^1.0.0")
	if got := problems(t, dir); got != "extends.missing" {
		t.Errorf("missing base: %q", got)
	}
	if c, _ := ReadCanvas(dir, "acme-brand", "1.0.0", ""); c.Extends.Rule != "extends.missing" {
		t.Errorf("the canvas says so too: %+v", c.Extends)
	}

	// no version under the range
	dir = t.TempDir()
	installKit(t, dir, baseFixture, "acme-ui", "2.0.0", "")
	installKit(t, dir, childFixture, "acme-brand", "1.0.0", "acme-ui@^1.0.0")
	if got := problems(t, dir); got != "extends.version" {
		t.Errorf("no version: %q", got)
	}

	// a loop: the base extends the child
	dir = t.TempDir()
	installKit(t, dir, baseFixture, "acme-ui", "1.0.0", "acme-brand@^1.0.0")
	installKit(t, dir, childFixture, "acme-brand", "1.0.0", "acme-ui@^1.0.0")
	if got := problems(t, dir); !strings.Contains(got, "extends.cycle") {
		t.Errorf("loop: %q", got)
	}

	// a chain of four bases
	dir = t.TempDir()
	installKit(t, dir, baseFixture, "acme-x3", "1.0.0", "")
	installKit(t, dir, baseFixture, "acme-x2", "1.0.0", "acme-x3@^1.0.0")
	installKit(t, dir, baseFixture, "acme-x1", "1.0.0", "acme-x2@^1.0.0")
	installKit(t, dir, baseFixture, "acme-ui", "1.0.0", "acme-x1@^1.0.0")
	installKit(t, dir, childFixture, "acme-brand", "1.0.0", "acme-ui@^1.0.0")
	if got := problems(t, dir); !strings.Contains(got, "extends.depth") {
		t.Errorf("depth: %q", got)
	}
	// three bases are fine
	dir = t.TempDir()
	installKit(t, dir, baseFixture, "acme-x2", "1.0.0", "")
	installKit(t, dir, baseFixture, "acme-x1", "1.0.0", "acme-x2@^1.0.0")
	installKit(t, dir, baseFixture, "acme-ui", "1.0.0", "acme-x1@^1.0.0")
	installKit(t, dir, childFixture, "acme-brand", "1.0.0", "acme-ui@^1.0.0")
	if got := problems(t, dir); got != "" {
		t.Errorf("three bases: %q", got)
	}
}

func TestAReplacingComponentKeepsTheBasesContract(t *testing.T) {
	cases := map[string]struct{ old, new string }{
		"drops a prop":             {`{ "name": "disabled", "type": "boolean", "description": "greys it out and ignores clicks" },`, ``},
		"retypes a prop":           {`"name": "disabled", "type": "boolean"`, `"name": "disabled", "type": "string"`},
		"drops an enum value":      {`"values": ["primary", "secondary", "ghost"]`, `"values": ["primary", "ghost"]`},
		"requires a new prop":      {`"name": "tone", "type": "enum", "values": ["calm", "bold"]`, `"name": "tone", "required": true, "type": "enum", "values": ["calm", "bold"]`},
		"requires an optional one": {`"name": "variant", "type": "enum"`, `"name": "variant", "required": true, "type": "enum"`},
	}
	for name, c := range cases {
		dir := t.TempDir()
		installKit(t, dir, baseFixture, "acme-ui", "1.0.0", "")
		root := installKit(t, dir, childFixture, "acme-brand", "1.0.0", "acme-ui@^1.0.0")
		edit(t, root, "design-system.json", c.old, c.new)
		rep, err := CheckIn(root, dir)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range rep.Problems {
			found = found || p.Rule == "extends.override"
		}
		if !found {
			t.Errorf("%s: not reported: %+v", name, rep.Problems)
		}
	}
	// the fixture itself adds an optional prop and an enum value: compatible
	if why := PropsCompatible([]Prop{{Name: "a", Type: "enum", Values: []string{"x"}}}, []Prop{{Name: "a", Type: "enum", Values: []string{"x", "y"}}, {Name: "b", Type: "string"}}); why != "" {
		t.Errorf("compatible: %s", why)
	}
}

func TestAPublishedChildShowsTheBaseItWasPublishedWith(t *testing.T) {
	dir := brandDir(t)
	// first read pins acme-ui 1.0.0 for acme-brand 1.0.0
	if _, err := ReadCanvas(dir, "acme-brand", "1.0.0", ""); err != nil {
		t.Fatal(err)
	}
	if got := readPins(dir, "acme-brand", "1.0.0")["acme-ui@^1.0.0"]; got != "1.0.0" {
		t.Fatalf("pin: %q", got)
	}
	// the base gets a release with another accent
	root := installKit(t, dir, baseFixture, "acme-ui", "1.1.0", "")
	edit(t, root, "tokens.json", `"value": { "light": "#ffffff", "dark": "#14161a" }`, `"value": { "light": "#fafafa", "dark": "#101010" }`)
	c, _ := ReadCanvas(dir, "acme-brand", "1.0.0", "")
	if c.Extends.Resolved != "1.0.0" || c.Colors[0].Values["light"] != "#ffffff" {
		t.Errorf("the published child shows the old base: %+v %+v", c.Extends, c.Colors[0])
	}
	// a new version of the child takes the newest base that fits
	installKit(t, dir, childFixture, "acme-brand", "1.0.1", "acme-ui@^1.0.0")
	c, _ = ReadCanvas(dir, "acme-brand", "1.0.1", "")
	if c.Extends.Resolved != "1.1.0" || c.Colors[0].Values["light"] != "#fafafa" {
		t.Errorf("a new version takes the new base: %+v %+v", c.Extends, c.Colors[0])
	}
	// the pinned base must stay installed
	if err := os.RemoveAll(filepath.Join(dir, "acme-ui", "1.0.0")); err != nil {
		t.Fatal(err)
	}
	c, _ = ReadCanvas(dir, "acme-brand", "1.0.0", "")
	if c.Extends.Rule != "extends.missing" || !strings.Contains(c.Extends.Error, "1.0.0") {
		t.Errorf("the pinned base is gone: %+v", c.Extends)
	}
}

func TestExportOfAChildRequiresItsBase(t *testing.T) {
	dir := brandDir(t)
	m, err := BuildManifest(dir, "acme-brand", "1.0.0", ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Requires) != 1 || m.Requires[0] != (Require{ID: "acme-ui", Version: "1.0.0"}) {
		t.Errorf("requires: %+v", m.Requires)
	}
}

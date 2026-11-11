package formats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// semEqual compares two JSON documents by value (key order does not matter).
func semEqual(t *testing.T, want, got []byte) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, got)
	}
	if !reflect.DeepEqual(a, b) {
		wa, _ := json.MarshalIndent(a, "", " ")
		wb, _ := json.MarshalIndent(b, "", " ")
		t.Fatalf("differ.\nwant:\n%s\ngot:\n%s", wa, wb)
	}
}

// newBareKit is a kit without components, so a Storybook import into it is all new.
func newBareKit(t *testing.T) *Kit {
	t.Helper()
	k := newKit(t, nil)
	m, _ := parseOrdered(mustRead(t, filepath.Join(k.Dir, "design-system.json")))
	m.set("groups", []any{})
	if err := os.WriteFile(filepath.Join(k.Dir, "design-system.json"), pretty(m), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"Button", "Card", "CardHeader", "CardBody", "Root"} {
		_ = os.RemoveAll(filepath.Join(k.Dir, "components", c))
	}
	k2, err := OpenKit(k.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return k2
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// newKit makes a valid kit (kit init) with the given tokens.json, the way a person starts one.
func newKit(t *testing.T, tokens []byte) *Kit {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kit")
	if _, err := designkit.Init(dir, designkit.InitOptions{ID: "acme-ui", Namespace: "Acme", Title: "Acme UI", Source: "draft"}); err != nil {
		t.Fatal(err)
	}
	if tokens != nil {
		if err := os.WriteFile(filepath.Join(dir, "tokens.json"), tokens, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	k, err := OpenKit(dir)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func tokenOf(t *testing.T, tk *Tokens, name string) Token {
	t.Helper()
	tok, _ := tk.Find(name)
	if tok == nil {
		t.Fatalf("no token %q in %d tokens", name, tk.Count())
	}
	return *tok
}

func mustContain(t *testing.T, what, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Errorf("%s lacks %q:\n%s", what, p, text)
		}
	}
}

func checkErrors(t *testing.T, dir string) []designkit.Problem {
	t.Helper()
	rep, err := designkit.Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	var errs []designkit.Problem
	for _, p := range rep.Problems {
		if p.Level == designkit.LevelError {
			errs = append(errs, p)
		}
	}
	return errs
}

// ---- the model ------------------------------------------------------------------------------------------------------

func TestTokensModelWritesBackWhatItRead(t *testing.T) {
	for _, name := range []string{"tokens.rich.json"} {
		raw := read(t, name)
		tk, err := ParseTokens(raw)
		if err != nil {
			t.Fatal(err)
		}
		semEqual(t, raw, tk.Marshal())
	}
	scaffold := newKit(t, nil)
	raw, _ := os.ReadFile(filepath.Join(scaffold.Dir, "tokens.json"))
	tk, err := ParseTokens(raw)
	if err != nil {
		t.Fatal(err)
	}
	semEqual(t, raw, tk.Marshal())
}

// ---- DTCG -----------------------------------------------------------------------------------------------------------

func TestDTCGRoundTripLosesNothing(t *testing.T) {
	raw := read(t, "tokens.rich.json")
	tk, err := ParseTokens(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, rep := ExportDTCG(tk)
	if len(rep.NotCarried) == 0 {
		t.Error("the export must list what DTCG has no place for")
	}
	back, _, err := ImportDTCG(out, DTCGOptions{})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	semEqual(t, raw, back.Marshal())
}

func TestDTCGExportShape(t *testing.T) {
	tk, _ := ParseTokens(read(t, "tokens.rich.json"))
	out, rep := ExportDTCG(tk)
	text := string(out)
	mustContain(t, "dtcg", text,
		`"$type": "color"`, `"$value": "#f6f7f9"`, `"$description": "Page background behind cards."`,
		`"mode": {`, `"dark": "#0f1115"`, `{color.accent}`, `"danger_soft"`, `{color.surface-1}`,
		`"$type": "fontFamily"`, `"$type": "typography"`, `{type.families.sans}`, `"offsetY": "1px"`)
	doc := map[string]any{}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	// two shadow layers are a list, one layer an object
	shadow := doc["shadow"].(map[string]any)
	if _, ok := shadow["shadow-2"].(map[string]any)["$value"].([]any); !ok {
		t.Errorf("two layers should be a list: %v", shadow["shadow-2"])
	}
	if _, ok := shadow["shadow-1"].(map[string]any)["$value"].(map[string]any); !ok {
		t.Errorf("one layer should be an object: %v", shadow["shadow-1"])
	}
	mustContain(t, "report", rep.Lines(), "not carried", "components, cards, templates")
	if !strings.Contains(rep.Lines(), "shadow-glow") {
		t.Errorf("a shadow that is not a layer list is named: %s", rep.Lines())
	}
}

func TestDTCGImportOfAForeignFile(t *testing.T) {
	tk, rep, err := ImportDTCG(read(t, "foreign.dtcg.json"), DTCGOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if v := tokenOf(t, tk, "brand-primary"); v.Value.Plain != "#3b5bdb" || v.Usage != "The primary action." {
		t.Errorf("primary: %+v", v)
	}
	if v := tokenOf(t, tk, "brand-ink"); v.Value.Plain != "#16181d" {
		t.Errorf("a colour object becomes hex: %+v", v)
	}
	if v := tokenOf(t, tk, "brand-wash"); v.Value.Plain != "rgb(0 0 0 / 0.5)" {
		t.Errorf("alpha: %+v", v)
	}
	act := tokenOf(t, tk, "brand-action")
	if len(act.Value.Modes) != 2 || act.Value.Modes[0].Value != "{brand-primary}" || act.Value.Modes[1].Mode != "dark" || act.Value.Modes[1].Value != "{brand-ink}" {
		t.Errorf("alias with a mode: %+v", act.Value)
	}
	if got := tk.family("color").Themes; len(got) != 2 || got[0].ID != "light" || got[1].ID != "dark" {
		t.Errorf("themes: %+v", got)
	}
	if v, fam := tk.Find("size-base"); v == nil || fam != "spacing" || v.Value.Plain != "4px" {
		t.Errorf("a {value, unit} dimension: %+v %s", v, fam)
	}
	if v, fam := tk.Find("size-gap-wide"); v == nil || fam != "spacing" || v.Value.Plain != "24px" {
		t.Errorf("a nested group joins its names: %+v %s", v, fam)
	}
	if v, fam := tk.Find("card"); v == nil || fam != "radius" || v.Value.Plain != "12px" {
		t.Errorf("a radii group: %+v %s", v, fam)
	}
	if v, fam := tk.Find("raised"); v == nil || fam != "shadow" || v.Value.Plain != "0 1px 2px #00000014" {
		t.Errorf("a shadow object: %+v %s", v, fam)
	}
	if tk.Type == nil || len(tk.Type.Families) != 1 || tk.Type.Families[0].Value != "Inter, system-ui, sans-serif" {
		t.Fatalf("type: %+v", tk.Type)
	}
	if len(tk.Type.Groups) != 1 || tk.Type.Groups[0].Name != "heading" || tk.Type.Groups[0].Family != tk.Type.Families[0].Key {
		t.Fatalf("groups: %+v", tk.Type.Groups)
	}
	if st := tk.Type.Groups[0].Styles[0]; st.Name != "h1" || st.FontSize != "32px" || st.LineHeight != "1.2" || st.LetterSpacing != "-0.02em" || string(st.FontWeight) != "700" {
		t.Errorf("style: %+v", st)
	}
	lines := rep.Lines()
	mustContain(t, "report", lines, `"duration"`, `"number"`)
	// the dimension that aliases a number the kit does not keep is not silently dropped
	if !strings.Contains(lines, "alias-to-number") {
		t.Errorf("a token that cannot be kept is named:\n%s", lines)
	}
}

func TestDTCGModeFileMergesIntoTheKit(t *testing.T) {
	k := newKit(t, nil)
	dark := `{"color": {"$type": "color", "surface-0": {"$value": "#000000"}, "text": {"$value": "#ffffff"}, "brand": {"$value": "#ff00ff"}}}`
	src := filepath.Join(t.TempDir(), "dark.json")
	if err := os.WriteFile(src, []byte(dark), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := k.Import("dtcg", src, ImportOptions{Mode: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	tk, _, _ := k.Tokens()
	if v := tokenOf(t, tk, "surface-0"); v.Value.For("dark") != "#000000" || v.Value.For("light") != "#f6f7f9" {
		t.Errorf("the light value stays, the dark one is set: %+v", v.Value)
	}
	if _, fam := tk.Find("brand"); fam != "color" {
		t.Errorf("a new token is added")
	}
	mustContain(t, "report", rep.Lines(), "mode values set")
}

// ---- CSS and Tailwind -----------------------------------------------------------------------------------------------

func TestCSSExportThenImportKeepsNamesValuesAndVars(t *testing.T) {
	k := newKit(t, nil)
	out, rep, err := k.Export("css")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, "css", string(out), `:root, [data-theme="light"] {`, `--acme-ui-surface-0: #f6f7f9; /* Page background behind cards. */`,
		`[data-theme="dark"] {`, `--acme-ui-font-sans:`)
	if len(rep.NotCarried) == 0 {
		t.Error("css must list what it does not carry")
	}
	// the scaffold's own tokens.css is the same variables
	own, _ := os.ReadFile(filepath.Join(k.Dir, "tokens.css"))
	if !strings.Contains(string(own), "--acme-ui-surface-0: #f6f7f9;") {
		t.Fatalf("scaffold css changed: %s", own)
	}
	orig, _, _ := k.Tokens()
	back, _, err := ImportCSS(string(out), CSSOptions{Prefix: "--acme-ui-", Modes: orig.Modes()})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range orig.Families {
		for _, want := range f.Tokens {
			got := tokenOf(t, back, want.Name)
			if !reflect.DeepEqual(got.Value, want.Value) || got.Usage != want.Usage || got.Var != "--acme-ui-"+want.Name {
				t.Errorf("%s: got %+v want %+v", want.Name, got, want)
			}
		}
	}
	if v, ok := back.Type.Family("sans"); !ok || !strings.Contains(v, "system-ui") {
		t.Errorf("font family: %+v", back.Type)
	}
}

func TestCSSImportReadsRootDarkAndAliases(t *testing.T) {
	tk, rep, err := ImportCSS(string(read(t, "vars.css")), CSSOptions{Prefix: "--acme-"})
	if err != nil {
		t.Fatal(err)
	}
	s0 := tokenOf(t, tk, "surface-0")
	if s0.Var != "--acme-surface-0" || s0.Usage != "Page background behind cards." || s0.Value.For("light") != "#f6f7f9" || s0.Value.For("dark") != "#0f1115" {
		t.Errorf("surface-0: %+v", s0)
	}
	if v := tokenOf(t, tk, "surface-1"); v.Usage != "Cards and inputs." {
		t.Errorf("a comment after the declaration is its usage: %+v", v)
	}
	if v := tokenOf(t, tk, "link"); v.Value.Plain != "{accent}" {
		t.Errorf("var(--x) of a colour is an alias: %+v", v)
	}
	if v := tokenOf(t, tk, "on-accent"); v.Value.Plain != "#ffffff" {
		t.Errorf("a named colour the readers drop becomes a value: %+v", v)
	}
	if v := tokenOf(t, tk, "text"); v.Value.For("dark") != "#eceef2" {
		t.Errorf("text: %+v", v)
	}
	if _, fam := tk.Find("space-2"); fam != "spacing" {
		t.Errorf("space-2: %s", fam)
	}
	if _, fam := tk.Find("radius-m"); fam != "radius" {
		t.Errorf("radius-m: %s", fam)
	}
	if v, fam := tk.Find("shadow-1"); fam != "shadow" || v.Value.Plain != "0 1px 2px rgba(0, 0, 0, .08)" {
		t.Errorf("shadow-1: %+v %s", v, fam)
	}
	if v, ok := tk.Type.Family("sans"); !ok || !strings.Contains(v, "Acme Sans") {
		t.Errorf("font: %+v", tk.Type)
	}
	if got := tk.family("color").Themes; len(got) != 2 || got[1].ID != "dark" {
		t.Errorf("themes: %+v", got)
	}
	lines := rep.Lines()
	mustContain(t, "report", lines, "--acme-z-modal", "--acme-ring", "--acme-ease", ".card:hover")
}

func TestTailwindExports(t *testing.T) {
	k := newKit(t, read(t, "tokens.rich.json"))
	v3, _, err := k.Export("tailwind")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, "tailwind v3", string(v3), "module.exports", "darkMode: ['selector', '[data-theme=\"dark\"]']", "theme: {", "extend: {", "colors: {",
		"'surface-1': 'var(--acme-ui-surface-1)'", "'surface-0': 'var(--acme-surface-0)'", "borderRadius: {", "'m': 'var(--acme-ui-radius-m)'", "boxShadow: {",
		"fontFamily: {", "'sans': ['var(--acme-ui-font-sans)']", "fontSize: {", "'body': ['15px', { lineHeight: '22px', fontWeight: '400' }]")
	v4, rep, err := k.Export("tailwind4")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, "tailwind v4", string(v4), "@custom-variant dark (", "@theme inline {", "--color-surface-1: var(--acme-ui-surface-1);",
		"--radius-m: var(--acme-ui-radius-m);", "--spacing-2: var(--acme-ui-space-2);", "--font-sans: var(--acme-ui-font-sans);", "--text-body: 15px;",
		"--text-body--line-height: 22px;", ":root, [data-theme=\"light\"] {")
	if len(rep.NotCarried) == 0 {
		t.Error("tailwind must list what it does not carry")
	}
}

// ---- DESIGN.md ------------------------------------------------------------------------------------------------------

func TestDesignMDExportThenImport(t *testing.T) {
	k := newKit(t, read(t, "tokens.rich.json"))
	out, rep, err := k.Export("design-md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	mustContain(t, "design.md", text, "---\nname: \"Acme UI\"\ncolors:\n", `  surface-0: "#f6f7f9"`, `  link: "{colors.accent}"`, "typography:\n  body:\n    fontFamily:",
		"rounded:\n  radius-m:", "## Colors", "| Token | Light | Dark | Use |", "## Components", "#### Button", "| `variant` |", "## Elevation")
	mustContain(t, "report", rep.Lines(), "colour modes other than light", "shadow tokens in the front matter")
	src := filepath.Join(t.TempDir(), "DESIGN.md")
	if err := os.WriteFile(src, out, 0o644); err != nil {
		t.Fatal(err)
	}
	res, _, err := ImportDesignMD(out, DTCGOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tk := res.Tokens
	acc := tokenOf(t, tk, "accent")
	if acc.Value.For("light") != "#3b5bdb" || acc.Value.For("dark") != "#7c94f5" || acc.Usage != "The primary action and links; once per area." {
		t.Errorf("modes and usage come back from the table: %+v", acc)
	}
	if v := tokenOf(t, tk, "link"); v.Value.Default() != "{accent}" {
		t.Errorf("link: %+v", v)
	}
	if v := tokenOf(t, tk, "radius-m"); v.Value.Plain != "8px" || v.Usage != "Buttons and inputs." {
		t.Errorf("radius-m: %+v", v)
	}
	if tk.Type == nil || countStyles(tk.Type) != 3 {
		t.Errorf("type: %+v", tk.Type)
	}
}

func TestDesignMDImportOfAForeignFile(t *testing.T) {
	res, rep, err := ImportDesignMD(read(t, "design.md"), DTCGOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tk := res.Tokens
	if v := tokenOf(t, tk, "primary"); v.Value.Plain != "#1A1C1E" || v.Usage != "Headlines and core text." {
		t.Errorf("a **Name (#hex):** bullet is the usage of the token with that hex: %+v", v)
	}
	if v := tokenOf(t, tk, "tertiary"); v.Usage != "The one call to action per screen." {
		t.Errorf("tertiary: %+v", v)
	}
	if v := tokenOf(t, tk, "neutral"); v.Usage != "Page background." {
		t.Errorf("a **name:** bullet names the token: %+v", v)
	}
	if v := tokenOf(t, tk, "on-tertiary"); v.Value.Plain != "{neutral}" {
		t.Errorf("reference: %+v", v)
	}
	if v, fam := tk.Find("md"); v == nil || fam != "radius" || v.Value.Plain != "8px" {
		t.Errorf("rounded.md: %+v %s", v, fam)
	}
	if v, fam := tk.Find("spacing-md"); v == nil || fam != "spacing" || v.Value.Plain != "16px" {
		t.Errorf("a name used in two families is renamed, none is lost: %+v %s", v, fam)
	}
	if tk.Type == nil || countStyles(tk.Type) != 2 || len(tk.Type.Families) != 1 || tk.Type.Families[0].Value != "Public Sans" {
		t.Errorf("type: %+v", tk.Type)
	}
	// components are not made up from prose or recipes
	lines := rep.Lines()
	mustContain(t, "report", lines, "2 style recipes", "never turned into kit components")
	mustContain(t, "notes", res.Notes, "## Overview", "Architectural minimalism", "## Do's and Don'ts", "button-primary-hover", "## Components")
	if _, _, err := ImportDesignMD([]byte("# no front matter\n\n## Colors\n* **Ink (#000000):** text\n"), DTCGOptions{}); err == nil {
		t.Error("tokens are never read from prose alone")
	}
}

// ---- Storybook ------------------------------------------------------------------------------------------------------

func TestStorybookImportMakesDraftsThatCheckCleanWhereAchievable(t *testing.T) {
	k := newBareKit(t)
	rep, err := k.Import("storybook", filepath.Join("testdata", "storybook"), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lines := rep.Lines()
	mustContain(t, "report", lines, "descriptions, props and imports for 2 components", "MDX and autodocs", "TextField")
	manifest, _ := os.ReadFile(filepath.Join(k.Dir, "design-system.json"))
	var doc struct {
		Groups []struct {
			ID, Title  string
			Components []map[string]any
		}
	}
	if err := json.Unmarshal(manifest, &doc); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	groups := map[string]string{}
	for _, g := range doc.Groups {
		for _, c := range g.Components {
			byID[c["id"].(string)] = c
			groups[c["id"].(string)] = g.ID
		}
	}
	for _, id := range []string{"Button", "Tooltip", "TextField"} {
		if byID[id] == nil {
			t.Errorf("%s missing: %v", id, groups)
		}
	}
	if groups["Button"] != "components" || groups["Tooltip"] != "components-overlays" || groups["TextField"] != "forms" {
		t.Errorf("groups come from the story title: %v", groups)
	}
	if code, _ := byID["Tooltip"]["code"].(map[string]any); code["import"] != "@acme/ui/tooltip" || code["export"] != "default" {
		t.Errorf("code from the manifest's import: %v", byID["Tooltip"]["code"])
	}
	if _, has := byID["TextField"]["description"]; !has || byID["TextField"]["description"] != "" {
		t.Errorf("no invented description: %v", byID["TextField"]["description"])
	}
	todo, _ := byID["TextField"]["todo"].([]any)
	if len(todo) < 3 {
		t.Errorf("a draft lists what it lacks: %v", todo)
	}
	// the card of the one with a description is a valid card; the check reports the rest
	problems := map[string]bool{}
	rpt, _ := designkit.Check(k.Dir)
	for _, p := range rpt.Problems {
		problems[p.Rule+" "+p.Message] = true
		if p.Level == designkit.LevelError && !strings.Contains(p.Message+p.File, "TextField") {
			t.Errorf("an error besides the undescribed component: %+v", p)
		}
	}
	found := false
	for k2 := range problems {
		if strings.HasPrefix(k2, "component.todo") && strings.Contains(k2, "Button") {
			found = true
		}
	}
	if !found {
		t.Errorf("kit check warns while a draft has a todo: %v", problems)
	}
	card, err := os.ReadFile(filepath.Join(k.Dir, "components", "Button", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, "Button card", string(card), "# Button\n\nThe main way to act on a page", "| `variant` | primary · secondary |", "component-from-global-scope=\"Acme.Button\"")
	// a second import adds nothing and says why
	rep2, err := k.Import("storybook", filepath.Join("testdata", "storybook"), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, "second import", rep2.Lines(), "already has a component with this id", "0 components as drafts")
}

func TestStorybookStoriesJSONGivesPropsVariantsAndASample(t *testing.T) {
	k := newBareKit(t)
	if _, err := k.Import("storybook", filepath.Join("testdata", "storybook-v3", "stories.json"), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Groups []struct {
			ID         string
			Components []struct {
				ID          string
				Props       []designkit.Prop
				Variants    []designkit.Variant
				Sample      map[string]any
				Todo        []string
				Description string
			}
		}
	}
	raw, _ := os.ReadFile(filepath.Join(k.Dir, "design-system.json"))
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var btn *struct {
		ID          string
		Props       []designkit.Prop
		Variants    []designkit.Variant
		Sample      map[string]any
		Todo        []string
		Description string
	}
	for gi := range doc.Groups {
		for ci := range doc.Groups[gi].Components {
			if doc.Groups[gi].Components[ci].ID == "Button" {
				btn = &doc.Groups[gi].Components[ci]
			}
		}
	}
	if btn == nil {
		t.Fatal("no Button")
	}
	props := map[string]designkit.Prop{}
	for _, p := range btn.Props {
		props[p.Name] = p
	}
	if p := props["variant"]; p.Type != "enum" || strings.Join(p.Values, ",") != "primary,secondary,ghost" || p.Description != "visual weight" {
		t.Errorf("variant: %+v", p)
	}
	if p := props["tone"]; p.Type != "enum" || strings.Join(p.Values, ",") != "calm,loud" {
		t.Errorf("an argType with a radio control and options is an enum: %+v", p)
	}
	if p := props["children"]; !p.Required || p.Type != "node" {
		t.Errorf("children: %+v", p)
	}
	if p := props["onClick"]; p.Type != "function" {
		t.Errorf("onClick: %+v", p)
	}
	if _, has := props["internal"]; has {
		t.Error("a prop hidden from Storybook's table is not a prop")
	}
	if len(btn.Variants) != 2 || btn.Variants[0].ID != "primary" || btn.Variants[1].Props["children"] != "Cancel" {
		t.Errorf("variants: %+v", btn.Variants)
	}
	if btn.Sample["variant"] != "primary" || btn.Sample["children"] != "Save" || btn.Sample["disabled"] != false {
		t.Errorf("sample: %+v", btn.Sample)
	}
	if _, has := btn.Sample["size"]; has {
		t.Error("an arg the component does not declare is not in the sample")
	}
	for _, p := range checkErrors(t, k.Dir) {
		if p.Rule != "component.description" && p.Rule != "card.summary" {
			t.Errorf("an error beyond the missing description: %+v", p)
		}
	}
}

// ---- Figma ----------------------------------------------------------------------------------------------------------

func TestFigmaVariables(t *testing.T) {
	raw := read(t, "figma-variables.json")
	if !IsFigmaVariables(raw) || IsFigmaVariables(read(t, "foreign.dtcg.json")) {
		t.Fatal("a Figma export is recognised by meta.variables, nothing else is")
	}
	tk, rep, err := ImportFigma(raw, DTCGOptions{})
	if err != nil {
		t.Fatal(err)
	}
	page := tokenOf(t, tk, "color-surface-page")
	if page.Value.For("light") != "#f6f7f9" || page.Value.For("dark") != "#0f1115" || page.Usage != "Page background behind cards." {
		t.Errorf("page: %+v", page)
	}
	if v := tokenOf(t, tk, "color-link"); v.Value.For("dark") != "{color-accent}" {
		t.Errorf("alias: %+v", v)
	}
	if v := tokenOf(t, tk, "color-scrim"); v.Value.For("light") != "rgb(0 0 0 / 0.5)" || v.Value.For("dark") != "rgb(0 0 0 / 0.7)" {
		t.Errorf("alpha: %+v", v)
	}
	if v, fam := tk.Find("space-4"); fam != "spacing" || v.Value.Plain != "16px" {
		t.Errorf("space-4: %+v %s", v, fam)
	}
	if v, fam := tk.Find("radius-m"); fam != "radius" || v.Value.Plain != "8px" {
		t.Errorf("radius-m: %+v %s", v, fam)
	}
	if v, ok := tk.Type.Family("sans"); !ok || v != "Inter, system-ui, sans-serif" {
		t.Errorf("font: %+v", tk.Type)
	}
	if got := tk.family("color").Themes; len(got) != 2 || got[0].ID != "light" || got[1].ID != "dark" {
		t.Errorf("themes: %+v", got)
	}
	lines := rep.Lines()
	mustContain(t, "report", lines, "boolean variables", "float variables", "scopes", "effects (shadows)")
	// it merges into a kit and the kit still checks
	k := newKit(t, nil)
	src := filepath.Join(t.TempDir(), "figma.json")
	if err := os.WriteFile(src, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Import("figma", src, ImportOptions{CSS: true}); err != nil {
		t.Fatal(err)
	}
	if errs := checkErrors(t, k.Dir); len(errs) != 0 {
		t.Errorf("errors after the import: %+v", errs)
	}
	css, _ := os.ReadFile(filepath.Join(k.Dir, "tokens.css"))
	mustContain(t, "tokens.css", string(css), "--acme-ui-color-surface-page: #f6f7f9;")
}

// ---- into a kit, always checked -------------------------------------------------------------------------------------

func TestEveryTokenImportLeavesAKitThatChecks(t *testing.T) {
	for _, c := range []struct{ format, file string }{
		{"dtcg", "foreign.dtcg.json"}, {"css", "vars.css"}, {"design-md", "design.md"}, {"figma", "figma-variables.json"},
	} {
		t.Run(c.format, func(t *testing.T) {
			k := newKit(t, nil)
			if _, err := k.Import(c.format, filepath.Join("testdata", c.file), ImportOptions{}); err != nil {
				t.Fatal(err)
			}
			if errs := checkErrors(t, k.Dir); len(errs) != 0 {
				t.Errorf("kit check errors after %s: %+v", c.format, errs)
			}
		})
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	k := newKit(t, nil)
	before, _ := os.ReadFile(filepath.Join(k.Dir, "tokens.json"))
	if _, err := k.Import("css", filepath.Join("testdata", "vars.css"), ImportOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Import("storybook", filepath.Join("testdata", "storybook"), ImportOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(k.Dir, "tokens.json"))
	if string(before) != string(after) {
		t.Error("tokens.json changed on a dry run")
	}
	if _, err := os.Stat(filepath.Join(k.Dir, "components", "Tooltip")); err == nil {
		t.Error("a card was written on a dry run")
	}
}

func TestExportsAreJSONOrCheckedText(t *testing.T) {
	k := newKit(t, read(t, "tokens.rich.json"))
	for _, f := range []string{"dtcg", "css", "tailwind", "tailwind4", "design-md"} {
		out, rep, err := k.Export(f)
		if err != nil || len(out) == 0 || rep == nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, _, err := k.Export("sketch"); err == nil {
		t.Error("an unknown format is refused")
	}
}

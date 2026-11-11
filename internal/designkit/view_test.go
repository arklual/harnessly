package designkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installFixture puts the fixture kit where the store looks: <dir>/acme-ui/1.0.0.
func installFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := copyTree(t, "testdata/kit/acme-ui")
	if err := os.MkdirAll(filepath.Join(dir, "acme-ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, filepath.Join(dir, "acme-ui", "1.0.0")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func fixtureCanvas(t *testing.T) (string, *Canvas) {
	t.Helper()
	dir := installFixture(t)
	c, err := ReadCanvas(dir, "acme-ui", "1.0.0", "")
	if err != nil {
		t.Fatal(err)
	}
	return dir, c
}

func TestFixtureKitPassesItsOwnCheck(t *testing.T) {
	rep := check(t, "testdata/kit/acme-ui")
	if rep.Errors() != 0 {
		t.Fatalf("the fixture kit has errors: %+v", rep.Problems)
	}
}

func TestIndexOfFixtureNamesGroupsPartsAndStatus(t *testing.T) {
	_, c := fixtureCanvas(t)
	v := IndexOf(c)
	if v.Schema != ViewSchema || v.Kit != "acme-ui" || v.Root != "Root" {
		t.Fatalf("header: %+v", v)
	}
	var lines []string
	for _, g := range v.Components {
		lines = append(lines, g.Items...)
	}
	all := strings.Join(lines, "\n")
	for _, want := range []string{"Card +2 — A raised block", "Button — The main way", "Root [provider]", "Tag [deprecated → Card]"} {
		if !strings.Contains(all, want) {
			t.Errorf("index lacks %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "CardHeader") {
		t.Errorf("a part is counted under its parent, not listed:\n%s", all)
	}
	if v.Tokens["color"] != 2 || v.Tokens["spacing"] != 1 || v.Tokens["total"] != 5 {
		t.Errorf("token counts: %v", v.Tokens)
	}
	if len(v.Templates) != 1 || v.Templates[0].Layouts != 1 {
		t.Errorf("templates: %+v", v.Templates)
	}
}

// fiftyComponents is a canvas of fifty components with descriptions as long as real ones.
func fiftyComponents() *Canvas {
	c := &Canvas{Kit: "big", Version: "1.0.0", Theme: "default", Title: "Big", Namespace: "Big"}
	g := ComponentGroup{ID: "all", Title: "All"}
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("Component%02dName", i)
		g.Components = append(g.Components, Component{ID: id, Name: id,
			Description: "A control that lets a person choose one thing out of many and sends it to the form that holds it."})
	}
	c.Groups = []ComponentGroup{g}
	return c
}

func TestIndexOfFiftyComponentsFitsTheBudget(t *testing.T) {
	v := IndexOf(fiftyComponents())
	size := len(MarshalCompact(v))
	if size > IndexBudget {
		t.Fatalf("the index of 50 components is %d bytes, the aim is %d", size, IndexBudget)
	}
	n := 0
	for _, g := range v.Components {
		n += len(g.Items)
	}
	if n != 50 {
		t.Fatalf("all 50 components stay listed, got %d", n)
	}
}

func TestIndexKeepsWholeDescriptionsForASmallKit(t *testing.T) {
	_, c := fixtureCanvas(t)
	all := ""
	for _, g := range IndexOf(c).Components {
		all += strings.Join(g.Items, "\n")
	}
	if !strings.Contains(all, "submits a form or starts a flow.") {
		t.Fatalf("a small kit keeps its descriptions whole:\n%s", all)
	}
}

func TestComponentOfCardManifestAndComputedFields(t *testing.T) {
	dir, c := fixtureCanvas(t)
	v := ComponentOf(dir, c, "Button")
	if v == nil {
		t.Fatal("no Button")
	}
	if v.Required[0] != "children" || strings.Join(v.Optional, ",") != "variant,disabled" {
		t.Errorf("required %v optional %v", v.Required, v.Optional)
	}
	if v.Code == nil || v.Code.Import != "@acme/ui" || v.Code.Export != "Button" || v.Status != "stable" {
		t.Errorf("code/status: %+v %q", v.Code, v.Status)
	}
	if v.SampleJSX != `<Button variant="primary">Save</Button>` {
		t.Errorf("sample JSX: %s", v.SampleJSX)
	}
	if !v.Card.Found || len(v.Card.WhenToUse) != 2 || len(v.Card.WhenNotToUse) != 1 || len(v.Card.Examples) != 1 {
		t.Errorf("card: %+v", v.Card)
	}
	if v.Card.InCode != "import { Button } from '@acme/ui';" || v.Card.Status != "Stable since 1.0." || len(v.Card.Accessibility) != 1 || len(v.Card.States) != 1 || len(v.Card.Content) != 1 {
		t.Errorf("optional sections: %+v", v.Card)
	}
	if len(v.Presets) != 1 || v.Presets[0].ID != "PrimaryButton" || v.Presets[0].Props["variant"] != "primary" {
		t.Errorf("presets: %+v", v.Presets)
	}
	// Used with is computed: the layout's board names Acme.Button; Card's sample holds no Button.
	if strings.Join(v.UsedWith.Layouts, ",") != "pages/record-page" || len(v.UsedWith.Components) != 0 {
		t.Errorf("used with: %+v", v.UsedWith)
	}
	if v.Variants[0].ID != "secondary" {
		t.Errorf("variants: %+v", v.Variants)
	}
}

func TestComponentOfCompoundShowsPartsAndNestedJSX(t *testing.T) {
	dir, c := fixtureCanvas(t)
	v := ComponentOf(dir, c, "Card")
	if len(v.Parts) != 2 || !v.Parts[0].InSample || v.Parts[0].ID != "CardHeader" {
		t.Errorf("parts: %+v", v.Parts)
	}
	want := `<Card><CardHeader>Spring concert</CardHeader><CardBody>412 tickets sold</CardBody></Card>`
	if v.SampleJSX != want {
		t.Errorf("JSX:\n got %s\nwant %s", v.SampleJSX, want)
	}
	if strings.Join(v.UsedWith.Components, ",") != "CardBody,CardHeader" || strings.Join(v.UsedWith.Layouts, ",") != "pages/record-page" {
		t.Errorf("used with: %+v", v.UsedWith)
	}
	part := ComponentOf(dir, c, "CardHeader")
	if part.Part != "Card" || part.UsedWith.Components[0] != "Card" {
		t.Errorf("part: %+v", part)
	}
}

func TestComponentOfPresetTakesItsTargetsCardAndProps(t *testing.T) {
	dir, c := fixtureCanvas(t)
	v := ComponentOf(dir, c, "PrimaryButton")
	if v.Preset == nil || v.Preset.Of != "Button" || v.Props == nil || len(v.Props) != 3 {
		t.Fatalf("preset: %+v props %v", v.Preset, v.Props)
	}
	if v.SampleJSX != `<PrimaryButton variant="primary">Save</PrimaryButton>` {
		t.Errorf("a preset is drawn with its target's sample and its own props: %s", v.SampleJSX)
	}
	if ComponentOf(dir, c, "Nope") != nil {
		t.Error("an unknown component is nil")
	}
}

func TestSearchTokensTemplatesAndCheckViews(t *testing.T) {
	dir, c := fixtureCanvas(t)
	s := SearchOf(c, "submit", 5)
	if len(s.Results) == 0 || s.Results[0].Ref != "Button" || s.Results[0].Kind != KindComponent {
		t.Fatalf("search: %+v", s.Results)
	}
	if l := SearchOf(c, "record", 5); len(l.Results) == 0 || l.Results[0].Ref != "record-page" {
		t.Fatalf("layout search: %+v", l.Results)
	}

	all := TokensOf(c, "")
	if all.Prefix != "--acme-" || len(all.Groups["color"]) != 2 {
		t.Fatalf("tokens: %+v", all)
	}
	if got := all.Groups["color"][0].Var; got != "--acme-surface-1" {
		t.Errorf("explicit var: %s", got)
	}
	if got := all.Groups["color"][1].Var; got != "--acme-accent" {
		t.Errorf("var from the prefix: %s", got)
	}
	if got := TokensOf(c, "spacing").Groups; len(got) != 1 || got["spacing"][0].Value != "8px" {
		t.Errorf("one family: %+v", got)
	}
	if strings.Join(TokenGroups(c), ",") != "color,radius,spacing,type" {
		t.Errorf("groups: %v", TokenGroups(c))
	}

	tl := TemplatesOf(c, "")
	if len(tl.Templates) != 1 || tl.Templates[0].Layouts[0].Purpose == "" {
		t.Fatalf("templates: %+v", tl)
	}
	tv := TemplateOf(dir, c, "record-page")
	if tv == nil || tv.Template != "pages" || len(tv.Slots) != 1 || !strings.Contains(tv.Source, "Acme.Card") {
		t.Fatalf("template: %+v", tv)
	}
	if TemplateOf(dir, c, "pages") == nil || TemplateOf(dir, c, "nope") != nil {
		t.Error("a template's id gives its first layout; an unknown id nothing")
	}

	rep, err := Check(Resolve(dir, "acme-ui", "1.0.0", nil))
	if err != nil {
		t.Fatal(err)
	}
	cv := CheckOf("acme-ui", "1.0.0", rep)
	if !cv.Ok || cv.Components != 7 || cv.Errors != 0 || cv.Schema != ViewSchema {
		t.Errorf("check: %+v", cv)
	}
	total := 0
	for _, n := range cv.Levels {
		total += n
	}
	if total == 0 {
		t.Errorf("levels: %+v", cv.Levels)
	}
}

func TestParseCardKeepsPlainTextAndSkipsFencedHeadings(t *testing.T) {
	v := ParseCard("# X\n\nSummary here.\n\n## When to use\nA paragraph, not a list.\n\n## Example\n```html\n<x-import component-from-global-scope=\"A.X\"></x-import>\n\n## not a heading\n```\n")
	if v.Summary != "Summary here." || len(v.WhenToUse) != 1 || v.WhenToUse[0] != "A paragraph, not a list." {
		t.Errorf("%+v", v)
	}
	if len(v.Examples) != 1 || !strings.Contains(v.Examples[0], "## not a heading") {
		t.Errorf("a heading inside a fence stays in the example: %+v", v.Examples)
	}
}

func TestJSXWritesValuesElementsAndFunctions(t *testing.T) {
	got := JSX("Table", map[string]any{"dense": true, "rowKey": map[string]any{"$fn": "id"}, "title": `say "hi"`,
		"rows": []any{map[string]any{"id": 1.0}}, "children": map[string]any{"$c": "Cell", "props": map[string]any{"tone": "x"}, "children": "A < B"}})
	want := `<Table dense={true} rowKey={(row) => row.id} rows={[{"id": 1}]} title={"say \"hi\""}><Cell tone="x">{"A < B"}</Cell></Table>`
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

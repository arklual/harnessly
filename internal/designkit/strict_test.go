package designkit

import (
	"strings"
	"testing"
)

// strictCanvas is a made-up kit: a root, a button with an enum and a token prop, a stack, a deprecated badge that a
// tag replaces, a preset, colour and spacing tokens.
func strictCanvas() *Canvas {
	code := func(exp string) *Code { return &Code{Import: "@acme/ui", Export: exp} }
	return &Canvas{Kit: "acme-ui", Version: "1.0.0", Namespace: "Acme", Package: "@acme/ui", TokenPrefix: "--acme-",
		Colors: []ColorToken{{Name: "primary", Values: map[string]string{"light": "#2563EB", "dark": "#3b82f6"}}},
		Tokens: []PlainToken{{Kind: "spacing", Name: "space-4", Value: "16px"}},
		Groups: []ComponentGroup{{ID: "g", Components: []Component{
			{ID: "Root", Code: code("Root"), Props: []Prop{{Name: "children", Type: "node"}}},
			{ID: "Button", Code: code("Button"), Props: []Prop{{Name: "variant", Type: "enum", Values: []string{"primary", "secondary"}},
				{Name: "tone", Type: "color"}, {Name: "onClick", Type: "function"}, {Name: "children", Type: "node"}}},
			{ID: "Stack", Code: code("Stack"), Props: []Prop{{Name: "gap", Type: "token"}, {Name: "children", Type: "node"}}},
			{ID: "Badge", Code: code("Badge"), Status: "deprecated", ReplacedBy: "Tag", Props: []Prop{{Name: "children", Type: "node"}}},
			{ID: "Tag", Code: code("Tag"), Props: []Prop{{Name: "children", Type: "node"}}},
			{ID: "PrimaryButton", Code: code("Button"), Preset: &Preset{Of: "Button", Props: map[string]any{"variant": "primary"}}},
		}}}}
}

func rulesOf(fs []StrictFinding) map[string]int {
	m := map[string]int{}
	for _, f := range fs {
		m[f.Rule]++
	}
	return m
}

const cleanPage = `import { Root, Button, Stack } from "@acme/ui";
import { Card } from "../components/card";
import { events } from "./home.fixtures";

export default function Home() {
  const big = events.length > 2 && events.length < 10;
  return (
    <Root>
      <Stack gap="space-4">
        {events.map((e) => <Card key={e.id} title={e.name} />)}
        {big ? <Button variant="primary" tone="primary" onClick={() => {}}>More</Button> : null}
        <>
          <Button aria-label="Close" data-test="x">×</Button>
        </>
      </Stack>
    </Root>
  );
}
`

func TestStrictPassesAPageMadeOfTheKit(t *testing.T) {
	k := NewStrictKit(strictCanvas())
	if fs := k.CheckModule("pages/home.tsx", cleanPage, false); len(fs) != 0 {
		t.Fatalf("a clean page has findings: %v", fs)
	}
}

// Each of the four cases fails the check, says where, and says what to put instead.
func TestStrictCatchesEachCaseInAPage(t *testing.T) {
	k := NewStrictKit(strictCanvas())
	cases := []struct {
		name, src, rule, where, fix string
	}{
		{"element: plain HTML", `import { Root } from "@acme/ui";
export default () => <Root><div>x</div></Root>;`, "strict.element", "pages/p.tsx:2:29", "Stack"},
		{"element: written in the page", `import { Root } from "@acme/ui";
const Box = () => null;
export default () => <Root><Box /></Root>;`, "strict.element", "pages/p.tsx:3:29", "components/Box.tsx"},
		{"element: another package", `import { Root } from "@acme/ui";
import { Chart } from "chart-lib";
export default () => <Root><Chart /></Root>;`, "strict.element", "pages/p.tsx:3:29", "kit_search"},
		{"element: not exported by the kit", `import { Root, Buton } from "@acme/ui";
export default () => <Root><Buton /></Root>;`, "strict.element", "pages/p.tsx:2:29", "did you mean Button"},
		{"value: raw colour in a style", `import { Root } from "@acme/ui";
import { Card } from "../components/card";
export default () => <Root><Card style={{ color: "#2563eb" }} /></Root>;`, "strict.value", "pages/p.tsx:3:51", "var(--acme-primary)"},
		{"value: a token prop given a raw value", `import { Root, Button } from "@acme/ui";
export default () => <Root><Button tone="#2563eb">x</Button></Root>;`, "strict.value", "pages/p.tsx:2:42", "the token primary"},
		{"value: a variable the kit lacks", `import { Root } from "@acme/ui";
import { Card } from "../components/card";
export default () => <Root><Card accent="var(--acme-primry)" /></Root>;`, "strict.value", "pages/p.tsx:3:46", "--acme-primary"},
		{"prop: undeclared", `import { Root, Button } from "@acme/ui";
export default () => <Root><Button variantt="primary">x</Button></Root>;`, "strict.prop", "pages/p.tsx:2:36", "did you mean variant"},
		{"prop: style on a kit component", `import { Root, Button } from "@acme/ui";
export default () => <Root><Button style={{}}>x</Button></Root>;`, "strict.prop", "pages/p.tsx:2:36", "local component"},
		{"prop: enum value", `import { Root, Button } from "@acme/ui";
export default () => <Root><Button variant="danger">x</Button></Root>;`, "strict.prop", "pages/p.tsx:2:45", "primary, secondary"},
		{"deprecated", `import { Root, Badge } from "@acme/ui";
export default () => <Root><Badge>new</Badge></Root>;`, "strict.deprecated", "pages/p.tsx:2:29", `import { Tag } from "@acme/ui"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := k.CheckModule("pages/p.tsx", c.src, false)
			if len(fs) != 1 {
				t.Fatalf("want one finding, got %v", fs)
			}
			f := fs[0]
			line := f.String()
			if f.Rule != c.rule || !strings.HasPrefix(line, c.where+" ") || !strings.Contains(f.Fix, c.fix) {
				t.Errorf("want %s at %s with a fix naming %q, got %s", c.rule, c.where, c.fix, line)
			}
		})
	}
}

// A local component may be written in HTML; its values are still the kit's tokens, and a kit component inside it is
// still checked.
func TestStrictLocalComponents(t *testing.T) {
	k := NewStrictKit(strictCanvas())
	ok := `import { Button } from "@acme/ui";
export const Card = ({ title }: { title: string }) => (
  <section className="card" style={{ padding: "var(--acme-space-4)", margin: 0 }}>
    <h2>{title}</h2><Button variant="secondary">Open</Button>
  </section>
);`
	if fs := k.CheckModule("components/card.tsx", ok, true); len(fs) != 0 {
		t.Fatalf("a local component made of HTML and tokens has findings: %v", fs)
	}
	bad := `export const Card = () => <section style={{ padding: 12, color: "rgb(0, 0, 0)" }} />;`
	got := rulesOf(k.CheckModule("components/card.tsx", bad, true))
	if got["strict.value"] != 2 || len(got) != 1 {
		t.Fatalf("want two raw values, got %v", got)
	}
}

func TestStrictReadsTypesAndComparisonsAsCode(t *testing.T) {
	k := NewStrictKit(strictCanvas())
	src := `import { Root, Button } from "@acme/ui";
type Row = Array<{ id: number }>;
function pick<T>(a: T[], n: number): T | undefined { return n < a.length ? a[n] : undefined; }
const re = /<div>/g;
const s = "<div>" + ` + "`<span>${1 < 2}</span>`" + `;
// <div> in a comment
export default function P(): JSX.Element {
  return <Root>{[1, 2].map((n) => n > 1 && <Button key={n}>{n}</Button>)}</Root>;
}`
	if fs := k.CheckModule("pages/p.tsx", src, false); len(fs) != 0 {
		t.Fatalf("code read as elements: %v", fs)
	}
}

const strictBoard = `<!doctype html>
<html><head>
<style>html,body{margin:0;background:var(--acme-primary)}</style>
<script src="ds/acme/components/bundle.js"></script>
</head><body>
<x-dc>
<helmet><style>.x{gap:0}</style></helmet>
<main class="board">
<x-import component-from-global-scope="Acme.Root">
  <x-import component-from-global-scope="Acme.Button" variant="primary" on-click="{{go}}">Go</x-import>
  <x-import component="Card" from="components/card.jsx"></x-import>
</x-import>
</main>
</x-dc>
<script type="text/x-dc" data-dc-script>
class Component extends DCLogic { renderVals() { const { Tag } = window.Acme; return { go: () => {}, t: React.createElement(Tag, null, 'x') }; } }
</script>
</body></html>
`

func TestStrictPassesABoardMadeOfTheKit(t *testing.T) {
	k := NewStrictKit(strictCanvas())
	local := func(from string) bool { return from == "components/card.jsx" }
	if fs := k.CheckBoard("home.dc.html", strictBoard, local); len(fs) != 0 {
		t.Fatalf("a clean board has findings: %v", fs)
	}
}

func TestStrictCatchesEachCaseInABoard(t *testing.T) {
	k := NewStrictKit(strictCanvas())
	local := func(string) bool { return false }
	cases := []struct {
		name, insert, rule string
		line               int
	}{
		{"element: plain HTML", `<div>hi</div>`, "strict.element", 11},
		{"element: not in the kit", `<x-import component-from-global-scope="Acme.Chart"></x-import>`, "strict.element", 11},
		{"element: a foreign file", `<x-import component="X" from="../other/x.jsx"></x-import>`, "strict.element", 11},
		{"value: a style attribute", `<x-import component-from-global-scope="Acme.Tag" style="color:#fff"></x-import>`, "strict.value", 11},
		{"prop: undeclared", `<x-import component-from-global-scope="Acme.Button" size="l">x</x-import>`, "strict.prop", 11},
		{"deprecated", `<x-import component-from-global-scope="Acme.Badge">x</x-import>`, "strict.deprecated", 11},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := strings.Replace(strictBoard, `  <x-import component="Card" from="components/card.jsx"></x-import>`, c.insert, 1)
			fs := k.CheckBoard("home.dc.html", src, local)
			got := rulesOf(fs)
			if got[c.rule] == 0 {
				t.Fatalf("want %s, got %v", c.rule, fs)
			}
			for _, f := range fs {
				if f.Rule == c.rule && (f.Line != c.line || f.Fix == "") {
					t.Errorf("want line %d and a fix, got %s", c.line, f)
				}
			}
		})
	}
	src := strings.Replace(strictBoard, "'x'", "React.createElement('div', { style: { color: '#123456' } })", 1)
	got := rulesOf(k.CheckBoard("home.dc.html", src, func(string) bool { return true }))
	if got["strict.element"] != 1 || got["strict.value"] != 1 {
		t.Fatalf("renderVals: want one element and one value, got %v", got)
	}
}

// A source nested past what the reader follows, or full of `<` that open nothing, is read in bounded time and stack.
func TestStrictReaderIsBounded(t *testing.T) {
	k := NewStrictKit(strictCanvas())
	deep := `import { Root } from "@acme/ui";
export default () => <Root>` + strings.Repeat("<Root>", 5000) + strings.Repeat("</Root>", 5000) + `</Root>;`
	_ = k.CheckModule("pages/p.tsx", deep, false)
	braces := "export default () => (" + strings.Repeat("{", 20000) + strings.Repeat("}", 20000) + ");"
	_ = k.CheckModule("pages/p.tsx", braces, false)
	junk := "const x = (" + strings.Repeat("< a ", 20000) + ");"
	_ = k.CheckModule("pages/p.tsx", junk, false)
}

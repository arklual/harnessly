package kitadd

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// The tests make kits from packages on disk: nothing is installed and nothing is fetched. The types' reader (node with
// typescript) is replaced by what it would answer for the fixture, so the tests check what the command does with it.

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// acmePackage is @acme/ui at a version: 1.0.0 has Button (variant, tone), Card with its part CardHeader, Tag and a
// hook; 2.0.0 drops Tag and Button's tone, adds Badge, a ghost variant and Button's size.
func acmePackage(t *testing.T, version string) (dir string, analysis Analysis) {
	dir = t.TempDir()
	v2 := version == "2.0.0"
	js := `import { jsx } from "react/jsx-runtime";
export function Button(p) { return jsx("button", { className: "acme-button acme-" + (p.variant || "primary"), children: p.children }); }
export function Card(p) { return jsx("div", { className: "acme-card", children: p.children }); }
export function CardHeader(p) { return jsx("div", { className: "acme-card-header", children: p.title }); }
export function useAcme() { return 1; }
`
	if v2 {
		js += "export function Badge(p) { return jsx(\"span\", { className: \"acme-badge\", children: p.children }); }\n"
	} else {
		js += "export function Tag(p) { return jsx(\"span\", { children: p.label }); }\n"
	}
	css := `:root { --acme-primary: #1d4ed8; --acme-radius: 8px; }
[data-theme="dark"] { --acme-primary: #93c5fd; }
.acme-button { color: var(--acme-primary); border-radius: var(--acme-radius); }
body { margin: 0; }
`
	writeFiles(t, dir, map[string]string{
		"node_modules/@acme/ui/package.json": `{"name":"@acme/ui","version":"` + version + `","module":"index.js","main":"index.js","style":"styles.css"}`,
		"node_modules/@acme/ui/index.js":     js,
		"node_modules/@acme/ui/styles.css":   css,
	})
	variant := TypeProp{Name: "variant", Type: "enum", Values: []string{"primary", "secondary"}}
	button := []TypeProp{{Name: "children", Type: "node", Required: true}, variant, {Name: "tone", Type: "string"}}
	if v2 {
		variant.Values = append(variant.Values, "ghost")
		button = []TypeProp{{Name: "children", Type: "node", Required: true}, variant, {Name: "size", Type: "enum", Values: []string{"s", "m"}}}
	}
	analysis = Analysis{Exports: map[string]Export{
		"Button":     {Kind: "component", Description: "The main way to act on a page.", Props: button, File: "button.d.ts"},
		"Card":       {Kind: "component", Props: []TypeProp{{Name: "children", Type: "node"}}, File: "card.d.ts", Pos: 1},
		"CardHeader": {Kind: "component", Props: []TypeProp{{Name: "title", Type: "string", Required: true, Description: "the card's title"}}, File: "card.d.ts", Pos: 2},
		"useAcme":    {Kind: "other", File: "hooks.d.ts"},
	}}
	if v2 {
		analysis.Exports["Badge"] = Export{Kind: "component", Props: []TypeProp{{Name: "children", Type: "node"}}, File: "badge.d.ts"}
	} else {
		analysis.Exports["Tag"] = Export{Kind: "component", Props: []TypeProp{{Name: "label", Type: "string", Required: true}}, File: "tag.d.ts"}
	}
	return dir, analysis
}

// fakeNode answers for node: the analysis for the types' reader, a stylesheet for Tailwind's command line.
func fakeNode(a Analysis) func(dir string, args ...string) ([]byte, error) {
	return func(dir string, args ...string) ([]byte, error) {
		if strings.HasSuffix(args[0], ".kit-analyze.mjs") {
			return json.Marshal(a)
		}
		for i, x := range args {
			if x == "-o" {
				return nil, os.WriteFile(args[i+1], []byte(".bg-primary{background:var(--primary)}\n"), 0o644)
			}
		}
		return nil, nil
	}
}

func npmOptions(t *testing.T, folder, version string) Options {
	from, a := acmePackage(t, version)
	src, err := ParseSource("npm:@acme/ui@" + version)
	if err != nil {
		t.Fatal(err)
	}
	return Options{Source: src, Folder: folder, From: from, Node: fakeNode(a)}
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func checkClean(t *testing.T, folder string) *designkit.CheckReport {
	t.Helper()
	rep, err := designkit.Check(folder)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range rep.Problems {
		if p.Level == designkit.LevelError {
			t.Errorf("check: %s %s: %s", p.Rule, p.File, p.Message)
		}
	}
	return rep
}

func TestParseSource(t *testing.T) {
	for in, want := range map[string]Source{
		"npm:@acme/ui":       {Kind: "npm", Package: "@acme/ui"},
		"npm:@acme/ui@1.2.3": {Kind: "npm", Package: "@acme/ui", Version: "1.2.3"},
		"npm:widgets@^2":     {Kind: "npm", Package: "widgets", Version: "^2"},
		"shadcn:ui":          {Kind: "shadcn"},
		"shadcn:./mirror/r":  {Kind: "shadcn", Base: "./mirror/r"},
	} {
		got, err := ParseSource(in)
		if err != nil || got != want {
			t.Errorf("%s: %+v %v, want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"@acme/ui", "npm:Bad Name", "pip:requests"} {
		if _, err := ParseSource(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	if DefaultID(Source{Kind: "npm", Package: "@acme/ui"}) != "acme-ui" || defaultNamespace("acme-ui") != "AcmeUi" {
		t.Error("default id or namespace")
	}
}

func TestAddNpmMakesAKitThatPassesTheCheck(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "acme-ui")
	rep, err := Add(npmOptions(t, folder, "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	checkClean(t, folder)
	if rep.Version != "1.0.0" || rep.Components != 3 || rep.Parts != 1 {
		t.Errorf("report: %+v", rep)
	}
	files := readTree(t, folder)
	js := files["components/bundle.js"]
	if !strings.Contains(js, `AcmeUi`) || !strings.Contains(js, "window.React") || strings.Contains(js, "__SECRET_INTERNALS") {
		t.Errorf("bundle: React must come from the page and the exports go to window.AcmeUi:\n%.400s", js)
	}
	css := files["components/bundle.css"]
	if !strings.HasPrefix(css, "@layer kit-acme-ui {") || !strings.Contains(css, ".acme-button") {
		t.Errorf("bundle.css is not in the kit's layer:\n%s", css)
	}
	for _, want := range []string{`"name": "acme-primary"`, `"light": "#1d4ed8"`, `"dark": "#93c5fd"`} {
		if !strings.Contains(files["tokens.json"], want) {
			t.Errorf("tokens.json lacks %s:\n%s", want, files["tokens.json"])
		}
	}
	if !strings.Contains(files["tokens.css"], "--acme-primary: #93c5fd") {
		t.Errorf("tokens.css:\n%s", files["tokens.css"])
	}
	var d struct {
		Kit    map[string]any `json:"kit"`
		Groups []struct {
			Components []map[string]any `json:"components"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(files["design-system.json"]), &d); err != nil {
		t.Fatal(err)
	}
	if d.Kit["version"] != "1.0.0" || d.Kit["package"] != "@acme/ui" {
		t.Errorf("kit record: %v", d.Kit)
	}
	comps := map[string]map[string]any{}
	for _, c := range d.Groups[0].Components {
		comps[c["id"].(string)] = c
	}
	if comps["CardHeader"]["part"] != "Card" {
		t.Errorf("CardHeader is not Card's part: %v", comps["CardHeader"])
	}
	if code, _ := comps["Button"]["code"].(map[string]any); code["import"] != "@acme/ui" || code["export"] != "Button" {
		t.Errorf("Button's code: %v", comps["Button"]["code"])
	}
	// Card's sample draws its part with a value for the part's required prop.
	if s, _ := json.Marshal(comps["Card"]["sample"]); !strings.Contains(string(s), `"$c":"CardHeader"`) || !strings.Contains(string(s), `"title":"Title"`) {
		t.Errorf("Card's sample: %s", s)
	}
	if !strings.Contains(files["README.md"], "`useAcme`") || !strings.Contains(files["README.md"], "## Consuming") {
		t.Errorf("README:\n%s", files["README.md"])
	}
	if !strings.Contains(files["components/Button/README.md"], "import { Button } from '@acme/ui';") {
		t.Errorf("Button's card:\n%s", files["components/Button/README.md"])
	}
}

func TestAddTwiceOnTheSameVersionWritesTheSameFiles(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "acme-ui")
	if _, err := Add(npmOptions(t, folder, "1.0.0")); err != nil {
		t.Fatal(err)
	}
	first := readTree(t, folder)
	rep, err := Add(npmOptions(t, folder, "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if again := readTree(t, folder); !reflect.DeepEqual(first, again) {
		for k, v := range again {
			if first[k] != v {
				t.Errorf("%s differs on the second run:\n--- first\n%s\n--- second\n%s", k, first[k], v)
			}
		}
		if len(first) != len(again) {
			t.Errorf("%d files, then %d", len(first), len(again))
		}
	}
	if len(rep.Added)+len(rep.Removed)+len(rep.Changed) != 0 {
		t.Errorf("the same version reports changes: %+v", rep)
	}
}

func TestUpdateReportsChangesAndKeepsWhatAPersonWrote(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "acme-ui")
	if _, err := Add(npmOptions(t, folder, "1.0.0")); err != nil {
		t.Fatal(err)
	}
	// A person writes: Card's purpose and keywords, Button's card, a token's usage, a sample with the prop that goes.
	edit := func(rel string, f func(string) string) {
		p := filepath.Join(folder, filepath.FromSlash(rel))
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f(string(raw))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	edit("design-system.json", func(s string) string {
		s = strings.Replace(s, `"description": "Card from @acme/ui; what it is for is not written yet."`,
			`"description": "A raised surface that groups one record.", "keywords": ["panel"]`, 1)
		return strings.Replace(s, `"sample": {
            "children": "Button"
          }`, `"sample": {
            "children": "Save",
            "tone": "calm"
          }`, 1)
	})
	edit("components/Button/README.md", func(s string) string {
		return strings.Replace(s, "## Props", "## When not to use\n\n- Going to another page: a link.\n\n## Props", 1)
	})
	edit("tokens.json", func(s string) string {
		return strings.Replace(s, `"name": "acme-radius",`, `"name": "acme-radius", "usage": "Buttons and cards.",`, 1)
	})
	if rep := checkClean(t, folder); rep == nil {
		return
	}

	opt := npmOptions(t, folder, "2.0.0")
	rep, err := Add(opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Previous != "1.0.0" || rep.Version != "2.0.0" {
		t.Errorf("versions: %s → %s", rep.Previous, rep.Version)
	}
	if !reflect.DeepEqual(rep.Added, []string{"Badge"}) || !reflect.DeepEqual(rep.Removed, []string{"Tag"}) {
		t.Errorf("added %v removed %v", rep.Added, rep.Removed)
	}
	want := PropChange{Component: "Button", Added: []string{"size"}, Removed: []string{"tone"},
		Changed: []string{"variant: enum(primary|secondary) → enum(primary|secondary|ghost)"}}
	if len(rep.Changed) != 1 || !reflect.DeepEqual(rep.Changed[0], want) {
		t.Errorf("changed: %+v", rep.Changed)
	}
	checkClean(t, folder)
	files := readTree(t, folder)
	ds := files["design-system.json"]
	for _, keep := range []string{`"description": "A raised surface that groups one record."`, `"panel"`, `"children": "Save"`} {
		if !strings.Contains(ds, keep) {
			t.Errorf("design-system.json lost %s", keep)
		}
	}
	if strings.Contains(ds, `"tone"`) || strings.Contains(ds, `"Tag"`) || !strings.Contains(ds, `"Badge"`) || !strings.Contains(ds, `"version": "2.0.0"`) {
		t.Errorf("design-system.json does not follow 2.0.0:\n%s", ds)
	}
	card := files["components/Button/README.md"]
	if !strings.Contains(card, "## When not to use\n\n- Going to another page: a link.") || !strings.Contains(card, "`size`") || strings.Contains(card, "`tone`") {
		t.Errorf("Button's card:\n%s", card)
	}
	if !strings.Contains(files["tokens.json"], `"usage": "Buttons and cards."`) {
		t.Errorf("the token's usage is lost:\n%s", files["tokens.json"])
	}
	if _, ok := files["components/Badge/README.md"]; !ok {
		t.Error("no card for the new Badge")
	}
	if !strings.Contains(strings.Join(rep.ToWrite, "\n"), "Tag is gone") {
		t.Errorf("the kept card of Tag is not reported: %v", rep.ToWrite)
	}
	if src, _, err := ReadSource(folder); err != nil || src.String() != "npm:@acme/ui@2.0.0" {
		t.Errorf("source record: %v %v", src, err)
	}
}

func TestSafeNpmSpec(t *testing.T) {
	for _, ok := range []string{"radix-ui", "@acme/ui", "recharts@3.8.0", "react-resizable-panels@^4", "zod@>=3 <4"} {
		if !SafeNpmSpec(ok) {
			t.Errorf("%s refused", ok)
		}
	}
	for _, bad := range []string{"--registry=http://x", "-g", "git+https://x/y.git", "file:../evil", "https://x/y.tgz", "../evil", "a@npm:b", "@acme/ui/../../x"} {
		if SafeNpmSpec(bad) {
			t.Errorf("%s allowed", bad)
		}
	}
}

func TestAddRefusesAFolderItDidNotMake(t *testing.T) {
	folder := t.TempDir()
	if _, err := designkit.Init(folder, designkit.InitOptions{ID: "acme-ui", Namespace: "Acme", Source: "draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(npmOptions(t, folder, "1.0.0")); err == nil || !strings.Contains(err.Error(), "did not make") {
		t.Errorf("err = %v", err)
	}
}

func TestAddShadcnRegistryFromAFolder(t *testing.T) {
	reg := t.TempDir()
	item := func(name, typ, path, content string, deps ...string) string {
		raw, _ := json.Marshal(map[string]any{"name": name, "type": typ, "registryDependencies": deps,
			"files": []map[string]string{{"path": path, "type": typ, "content": content}}})
		return string(raw)
	}
	writeFiles(t, reg, map[string]string{
		"registry.json": `{"name":"acme","items":[{"name":"button","type":"registry:ui"},{"name":"card","type":"registry:ui"},{"name":"utils","type":"registry:lib"}]}`,
		"button.json": item("button", "registry:ui", "registry/acme/ui/button.tsx", `import * as React from "react"
import { cn } from "@/registry/acme/lib/utils"
export function Button({ className, ...props }: React.ComponentProps<"button">) {
  return <button className={cn("bg-primary rounded-md", className)} {...props} />
}
`, "utils"),
		"card.json": item("card", "registry:ui", "registry/acme/ui/card.tsx", `import * as React from "react"
import { Button } from "@/components/ui/button"
export function Card(props: React.ComponentProps<"div">) { return <div className="rounded-lg border" {...props} /> }
export function CardFooter(props: React.ComponentProps<"div">) { return <div {...props}><Button>OK</Button></div> }
`),
		"utils.json": item("utils", "registry:lib", "registry/acme/lib/utils.ts", `export function cn(...a: (string | undefined)[]) { return a.filter(Boolean).join(" ") }
`),
		"style.json": `{"name":"style","type":"registry:style","cssVars":{"light":{"primary":"oklch(0.2 0 0)","radius":"0.5rem"},"dark":{"primary":"oklch(0.9 0 0)"}},"css":{"@layer base":{"body":{"@apply bg-background":{}}}}}`,
	})
	children := []TypeProp{{Name: "children", Type: "node"}}
	a := Analysis{Exports: map[string]Export{
		"Button":     {Kind: "component", Props: children, File: "ui/button.tsx"},
		"Card":       {Kind: "component", Props: children, File: "ui/card.tsx", Pos: 10},
		"CardFooter": {Kind: "component", Props: children, File: "ui/card.tsx", Pos: 90},
		"Root":       {Kind: "component", Props: []TypeProp{{Name: "theme", Type: "enum", Values: []string{"light", "dark"}}, {Name: "children", Type: "node"}}, File: "Root.tsx"},
	}}
	src, _ := ParseSource("shadcn:" + reg)
	folder := filepath.Join(t.TempDir(), "acme")
	work := t.TempDir()
	rep, err := Add(Options{Source: src, Folder: folder, From: work, ID: "acme", Node: fakeNode(a)})
	if err != nil {
		t.Fatal(err)
	}
	checkClean(t, folder)
	if rep.Components != 2 || rep.Parts != 1 || rep.Providers != 1 || rep.Tokens != 2 {
		t.Errorf("report: %+v", rep)
	}
	files := readTree(t, folder)
	if !strings.Contains(files["components/bundle.css"], ".bg-primary{background:var(--primary)}") {
		t.Errorf("Tailwind's output is not in bundle.css:\n%s", files["components/bundle.css"])
	}
	if !strings.Contains(files["design-system.json"], `"import": "@/components/ui/card"`) || !strings.Contains(files["design-system.json"], `"root": "Root"`) {
		t.Errorf("design-system.json:\n%s", files["design-system.json"])
	}
	// The registry's own paths are relative paths in the build folder: the bundle builds.
	if body, _ := os.ReadFile(filepath.Join(work, "src", "ui", "card.tsx")); !strings.Contains(string(body), `from "../ui/button"`) {
		t.Errorf("card.tsx imports:\n%s", body)
	}
	if !strings.Contains(files["tokens.css"], "--primary: oklch(0.9 0 0)") {
		t.Errorf("tokens.css:\n%s", files["tokens.css"])
	}
	// The same registry again writes the same files.
	first := files
	if _, err := Add(Options{Source: src, Folder: folder, From: work, Node: fakeNode(a)}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, readTree(t, folder)) {
		t.Error("the second run changed the kit")
	}
}

func TestDropSamplesListsTheComponentInTheReadme(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "acme-ui")
	if _, err := Add(npmOptions(t, folder, "1.0.0")); err != nil {
		t.Fatal(err)
	}
	if err := DropSamples(folder, "AcmeUi", "", map[string]string{"Tag": "Error: needs a router"}); err != nil {
		t.Fatal(err)
	}
	files := readTree(t, folder)
	if !strings.Contains(files["README.md"], "`Tag` — draws only inside an application (Error: needs a router)") || !strings.Contains(files["README.md"], "`useAcme`") {
		t.Errorf("README:\n%s", files["README.md"])
	}
	if strings.Contains(files["design-system.json"], `"label": "Label"`) {
		t.Error("Tag kept its sample")
	}
	checkClean(t, folder)
}

func TestLayerCSSKeepsImportsAndPropertiesOutside(t *testing.T) {
	got := layerCSS(`@charset "utf-8";@import url("x.css");@property --a{syntax:"*";inherits:false}/* c } */body{content:"}"}`, "kit-x")
	want := "@import url(\"x.css\");\n@property --a{syntax:\"*\";inherits:false}\n@layer kit-x {\nbody{content:\"}\"}\n}\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestReplaceSection(t *testing.T) {
	md := "# B\n\nText.\n\n## Props\n\nold\n\n## Example\n\nex\n"
	if got := replaceSection(md, "Props", "new\n"); got != "# B\n\nText.\n\n## Props\n\nnew\n\n## Example\n\nex\n" {
		t.Errorf("got %q", got)
	}
	if got := replaceSection("# B\n", "In code", "x\n"); got != "# B\n\n## In code\n\nx\n" {
		t.Errorf("got %q", got)
	}
}

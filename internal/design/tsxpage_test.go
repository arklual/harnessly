package design

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const pagesKitManifest = `{
  "title": "Acme", "namespace": "Acme",
  "kit": {"id": "acme-ds", "version": "1.0.0", "source": "library", "package": "@acme/ui", "root": "Root"},
  "groups": [{"id": "g", "title": "G", "components": [
    {"id": "Button", "code": {"import": "@acme/ui", "export": "Button"}},
    {"id": "Root", "code": {"import": "@acme/ui", "export": "Root"}},
    {"id": "Chip", "code": {"import": "@acme/ui/chip", "export": "Chip"}}
  ]}]
}`

const pagesHome = `import { Root, Button } from "@acme/ui";
import { Chip } from "@acme/ui/chip";
import { useState } from "react";
import { events } from "./home.fixtures";
import { Title } from "../components/title";

export default function Home() {
  const [n] = useState(0);
  return (
    <Root>
      <Title text="Events" />
      {events.map((e) => <Chip key={e.id}>{e.name}</Chip>)}
      <Button variant="primary">{n}</Button>
    </Root>
  );
}
`

// pagesRig is a library with one project "x" pinned to a made-up kit, holding the page "home".
func pagesRig(t *testing.T) (*Library, string) {
	t.Helper()
	t.Setenv("RUNTIME_LOCALE", "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	lib := filepath.Join(root, "lib")
	kits := filepath.Join(root, "kits")
	project := filepath.Join(lib, "x", "project")
	writeFile(t, filepath.Join(lib, "x", "meta", "project.json"), `{"v":1,"title":"X","kit":{"id":"acme-ds","version":"1.0.0","mounts":["plain"]}}`)
	writeFile(t, filepath.Join(kits, "acme-ds", "1.0.0", "themes", "plain", "design-system.json"), pagesKitManifest)
	writeFile(t, filepath.Join(project, "pages", "home.tsx"), pagesHome)
	writeFile(t, filepath.Join(project, "pages", "home.fixtures.ts"), "export const events = [{ id: 1, name: 'Spring concert' }];\n")
	writeFile(t, filepath.Join(project, "components", "title.tsx"), "export const Title = ({ text }: { text: string }) => <h1>{text}</h1>;\n")
	l := New(Config{LibraryDir: lib, KitsDir: kits, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(func() { l.StopWatching(); l.StopPages() })
	return l, project
}

func TestAPageModuleBuildsToABoardWithTheKitAndItsFixtures(t *testing.T) {
	l, project := pagesRig(t)
	b := l.BuildPage("x", "home")
	if b.Status != "built" {
		t.Fatalf("build: %+v", b)
	}
	board := readFileT(t, filepath.Join(project, "home.dc.html"))
	for _, want := range []string{
		"<!-- tk-page: generated from pages/home.tsx (in:",
		`<link rel="stylesheet" href="ds/plain/tokens.css">`,
		`<script src="ds/plain/components/bundle.js"></script>`,
		`component-from-global-scope="__tkPage.default"`,
		"Spring concert",      // the fixtures
		"React.createElement", // JSX is translated
		`window.Acme`,
		`"Chip"`,
	} {
		if !strings.Contains(board, want) {
			t.Errorf("the board lacks %q", want)
		}
	}
	if strings.Contains(board, `from "@acme/ui`) || strings.Contains(board, `require("@acme/ui`) {
		t.Errorf("an import of the kit package is left in the board")
	}
	want := []string{"components/title.tsx", "pages/home.fixtures.ts", "pages/home.tsx"}
	if strings.Join(b.Inputs, ",") != strings.Join(want, ",") {
		t.Errorf("inputs %v, want %v", b.Inputs, want)
	}
}

func TestAPageImportsOnlyTheKitReactAndItsOwnProject(t *testing.T) {
	l, project := pagesRig(t)
	cases := []struct{ name, src, want string }{
		{"lodash", `import _ from "lodash"; export default () => null;`, `import "lodash" is not allowed`},
		{"kit name", `import { Nope } from "@acme/ui"; export default () => <Nope />;`, `"Nope" is not exported by the kit package @acme/ui`},
		{"kit path", `import { A } from "@acme/ui/deep/a"; export default () => null;`, "not a path the kit acme-ds exports"},
		{"css", `import "./x.css"; export default () => null;`, "only code is imported"},
		{"outside", `import { s } from "../../other/secret"; export default () => null;`, "is not found"},
		{"ds", `import { s } from "../ds/plain/tokens.css"; export default () => null;`, "only code is imported"},
		{"syntax", `export default () => <div>;`, "pages/bad.tsx:1:"},
	}
	writeFile(t, filepath.Join(filepath.Dir(project), "..", "other", "secret.ts"), "export const s = 1;")
	for _, c := range cases {
		writeFile(t, filepath.Join(project, "pages", "bad.tsx"), c.src)
		b := l.BuildPage("x", "bad")
		if b.Status != "error" || !strings.Contains(b.Error, c.want) {
			t.Errorf("%s: %+v, want an error with %q", c.name, b, c.want)
		}
		if _, err := os.Stat(filepath.Join(project, "bad.dc.html")); err == nil {
			t.Errorf("%s: a board was written for a page that does not build", c.name)
		}
	}
}

func TestAnImportLeavingTheProjectByAnAbsoluteFileIsRefused(t *testing.T) {
	l, project := pagesRig(t)
	outside := filepath.Join(filepath.Dir(filepath.Dir(project)), "secret.ts")
	writeFile(t, outside, "export const s = 1;")
	writeFile(t, filepath.Join(project, "pages", "bad.tsx"), `import { s } from "../../../secret"; export default () => null;`)
	b := l.BuildPage("x", "bad")
	if b.Status != "error" || !strings.Contains(b.Error, "leaves the project") {
		t.Fatalf("%+v", b)
	}
}

func TestAPageBuildsToTheSameBytesEveryTime(t *testing.T) {
	l, project := pagesRig(t)
	l.BuildPage("x", "home")
	first := readFileT(t, filepath.Join(project, "home.dc.html"))
	// A library that has kept nothing builds the same bytes.
	if err := os.Remove(filepath.Join(project, "home.dc.html")); err != nil {
		t.Fatal(err)
	}
	l2 := New(Config{LibraryDir: l.cfg.LibraryDir, KitsDir: l.cfg.KitsDir, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(func() { l2.StopWatching(); l2.StopPages() })
	if b := l2.BuildPage("x", "home"); b.Status != "built" {
		t.Fatalf("%+v", b)
	}
	if second := readFileT(t, filepath.Join(project, "home.dc.html")); second != first {
		t.Fatalf("two builds differ:\n%s\n---\n%s", first, second)
	}
}

func TestAPageIsBuiltAgainOnlyWhenAnInputChanged(t *testing.T) {
	l, project := pagesRig(t)
	if b := l.BuildPage("x", "home"); b.Status != "built" {
		t.Fatalf("%+v", b)
	}
	if b := l.BuildPage("x", "home"); b.Status != "current" || l.PageBuildsDone() != 1 {
		t.Fatalf("second: %+v, builds %d", b, l.PageBuildsDone())
	}
	fixtures := filepath.Join(project, "pages", "home.fixtures.ts")
	writeFile(t, fixtures, "export const events = [{ id: 2, name: 'Autumn fair' }];\n")
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(fixtures, future, future)
	b := l.BuildPage("x", "home")
	if b.Status != "built" || l.PageBuildsDone() != 2 {
		t.Fatalf("after a change: %+v, builds %d", b, l.PageBuildsDone())
	}
	if board := readFileT(t, filepath.Join(project, "home.dc.html")); !strings.Contains(board, "Autumn fair") || strings.Contains(board, "Spring concert") {
		t.Fatalf("the board does not follow the fixtures")
	}
	// A new version of the kit is a new build of every page.
	manifest := filepath.Join(l.cfg.KitsDir, "acme-ds", "1.0.0", "themes", "plain", "design-system.json")
	writeFile(t, manifest, strings.Replace(pagesKitManifest, `"id": "Chip"`, `"id": "Chip2"`, 1))
	if b := l.BuildPage("x", "home"); b.Status != "built" || l.PageBuildsDone() != 3 {
		t.Fatalf("after the kit changed: %+v", b)
	}
}

func TestABoardNotBuiltFromThePageIsNotOverwritten(t *testing.T) {
	l, project := pagesRig(t)
	writeFile(t, filepath.Join(project, "home.dc.html"), "<x-dc>by hand</x-dc>")
	b := l.BuildPage("x", "home")
	if b.Status != "error" || !strings.Contains(b.Error, "not overwritten") {
		t.Fatalf("%+v", b)
	}
	if got := readFileT(t, filepath.Join(project, "home.dc.html")); got != "<x-dc>by hand</x-dc>" {
		t.Fatalf("the hand-made board was changed: %q", got)
	}
}

func TestTheCheckSaysWhetherABuiltBoardIsCurrentAndItIsReadOnly(t *testing.T) {
	l, project := pagesRig(t)
	if p := l.PageSourceProblems("x"); len(p) != 1 || !strings.Contains(p[0].Message, "has no board") {
		t.Fatalf("before the build: %+v", p)
	}
	l.BuildPage("x", "home")
	if p := l.PageSourceProblems("x"); len(p) != 0 {
		t.Fatalf("after the build: %+v", p)
	}
	writeFile(t, filepath.Join(project, "pages", "home.fixtures.ts"), "export const events = [];\n")
	if p := l.PageSourceProblems("x"); len(p) != 1 || !strings.Contains(p[0].Message, "out of date") {
		t.Fatalf("after an edit: %+v", p)
	}
	if reason := l.BoardLockReason("x", filepath.Join(project, "home.dc.html")); !strings.Contains(reason, "pages/home.tsx") {
		t.Fatalf("a built board is open to a write: %q", reason)
	}
	if reason := l.BoardLockReason("x", filepath.Join(project, "pages", "home.tsx")); reason != "" {
		t.Fatalf("the source is closed: %q", reason)
	}
}

func TestAPageWithoutAKitStillBuildsButCannotImportOne(t *testing.T) {
	l, project := pagesRig(t)
	writeFile(t, filepath.Join(filepath.Dir(project), "meta", "project.json"), `{"v":1,"title":"X"}`)
	writeFile(t, filepath.Join(project, "pages", "plain.tsx"), `export default () => <p>hi</p>;`)
	if b := l.BuildPage("x", "plain"); b.Status != "built" {
		t.Fatalf("%+v", b)
	}
	if board := readFileT(t, filepath.Join(project, "plain.dc.html")); strings.Contains(board, "bundle.js") {
		t.Fatalf("a kit is loaded for a project without one")
	}
	b := l.BuildPage("x", "home")
	if b.Status != "error" || !strings.Contains(b.Error, "import \"@acme/ui\" is not allowed") {
		t.Fatalf("%+v", b)
	}
}

// TestServePagesForBrowser serves a project with a page module and a made-up kit bundle, for a look in a real
// browser: DESIGN_PAGES_SERVE=<file> gets the port, <file>.stop ends it.
func TestServePagesForBrowser(t *testing.T) {
	out := os.Getenv("DESIGN_PAGES_SERVE")
	if out == "" {
		t.Skip()
	}
	l, project := pagesRig(t)
	engine, err := os.ReadFile("ownengine/support.js")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, "support.js"), string(engine))
	theme := filepath.Join(l.cfg.KitsDir, "acme-ds", "1.0.0", "themes", "plain")
	writeFile(t, filepath.Join(theme, "tokens.css"), ":root{--acme-ink:#123;--acme-chip:#fde68a}\n")
	writeFile(t, filepath.Join(theme, "components", "bundle.css"), ".acme-root{font:16px sans-serif;color:var(--acme-ink);padding:24px}.acme-chip{background:var(--acme-chip);padding:4px 10px;margin-right:8px;border-radius:12px}.acme-btn{padding:8px 16px}\n")
	writeFile(t, filepath.Join(theme, "components", "bundle.js"), `(function(){var h=React.createElement;window.Acme={`+
		`Root:function(p){return h('div',{className:'acme-root','data-acme':'root'},p.children)},`+
		`Button:function(p){return h('button',{className:'acme-btn'},p.children)},`+
		`Chip:function(p){return h('span',{className:'acme-chip'},p.children)}};})();`+"\n")
	if b := l.BuildPage("x", "home"); b.Status != "built" {
		t.Fatalf("%+v", b)
	}
	l.WatchPages()
	port, err := l.EnsureServer()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(out, []byte(strconv.Itoa(port)), 0o644)
	for i := 0; i < 3000; i++ {
		if _, err := os.Stat(out + ".stop"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

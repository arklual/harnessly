package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boardOld is an old board with what boards are made of: a helmet, props with defaults, a logic class, text and
// attribute expressions, a condition, a loop, a component from the global scope with a place and children.
const boardOld = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<script src="./support.js"></script>
<link rel="stylesheet" href="./theme.css">
</head>
<body>
<x-dc>
<helmet>
<style>.card{padding:8px}</style>
</helmet>
<main class="board" data-theme="{{ theme }}" style="padding: 24px; --gap: 8px">
  <h1 class="title">{{ title }} — {{ count }}</h1>
  <sc-if value="{{ !empty }}">
    <ul>
      <sc-for list="{{ rows }}" as="row">
        <li class="card" onClick="{{ pick }}">{{ row.name }} #{{ $index }}</li>
      </sc-for>
    </ul>
  </sc-if>
  <x-import component-from-global-scope="Acme.Button" variant="primary" style="width: 120px; color: red" hint-size="120px,40px">Go</x-import>
  <svg viewBox="0 0 10 10"><rect stroke-width="2" width="10" height="10"></rect></svg>
</main>
</x-dc>
<script type="text/x-dc" data-dc-script data-props="{&quot;title&quot;: {&quot;editor&quot;: &quot;text&quot;, &quot;default&quot;: &quot;Orders&quot;}, &quot;theme&quot;: {&quot;default&quot;: &quot;dark&quot;}, &quot;flag&quot;: {&quot;editor&quot;: &quot;bool&quot;}}">
class Component extends DCLogic {
  renderVals() {
    return { rows: [{ name: 'A' }, { name: 'B' }], count: 2, empty: false };
  }
}
</script>
</body>
</html>
`

func TestABoardTurnsIntoAPageModule(t *testing.T) {
	m, err := TranslateBoard("Заказы — список", boardOld, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`import { props } from "./Заказы — список.fixtures";`,
		"export default function ЗаказыСписок() {",
		"const v = useBoard(Logic, props);",
		`<main className="board" data-theme={v.theme} style={{ padding: "24px", "--gap": "8px" }}>`,
		`<h1 className="title">`,
		"{show(v.title)}",
		`{" — "}`,
		"{!v.empty ? (",
		"{list(v.rows).map((row, index) => (",
		`<li className="card" onClick={fn(v.pick)}>`,
		"{show(row?.name)}",
		"{show(index)}",
		`<div className="sc-host-x" style={{ width: "120px" }}>`,
		`<Global __global="Acme.Button" __hint="120px,40px" variant="primary">`,
		`<svg viewBox="0 0 10 10">`,
		`<rect stroke-width="2" width="10" height="10" />`,
		"class Component extends DCLogic {",
	} {
		if !strings.Contains(m.Page, want) {
			t.Errorf("the page has no %q:\n%s", want, m.Page)
		}
	}
	if !m.HasProps || !strings.Contains(m.Fixtures, `"title": "Orders",`) || !strings.Contains(m.Fixtures, `"theme": "dark",`) || strings.Contains(m.Fixtures, "flag") {
		t.Errorf("fixtures: the defaults the props give:\n%s", m.Fixtures)
	}
	// The frame is the file without the page and the logic, with the helmet and the page's place.
	for _, want := range []string{`<link rel="stylesheet" href="./theme.css">`, "<helmet>\n<style>.card{padding:8px}</style>\n</helmet>", pageSlot, "</x-dc>\n</body>"} {
		if !strings.Contains(m.Frame, want) {
			t.Errorf("the frame has no %q:\n%s", want, m.Frame)
		}
	}
	for _, gone := range []string{"data-dc-script", "<main", "renderVals"} {
		if strings.Contains(m.Frame, gone) {
			t.Errorf("the frame still has %q:\n%s", gone, m.Frame)
		}
	}
	// Exact: the whitespace as it was written.
	exact, err := TranslateBoard("x", boardOld, false)
	if err != nil || !strings.Contains(exact.Page, `{"\n  "}`) {
		t.Errorf("exact whitespace: %v\n%s", err, exact.Page)
	}
}

func TestExpressionsReadTheValuesAsTheEngineDoes(t *testing.T) {
	sc := &tsxScope{vals: "v"}
	loop := &tsxScope{parent: sc, names: map[string]string{"row": "row", "$index": "i"}}
	for src, want := range map[string]string{
		"a":                 "v.a",
		" a.b.c ":           "v.a?.b?.c",
		"a.0":               "v.a?.[0]",
		"a[1]":              "v.a?.[1]",
		`a["x y"]`:          `v.a?.["x y"]`,
		"!a":                "!v.a",
		"'x'":               `"x"`,
		"-1.50":             "-1.5",
		"a[b]":              "undefined",
		"a == 'x'":          "undefined",
		"n + 1":             "undefined",
		"null":              "null",
		"a b":               "undefined",
		"":                  "undefined",
		"row.name":          "row?.name",
		"$index":            "i",
		"rows[$index].name": "undefined",
	} {
		s := sc
		if strings.Contains(src, "row") || strings.Contains(src, "$index") {
			s = loop
		}
		if got := tsxExpr(src, s); got != want {
			t.Errorf("%q: %s, want %s", src, got, want)
		}
	}
	if got, _ := tsxAttr("a {{ x }} b", sc); got != "`a ${v.x ?? \"\"} b`" {
		t.Errorf("text with an expression: %s", got)
	}
}

func TestWhatTheRulesDoNotCoverStaysABoard(t *testing.T) {
	wrap := func(body string) string {
		return "<!doctype html><html><head><script src=\"./support.js\"></script></head><body><x-dc>" + body + "</x-dc></body></html>"
	}
	for body, want := range map[string]string{
		`<dc-import name="Other"></dc-import>`:                      "dc-import",
		`<x-import from="./parts.jsx" component="Card"></x-import>`: "loads a file",
		`<div style-hover="color: red">x</div>`:                     "style-hover",
		`<div><helmet><style>a{}</style></helmet></div>`:            "helmet",
	} {
		_, err := TranslateBoard("x", wrap(body), true)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want a reason with %q", body, err, want)
		}
	}
	if _, err := TranslateBoard("x", "<html><body>no board</body></html>", true); err == nil {
		t.Error("a file without <x-dc> is not a board")
	}
}

// fakeCompare says the page draws like the board, or n pixels differently.
type fakeCompare struct {
	pixels int
	seen   []string
}

func (f *fakeCompare) Compare(board string, original, built []byte) (int, error) {
	f.seen = append(f.seen, board)
	return f.pixels, nil
}
func (f *fakeCompare) Close() {}

func tsxRig(t *testing.T) (*Library, string, string) {
	t.Helper()
	t.Setenv("RUNTIME_LOCALE", "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	lib := filepath.Join(root, "lib")
	project := filepath.Join(lib, "x", "project")
	writeFile(t, filepath.Join(lib, "x", "meta", "project.json"), `{"v":1,"title":"X"}`)
	writeFile(t, filepath.Join(project, "support.js"), "/* engine */\n")
	writeFile(t, filepath.Join(project, "Заказы — список.dc.html"), boardOld)
	writeFile(t, filepath.Join(project, "Other.dc.html"), "<!doctype html><html><head></head><body><x-dc><dc-import name=\"x\"></dc-import></x-dc></body></html>\n")
	l := New(Config{LibraryDir: lib, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(func() { l.StopWatching(); l.StopPages() })
	return l, lib, project
}

func tsxTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			out[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return out
}

func TestMigrateToTSXAndBackGivesTheBoardByteForByte(t *testing.T) {
	l, lib, project := tsxRig(t)
	before := tsxTree(t, lib)
	board := "Заказы — список.dc.html"

	dry, err := l.MigrateToTSX("x", TSXMigrateOptions{DryRun: true, Compare: &fakeCompare{}})
	if err != nil || dry.Done != 1 || dry.Kept != 1 {
		t.Fatalf("dry run: %v %+v", err, dry)
	}
	if !jsonEqual(before, tsxTree(t, lib)) {
		t.Fatal("the dry run wrote something")
	}
	// A page that draws differently: nothing changes.
	rep, err := l.MigrateToTSX("x", TSXMigrateOptions{Compare: &fakeCompare{pixels: 12}})
	if err != nil || rep.Done != 0 || rep.Boards[1].Status != "differs" || rep.Boards[1].Pixels != 12 {
		t.Fatalf("differs: %v %+v", err, rep)
	}
	if !jsonEqual(before, tsxTree(t, lib)) {
		t.Fatal("a board whose page differs was changed")
	}

	cmp := &fakeCompare{}
	rep, err = l.MigrateToTSX("x", TSXMigrateOptions{Compare: cmp})
	if err != nil || rep.Done != 1 {
		t.Fatalf("migrate: %v %+v", err, rep)
	}
	if rep.Boards[0].Board != "Other.dc.html" || rep.Boards[0].Status != "kept" || !strings.Contains(rep.Boards[0].Reason, "dc-import") {
		t.Errorf("the board the rules do not cover: %+v", rep.Boards[0])
	}
	if src, built := l.BoardIsBuilt("x", board); !built || src != "pages/Заказы — список.tsx" {
		t.Fatalf("the board is built from the page: %q %v", src, built)
	}
	for _, f := range []string{"pages/Заказы — список.tsx", "pages/Заказы — список.fixtures.ts", "pages/Заказы — список.board.html", "pages/board-engine/index.ts"} {
		if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(f))); err != nil {
			t.Errorf("no %s", f)
		}
	}
	builtBoard := readFileT(t, filepath.Join(project, board))
	for _, want := range []string{"<!-- tk-page: generated from pages/Заказы — список.tsx", `<link rel="stylesheet" href="./theme.css">`, "<helmet>", pageSlot, "tkPageWhenReact"} {
		if !strings.Contains(builtBoard, want) {
			t.Errorf("the built board has no %q", want)
		}
	}
	// The page builds again to the same board: the watcher leaves it as it is.
	if b := l.BuildPage("x", "Заказы — список"); b.Status != "current" {
		t.Errorf("rebuild: %+v", b)
	}
	// A second run leaves a migrated board alone.
	again, _ := l.MigrateToTSX("x", TSXMigrateOptions{Compare: &fakeCompare{}})
	if again.Boards[1].Status != "already" {
		t.Errorf("second run: %+v", again.Boards[1])
	}

	// Back: an edited page is not rolled back without --force.
	page := filepath.Join(project, "pages", "Заказы — список.tsx")
	writeFile(t, page, readFileT(t, page)+"// edited\n")
	back, err := l.MigrateToBoard("x", TSXMigrateOptions{})
	if err != nil || back.Done != 0 || back.Boards[0].Status != "changed" {
		t.Fatalf("an edited page: %v %+v", err, back)
	}
	back, err = l.MigrateToBoard("x", TSXMigrateOptions{Force: true})
	if err != nil || back.Done != 1 {
		t.Fatalf("rollback: %v %+v", err, back)
	}
	if after := tsxTree(t, lib); !jsonEqual(before, after) {
		for k, v := range after {
			if before[k] != v {
				t.Errorf("not as it was: %s", k)
			}
		}
		for k := range before {
			if _, ok := after[k]; !ok {
				t.Errorf("gone: %s", k)
			}
		}
	}
}

func TestMigrationWritesAndRemovesOnlyItsOwnFiles(t *testing.T) {
	l, lib, project := tsxRig(t)
	// pages/ that leads out of the project: the board stays.
	outside := filepath.Join(filepath.Dir(lib), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "pages")); err != nil {
		t.Fatal(err)
	}
	rep, err := l.MigrateToTSX("x", TSXMigrateOptions{Compare: &fakeCompare{}})
	if err != nil || rep.Done != 0 || !strings.Contains(rep.Boards[1].Reason, "symbolic link") {
		t.Fatalf("pages/ is a link: %v %+v", err, rep)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("written through the link: %v", entries)
	}
	if err := os.Remove(filepath.Join(project, "pages")); err != nil {
		t.Fatal(err)
	}
	if rep, err = l.MigrateToTSX("x", TSXMigrateOptions{Compare: &fakeCompare{}}); err != nil || rep.Done != 1 {
		t.Fatalf("migrate: %v %+v", err, rep)
	}
	// A record that names another file: the rollback removes nothing.
	keep := filepath.Join(project, "keep.txt")
	writeFile(t, keep, "mine\n")
	recFile := filepath.Join(l.tsxMigrationDir("x"), "Заказы — список.json")
	rec := readFileT(t, recFile)
	writeFile(t, recFile, strings.Replace(rec, `"files": {`, `"files": {"keep.txt": "0",`, 1))
	back, err := l.MigrateToBoard("x", TSXMigrateOptions{Force: true})
	if err != nil || back.Done != 0 || back.Boards[0].Status != "error" {
		t.Fatalf("a tampered record: %v %+v", err, back)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("a file the record named was removed")
	}
}

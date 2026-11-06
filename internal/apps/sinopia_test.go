package apps

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const lintManifest = `{
  "id": "harnessly.deck-lint",
  "name": "Валидатор презентаций",
  "description": "Находит слайды, где слишком много текста или мелкий шрифт",
  "version": "0.1.0",
  "harnessly": ">=0.17",
  "trust": "sandboxed",
  "targets": ["sinopia.slides"],
  "activation": { "on": ["document"] },
  "permissions": ["sinopia.read", "sinopia.write"],
  "views": [{ "id": "panel", "entry": "ui/panel.html", "targets": ["sinopia.slides"] }],
  "sinopia": {
    "panels": [{ "id": "lint", "view": "panel", "kinds": ["slides"] }],
    "checks": [{ "id": "deck", "on": ["save", "share"], "blockShare": "error", "rules": [
      { "id": "wordy", "select": "p, li, h1, h2", "maxWords": 5, "level": "error", "message": "Слишком много слов: {n} из {limit}" },
      { "id": "tiny", "select": "p, li", "minFontPx": 20, "message": "Мелкий шрифт: {n}px", "fix": { "font-size": "24px" } }
    ] }],
    "selection": [{ "id": "fix", "title": "Исправить", "agent": "Исправь замечание" }]
  }
}`

func TestSinopiaManifestChecked(t *testing.T) {
	if m, ps := ParseManifest([]byte(lintManifest)); m == nil || len(ps) != 0 {
		t.Fatalf("the example: %v", ps)
	}
	cases := map[string]struct{ from, to, path string }{
		"unknown field":                     {`"selection": [{`, `"colours": [], "selection": [{`, "sinopia"},
		"panel without view":                {`"view": "panel"`, `"view": "nope"`, "sinopia.panels[0].view"},
		"panel kind":                        {`"kinds": ["slides"] }],`, `"kinds": ["video"] }],`, "sinopia.panels[0].kinds[0]"},
		"check without on":                  {`"on": ["save", "share"], `, ``, "sinopia.checks[0].on"},
		"check level":                       {`"blockShare": "error"`, `"blockShare": "fatal"`, "sinopia.checks[0].blockShare"},
		"rule without limit":                {`"maxWords": 5, `, ``, "sinopia.checks[0].rules[0]"},
		"bad selector":                      {`"select": "p, li, h1, h2"`, `"select": "p:hover"`, "sinopia.checks[0].rules[0].select"},
		"selection both":                    {`"agent": "Исправь замечание"`, `"agent": "x", "tool": "a.b"`, "sinopia.selection[0]"},
		"selection neither":                 {`"agent": "Исправь замечание"`, `"title2": 1`, "sinopia"},
		"document without a sinopia target": {`"targets": ["sinopia.slides"]`, `"targets": ["chat"]`, "activation.on[0]"},
	}
	for name, c := range cases {
		raw := strings.Replace(lintManifest, c.from, c.to, 1)
		if raw == lintManifest {
			t.Fatalf("%s: the case changes nothing", name)
		}
		_, ps := ParseManifest([]byte(raw))
		if problemAt(ps, c.path) == nil && !strings.Contains(ps.Error(), c.path) {
			t.Errorf("%s: want a problem at %s, got %v", name, c.path, ps)
		}
	}
	// A check by tool names a server of the app.
	raw := strings.Replace(lintManifest, `"rules": [`, `"tool": "lint.check", "rules2": [`, 1)
	if _, ps := ParseManifest([]byte(raw)); len(ps) == 0 {
		t.Error("a typo in a check is caught")
	}
}

func TestSinopiaOnInDocument(t *testing.T) {
	m, _ := ParseManifest([]byte(lintManifest))
	if !m.OnInDocument("slides") || m.OnInDocument("site") || m.OnInDocument("video") {
		t.Fatal("the validator is on in slides only")
	}
	all := strings.Replace(strings.Replace(lintManifest, `["sinopia.slides"]`, `["sinopia.*"]`, 1), `"targets": ["sinopia.*"] }]`, `"targets": ["sinopia.*"] }]`, 1)
	if m, _ := ParseManifest([]byte(all)); m == nil || !m.OnInDocument("site") || !m.OnInDocument("video") {
		t.Fatal("sinopia.* is every kind")
	}
	manual := strings.Replace(lintManifest, `"on": ["document"]`, `"on": ["manual"]`, 1)
	if m, _ := ParseManifest([]byte(manual)); m.OnInDocument("slides") {
		t.Fatal("a document does not turn on an app that did not ask")
	}
}

// fakeDocs is a document of a few nodes, and what was written to it.
type fakeDocs struct {
	kind    string
	nodes   []design.DocNode
	patched [][]design.DocOp
	added   string
	failAt  error
}

func (f *fakeDocs) Document(p string) (SinopiaDoc, error) {
	return SinopiaDoc{ID: p, Kind: f.kind, Title: "Deck", Boards: []design.DocPageRef{{ID: "P", Title: "One"}}, Rev: 5}, nil
}
func (f *fakeDocs) Nodes(p, page string) ([]design.DocNode, error) { return f.nodes, nil }
func (f *fakeDocs) Query(p, sel, page string) ([]design.DocNode, error) {
	if err := design.ValidSelector(sel); err != nil {
		return nil, err
	}
	var out []design.DocNode
	for _, n := range f.nodes {
		for _, part := range strings.Split(sel, ",") {
			if n.Tag == strings.TrimSpace(part) || (strings.HasPrefix(strings.TrimSpace(part), "[data-hl-field]") && n.Field != "") {
				out = append(out, n)
				break
			}
		}
	}
	return out, nil
}
func (f *fakeDocs) Node(p, id string) (design.DocNode, error) {
	for _, n := range f.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return design.DocNode{}, &design.DocError{Code: design.DocErrNotFound, Msg: "no such node"}
}
func (f *fakeDocs) Fields(p string) ([]design.DocField, error) {
	return []design.DocField{{Field: "percent", Value: "5", Nodes: []string{"01ARYZ6S41TSV4RRFFQ69G5FAV:2"}}, {Field: "title", Value: "Hi"}}, f.failAt
}
func (f *fakeDocs) Patch(p string, ops []design.DocOp, who design.DocActor) (design.DocPatched, error) {
	if f.failAt != nil {
		return design.DocPatched{}, f.failAt
	}
	f.patched = append(f.patched, ops)
	return design.DocPatched{Rev: 9}, nil
}
func (f *fakeDocs) AddBoard(p, title string, src []byte, tpl *design.DocTemplate, who design.DocActor) (design.DocPageRef, error) {
	f.added = title + "|" + string(src)
	return design.DocPageRef{ID: "NEW", Title: title}, nil
}

const pg = "01ARYZ6S41TSV4RRFFQ69G5FAV"

func wordyDeck() *fakeDocs {
	return &fakeDocs{kind: "slides", nodes: []design.DocNode{
		{ID: pg + ":1", Page: pg, Tpl: 1, Fingerprint: "h1:a", Tag: "h1", Own: "Short", Text: "Short"},
		{ID: pg + ":2", Page: pg, Tpl: 2, Fingerprint: "p:b", Tag: "p", Own: "one two three four five six seven", Style: "font-size: 18px", BoardW: 1920, Field: "percent"},
		{ID: pg + ":3", Page: pg, Tpl: 3, Fingerprint: "li:c", Tag: "li", Own: "tiny", Style: "color: red; font-size: 1cqw", BoardW: 1920},
		{ID: pg + ":4", Page: pg, Tpl: 4, Fingerprint: "li:d", Tag: "li", Own: "fine text", Style: "font-size: 30px"},
		{ID: pg + ":5", Page: pg, Tpl: 5, Fingerprint: "li:e", Tag: "li", Own: "no size"},
	}}
}

func installed(t *testing.T, raw string) Installed {
	t.Helper()
	m, ps := ParseManifest([]byte(raw))
	if m == nil {
		t.Fatalf("manifest: %v", ps)
	}
	return Installed{ID: m.ID, Manifest: m}
}

func TestChecksMarkViolations(t *testing.T) {
	docs, state := wordyDeck(), NewSinopiaState()
	app := installed(t, lintManifest)
	res := RunChecks(context.Background(), docs, state, "p1", "slides", CheckOnShare, []Installed{app}, func(string) *Package { return nil })
	if len(res) != 1 || res[0].Failed != "" || !res[0].Blocks {
		t.Fatalf("results: %+v", res)
	}
	var got []string
	for _, f := range res[0].Findings {
		got = append(got, f.Node[27:]+" "+f.Level+" "+f.Rule+" "+f.Message)
	}
	want := []string{
		"2 error wordy Слишком много слов: 7 из 5",
		"2 warning tiny Мелкий шрифт: 18px",
		"3 warning tiny Мелкий шрифт: 19.2px",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("findings:\n%s", strings.Join(got, "\n"))
	}
	if fix, _ := json.Marshal(res[0].Findings[1].Fix); !strings.Contains(string(fix), `"font-size":"24px"`) || !strings.Contains(string(fix), `"fp":"p:b"`) {
		t.Errorf("fix: %s", fix)
	}
	marks := state.Marks("p1")
	if len(marks) != 3 || marks[0].Source != "check:harnessly.deck-lint:deck" || marks[0].Level != "error" {
		t.Fatalf("marks: %+v", marks)
	}
	// A run replaces the check's earlier marks, it does not pile up.
	RunChecks(context.Background(), docs, state, "p1", "slides", CheckOnSave, []Installed{app}, func(string) *Package { return nil })
	if n := len(state.Marks("p1")); n != 3 {
		t.Errorf("marks after a second run: %d", n)
	}
	// Fixed, the marks go.
	docs.nodes = docs.nodes[:1]
	res = RunChecks(context.Background(), docs, state, "p1", "slides", CheckOnShare, []Installed{app}, func(string) *Package { return nil })
	if res[0].Blocks || len(state.Marks("p1")) != 0 || len(state.Results("p1")) != 1 || state.Results("p1")[0].Blocks {
		t.Errorf("a clean deck: %+v %v", res, state.Marks("p1"))
	}
}

func TestChecksRunOnTheirTrigger(t *testing.T) {
	app := installed(t, strings.Replace(lintManifest, `"on": ["save", "share"]`, `"on": ["share"]`, 1))
	res := RunChecks(context.Background(), wordyDeck(), NewSinopiaState(), "p1", "slides", CheckOnSave, []Installed{app}, func(string) *Package { return nil })
	if len(res) != 0 {
		t.Fatalf("a share check ran on save: %+v", res)
	}
	app = installed(t, strings.Replace(lintManifest, `"blockShare": "error"`, `"blockShare": "warning"`, 1))
	docs := wordyDeck()
	docs.nodes = docs.nodes[2:3]
	if res := RunChecks(context.Background(), docs, NewSinopiaState(), "p1", "slides", CheckOnShare, []Installed{app}, func(string) *Package { return nil }); !res[0].Blocks {
		t.Error("a warning blocks when asked to")
	}
}

func TestActiveInDocumentIsPerKind(t *testing.T) {
	list := []Installed{installed(t, lintManifest), installed(t, review)}
	if got := ActiveInDocument(list, "slides"); len(got) != 1 || got[0].ID != "harnessly.deck-lint" {
		t.Fatalf("slides: %v", got)
	}
	for _, kind := range []string{"site", "video", "ds"} {
		if got := ActiveInDocument(list, kind); len(got) != 0 {
			t.Errorf("%s: %v", kind, got)
		}
	}
}

func TestLocksUnbound(t *testing.T) {
	plain := installed(t, lintManifest)
	locking := installed(t, strings.Replace(lintManifest, `"selection": [`, `"bindings": { "inspector": "panel", "lockUnbound": true }, "selection": [`, 1))
	if LocksUnbound([]Installed{plain}, "slides") || !LocksUnbound([]Installed{plain, locking}, "slides") {
		t.Error("the lock is the bindings' wish")
	}
	if LocksUnbound([]Installed{locking}, "site") {
		t.Error("an app that is not on in a kind locks nothing there")
	}
}

func callRig(t *testing.T, perms string) (SinopiaCall, *fakeDocs) {
	t.Helper()
	docs := wordyDeck()
	raw := strings.Replace(lintManifest, `["sinopia.read", "sinopia.write"]`, perms, 1)
	return SinopiaCall{Docs: docs, State: NewSinopiaState(), App: installed(t, raw), Project: "p1", Who: "Валидатор"}, docs
}

func TestDocumentAPIPermissionsAndTargets(t *testing.T) {
	c, _ := callRig(t, `["sinopia.read"]`)
	for method, perm := range SinopiaMethods {
		if _, ok := RequiredPermission("harnessly/sinopia." + method); !ok {
			t.Errorf("%s is not in the bridge table", method)
		}
		got, _ := RequiredPermission("harnessly/sinopia." + method)
		if got != perm {
			t.Errorf("%s: bridge says %s, API says %s", method, got, perm)
		}
	}
	var be *BridgeError
	if _, err := c.Call("patch", map[string]any{"ops": []any{map[string]any{"op": "remove", "node": pg + ":1"}}}); !errors.As(err, &be) || be.Code != CodePermissionDenied {
		t.Errorf("patch without sinopia.write: %v", err)
	}
	if _, err := c.Call("render", map[string]any{"board": "P"}); !errors.As(err, &be) || be.Code != CodePermissionDenied {
		t.Errorf("render without sinopia.export: %v", err)
	}
	if _, err := c.Call("document", nil); err != nil {
		t.Errorf("document: %v", err)
	}
	c.Docs.(*fakeDocs).kind = "site"
	if _, err := c.Call("document", nil); !errors.As(err, &be) || be.Code != CodeNotInTarget {
		t.Errorf("a site document: %v", err)
	}
	if _, err := c.Call("spin", nil); !errors.Is(err, ErrUnknownMethod) {
		t.Errorf("unknown: %v", err)
	}
}

func TestDocumentAPIReadsAndWrites(t *testing.T) {
	c, docs := callRig(t, `["sinopia.read", "sinopia.write", "sinopia.export", "sinopia.comment"]`)
	res, err := c.Call("query", map[string]any{"selector": "li"})
	if err != nil || res.(map[string]any)["count"] != 3 {
		t.Fatalf("query: %v %v", res, err)
	}
	if _, err := c.Call("query", map[string]any{"selector": "li:hover"}); err == nil {
		t.Error("a selector the API does not read")
	}
	if n, err := c.Call("node", map[string]any{"id": pg + ":2"}); err != nil || n.(design.DocNode).Field != "percent" {
		t.Fatalf("node: %v %v", n, err)
	}
	f, err := c.Call("fields", nil)
	if err != nil || f.(map[string]any)["fields"].(map[string]any)["percent"] != "5" {
		t.Fatalf("fields: %v %v", f, err)
	}

	// The selection is what the page reported.
	c.State.SetSelection("p1", []SelectedNode{{ID: pg + ":2", Tag: "p", Field: "percent", Text: "x"}})
	sel, _ := c.Call("selection", nil)
	if nodes := sel.(map[string]any)["nodes"].([]SelectedNode); len(nodes) != 1 || nodes[0].Field != "percent" {
		t.Fatalf("selection: %+v", sel)
	}

	if _, err := c.Call("patch", map[string]any{"ops": []any{map[string]any{"op": "setText", "node": pg + ":1", "value": "New"}}}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if _, err := c.Call("patch", map[string]any{"ops": []any{map[string]any{"op": "setText", "node": pg + ":1", "colour": "red"}}}); err == nil {
		t.Error("an op with a field it does not know")
	}
	if _, err := c.Call("setFields", map[string]any{"patch": map[string]any{"percent": 7.0, "title": "T"}}); err != nil {
		t.Fatalf("setFields: %v", err)
	}
	last := docs.patched[len(docs.patched)-1]
	if len(last) != 2 || last[0].Op != "setField" || last[0].Field != "percent" || last[1].Field != "title" {
		t.Errorf("setFields ops: %+v", last)
	}
	if len(docs.patched) != 2 {
		t.Errorf("each patch is one write: %d", len(docs.patched))
	}

	// A refusal of the library reaches the caller in its words, with a code that says what kind.
	docs.failAt = &design.DocError{Code: design.DocErrClaimed, Msg: "being edited by Agent A"}
	var be *BridgeError
	if _, err := c.Call("setFields", map[string]any{"patch": map[string]any{"percent": 1.0}}); !errors.As(err, &be) || be.Code != CodePolicyBlocked || !strings.Contains(be.Message, "Agent A") {
		t.Errorf("claimed: %v", err)
	}
	docs.failAt = nil

	if _, err := c.Call("render", map[string]any{"board": "P"}); !errors.As(err, &be) || !strings.Contains(be.Message, "not available") {
		t.Errorf("render is refused plainly: %v", err)
	}

	// Marks: by id, with a level; comment puts one of its own kind.
	if _, err := c.Call("mark", map[string]any{"nodes": []any{pg + ":2"}, "note": "check this", "level": "warning"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call("mark", map[string]any{"nodes": []any{"nope"}, "note": "x"}); err == nil {
		t.Error("a bad node id")
	}
	if _, err := c.Call("comment", map[string]any{"node": pg + ":3", "text": "why so small?"}); err != nil {
		t.Fatal(err)
	}
	marks := c.State.Marks("p1")
	if len(marks) != 2 || marks[0].Level != "warning" || marks[1].Level != "comment" || marks[1].Source != "comment:harnessly.deck-lint" {
		t.Fatalf("marks: %+v", marks)
	}
	// replace with no nodes clears the app's own marks, not the comments.
	if _, err := c.Call("mark", map[string]any{"nodes": []any{}, "replace": true}); err != nil {
		t.Fatal(err)
	}
	if marks := c.State.Marks("p1"); len(marks) != 1 || marks[0].Level != "comment" {
		t.Errorf("after clear: %+v", marks)
	}
}

func TestFieldSchemaChecksValues(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "harnessly"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := strings.Replace(lintManifest, `"selection": [{ "id": "fix", "title": "Исправить", "agent": "Исправь замечание" }]`,
		`"bindings": { "schema": "schema.json", "inspector": "panel" }`, 1)
	for file, body := range map[string]string{
		"harnessly/app.json": m, "ui/panel.html": "<p>x</p>",
		"schema.json": `{"type":"object","properties":{
			"percent":{"type":"number","minimum":1,"maximum":30},
			"title":{"type":"string","maxLength":5},
			"style":{"type":"string","enum":["a","b"]}}}`,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkg, ps := LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) != 0 {
		t.Fatalf("package: %v", ps)
	}
	c, docs := callRig(t, `["sinopia.read", "sinopia.write"]`)
	c.App = installed(t, m)
	c.Pkg = pkg
	bad := []map[string]any{{"percent": 31.0}, {"percent": "5"}, {"title": "toolong"}, {"style": "c"}, {"nope": 1.0}}
	for _, patch := range bad {
		if _, err := c.Call("setFields", map[string]any{"patch": patch}); err == nil {
			t.Errorf("%v goes through", patch)
		}
	}
	if len(docs.patched) != 0 {
		t.Error("a refused value was written")
	}
	if _, err := c.Call("setFields", map[string]any{"patch": map[string]any{"percent": 5.0, "title": "ok", "style": "a"}}); err != nil {
		t.Fatalf("a good patch: %v", err)
	}
	// fields() answers with the types the schema gives.
	f, _ := c.Call("fields", nil)
	if v := f.(map[string]any)["fields"].(map[string]any)["percent"]; v != 5.0 {
		t.Errorf("percent is a number: %#v", v)
	}
}

func TestBoardsAddFromATemplate(t *testing.T) {
	dir := t.TempDir()
	m := strings.Replace(lintManifest, `"selection": [`, `"templates": ["templates/card"], "selection": [`, 1)
	for file, body := range map[string]string{"harnessly/app.json": m, "ui/panel.html": "x", "templates/card/board.dc.html": "<x-dc><p data-hl-field='a'>0</p></x-dc>"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkg, ps := LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) != 0 {
		t.Fatalf("package: %v", ps)
	}
	c, docs := callRig(t, `["sinopia.read", "sinopia.write"]`)
	c.App, c.Pkg = installed(t, m), pkg
	docs.nodes = append(docs.nodes, design.DocNode{ID: "NEW:0", Tag: "p", Field: "a", Attrs: map[string]string{}})
	if _, err := c.Call("boards.add", map[string]any{"template": "card", "title": "Offer", "data": map[string]any{"a": "7"}}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.HasPrefix(docs.added, "Offer|<x-dc>") {
		t.Errorf("added: %q", docs.added)
	}
	if _, err := c.Call("boards.add", map[string]any{"template": "other"}); err == nil {
		t.Error("a template the app does not have")
	}
	_ = jsvalue.NewObject
}

func TestParseFindings(t *testing.T) {
	good := `{"content":[{"type":"text","text":"{\"findings\":[{\"node\":\"` + pg + `:3\",\"level\":\"error\",\"message\":\"Bad\"},{\"node\":\"junk\",\"message\":\"x\"},{\"node\":\"` + pg + `:4\",\"message\":\"No level\"}]}"}]}`
	got, err := parseFindings(json.RawMessage(good))
	if err != nil || len(got) != 2 || got[0].Level != "error" || got[1].Level != "warning" {
		t.Fatalf("text: %+v %v", got, err)
	}
	structured := `{"structuredContent":{"findings":[{"node":"` + pg + `:1","level":"info","message":"Hi"}]},"content":[]}`
	if got, err := parseFindings(json.RawMessage(structured)); err != nil || len(got) != 1 {
		t.Fatalf("structured: %+v %v", got, err)
	}
	if _, err := parseFindings(json.RawMessage(`{"isError":true,"content":[]}`)); err == nil {
		t.Error("a tool error")
	}
	if _, err := parseFindings(json.RawMessage(`{"content":[{"type":"text","text":"hello"}]}`)); err == nil {
		t.Error("not findings")
	}
}

func TestMarksAreBounded(t *testing.T) {
	s := NewSinopiaState()
	var marks []Mark
	for i := 0; i < 300; i++ {
		marks = append(marks, Mark{Node: pg + ":1", Level: LevelInfo, Note: strings.Repeat("x", 1000)})
	}
	s.AddMarks("p", "app:a", false, marks)
	got := s.Marks("p")
	if len(got) != maxMarksSource || len([]rune(got[0].Note)) != maxNote {
		t.Fatalf("%d marks of %d characters", len(got), len([]rune(got[0].Note)))
	}
	s.SetSelection("p", make([]SelectedNode, 80))
	if n := len(s.Selection("p")); n != maxSelected {
		t.Errorf("selection: %d", n)
	}
}

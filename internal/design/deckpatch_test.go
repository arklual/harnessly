package design

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/ly-solutions/harnessly/internal/locale"
)

func deckBoardOf(tpl string) string {
	return "<!doctype html>\n<html><head><script src=\"./support.js\"></script></head>\n<body>\n<x-dc>" + tpl + "</x-dc>\n</body></html>\n"
}

func deckNames(t *testing.T, tpl string) []string {
	t.Helper()
	b, ok := parseDcBoard(deckBoardOf(tpl))
	if !ok {
		t.Fatalf("not parsed: %s", tpl)
	}
	out := make([]string, len(b.elems))
	for i, e := range b.elems {
		out[i] = e.name
		if e.tag < 0 {
			out[i] += "?"
		}
	}
	return out
}

func deckStrP(s string) *string   { return &s }
func deckNumP(f float64) *float64 { return &f }

// The numbering is the engine's: preorder of the encoded template's elements, a nested <template>'s content left out,
// tables renamed so the parser neither adds a tbody nor moves anything.
func TestDeckPatchNumbering(t *testing.T) {
	cases := []struct{ tpl, want string }{
		{`<div><h1>Title</h1><p>Body <b>bold</b></p></div><p>Two</p>`, "div h1 p b p"},
		{`<sc-for list="{{ items }}" as="it"><li>{{ it.t }}</li></sc-for><sc-if value="{{ on }}"><p>On</p></sc-if>`, "sc-for li sc-if p"},
		{`<helmet><style>a>b{color:red}</style></helmet><h1>X</h1>`, "sc-helmet style h1"},
		{`<div title="a>b" data-x='c>d'><h2>Hi</h2></div>`, "div h2"},
		{`<table><tr><td>Cell</td></tr></table><select><option>a</option></select>`, "sc-raw-table sc-raw-tr sc-raw-td sc-raw-select option"},
		{`<template><p>x</p></template><h1>T</h1>`, "template h1"},
		{`<!-- <div> --><h1>T</h1><script>if (a<b) document.write('<p>')</script><p>x</p>`, "h1 script p"},
		{`<x-import component="A" from="a.js"/><p>T</p>`, "x-import p"},
		{`<p>One<div>Two</div>`, "p div"},
		{`<svg viewBox="0 0 1 1"><linearGradient id="g"></linearGradient><foreignObject><p>x</p></foreignObject></svg>`, "svg linearGradient foreignObject p"},
		{`<p>a</p></p><b>1<i>2</b>3</i>`, "p p? b i i"},
		{`<div fooBar="1" onClick="{{ go }}"><span>x</span></div>`, "div span"},
	}
	for _, c := range cases {
		if got := strings.Join(deckNames(t, c.tpl), " "); got != c.want {
			t.Errorf("%s\n got  %s\n want %s", c.tpl, got, c.want)
		}
	}
}

func TestDeckPatchOps(t *testing.T) {
	type tc struct {
		name string
		tpl  string
		req  DeckPatchRequest
		w    int
		want string // the new template, or "!changed" / "!dynamic" / "!bad"
	}
	cases := []tc{
		{"nested text", `<div><h1>Title</h1><p>Body <b>bold</b></p></div>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 2, Was: deckStrP(`Body <b>bold</b>`), HTML: deckStrP(`Body <i>new</i>`)},
			0, `<div><h1>Title</h1><p>Body <i>new</i></p></div>`},
		{"sibling text", `<h1>One</h1><h1>Two</h1>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 1, Was: deckStrP(`Two`), HTML: deckStrP(`Три`)}, 0, `<h1>One</h1><h1>Три</h1>`},
		{"cyrillic, entities, spaces kept around", "<h1 class=\"t\">\n    Как агент ходит &amp; в ваши&nbsp;сервисы\n  </h1>",
			DeckPatchRequest{Op: DeckPatchText, Tpl: 0, Was: deckStrP("Как агент ходит &amp; в ваши&nbsp;сервисы"), HTML: deckStrP(`Как агент <b>ходит</b> в сервисы &amp; API<br>`)},
			0, "<h1 class=\"t\">\n    Как агент <b>ходит</b> в сервисы &amp; API\n  </h1>"},
		{"rendered markup is cleaned", `<p>A <strong>B</strong></p>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 0, Was: deckStrP(`A <strong data-dc-tpl="1">B</strong>`), HTML: deckStrP(`<span style="x">C</span><script>alert(1)</script> <em class="k">D</em>`)},
			0, `<p>Calert(1) <em>D</em></p>`},
		{"after an sc-for", `<sc-for list="{{ xs }}" as="x"><li>{{ x }}</li></sc-for><p>Static</p>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 2, Was: deckStrP(`Static`), HTML: deckStrP(`Fixed`)}, 0, `<sc-for list="{{ xs }}" as="x"><li>{{ x }}</li></sc-for><p>Fixed</p>`},
		{"interpolated text", `<sc-for list="{{ xs }}" as="x"><li>{{ x }}</li></sc-for>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 1, Was: deckStrP(`a`), HTML: deckStrP(`b`)}, 0, "!dynamic"},
		{"block children", `<div><p>a</p></div>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 0, Was: deckStrP(`<p>a</p>`), HTML: deckStrP(`b`)}, 0, "!dynamic"},
		{"new text would interpolate", `<h1>T</h1>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 0, Was: deckStrP(`T`), HTML: deckStrP(`{{ x }}`)}, 0, "!dynamic"},
		{"source changed", `<h1>Other</h1>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 0, Was: deckStrP(`Title`), HTML: deckStrP(`New`)}, 0, "!changed"},
		{"numbers moved: unique match", `<div class="new"><span>x</span></div><h1>Title</h1>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 0, Was: deckStrP(`Title`), HTML: deckStrP(`New`)}, 0, `<div class="new"><span>x</span></div><h1>New</h1>`},
		{"numbers moved: two matches", `<h2>Same</h2><h2>Same</h2><p>x</p>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 2, Was: deckStrP(`Same`), HTML: deckStrP(`New`)}, 0, "!changed"},
		{"helmet before", `<helmet><style>h1>b{}</style></helmet><h1>A</h1>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 2, Was: deckStrP(`A`), HTML: deckStrP(`B`)}, 0, `<helmet><style>h1>b{}</style></helmet><h1>B</h1>`},
		{"attributes with >", `<div title="a>b"><h2 data-x='c>d'>Hi</h2></div>`,
			DeckPatchRequest{Op: DeckPatchStyle, Tpl: 1, Style: map[string]*string{"color": deckStrP("red")}}, 0, `<div title="a>b"><h2 data-x='c>d' style="color: red">Hi</h2></div>`},
		{"table cell", `<table><tr><td>Cell</td></tr></table>`,
			DeckPatchRequest{Op: DeckPatchText, Tpl: 2, Was: deckStrP(`Cell`), HTML: deckStrP(`Ячейка`)}, 0, `<table><tr><td>Ячейка</td></tr></table>`},
		{"style merge", `<div class="b" style="color: red; translate: 1px 2px;">x</div>`,
			DeckPatchRequest{Op: DeckPatchStyle, Tpl: 0, Style: map[string]*string{"color": deckStrP("#123456"), "font-size": deckStrP("44px"), "font-family": deckStrP(`"Golos Text", Inter`)}},
			0, `<div class="b" style="color: #123456; translate: 1px 2px; font-family: &quot;Golos Text&quot;, Inter; font-size: 44px">x</div>`},
		{"style remove", `<div style='color: red;font-weight:700'>x</div>`,
			DeckPatchRequest{Op: DeckPatchStyle, Tpl: 0, Style: map[string]*string{"color": nil}}, 0, `<div style="font-weight:700">x</div>`},
		{"style self-closed", `<img src="a.png"/>`,
			DeckPatchRequest{Op: DeckPatchStyle, Tpl: 0, Style: map[string]*string{"opacity": deckStrP(".5")}}, 0, `<img src="a.png" style="opacity: .5"/>`},
		{"style interpolated", `<h1 style="{{ s.mv.h }}">T</h1>`,
			DeckPatchRequest{Op: DeckPatchStyle, Tpl: 0, Style: map[string]*string{"color": deckStrP("red")}}, 0, "!dynamic"},
		{"style value refused", `<h1>T</h1>`,
			DeckPatchRequest{Op: DeckPatchStyle, Tpl: 0, Style: map[string]*string{"color": deckStrP("red;background:url(x)")}}, 0, "!bad"},
		{"move in cqw", `<ul class="list" data-block="List">
  <li>a</li>
</ul>`, DeckPatchRequest{Op: DeckPatchMove, Tpl: 0, X: deckNumP(192), Y: deckNumP(-108)}, 1920,
			`<ul class="list" data-block="List" style="translate: 10cqw -5.63cqw">
  <li>a</li>
</ul>`},
		{"move again", `<div style="position: absolute; translate: 10cqw 0">x</div>`,
			DeckPatchRequest{Op: DeckPatchMove, Tpl: 0, X: deckNumP(0), Y: deckNumP(19.2)}, 1920, `<div style="position: absolute; translate: 0 1cqw">x</div>`},
		{"move, width unknown", `<div>x</div>`,
			DeckPatchRequest{Op: DeckPatchMove, Tpl: 0, X: deckNumP(12.345), Y: deckNumP(3)}, 0, `<div style="translate: 12.35px 3px">x</div>`},
		{"reset keeps the rest", `<div style="position: absolute; translate: 10cqw 0">x</div>`,
			DeckPatchRequest{Op: DeckPatchReset, Tpl: 0}, 1920, `<div style="position: absolute">x</div>`},
		{"reset drops an empty style", `<div class="a" style="translate: 10cqw 0" data-block="A">x</div>`,
			DeckPatchRequest{Op: DeckPatchReset, Tpl: 0}, 1920, `<div class="a" data-block="A">x</div>`},
		{"no such element", `<div>x</div>`,
			DeckPatchRequest{Op: DeckPatchReset, Tpl: 5}, 1920, "!changed"},
		{"implied element", `<p>a</p></p>`,
			DeckPatchRequest{Op: DeckPatchStyle, Tpl: 1, Style: map[string]*string{"color": deckStrP("red")}}, 0, "!changed"},
	}
	// A style, move or reset says which element it means by the fingerprint the board gave: here the source's own.
	fingerprint := func(tpl string, n int) *string {
		b, _ := parseDcBoard(deckBoardOf(tpl))
		if e := b.elem(n); e != nil {
			return deckStrP(deckFingerprint(e.node))
		}
		return deckStrP("none:0")
	}
	cases = append(cases,
		tc{"fingerprint of another element", `<h1 style="color: red">A</h1><h2>B</h2>`,
			DeckPatchRequest{Op: DeckPatchReset, Tpl: 1, Was: fingerprint(`<h1 style="color: red">A</h1><h2>B</h2>`, 0)}, 0,
			`<h1 style="color: red">A</h1><h2>B</h2>`},
		tc{"numbers moved: the one element with the fingerprint", `<div class="new"><span>x</span></div><h2 style="translate: 1px 0">B</h2>`,
			DeckPatchRequest{Op: DeckPatchReset, Tpl: 0, Was: fingerprint(`<h2 style="translate: 1px 0">B</h2>`, 0)}, 0,
			`<div class="new"><span>x</span></div><h2>B</h2>`},
		tc{"style changed since shown", `<h2 style="color: blue">B</h2>`,
			DeckPatchRequest{Op: DeckPatchStyle, Tpl: 0, Was: fingerprint(`<h2 style="color: red">B</h2>`, 0), Style: map[string]*string{"color": deckStrP("green")}}, 0,
			"!changed"},
		tc{"words changed since shown", `<h2>C</h2>`,
			DeckPatchRequest{Op: DeckPatchMove, Tpl: 0, Was: fingerprint(`<h2>B</h2>`, 0), X: deckNumP(1), Y: deckNumP(1)}, 0, "!changed"},
		tc{"two elements with the fingerprint", `<h2>B</h2><p>x</p><h2>B</h2>`,
			DeckPatchRequest{Op: DeckPatchReset, Tpl: 1, Was: fingerprint(`<h2>B</h2>`, 0)}, 0, "!changed"},
		tc{"no fingerprint", `<h2>B</h2>`, DeckPatchRequest{Op: DeckPatchReset, Tpl: 0, Was: deckStrP("")}, 0, "!bad"},
		tc{"move in px inside a container", `<div>x</div>`,
			DeckPatchRequest{Op: DeckPatchMove, Tpl: 0, X: deckNumP(192), Y: deckNumP(0), Unit: "px"}, 1920, `<div style="translate: 192px 0">x</div>`},
	)
	for _, c := range cases {
		src := deckBoardOf(c.tpl)
		if c.req.Op != DeckPatchText && c.req.Was == nil {
			c.req.Was = fingerprint(c.tpl, c.req.Tpl)
		}
		got, err := applyDeckPatch(src, c.req, c.w)
		var pe *deckPatchErr
		switch {
		case strings.HasPrefix(c.want, "!"):
			code := ""
			if errors.As(err, &pe) {
				code = pe.code
				if code == "" {
					code = "bad"
				}
			}
			if "!"+code != c.want {
				t.Errorf("%s: got %v %q, want %s", c.name, err, got, c.want)
			}
		case err != nil:
			t.Errorf("%s: %v", c.name, err)
		case got != deckBoardOf(c.want):
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, deckBoardOf(c.want))
		}
	}
}

func TestDeckCanon(t *testing.T) {
	same := [][2]string{
		{"\n  A &amp; B&nbsp;C\n", `A &amp; B&nbsp;C`},
		{"<b>a</b>\n<i>b</i>", `<b data-dc-tpl="3">a</b><i data-dc-tpl="4">b</i>`},
		{"x —  y", "x — y"},
		{`<strong>1</strong>`, `<strong>1</strong>`},
		{"a<br/>b", "a<br>b"},
	}
	for _, s := range same {
		if deckCanon(s[0]) != deckCanon(s[1]) {
			t.Errorf("%q vs %q: %q != %q", s[0], s[1], deckCanon(s[0]), deckCanon(s[1]))
		}
	}
	if deckCanon("<b>a</b>") == deckCanon("<i>a</i>") || deckCanon("a b") == deckCanon("ab") {
		t.Error("different texts compare equal")
	}
}

// A hand edit through the route: written in place, the history has a version named for it, a stale text is refused.
func TestDeckPatchRoute(t *testing.T) {
	l, library := historyLibrary(t)
	const project, page = "p1", "01ARYZ6S41TSV4RRFFQ69G5FAV"
	dir := filepath.Join(library, project)
	for _, d := range []string{filepath.Join(dir, ProjectDirname), filepath.Join(dir, MetaDirname, MetaPagesDirname)} {
		if err := os.MkdirAll(d, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	board := filepath.Join(dir, ProjectDirname, "Slide 1.dc.html")
	src := deckBoardOf("\n<main class=\"slide\">\n  <h1 data-block=\"Title\">Как агент ходит в ваши сервисы</h1>\n</main>\n")
	for file, body := range map[string]string{
		board: src,
		filepath.Join(dir, MetaDirname, MetaProjectFile):                `{"title":"Deck","kind":"slides"}`,
		filepath.Join(dir, MetaDirname, MetaPagesDirname, page+".json"): `{"id":"` + page + `","title":"Обложка","file":"Slide 1.dc.html","w":1920,"h":1080}`,
	} {
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.snapshotNow("rt", nil, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	a := &API{L: l, Session: func(*http.Request) bool { return true }, PlatformAPI: "/api/platform",
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	post := func(body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, "/api/platform/design/deck/patch", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		writeAnswer(w, a.DeckPatch(r))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, out := post(map[string]any{"artifact": project, "page": page, "tpl": 1, "op": "text",
		"was": "Как агент ходит в ваши сервисы", "html": "Как агент <b>ходит</b> к вам"})
	if code != 200 || out["rev"] == nil {
		t.Fatalf("text: %d %v", code, out)
	}
	got, _ := os.ReadFile(board)
	if want := strings.Replace(src, "Как агент ходит в ваши сервисы", "Как агент <b>ходит</b> к вам", 1); string(got) != want {
		t.Fatalf("board:\n%s", got)
	}
	file := "Slide 1.dc.html"
	h, err := l.readHistory(project, &file, 5)
	if err != nil || len(h.entries) == 0 || h.entries[0].Subject != locale.Pick("Manual edit: \"Обложка\"", "Правка вручную: «Обложка»") {
		t.Fatalf("history: %+v %v", h.entries, err)
	}
	if code, out := post(map[string]any{"artifact": project, "page": page, "tpl": 1, "op": "text", "was": "Как агент ходит в ваши сервисы", "html": "x"}); code != 409 || out["code"] != "changed" {
		t.Fatalf("stale: %d %v", code, out)
	}
	if code, out := post(map[string]any{"artifact": project, "page": page, "tpl": 1, "op": "move", "x": 96, "y": 0, "was": deckFingerprint(mustElem(t, readFile(t, board), 1))}); code != 200 || out["rev"] == nil {
		t.Fatalf("move: %d %v", code, out)
	}
	if got, _ := os.ReadFile(board); !strings.Contains(string(got), `<h1 data-block="Title" style="translate: 5cqw 0">`) {
		t.Fatalf("moved board:\n%s", got)
	}
	if code, _ := post(map[string]any{"artifact": project, "page": "01ARYZ6S41TSV4RRFFQ69G5FAW", "tpl": 1, "op": "reset"}); code != 404 {
		t.Fatalf("unknown page: %d", code)
	}
	if code, _ := post(map[string]any{"artifact": project, "page": page, "tpl": 1, "op": "spin"}); code != 400 {
		t.Fatalf("unknown op: %d", code)
	}
}

func mustElem(t *testing.T, src string, tpl int) *html.Node {
	t.Helper()
	b, ok := parseDcBoard(src)
	if !ok || b.elem(tpl) == nil {
		t.Fatalf("no element %d", tpl)
	}
	return b.elem(tpl).node
}

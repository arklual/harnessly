package design

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The numbering parity with the browser: a hand edit names an element by the number the board's engine gives it, and
// the runtime finds that element in the source by its own parse. Two checks, in the machine's headless Chrome:
//
//   - the engine's own code (its parseDcText, encodeCase and compileTemplate, cut out of support.js) numbers each test
//     template in the browser; the names must be the runtime's one by one, and the content of every element the deck
//     may edit must read the same as the runtime reads it from the source;
//   - whole boards are drawn by the real engine (React from the runtime's vendor files): every drawn element with a
//     data-dc-tpl has the runtime's name for that number, and every editable one's drawn markup is what the runtime
//     would compare a `was` against.
//
// More boards for the second check: DECK_PARITY_BOARDS, a list of .dc.html paths split by the path list separator.
func TestDeckPatchChromeParity(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a browser")
	}
	l := New(Config{LibraryDir: t.TempDir(), Texts: Texts{Tag: "test-design", CLI: "acme", KitGlobal: "AcmeKit", PagesGlobal: "AcmePages", ArgsGlobal: "__ACME_ARGS"}})
	bin := l.findChrome()
	if bin == "" {
		t.Skip("no browser on this machine")
	}
	defer l.StopWatching()
	engine := realEngine(t)
	cut := func(from, to string) string {
		s := string(engine)
		i := strings.Index(s, from)
		j := strings.Index(s[i+1:], to)
		if i < 0 || j < 0 {
			t.Fatalf("support.js has no %q … %q", from, to)
		}
		return s[i : i+1+j]
	}
	engineParts := cut("function parseDcText(src) {", "function dcNameFromPath(") +
		cut("// src/encode.ts", "// src/compile.ts") +
		cut("function compileTemplate(html, host) {", "const builders") + "return tpl; }"

	templates := deckParityTemplates()
	b, err := l.launchBrowser(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	tab, err := b.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		t.Fatal(err)
	}
	at, err := b.cdp.send("Target.attachToTarget", map[string]any{"targetId": tab["targetId"], "flatten": true}, "")
	if err != nil {
		t.Fatal(err)
	}
	session, _ := at["sessionId"].(string)
	eval := func(expr string) any {
		t.Helper()
		res, err := b.cdp.send("Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, session)
		if err != nil {
			t.Fatal(err)
		}
		if ex, ok := res["exceptionDetails"]; ok {
			t.Fatalf("page threw: %v", ex)
		}
		r, _ := res["result"].(map[string]any)
		return r["value"]
	}

	// 1. The engine's numbering of each template.
	srcs := make([]string, len(templates))
	for i, tpl := range templates {
		srcs[i] = deckBoardOf(tpl)
	}
	in, _ := json.Marshal(srcs)
	// The board script's fingerprint of a template element (client.js deckFingerprint), cut out of it.
	client, err := os.ReadFile("texts/client.js")
	if err != nil {
		t.Fatal(err)
	}
	fpSrc := string(client)
	fpAt := strings.Index(fpSrc, "  function deckFingerprint(el) {")
	if fpAt < 0 {
		t.Fatal("client.js has no deckFingerprint")
	}
	fpEnd := strings.Index(fpSrc[fpAt:], "\n  }\n")
	if fpEnd < 0 {
		t.Fatal("client.js deckFingerprint has no end")
	}
	fingerprint := strings.Replace(fpSrc[fpAt:fpAt+fpEnd+4], "var source = deckSourceOf(el);", "var source = el;", 1)
	got := eval(`(() => {` + engineParts + fingerprint + `
	  const fingerprint = deckFingerprint;
	  return ` + string(in) + `.map((src) => {
	    const p = parseDcText(src);
	    if (!p) return null;
	    return [...compileTemplate(p.template).content.querySelectorAll('[data-dc-tpl]')]
	      .map((el) => [Number(el.getAttribute('data-dc-tpl')), el.localName, el.innerHTML, fingerprint(el)]);
	  });
	})()`)
	rows, _ := got.([]any)
	if len(rows) != len(templates) {
		t.Fatalf("the browser answered %d templates of %d", len(rows), len(templates))
	}
	checked := 0
	for i, row := range rows {
		checked += deckParityCompare(t, "template "+templates[i], srcs[i], row, false)
	}
	t.Logf("templates: %d, editable texts compared: %d", len(templates), checked)

	// 2. Whole boards drawn by the engine.
	boards := map[string]string{
		"slide.dc.html":  deckBoardOf(deckParitySlide),
		"tables.dc.html": deckBoardOf(strings.Join(templates, "\n")),
	}
	for _, p := range filepath.SplitList(os.Getenv("DECK_PARITY_BOARDS")) {
		if p == "" {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		boards["extra-"+strings.Map(func(r rune) rune {
			if r < 128 && (r == '-' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				return r
			}
			return '_'
		}, filepath.Base(p))] = string(raw)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, vendorPrefix):
			l.serveVendor(w, r, r.URL.Path, false)
		case r.URL.Path == "/b/support.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(engine)
		case strings.HasPrefix(r.URL.Path, "/b/") && boards[strings.TrimPrefix(r.URL.Path, "/b/")] != "":
			src := boards[strings.TrimPrefix(r.URL.Path, "/b/")]
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(strings.Replace(src, "<head>", "<head>"+resourcesScript(), 1)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	for name, src := range boards {
		start, end, ok := dcTemplateRange(src)
		if !ok {
			t.Fatalf("%s has no template", name)
		}
		want, _ := json.Marshal(src[start:end])
		if _, err := b.cdp.send("Page.navigate", map[string]any{"url": srv.URL + "/b/" + name}, session); err != nil {
			t.Fatal(err)
		}
		// Drawn from the fetched file (not the DOM's copy of the template): the numbers the page then carries.
		var drawn any
		last := ""
		deadline := time.Now().Add(90 * time.Second)
		for {
			drawn = eval(`(() => {
			  if (typeof window.__dcRootName !== 'function' || !document.getElementById('dc-root')) return null;
			  if (window.__dcTemplateSource(window.__dcRootName()) !== ` + string(want) + `) return null;
			  const els = [...document.querySelectorAll('#dc-root [data-dc-tpl]')];
			  if (!els.length) return null;
			  return els.map((el) => [Number(el.getAttribute('data-dc-tpl')), el.localName, el.innerHTML, el.className === 'sc-host-x' ? 1 : 0]);
			})()`)
			// React draws the new template a moment after the engine takes it: two equal looks in a row.
			if drawn != nil && time.Now().After(deadline) {
				break
			}
			if drawn != nil {
				now, _ := json.Marshal(drawn)
				if string(now) == last {
					break
				}
				last = string(now)
			} else if time.Now().After(deadline) {
				break
			}
			time.Sleep(400 * time.Millisecond)
		}
		if drawn == nil {
			t.Fatalf("%s: the engine did not draw it", name)
		}
		n := deckParityCompare(t, "board "+name, src, drawn, true)
		t.Logf("board %s: %d drawn elements, editable texts compared: %d", name, len(drawn.([]any)), n)
	}
}

// deckParityCompare holds the browser's numbering of one template against the runtime's; the count of editable texts
// compared.
func deckParityCompare(t *testing.T, what, src string, row any, drawn bool) int {
	t.Helper()
	b, ok := parseDcBoard(src)
	if !ok {
		t.Errorf("%s: the runtime did not parse it", what)
		return 0
	}
	els, _ := row.([]any)
	if !drawn && len(els) != len(b.elems) {
		t.Errorf("%s: the browser numbers %d elements, the runtime %d", what, len(els), len(b.elems))
	}
	tpl := src[b.start:b.end]
	compared := 0
	for i, raw := range els {
		el, _ := raw.([]any)
		n, _ := el[0].(float64)
		name, _ := el[1].(string)
		inner, _ := el[2].(string)
		k := int(n)
		if !drawn && k != i {
			t.Errorf("%s: the browser's element %d is numbered %d", what, i, k)
			continue
		}
		if k < 0 || k >= len(b.elems) {
			t.Errorf("%s: the browser has number %d, the runtime %d elements", what, k, len(b.elems))
			continue
		}
		e := b.elems[k]
		want := e.name
		if drawn {
			if host, _ := el[3].(float64); host == 1 || want == "dc-import" || want == "x-import" {
				continue // an import is drawn by its component, in a box carrying the import's number
			}
			for real, alias := range dcRawWrap {
				if alias == want {
					want = real
				}
			}
		}
		if name != want {
			t.Errorf("%s: number %d is <%s> in the browser, <%s> in the runtime", what, k, name, want)
			continue
		}
		if !drawn {
			if fp, _ := el[3].(string); fp != deckFingerprint(e.node) {
				t.Errorf("%s: number %d has fingerprint %s in the browser, %s in the runtime", what, k, fp, deckFingerprint(e.node))
				continue
			}
		}
		if e.tag < 0 || !textStatic(e.node) {
			continue
		}
		s, en, ok := b.inner(&e)
		if !ok {
			continue
		}
		if got, want := deckCanon(inner), deckCanon(tpl[s:en]); got != want {
			t.Errorf("%s: number %d reads %q in the browser, %q in the source", what, k, got, want)
			continue
		}
		compared++
	}
	return compared
}

// deckParityTemplates are the shapes that could move the numbers: nesting, the engine's control tags, helmet, tables
// and selects, attributes holding >, Cyrillic, entities, auto-closed paragraphs, lists, self-closed imports, nested
// templates, comments, raw text, SVG and MathML, misnested formatting, stray end tags, forms, void elements.
func deckParityTemplates() []string {
	return []string{
		`<div><h1>Title</h1><p>Body <b>bold</b> and <i>it</i></p></div><p>Two</p>`,
		`<sc-for list="{{ items }}" as="it"><li>{{ it.t }}</li></sc-for><sc-if value="{{ on }}" hint-placeholder-val="{{ true }}"><p>On</p></sc-if>`,
		`<helmet><style>a>b{color:red}</style><meta name="x" content="y"></helmet><h1>X</h1>`,
		`<div title="a>b" data-x='c>d' data-y=e>f<h2>Hi &gt; there</h2></div>`,
		`<h1 class="sl-h">Как агент ходит в ваши сервисы</h1><p>Ёлка — «кавычки»&nbsp;и&amp;</p>`,
		`<table><caption>Cap</caption><tr><th>H</th><td>Cell <b>b</b></td></tr><tbody><tr><td>2</td></tr></tbody></table>`,
		`<TABLE><TR><TD>Upper</TD></TR></TABLE><select><option>a</option><optgroup><option>b</option></optgroup></select>`,
		`<template><p>x</p><div><span>y</span></div></template><h1>T</h1><template><template><i>z</i></template></template><p>after</p>`,
		`<!-- <div> --><h1>T</h1><script>if (a<b) document.write('<p>')</script><style>p>i{}</style><textarea><b>no</b></textarea><title><i>no</i></title><p>x</p>`,
		`<x-import component="A" from="a.js"/><p>T</p><dc-import name="B" fooBar="1"/><h2>U</h2>`,
		`<p>One<div>Two</div><p>Three<p>Four<ul><li>a<li>b</ul>`,
		`<svg viewBox="0 0 1 1"><linearGradient id="g"><stop offset="0"/></linearGradient><foreignObject><p>x</p></foreignObject><title>t</title><style>a{}</style><text>w</text></svg><math><mi>x</mi></math>`,
		`<p>a</p></p><b>1<i>2</b>3</i><p>tail</p>`,
		`<div fooBar="1" onClick="{{ go }}" style="{{ s }}"><span>x</span><img src="a.png"><br><input value="v"><hr/></div>`,
		`<form><form><input></form></form><button><button>b</button></button><a href="#"><a href="#">a</a></a>`,
		`<dl><dt>t<dd>d</dl><ruby>漢<rt>kan</rt></ruby><image src="x.png"><noscript><p>ns</p></noscript>`,
		`<main class="slide"><section data-block="List" style="position:absolute;left:7%"><h2 data-step="1">Шаг</h2>
  <ul>
    <li data-step="2"><b>1</b> Первый пункт</li>
    <li data-step="3"><b>2</b> Второй &mdash; пункт</li>
  </ul></section></main>`,
		`<html><body class="x"><div>in</div></body></html><head><p>h</p></head>`,
	}
}

// deckParitySlide is a slide as the agent writes one: blocks, steps, theme variables, an interpolated title.
const deckParitySlide = `
<helmet>
<link rel="stylesheet" href="deck-theme.css">
<style>
.slide{position:relative;width:1920px;height:1080px;background:var(--deck-bg);color:var(--deck-ink);font-family:var(--deck-font)}
.slide>h1{position:absolute;left:7cqw;top:10cqw;font-size:4.6cqw;margin:0}
.list{position:absolute;left:7cqw;top:25cqw;width:52cqw}
.fig{position:absolute;right:6cqw;top:9cqw;width:27cqw;height:34cqw}
</style>
</helmet>
<main class="slide">
  <h1 data-block="Заголовок">Как агент ходит в ваши сервисы</h1>
  <ul class="list" data-block="Список">
    <li data-step="1"><b>MCP</b> — один <i>протокол</i> для всех</li>
    <li data-step="2">Ключи остаются у вас</li>
  </ul>
  <div class="fig" data-block="Схема" data-free="1">
    <sc-for list="{{ nodes }}" as="n" hint-placeholder-count="3"><div class="node">{{ n }}</div></sc-for>
  </div>
  <table class="t"><tr><td>Ячейка</td><td>{{ cell }}</td></tr></table>
  <p class="foot">Слайд&nbsp;3 · 2026</p>
</main>
`

// deckBridgeBoard is a slide with filled-in values (its props' defaults), served with the board script, as a deck shows it.
const deckBridgeBoard = `<!doctype html>
<html><head><meta charset="utf-8"><script src="./support.js"></script></head>
<body>
<x-dc>
<helmet><style>
html,body{margin:0}
.slide{position:relative;width:1920px;height:1080px;background:#fff;color:#1c1c1f;font-family:sans-serif}
.slide>h1{position:absolute;left:134px;top:192px;width:1075px;font-size:88px;line-height:1.08;margin:0}
.list{position:absolute;left:134px;top:480px;width:998px;margin:0;padding:0;list-style:none;font-size:36px}
.fig{position:absolute;left:1300px;top:173px;width:518px;height:653px;background:#f4f4f6}
.cell{position:absolute;left:134px;top:900px;font-size:30px}
</style></helmet>
<main class="slide">
  <h1 data-block="Заголовок">Как агент ходит в ваши сервисы</h1>
  <ul class="list" data-block="Список">
    <li data-step="1"><b>MCP</b> — один протокол</li>
    <li data-step="2">Ключи остаются у вас</li>
  </ul>
  <div class="fig" data-block="Схема" data-free="1"><sc-for list="{{ nodes }}" as="n"><p>{{ n }}</p></sc-for></div>
  <p class="cell">Ячейка: {{ cell }}</p>
</main>
</x-dc>
<script type="text/x-dc" data-dc-script data-props='{"cell":{"default":"значение"},"nodes":{"default":["Агент","MCP"]}}'></script>
</body></html>
`

// The deck section of the board script in a real page: a host frames a slide, turns the deck on and gets the words of
// §1.5 back; a text edited in place and a free block dragged come back as edits the runtime then writes into the same
// source, so the numbers the browser shows and the runtime finds are one.
func TestDeckBridgeInChrome(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a browser")
	}
	l := New(Config{LibraryDir: t.TempDir(), Texts: Texts{Tag: "test-design", CLI: "acme", KitGlobal: "AcmeKit", PagesGlobal: "AcmePages", ArgsGlobal: "__ACME_ARGS"}})
	bin := l.findChrome()
	if bin == "" {
		t.Skip("no browser on this machine")
	}
	defer l.StopWatching()
	engine := realEngine(t)
	board := strings.Replace(deckBridgeBoard, "<head>", "<head>"+resourcesScript()+`<script data-artifact="demo" src="`+scriptPath+`"></script>`, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, vendorPrefix):
			l.serveVendor(w, r, r.URL.Path, false)
		case r.URL.Path == scriptPath:
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write([]byte(l.clientScript()))
		case r.URL.Path == "/b/support.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(engine)
		case r.URL.Path == "/b/slide.dc.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(board))
		case r.URL.Path == "/host":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body style="margin:0"><iframe id="f" src="/b/slide.dc.html" style="border:0;width:1920px;height:1080px;display:block"></iframe>
<script>window.got=[];addEventListener('message',function(e){if(e.data&&typeof e.data.type==='string'&&e.data.type.indexOf('tk:')===0)got.push(e.data)});
window.send=function(m){document.getElementById('f').contentWindow.postMessage(m,location.origin)};
window.frameRect=function(sel){var r=document.getElementById('f').contentDocument.querySelector(sel).getBoundingClientRect();return [r.left+r.width/2,r.top+r.height/2]};</script></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b, err := l.launchBrowser(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	tab, err := b.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		t.Fatal(err)
	}
	at, err := b.cdp.send("Target.attachToTarget", map[string]any{"targetId": tab["targetId"], "flatten": true}, "")
	if err != nil {
		t.Fatal(err)
	}
	session, _ := at["sessionId"].(string)
	send := func(method string, params map[string]any) {
		t.Helper()
		if _, err := b.cdp.send(method, params, session); err != nil {
			t.Fatal(method, err)
		}
	}
	eval := func(expr string) any {
		t.Helper()
		res, err := b.cdp.send("Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, session)
		if err != nil {
			t.Fatal(err)
		}
		if ex, ok := res["exceptionDetails"]; ok {
			t.Fatalf("page threw: %v", ex)
		}
		r, _ := res["result"].(map[string]any)
		return r["value"]
	}
	// wait returns the first message of a type (with every field of want) the host got after mark.
	mark := 0
	wait := func(typ string, want map[string]any) map[string]any {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			got, _ := eval(`window.got`).([]any)
			for _, raw := range got[mark:] {
				m, _ := raw.(map[string]any)
				if m["type"] != typ {
					continue
				}
				ok := true
				for k, v := range want {
					a, _ := json.Marshal(m[k])
					e, _ := json.Marshal(v)
					ok = ok && string(a) == string(e)
				}
				if ok {
					return m
				}
			}
			if time.Now().After(deadline) {
				all, _ := json.Marshal(got[mark:])
				t.Fatalf("no %s %v; the host got %s", typ, want, all)
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	since := func() { got, _ := eval(`window.got`).([]any); mark = len(got) }
	point := func(sel string) (float64, float64) {
		p, _ := eval(`frameRect(` + strconvQuote(sel) + `)`).([]any)
		x, _ := p[0].(float64)
		y, _ := p[1].(float64)
		return x, y
	}
	mouse := func(typ string, x, y float64, buttons int) {
		p := map[string]any{"type": typ, "x": x, "y": y, "button": "none", "buttons": buttons}
		if typ != "mouseMoved" {
			p["button"], p["clickCount"] = "left", 1
		} else if buttons > 0 {
			p["button"] = "left"
		}
		send("Input.dispatchMouseEvent", p)
	}

	send("Emulation.setDeviceMetricsOverride", map[string]any{"width": 1920, "height": 1080, "deviceScaleFactor": 1, "mobile": false})
	send("Page.navigate", map[string]any{"url": srv.URL + "/host"})
	start, end, _ := dcTemplateRange(deckBridgeBoard)
	want, _ := json.Marshal(deckBridgeBoard[start:end])
	deadline := time.Now().Add(90 * time.Second)
	for eval(`(() => { const w = document.getElementById('f') && document.getElementById('f').contentWindow; return !!(w && w.__dcTemplateSource && w.__dcTemplateSource(w.__dcRootName()) === `+string(want)+` && w.document.querySelector('.cell') && w.document.querySelector('.cell').textContent.includes('значение')); })()`) != true {
		if time.Now().After(deadline) {
			t.Fatal("the slide was not drawn")
		}
		time.Sleep(300 * time.Millisecond)
	}
	parsed, _ := parseDcBoard(deckBridgeBoard)
	tplOf := func(name string, nth int) float64 {
		for _, e := range parsed.elems {
			if e.name == name {
				if nth == 0 {
					return float64(e.tpl)
				}
				nth--
			}
		}
		t.Fatalf("no <%s>", name)
		return 0
	}
	h1, ul, fig := tplOf("h1", 0), tplOf("ul", 0), tplOf("div", 0)

	// On: the size and the steps.
	eval(`send({ type: 'tk:deck', on: true, mode: 'edit' })`)
	wait("tk:deck-ready", map[string]any{"w": 1920, "h": 1080, "steps": 2})

	// Hover: the block under the mouse.
	since()
	x, y := point("h1")
	mouse("mouseMoved", x, y, 0)
	hover := wait("tk:deck-hover", map[string]any{"tpl": h1, "name": "Заголовок", "moved": false, "movable": true})
	if r, _ := hover["rect"].([]any); len(r) != 4 || r[0] != 134.0 || r[1] != 192.0 {
		t.Fatalf("hover rect %v", hover["rect"])
	}

	// A text edited in place comes back with what it was; the runtime writes it into the same source.
	since()
	mouse("mousePressed", x, y, 1)
	mouse("mouseReleased", x, y, 0)
	wait("tk:deck-select", map[string]any{"tpl": h1, "label": "Заголовок", "kind": "title", "size": 88})
	eval(`(() => { const w = document.getElementById('f').contentWindow; const r = w.document.createRange(); r.selectNodeContents(w.document.querySelector('h1')); r.collapse(false); const s = w.getSelection(); s.removeAllRanges(); s.addRange(r); })()`)
	send("Input.insertText", map[string]any{"text": " и обратно"})
	mouse("mousePressed", 1850, 1050, 1)
	mouse("mouseReleased", 1850, 1050, 0)
	edited := wait("tk:deck-text", map[string]any{"tpl": h1, "was": "Как агент ходит в ваши сервисы"})
	wait("tk:deck-deselect", nil)
	html, _ := edited["html"].(string)
	was, _ := edited["was"].(string)
	src, err := applyDeckPatch(deckBridgeBoard, DeckPatchRequest{Op: DeckPatchText, Tpl: int(h1), Was: &was, HTML: &html}, 1920)
	if err != nil || !strings.Contains(src, `<h1 data-block="Заголовок">Как агент ходит в ваши сервисы и обратно</h1>`) {
		t.Fatalf("text patch: %v\n%s", err, src)
	}

	// Interpolated text: the host is told it is the code's.
	since()
	cx, cy := point(".cell")
	mouse("mousePressed", cx, cy, 1)
	mouse("mouseReleased", cx, cy, 0)
	wait("tk:deck-dynamic", map[string]any{"tpl": tplOf("p", 1)})

	// A free block dragged: snapped to the 7% field, the end comes back and the runtime writes the move.
	since()
	fx, fy := point(".fig")
	mouse("mouseMoved", fx, fy, 0)
	mouse("mousePressed", fx, fy, 1)
	for i := 1; i <= 10; i++ {
		mouse("mouseMoved", fx-float64(i)*116.4, fy+float64(i)*3, 1)
	}
	mouse("mouseReleased", fx-1164, fy+30, 0)
	drag := wait("tk:deck-drag", map[string]any{"tpl": fig, "end": true})
	dx, _ := drag["x"].(float64)
	dy, _ := drag["y"].(float64)
	if dx != -1165.6 || dy != 30 {
		t.Fatalf("drag ended at %v, %v (want the left side on the 7%% field: -1165.6, 30)", dx, dy)
	}
	figWas, _ := drag["was"].(string)
	if drag["px"] != false {
		t.Fatalf("drag px %v", drag["px"])
	}
	src, err = applyDeckPatch(deckBridgeBoard, DeckPatchRequest{Op: DeckPatchMove, Tpl: int(fig), X: &dx, Y: &dy, Was: &figWas}, 1920)
	if err != nil || !strings.Contains(src, `<div class="fig" data-block="Схема" data-free="1" style="translate: -60.71cqw 1.56cqw">`) {
		t.Fatalf("move patch: %v\n%s", err, src)
	}
	wait("tk:deck-hover", map[string]any{"tpl": fig, "moved": true})

	// The host puts the block back (a live move to 0, 0), then a hit test, steps and keys.
	eval(`send({ type: 'tk:deck-move', tpl: ` + jsNum(fig) + `, x: 0, y: 0 })`)
	deadline = time.Now().Add(10 * time.Second)
	for eval(`document.getElementById('f').contentDocument.querySelector('.fig').style.translate === ''`) != true {
		if time.Now().After(deadline) {
			t.Fatal("the block was not put back")
		}
		time.Sleep(100 * time.Millisecond)
	}
	since()
	lx, ly := point(".list li")
	eval(`send({ type: 'tk:deck-hit', req: 7, x: ` + jsNum(lx) + `, y: ` + jsNum(ly) + ` })`)
	wait("tk:deck-hit", map[string]any{"req": 7, "label": "Список", "tpl": ul})
	eval(`send({ type: 'tk:deck', on: true, mode: 'show' }); send({ type: 'tk:deck-step', n: 1 })`)
	deadline = time.Now().Add(10 * time.Second)
	for eval(`(() => { const d = document.getElementById('f').contentDocument; const li = d.querySelectorAll('.list li'); return li[0].hasAttribute('data-step-on') && !li[1].hasAttribute('data-step-on') && getComputedStyle(li[1]).opacity !== '1'; })()`) != true {
		if time.Now().After(deadline) {
			t.Fatal("step 1 does not hide step 2")
		}
		time.Sleep(200 * time.Millisecond)
	}
	since()
	eval(`document.getElementById('f').contentWindow.focus()`)
	for _, k := range []struct{ key, code string }{{"ArrowRight", "ArrowRight"}, {"c", "KeyC"}, {"с", "KeyC"}} {
		send("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": k.key, "code": k.code})
		send("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": k.key, "code": k.code})
	}
	wait("tk:key", map[string]any{"key": "ArrowRight"})
	wait("tk:key", map[string]any{"key": "c"})

	// A touch held: the ring at 250 ms, the pick with its point at 730 ms.
	send("Emulation.setTouchEmulationEnabled", map[string]any{"enabled": true, "maxTouchPoints": 1})
	eval(`send({ type: 'tk:deck', on: true, mode: 'edit' })`)
	since()
	send("Input.dispatchTouchEvent", map[string]any{"type": "touchStart", "touchPoints": []any{map[string]any{"x": 1500, "y": 300}}})
	wait("tk:deck-press", map[string]any{"phase": "ring", "x": 1500, "y": 300})
	picked := wait("tk:picked", map[string]any{"point": []any{1500, 300}})
	send("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []any{}})
	if picked["tag"] == nil {
		t.Fatalf("picked %v", picked)
	}
	// Moved away: cancelled.
	since()
	send("Input.dispatchTouchEvent", map[string]any{"type": "touchStart", "touchPoints": []any{map[string]any{"x": 1500, "y": 300}}})
	wait("tk:deck-press", map[string]any{"phase": "ring"})
	send("Input.dispatchTouchEvent", map[string]any{"type": "touchMove", "touchPoints": []any{map[string]any{"x": 1530, "y": 300}}})
	wait("tk:deck-press", map[string]any{"phase": "cancel"})
	send("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []any{}})
	time.Sleep(800 * time.Millisecond)
	if got, _ := eval(`window.got.slice(` + jsNum(float64(mark)) + `).filter((m) => m.type === 'tk:picked').length`).(float64); got != 0 {
		t.Fatal("a cancelled hold still picked")
	}

	// Off: nothing is editable any more.
	eval(`send({ type: 'tk:deck', on: false, mode: 'edit' })`)
	deadline = time.Now().Add(10 * time.Second)
	for eval(`document.getElementById('f').contentDocument.querySelectorAll('[data-tk-deck-text]').length === 0 && !document.getElementById('f').contentDocument.documentElement.hasAttribute('data-tk-deck')`) != true {
		if time.Now().After(deadline) {
			t.Fatal("the deck stayed on")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func jsNum(f float64) string { b, _ := json.Marshal(f); return string(b) }

package design

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	docProject = "p1"
	docPage1   = "01ARYZ6S41TSV4RRFFQ69G5FAV"
	docPage2   = "01ARYZ6S41TSV4RRFFQ69G5FAW"
)

// docRig is a project of two boards under meta/, the way a deck lies, and the history that watches it.
func docRig(t *testing.T) (*Library, string, string) {
	t.Helper()
	l, library := historyLibrary(t)
	dir := filepath.Join(library, docProject)
	for _, d := range []string{filepath.Join(dir, ProjectDirname), filepath.Join(dir, MetaDirname, MetaPagesDirname)} {
		if err := os.MkdirAll(d, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(dir, ProjectDirname, "Offer.dc.html")
	files := map[string]string{
		first: deckBoardOf(`
<main class="slide">
  <h1 data-hl-field="title">Cashback</h1>
  <p class="big" data-hl-field="percent" style="font-size: 12px">5%</p>
  <a data-hl-field="link" data-hl-attr="href" href="https://old.example/">Open</a>
  <ul><li>One</li><li>Two</li></ul>
  <img src="a.png">
</main>
`),
		filepath.Join(dir, ProjectDirname, "Two.dc.html"):                   deckBoardOf("\n<main><h2>Second</h2></main>\n"),
		filepath.Join(dir, MetaDirname, MetaProjectFile):                    `{"title":"Deck","kind":"slides"}`,
		filepath.Join(dir, MetaDirname, MetaPagesDirname, docPage1+".json"): `{"id":"` + docPage1 + `","title":"Offer","file":"Offer.dc.html","w":1920,"h":1080,"order":1}`,
		filepath.Join(dir, MetaDirname, MetaPagesDirname, docPage2+".json"): `{"id":"` + docPage2 + `","title":"Two","file":"Two.dc.html","w":1920,"h":1080,"order":2}`,
	}
	for file, body := range files {
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.snapshotNow("rt", nil, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	return l, first, dir
}

func nodeIDs(nodes []DocNode) string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.Tag + "#" + n.ID[27:]
	}
	return strings.Join(ids, " ")
}

func TestDocReadsNodes(t *testing.T) {
	l, _, _ := docRig(t)
	nodes, err := l.DocNodes(docProject, docPage1)
	if err != nil {
		t.Fatal(err)
	}
	if got := nodeIDs(nodes); got != "main#0 h1#1 p#2 a#3 ul#4 li#5 li#6 img#7" {
		t.Fatalf("nodes: %s", got)
	}
	h1 := nodes[1]
	if h1.Field != "title" || h1.Text != "Cashback" || !h1.Editable || h1.Selector != `[data-dc-tpl="1"]` || h1.BoardW != 1920 {
		t.Errorf("h1: %+v", h1)
	}
	if h1.Words != 1 || nodes[0].Words != 1 {
		t.Errorf("a node counts its own words: %d %d", h1.Words, nodes[0].Words)
	}
	if nodes[0].Own != "Open" {
		t.Errorf("a block's own words leave out its block children: %q", nodes[0].Own)
	}

	got, err := l.DocQuery(docProject, "[data-hl-field]", "")
	if err != nil || nodeIDs(got) != "h1#1 p#2 a#3" {
		t.Fatalf("fields: %s %v", nodeIDs(got), err)
	}
	for q, want := range map[string]string{
		"li":                           "li#5 li#6",
		"ul > li, h1":                  "h1#1 li#5 li#6",
		"main li":                      "li#5 li#6",
		"main > li":                    "",
		".big":                         "p#2",
		"p.big[data-hl-field=percent]": "p#2",
		"a[href^='https://old']":       "a#3",
		"h2":                           "h2#1",
	} {
		page := ""
		if q == "h2" {
			page = docPage2
		}
		nodes, err := l.DocQuery(docProject, q, page)
		if err != nil || nodeIDs(nodes) != want {
			t.Errorf("%q: %s %v, want %s", q, nodeIDs(nodes), err, want)
		}
	}
	if _, err := l.DocQuery(docProject, "li:first-child", ""); err == nil {
		t.Error("a pseudo-class is refused")
	}
	fields, err := l.DocFields(docProject)
	if err != nil || len(fields) != 3 || fields[1].Value != "5%" || fields[2].Value != "https://old.example/" || fields[2].Attr != "href" {
		t.Fatalf("fields: %+v %v", fields, err)
	}
	one, err := l.DocNodeByID(docProject, docPage1+":2")
	if err != nil || one.Field != "percent" || !strings.HasPrefix(one.Fingerprint, "p:") {
		t.Fatalf("node: %+v %v", one, err)
	}
	if _, err := l.DocNodeByID(docProject, docPage1+":99"); err == nil {
		t.Error("no such node")
	}
}

func TestDocPatchIsOneHistoryEntry(t *testing.T) {
	l, board, _ := docRig(t)
	was := readFile(t, board)
	res, err := l.DocPatch(docProject, []DocOp{
		{Op: "setField", Field: "percent", Value: "7%"},
		{Op: "setField", Field: "link", Value: "https://new.example/?a=1&b=2"},
		{Op: "setText", Node: docPage1 + ":1", Value: "A & B <c>"},
		{Op: "setStyle", Node: docPage1 + ":2", Style: map[string]*string{"font-size": strp("24px"), "color": strp("red")}},
		{Op: "setAttr", Node: docPage1 + ":7", Name: "alt", Value: "pic"},
		{Op: "insert", Node: docPage1 + ":4", Position: "end", HTML: `<li class="n">Three</li>`},
		{Op: "remove", Node: docPage1 + ":6"},
	}, DocActor{Subject: "Cashback"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Rev == 0 || len(res.Pages) != 1 || res.Pages[0] != docPage1 {
		t.Fatalf("result: %+v", res)
	}
	got := readFile(t, board)
	for _, want := range []string{
		`<h1 data-hl-field="title">A &amp; B &lt;c&gt;</h1>`,
		`<p class="big" data-hl-field="percent" style="font-size: 24px; color: red">7%</p>`,
		`href="https://new.example/?a=1&amp;b=2"`,
		`<li>One</li><li class="n">Three</li></ul>`,
		`<img src="a.png" alt="pic">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Two</li>") {
		t.Errorf("the removed item is there:\n%s", got)
	}
	if got == was {
		t.Fatal("nothing written")
	}

	file := "Offer.dc.html"
	h, err := l.readHistory(docProject, &file, 10)
	if err != nil || len(h.entries) != 2 {
		t.Fatalf("history: %+v %v", h.entries, err)
	}
	// What was there is the snapshot under it; the patch is one version above, named for the app.
	if !strings.Contains(h.entries[0].Subject, "Cashback") {
		t.Errorf("the entry does not name the app: %q", h.entries[0].Subject)
	}
}

func TestDocPatchRefusals(t *testing.T) {
	l, board, _ := docRig(t)
	was := readFile(t, board)
	code := func(err error) string {
		var de *DocError
		if errors.As(err, &de) {
			return de.Code
		}
		return "?"
	}
	cases := []struct {
		name string
		ops  []DocOp
		want string
	}{
		{"no ops", nil, DocErrInvalid},
		{"unknown op", []DocOp{{Op: "spin", Node: docPage1 + ":1"}}, DocErrInvalid},
		{"no such node", []DocOp{{Op: "setText", Node: docPage1 + ":99", Value: "x"}}, DocErrChanged},
		{"stale fingerprint", []DocOp{{Op: "setText", Node: docPage1 + ":1", FP: "h1:zzz", Value: "x"}}, DocErrChanged},
		{"bad id", []DocOp{{Op: "setText", Node: "nope", Value: "x"}}, DocErrInvalid},
		{"event handler", []DocOp{{Op: "setAttr", Node: docPage1 + ":1", Name: "onclick", Value: "x()"}}, DocErrInvalid},
		{"script address", []DocOp{{Op: "setAttr", Node: docPage1 + ":3", Name: "href", Value: "javascript:alert(1)"}}, DocErrInvalid},
		{"template expression", []DocOp{{Op: "setAttr", Node: docPage1 + ":1", Name: "title", Value: "{{ x }}"}}, DocErrInvalid},
		{"style url", []DocOp{{Op: "setStyle", Node: docPage1 + ":1", Style: map[string]*string{"background": strp("url(x)")}}}, DocErrInvalid},
		{"script tag", []DocOp{{Op: "insert", Node: docPage1 + ":4", Position: "end", HTML: `<script>x</script>`}}, DocErrInvalid},
		{"handler in insert", []DocOp{{Op: "insert", Node: docPage1 + ":4", Position: "end", HTML: `<li onclick="x()">a</li>`}}, DocErrInvalid},
		{"unbound field", []DocOp{{Op: "setField", Field: "nope", Value: "x"}}, DocErrNotFound},
		{"text into a list", []DocOp{{Op: "setText", Node: docPage1 + ":4", Value: "x"}}, DocErrDynamic},
		{"overlap", []DocOp{{Op: "setText", Node: docPage1 + ":4", Value: "x"}, {Op: "setText", Node: docPage1 + ":5", Value: "y"}}, DocErrDynamic},
	}
	for _, c := range cases {
		_, err := l.DocPatch(docProject, c.ops, DocActor{Subject: "x"}, time.Now())
		if err == nil || code(err) != c.want {
			t.Errorf("%s: %v (%s), want %s", c.name, err, code(err), c.want)
		}
	}
	if readFile(t, board) != was {
		t.Error("a refused patch wrote something")
	}
}

// A patch is all or nothing: one op that cannot be placed leaves the other pages as they were.
func TestDocPatchAllOrNothing(t *testing.T) {
	l, board, dir := docRig(t)
	two := filepath.Join(dir, ProjectDirname, "Two.dc.html")
	wasOne, wasTwo := readFile(t, board), readFile(t, two)
	_, err := l.DocPatch(docProject, []DocOp{
		{Op: "setText", Node: docPage1 + ":1", Value: "new"},
		{Op: "setText", Node: docPage2 + ":99", Value: "new"},
	}, DocActor{Subject: "x"}, time.Now())
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if readFile(t, board) != wasOne || readFile(t, two) != wasTwo {
		t.Error("a half patch was written")
	}
}

// A board a chat holds is not written over; the chat itself may.
func TestDocPatchRespectsClaims(t *testing.T) {
	l, board, _ := docRig(t)
	now := time.Now()
	res, err := l.ClaimBoard(docProject, "Offer", ClaimOwner{User: "ann", Runtime: "rt", TabID: "tab-a", ChatTitle: "Agent A"}, now.UnixMilli())
	if err != nil || res.Held != nil {
		t.Fatalf("claim: %+v %v", res, err)
	}
	ops := []DocOp{{Op: "setText", Node: docPage1 + ":1", Value: "mine"}}
	_, err = l.DocPatch(docProject, ops, DocActor{Subject: "App"}, now)
	var de *DocError
	if !errors.As(err, &de) || de.Code != DocErrClaimed || !strings.Contains(de.Msg, "Agent A") {
		t.Fatalf("a claimed board: %v", err)
	}
	if _, err := l.DocPatch(docProject, ops, DocActor{Subject: "App", Tab: "tab-b"}, now); err == nil {
		t.Fatal("another chat's claim holds")
	}
	if _, err := l.DocPatch(docProject, ops, DocActor{Subject: "App", Tab: "tab-a"}, now); err != nil {
		t.Fatalf("the holder writes: %v", err)
	}
	if !strings.Contains(readFile(t, board), ">mine</h1>") {
		t.Error("not written")
	}
}

// With an app's lock on, a hand edit of an element no field is bound to is refused; a bound one goes through.
func TestHandEditLockedToBoundElements(t *testing.T) {
	l, board, _ := docRig(t)
	l.LockUnbound = func(p string) bool { return p == docProject }
	// <li> (5) is not bound; <h1> (1) is.
	_, _, err := l.patchDeckBoard(DeckPatchRequest{Artifact: docProject, Page: docPage1, Tpl: 5, Op: DeckPatchText, Was: strp("One"), HTML: strp("Uno")}, time.Now())
	var pe *deckPatchErr
	if !errors.As(err, &pe) || pe.code != DeckPatchLocked || pe.status != 403 {
		t.Fatalf("unbound: %v", err)
	}
	if _, _, err := l.patchDeckBoard(DeckPatchRequest{Artifact: docProject, Page: docPage1, Tpl: 1, Op: DeckPatchText, Was: strp("Cashback"), HTML: strp("Back")}, time.Now()); err != nil {
		t.Fatalf("bound: %v", err)
	}
	if !strings.Contains(readFile(t, board), ">Back</h1>") {
		t.Error("not written")
	}
	l.LockUnbound = nil
	if _, _, err := l.patchDeckBoard(DeckPatchRequest{Artifact: docProject, Page: docPage1, Tpl: 5, Op: DeckPatchText, Was: strp("One"), HTML: strp("Uno")}, time.Now()); err != nil {
		t.Fatalf("no lock: %v", err)
	}
}

// The board template of the Cashback example (docs/apps/cashback) is a board the Document API reads: its elements are
// bound, the app's values are written into them, and the element it leaves unbound has no field.
func TestCashbackTemplateIsABoundBoard(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "docs", "apps", "cashback", "templates", "cashback-card", "board.dc.html"))
	if err != nil {
		t.Fatal(err)
	}
	l, board, _ := docRig(t)
	if err := os.WriteFile(board, src, 0o644); err != nil {
		t.Fatal(err)
	}
	fields, err := l.DocFields(docProject)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range fields {
		got[f.Field] = f.Attr
	}
	for field, attr := range map[string]string{"title": "", "percent": "", "limit": "", "note": "", "link": "href"} {
		if a, ok := got[field]; !ok || a != attr {
			t.Errorf("field %s: %q %v (all %v)", field, a, ok, got)
		}
	}
	if _, err := l.DocPatch(docProject, []DocOp{
		{Op: "setField", Field: "percent", Value: "9"},
		{Op: "setField", Field: "link", Value: "https://example.org/new"},
	}, DocActor{Subject: "Cashback"}, time.Now()); err != nil {
		t.Fatalf("patch: %v", err)
	}
	out, _ := os.ReadFile(board)
	if !strings.Contains(string(out), `data-hl-field="percent">9</span>`) || !strings.Contains(string(out), `href="https://example.org/new"`) {
		t.Errorf("the values are not in the board:\n%s", out)
	}
}

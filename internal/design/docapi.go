package design

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The Document API's part in the library (internal/apps serves it to an app's view and to the agent): reading a
// project's boards as nodes, and writing them. A node is an element of a board's template, named by its page and the
// engine's number (data-dc-tpl) — "<page id>:<tpl>" — and, when the caller has it, the fingerprint the board gives it
// (deckFingerprint), so a patch lands on the element the caller saw or on nothing.
//
// A patch is read against the board as it was before the patch: every target is found in one parse and the edits are made
// from the end of the source to its start, so one op never moves the number of another. It is one history entry: a
// snapshot of what was there, the write, a snapshot of the result (deckSnapshot, as a hand edit does).

// DocError is a refusal of the Document API; Code is one of the DocErr words.
type DocError struct {
	Code string
	Msg  string
}

func (e *DocError) Error() string { return e.Msg }

// The codes of a refused call.
const (
	DocErrNotFound = "not_found"
	DocErrInvalid  = "invalid"
	DocErrChanged  = "changed"
	DocErrDynamic  = "dynamic"
	DocErrClaimed  = "claimed"
	DocErrLocked   = "locked"
	DocErrFailed   = "failed"
)

func docErr(code, en, ru string) error {
	return &DocError{Code: code, Msg: locale.Pick(en, ru)}
}

func docInvalid(en, ru string) error { return docErr(DocErrInvalid, en, ru) }

// DocNode is an element of a board as the Document API shows it.
type DocNode struct {
	// ID is "<page id>:<tpl>".
	ID          string `json:"id"`
	Page        string `json:"page"`
	PageTitle   string `json:"pageTitle,omitempty"`
	Tpl         int    `json:"tpl"`
	Fingerprint string `json:"fp"`
	// Selector finds the element in the board as the browser shows it.
	Selector string `json:"selector"`
	Tag      string `json:"tag"`
	// Field is the element's data-hl-field, the binding an app edits it through.
	Field string            `json:"field,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
	// Text is the element's words, clipped; Own the words that are its own (not a block child's), for checks.
	Text string `json:"text,omitempty"`
	Own  string `json:"own,omitempty"`
	// Words counts the words of Own before it is clipped (what a maxWords rule counts), so a panel need not count a clipped text.
	Words int    `json:"words,omitempty"`
	Style string `json:"style,omitempty"`
	// Editable: the element's words are plain static text a setText can change.
	Editable bool `json:"editable,omitempty"`
	// BoardW is the page's width in px (0 unknown), for units that follow it.
	BoardW int `json:"boardW,omitempty"`
}

const (
	docTextMax = 300
	docOwnMax  = 4000
	// DocNodesMax bounds what one call lists.
	DocNodesMax = 4000
	docOpsMax   = 200
)

// DocPageRef is a page as the Document API lists it.
type DocPageRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	File  string `json:"file"`
	W     *int   `json:"w"`
	H     *int   `json:"h"`
}

// DocPages are the project's pages in their order. A project without meta/ has no pages model to address nodes by.
func (l *Library) DocPages(project string) ([]Page, error) {
	if !IsLibraryID(project) || !isRealDir(l.ProjectDir(project)) {
		return nil, docErr(DocErrNotFound, "No such project", "Нет такого проекта")
	}
	m := l.ReadModel(project)
	if m == nil {
		return nil, docErr(DocErrInvalid, "This project has no pages model (meta/): the Document API needs one", "У проекта нет модели страниц (meta/): Document API без неё не работает")
	}
	return m.Pages, nil
}

// DocRev is the time of the project's newest board, in ms: it changes when any board is written.
func (l *Library) DocRev(project string) int64 {
	var rev int64
	m := l.ReadModel(project)
	if m == nil {
		return 0
	}
	for _, p := range m.Pages {
		if st, err := os.Stat(filepath.Join(l.ProjectDir(project), pageFileOf(p.File))); err == nil && st.ModTime().UnixMilli() > rev {
			rev = st.ModTime().UnixMilli()
		}
	}
	return rev
}

func (l *Library) docBoard(project string, p Page) (string, *dcBoard, error) {
	file := pageFileOf(p.File)
	if !IsBoardFile(file) {
		return "", nil, docInvalid("The page "+p.Title+" is not a board the Document API reads", "Страницу «"+p.Title+"» Document API не читает")
	}
	raw, err := readFileLimited(filepath.Join(l.ProjectDir(project), file), deckBoardMax)
	if err != nil {
		return "", nil, docErr(DocErrNotFound, "No such board", "Нет такой доски")
	}
	b, ok := parseDcBoard(string(raw))
	if !ok {
		return "", nil, docInvalid("The board "+p.Title+" has no template the Document API can number", "В доске «"+p.Title+"» нет шаблона, который Document API может пронумеровать")
	}
	return string(raw), b, nil
}

// nodeOf describes one element.
func nodeOf(p Page, e dcElem) DocNode {
	n := e.node
	node := DocNode{
		ID: p.ID + ":" + strconv.Itoa(e.tpl), Page: p.ID, PageTitle: p.Title, Tpl: e.tpl, Fingerprint: deckFingerprint(n),
		Selector: `[data-dc-tpl="` + strconv.Itoa(e.tpl) + `"]`, Tag: tagOf(n),
	}
	if p.W != nil {
		node.BoardW = *p.W
	}
	for _, a := range n.Attr {
		if a.Namespace != "" || a.Key == deckTagMark {
			continue
		}
		switch {
		case a.Key == "style":
			node.Style = clipRunes(a.Val, 600)
		case a.Key == "id" || a.Key == "class" || a.Key == "data-block" || strings.HasPrefix(a.Key, "data-hl-"):
			if node.Attrs == nil {
				node.Attrs = map[string]string{}
			}
			node.Attrs[a.Key] = clipRunes(a.Val, 200)
		}
	}
	node.Field = node.Attrs["data-hl-field"]
	node.Text = clipRunes(collapseSpace(strings.TrimSpace(nodeText(n, false))), docTextMax)
	own := collapseSpace(strings.TrimSpace(nodeText(n, true)))
	node.Words = len(strings.Fields(own))
	node.Own = clipRunes(own, docOwnMax)
	node.Editable = e.tag >= 0 && textStatic(n)
	return node
}

func clipRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

// docInline are the elements whose words count as the words of the element around them.
var docInline = map[string]bool{"b": true, "strong": true, "i": true, "em": true, "u": true, "s": true, "br": true, "span": true,
	"a": true, "mark": true, "small": true, "sub": true, "sup": true, "code": true, "abbr": true, "q": true}

// nodeText is the element's words; own keeps to the text of the element and of the inline elements in it.
func nodeText(n *html.Node, own bool) string {
	var out strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				out.WriteString(c.Data)
			case html.ElementNode:
				if c.Namespace != "" && c.Data != "svg" {
					continue
				}
				if (deckRawText[c.Data] && c.Namespace == "") || c.DataAtom == atom.Template {
					continue
				}
				if own && !docInline[c.Data] {
					out.WriteByte(' ')
					continue
				}
				if c.Data == "br" {
					out.WriteByte(' ')
				}
				walk(c)
			}
		}
	}
	walk(n)
	return out.String()
}

// ParseDocNodeID reads "<page id>:<tpl>".
func ParseDocNodeID(id string) (page string, tpl int, ok bool) {
	p, n, found := strings.Cut(id, ":")
	if !found || !IsPageID(p) {
		return "", 0, false
	}
	t, err := strconv.Atoi(n)
	if err != nil || t < 0 || strconv.Itoa(t) != n {
		return "", 0, false
	}
	return p, t, true
}

func (l *Library) docPage(project, ref string) (Page, error) {
	m := l.ReadModel(project)
	if m == nil {
		return Page{}, docInvalid("This project has no pages model", "У проекта нет модели страниц")
	}
	p, err := ResolvePage(m, ref)
	if err != nil {
		return Page{}, docErr(DocErrNotFound, "No such page: "+ref, "Нет такой страницы: "+ref)
	}
	return p, nil
}

// DocNodes lists the elements of a page (page "" is every page) in the board's order.
func (l *Library) DocNodes(project, page string) ([]DocNode, error) {
	pages, err := l.DocPages(project)
	if err != nil {
		return nil, err
	}
	if page != "" {
		p, err := l.docPage(project, page)
		if err != nil {
			return nil, err
		}
		pages = []Page{p}
	}
	var out []DocNode
	for _, p := range pages {
		if !IsBoardFile(pageFileOf(p.File)) {
			continue
		}
		_, b, err := l.docBoard(project, p)
		if err != nil {
			if page != "" {
				return nil, err
			}
			continue
		}
		for _, e := range b.elems {
			if e.tag < 0 {
				continue
			}
			if out = append(out, nodeOf(p, e)); len(out) >= DocNodesMax {
				return out, nil
			}
		}
	}
	return out, nil
}

// DocNodeByID is the element a node id names, as it is now.
func (l *Library) DocNodeByID(project, id string) (DocNode, error) {
	pageID, tpl, ok := ParseDocNodeID(id)
	if !ok {
		return DocNode{}, docInvalid("A node id is <page>:<number>", "Номер узла — <страница>:<номер>")
	}
	p, err := l.docPage(project, pageID)
	if err != nil {
		return DocNode{}, err
	}
	_, b, err := l.docBoard(project, p)
	if err != nil {
		return DocNode{}, err
	}
	e := b.elem(tpl)
	if e == nil {
		return DocNode{}, docErr(DocErrNotFound, "No such node: "+id, "Нет такого узла: "+id)
	}
	return nodeOf(p, *e), nil
}

// DocQuery are the elements of a page (page "" is every page) that match a selector.
func (l *Library) DocQuery(project, selector, page string) ([]DocNode, error) {
	chains, err := parseSelector(selector)
	if err != nil {
		return nil, docInvalid(err.Error(), "Селектор не из тех, что читает API: тип, #id, .класс, [атрибут], пробел и >")
	}
	pages, err := l.DocPages(project)
	if err != nil {
		return nil, err
	}
	if page != "" {
		p, err := l.docPage(project, page)
		if err != nil {
			return nil, err
		}
		pages = []Page{p}
	}
	var out []DocNode
	for _, p := range pages {
		if !IsBoardFile(pageFileOf(p.File)) {
			continue
		}
		_, b, err := l.docBoard(project, p)
		if err != nil {
			if page != "" {
				return nil, err
			}
			continue
		}
		for _, e := range b.elems {
			if e.tag < 0 {
				continue
			}
			for _, ch := range chains {
				if ch.matches(e.node) {
					if out = append(out, nodeOf(p, e)); len(out) >= DocNodesMax {
						return out, nil
					}
					break
				}
			}
		}
	}
	return out, nil
}

// DocField is a binding: the field's name, its value now and the elements it is on.
type DocField struct {
	Field string `json:"field"`
	Value string `json:"value"`
	// Attr is the attribute the field sets (data-hl-attr); "" when it sets the element's words.
	Attr  string   `json:"attr,omitempty"`
	Nodes []string `json:"nodes"`
}

// DocFields are the data bindings of the project's boards, in the order they appear.
func (l *Library) DocFields(project string) ([]DocField, error) {
	nodes, err := l.DocQuery(project, "[data-hl-field]", "")
	if err != nil {
		return nil, err
	}
	var out []DocField
	at := map[string]int{}
	for _, n := range nodes {
		if i, ok := at[n.Field]; ok {
			out[i].Nodes = append(out[i].Nodes, n.ID)
			continue
		}
		f := DocField{Field: n.Field, Attr: n.Attrs["data-hl-attr"], Nodes: []string{n.ID}}
		f.Value = n.Text
		if f.Attr != "" {
			f.Value = l.docAttrValue(project, n, f.Attr)
		}
		at[n.Field] = len(out)
		out = append(out, f)
	}
	return out, nil
}

func (l *Library) docAttrValue(project string, n DocNode, attr string) string {
	p, err := l.docPage(project, n.Page)
	if err != nil {
		return ""
	}
	_, b, err := l.docBoard(project, p)
	if err != nil {
		return ""
	}
	if e := b.elem(n.Tpl); e != nil {
		return attrValue(e.node, attr)
	}
	return ""
}

// DocOp is one change of a patch.
type DocOp struct {
	// Op is setText, setStyle, setAttr, insert, remove or setField.
	Op   string `json:"op"`
	Node string `json:"node,omitempty"`
	// FP is the fingerprint the caller saw the node with.
	FP    string `json:"fp,omitempty"`
	Field string `json:"field,omitempty"`
	Name  string `json:"name,omitempty"`
	// Value is a text, a number or a switch (setText, setAttr, setField); null removes an attribute.
	Value any `json:"value,omitempty"`
	// Style is the properties to put in (a nil value takes one out).
	Style map[string]*string `json:"style,omitempty"`
	// Position is before, after, start or end (insert).
	Position string `json:"position,omitempty"`
	HTML     string `json:"html,omitempty"`
}

// DocActor is who writes: the name the history keeps, and the chat tab whose claims do not stop the write.
type DocActor struct {
	Subject string
	Tab     string
}

// DocPatched is what a patch did.
type DocPatched struct {
	Rev   int64    `json:"rev"`
	Pages []string `json:"pages"`
	Edits int      `json:"edits"`
}

var (
	docAttrNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_:-]{0,60}$`)
	docURLAttr    = map[string]bool{"href": true, "src": true, "xlink:href": true, "poster": true, "action": true, "formaction": true}
)

// docAttrRefused are the attributes an app may not set: scripts and the board's own machinery.
func docAttrRefused(name string) bool {
	l := strings.ToLower(name)
	return strings.HasPrefix(l, "on") || l == "style" || l == "srcdoc" || strings.HasPrefix(l, "data-dc-") || strings.HasPrefix(l, "data-tk-")
}

func docValueText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	case int:
		return strconv.Itoa(x), true
	}
	return "", false
}

func safeURLValue(v string) bool {
	t := strings.ToLower(strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, v)))
	switch {
	case strings.HasPrefix(t, "javascript:"), strings.HasPrefix(t, "vbscript:"), strings.HasPrefix(t, "file:"):
		return false
	case strings.HasPrefix(t, "data:"):
		return strings.HasPrefix(t, "data:image/") && !strings.HasPrefix(t, "data:image/svg")
	}
	return true
}

type attrChange struct {
	name  string
	value *string
}

// docEdit is a replacement of src[from:to] by text.
type docEdit struct {
	from, to int
	text     string
	seq      int
}

// docTarget is what the ops want of one element.
type docTarget struct {
	e       *dcElem
	attrs   []attrChange
	style   map[string]*string
	text    *string
	remove  bool
	inserts []docInsert
	seq     int
}

type docInsert struct {
	position, html string
	seq            int
}

// DocPatch writes ops into the project's boards: all of them or none.
func (l *Library) DocPatch(project string, ops []DocOp, who DocActor, now time.Time) (DocPatched, error) {
	if len(ops) == 0 || len(ops) > docOpsMax {
		return DocPatched{}, docInvalid("A patch has 1 to "+strconv.Itoa(docOpsMax)+" ops", "В правке от 1 до "+strconv.Itoa(docOpsMax)+" операций")
	}
	pages, err := l.DocPages(project)
	if err != nil {
		return DocPatched{}, err
	}
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()

	type pageWork struct {
		page    Page
		src     string
		board   *dcBoard
		targets map[int]*docTarget
		order   []int
	}
	work := map[string]*pageWork{}
	var order []string
	workOf := func(p Page) (*pageWork, error) {
		if w := work[p.ID]; w != nil {
			return w, nil
		}
		src, b, err := l.docBoard(project, p)
		if err != nil {
			return nil, err
		}
		w := &pageWork{page: p, src: src, board: b, targets: map[int]*docTarget{}}
		work[p.ID] = w
		order = append(order, p.ID)
		return w, nil
	}
	target := func(p Page, tpl int, fp string) (*docTarget, *pageWork, error) {
		w, err := workOf(p)
		if err != nil {
			return nil, nil, err
		}
		var e *dcElem
		if fp != "" {
			e = w.board.byFingerprint(tpl, fp)
		} else {
			e = w.board.elem(tpl)
		}
		if e == nil {
			return nil, nil, docErr(DocErrChanged, "The node changed since it was read: read it again", "Узел поменялся, пока его читали: прочитайте ещё раз")
		}
		t := w.targets[e.tpl]
		if t == nil {
			t = &docTarget{e: e}
			w.targets[e.tpl] = t
			w.order = append(w.order, e.tpl)
		}
		return t, w, nil
	}
	pageOf := func(id string) (Page, int, error) {
		pageID, tpl, ok := ParseDocNodeID(id)
		if !ok {
			return Page{}, 0, docInvalid("A node id is <page>:<number>", "Номер узла — <страница>:<номер>")
		}
		p, err := l.docPage(project, pageID)
		return p, tpl, err
	}

	for seq, op := range ops {
		switch op.Op {
		case "setField":
			if op.Field == "" || len(op.Field) > 80 {
				return DocPatched{}, docInvalid("setField needs a field", "setField требует поле")
			}
			text, ok := docValueText(op.Value)
			if !ok || len(text) > 20000 {
				return DocPatched{}, docInvalid("A field's value is a text, a number or a switch", "Значение поля — текст, число или переключатель")
			}
			found := 0
			for _, p := range pages {
				if !IsBoardFile(pageFileOf(p.File)) {
					continue
				}
				w, err := workOf(p)
				if err != nil {
					continue
				}
				for i := range w.board.elems {
					e := &w.board.elems[i]
					if e.tag < 0 || attrValue(e.node, "data-hl-field") != op.Field {
						continue
					}
					t, _, err := target(p, e.tpl, "")
					if err != nil {
						return DocPatched{}, err
					}
					found++
					if attr := attrValue(e.node, "data-hl-attr"); attr != "" {
						if err := t.setAttr(attr, &text); err != nil {
							return DocPatched{}, err
						}
					} else {
						t.text = &text
					}
				}
			}
			if found == 0 {
				return DocPatched{}, docErr(DocErrNotFound, "No element is bound to the field "+op.Field, "Нет элемента, привязанного к полю "+op.Field)
			}
			continue
		case "setText", "setStyle", "setAttr", "insert", "remove":
		default:
			return DocPatched{}, docInvalid("Unknown op "+strconv.Quote(op.Op), "Неизвестная операция "+strconv.Quote(op.Op))
		}
		p, tpl, err := pageOf(op.Node)
		if err != nil {
			return DocPatched{}, err
		}
		t, _, err := target(p, tpl, op.FP)
		if err != nil {
			return DocPatched{}, err
		}
		switch op.Op {
		case "setText":
			text, ok := docValueText(op.Value)
			if !ok || len(text) > 20000 {
				return DocPatched{}, docInvalid("setText takes a text", "setText принимает текст")
			}
			t.text = &text
		case "setStyle":
			if len(op.Style) == 0 || len(op.Style) > 16 {
				return DocPatched{}, docInvalid("No style to set", "Нечего менять в стиле")
			}
			if t.style == nil {
				t.style = map[string]*string{}
			}
			for prop, v := range op.Style {
				if !cssPropRe.MatchString(prop) || prop == "translate" {
					return DocPatched{}, docInvalid("Unknown style property", "Неизвестное свойство стиля")
				}
				if v != nil && (!cssValueRe.MatchString(*v) || strings.Contains(strings.ToLower(*v), "url(")) {
					return DocPatched{}, docInvalid("Unusable style value", "Такое значение стиля не подходит")
				}
				t.style[prop] = v
			}
		case "setAttr":
			if !docAttrNameRe.MatchString(op.Name) || docAttrRefused(op.Name) {
				return DocPatched{}, docInvalid("This attribute cannot be set: "+op.Name, "Этот атрибут нельзя менять: "+op.Name)
			}
			var value *string
			if op.Value != nil {
				text, ok := docValueText(op.Value)
				if !ok {
					return DocPatched{}, docInvalid("An attribute's value is a text, a number or a switch", "Значение атрибута — текст, число или переключатель")
				}
				value = &text
			}
			if err := t.setAttr(op.Name, value); err != nil {
				return DocPatched{}, err
			}
		case "remove":
			t.remove = true
		case "insert":
			switch op.Position {
			case "before", "after", "start", "end":
			default:
				return DocPatched{}, docInvalid("insert's position is before, after, start or end", "Позиция вставки — before, after, start или end")
			}
			clean, err := docCleanInsert(op.HTML)
			if err != nil {
				return DocPatched{}, err
			}
			t.inserts = append(t.inserts, docInsert{position: op.Position, html: clean, seq: seq})
		}
		if t.seq == 0 {
			t.seq = seq + 1
		}
	}

	// Edits per page, then the claims, then the write.
	type result struct {
		w    *pageWork
		next string
		n    int
	}
	var results []result
	for _, id := range order {
		w := work[id]
		var edits []docEdit
		for _, tpl := range w.order {
			es, err := w.targets[tpl].edits(w.board)
			if err != nil {
				return DocPatched{}, err
			}
			edits = append(edits, es...)
		}
		sort.SliceStable(edits, func(i, j int) bool {
			if edits[i].from != edits[j].from {
				return edits[i].from < edits[j].from
			}
			return edits[i].seq < edits[j].seq
		})
		var out strings.Builder
		at := 0
		for _, ed := range edits {
			if ed.from < at {
				return DocPatched{}, docInvalid("Two ops change the same part of a board: join them", "Две операции меняют одно место доски: объедините их")
			}
			out.WriteString(w.src[at:ed.from])
			out.WriteString(ed.text)
			at = ed.to
		}
		out.WriteString(w.src[at:])
		if out.String() != w.src {
			results = append(results, result{w: w, next: out.String(), n: len(edits)})
		}
	}
	res := DocPatched{Pages: []string{}}
	for _, r := range results {
		if err := l.docClaimed(project, r.w.page, who, now); err != nil {
			return DocPatched{}, err
		}
	}
	if len(results) > 0 {
		runtime := ""
		var editors []string
		if id, err := l.Identity(); err == nil {
			runtime = id.Runtime
			if id.Person != "" {
				editors = []string{id.Person}
			}
		}
		l.deckSnapshot(runtime, editors, locale.Pick("Before an edit by "+who.Subject, "Перед правкой приложения «"+who.Subject+"»"), now)
		for _, r := range results {
			path := filepath.Join(l.ProjectDir(project), pageFileOf(r.w.page.File))
			// The agent may have written the board since it was read: the patch goes over what it was made against only.
			if again, err := readFileLimited(path, deckBoardMax); err != nil || string(again) != r.w.src {
				return DocPatched{}, docErr(DocErrChanged, "The board changed while the patch was made: read it again", "Доска поменялась, пока готовилась правка: прочитайте её ещё раз")
			}
		}
		for _, r := range results {
			if _, err := writeIfChanged(filepath.Join(l.ProjectDir(project), pageFileOf(r.w.page.File)), []byte(r.next)); err != nil {
				return DocPatched{}, docErr(DocErrFailed, "Could not save the edit", "Не удалось сохранить правку")
			}
			res.Pages = append(res.Pages, r.w.page.ID)
			res.Edits += r.n
		}
		l.deckSnapshot(runtime, editors, locale.Pick("Edit by "+who.Subject, "Правка приложения «"+who.Subject+"»"), now)
	}
	res.Rev = l.DocRev(project)
	return res, nil
}

// docClaimed refuses a write over a board another chat holds: the claim is that chat's word that it is editing it.
func (l *Library) docClaimed(project string, p Page, who DocActor, now time.Time) error {
	keys := map[string]bool{p.Title: true, BoardTitleOf(pageFileOf(p.File)): true, pageFileOf(p.File): true}
	for _, c := range l.LiveClaims(project, now.UnixMilli()) {
		if !keys[c.Board] || (who.Tab != "" && c.Owner.TabID == who.Tab) {
			continue
		}
		title := c.Owner.ChatTitle
		return docErr(DocErrClaimed, "The board "+p.Title+" is being edited by "+title+": wait for it or ask it", "Доску «"+p.Title+"» сейчас правит «"+title+"»: подождите или попросите его")
	}
	return nil
}

func (t *docTarget) setAttr(name string, value *string) error {
	if value != nil {
		if strings.Contains(*value, "{{") || strings.Contains(*value, "}}") || len(*value) > 2000 {
			return docInvalid("An attribute's value is a plain text of up to 2000 characters", "Значение атрибута — простой текст до 2000 символов")
		}
		if docURLAttr[strings.ToLower(name)] && !safeURLValue(*value) {
			return docInvalid("This address is not allowed in "+name, "Такой адрес в "+name+" нельзя")
		}
	}
	t.attrs = append(t.attrs, attrChange{name: strings.ToLower(name), value: value})
	return nil
}

// edits turns what the ops asked of one element into replacements of the board's source.
func (t *docTarget) edits(b *dcBoard) ([]docEdit, error) {
	tok := b.tagTok(t.e)
	tagFrom, tagTo := b.abs(tok.start), b.abs(tok.end)
	void := tok.kind == html.SelfClosingTagToken || voidTags[t.e.name]
	outerTo := tagTo
	innerFrom, innerTo, innerOK := 0, 0, false
	if !void {
		if s, e, ok := b.inner(t.e); ok {
			innerFrom, innerTo, innerOK = b.abs(s), b.abs(e), true
			// The end tag follows the content.
			for _, x := range b.toks {
				if b.abs(x.start) == innerTo && x.kind == html.EndTagToken {
					outerTo = b.abs(x.end)
					break
				}
			}
		}
	}
	if t.remove {
		if !void && !innerOK {
			return nil, docErr(DocErrDynamic, "This element cannot be taken out: its content is not plain markup", "Этот элемент нельзя убрать: его содержимое не простая разметка")
		}
		return []docEdit{{from: tagFrom, to: outerTo, seq: t.seq}}, nil
	}
	var out []docEdit
	if len(t.attrs) > 0 || len(t.style) > 0 {
		raw := b.src[tagFrom:tagTo]
		next, err := rewriteStartTag(raw, t.attrs, t.style)
		if err != nil {
			return nil, err
		}
		if next != raw {
			out = append(out, docEdit{from: tagFrom, to: tagTo, text: next, seq: t.seq})
		}
	}
	if t.text != nil {
		if !innerOK || !textStatic(t.e.node) {
			return nil, docErr(DocErrDynamic, "This text is filled in by the board's code: ask the agent to change it", "Этот текст подставляет код доски: попросите агента поменять его")
		}
		old := b.src[innerFrom:innerTo]
		lead := old[:len(old)-len(strings.TrimLeft(old, " \t\n\r\f"))]
		trail := old[len(strings.TrimRight(old, " \t\n\r\f")):]
		if strings.TrimSpace(old) == "" {
			lead, trail = "", ""
		}
		text := strings.ReplaceAll(html.EscapeString(*t.text), "\n", "<br>")
		out = append(out, docEdit{from: innerFrom, to: innerTo, text: lead + text + trail, seq: t.seq})
	}
	for _, in := range t.inserts {
		var at int
		switch in.position {
		case "before":
			at = tagFrom
		case "after":
			if !void && !innerOK {
				return nil, docErr(DocErrDynamic, "Nothing can be put after this element: its content is not plain markup", "После этого элемента ничего не вставить: его содержимое не простая разметка")
			}
			at = outerTo
		case "start", "end":
			if void || !innerOK {
				return nil, docErr(DocErrDynamic, "Nothing can be put inside this element", "Внутрь этого элемента ничего не вставить")
			}
			at = innerFrom
			if in.position == "end" {
				at = innerTo
			}
		}
		out = append(out, docEdit{from: at, to: at, text: in.html, seq: in.seq})
	}
	return out, nil
}

var voidTags = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}

// rewriteStartTag puts attribute changes and a style merge into a raw start tag.
func rewriteStartTag(raw string, changes []attrChange, style map[string]*string) (string, error) {
	find := func(name string) *deckAttr {
		for _, a := range startTagAttrs(raw) {
			if a.name == name {
				a := a
				return &a
			}
		}
		return nil
	}
	apply := func(name string, value *string) error {
		a := find(name)
		if a != nil && a.hasValue && strings.Contains(raw[a.valStart:a.valEnd], "{{") {
			return docErr(DocErrDynamic, "This attribute is filled in by the board's code", "Этот атрибут подставляет код доски")
		}
		switch {
		case value == nil && a == nil:
		case value == nil:
			from := a.start
			for from > 0 && strings.ContainsRune(" \t\n\r\f", rune(raw[from-1])) {
				from--
			}
			raw = raw[:from] + raw[a.end:]
		case a != nil:
			raw = raw[:a.start] + name + `="` + attrEscape(*value) + `"` + raw[a.end:]
		default:
			end := len(raw) - 1
			if strings.HasSuffix(raw, "/>") {
				end = len(raw) - 2
			}
			pre := strings.TrimRight(raw[:end], " \t\n\r\f")
			raw = pre + " " + name + `="` + attrEscape(*value) + `"` + raw[len(pre):]
		}
		return nil
	}
	for _, c := range changes {
		if err := apply(c.name, c.value); err != nil {
			return "", err
		}
	}
	if len(style) > 0 {
		current := ""
		if a := find("style"); a != nil && a.hasValue {
			current = html.UnescapeString(raw[a.valStart:a.valEnd])
		}
		if strings.Contains(current, "{{") {
			return "", patchDynamic()
		}
		text := mergeStyle(current, style)
		var value *string
		if text != "" {
			value = &text
		}
		if err := apply("style", value); err != nil {
			return "", err
		}
	}
	return raw, nil
}

var (
	docInsertTags = map[string]bool{"div": true, "span": true, "p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
		"h6": true, "ul": true, "ol": true, "li": true, "b": true, "i": true, "em": true, "strong": true, "u": true, "s": true,
		"br": true, "hr": true, "img": true, "a": true, "section": true, "header": true, "footer": true, "figure": true,
		"figcaption": true, "blockquote": true, "small": true, "sub": true, "sup": true, "mark": true, "code": true}
	docInsertAttrs = map[string]bool{"class": true, "id": true, "alt": true, "src": true, "href": true, "width": true, "height": true,
		"title": true, "role": true, "style": true}
)

// docCleanInsert is markup an app may insert: static, from a short list of tags and attributes, no scripts, no
// addresses that run, no template expressions. It comes back as the parser writes it.
func docCleanInsert(markup string) (string, error) {
	if len(markup) == 0 || len(markup) > 20000 {
		return "", docInvalid("The markup to insert is 1 to 20000 characters", "Вставляемая разметка — от 1 до 20000 символов")
	}
	if strings.Contains(markup, "{{") || strings.Contains(markup, "}}") {
		return "", docInvalid("Inserted markup holds no template expressions", "В вставляемой разметке не бывает выражений шаблона")
	}
	nodes, err := html.ParseFragment(strings.NewReader(markup), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", docInvalid("The markup could not be read", "Разметку не удалось прочитать")
	}
	var check func(n *html.Node) error
	check = func(n *html.Node) error {
		if n.Type == html.CommentNode {
			return docInvalid("Inserted markup has no comments", "В вставляемой разметке нет комментариев")
		}
		if n.Type == html.ElementNode {
			if n.Namespace != "" || !docInsertTags[n.Data] {
				return docInvalid("The tag <"+n.Data+"> cannot be inserted", "Тег <"+n.Data+"> вставлять нельзя")
			}
			kept := n.Attr[:0]
			for _, a := range n.Attr {
				k := strings.ToLower(a.Key)
				if a.Namespace != "" || !(docInsertAttrs[k] || strings.HasPrefix(k, "data-hl-")) {
					return docInvalid("The attribute "+a.Key+" cannot be inserted", "Атрибут "+a.Key+" вставлять нельзя")
				}
				if docURLAttr[k] && !safeURLValue(a.Val) {
					return docInvalid("This address is not allowed in "+a.Key, "Такой адрес в "+a.Key+" нельзя")
				}
				if k == "style" && (strings.Contains(strings.ToLower(a.Val), "url(") || strings.ContainsAny(a.Val, "<>\\")) {
					return docInvalid("Unusable style value", "Такое значение стиля не подходит")
				}
				kept = append(kept, a)
			}
			n.Attr = kept
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := check(c); err != nil {
				return err
			}
		}
		return nil
	}
	var out strings.Builder
	for _, n := range nodes {
		if err := check(n); err != nil {
			return "", err
		}
		if err := html.Render(&out, n); err != nil {
			return "", docInvalid("The markup could not be written", "Разметку не удалось записать")
		}
	}
	return out.String(), nil
}

// DocAddBoard adds a page whose board is src (a board with a template, <x-dc>), as one history entry.
func (l *Library) DocAddBoard(project, title string, src []byte, who DocActor, now time.Time) (Page, error) {
	if _, err := l.DocPages(project); err != nil {
		return Page{}, err
	}
	if len(src) == 0 || len(src) > deckBoardMax {
		return Page{}, docInvalid("A board is 1 byte to 8 MB", "Доска — от 1 байта до 8 МБ")
	}
	if _, ok := parseDcBoard(string(src)); !ok {
		return Page{}, docInvalid("The template is not a board: it has no <x-dc>", "Шаблон — не доска: в нём нет <x-dc>")
	}
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()
	runtime := ""
	var editors []string
	if id, err := l.Identity(); err == nil {
		runtime = id.Runtime
		if id.Person != "" {
			editors = []string{id.Person}
		}
	}
	l.deckSnapshot(runtime, editors, locale.Pick("Before a board by "+who.Subject, "Перед доской приложения «"+who.Subject+"»"), now)
	p, err := l.CreatePage(project, PageSpec{Title: title})
	if err != nil {
		if IsPageError(err) {
			return Page{}, docInvalid(err.Error(), err.Error())
		}
		return Page{}, docErr(DocErrFailed, "Could not add the board", "Не удалось добавить доску")
	}
	if _, err := writeIfChanged(filepath.Join(l.ProjectDir(project), pageFileOf(p.File)), src); err != nil {
		return Page{}, docErr(DocErrFailed, "Could not save the board", "Не удалось сохранить доску")
	}
	l.deckSnapshot(runtime, editors, locale.Pick("Board by "+who.Subject+": "+p.Title, "Доска приложения «"+who.Subject+"»: "+p.Title), now)
	return p, nil
}

// DocStatus maps a Document API refusal to an HTTP status for a route.
func DocStatus(err error) int {
	var de *DocError
	if !asDocError(err, &de) {
		return http.StatusInternalServerError
	}
	switch de.Code {
	case DocErrNotFound:
		return http.StatusNotFound
	case DocErrChanged, DocErrClaimed, DocErrLocked:
		return http.StatusConflict
	case DocErrDynamic:
		return http.StatusUnprocessableEntity
	case DocErrInvalid:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func asDocError(err error, target **DocError) bool {
	for err != nil {
		if de, ok := err.(*DocError); ok {
			*target = de
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// String keeps a node readable in a log.
func (n DocNode) String() string { return fmt.Sprintf("%s <%s>", n.ID, n.Tag) }

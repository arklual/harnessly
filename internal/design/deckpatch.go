package design

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A hand edit written into a board's source: the deck's text edit, a style from the toolbar, a block moved or put back.
//
// The page names an element by its data-dc-tpl, the number the board's engine (support.js) gives it: compileTemplate
// takes the text between the first <x-dc> and the last </x-dc>, encodes it (encodeCase), parses it as a <template>'s
// content and numbers the elements of that content in preorder; a nested <template>'s own content is not numbered.
// Here the same text is parsed by the same HTML algorithm (x/net/html, a fragment in a <template> context), with a
// marker on every start tag first, so each element of the parsed tree is known by the source tag that made it. An
// element the parser made up (an implied one) has no source and is never patched. The Chrome parity test holds the
// numbering against the browser's.

// deckBoardMax is the largest board a hand edit opens.
const deckBoardMax = 8 << 20

// deckTagMark is the attribute put on every source start tag before parsing; it never reaches a file.
const deckTagMark = "data-tk-src-tag-7f3a"

// jsSpace is JavaScript's \s, which the engine's regular expressions use.
const jsSpace = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	dcOpenRe          = regexp.MustCompile(`<x-dc(?:` + jsSpace + `[^>]*)?>`)
	importSelfCloseRe = regexp.MustCompile(`(?i)<(x-import|dc-import)((?:[^>"']|"[^"]*"|'[^']*')*)/>`)
	helmetOpenRe      = regexp.MustCompile(`(?i)<helmet(` + jsSpace + `|>)`)
	helmetCloseRe     = regexp.MustCompile(`(?i)</helmet` + jsSpace + `*>`)
	camelAttrRe       = regexp.MustCompile(`(` + jsSpace + `)([a-z]+[A-Z][A-Za-z0-9]*)(` + jsSpace + `*=)`)
	rawWrapRe         = regexp.MustCompile(`(?i)(</?)(select|table|tbody|thead|tfoot|tr|td|th|caption)(` + jsSpace + `|>)`)
	upperRe           = regexp.MustCompile(`[A-Z]`)
)

// dcRawWrap is the engine's RAW_WRAP: table and select tags are renamed before parsing, so the parser does not move them.
var dcRawWrap = map[string]string{
	"select": "sc-raw-select", "table": "sc-raw-table", "tbody": "sc-raw-tbody", "thead": "sc-raw-thead",
	"tfoot": "sc-raw-tfoot", "tr": "sc-raw-tr", "td": "sc-raw-td", "th": "sc-raw-th", "caption": "sc-raw-caption",
}

// dcTemplateRange is where a board's template is: after the first <x-dc …> and before the last </x-dc>, as the
// engine's parseDcText reads the fetched file.
func dcTemplateRange(src string) (start, end int, ok bool) {
	open := dcOpenRe.FindStringIndex(src)
	if open == nil {
		return 0, 0, false
	}
	end = strings.LastIndex(src, "</x-dc>")
	if end < 0 || end < open[0] {
		return 0, 0, false
	}
	if end < open[1] {
		end = open[1]
	}
	return open[1], end, true
}

// dcEncode is the engine's encodeCase: self-closed imports opened, helmet renamed, camelCase attributes kebab-cased,
// table and select tags renamed. It only renames: the start tags stay the same tags in the same order.
func dcEncode(s string) string {
	s = importSelfCloseRe.ReplaceAllString(s, "<$1$2></$1>")
	s = helmetOpenRe.ReplaceAllString(s, "<sc-helmet$1")
	s = helmetCloseRe.ReplaceAllString(s, "</sc-helmet>")
	s = camelAttrRe.ReplaceAllStringFunc(s, func(m string) string {
		g := camelAttrRe.FindStringSubmatch(m)
		return g[1] + "sc-camel-" + upperRe.ReplaceAllStringFunc(g[2], func(c string) string { return "-" + strings.ToLower(c) }) + g[3]
	})
	return rawWrapRe.ReplaceAllStringFunc(s, func(m string) string {
		g := rawWrapRe.FindStringSubmatch(m)
		return g[1] + dcRawWrap[strings.ToLower(g[2])] + g[3]
	})
}

// dcTok is any token of the source, for finding an element's end tag.
type dcTok struct {
	kind       html.TokenType
	name       string
	start, end int
}

// tokenize lists the tokens of a template text with their places.
func dcTokenize(s string) []dcTok {
	z := html.NewTokenizerFragment(strings.NewReader(s), "template")
	var out []dcTok
	at := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out
		}
		raw := z.Raw()
		t := dcTok{kind: tt, start: at, end: at + len(raw)}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken || tt == html.EndTagToken {
			name, _ := z.TagName()
			t.name = string(name)
			// A template's content belongs to no page that runs scripts: <noscript> holds markup there.
			if tt == html.StartTagToken && t.name == "noscript" {
				z.NextIsNotRawText()
			}
		}
		out = append(out, t)
		at += len(raw)
	}
}

// tagNameEnd is where a raw start tag's name ends.
func tagNameEnd(raw string) int {
	i := 1
	for i < len(raw) && !strings.ContainsRune("\t\n\f\r />", rune(raw[i])) {
		i++
	}
	return i
}

// dcElem is a numbered element of the template: its data-dc-tpl, its name as parsed, the source tag that made it
// (-1 for one the parser made up) and its node in the parsed tree.
type dcElem struct {
	tpl  int
	name string
	tag  int
	node *html.Node
}

// dcBoard is a board's template, parsed and numbered.
type dcBoard struct {
	src        string // the whole file
	start, end int    // the template in src
	toks       []dcTok
	tags       []int // the start tags among toks, by their number
	elems      []dcElem
	mapped     bool // the encoded text's start tags are the source's one by one
}

// parseDcBoard numbers a board's template as the engine does.
func parseDcBoard(src string) (*dcBoard, bool) {
	start, end, ok := dcTemplateRange(src)
	if !ok {
		return nil, false
	}
	tpl := src[start:end]
	b := &dcBoard{src: src, start: start, end: end, toks: dcTokenize(tpl)}
	for i, t := range b.toks {
		if t.kind == html.StartTagToken || t.kind == html.SelfClosingTagToken {
			b.tags = append(b.tags, i)
		}
	}
	enc := dcEncode(tpl)
	encToks := dcTokenize(enc)
	var marked strings.Builder
	n := 0
	b.mapped = true
	for _, t := range encToks {
		raw := enc[t.start:t.end]
		if t.kind != html.StartTagToken && t.kind != html.SelfClosingTagToken {
			marked.WriteString(raw)
			continue
		}
		if n >= len(b.tags) || !sameTag(b.toks[b.tags[n]].name, t.name) {
			b.mapped = false
		}
		cut := tagNameEnd(raw)
		marked.WriteString(raw[:cut] + " " + deckTagMark + `="` + strconv.Itoa(n) + `"` + raw[cut:])
		n++
	}
	if n != len(b.tags) {
		b.mapped = false
	}
	context := &html.Node{Type: html.ElementNode, Data: "template", DataAtom: atom.Template}
	nodes, err := html.ParseFragmentWithOptions(strings.NewReader(marked.String()), context, html.ParseOptionEnableScripting(false))
	if err != nil {
		return nil, false
	}
	count := 0
	var walk func(n *html.Node, counted bool)
	walk = func(n *html.Node, counted bool) {
		if n.Type == html.ElementNode {
			if counted {
				e := dcElem{tpl: count, name: n.Data, tag: -1, node: n}
				for _, a := range n.Attr {
					if a.Key == deckTagMark && a.Namespace == "" {
						if k, err := strconv.Atoi(a.Val); err == nil && k >= 0 && k < len(b.tags) && b.mapped {
							e.tag = k
						}
					}
				}
				b.elems = append(b.elems, e)
				count++
			}
			// The content of a <template> is its own fragment in the browser: the engine does not number it.
			if n.DataAtom == atom.Template && n.Namespace == "" {
				counted = false
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, counted)
		}
	}
	for _, n := range nodes {
		walk(n, true)
	}
	return b, true
}

// sameTag: the source's tag and the encoded one are the same tag (the encoding renames some).
func sameTag(source, encoded string) bool {
	if source == encoded {
		return true
	}
	if source == "helmet" && encoded == "sc-helmet" {
		return true
	}
	return dcRawWrap[source] == encoded
}

// elem is the element numbered tpl; nil when there is none or it has no source tag.
func (b *dcBoard) elem(tpl int) *dcElem {
	if tpl < 0 || tpl >= len(b.elems) || b.elems[tpl].tag < 0 {
		return nil
	}
	return &b.elems[tpl]
}

// tagTok is the source token of a start tag.
func (b *dcBoard) tagTok(e *dcElem) dcTok { return b.toks[b.tags[e.tag]] }

// abs is a place in the template as a place in the file.
func (b *dcBoard) abs(at int) int { return b.start + at }

// deckRawText are the elements whose content is text to the parser, never a text the deck edits.
var deckRawText = map[string]bool{"script": true, "style": true, "textarea": true, "title": true, "xmp": true, "iframe": true,
	"noembed": true, "noframes": true, "noscript": true, "plaintext": true}

// deckInline are the tags a hand-edited text may hold.
var deckInline = map[string]bool{"b": true, "strong": true, "i": true, "em": true, "u": true, "s": true, "br": true}

// textStatic: an element whose content is text and b strong i em u s br without attributes, with no {{ }}: the deck
// edits only those in place.
func textStatic(n *html.Node) bool {
	if deckRawText[n.Data] && n.Namespace == "" {
		return false
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			if strings.Contains(c.Data, "{{") {
				return false
			}
		case html.ElementNode:
			if c.Namespace != "" || !deckInline[c.Data] {
				return false
			}
			for _, a := range c.Attr {
				if a.Key != deckTagMark {
					return false
				}
			}
			if !textStatic(c) {
				return false
			}
		case html.CommentNode:
		default:
			return false
		}
	}
	return true
}

// descendants counts the elements under n.
func descendants(n *html.Node) int {
	k := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			k += 1 + descendants(c)
		}
	}
	return k
}

// inner is the place of a text element's content in the template: from after its start tag to its end tag. ok is false
// when the source does not close it plainly, or holds other tags than the parsed tree says.
func (b *dcBoard) inner(e *dcElem) (start, end int, ok bool) {
	ti := b.tags[e.tag]
	open := b.toks[ti]
	if open.kind == html.SelfClosingTagToken {
		return 0, 0, false
	}
	depth, starts := 0, 0
	for i := ti + 1; i < len(b.toks); i++ {
		t := b.toks[i]
		switch t.kind {
		case html.StartTagToken, html.SelfClosingTagToken:
			starts++
			if t.name == open.name && t.kind == html.StartTagToken {
				depth++
			}
		case html.EndTagToken:
			if t.name != open.name {
				continue
			}
			if depth > 0 {
				depth--
				continue
			}
			if starts != descendants(e.node) {
				return 0, 0, false
			}
			// The tags inside are the element's own descendants, in order, each closed inside (misnested ones are not).
			want := e.tag + 1
			var unclosed []string
			for j := ti + 1; j < i; j++ {
				t := b.toks[j]
				switch t.kind {
				case html.StartTagToken, html.SelfClosingTagToken:
					if b.tags[want] != j {
						return 0, 0, false
					}
					want++
					if t.kind == html.StartTagToken && t.name != "br" {
						unclosed = append(unclosed, t.name)
					}
				case html.EndTagToken:
					if len(unclosed) == 0 || unclosed[len(unclosed)-1] != t.name {
						return 0, 0, false
					}
					unclosed = unclosed[:len(unclosed)-1]
				}
			}
			if len(unclosed) > 0 {
				return 0, 0, false
			}
			return open.end, t.start, true
		}
	}
	return 0, 0, false
}

// deckCanon is a text's markup as the deck compares it: the words with their entities read and their spaces run
// together, b strong i em u s br kept bare, every other tag dropped. A text node of white space with no plain space in
// it is dropped, as the engine drops it (walkText).
func deckCanon(fragment string) string {
	z := html.NewTokenizerFragment(strings.NewReader(fragment), "div")
	var out, text strings.Builder
	flush := func() {
		t := text.String()
		text.Reset()
		if strings.TrimSpace(t) == "" && !strings.Contains(t, " ") {
			return
		}
		out.WriteString(t)
	}
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			flush()
			return strings.TrimSpace(collapseSpace(out.String()))
		case html.TextToken:
			text.WriteString(html.EscapeString(string(z.Text())))
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			flush()
			name, _ := z.TagName()
			n := string(name)
			if !deckInline[n] {
				continue
			}
			if n == "br" {
				out.WriteString("<br>")
			} else if tt == html.EndTagToken {
				out.WriteString("</" + n + ">")
			} else {
				out.WriteString("<" + n + ">")
			}
		}
	}
}

var deckSpaceRun = regexp.MustCompile(`[\t\n\f\r ]+`)

func collapseSpace(s string) string { return deckSpaceRun.ReplaceAllString(s, " ") }

// deckCleanHTML is the new text as written into the board: b strong i em u s br without attributes and escaped words;
// trailing line breaks an editable block leaves are dropped. dynamic: it would read as {{ }}.
func deckCleanHTML(s string) (out string, dynamic bool) {
	z := html.NewTokenizerFragment(strings.NewReader(s), "div")
	var b, words strings.Builder
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.TextToken:
			t := string(z.Text())
			words.WriteString(t)
			b.WriteString(strings.ReplaceAll(html.EscapeString(t), "\xc2\xa0", "&nbsp;"))
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, _ := z.TagName()
			n := string(name)
			if !deckInline[n] {
				continue
			}
			words.WriteString(" ")
			if n == "br" {
				b.WriteString("<br>")
			} else if tt == html.EndTagToken {
				b.WriteString("</" + n + ">")
			} else {
				b.WriteString("<" + n + ">")
			}
		}
	}
	out = b.String()
	for {
		trimmed := strings.TrimRight(out, " \t\n\r")
		trimmed = strings.TrimSuffix(trimmed, "<br>")
		if trimmed == out {
			break
		}
		out = trimmed
	}
	return out, strings.Contains(words.String(), "{{")
}

// deckAttr is an attribute of a raw start tag: where it is (name to end of value) and its value as written.
type deckAttr struct {
	name             string
	start, end       int
	valStart, valEnd int
	hasValue         bool
}

// startTagAttrs reads a raw start tag's attributes as the HTML tokenizer does.
func startTagAttrs(raw string) []deckAttr {
	var out []deckAttr
	i := tagNameEnd(raw)
	ws := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }
	for i < len(raw) {
		for i < len(raw) && (ws(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= len(raw) || raw[i] == '>' {
			break
		}
		a := deckAttr{start: i}
		j := i + 1 // a name may begin with =
		for j < len(raw) && !ws(raw[j]) && raw[j] != '/' && raw[j] != '>' && raw[j] != '=' {
			j++
		}
		a.name = strings.ToLower(raw[i:j])
		a.end = j
		k := j
		for k < len(raw) && ws(raw[k]) {
			k++
		}
		if k < len(raw) && raw[k] == '=' {
			k++
			for k < len(raw) && ws(raw[k]) {
				k++
			}
			a.hasValue = true
			switch {
			case k < len(raw) && (raw[k] == '"' || raw[k] == '\''):
				q := raw[k]
				e := strings.IndexByte(raw[k+1:], q)
				if e < 0 {
					a.valStart, a.valEnd, a.end = k+1, len(raw), len(raw)
				} else {
					a.valStart, a.valEnd, a.end = k+1, k+1+e, k+2+e
				}
			default:
				e := k
				for e < len(raw) && !ws(raw[e]) && raw[e] != '>' {
					e++
				}
				a.valStart, a.valEnd, a.end = k, e, e
			}
			i = a.end
		} else {
			i = j
		}
		out = append(out, a)
	}
	return out
}

// cssDecl is one declaration of a style attribute: its property (lower case unless custom) and its text.
type cssDecl struct{ prop, text string }

func cssProp(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "--") {
		return p
	}
	return strings.ToLower(p)
}

// parseStyle splits a style attribute as the engine's cssToObj does (by ;, then the first :).
func parseStyle(s string) []cssDecl {
	var out []cssDecl
	for _, part := range strings.Split(s, ";") {
		t := strings.TrimSpace(part)
		i := strings.IndexByte(t, ':')
		if i < 0 {
			continue
		}
		out = append(out, cssDecl{prop: cssProp(t[:i]), text: t})
	}
	return out
}

func styleText(decls []cssDecl) string {
	parts := make([]string, len(decls))
	for i, d := range decls {
		parts[i] = d.text
	}
	return strings.Join(parts, "; ")
}

var (
	cssPropRe  = regexp.MustCompile(`^(--[A-Za-z0-9_-]{1,60}|-?[a-z][a-z-]{0,60})$`)
	cssValueRe = regexp.MustCompile(`^[^;{}<>\\\x00-\x1f]{1,300}$`)
)

// deckPatchErr is a refused patch: its status and code.
type deckPatchErr struct {
	status int
	code   string
	msg    string
}

func (e *deckPatchErr) Error() string { return e.msg }

func patchChanged() error {
	return &deckPatchErr{http.StatusConflict, DeckPatchChanged, locale.Pick("The slide changed since it was shown: look again", "Слайд поменялся, пока вы правили: посмотрите ещё раз")}
}

func patchDynamic() error {
	return &deckPatchErr{http.StatusUnprocessableEntity, DeckPatchDynamic, locale.Pick("This text is filled in by the board's code: ask the agent to change it", "Этот текст подставляет код слайда: попросите агента поменять его")}
}

func patchBad(msg string) error { return &deckPatchErr{http.StatusBadRequest, "", msg} }

// applyDeckPatch is a board's source with a hand edit made. w is the board's width (cqw for a move), 0 when unknown.
func applyDeckPatch(src string, req DeckPatchRequest, w int) (string, error) {
	b, ok := parseDcBoard(src)
	if !ok {
		return "", patchChanged()
	}
	if req.Op == DeckPatchText {
		return b.patchText(req)
	}
	if req.Was == nil || *req.Was == "" {
		return "", patchBad(locale.Pick("No fingerprint of the block", "Нет отпечатка блока"))
	}
	if req.Unit != "" && req.Unit != "px" {
		return "", patchBad(locale.Pick("Unknown unit", "Неизвестная единица"))
	}
	e := b.byFingerprint(req.Tpl, *req.Was)
	if e == nil {
		return "", patchChanged()
	}
	set := map[string]*string{}
	switch req.Op {
	case DeckPatchStyle:
		if len(req.Style) == 0 || len(req.Style) > 16 {
			return "", patchBad(locale.Pick("No style to set", "Нечего менять в стиле"))
		}
		for p, v := range req.Style {
			if !cssPropRe.MatchString(p) || p == "translate" {
				return "", patchBad(locale.Pick("Unknown style property", "Неизвестное свойство стиля"))
			}
			if v != nil && (!cssValueRe.MatchString(*v) || strings.Contains(*v, "url(")) {
				return "", patchBad(locale.Pick("Unusable style value", "Такое значение стиля не подходит"))
			}
			set[p] = v
		}
	case DeckPatchMove:
		if req.X == nil || req.Y == nil || !finite(*req.X) || !finite(*req.Y) || math.Abs(*req.X) > 1e5 || math.Abs(*req.Y) > 1e5 {
			return "", patchBad(locale.Pick("No place to move to", "Непонятно, куда двигать"))
		}
		if req.Unit == "px" {
			w = 0
		}
		set["translate"] = deckTranslate(*req.X, *req.Y, w)
	case DeckPatchReset:
		set["translate"] = nil
	default:
		return "", patchBad(locale.Pick("Unknown edit", "Неизвестная правка"))
	}
	return b.patchStyle(e, set)
}

// deckFingerprint is what the board says an element is, from the template's source of it: its name, then a hash (the
// engine's contentKey hash, over UTF-16) of its style attribute and its words run together. The board computes it
// from the engine's annotated template (client.js deckFingerprint), so a style, move or reset lands on the element
// the person saw or on nothing.
func deckFingerprint(n *html.Node) string {
	style := ""
	for _, a := range n.Attr {
		if a.Key == "style" && a.Namespace == "" {
			style = a.Val
			break
		}
	}
	var text strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Template && n.Namespace == "" {
			return // a template's content is not its text in the browser
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	if n.DataAtom != atom.Template || n.Namespace != "" {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	s := style + "\n" + strings.Trim(collapseSpace(text.String()), jsTrimSet)
	h := int32(5381)
	for _, u := range utf16.Encode([]rune(s)) {
		h = h<<5 + h + int32(u)
	}
	return n.Data + ":" + strconv.FormatUint(uint64(uint32(h)), 36)
}

// jsTrimSet is what JavaScript's trim() takes off.
var jsTrimSet = "\t\n\v\f\r " + string([]rune{0xa0, 0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff})

// byFingerprint is the element numbered tpl when it is still the one the board described; else the one element with
// that fingerprint (the numbers moved); nil when there is none or more than one.
func (b *dcBoard) byFingerprint(tpl int, was string) *dcElem {
	if e := b.elem(tpl); e != nil && deckFingerprint(e.node) == was {
		return e
	}
	var found *dcElem
	for i := range b.elems {
		e := &b.elems[i]
		if e.tag < 0 || deckFingerprint(e.node) != was {
			continue
		}
		if found != nil {
			return nil
		}
		found = e
	}
	return found
}

// deckTranslate is a move as CSS: in cqw of the board's width (the slide keeps it at any size), or px when the width is
// not known; nil (no translate) for no move.
func deckTranslate(x, y float64, w int) *string {
	num := func(v float64) string {
		v = math.Round(v*100) / 100
		if v == 0 {
			return "0"
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	unit := "px"
	if w > 0 {
		x, y, unit = x*100/float64(w), y*100/float64(w), "cqw"
	}
	if num(x) == "0" && num(y) == "0" {
		return nil
	}
	s := num(x) + unit + " " + num(y) + unit
	if num(x) == "0" {
		s = "0 " + num(y) + unit
	} else if num(y) == "0" {
		s = num(x) + unit + " 0"
	}
	return &s
}

func (b *dcBoard) patchText(req DeckPatchRequest) (string, error) {
	if req.Was == nil || req.HTML == nil {
		return "", patchBad(locale.Pick("No text", "Нет текста"))
	}
	next, dynamic := deckCleanHTML(*req.HTML)
	if dynamic {
		return "", patchDynamic()
	}
	was := deckCanon(*req.Was)
	tpl := b.src[b.start:b.end]
	try := func(e *dcElem) (int, int, bool) {
		if !textStatic(e.node) {
			return 0, 0, false
		}
		s, en, ok := b.inner(e)
		if !ok || deckCanon(tpl[s:en]) != was {
			return 0, 0, false
		}
		return s, en, true
	}
	target := b.elem(req.Tpl)
	s, en, ok := 0, 0, false
	if target != nil {
		if !textStatic(target.node) {
			// The element the page named, holding that text, is not plain text: its words come from the code.
			if ts, te, tok := b.inner(target); tok && deckCanon(tpl[ts:te]) == was {
				return "", patchDynamic()
			}
		}
		s, en, ok = try(target)
	}
	if !ok {
		// The numbers moved (the agent wrote the board meanwhile): the one element that holds the text the page saw.
		found := 0
		for i := range b.elems {
			e := &b.elems[i]
			if e.tag < 0 {
				continue
			}
			if fs, fe, fok := try(e); fok {
				if found == 0 {
					s, en = fs, fe
				}
				found++
			}
		}
		if found != 1 {
			if target != nil && !textStatic(target.node) {
				return "", patchDynamic()
			}
			return "", patchChanged()
		}
	}
	old := tpl[s:en]
	lead := old[:len(old)-len(strings.TrimLeft(old, " \t\n\r\f"))]
	trail := old[len(strings.TrimRight(old, " \t\n\r\f")):]
	if strings.TrimSpace(old) == "" {
		lead, trail = "", ""
	}
	return b.src[:b.abs(s)] + lead + next + trail + b.src[b.abs(en):], nil
}

func (b *dcBoard) patchStyle(e *dcElem, set map[string]*string) (string, error) {
	tok := b.tagTok(e)
	raw := b.src[b.abs(tok.start):b.abs(tok.end)]
	var style *deckAttr
	for _, a := range startTagAttrs(raw) {
		if a.name == "style" {
			a := a
			style = &a
			break
		}
	}
	current := ""
	if style != nil && style.hasValue {
		current = html.UnescapeString(raw[style.valStart:style.valEnd])
	}
	if strings.Contains(current, "{{") {
		return "", patchDynamic()
	}
	decls := parseStyle(current)
	keys := make([]string, 0, len(set))
	for p := range set {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		v := set[p]
		key := cssProp(p)
		kept := decls[:0:0]
		placed := false
		for _, d := range decls {
			if d.prop != key {
				kept = append(kept, d)
				continue
			}
			if v != nil && !placed {
				kept = append(kept, cssDecl{prop: key, text: p + ": " + *v})
				placed = true
			}
		}
		if v != nil && !placed {
			kept = append(kept, cssDecl{prop: key, text: p + ": " + *v})
		}
		decls = kept
	}
	text := styleText(decls)
	var next string
	switch {
	case style == nil && text == "":
		return b.src, nil
	case style == nil:
		end := len(raw) - 1
		if strings.HasSuffix(raw, "/>") {
			end = len(raw) - 2
		}
		pre := strings.TrimRight(raw[:end], " \t\n\r\f")
		next = pre + ` style="` + attrEscape(text) + `"` + raw[len(pre):]
	case text == "":
		from := style.start
		for from > 0 && strings.ContainsRune(" \t\n\r\f", rune(raw[from-1])) {
			from--
		}
		next = raw[:from] + raw[style.end:]
	default:
		next = raw[:style.start] + `style="` + attrEscape(text) + `"` + raw[style.end:]
	}
	return b.src[:b.abs(tok.start)] + next + b.src[b.abs(tok.end):], nil
}

func attrEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;").Replace(s)
}

// DeckPatch is POST DeckPatchRequest (edit): 200 {rev}; 409 {code: DeckPatchChanged}; 422 {code: DeckPatchDynamic}.
func (a *API) DeckPatch(r *http.Request) Answer {
	body, refused := a.Guard(r, http.MethodPost)
	if refused != nil {
		return *refused
	}
	h := noStore()
	var req DeckPatchRequest
	raw, err := json.Marshal(body)
	if err == nil {
		err = json.Unmarshal(raw, &req)
	}
	if err != nil || !IsLibraryID(req.Artifact) || !IsPageID(req.Page) || req.Tpl < 0 {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Unknown slide", "Неизвестный слайд")), h)
	}
	if no := a.gate(r, req.Artifact, RoleEdit); no != nil {
		return *no
	}
	a.startBackground()
	rev, err := a.L.patchDeckBoard(req, time.Now())
	if err != nil {
		var pe *deckPatchErr
		if errors.As(err, &pe) {
			if pe.code == "" {
				return JSON(r, pe.status, errorBody(pe.msg), h)
			}
			return JSON(r, pe.status, jsvalue.NewObject("error", pe.msg, "code", pe.code), h)
		}
		a.warn("design deck patch failed", err)
		return JSON(r, http.StatusInternalServerError, errorBody(locale.Pick("Could not save the edit", "Не удалось сохранить правку")), h)
	}
	return JSON(r, http.StatusOK, jsvalue.NewObject("rev", float64(rev)), h)
}

// patchDeckBoard writes a hand edit into a slide's board, between two snapshots of the history that name the person:
// what was there before is kept, and the edit is a version of its own. rev is the board's time after the edit.
func (l *Library) patchDeckBoard(req DeckPatchRequest, now time.Time) (int64, error) {
	page, ok := l.ReadModel(req.Artifact).Page(req.Page)
	file := pageFileOf(page.File)
	if !ok || !IsBoardFile(file) {
		return 0, &deckPatchErr{http.StatusNotFound, "", locale.Pick("No such slide", "Нет такого слайда")}
	}
	path := filepath.Join(l.ProjectDir(req.Artifact), file)
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()
	before, err := readFileLimited(path, deckBoardMax)
	if err != nil {
		return 0, &deckPatchErr{http.StatusNotFound, "", locale.Pick("No such slide", "Нет такого слайда")}
	}
	w := 0
	if page.W != nil {
		w = *page.W
	}
	next, err := applyDeckPatch(string(before), req, w)
	if err != nil {
		return 0, err
	}
	board := BoardTitleOf(file)
	if page.Title != "" {
		board = page.Title
	}
	var editors []string
	runtime := ""
	if id, err := l.Identity(); err == nil {
		runtime = id.Runtime
		if id.Person != "" {
			editors = []string{id.Person}
		}
	}
	if next != string(before) {
		l.deckSnapshot(runtime, editors, locale.Pick("Before a manual edit of \""+board+"\"", "Перед правкой вручную «"+board+"»"), now)
		// The agent may have written the board meanwhile: the edit goes over what it was made against only.
		if again, err := readFileLimited(path, deckBoardMax); err != nil || string(again) != string(before) {
			return 0, patchChanged()
		}
		if _, err := writeIfChanged(path, []byte(next)); err != nil {
			return 0, err
		}
		l.deckSnapshot(runtime, editors, locale.Pick("Manual edit: \""+board+"\"", "Правка вручную: «"+board+"»"), now)
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.ModTime().UnixMilli(), nil
}

// deckSnapshot saves the history around a hand edit; a busy or failing history does not lose the edit (the next
// snapshot takes it), it is only logged.
func (l *Library) deckSnapshot(runtime string, editors []string, subject string, now time.Time) {
	out, err := l.snapshotNow(runtime, editors, subject, now)
	if err != nil {
		l.warn("design deck patch: history snapshot failed", err)
	} else if out.kind == SnapshotBusy {
		l.warn("design deck patch: history busy", nil)
	}
}

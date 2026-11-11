package design

// The turn of an old board into a page module (`harnessly design migrate --to tsx`): rules over the board's
// text, without a model, written to give the DOM the board gives.
//
//	pages/<name>.tsx            the template as JSX, the logic class as it was written, renderVals() in the body
//	pages/<name>.fixtures.ts    the defaults of the board's props (data-props), the page's data
//	pages/<name>.board.html     the frame: the board file without the page (its <head>, the helmet), where the build
//	                            puts the page back
//	pages/board-engine/index.ts what drawing a board takes (scope, logic, components on window), shared by the pages
//
// The rules are the board format as our own engine (ownengine/support.js) and texts/format.md define it: a {{ }} is
// the same lookup in JS (optional chaining), a text value the same <span class="sc-interp">, an <x-import> the same
// <div class="sc-host-x"> around its component. Whether the project's engine draws the page as it drew the board is
// not assumed: the migration draws both and compares them (migrateverify.go). What the rules do not cover makes the
// board not translatable, with the reason; such a board stays as it is.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	pageFrameExt   = ".board.html"
	boardEngineDir = "board-engine"
	pageSlot       = `<x-import component-from-global-scope="` + pageGlobal + `.default"></x-import>`
)

// BoardModule is a board turned into a page module: the files' contents, by their project-relative paths.
type BoardModule struct {
	Page, Fixtures, Frame string
	// HasProps: the board has props, and Fixtures holds their defaults.
	HasProps bool
}

// NotTranslatable says why a board stays in the old format.
type NotTranslatable struct{ Reason string }

func (e *NotTranslatable) Error() string { return e.Reason }

func notTranslatable(format string, a ...any) error {
	return &NotTranslatable{Reason: fmt.Sprintf(format, a...)}
}

var (
	dcScriptRe   = regexp.MustCompile(`(?is)<script\b[^>]*\bdata-dc-script\b[^>]*>(.*?)</script\s*>`)
	helmetSpanRe = regexp.MustCompile(`(?is)<helmet(?:\s[^>]*)?>.*?</helmet\s*>`)
	kebabRe      = regexp.MustCompile(`-([a-z0-9])`)
	cssNameRe    = regexp.MustCompile(`-([a-z])`)
	wholeExprRe  = regexp.MustCompile(`^\{\{([\s\S]*)\}\}$`)
	oneExprRe    = regexp.MustCompile(`\{\{([\s\S]*?)\}\}`)
	pathHeadRe   = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*`)
	pathStepRe   = regexp.MustCompile(`^(?:\.([A-Za-z0-9_$]+)|\[\s*(\d+)\s*\]|\[\s*"([^"]*)"\s*\]|\[\s*'([^']*)'\s*\])`)
	numberRe     = regexp.MustCompile(`^-?\d+(?:\.\d+)?$`)
	quotedRe     = regexp.MustCompile(`^"([^"]*)"$|^'([^']*)'$`)
	jsxNameRe    = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$-]*$`)
	jsxTagRe     = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(-[a-zA-Z0-9.]*)*$`)
	jsIdentRe2   = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	digitsOnlyRe = regexp.MustCompile(`^\d+$`)
	plainTextRe  = regexp.MustCompile(`^[^{}<>&\s"'` + "`" + `\\](?:[^{}<>&\n\r\t\\]*[^{}<>&\s\\])?$`)
	plainAttrRe  = regexp.MustCompile(`^[^"\\&{}<>\n\r\t]*$`)
	importantRe  = regexp.MustCompile(`(?i)!\s*important\s*$`)
)

// boardEvents: an event attribute written in lower case, in React's spelling.
var boardEvents = map[string]string{"ondblclick": "onDoubleClick"}

func init() {
	for _, name := range strings.Fields(`Abort AnimationEnd AnimationIteration AnimationStart AuxClick BeforeInput Blur CanPlay
CanPlayThrough Change Click Close CompositionEnd CompositionStart CompositionUpdate ContextMenu Copy Cut DoubleClick Drag
DragEnd DragEnter DragExit DragLeave DragOver DragStart Drop DurationChange Emptied Encrypted Ended Error Focus
GotPointerCapture Input Invalid KeyDown KeyPress KeyUp Load LoadedData LoadedMetadata LoadStart LostPointerCapture
MouseDown MouseEnter MouseLeave MouseMove MouseOut MouseOver MouseUp Paste Pause Play Playing PointerCancel PointerDown
PointerEnter PointerLeave PointerMove PointerOut PointerOver PointerUp Progress RateChange Reset Resize Scroll Seeked
Seeking Select Stalled Submit Suspend TimeUpdate Toggle TouchCancel TouchEnd TouchMove TouchStart TransitionEnd
VolumeChange Waiting Wheel`) {
		boardEvents["on"+strings.ToLower(name)] = "on" + name
	}
}

// booleanAttrs: written without a value on an HTML element, they are true (format.md).
var booleanAttrs = map[string]bool{"async": true, "checked": true, "controls": true, "default": true, "defer": true,
	"disabled": true, "hidden": true, "loop": true, "multiple": true, "muted": true, "open": true, "required": true,
	"reversed": true, "scoped": true, "seamless": true, "selected": true}

// droppedAttrs describe a placeholder while a board is written; they never reach the page.
var droppedAttrs = map[string]bool{"data-dc-tpl": true, "hint-size": true, "hint-placeholder-val": true, "hint-placeholder-count": true}

// hostStyleProps: the part of an <x-import>'s style its wrapper keeps (format.md).
var hostStyleProps = map[string]bool{"width": true, "height": true, "position": true, "top": true, "left": true,
	"right": true, "bottom": true, "inset": true, "zIndex": true, "transform": true}

func kebabToCamel(s string) string {
	return kebabRe.ReplaceAllStringFunc(s, func(m string) string { return strings.ToUpper(m[1:]) })
}

// jsTrim is String.prototype.trim.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' || r == '\u00a0' })
}

// tsxScope is where the names of a template's expressions are looked up: the board's values, or a loop's item.
type tsxScope struct {
	parent *tsxScope
	names  map[string]string // template name -> JS expression
	vals   string            // the values object at the root
}

func (s *tsxScope) lookup(name string) string {
	for at := s; at != nil; at = at.parent {
		if js, ok := at.names[name]; ok {
			return js
		}
		if at.parent == nil {
			if jsIdentRe2.MatchString(name) {
				return at.vals + "." + name
			}
			return at.vals + "[" + jsString(name) + "]"
		}
	}
	return "undefined"
}

// tsxExpr is a {{ }} expression as JS: a path read with optional chaining, a literal, a "!" in front (format.md).
// Anything else does not resolve: undefined.
func tsxExpr(src string, sc *tsxScope) string {
	text := jsTrim(src)
	if strings.HasPrefix(text, "!") {
		return "!" + tsxExpr(text[1:], sc)
	}
	if numberRe.MatchString(text) {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return "undefined"
		}
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	switch text {
	case "true", "false", "null":
		return text
	case "undefined", "":
		return "undefined"
	}
	if m := quotedRe.FindStringSubmatch(text); m != nil {
		return jsString(m[1] + m[2])
	}
	head := pathHeadRe.FindString(text)
	if head == "" {
		return "undefined"
	}
	out := sc.lookup(head)
	for rest := text[len(head):]; rest != ""; {
		m := pathStepRe.FindStringSubmatch(rest)
		if m == nil {
			return "undefined"
		}
		key := m[1] + m[2] + m[3] + m[4]
		switch {
		case m[1] != "" && jsIdentRe2.MatchString(key):
			out += "?." + key
		case digitsOnlyRe.MatchString(key) && (m[1] != "" || m[2] != ""):
			out += "?.[" + key + "]"
		default:
			out += "?.[" + jsString(key) + "]"
		}
		rest = rest[len(m[0]):]
	}
	return out
}

// tsxAttr is an attribute's value as JS: one expression keeps its value, text with expressions is a string with null
// and undefined left out. literal is set when the value has no expression.
func tsxAttr(raw string, sc *tsxScope) (js string, literal bool) {
	if m := wholeExprRe.FindStringSubmatch(jsTrim(raw)); m != nil {
		return tsxExpr(m[1], sc), false
	}
	if !strings.Contains(raw, "{{") {
		return jsString(raw), true
	}
	var b strings.Builder
	b.WriteString("`")
	at := 0
	for _, loc := range oneExprRe.FindAllStringSubmatchIndex(raw, -1) {
		b.WriteString(templateText(raw[at:loc[0]]))
		b.WriteString("${" + tsxExpr(raw[loc[2]:loc[3]], sc) + " ?? \"\"}")
		at = loc[1]
	}
	b.WriteString(templateText(raw[at:]))
	b.WriteString("`")
	return b.String(), false
}

func templateText(s string) string {
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "${", "\\${").Replace(s)
}

// boardDecl is a declaration of a style written as text: its name in React's spelling (custom properties as they are)
// and its value without !important. A name written twice keeps its first place and its last value (format.md).
type boardDecl struct{ name, value string }

func cssToDecls(css string) []boardDecl {
	var out []boardDecl
	index := map[string]int{}
	take := func(decl string) {
		colon := strings.Index(decl, ":")
		if colon <= 0 {
			return
		}
		name := jsTrim(decl[:colon])
		value := jsTrim(importantRe.ReplaceAllString(decl[colon+1:], ""))
		if name == "" || value == "" {
			return
		}
		if !strings.HasPrefix(name, "--") {
			name = strings.ToLower(name)
			if strings.HasPrefix(name, "-ms-") {
				name = "ms-" + name[4:]
			}
			name = cssNameRe.ReplaceAllStringFunc(name, func(m string) string { return strings.ToUpper(m[1:]) })
		}
		if at, ok := index[name]; ok {
			out[at].value = value
			return
		}
		index[name] = len(out)
		out = append(out, boardDecl{name, value})
	}
	start, depth := 0, 0
	quote := byte(0)
	for i := 0; i < len(css); i++ {
		c := css[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == ';' && depth == 0:
			take(css[start:i])
			start = i + 1
		}
	}
	take(css[start:])
	return out
}

func objectLiteral(decls []boardDecl) string {
	if len(decls) == 0 {
		return "{}"
	}
	parts := make([]string, len(decls))
	for i, d := range decls {
		key := d.name
		if !jsIdentRe2.MatchString(key) || isIntegerKey(key) {
			key = jsString(key)
		}
		parts[i] = key + ": " + jsString(d.value)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func isIntegerKey(s string) bool {
	_, err := strconv.ParseUint(s, 10, 32)
	return err == nil
}

// tsxWriter writes JSX with indentation.
type tsxWriter struct {
	b      strings.Builder
	indent int
	tidy   bool // whitespace-only text as {" "}, not as it was written
	used   map[string]bool
	locals int
}

func (w *tsxWriter) line(s string) {
	w.b.WriteString(strings.Repeat("  ", w.indent))
	w.b.WriteString(s)
	w.b.WriteString("\n")
}

func (w *tsxWriter) local(base string) string {
	w.locals++
	name := base
	if w.used[name] {
		name = base + strconv.Itoa(w.locals)
	}
	w.used[name] = true
	return name
}

func boardAttrOf(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

// jsxProp is a prop as JSX: name="text", name={js}, or a spread for a name JSX cannot write.
func jsxProp(name, js string, literal bool, raw string) string {
	if !jsxNameRe.MatchString(name) {
		return "{...{ " + jsString(name) + ": " + js + " }}"
	}
	if literal && plainAttrRe.MatchString(raw) && utf8.ValidString(raw) {
		return name + "=\"" + raw + "\""
	}
	return name + "={" + js + "}"
}

// props keeps the engine's order of props with a later value of the same name replacing the earlier one.
type propList struct {
	names []string
	text  map[string]string
}

func (p *propList) set(name, text string) {
	if p.text == nil {
		p.text = map[string]string{}
	}
	if _, ok := p.text[name]; !ok {
		p.names = append(p.names, name)
	}
	p.text[name] = text
}

func (p *propList) list() []string {
	out := make([]string, 0, len(p.names))
	for _, n := range p.names {
		out = append(out, p.text[n])
	}
	return out
}

func (w *tsxWriter) open(tag string, props []string, selfClose bool) {
	end := ">"
	if selfClose {
		end = " />"
	}
	head := "<" + tag
	if len(props) > 0 {
		head += " " + strings.Join(props, " ")
	}
	if len(head) <= 110 || len(props) < 2 {
		w.line(head + end)
		return
	}
	w.line("<" + tag)
	w.indent++
	for _, p := range props {
		w.line(p)
	}
	w.indent--
	w.line(strings.TrimPrefix(end, " "))
}

// children writes the nodes of a parent the way the engine's walkChildren draws them.
func (w *tsxWriter) children(n *html.Node, sc *tsxScope) error {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := w.node(c, sc); err != nil {
			return err
		}
	}
	return nil
}

func (w *tsxWriter) node(n *html.Node, sc *tsxScope) error {
	switch n.Type {
	case html.TextNode:
		w.text(n.Data, sc)
		return nil
	case html.ElementNode:
	default:
		return nil
	}
	tag := n.Data
	if n.Namespace == "svg" || n.Namespace == "math" {
		tag = n.Data
	}
	switch strings.ToLower(tag) {
	case "sc-helmet":
		return notTranslatable("a <helmet> inside the page (not at its top): it cannot move into the frame")
	case "sc-for":
		return w.forEach(n, sc)
	case "sc-if":
		return w.ifThen(n, sc)
	case "x-import":
		return w.ximport(n, sc)
	case "dc-import":
		return notTranslatable("<dc-import> draws another board inside this one")
	case "x-dc":
		return notTranslatable("an <x-dc> inside the board")
	}
	return w.element(n, sc)
}

func (w *tsxWriter) text(txt string, sc *tsxScope) {
	if !strings.Contains(txt, "{{") {
		if jsTrim(txt) == "" && !strings.Contains(txt, " ") {
			return
		}
		w.literalText(txt)
		return
	}
	// The engine splits on the expressions: text, expression, text, …
	at := 0
	for _, loc := range oneExprRe.FindAllStringSubmatchIndex(txt, -1) {
		if loc[0] > at {
			w.literalText(txt[at:loc[0]])
		}
		w.line("{show(" + tsxExpr(txt[loc[2]:loc[3]], sc) + ")}")
		at = loc[1]
	}
	if at < len(txt) {
		w.literalText(txt[at:])
	}
}

func (w *tsxWriter) literalText(s string) {
	if s == "" {
		return
	}
	if jsTrim(s) == "" && w.tidy {
		w.line(`{" "}`)
		return
	}
	if plainTextRe.MatchString(s) {
		w.line(s)
		return
	}
	w.line("{" + jsString(s) + "}")
}

func (w *tsxWriter) element(n *html.Node, sc *tsxScope) error {
	tag := strings.TrimPrefix(n.Data, "sc-raw-")
	if !jsxTagRe.MatchString(tag) {
		return notTranslatable("the element <%s> has a name JSX cannot write", tag)
	}
	var props propList
	for _, a := range n.Attr {
		raw := a.Key
		if a.Namespace != "" {
			raw = a.Namespace + ":" + a.Key
		}
		if droppedAttrs[raw] {
			continue
		}
		if strings.HasPrefix(raw, "style-") {
			return notTranslatable("%s=\"…\" (a style for a state of the element) has no JSX form", raw)
		}
		key := authoredName(raw)
		if key == "style" {
			js, literal := tsxAttr(a.Val, sc)
			if literal {
				props.set(key, "style={"+objectLiteral(cssToDecls(a.Val))+"}")
			} else {
				props.set(key, "style={css("+js+")}")
			}
			continue
		}
		switch {
		case key == "class":
			key = "className"
		case key == "for":
			key = "htmlFor"
		case boardEvents[key] != "":
			key = boardEvents[key]
		}
		if booleanAttrs[raw] && a.Val == "" {
			props.set(key, key)
			continue
		}
		js, literal := tsxAttr(a.Val, sc)
		switch {
		case literal:
		case strings.HasPrefix(key, "on") && len(key) > 2 && key[2] >= 'A' && key[2] <= 'Z':
			js = "fn(" + js + ")"
		case key == "ref":
			js = "ref(" + js + ")"
		}
		props.set(key, jsxProp(key, js, literal, a.Val))
	}
	if tag == "template" || n.FirstChild == nil {
		w.open(tag, props.list(), true)
		return nil
	}
	w.open(tag, props.list(), false)
	w.indent++
	if err := w.children(n, sc); err != nil {
		return err
	}
	w.indent--
	w.line("</" + tag + ">")
	return nil
}

func authoredName(raw string) string {
	if strings.HasPrefix(raw, "sc-camel-") {
		return kebabToCamel(raw[len("sc-camel-"):])
	}
	return raw
}

func (w *tsxWriter) ifThen(n *html.Node, sc *tsxScope) error {
	raw, ok := boardAttrOf(n, "value")
	if !ok || n.FirstChild == nil {
		return nil
	}
	cond, literal := tsxAttr(raw, sc)
	if literal {
		if raw == "" {
			return nil
		}
		cond = "true"
	}
	w.line("{" + cond + " ? (")
	w.indent++
	w.line("<>")
	w.indent++
	if err := w.children(n, sc); err != nil {
		return err
	}
	w.indent--
	w.line("</>")
	w.indent--
	w.line(") : null}")
	return nil
}

func (w *tsxWriter) forEach(n *html.Node, sc *tsxScope) error {
	raw, _ := boardAttrOf(n, "list")
	list, _ := tsxAttr(raw, sc)
	as, _ := boardAttrOf(n, "as")
	names := map[string]string{}
	item := "_"
	if as != "" {
		base := as
		if !jsIdentRe2.MatchString(base) || reservedJS[base] {
			base = "item"
		}
		item = w.local(base)
		names[as] = item
	}
	index := w.local("index")
	names["$index"] = index
	inner := &tsxScope{parent: sc, names: names}
	w.line("{list(" + list + ").map((" + item + ", " + index + ") => (")
	w.indent++
	w.line("<Fragment key={" + index + "}>")
	w.indent++
	if err := w.children(n, inner); err != nil {
		return err
	}
	w.indent--
	w.line("</Fragment>")
	w.indent--
	w.line("))}")
	return nil
}

func (w *tsxWriter) ximport(n *html.Node, sc *tsxScope) error {
	if from, ok := boardAttrOf(n, "from"); ok && jsTrim(from) != "" {
		return notTranslatable("<x-import from=\"%s\"> loads a file of components at run time", from)
	}
	global, ok := boardAttrOf(n, "component-from-global-scope")
	if !ok {
		return notTranslatable("an <x-import> without component-from-global-scope")
	}
	host := `{ display: "contents" }`
	if styleRaw, ok := boardAttrOf(n, "style"); ok {
		js, literal := tsxAttr(styleRaw, sc)
		if literal {
			var kept []boardDecl
			for _, d := range cssToDecls(styleRaw) {
				if hostStyleProps[d.name] {
					kept = append(kept, d)
				}
			}
			if len(kept) > 0 {
				host = objectLiteral(kept)
			}
		} else {
			host = "hostStyle(" + js + ")"
		}
	}
	var props propList
	name, nameLiteral := tsxAttr(global, sc)
	props.set("__global", jsxProp("__global", name, nameLiteral, global))
	if hint, ok := boardAttrOf(n, "hint-size"); ok {
		js, literal := tsxAttr(hint, sc)
		props.set("__hint", jsxProp("__hint", js, literal, hint))
	}
	for _, a := range n.Attr {
		raw := a.Key
		if a.Namespace != "" {
			raw = a.Namespace + ":" + a.Key
		}
		switch raw {
		case "data-dc-tpl", "hint-size", "style", "component-from-global-scope", "from":
			continue
		}
		if strings.HasPrefix(raw, "style-") {
			continue // a state's style on a component: it reaches nothing
		}
		key := authoredName(raw)
		if !strings.HasPrefix(key, "data-") && !strings.HasPrefix(key, "aria-") {
			key = kebabToCamel(key)
		}
		if key == "__global" || key == "__hint" || key == "key" {
			return notTranslatable("an <x-import> prop named %s", key)
		}
		js, literal := tsxAttr(a.Val, sc)
		props.set(key, jsxProp(key, js, literal, a.Val))
	}
	w.line(`<div className="sc-host-x" style={` + host + `}>`)
	w.indent++
	if n.FirstChild == nil {
		w.open("Global", props.list(), true)
	} else {
		// Each child the template has is one child of the component: a text with values is one, not several.
		w.open("Global", props.list(), false)
		w.indent++
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode && strings.Contains(c.Data, "{{") {
				w.line("<>")
				w.indent++
				w.text(c.Data, sc)
				w.indent--
				w.line("</>")
				continue
			}
			if err := w.node(c, sc); err != nil {
				return err
			}
		}
		w.indent--
		w.line("</Global>")
	}
	w.indent--
	w.line("</div>")
	return nil
}

var reservedJS = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`break case catch class const continue debugger default delete do else enum export extends
false finally for function if import in instanceof new null return super switch this throw true try typeof var void
while with yield let static implements interface package private protected public await arguments eval undefined
Fragment React ReactDOM list show css fn ref hostStyle Global useBoard DCLogic Logic props v _`) {
		reservedJS[w] = true
	}
}

// componentName is a page's name as a JS identifier: its words, each from a capital letter.
func componentName(name string) string {
	var b strings.Builder
	up := true
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if b.Len() == 0 && unicode.IsDigit(r) {
				b.WriteString("Page")
			}
			if up {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
			up = false
		default:
			up = true
		}
	}
	if b.Len() == 0 {
		return "Page"
	}
	return b.String()
}

// TranslateBoard turns a board file into a page module. tidy writes whitespace between elements as {" "}, as a
// person would; without it the whitespace is kept as it was written (for a board whose text keeps its spaces).
func TranslateBoard(name, src string, tidy bool) (*BoardModule, error) {
	open := dcOpenRe.FindStringIndex(src)
	end := strings.LastIndex(src, "</x-dc>")
	if open == nil || end < open[1] {
		return nil, notTranslatable("no <x-dc> in the file")
	}
	if strings.Contains(src[:open[0]], "</x-dc>") || dcOpenRe.MatchString(src[open[1]:]) {
		return nil, notTranslatable("more than one <x-dc> in the file")
	}
	tpl := src[open[1]:end]
	if strings.Contains(src, pageMarkPrefix) {
		return nil, notTranslatable("the board is already built from a page module")
	}
	// The logic: the first <script data-dc-script>, after the template.
	scripts := dcScriptRe.FindAllStringSubmatchIndex(src, -1)
	if len(scripts) > 1 {
		return nil, notTranslatable("more than one logic script")
	}
	js, propsAttr := "", ""
	hasProps := false
	var scriptSpan []int
	if len(scripts) == 1 {
		s := scripts[0]
		if s[0] < end {
			return nil, notTranslatable("the logic script is not after the template")
		}
		scriptSpan = s
		js = src[s[2]:s[3]]
		z := html.NewTokenizer(strings.NewReader(src[s[0]:s[2]]))
		if z.Next() == html.StartTagToken {
			for {
				k, v, more := z.TagAttr()
				if string(k) == "data-props" {
					propsAttr, hasProps = string(v), true
				}
				if !more {
					break
				}
			}
		}
	}
	// The helmet goes to the frame as it is written: it has to be at the top of the template.
	helmets := helmetSpanRe.FindAllStringIndex(tpl, -1)
	encoded := dcEncode(tpl)
	ctx := &html.Node{Type: html.ElementNode, Data: "template", DataAtom: atom.Template}
	nodes, err := html.ParseFragment(strings.NewReader(encoded), ctx)
	if err != nil {
		return nil, notTranslatable("the template does not parse: %v", err)
	}
	top := 0
	for _, n := range nodes {
		if n.Type == html.ElementNode && n.Data == "sc-helmet" {
			top++
		}
	}
	if top != len(helmets) {
		return nil, notTranslatable("a <helmet> that is not at the top of the template")
	}

	w := &tsxWriter{indent: 3, tidy: tidy, used: map[string]bool{}}
	for k := range reservedJS {
		w.used[k] = true
	}
	root := &tsxScope{vals: "v"}
	for _, n := range nodes {
		if n.Type == html.ElementNode && n.Data == "sc-helmet" {
			continue
		}
		if err := w.node(n, root); err != nil {
			return nil, err
		}
	}

	// The defaults of the props, in the order data-props lists them.
	fixtures := ""
	if hasProps {
		defaults, err := propDefaults(propsAttr)
		if err != nil {
			return nil, err
		}
		fixtures = "// The data of the page " + name + ": the defaults of the board's props, as data-props gave them.\n" +
			"export const props: Record<string, unknown> = " + defaults + ";\n"
	}

	var page strings.Builder
	page.WriteString("// The page " + name + ", turned from the board " + name + ".dc.html by `migrate --to tsx`.\n")
	page.WriteString("// The rest of that board (its <head>, the helmet) is ./" + name + pageFrameExt + ": the build puts this page into it.\n")
	page.WriteString("import React, { Fragment } from \"react\";\n")
	page.WriteString("import { DCLogic, Global, css, fn, hostStyle, list, ref, show, useBoard } from \"./" + boardEngineDir + "\";\n")
	if hasProps {
		page.WriteString("import { props } from \"./" + name + pageFixtureEx + "\";\n")
	} else {
		page.WriteString("\nconst props = {};\n")
	}
	page.WriteString("\nvoid React;\nvoid [css, fn, hostStyle, list, ref, show, Fragment, Global];\n")
	if jsTrim(js) != "" {
		if strings.Contains(js, "</script") {
			return nil, notTranslatable("the logic has a </script> in it")
		}
		page.WriteString("\n// The board's logic as it was written; its renderVals() gives the page its values.\n")
		page.WriteString("const Logic = (function (DCLogic: any, StreamableLogic: any, React: any, ReactDOM: any): any {\n")
		page.WriteString(strings.Trim(js, "\n"))
		page.WriteString("\n;return typeof Component === \"undefined\" ? null : Component;\n})(DCLogic, DCLogic, React, (window as any).ReactDOM);\n")
	} else {
		page.WriteString("\nconst Logic = undefined;\n")
	}
	fn := componentName(name)
	page.WriteString("\nexport default function " + fn + "() {\n")
	page.WriteString("  const v = useBoard(Logic, props);\n")
	page.WriteString("  return (\n    <>\n")
	page.WriteString(w.b.String())
	page.WriteString("    </>\n  );\n}\n")

	// The frame: the file with the page's place instead of the template's content, and without the logic.
	var frame strings.Builder
	frame.WriteString(src[:open[1]])
	frame.WriteString("\n")
	for _, h := range helmets {
		frame.WriteString(tpl[h[0]:h[1]])
		frame.WriteString("\n")
	}
	frame.WriteString(pageSlot + "\n")
	tail := src[end:]
	if scriptSpan != nil {
		a, b := scriptSpan[0]-end, scriptSpan[1]-end
		if b < len(tail) && tail[b] == '\n' {
			b++
		}
		tail = tail[:a] + tail[b:]
	}
	frame.WriteString(tail)
	return &BoardModule{Page: page.String(), Fixtures: fixtures, Frame: frame.String(), HasProps: hasProps}, nil
}

// propDefaults is the object of the defaults data-props gives, in its order, as a JS literal.
func propDefaults(attr string) (string, error) {
	dec := json.NewDecoder(strings.NewReader(attr))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		// The engine reads no props from data-props it cannot parse.
		return "{}", nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return "{}", nil
	}
	type kv struct{ k, v string }
	var out []kv
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return "{}", nil
		}
		key, _ := kt.(string)
		var meta map[string]json.RawMessage
		if err := dec.Decode(&meta); err != nil {
			continue
		}
		def, ok := meta["default"]
		if !ok {
			continue
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, def); err != nil {
			continue
		}
		out = append(out, kv{key, buf.String()})
	}
	// JS puts integer-like keys first, in order: the same object either way.
	sort.SliceStable(out, func(i, j int) bool { return isIntegerKey(out[i].k) && !isIntegerKey(out[j].k) })
	if len(out) == 0 {
		return "{}", nil
	}
	var b strings.Builder
	b.WriteString("{\n")
	for _, e := range out {
		b.WriteString("  " + jsString(e.k) + ": " + e.v + ",\n")
	}
	b.WriteString("}")
	return b.String(), nil
}

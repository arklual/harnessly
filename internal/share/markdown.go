package share

import (
	"bytes"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"strconv"
	"strings"

	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The Markdown of a message rendered the way the previous runtime's page did it (react-markdown with GFM, through
// mdast → hast → React's static markup): the same elements, the same line breaks between blocks, links only through the
// page's URL check, images replaced by a note. The tree is built like hast and written like React writes it.

type hnode struct {
	tag  string // "" for text
	text string
	// enc: text as written in the source, its escapes and character references not yet resolved.
	enc bool
	// spaceBreak: a hard break made of trailing spaces, which the text before it does not keep.
	spaceBreak bool
	attrs      [][2]string
	kids       []*hnode
}

func el(tag string, kids ...*hnode) *hnode { return &hnode{tag: tag, kids: kids} }
func txt(s string) *hnode                  { return &hnode{text: s} }
func eol() *hnode                          { return txt("\n") }

func (n *hnode) attr(k, v string) *hnode { n.attrs = append(n.attrs, [2]string{k, v}); return n }

var voidTags = map[string]bool{"br": true, "hr": true, "input": true, "img": true, "meta": true, "link": true}

// tableTags drop their whitespace-only text children (hast-util-to-jsx-runtime does, React would warn otherwise).
var tableTags = map[string]bool{"table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true}

// reactEscape is React's escaping of text and attribute values.
var reactEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;")

func reactEscape(s string) string { return reactEscaper.Replace(s) }

func (n *hnode) write(b *strings.Builder) {
	if n.tag == "" {
		b.WriteString(reactEscape(n.text))
		return
	}
	b.WriteByte('<')
	b.WriteString(n.tag)
	for _, a := range n.attrs {
		b.WriteByte(' ')
		b.WriteString(a[0])
		b.WriteString(`="`)
		b.WriteString(reactEscape(a[1]))
		b.WriteByte('"')
	}
	if voidTags[n.tag] {
		b.WriteString("/>")
		return
	}
	b.WriteByte('>')
	for _, k := range n.kids {
		if tableTags[n.tag] && k.tag == "" && isWhitespace(k.text) {
			continue
		}
		k.write(b)
	}
	b.WriteString("</")
	b.WriteString(n.tag)
	b.WriteByte('>')
}

// isWhitespace is hast's inter-element whitespace: spaces, tabs, line feeds, form feeds and carriage returns only.
func isWhitespace(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\f', '\r':
		default:
			return false
		}
	}
	return true
}

// wrap is mdast-util-to-hast's wrap: a line feed between nodes, and around them when loose.
func wrap(nodes []*hnode, loose bool) []*hnode {
	out := make([]*hnode, 0, 2*len(nodes)+1)
	if loose {
		out = append(out, eol())
	}
	for i, n := range nodes {
		if i > 0 {
			out = append(out, eol())
		}
		out = append(out, n)
	}
	if loose && len(nodes) > 0 {
		out = append(out, eol())
	}
	return out
}

// mdRenderer turns one message into hast.
type mdRenderer struct {
	src       []byte
	transform func(string) (string, bool)
	// footnotes, as mdast-util-to-hast keeps them: identifiers in the order of their first reference, references per id.
	footnoteByID map[string]*east.Footnote
	order        []string
	counts       map[string]int
	// inLink: inside a link's text, where no autolink literal is made.
	inLink int
}

// mdParser is goldmark's CommonMark parser with GFM strikethrough and task lists, and with the pieces where micromark
// reads differently replaced: tables, footnotes, autolink literals, lists after indented code.
var mdParser = func() parser.Parser {
	blocks := parser.DefaultBlockParsers()
	for i, b := range blocks {
		if _, ok := b.Value.(parser.BlockParser); ok && b.Priority == 300 {
			blocks[i] = util.Prioritized(listGuard{inner: parser.NewListParser()}, 300)
		}
	}
	blocks = append(blocks, footnoteDefs)
	inlines := append(parser.DefaultInlineParsers(),
		util.Prioritized(extension.NewStrikethroughParser(), 500), util.Prioritized(extension.NewTaskCheckBoxParser(), 0),
		literalInline, literalAfterAngle, footnoteRefs)
	return parser.NewParser(
		parser.WithBlockParsers(blocks...),
		parser.WithInlineParsers(inlines...),
		parser.WithParagraphTransformers(append(parser.DefaultParagraphTransformers(),
			util.Prioritized(tableGuard{extension.NewTableParagraphTransformer()}, 200))...),
		parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)),
	)
}()

// renderMarkdown is the hast children of a message's Markdown.
func renderMarkdown(source string, transform func(string) (string, bool)) []*hnode {
	src := []byte(source)
	doc := mdParser.Parse(text.NewReader(src), parser.WithContext(parser.NewContext()))
	r := &mdRenderer{src: src, transform: transform, footnoteByID: map[string]*east.Footnote{}, counts: map[string]int{}}
	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if fn, ok := n.(*east.Footnote); ok && entering {
			if id := footnoteID(fn.Ref); r.footnoteByID[id] == nil {
				r.footnoteByID[id] = fn
			}
		}
		return gast.WalkContinue, nil
	})
	var blocks []*hnode
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		blocks = append(blocks, r.block(c)...)
	}
	out := wrap(blocks, false)
	if foot := r.footer(); foot != nil {
		out = append(out, eol(), foot)
	}
	return out
}

func footnoteID(ref []byte) string {
	return strings.ToUpper(normalizeLabel(string(ref)))
}

// normalizeLabel is micromark's normalizeIdentifier: whitespace collapsed and trimmed, case folded.
func normalizeLabel(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }), " ")
	return strings.ToUpper(strings.ToLower(s))
}

func (r *mdRenderer) all(n gast.Node) []*hnode {
	var out []*hnode
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.block(c)...)
	}
	return out
}

// block is the hast of a node (an inline one too: the walk is shared).
func (r *mdRenderer) block(n gast.Node) []*hnode {
	switch n := n.(type) {
	case *gast.Paragraph:
		return []*hnode{el("p", r.inlines(n)...)}
	case *gast.TextBlock:
		return []*hnode{el("p", r.inlines(n)...)}
	case *gast.Heading:
		return []*hnode{el("h"+strconv.Itoa(n.Level), r.inlines(n)...)}
	case *gast.ThematicBreak:
		return []*hnode{el("hr")}
	case *gast.Blockquote:
		return []*hnode{el("blockquote", wrap(r.all(n), true)...)}
	case *gast.CodeBlock:
		return []*hnode{codeBlock(r.lines(n), "", false)}
	case *gast.FencedCodeBlock:
		lang, hasLang := "", false
		if n.Info != nil {
			// The info's first word (up to a space or tab), decoded, then cut at any JavaScript whitespace.
			info := strings.TrimLeft(string(n.Info.Segment.Value(r.src)), " \t")
			if i := strings.IndexAny(info, " \t"); i >= 0 {
				info = info[:i]
			}
			if info = decodeText(info); info != "" {
				hasLang = true
				if i := strings.IndexFunc(info, jscompat.IsSpace); i >= 0 {
					info = info[:i]
				}
				lang = info
			}
		}
		return []*hnode{codeBlock(r.lines(n), lang, hasLang)}
	case *gast.HTMLBlock:
		v := r.lines(n)
		if n.HasClosure() {
			v += string(n.ClosureLine.Value(r.src))
		}
		if _, quoted := n.Parent().(*gast.Blockquote); !quoted && n.HTMLBlockType <= gast.HTMLBlockType5 && !htmlClosed(n.HTMLBlockType, v) {
			// Unclosed, it runs to the end of its container with every line ending.
			return []*hnode{txt(v)}
		}
		return []*hnode{txt(trimFinalEOL(v))}
	case *gast.List:
		return []*hnode{r.list(n)}
	case *east.Table:
		return []*hnode{r.table(n)}
	case *gast.LinkReferenceDefinition, *east.Footnote:
		return nil
	}
	return r.inline(n)
}

func trimFinalEOL(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\r") {
		return s[:len(s)-1]
	}
	return s
}

func (r *mdRenderer) lines(n gast.Node) string {
	var b bytes.Buffer
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		seg := l.At(i)
		v := seg.Value(r.src)
		// goldmark gives a blank line inside a container its line feed alone; the source had a CRLF there.
		if len(v) == 1 && v[0] == '\n' && seg.Start > 0 && r.src[seg.Start-1] == '\r' {
			v = []byte("\r\n")
		}
		b.Write(v)
	}
	return b.String()
}

func codeBlock(value, lang string, hasLang bool) *hnode {
	value = trimFinalEOL(value)
	code := el("code")
	if hasLang {
		code.attr("class", "language-"+lang)
	}
	if value != "" {
		code.kids = []*hnode{txt(value + "\n")}
	}
	return el("pre", code)
}

func (r *mdRenderer) list(n *gast.List) *hnode {
	loose := !n.IsTight
	tag := "ul"
	if n.IsOrdered() {
		tag = "ol"
	}
	out := el(tag)
	if n.IsOrdered() && n.Start != 1 {
		out.attr("start", strconv.Itoa(n.Start))
	}
	var items []*hnode
	task := false
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		item, isTask := r.listItem(c, loose)
		task = task || isTask
		items = append(items, item)
	}
	if task {
		out.attr("class", "contains-task-list")
	}
	out.kids = wrap(items, true)
	return out
}

func (r *mdRenderer) listItem(n gast.Node, loose bool) (*hnode, bool) {
	results := r.all(n)
	li := el("li")
	isTask := false
	if len(results) > 0 && results[0].tag == "p" && len(results[0].kids) > 0 && results[0].kids[0].tag == "input" {
		isTask = true
		// mdast drops the one whitespace character after the brackets from the text that follows; hast puts a space back.
		head := results[0]
		rest := head.kids[1:]
		if len(rest) > 0 && rest[0].tag == "" {
			_, size := utf8.DecodeRuneInString(rest[0].text)
			if t := rest[0].text[size:]; t != "" {
				rest = append([]*hnode{txt(t)}, rest[1:]...)
			} else {
				rest = rest[1:]
			}
		}
		head.kids = []*hnode{head.kids[0]}
		if len(rest) > 0 {
			head.kids = append(append(head.kids, txt(" ")), rest...)
		}
		li.attr("class", "task-list-item")
	}
	var kids []*hnode
	for i, child := range results {
		if loose || i != 0 || child.tag != "p" {
			kids = append(kids, eol())
		}
		if child.tag == "p" && !loose {
			kids = append(kids, child.kids...)
		} else {
			kids = append(kids, child)
		}
	}
	if len(results) > 0 {
		tail := results[len(results)-1]
		if loose || tail.tag != "p" {
			kids = append(kids, eol())
		}
	}
	li.kids = kids
	return li, isTask
}

func (r *mdRenderer) table(n *east.Table) *hnode {
	align := n.Alignments
	var rows []*hnode
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		cellTag := "td"
		if _, ok := c.(*east.TableHeader); ok {
			cellTag = "th"
		}
		var cells []*hnode
		i := 0
		for cell := c.FirstChild(); cell != nil && i < len(align); cell = cell.NextSibling() {
			cells = append(cells, r.cell(cellTag, r.inlines(cell), align[i]))
			i++
		}
		for ; i < len(align); i++ {
			cells = append(cells, r.cell(cellTag, nil, align[i]))
		}
		rows = append(rows, el("tr", wrap(cells, true)...))
	}
	head := el("thead", wrap(rows[:1], true)...)
	table := []*hnode{head}
	if len(rows) > 1 {
		table = append(table, el("tbody", wrap(rows[1:], true)...))
	}
	return el("table", wrap(table, true)...)
}

func (r *mdRenderer) cell(tag string, kids []*hnode, a east.Alignment) *hnode {
	c := el(tag, kids...)
	switch a {
	case east.AlignLeft:
		c.attr("style", "text-align:left")
	case east.AlignRight:
		c.attr("style", "text-align:right")
	case east.AlignCenter:
		c.attr("style", "text-align:center")
	}
	return c
}

func (r *mdRenderer) inlines(n gast.Node) []*hnode {
	var out []*hnode
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.inline(c)...)
	}
	return r.mergeTrim(out)
}

// mergeTrim joins adjacent text the way mdast has it (one text node between other inline nodes), makes links of the
// autolink literals in it (outside links), and trims the spaces and tabs around its line endings, as
// mdast-util-to-hast's trimLines does.
func (r *mdRenderer) mergeTrim(nodes []*hnode) []*hnode {
	var merged []*hnode
	for _, n := range nodes {
		if n.tag == "" && n.text == "" {
			continue
		}
		if n.tag == "" && len(merged) > 0 && merged[len(merged)-1].tag == "" && !merged[len(merged)-1].raw() && !n.raw() {
			last := merged[len(merged)-1]
			if last.enc != n.enc {
				last.text, last.enc = last.decoded(), false
				n = txt(n.decoded())
			}
			merged[len(merged)-1] = &hnode{text: last.text + n.text, enc: last.enc}
			continue
		}
		merged = append(merged, n)
	}
	for i, n := range merged {
		if n.tag == "" && n.enc {
			n.text, n.enc = decodeText(n.text), false
		}
		if n.tag == "" && !n.raw() && i+1 < len(merged) && merged[i+1].spaceBreak {
			n.text = strings.TrimRight(n.text, " \t")
		}
	}
	out := make([]*hnode, 0, len(merged))
	for _, n := range merged {
		if n.tag != "" || n.raw() {
			if n.raw() {
				n.attrs = nil
			}
			out = append(out, n)
			continue
		}
		if r.inLink > 0 {
			out = append(out, txt(trimLines(n.text)))
			continue
		}
		for _, p := range linkTexts(n.text) {
			if p.link != nil {
				out = append(out, r.anchor(p.link.url, []*hnode{txt(p.link.label)}))
			} else if p.text != "" {
				out = append(out, txt(trimLines(p.text)))
			}
		}
	}
	return out
}

func (n *hnode) decoded() string {
	if n.enc {
		return decodeText(n.text)
	}
	return n.text
}

// raw marks text that came from inline HTML or a hard break's line feed: it is not merged with its neighbours.
func (n *hnode) raw() bool { return n.tag == "" && len(n.attrs) > 0 }

func rawText(s string) *hnode { return &hnode{text: s, attrs: [][2]string{{"raw", ""}}} }

func trimLines(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	var b strings.Builder
	start := 0
	first := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\n' && c != '\r' {
			continue
		}
		end := i
		if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			i++
		}
		b.WriteString(trimLine(s[start:end], !first, true))
		b.WriteString(s[end : i+1])
		start = i + 1
		first = false
	}
	b.WriteString(trimLine(s[start:], !first, false))
	return b.String()
}

func trimLine(s string, start, end bool) string {
	if start {
		s = strings.TrimLeft(s, " \t")
	}
	if end {
		s = strings.TrimRight(s, " \t")
	}
	return s
}

func (r *mdRenderer) inline(n gast.Node) []*hnode {
	switch n := n.(type) {
	case *gast.Text:
		v := string(n.Segment.Value(r.src))
		if n.HardLineBreak() && (n.Segment.Stop >= len(r.src) || r.src[n.Segment.Stop] != '\\') {
			v = strings.TrimRight(v, " \t")
		}
		node := &hnode{text: v, enc: !n.IsRaw()}
		if n.HardLineBreak() {
			br := el("br")
			// Spaces before a break made of spaces belong to the break; before a backslash they are text.
			br.spaceBreak = n.Segment.Stop >= len(r.src) || r.src[n.Segment.Stop] != '\\'
			return []*hnode{node, br, rawText("\n")}
		}
		if n.SoftLineBreak() {
			node.text += lineEnding(r.src, n.Segment.Stop)
		}
		return []*hnode{node}
	case *gast.String:
		v := string(n.Value)
		if !n.IsRaw() && !n.IsCode() {
			v = decodeText(v)
		}
		return []*hnode{txt(v)}
	case *gast.CodeSpan:
		var b strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			var v []byte
			switch t := c.(type) {
			case *gast.Text:
				v = t.Segment.Value(r.src)
			case *gast.String:
				v = t.Value
			}
			if bytes.HasSuffix(v, []byte("\n")) {
				b.Write(v[:len(v)-1])
				b.WriteByte(' ')
			} else {
				b.Write(v)
			}
		}
		return []*hnode{el("code", rawText(b.String()))}
	case *gast.Emphasis:
		tag := "em"
		if n.Level == 2 {
			tag = "strong"
		}
		return []*hnode{el(tag, r.inlines(n)...)}
	case *east.Strikethrough:
		return []*hnode{el("del", r.inlines(n)...)}
	case *gast.Link:
		r.inLink++
		kids := r.inlines(n)
		r.inLink--
		return []*hnode{r.anchor(decodeText(string(n.Destination)), kids)}
	case *literalLink:
		if r.inLink > 0 {
			return []*hnode{{text: n.label, enc: true}}
		}
		return []*hnode{r.anchor(n.url, []*hnode{rawText(n.label)})}
	case *gast.AutoLink:
		url := string(n.URL(r.src))
		label := string(n.Label(r.src))
		if n.AutoLinkType == gast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(url), "mailto:") {
			url = "mailto:" + url
		}
		return []*hnode{r.anchor(url, []*hnode{txt(label)})}
	case *gast.Image:
		alt := r.plain(n)
		if alt != "" {
			return []*hnode{el("span", txt("[изображение скрыто: "+alt+"]")).attr("class", "tw-share-img")}
		}
		return []*hnode{el("span", txt("[изображение скрыто]")).attr("class", "tw-share-img")}
	case *gast.RawHTML:
		var b bytes.Buffer
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			b.Write(seg.Value(r.src))
		}
		return []*hnode{rawText(b.String())}
	case *east.TaskCheckBox:
		p := n.Parent()
		if p == nil || p.Lines().Len() == 0 || p.Lines().At(0).Start+3 > len(r.src) {
			return nil
		}
		start := p.Lines().At(0).Start
		after := start + 3
		// micromark wants whitespace after the brackets and more of the paragraph after that; otherwise they are text.
		j := after
		for j < len(r.src) && (r.src[j] == ' ' || r.src[j] == '\t') {
			j++
		}
		lineEnd := j >= len(r.src) || r.src[j] == '\n' || r.src[j] == '\r'
		if j == after && !lineEnd || lineEnd && p.Lines().Len() < 2 {
			return []*hnode{{text: string(r.src[start:after]), enc: true}}
		}
		in := el("input").attr("type", "checkbox").attr("disabled", "")
		if n.IsChecked {
			in.attr("checked", "")
		}
		// goldmark drops the end of a line the brackets end; two spaces before it make it a hard break in mdast.
		if lineEnd && strings.HasSuffix(string(r.src[after:j]), "  ") {
			return []*hnode{in, el("br"), rawText("\n")}
		}
		// The whitespace goldmark skipped after the brackets, which the text after them keeps; at the end of a line
		// the text starts on the next one.
		gap := " "
		if !lineEnd {
			gap = string(r.src[after:j])
			if next, ok := n.NextSibling().(*gast.Text); ok && next.Segment.Start >= after && next.Segment.Start <= j {
				gap = string(r.src[after:next.Segment.Start])
			}
		}
		return []*hnode{in, {text: gap, enc: true}}
	case *footnoteRef:
		return r.footnoteRef(n.id)
	}
	if n.HasChildren() {
		return r.inlines(n)
	}
	return nil
}

// plain is the alt text of an image: its description's text without markup.
func (r *mdRenderer) plain(n gast.Node) string {
	var b strings.Builder
	var walk func(gast.Node)
	walk = func(n gast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch t := c.(type) {
			case *gast.Text:
				b.WriteString(string(t.Segment.Value(r.src)))
				if t.SoftLineBreak() || t.HardLineBreak() {
					b.WriteByte('\n')
				}
			case *gast.String:
				v := string(t.Value)
				if !t.IsRaw() && !t.IsCode() {
					v = decodeText(v)
				}
				b.WriteString(v)
			case *gast.CodeSpan:
				for _, k := range r.inline(t)[0].kids {
					b.WriteString(k.text)
				}
			case *gast.RawHTML:
				for i := 0; i < t.Segments.Len(); i++ {
					seg := t.Segments.At(i)
					b.WriteString(string(seg.Value(r.src)))
				}
			case *gast.AutoLink:
				b.WriteString(string(t.Label(r.src)))
			case *literalLink:
				b.WriteString(t.label)
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return decodeText(b.String())
}

// anchor is the page's link component: an <a> for a URL that survives the check, a <span> otherwise.
func (r *mdRenderer) anchor(dest string, kids []*hnode) *hnode {
	if href, ok := r.transform(normalizeURI(dest)); ok {
		return el("a", kids...).attr("href", href).attr("rel", linkRel).attr("target", "_blank")
	}
	return el("span", kids...)
}

func (r *mdRenderer) footnoteRef(id string) []*hnode {
	counter := 0
	reuse, seen := r.counts[id]
	if !seen {
		r.order = append(r.order, id)
		counter = len(r.order)
	} else {
		for i, o := range r.order {
			if o == id {
				counter = i + 1
			}
		}
	}
	r.counts[id] = reuse + 1
	// The reference's link points into the page (#…): the URL check drops it, the component leaves a <span>.
	return []*hnode{el("sup", el("span", txt(strconv.Itoa(counter))))}
}

func (r *mdRenderer) footer() *hnode {
	var items []*hnode
	for i := 0; i < len(r.order); i++ {
		id := r.order[i]
		def := r.footnoteByID[id]
		if def == nil {
			continue
		}
		content := r.all(def)
		var back []*hnode
		for k := 1; k <= r.counts[id]; k++ {
			if len(back) > 0 {
				back = append(back, txt(" "))
			}
			span := el("span", txt("↩"))
			if k > 1 {
				span.kids = append(span.kids, el("sup", txt(strconv.Itoa(k))))
			}
			back = append(back, span)
		}
		if len(content) > 0 && content[len(content)-1].tag == "p" {
			tail := content[len(content)-1]
			if len(tail.kids) > 0 && tail.kids[len(tail.kids)-1].tag == "" {
				last := tail.kids[len(tail.kids)-1]
				tail.kids[len(tail.kids)-1] = txt(last.text + " ")
			} else {
				tail.kids = append(tail.kids, txt(" "))
			}
			tail.kids = append(tail.kids, back...)
		} else {
			content = append(content, back...)
		}
		items = append(items, el("li", wrap(content, true)...).attr("id", "user-content-fn-"+normalizeURI(strings.ToLower(id))))
	}
	if len(items) == 0 {
		return nil
	}
	return el("section",
		el("h2", txt("Footnotes")).attr("class", "sr-only").attr("id", "footnote-label"),
		eol(),
		el("ol", wrap(items, true)...),
		eol(),
	).attr("data-footnotes", "true").attr("class", "footnotes")
}

// decodeText resolves backslash escapes and character references in one pass, as a Markdown parser does for text.
func decodeText(s string) string {
	if !strings.ContainsAny(s, `\&`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) && util.IsPunct(s[i+1]) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		if c == '&' {
			if v, n := charRef(s[i:]); n > 0 {
				b.WriteString(v)
				i += n - 1
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// charRef is the value and length of a character reference at the start of s; 0 when there is none.
func charRef(s string) (string, int) {
	end := strings.IndexByte(s, ';')
	if end < 2 || end > 40 {
		return "", 0
	}
	body := s[1:end]
	if body[0] == '#' {
		num := body[1:]
		base := 10
		if len(num) > 0 && (num[0] == 'x' || num[0] == 'X') {
			num, base = num[1:], 16
		}
		if num == "" || (base == 10 && len(num) > 7) || (base == 16 && len(num) > 6) {
			return "", 0
		}
		v, err := strconv.ParseUint(num, base, 32)
		if err != nil {
			return "", 0
		}
		if v == 0 || v > 0x10ffff || (v >= 0xd800 && v <= 0xdfff) {
			return "\uFFFD", end + 1
		}
		return string(rune(v)), end + 1
	}
	for i := 0; i < len(body); i++ {
		if !(body[i] >= 'a' && body[i] <= 'z' || body[i] >= 'A' && body[i] <= 'Z' || body[i] >= '0' && body[i] <= '9') {
			return "", 0
		}
	}
	if e, ok := util.LookUpHTML5EntityByName(body); ok {
		return string(e.Characters), end + 1
	}
	return "", 0
}

// renderMarkdownHTML is a message's Markdown as the page's markup (inside its wrapper).
func renderMarkdownHTML(source string, transform func(string) (string, bool)) string {
	var b strings.Builder
	for _, n := range renderMarkdown(source, transform) {
		n.write(&b)
	}
	return b.String()
}

const linkRel = "noopener noreferrer nofollow ugc"

// tableGuard keeps a lazy continuation line of a list item or a quote from becoming a table's delimiter row, as in
// micromark, where a table never continues lazily.
type tableGuard struct{ inner parser.ParagraphTransformer }

func (g tableGuard) Transform(node *gast.Paragraph, reader text.Reader, pc parser.Context) {
	src := reader.Source()
	lines := node.Lines()
	for i := 1; i < lines.Len(); i++ {
		seg := lines.At(i)
		value := strings.TrimRight(string(seg.Value(src)), "\r\n")
		if strings.Trim(value, "-:| \t") != "" || !strings.Contains(value, "-") {
			continue
		}
		if lazyLine(node, src, seg) {
			return
		}
		// The header row must have as many cells as the delimiter row (goldmark pads it).
		prev := lines.At(i - 1)
		header := strings.TrimRight(string(prev.Value(src)), "\r\n")
		if tableCells(header) != tableCells(value) {
			return
		}
		break
	}
	g.inner.Transform(node, reader, pc)
}

// tableCells counts the cells of a table row: pipes not escaped split it, the outer ones aside.
func tableCells(row string) int {
	row = strings.Trim(row, " \t")
	row = strings.TrimPrefix(row, "|")
	if strings.HasSuffix(row, "|") && !strings.HasSuffix(row, "\\|") {
		row = row[:len(row)-1]
	}
	n := 1
	for i := 0; i < len(row); i++ {
		if row[i] == '\\' {
			i++
			continue
		}
		if row[i] == '|' {
			n++
		}
	}
	return n
}

// lazyLine: the line lacks the indentation of its list item or the marker of its quote.
func lazyLine(node gast.Node, src []byte, seg text.Segment) bool {
	start := seg.Start
	lineStart := start
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	prefix := string(src[lineStart:start])
	col := seg.Padding
	for _, c := range prefix {
		if c == '\t' {
			col += 4 - col%4
		} else {
			col++
		}
	}
	quotes := strings.Count(prefix, ">")
	for p := node.Parent(); p != nil; p = p.Parent() {
		switch p := p.(type) {
		case *gast.ListItem:
			if col < p.Offset {
				return true
			}
		case *gast.Blockquote:
			if quotes == 0 {
				return true
			}
			quotes--
		case *east.Footnote:
			if col < 4 {
				return true
			}
		}
	}
	return false
}

// htmlClosed: the last line of an HTML block of kinds 1–5 meets its end condition.
func htmlClosed(kind gast.HTMLBlockType, v string) bool {
	last := trimFinalEOL(v)
	if i := strings.LastIndexAny(last, "\r\n"); i >= 0 {
		last = last[i+1:]
	}
	switch kind {
	case gast.HTMLBlockType1:
		l := strings.ToLower(last)
		return strings.Contains(l, "</script>") || strings.Contains(l, "</pre>") || strings.Contains(l, "</style>") || strings.Contains(l, "</textarea>")
	case gast.HTMLBlockType2:
		return strings.Contains(last, "-->")
	case gast.HTMLBlockType3:
		return strings.Contains(last, "?>")
	case gast.HTMLBlockType4:
		return strings.Contains(last, ">")
	case gast.HTMLBlockType5:
		return strings.Contains(last, "]]>")
	}
	return true
}

// lineEnding is the line ending at or after i (past trailing spaces), as written.
func lineEnding(src []byte, i int) string {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	if i+1 < len(src) && src[i] == '\r' && src[i+1] == '\n' {
		return "\r\n"
	}
	if i < len(src) && src[i] == '\r' {
		return "\r"
	}
	return "\n"
}

// listGuard: right after an indented code block micromark still reads the next block as interrupting one, so an ordered
// list not starting at 1, or an empty item, does not start a list there.
type listGuard struct{ inner parser.BlockParser }

func (g listGuard) Trigger() []byte { return g.inner.Trigger() }

func (g listGuard) Open(parent gast.Node, reader text.Reader, pc parser.Context) (gast.Node, parser.State) {
	if _, cont := pc.LastOpenedBlock().Node.(*gast.List); !cont {
		if _, code := parent.LastChild().(*gast.CodeBlock); code {
			line, _ := reader.PeekLine()
			if pos := pc.BlockOffset(); pos >= 0 && interruptingItemRefused(line[pos:]) {
				return nil, parser.NoChildren
			}
		}
	}
	return g.inner.Open(parent, reader, pc)
}

// interruptingItemRefused: an ordered marker other than 1, or a marker with nothing after it.
func interruptingItemRefused(line []byte) bool {
	i := 0
	for i < len(line) && i < 9 && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	ordered := i > 0
	if ordered {
		if i >= len(line) || (line[i] != '.' && line[i] != ')') {
			return false
		}
		if n := strings.TrimLeft(string(line[:i]), "0"); n != "1" {
			return true
		}
		i++
	} else {
		if len(line) == 0 || (line[0] != '-' && line[0] != '+' && line[0] != '*') {
			return false
		}
		i = 1
	}
	return strings.TrimSpace(string(line[i:])) == ""
}

func (g listGuard) Continue(node gast.Node, reader text.Reader, pc parser.Context) parser.State {
	return g.inner.Continue(node, reader, pc)
}
func (g listGuard) Close(node gast.Node, reader text.Reader, pc parser.Context) {
	g.inner.Close(node, reader, pc)
}
func (g listGuard) CanInterruptParagraph() bool { return g.inner.CanInterruptParagraph() }
func (g listGuard) CanAcceptIndentedLine() bool { return g.inner.CanAcceptIndentedLine() }

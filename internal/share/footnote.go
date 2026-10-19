package share

import (
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// GFM footnotes as micromark reads them: a label has no whitespace or "[", references match definitions case-insensitively
// wherever in the message they are, and a reference without a definition stays text.

var footnotesKey = parser.NewContextKey()

// footnoteLabel is the label of "[^label]" at the start of b and the length up to the closing bracket; false when it is
// not one.
func footnoteLabel(b []byte) (string, int, bool) {
	if len(b) < 3 || b[0] != '[' || b[1] != '^' {
		return "", 0, false
	}
	for i := 2; i < len(b) && i < 2+1000; i++ {
		switch c := b[i]; c {
		case '\\':
			if i+1 < len(b) && (b[i+1] == '[' || b[i+1] == '\\' || b[i+1] == ']') {
				i++
			}
		case '[', ' ', '\t', '\n', '\r':
			return "", 0, false
		case ']':
			if i == 2 {
				return "", 0, false
			}
			return string(b[2:i]), i + 1, true
		}
	}
	return "", 0, false
}

type footnoteDefParser struct{ inner parser.BlockParser }

func (p footnoteDefParser) Trigger() []byte { return []byte{'['} }

func (p footnoteDefParser) Open(parent gast.Node, reader text.Reader, pc parser.Context) (gast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 {
		return nil, parser.NoChildren
	}
	_, n, ok := footnoteLabel(line[pos:])
	if !ok || pos+n >= len(line) || line[pos+n] != ':' {
		return nil, parser.NoChildren
	}
	return p.inner.Open(parent, reader, pc)
}

func (p footnoteDefParser) Continue(node gast.Node, reader text.Reader, pc parser.Context) parser.State {
	return p.inner.Continue(node, reader, pc)
}

// Close keeps the definition where it is (the renderer collects them) and notes its identifier for the references.
func (p footnoteDefParser) Close(node gast.Node, reader text.Reader, pc parser.Context) {
	ids, _ := pc.Get(footnotesKey).(map[string]bool)
	if ids == nil {
		ids = map[string]bool{}
		pc.Set(footnotesKey, ids)
	}
	ids[footnoteID(node.(*east.Footnote).Ref)] = true
}

func (p footnoteDefParser) CanInterruptParagraph() bool { return true }
func (p footnoteDefParser) CanAcceptIndentedLine() bool { return false }

// footnoteRef is a reference to a defined footnote.
type footnoteRef struct {
	gast.BaseInline
	id string
}

var kindFootnoteRef = gast.NewNodeKind("FootnoteRef")

func (n *footnoteRef) Kind() gast.NodeKind           { return kindFootnoteRef }
func (n *footnoteRef) Dump(source []byte, level int) { gast.DumpHelper(n, source, level, nil, nil) }

type footnoteRefParser struct{}

// Trigger: "!" too, since "![^1]" is a "!" and a reference, not an image.
func (footnoteRefParser) Trigger() []byte { return []byte{'[', '!'} }

func (footnoteRefParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	line, seg := block.PeekLine()
	bang := len(line) > 0 && line[0] == '!'
	if bang {
		line = line[1:]
	}
	label, n, ok := footnoteLabel(line)
	if !ok {
		return nil
	}
	ids, _ := pc.Get(footnotesKey).(map[string]bool)
	id := footnoteID([]byte(label))
	if !ids[id] {
		return nil
	}
	if bang {
		gast.MergeOrAppendTextSegment(parent, seg.WithStop(seg.Start+1))
		block.Advance(1)
	}
	block.Advance(n)
	return &footnoteRef{id: id}
}

var (
	footnoteDefs = util.Prioritized(footnoteDefParser{inner: extension.NewFootnoteBlockParser()}, 999)
	footnoteRefs = util.Prioritized(footnoteRefParser{}, 101)
)

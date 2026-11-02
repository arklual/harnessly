package design

import (
	"errors"
	"strings"

	"golang.org/x/net/html"
)

// A small CSS selector for the Document API's query: type, *, #id, .class, [attr], [attr=v] (also ^= $= *= ~=), the
// descendant and child combinators and a comma list. Nothing else (no pseudo-classes): a selector that asks for more is
// refused, not read as something it is not.

type selAttr struct {
	name, op, value string
}

type selCompound struct {
	tag     string
	id      string
	classes []string
	attrs   []selAttr
	// combinator joins this compound to the one on its left: ' ' (a descendant), '>' (a child) or 0 for the first.
	combinator byte
}

type selChain []selCompound

var errSelector = errors.New("not a selector this API reads: type, #id, .class, [attr], and the combinators ' ' and '>'")

func isIdentByte(c byte) bool {
	return c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

// ValidSelector says whether the Document API reads a selector.
func ValidSelector(s string) error {
	_, err := parseSelector(s)
	return err
}

// parseSelector reads a comma list of chains.
func parseSelector(s string) ([]selChain, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 512 {
		return nil, errSelector
	}
	var out []selChain
	for _, part := range splitTop(s) {
		chain, err := parseChain(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, chain)
	}
	return out, nil
}

// splitTop splits on commas outside brackets and quotes.
func splitTop(s string) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

func parseChain(s string) (selChain, error) {
	if s == "" {
		return nil, errSelector
	}
	var chain selChain
	var comb byte
	i := 0
	for i < len(s) {
		for i < len(s) && s[i] == ' ' {
			if comb == 0 && len(chain) > 0 {
				comb = ' '
			}
			i++
		}
		if i < len(s) && s[i] == '>' {
			if len(chain) == 0 {
				return nil, errSelector
			}
			comb = '>'
			i++
			continue
		}
		if i >= len(s) {
			break
		}
		c, n, err := parseCompound(s[i:])
		if err != nil {
			return nil, err
		}
		c.combinator = comb
		chain = append(chain, c)
		comb = 0
		i += n
	}
	if len(chain) == 0 || comb == '>' {
		return nil, errSelector
	}
	return chain, nil
}

func parseCompound(s string) (selCompound, int, error) {
	var c selCompound
	i := 0
	ident := func() string {
		j := i
		for j < len(s) && isIdentByte(s[j]) {
			j++
		}
		w := s[i:j]
		i = j
		return w
	}
	if i < len(s) && s[i] == '*' {
		i++
	} else if i < len(s) && isIdentByte(s[i]) {
		c.tag = strings.ToLower(ident())
	}
	for i < len(s) {
		switch s[i] {
		case '#':
			i++
			if c.id = ident(); c.id == "" {
				return c, 0, errSelector
			}
		case '.':
			i++
			cl := ident()
			if cl == "" {
				return c, 0, errSelector
			}
			c.classes = append(c.classes, cl)
		case '[':
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				return c, 0, errSelector
			}
			a, err := parseAttrSel(s[i+1 : i+end])
			if err != nil {
				return c, 0, err
			}
			c.attrs = append(c.attrs, a)
			i += end + 1
		case ' ', '>':
			return c, i, nil
		default:
			return c, 0, errSelector
		}
	}
	if c.tag == "" && c.id == "" && len(c.classes) == 0 && len(c.attrs) == 0 && !strings.HasPrefix(s, "*") {
		return c, 0, errSelector
	}
	return c, i, nil
}

func parseAttrSel(s string) (selAttr, error) {
	s = strings.TrimSpace(s)
	op := ""
	at := strings.IndexAny(s, "=~^$*|")
	if at < 0 {
		return selAttr{name: strings.ToLower(s)}, validAttrName(s)
	}
	name := strings.ToLower(strings.TrimSpace(s[:at]))
	rest := s[at:]
	switch {
	case strings.HasPrefix(rest, "="):
		op, rest = "=", rest[1:]
	case len(rest) >= 2 && rest[1] == '=' && strings.ContainsRune("~^$*", rune(rest[0])):
		op, rest = rest[:2], rest[2:]
	default:
		return selAttr{}, errSelector
	}
	v := strings.TrimSpace(rest)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	return selAttr{name: name, op: op, value: v}, validAttrName(name)
}

func validAttrName(n string) error {
	if n == "" {
		return errSelector
	}
	for i := 0; i < len(n); i++ {
		if !isIdentByte(n[i]) && n[i] != ':' {
			return errSelector
		}
	}
	return nil
}

// tagOf is an element's name as the author wrote it: the engine's renamed tags (sc-raw-td, sc-helmet) are the tags they stand for.
func tagOf(n *html.Node) string {
	for src, enc := range dcRawWrap {
		if n.Data == enc {
			return src
		}
	}
	if n.Data == "sc-helmet" {
		return "helmet"
	}
	return n.Data
}

func attrOf(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

func (c selCompound) matches(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	if c.tag != "" && tagOf(n) != c.tag {
		return false
	}
	if c.id != "" {
		if v, ok := attrOf(n, "id"); !ok || v != c.id {
			return false
		}
	}
	if len(c.classes) > 0 {
		have := strings.Fields(attrValue(n, "class"))
		for _, want := range c.classes {
			found := false
			for _, h := range have {
				if h == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for _, a := range c.attrs {
		v, ok := attrOf(n, a.name)
		if !ok {
			return false
		}
		switch a.op {
		case "":
		case "=":
			ok = v == a.value
		case "^=":
			ok = a.value != "" && strings.HasPrefix(v, a.value)
		case "$=":
			ok = a.value != "" && strings.HasSuffix(v, a.value)
		case "*=":
			ok = a.value != "" && strings.Contains(v, a.value)
		case "~=":
			ok = false
			for _, w := range strings.Fields(v) {
				if w == a.value {
					ok = true
				}
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func attrValue(n *html.Node, name string) string {
	v, _ := attrOf(n, name)
	return v
}

// matches says whether n is the last compound of the chain with the rest to its left as the combinators say.
func (ch selChain) matches(n *html.Node) bool { return ch.matchFrom(len(ch)-1, n) }

func (ch selChain) matchFrom(i int, n *html.Node) bool {
	if !ch[i].matches(n) {
		return false
	}
	if i == 0 {
		return true
	}
	switch ch[i].combinator {
	case '>':
		return n.Parent != nil && ch.matchFrom(i-1, n.Parent)
	default:
		for p := n.Parent; p != nil; p = p.Parent {
			if ch.matchFrom(i-1, p) {
				return true
			}
		}
		return false
	}
}

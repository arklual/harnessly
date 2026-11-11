package designkit

// A reader of the JSX in a page module (.tsx, .jsx) for the strict check: the elements with their attributes and
// places, and the imports. It is not a JavaScript parser: it skips strings, template literals, comments and regular
// expressions, and takes a `<` for the start of an element where an expression may start (after `(`, `=`, `return`,
// `=>`, `&&`, `?`, `:`, `,` …), which is how TSX itself tells an element from a comparison or a type argument. A `<`
// that does not open a well-formed element is read as an operator, so a source the reader cannot follow loses findings,
// never invents them.

import (
	"regexp"
	"sort"
	"strings"
)

// JSXElement is one element of a source; Name is "" for a fragment.
type JSXElement struct {
	Name  string
	Off   int
	Attrs []JSXAttr
	// CloseOff is where the name of the closing tag starts, -1 for a self-closing element (or a fragment).
	CloseOff int
}

// JSXAttr is an attribute of an element. Kind: 's' a string ("…"), 'e' an expression ({…}), 'b' a bare name (true),
// '.' a spread ({...x}), 'j' an element as the value. Value is the string or the expression's source; Off is where
// Value starts in the source (the attribute's name for a bare one).
type JSXAttr struct {
	Name  string
	Kind  byte
	Value string
	Off   int
	At    int // where the attribute's name starts
}

// JSXImport is one import of a source: local name -> imported name ("default", "*" for a namespace import).
type JSXImport struct {
	From  string
	Names map[string]string
	Off   int
	// End is where the statement ends: after its ";" when it has one.
	End int
}

type jsxReader struct {
	src   string
	elems []JSXElement
	// depth is how deep the reader is in elements, braces and templates; past jsxMaxDepth it stops descending.
	depth int
	// failed are the offsets of `<` that did not open an element, so a source is never read twice from them.
	failed map[int]bool
}

// jsxMaxDepth bounds the nesting the reader follows: a source nested deeper is read as text from there on (losing
// findings, not inventing them), so a hostile file cannot exhaust the stack.
const jsxMaxDepth = 256

// ScanJSX reads the elements of a source, in source order.
func ScanJSX(src string) []JSXElement {
	r := &jsxReader{src: src, failed: map[int]bool{}}
	r.js(0, false)
	sort.SliceStable(r.elems, func(i, j int) bool { return r.elems[i].Off < r.elems[j].Off })
	return r.elems
}

var importRe = regexp.MustCompile(`(?s)\bimport\s+(type\s+)?([^;'"]*?)\s*\bfrom\s*["']([^"']+)["']`)

// ScanImports reads the imports of a source; type-only imports are left out.
func ScanImports(src string) []JSXImport {
	var out []JSXImport
	for _, m := range importRe.FindAllStringSubmatchIndex(src, -1) {
		if m[2] >= 0 {
			continue
		}
		clause, from := src[m[4]:m[5]], src[m[6]:m[7]]
		end := m[1]
		if end < len(src) && src[end] == ';' {
			end++
		}
		imp := JSXImport{From: from, Names: map[string]string{}, Off: m[0], End: end}
		rest := clause
		if i := strings.Index(rest, "{"); i >= 0 {
			j := strings.Index(rest, "}")
			if j < i {
				continue
			}
			for _, part := range strings.Split(rest[i+1:j], ",") {
				f := strings.Fields(part)
				if len(f) == 0 || f[0] == "type" {
					continue
				}
				switch {
				case len(f) == 3 && f[1] == "as":
					imp.Names[f[2]] = f[0]
				case len(f) == 1:
					imp.Names[f[0]] = f[0]
				}
			}
			rest = rest[:i] + rest[j+1:]
		}
		for _, part := range strings.Split(rest, ",") {
			f := strings.Fields(part)
			switch {
			case len(f) == 3 && f[0] == "*" && f[1] == "as":
				imp.Names[f[2]] = "*"
			case len(f) == 1 && jsIdent(f[0]):
				imp.Names[f[0]] = "default"
			}
		}
		out = append(out, imp)
	}
	return out
}

func jsIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !(c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// exprStart: after these tokens an expression begins, so `<` opens an element and `/` a regular expression.
var exprStart = map[string]bool{"": true, "(": true, ",": true, "{": true, "[": true, ";": true, "return": true,
	"yield": true, "case": true, "default": true, "else": true, "do": true, "in": true, "of": true, "typeof": true,
	"void": true, "await": true, "new": true, "delete": true, "throw": true, "=>": true}

func opChar(c byte) bool { return strings.IndexByte("=!&|?:+-*%^~", c) >= 0 }

// js reads code from i: to the end, or (inBrace) to the `}` that closes the brace opened before i. It returns the
// index after that `}` (or len(src)).
func (r *jsxReader) js(i int, inBrace bool) int {
	s := r.src
	if inBrace {
		if r.depth >= jsxMaxDepth {
			return skipBalanced(s, i)
		}
		r.depth++
		defer func() { r.depth-- }()
	}
	depth := 0
	prev := ""
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return len(s)
			}
			i += end + 4
		case c == '\'' || c == '"':
			i = skipString(s, i)
			prev = "lit"
		case c == '`':
			i = r.template(i)
			prev = "lit"
		case c == '/':
			if exprStart[prev] || opChar(lastByte(prev)) {
				i = skipRegexp(s, i)
				prev = "lit"
			} else {
				i++
				prev = "/"
			}
		case c == '<':
			if exprStart[prev] || opChar(lastByte(prev)) {
				if end, ok := r.element(i); ok {
					i = end
					prev = "lit"
					continue
				}
			}
			i++
			prev = "<"
		case c == '{' || c == '(' || c == '[':
			if c == '{' {
				depth++
			}
			prev = string(c)
			i++
		case c == '}':
			if inBrace && depth == 0 {
				return i + 1
			}
			depth--
			prev = "}"
			i++
		case c == ')' || c == ']':
			prev = string(c)
			i++
		case isIdentStart(c):
			j := i
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			prev = s[i:j]
			if !exprStart[prev] {
				prev = "id"
			}
			i = j
		case c >= '0' && c <= '9':
			for i < len(s) && (isIdentPart(s[i]) || s[i] == '.') {
				i++
			}
			prev = "lit"
		case opChar(c):
			j := i
			for j < len(s) && opChar(s[j]) {
				j++
			}
			if j < len(s) && s[j] == '>' && s[j-1] == '=' {
				j++ // =>
			}
			prev = s[i:j]
			i = j
		case c == '>':
			prev = ">"
			i++
		default:
			prev = string(c)
			i++
		}
	}
	return len(s)
}

func lastByte(s string) byte {
	if s == "" {
		return 0
	}
	return s[len(s)-1]
}

func skipString(s string, i int) int {
	q := s[i]
	i++
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case q:
			return i + 1
		case '\n':
			return i
		}
		i++
	}
	return i
}

func skipRegexp(s string, i int) int {
	i++
	class := false
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '[':
			class = true
		case ']':
			class = false
		case '/':
			if !class {
				i++
				for i < len(s) && isIdentPart(s[i]) {
					i++
				}
				return i
			}
		case '\n':
			return i
		}
		i++
	}
	return i
}

func (r *jsxReader) template(i int) int {
	s := r.src
	i++
	for i < len(s) {
		switch {
		case s[i] == '\\':
			i += 2
		case s[i] == '`':
			return i + 1
		case s[i] == '$' && i+1 < len(s) && s[i+1] == '{':
			i = r.js(i+2, true)
		default:
			i++
		}
	}
	return i
}

func (r *jsxReader) skipSpace(i int) int {
	s := r.src
	for i < len(s) {
		switch {
		case s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r':
			i++
		case strings.HasPrefix(s[i:], "//"):
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return len(s)
			}
			i += end + 4
		default:
			return i
		}
	}
	return i
}

// jsxName reads an element or attribute name at i: letters, digits, _ $ -, and . or : between parts.
func (r *jsxReader) jsxName(i int, dots bool) (string, int) {
	s := r.src
	if i >= len(s) || !isIdentStart(s[i]) {
		return "", i
	}
	j := i
	for j < len(s) && (isIdentPart(s[j]) || s[j] == '-' || (dots && (s[j] == '.' || s[j] == ':')) || (!dots && s[j] == ':')) {
		j++
	}
	return s[i:j], j
}

// element reads the element whose `<` is at i; ok is false (and nothing is kept) when it is not a well-formed element.
func (r *jsxReader) element(i int) (int, bool) {
	if r.failed[i] || r.depth >= jsxMaxDepth {
		return 0, false
	}
	r.depth++
	defer func() { r.depth-- }()
	kept := len(r.elems)
	end, ok := r.elementAt(i)
	if !ok {
		r.elems = r.elems[:kept]
		r.failed[i] = true
	}
	return end, ok
}

// skipBalanced is the index after the `}` that closes a brace opened before i, counting braces only.
func skipBalanced(s string, i int) int {
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return i + 1
			}
			depth--
		}
	}
	return len(s)
}

func (r *jsxReader) elementAt(i int) (int, bool) {
	s := r.src
	j := r.skipSpace(i + 1)
	if j >= len(s) {
		return 0, false
	}
	el := JSXElement{Off: j, CloseOff: -1}
	if s[j] != '>' {
		name, k := r.jsxName(j, true)
		if name == "" {
			return 0, false
		}
		el.Name = name
		j = k
	}
	idx := len(r.elems)
	r.elems = append(r.elems, el)
	for {
		j = r.skipSpace(j)
		if j >= len(s) {
			return 0, false
		}
		switch {
		case strings.HasPrefix(s[j:], "/>"):
			return j + 2, true
		case s[j] == '>':
			end, closeOff, ok := r.children(j+1, el.Name)
			if ok && el.Name != "" {
				r.elems[idx].CloseOff = closeOff
			}
			return end, ok
		case s[j] == '{':
			k := r.skipSpace(j + 1)
			if !strings.HasPrefix(s[k:], "...") {
				return 0, false
			}
			end := r.js(k+3, true)
			r.elems[idx].Attrs = append(r.elems[idx].Attrs, JSXAttr{Kind: '.', Value: s[k+3 : end-1], Off: k + 3, At: j})
			j = end
		default:
			if el.Name == "" {
				return 0, false
			}
			name, k := r.jsxName(j, false)
			if name == "" {
				return 0, false
			}
			at := JSXAttr{Name: name, Kind: 'b', At: j, Off: j}
			k = r.skipSpace(k)
			if k < len(s) && s[k] == '=' {
				k = r.skipSpace(k + 1)
				if k >= len(s) {
					return 0, false
				}
				switch s[k] {
				case '"', '\'':
					q := strings.IndexByte(s[k+1:], s[k])
					if q < 0 {
						return 0, false
					}
					at.Kind, at.Value, at.Off = 's', s[k+1:k+1+q], k+1
					k += q + 2
				case '{':
					end := r.js(k+1, true)
					at.Kind, at.Value, at.Off = 'e', s[k+1:end-1], k+1
					k = end
				case '<':
					end, ok := r.element(k)
					if !ok {
						return 0, false
					}
					at.Kind, at.Value, at.Off = 'j', s[k:end], k
					k = end
				default:
					return 0, false
				}
			}
			r.elems[idx].Attrs = append(r.elems[idx].Attrs, at)
			j = k
		}
	}
}

// children reads an element's content to its closing tag.
func (r *jsxReader) children(i int, name string) (end, closeOff int, ok bool) {
	s := r.src
	for i < len(s) {
		switch s[i] {
		case '{':
			i = r.js(i+1, true)
		case '<':
			j := r.skipSpace(i + 1)
			if j < len(s) && s[j] == '/' {
				k := r.skipSpace(j + 1)
				close, k2 := r.jsxName(k, true)
				k2 = r.skipSpace(k2)
				if close != name || k2 >= len(s) || s[k2] != '>' {
					return 0, 0, false
				}
				return k2 + 1, k, true
			}
			e, ok := r.element(i)
			if !ok {
				return 0, 0, false
			}
			i = e
		default:
			i++
		}
	}
	return 0, 0, false
}

// lineTable holds where each line of a source starts, so a place is found in log time.
type lineTable []int

func newLineTable(src string) lineTable {
	t := lineTable{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			t = append(t, i+1)
		}
	}
	return t
}

// at is the 1-based line and column of an offset.
func (t lineTable) at(off int) (int, int) {
	i := sort.SearchInts(t, off+1) - 1
	if i < 0 {
		i = 0
	}
	return i + 1, off - t[i] + 1
}

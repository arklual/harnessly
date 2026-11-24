package preview

import (
	"bytes"
	"io"
	"regexp"
	"strings"
)

// A neighbour remote (a Vite dev server for a micro frontend) is served under /__tk/p/<port>, but its modules import
// each other by root-absolute specifiers: `from"/x"`, `import"/x"`, `import("/x")`, `new URL("/x", import.meta.url)`.
// The browser would send those to the main slot, so they get the neighbour prefix. Only JavaScript is touched.

var scriptTypes = regexp.MustCompile(`(?i)^(text/javascript|application/javascript)\b`)

// maxHold bounds how much text waits for a decision; a longer candidate is left as it is.
const maxHold = 4096

type jsMode int

const (
	jsCode jsMode = iota
	jsString
	jsTemplate
	jsLineComment
	jsBlockComment
	jsRegex
)

// moduleRewriter prefixes root-absolute module specifiers in code. Strings, templates, comments and regular expressions
// are tracked so text that only looks like an import stays as it is. The bytes pass through unchanged otherwise, so a
// multibyte character split across chunks never matters.
type moduleRewriter struct {
	w        io.Writer
	base     string
	document *pathRewriter
	carry    []byte

	mode       jsMode
	quote      byte
	tpl        []int // brace depth of each open `${`
	regexClass bool
	prev       byte // the last byte consumed
	lastSig    byte // the last byte that is not space or comment
	word       []byte
	lastWord   string
}

func newModuleRewriter(w io.Writer, base string) *moduleRewriter {
	return &moduleRewriter{w: w, base: base}
}

func isIdent(c byte) bool { return isWord(c) || c == '$' || c >= 0x80 }

func isJSSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

const (
	noMatch = iota
	matched
	needMore
)

func skipSpace(t []byte, j int) int {
	for j < len(t) && isJSSpace(t[j]) {
		j++
	}
	return j
}

// literal checks that t[j:] starts with s: the new position, or a verdict when it does not.
func literal(t []byte, j int, s string) (int, int) {
	for k := 0; k < len(s); k++ {
		if j+k >= len(t) {
			return j, needMore
		}
		if t[j+k] != s[k] {
			return j, noMatch
		}
	}
	return j + len(s), matched
}

// keywordEnd checks that the word ends at j.
func keywordEnd(t []byte, j int) int {
	if j >= len(t) {
		return needMore
	}
	if isIdent(t[j]) {
		return noMatch
	}
	return matched
}

// rootQuote: t[j] is a quote followed by one `/` that does not start `//` or the prefix already.
func (m *moduleRewriter) rootQuote(t []byte, j int) int {
	if j >= len(t) {
		return needMore
	}
	if t[j] != '"' && t[j] != '\'' {
		return noMatch
	}
	if j+1 >= len(t) {
		return needMore
	}
	if t[j+1] == '.' && m.document != nil {
		tail := t[j+1:]
		if len(tail) < 3 {
			return needMore
		}
		if bytes.HasPrefix(tail, []byte("./")) || bytes.HasPrefix(tail, []byte("../")) {
			for _, c := range tail {
				if c == t[j] {
					return matched
				}
				if c == '\\' || c == '\n' || c == '\r' {
					return noMatch
				}
			}
			return needMore
		}
	}
	if t[j+1] != '/' {
		return noMatch
	}
	if j+2 >= len(t) {
		return needMore
	}
	if t[j+2] == '/' {
		return noMatch
	}
	have := t[j+1:]
	want := m.base + "/"
	n := min(len(have), len(want))
	if string(have[:n]) == want[:n] {
		if len(have) < len(want) {
			return needMore
		}
		return noMatch
	}
	return matched
}

// matchAt: the offset of the specifier's opening quote when a root-absolute specifier starts at t[0].
func (m *moduleRewriter) matchAt(t []byte) (int, int) {
	switch t[0] {
	case 'f':
		j, v := literal(t, 0, "from")
		if v != matched {
			return 0, v
		}
		j = skipSpace(t, j)
		return j, m.rootQuote(t, j)
	case 'i':
		j, v := literal(t, 0, "import")
		if v != matched {
			return 0, v
		}
		if v = keywordEnd(t, j); v != matched {
			return 0, v
		}
		j = skipSpace(t, j)
		if j < len(t) && t[j] == '(' {
			j = skipSpace(t, j+1)
		}
		return j, m.rootQuote(t, j)
	case 'n':
		return m.matchURL(t)
	}
	return 0, noMatch
}

// matchURL is `new URL("/x", import.meta.url)`.
func (m *moduleRewriter) matchURL(t []byte) (int, int) {
	j, v := literal(t, 0, "new")
	if v != matched {
		return 0, v
	}
	k := skipSpace(t, j)
	if k == j {
		return 0, boolVerdict(k < len(t))
	}
	if j, v = literal(t, k, "URL"); v != matched {
		return 0, v
	}
	j = skipSpace(t, j)
	if j, v = literal(t, j, "("); v != matched {
		return 0, v
	}
	j = skipSpace(t, j)
	if v = m.rootQuote(t, j); v != matched {
		return 0, v
	}
	q, end := t[j], j+1
	for ; end < len(t) && t[end] != q; end++ {
		if t[end] == '\\' {
			end++
		}
		if end < len(t) && (t[end] == '\n' || t[end] == '\r') {
			return 0, noMatch
		}
	}
	if end >= len(t) {
		return 0, needMore
	}
	k = skipSpace(t, end+1)
	if k, v = literal(t, k, ","); v != matched {
		return 0, v
	}
	k = skipSpace(t, k)
	if k, v = literal(t, k, "import.meta.url"); v != matched {
		return 0, v
	}
	if v = keywordEnd(t, k); v != matched {
		return 0, v
	}
	return j, matched
}

// boolVerdict: more text decides only when there is room for it.
func boolVerdict(more bool) int {
	if more {
		return noMatch
	}
	return needMore
}

var regexAfterWord = map[string]bool{"return": true, "typeof": true, "case": true, "do": true, "else": true, "in": true, "of": true,
	"void": true, "delete": true, "throw": true, "new": true, "instanceof": true, "yield": true, "await": true}

// regexAllowed: a `/` after this starts a regular expression and not a division.
func (m *moduleRewriter) regexAllowed() bool {
	if len(m.word) > 0 {
		return regexAfterWord[string(m.word)]
	}
	if m.lastSig == 0 {
		return true
	}
	if isIdent(m.lastSig) {
		return regexAfterWord[m.lastWord]
	}
	return bytes.IndexByte([]byte("(,=:[!&|?{;+-*%<>~^"), m.lastSig) >= 0
}

func (m *moduleRewriter) endWord() {
	if len(m.word) > 0 {
		m.lastWord = string(m.word)
		m.word = m.word[:0]
	}
}

// step consumes one byte in code mode.
func (m *moduleRewriter) code(t []byte, i int) int {
	c := t[i]
	if isIdent(c) {
		m.word = append(m.word, c)
		m.lastSig = c
		return 1
	}
	m.endWord()
	switch {
	case isJSSpace(c):
		return 1
	case c == '"' || c == '\'':
		m.mode, m.quote = jsString, c
	case c == '`':
		m.mode = jsTemplate
	case c == '/' && i+1 < len(t) && t[i+1] == '/':
		m.mode = jsLineComment
		return 2
	case c == '/' && i+1 < len(t) && t[i+1] == '*':
		m.mode = jsBlockComment
		return 2
	case c == '/' && i+1 >= len(t):
		return 0
	case c == '/' && m.regexAllowed():
		m.mode, m.regexClass = jsRegex, false
	case c == '{' && len(m.tpl) > 0:
		m.tpl[len(m.tpl)-1]++
	case c == '}' && len(m.tpl) > 0:
		top := len(m.tpl) - 1
		if m.tpl[top] == 0 {
			m.tpl = m.tpl[:top]
			m.mode = jsTemplate
		} else {
			m.tpl[top]--
		}
	}
	m.lastSig = c
	return 1
}

// escapeLen: a backslash and what it escapes, 0 while that is not here yet.
func escapeLen(t []byte, i int) int {
	if i+1 >= len(t) {
		return 0
	}
	return 2
}

// other consumes one unit outside code.
func (m *moduleRewriter) other(t []byte, i int) int {
	c := t[i]
	switch m.mode {
	case jsString:
		if c == '\\' {
			return escapeLen(t, i)
		}
		if c == m.quote || c == '\n' {
			m.mode = jsCode
		}
	case jsTemplate:
		if c == '\\' {
			return escapeLen(t, i)
		}
		if c == '`' {
			m.mode = jsCode
		}
		if c == '$' && i+1 < len(t) && t[i+1] == '{' {
			m.tpl = append(m.tpl, 0)
			m.mode = jsCode
			return 2
		}
		if c == '$' && i+1 >= len(t) {
			return 0
		}
	case jsLineComment:
		if c == '\n' {
			m.mode = jsCode
		}
	case jsBlockComment:
		if c == '*' && i+1 < len(t) && t[i+1] == '/' {
			m.mode = jsCode
			return 2
		}
		if c == '*' && i+1 >= len(t) {
			return 0
		}
	case jsRegex:
		switch {
		case c == '\\':
			return escapeLen(t, i)
		case c == '[':
			m.regexClass = true
		case c == ']':
			m.regexClass = false
		case c == '/' && !m.regexClass, c == '\n':
			m.mode = jsCode
		}
	}
	return 1
}

func (m *moduleRewriter) process(text []byte, final bool) error {
	var out []byte
	i := 0
	for i < len(text) {
		if m.mode == jsCode && (text[i] == 'f' || text[i] == 'i' || text[i] == 'n') && !isIdent(m.prev) && m.prev != '.' && m.prev != '$' {
			q, v := m.matchAt(text[i:])
			if v == needMore && !final && len(text)-i < maxHold {
				break
			}
			if v == matched {
				end := i + q + 1
				out = append(out, text[i:end]...)
				if text[end] == '.' {
					close := end + bytes.IndexByte(text[end:], text[end-1])
					address := m.document.fix(string(text[end:close]))
					// A path segment inherited from the document may contain the literal delimiter.
					address = strings.ReplaceAll(address, "'", "%27")
					address = strings.ReplaceAll(address, "\"", "%22")
					out = append(out, address...)
					out = append(out, text[close])
					m.prev, m.lastSig, m.word = text[close], text[close], m.word[:0]
					i = close + 1
					continue
				}
				out = append(out, m.base...)
				m.prev, m.lastSig, m.word = text[end-1], text[end-1], m.word[:0]
				m.mode, m.quote = jsString, text[end-1]
				i = end
				continue
			}
		}
		var n int
		if m.mode == jsCode {
			n = m.code(text, i)
		} else {
			n = m.other(text, i)
		}
		if n == 0 {
			if final {
				n = 1
			} else {
				break
			}
		}
		out = append(out, text[i:i+n]...)
		m.prev = text[i+n-1]
		i += n
	}
	m.carry = append(m.carry[:0], text[i:]...)
	if len(out) == 0 {
		return nil
	}
	_, err := m.w.Write(out)
	return err
}

func (m *moduleRewriter) Write(b []byte) (int, error) {
	text := append(append([]byte{}, m.carry...), b...)
	if err := m.process(text, false); err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close writes what is left.
func (m *moduleRewriter) Close() error {
	text := append([]byte{}, m.carry...)
	return m.process(text, true)
}

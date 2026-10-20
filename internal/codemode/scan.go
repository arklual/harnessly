package codemode

import "strings"

// Object is an object literal: its keys in source order and the values the scanner could read (nil for an expression).
type Object struct {
	keys []string
	vals map[string]any
}

func (o *Object) str(key string) string {
	if o == nil {
		return ""
	}
	s, _ := o.vals[key].(string)
	return s
}

// list is the objects in an array literal under key; a single object counts as a list of one.
func (o *Object) list(key string) []any {
	if o == nil {
		return nil
	}
	var out []any
	switch v := o.vals[key].(type) {
	case []any:
		for _, item := range v {
			if _, ok := item.(*Object); ok {
				out = append(out, item)
			}
		}
	case *Object:
		out = append(out, v)
	}
	return out
}

func (o *Object) firstString() string {
	if o == nil {
		return ""
	}
	for _, k := range o.keys {
		if s, ok := o.vals[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// Calls lists the tools.<name>(...) calls of a script in source order, skipping text inside strings and comments.
func Calls(js string) []Call {
	var out []Call
	for i := 0; i < len(js); {
		switch c := js[i]; {
		case c == '"' || c == '\'' || c == '`':
			i = skipString(js, i)
		case c == '/' && i+1 < len(js) && js[i+1] == '/':
			i = skipLine(js, i)
		case c == '/' && i+1 < len(js) && js[i+1] == '*':
			i = skipBlockComment(js, i)
		case c == 't' && strings.HasPrefix(js[i:], "tools.") && (i == 0 || !isIdentByte(js[i-1]) && js[i-1] != '.'):
			j := i + len("tools.")
			k := j
			for k < len(js) && isIdentByte(js[k]) {
				k++
			}
			name := js[j:k]
			p := skipSpace(js, k)
			if name == "" || p >= len(js) || js[p] != '(' {
				i = k
				continue
			}
			v, next := parseValue(js, p+1)
			out = append(out, Call{Name: name, Arg: v})
			i = next
		default:
			i++
		}
	}
	return out
}

func skipLine(s string, i int) int {
	for i < len(s) && s[i] != '\n' {
		i++
	}
	return i
}

func skipBlockComment(s string, i int) int {
	if j := strings.Index(s[i+2:], "*/"); j >= 0 {
		return i + 2 + j + 2
	}
	return len(s)
}

// skipSpace skips blanks and comments.
func skipSpace(s string, i int) int {
	for i < len(s) {
		switch {
		case s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r':
			i++
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '/':
			i = skipLine(s, i)
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			i = skipBlockComment(s, i)
		default:
			return i
		}
	}
	return i
}

// skipString returns the index after the string literal that starts at i.
func skipString(s string, i int) int {
	q := s[i]
	for i++; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case q:
			return i + 1
		}
	}
	return len(s)
}

// parseValue reads the value that starts at i (after blanks) and returns the index after it. A value that is not a
// literal is skipped to the end of the expression and comes back as nil.
func parseValue(s string, i int) (any, int) {
	i = skipSpace(s, i)
	if i >= len(s) {
		return nil, i
	}
	switch s[i] {
	case '"', '\'', '`':
		v, next, ok := parseString(s, i)
		next = skipSpace(s, next)
		for ok && next < len(s) && s[next] == '+' && next+1 < len(s) {
			// "a" + "b"
			j := skipSpace(s, next+1)
			if j >= len(s) || (s[j] != '"' && s[j] != '\'' && s[j] != '`') {
				break
			}
			w, after, ok2 := parseString(s, j)
			if !ok2 {
				ok = false
				break
			}
			v, next = v+w, skipSpace(s, after)
		}
		if ok && (next >= len(s) || strings.IndexByte(",)]}", s[next]) >= 0 || s[next] == ';') {
			return v, next
		}
		return nil, skipExpr(s, next)
	case '{':
		return parseObject(s, i)
	case '[':
		return parseArray(s, i)
	}
	return nil, skipExpr(s, i)
}

// skipExpr moves to the next , ) ] } that is not inside brackets or a string.
func skipExpr(s string, i int) int {
	depth := 0
	for i < len(s) {
		switch c := s[i]; c {
		case '"', '\'', '`':
			i = skipString(s, i)
			continue
		case '/':
			if i+1 < len(s) && (s[i+1] == '/' || s[i+1] == '*') {
				i = skipSpace(s, i)
				continue
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return i
			}
			depth--
		case ',', ';':
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return i
}

// parseString decodes the literal at i; ok is false for a template with ${...} in it.
func parseString(s string, i int) (string, int, bool) {
	q := s[i]
	var b strings.Builder
	ok := true
	for i++; i < len(s); i++ {
		c := s[i]
		switch {
		case c == q:
			return b.String(), i + 1, ok
		case c == '$' && q == '`' && i+1 < len(s) && s[i+1] == '{':
			ok = false
			i = skipExpr(s, i+2)
		case c == '\\' && i+1 < len(s):
			i++
			switch e := s[i]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b', 'f', 'v':
				b.WriteByte(' ')
			case '0':
				b.WriteByte(0)
			case 'x':
				if r, n := hexRune(s, i+1, 2); n > 0 {
					b.WriteRune(r)
					i += n
				}
			case 'u':
				if i+1 < len(s) && s[i+1] == '{' {
					if end := strings.IndexByte(s[i:], '}'); end > 0 {
						if r, n := hexRune(s[:i+end], i+2, end-2); n > 0 {
							b.WriteRune(r)
						}
						i += end
					}
				} else if r, n := hexRune(s, i+1, 4); n > 0 {
					b.WriteRune(r)
					i += n
				}
			case '\n':
			default:
				b.WriteByte(e)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), len(s), ok
}

// hexRune reads up to n hex digits at s[i:] and returns the rune and how many digits it took (0 for none).
func hexRune(s string, i, n int) (rune, int) {
	var r rune
	k := 0
	for ; k < n && i+k < len(s); k++ {
		c := s[i+k]
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return r, k
		}
	}
	return r, k
}

func parseArray(s string, i int) (any, int) {
	out := []any{}
	i++
	for {
		i = skipSpace(s, i)
		if i >= len(s) {
			return out, i
		}
		if s[i] == ']' {
			return out, i + 1
		}
		if s[i] == ',' {
			i++
			continue
		}
		var v any
		v, i = parseValue(s, i)
		out = append(out, v)
	}
}

func parseObject(s string, i int) (any, int) {
	o := &Object{vals: map[string]any{}}
	i++
	for {
		i = skipSpace(s, i)
		if i >= len(s) {
			return o, i
		}
		switch s[i] {
		case '}':
			return o, i + 1
		case ',':
			i++
			continue
		}
		var key string
		switch c := s[i]; {
		case c == '"' || c == '\'' || c == '`':
			k, next, _ := parseString(s, i)
			key, i = k, next
		case isIdentByte(c):
			j := i
			for j < len(s) && isIdentByte(s[j]) {
				j++
			}
			key, i = s[i:j], j
		default: // a spread, a computed key or something else: skip it
			i = skipExpr(s, i)
			if i < len(s) && s[i] != '}' {
				i++
			}
			continue
		}
		i = skipSpace(s, i)
		if i >= len(s) || s[i] != ':' { // shorthand {a, b}
			o.set(key, nil)
			continue
		}
		var v any
		v, i = parseValue(s, i+1)
		i = skipExpr(s, i) // whatever is left of an expression like a + b
		o.set(key, v)
	}
}

func (o *Object) set(key string, v any) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

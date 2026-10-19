package transcript

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"harnessly/internal/jsvalue"
)

// codexJSON is JSON.parse into jsvalue values, hand-rolled because transcripts carry megabyte lines and the token
// stream of encoding/json is several times slower. A lone surrogate escape reads as U+FFFD, as jsvalue.Parse reads it.
type codexJSON struct {
	s string
	i int
}

func parseJSONFast(s string) (v any, ok bool) {
	p := codexJSON{s: s}
	p.space()
	v, ok = p.value()
	if !ok {
		return nil, false
	}
	p.space()
	return v, p.i == len(p.s)
}

func (p *codexJSON) space() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *codexJSON) lit(word string, v any) (any, bool) {
	if strings.HasPrefix(p.s[p.i:], word) {
		p.i += len(word)
		return v, true
	}
	return nil, false
}

func (p *codexJSON) value() (any, bool) {
	if p.i >= len(p.s) {
		return nil, false
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		s, ok := p.str()
		return s, ok
	case c == 't':
		return p.lit("true", true)
	case c == 'f':
		return p.lit("false", false)
	case c == 'n':
		return p.lit("null", nil)
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	return nil, false
}

func (p *codexJSON) object() (any, bool) {
	p.i++
	o := jsvalue.NewObject()
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return o, true
	}
	for {
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nil, false
		}
		k, ok := p.str()
		if !ok {
			return nil, false
		}
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, false
		}
		p.i++
		p.space()
		v, ok := p.value()
		if !ok {
			return nil, false
		}
		o.Set(k, v)
		p.space()
		if p.i >= len(p.s) {
			return nil, false
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return o, true
		default:
			return nil, false
		}
	}
}

func (p *codexJSON) array() (any, bool) {
	p.i++
	arr := []any{}
	p.space()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return arr, true
	}
	for {
		p.space()
		v, ok := p.value()
		if !ok {
			return nil, false
		}
		arr = append(arr, v)
		p.space()
		if p.i >= len(p.s) {
			return nil, false
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return arr, true
		default:
			return nil, false
		}
	}
}

func digits(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

func (p *codexJSON) number() (any, bool) {
	start := p.i
	i := p.i
	if p.s[i] == '-' {
		i++
	}
	switch {
	case i < len(p.s) && p.s[i] == '0':
		i++
	case i < len(p.s) && p.s[i] >= '1' && p.s[i] <= '9':
		i = digits(p.s, i)
	default:
		return nil, false
	}
	if i < len(p.s) && p.s[i] == '.' {
		j := digits(p.s, i+1)
		if j == i+1 {
			return nil, false
		}
		i = j
	}
	if i < len(p.s) && (p.s[i] == 'e' || p.s[i] == 'E') {
		i++
		if i < len(p.s) && (p.s[i] == '+' || p.s[i] == '-') {
			i++
		}
		j := digits(p.s, i)
		if j == i {
			return nil, false
		}
		i = j
	}
	p.i = i
	f, err := strconv.ParseFloat(p.s[start:i], 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			return nil, false
		}
	}
	return f, true
}

func codexHex4(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}

func (p *codexJSON) str() (string, bool) {
	p.i++
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			s := p.s[start:p.i]
			p.i++
			return s, true
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			return "", false
		}
		p.i++
	}
	var b strings.Builder
	b.WriteString(p.s[start:p.i])
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return b.String(), true
		case c < 0x20:
			return "", false
		case c != '\\':
			b.WriteByte(c)
			p.i++
			continue
		}
		if p.i+1 >= len(p.s) {
			return "", false
		}
		e := p.s[p.i+1]
		p.i += 2
		switch e {
		case '"', '\\', '/':
			b.WriteByte(e)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			r, ok := codexHex4(p.s[p.i:])
			if !ok {
				return "", false
			}
			p.i += 4
			if utf16.IsSurrogate(r) {
				if r < 0xDC00 && strings.HasPrefix(p.s[p.i:], `\u`) {
					if r2, ok := codexHex4(p.s[p.i+2:]); ok && r2 >= 0xDC00 && r2 <= 0xDFFF {
						p.i += 6
						b.WriteRune(utf16.DecodeRune(r, r2))
						continue
					}
				}
				r = utf8.RuneError
			}
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	return "", false
}

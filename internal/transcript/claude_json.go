package transcript

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// claudeParseJSON is jsvalue.Parse (JSON.parse: a repeated key keeps its first place and its last value, a lone
// surrogate escape reads as U+FFFD) without encoding/json's token stream, which a transcript tail spends most of its
// time in.
func claudeParseJSON(s string) (any, error) {
	p := jsonParser{s: s}
	p.space()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, errJSON
	}
	return v, nil
}

var errJSON = errors.New("invalid JSON")

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) space() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonParser) literal(word string, v any) (any, error) {
	if !strings.HasPrefix(p.s[p.i:], word) {
		return nil, errJSON
	}
	p.i += len(word)
	return v, nil
}

func (p *jsonParser) value() (any, error) {
	if p.i >= len(p.s) {
		return nil, errJSON
	}
	switch c := p.s[p.i]; {
	case c == '{':
		p.i++
		o := jsvalue.NewObject()
		p.space()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return o, nil
		}
		for {
			p.space()
			if p.i >= len(p.s) || p.s[p.i] != '"' {
				return nil, errJSON
			}
			k, err := p.str()
			if err != nil {
				return nil, err
			}
			p.space()
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				return nil, errJSON
			}
			p.i++
			p.space()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			o.Set(k, v)
			p.space()
			if p.i >= len(p.s) {
				return nil, errJSON
			}
			if p.s[p.i] == '}' {
				p.i++
				return o, nil
			}
			if p.s[p.i] != ',' {
				return nil, errJSON
			}
			p.i++
		}
	case c == '[':
		p.i++
		arr := []any{}
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return arr, nil
		}
		for {
			p.space()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
			p.space()
			if p.i >= len(p.s) {
				return nil, errJSON
			}
			if p.s[p.i] == ']' {
				p.i++
				return arr, nil
			}
			if p.s[p.i] != ',' {
				return nil, errJSON
			}
			p.i++
		}
	case c == '"':
		return p.str()
	case c == 't':
		return p.literal("true", true)
	case c == 'f':
		return p.literal("false", false)
	case c == 'n':
		return p.literal("null", nil)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return nil, errJSON
}

func (p *jsonParser) digits() int {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	return p.i - start
}

func (p *jsonParser) number() (any, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	if p.i < len(p.s) && p.s[p.i] == '0' {
		p.i++
	} else if p.digits() == 0 {
		return nil, errJSON
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if p.digits() == 0 {
			return nil, errJSON
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if p.digits() == 0 {
			return nil, errJSON
		}
	}
	f, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil, errJSON
	}
	return f, nil
}

func hex4(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	n, err := strconv.ParseUint(s[:4], 16, 32)
	return rune(n), err == nil
}

func (p *jsonParser) str() (string, error) {
	p.i++
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			s := p.s[start:p.i]
			p.i++
			return s, nil
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			return "", errJSON
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
			return b.String(), nil
		case c < 0x20:
			return "", errJSON
		case c != '\\':
			b.WriteByte(c)
			p.i++
			continue
		}
		if p.i+1 >= len(p.s) {
			return "", errJSON
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
			r, ok := hex4(p.s[p.i:])
			if !ok {
				return "", errJSON
			}
			p.i += 4
			if utf16.IsSurrogate(r) {
				if r < 0xDC00 && strings.HasPrefix(p.s[p.i:], `\u`) {
					if r2, ok := hex4(p.s[p.i+2:]); ok && r2 >= 0xDC00 && r2 <= 0xDFFF {
						p.i += 6
						b.WriteRune(utf16.DecodeRune(r, r2))
						continue
					}
				}
				r = utf8.RuneError
			}
			b.WriteRune(r)
		default:
			return "", errJSON
		}
	}
	return "", errJSON
}

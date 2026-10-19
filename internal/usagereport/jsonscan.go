package usagereport

import (
	"bytes"
	"encoding/json"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// A transcript line is validated once, then read by spans: only the fields the facts need are decoded. encoding/json
// validates its whole input on every call, which on lines carrying whole files made the first pass slower than the
// previous runtime's.

func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func skipWS(b []byte, i int) int {
	for i < len(b) && isWS(b[i]) {
		i++
	}
	return i
}

// valid is JSON.parse's grammar for one value with whitespace around it; the bytes are valid UTF-8 already.
func valid(b []byte) bool {
	i, ok := validValue(b, skipWS(b, 0), 0)
	return ok && skipWS(b, i) == len(b)
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func validString(b []byte, i int) (int, bool) {
	i++ // the opening quote
	for {
		for i < len(b) && b[i] >= 0x20 && b[i] != '"' && b[i] != '\\' {
			i++
		}
		if i >= len(b) || b[i] < 0x20 {
			return i, false
		}
		if b[i] == '"' {
			return i + 1, true
		}
		if i+1 >= len(b) {
			return i, false
		}
		switch b[i+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			i += 2
		case 'u':
			if i+5 >= len(b) || !isHexDigit(b[i+2]) || !isHexDigit(b[i+3]) || !isHexDigit(b[i+4]) || !isHexDigit(b[i+5]) {
				return i, false
			}
			i += 6
		default:
			return i, false
		}
	}
}

func digits(b []byte, i int) int {
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	return i
}

func validNumber(b []byte, i int) (int, bool) {
	if b[i] == '-' {
		i++
	}
	switch {
	case i < len(b) && b[i] == '0':
		i++
	case i < len(b) && b[i] >= '1' && b[i] <= '9':
		i = digits(b, i)
	default:
		return i, false
	}
	if i < len(b) && b[i] == '.' {
		j := digits(b, i+1)
		if j == i+1 {
			return i, false
		}
		i = j
	}
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		i++
		if i < len(b) && (b[i] == '+' || b[i] == '-') {
			i++
		}
		j := digits(b, i)
		if j == i {
			return i, false
		}
		i = j
	}
	return i, true
}

func validValue(b []byte, i, depth int) (int, bool) {
	if i >= len(b) {
		return i, false
	}
	switch c := b[i]; {
	case c == '"':
		return validString(b, i)
	case c == '{':
		i = skipWS(b, i+1)
		if i < len(b) && b[i] == '}' {
			return i + 1, true
		}
		for {
			if i >= len(b) || b[i] != '"' {
				return i, false
			}
			var ok bool
			if i, ok = validString(b, i); !ok {
				return i, false
			}
			i = skipWS(b, i)
			if i >= len(b) || b[i] != ':' {
				return i, false
			}
			if i, ok = validValue(b, skipWS(b, i+1), depth+1); !ok {
				return i, false
			}
			i = skipWS(b, i)
			if i < len(b) && b[i] == '}' {
				return i + 1, true
			}
			if i >= len(b) || b[i] != ',' {
				return i, false
			}
			i = skipWS(b, i+1)
		}
	case c == '[':
		i = skipWS(b, i+1)
		if i < len(b) && b[i] == ']' {
			return i + 1, true
		}
		for {
			var ok bool
			if i, ok = validValue(b, i, depth+1); !ok {
				return i, false
			}
			i = skipWS(b, i)
			if i < len(b) && b[i] == ']' {
				return i + 1, true
			}
			if i >= len(b) || b[i] != ',' {
				return i, false
			}
			i = skipWS(b, i+1)
		}
	case c == '-' || c >= '0' && c <= '9':
		return validNumber(b, i)
	case bytes.HasPrefix(b[i:], []byte("true")):
		return i + 4, true
	case bytes.HasPrefix(b[i:], []byte("false")):
		return i + 5, true
	case bytes.HasPrefix(b[i:], []byte("null")):
		return i + 4, true
	}
	return i, false
}

// skipString ends a string known to be valid.
func skipString(b []byte, i int) int {
	i++
	for {
		j := bytes.IndexByte(b[i:], '"')
		if j < 0 {
			return len(b)
		}
		i += j
		// The quote ends the string unless an odd run of backslashes precedes it.
		n := 0
		for k := i - 1; k >= 0 && b[k] == '\\'; k-- {
			n++
		}
		if n%2 == 0 {
			return i + 1
		}
		i++
	}
}

// skipValue ends a value known to be valid.
func skipValue(b []byte, i int) int {
	switch b[i] {
	case '"':
		return skipString(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				i = skipString(b, i)
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return i
	}
	for i < len(b) && b[i] != ',' && b[i] != '}' && b[i] != ']' && !isWS(b[i]) {
		i++
	}
	return i
}

// decodeString is a valid string's value; a lone surrogate escape becomes U+FFFD.
func decodeString(raw []byte) string {
	in := raw[1 : len(raw)-1]
	if bytes.IndexByte(in, '\\') < 0 {
		return string(in)
	}
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); {
		c := in[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		switch in[i+1] {
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			r, _ := strconv.ParseUint(string(in[i+2:i+6]), 16, 32)
			i += 6
			if utf16.IsSurrogate(rune(r)) {
				if r < 0xdc00 && i+5 < len(in) && in[i] == '\\' && in[i+1] == 'u' {
					if r2, err := strconv.ParseUint(string(in[i+2:i+6]), 16, 32); err == nil && r2 >= 0xdc00 && r2 <= 0xdfff {
						out = utf8.AppendRune(out, utf16.DecodeRune(rune(r), rune(r2)))
						i += 6
						continue
					}
				}
				out = utf8.AppendRune(out, utf8.RuneError)
				continue
			}
			out = utf8.AppendRune(out, rune(r))
			continue
		default:
			out = append(out, in[i+1])
		}
		i += 2
	}
	return string(out)
}

// member is a field of an object: its name as written (escapes kept, flagged) and its value's bytes.
type member struct {
	name    []byte
	escaped bool
	value   []byte
}

// object is a valid JSON object read one level deep; a name given twice means its last value, as in JavaScript.
type object []member

// objectOf is the object a value is; nil for any other value.
func objectOf(raw []byte) object {
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	o := make(object, 0, 16)
	i := skipWS(raw, 1)
	for i < len(raw) && raw[i] == '"' {
		end := skipString(raw, i)
		name := raw[i+1 : end-1]
		i = skipWS(raw, end)
		i = skipWS(raw, i+1) // the colon
		vEnd := skipValue(raw, i)
		o = append(o, member{name: name, escaped: bytes.IndexByte(name, '\\') >= 0, value: raw[i:vEnd]})
		i = skipWS(raw, vEnd)
		if i < len(raw) && raw[i] == ',' {
			i = skipWS(raw, i+1)
		}
	}
	return o
}

func (o object) get(k string) ([]byte, bool) {
	for i := len(o) - 1; i >= 0; i-- {
		m := o[i]
		if m.escaped {
			if decodeString(append(append([]byte{'"'}, m.name...), '"')) == k {
				return m.value, true
			}
		} else if string(m.name) == k {
			return m.value, true
		}
	}
	return nil, false
}

// elements are the values of a valid array.
func elements(raw []byte) [][]byte {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var out [][]byte
	i := skipWS(raw, 1)
	for i < len(raw) && raw[i] != ']' {
		end := skipValue(raw, i)
		out = append(out, raw[i:end])
		i = skipWS(raw, end)
		if i < len(raw) && raw[i] == ',' {
			i = skipWS(raw, i+1)
		}
	}
	return out
}

func (o object) raw(k string) []byte {
	v, _ := o.get(k)
	return v
}

func (o object) str(k string) (string, bool) {
	raw, ok := o.get(k)
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	return decodeString(raw), true
}

// num is a number field as `typeof v === 'number' && isFinite(v) ? v : 0`.
func (o object) num(k string) float64 {
	raw, _ := o.get(k)
	if len(raw) == 0 || !(raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') {
		return 0
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return 0 // out of range: ±Infinity in JavaScript, not finite
	}
	return f
}

// value is the field as a JSON value; present is false for a missing field (undefined), null decodes to nil.
func (o object) value(k string) (v any, present bool) {
	raw, ok := o.get(k)
	if !ok {
		return nil, false
	}
	return decodeValue(raw), true
}

// decodeValue is a valid value decoded, objects and arrays as encoding/json decodes them.
func decodeValue(raw []byte) (v any) {
	switch raw[0] {
	case '"':
		return decodeString(raw)
	case 'n':
		return nil
	case 't':
		return true
	case 'f':
		return false
	case '{', '[':
		if json.Unmarshal(raw, &v) != nil {
			return nil
		}
		return v
	}
	f, _ := strconv.ParseFloat(string(raw), 64)
	return f
}

// nullish is `v ?? fallback` on a field: ok false for undefined and null.
func (o object) nullish(k string) (any, bool) {
	v, present := o.value(k)
	return v, present && v != nil
}

// truthy is JavaScript truthiness of a field.
func (o object) truthy(k string) bool {
	raw, ok := o.get(k)
	if !ok {
		return false
	}
	switch raw[0] {
	case '"':
		return len(raw) > 2
	case 'n', 'f':
		return false
	case 't', '{', '[':
		return true
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	return err != nil || f != 0
}

func (o object) isTrue(k string) bool { return string(o.raw(k)) == "true" }

// is: the field is the JSON string s.
func (o object) is(k, s string) bool {
	v, ok := o.str(k)
	return ok && v == s
}

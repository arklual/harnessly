package messagecounts

import (
	"bytes"
	"encoding/json"
)

// A line is checked once as JSON.parse would take it, then only the fields counted are picked out of it, without
// decoding the rest (tool outputs are most of a transcript's bytes).

func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// valid says whether b is one JSON text, as JSON.parse accepts it.
func valid(b []byte) bool {
	i := skipWS(b, 0)
	i, ok := checkValue(b, i, 0)
	return ok && skipWS(b, i) == len(b)
}

func skipWS(b []byte, i int) int {
	for i < len(b) && isWS(b[i]) {
		i++
	}
	return i
}

const maxDepth = 10000

func checkValue(b []byte, i, depth int) (int, bool) {
	if i >= len(b) || depth > maxDepth {
		return i, false
	}
	switch c := b[i]; {
	case c == '"':
		return checkString(b, i)
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
			if i, ok = checkString(b, i); !ok {
				return i, false
			}
			i = skipWS(b, i)
			if i >= len(b) || b[i] != ':' {
				return i, false
			}
			if i, ok = checkValue(b, skipWS(b, i+1), depth+1); !ok {
				return i, false
			}
			i = skipWS(b, i)
			if i >= len(b) {
				return i, false
			}
			if b[i] == '}' {
				return i + 1, true
			}
			if b[i] != ',' {
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
			if i, ok = checkValue(b, i, depth+1); !ok {
				return i, false
			}
			i = skipWS(b, i)
			if i >= len(b) {
				return i, false
			}
			if b[i] == ']' {
				return i + 1, true
			}
			if b[i] != ',' {
				return i, false
			}
			i = skipWS(b, i+1)
		}
	case c == 't':
		return i + 4, bytes.HasPrefix(b[i:], []byte("true"))
	case c == 'f':
		return i + 5, bytes.HasPrefix(b[i:], []byte("false"))
	case c == 'n':
		return i + 4, bytes.HasPrefix(b[i:], []byte("null"))
	case c == '-' || c >= '0' && c <= '9':
		return checkNumber(b, i)
	}
	return i, false
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func checkString(b []byte, i int) (int, bool) {
	i++
	for {
		j := i
		for j < len(b) && b[j] != '"' && b[j] != '\\' && b[j] >= 0x20 {
			j++
		}
		if j >= len(b) || b[j] < 0x20 {
			return j, false
		}
		if b[j] == '"' {
			return j + 1, true
		}
		if j+1 >= len(b) {
			return j, false
		}
		switch b[j+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			i = j + 2
		case 'u':
			if j+6 > len(b) || !isHex(b[j+2]) || !isHex(b[j+3]) || !isHex(b[j+4]) || !isHex(b[j+5]) {
				return j, false
			}
			i = j + 6
		default:
			return j, false
		}
	}
}

func digits(b []byte, i int) int {
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	return i
}

func checkNumber(b []byte, i int) (int, bool) {
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
			return j, false
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
			return j, false
		}
		i = j
	}
	return i, true
}

// skip is the end of the valid value at i.
func skip(b []byte, i int) int {
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

func skipString(b []byte, i int) int {
	i++
	for {
		j := bytes.IndexByte(b[i:], '"')
		if j < 0 {
			return len(b)
		}
		i += j
		n := 0
		for k := i - 1; b[k] == '\\'; k-- {
			n++
		}
		i++
		if n%2 == 0 {
			return i
		}
	}
}

// str is a valid JSON string's value.
func str(raw []byte) string {
	inner := raw[1 : len(raw)-1]
	if bytes.IndexByte(inner, '\\') < 0 {
		return string(inner)
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// fields are the values of the wanted keys of a valid object, the last of a repeated key winning; nil for absent.
func fields(raw []byte, keys ...string) [][]byte {
	out := make([][]byte, len(keys))
	if len(raw) == 0 || raw[0] != '{' {
		return out
	}
	i := skipWS(raw, 1)
	for i < len(raw) && raw[i] == '"' {
		end := skipString(raw, i)
		key := raw[i:end]
		i = skipWS(raw, end)
		i = skipWS(raw, i+1) // ':'
		vEnd := skip(raw, i)
		name := key[1 : len(key)-1]
		if bytes.IndexByte(name, '\\') >= 0 {
			name = []byte(str(key))
		}
		for n, want := range keys {
			if string(name) == want {
				out[n] = raw[i:vEnd]
			}
		}
		i = skipWS(raw, vEnd)
		if i < len(raw) && raw[i] == ',' {
			i = skipWS(raw, i+1)
		}
	}
	return out
}

// elements are the values of a valid array; nil for anything else.
func elements(raw []byte) [][]byte {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var out [][]byte
	i := skipWS(raw, 1)
	for i < len(raw) && raw[i] != ']' {
		end := skip(raw, i)
		out = append(out, raw[i:end])
		i = skipWS(raw, end)
		if i < len(raw) && raw[i] == ',' {
			i = skipWS(raw, i+1)
		}
	}
	return out
}

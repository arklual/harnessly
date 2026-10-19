package stats

import (
	"bytes"
	"encoding/json"
)

// A transcript line is mostly a tool's output nobody here reads. These scanners check a line is JSON as JSON.parse
// would (one pass, no values built) and hand over the raw bytes of the few members a parser reads; only those are
// decoded. Keys compare exactly, as JavaScript's do, and a repeated key keeps its last value.

func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func skipWS(b []byte, i int) int {
	for i < len(b) && isWS(b[i]) {
		i++
	}
	return i
}

// stringStop marks the bytes a string scan stops at: the quote, a backslash and the control characters.
var stringStop = func() (t [256]bool) {
	for c := 0; c < 0x20; c++ {
		t[c] = true
	}
	t['"'], t['\\'] = true, true
	return t
}()

// skipString is past the string starting at b[i] ('"'); esc says whether it holds an escape.
func skipString(b []byte, i int) (end int, esc, ok bool) {
	i++
	for {
		for i < len(b) && !stringStop[b[i]] {
			i++
		}
		if i >= len(b) {
			return 0, false, false
		}
		switch b[i] {
		case '"':
			return i + 1, esc, true
		case '\\':
			esc = true
			if i+1 >= len(b) {
				return 0, false, false
			}
			switch b[i+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i += 2
			case 'u':
				if i+6 > len(b) || !isHex4(b[i+2:i+6]) {
					return 0, false, false
				}
				i += 6
			default:
				return 0, false, false
			}
		default:
			return 0, false, false
		}
	}
}

func isHex4(b []byte) bool {
	for _, c := range b {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func digits(b []byte, i int) int {
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	return i
}

func skipNumber(b []byte, i int) (int, bool) {
	if i < len(b) && b[i] == '-' {
		i++
	}
	switch {
	case i < len(b) && b[i] == '0':
		i++
	case i < len(b) && b[i] >= '1' && b[i] <= '9':
		i = digits(b, i)
	default:
		return 0, false
	}
	if i < len(b) && b[i] == '.' {
		j := digits(b, i+1)
		if j == i+1 {
			return 0, false
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
			return 0, false
		}
		i = j
	}
	return i, true
}

// skipValue is past the value starting at b[i] (no leading space), checked to the end of any nesting.
func skipValue(b []byte, i int) (int, bool) {
	if i >= len(b) {
		return 0, false
	}
	switch c := b[i]; {
	case c == '"':
		end, _, ok := skipString(b, i)
		return end, ok
	case c == '{' || c == '[':
		return skipContainer(b, i)
	case c == 't':
		return literal(b, i, "true")
	case c == 'f':
		return literal(b, i, "false")
	case c == 'n':
		return literal(b, i, "null")
	}
	return skipNumber(b, i)
}

func literal(b []byte, i int, word string) (int, bool) {
	if bytes.HasPrefix(b[i:], []byte(word)) {
		return i + len(word), true
	}
	return 0, false
}

// skipContainer checks nested objects and arrays without recursion.
func skipContainer(b []byte, i int) (int, bool) {
	stack := make([]byte, 0, 16)
	for {
		open := b[i]
		stack = append(stack, open)
		i = skipWS(b, i+1)
		if i < len(b) && ((open == '{' && b[i] == '}') || (open == '[' && b[i] == ']')) {
			i++
			stack = stack[:len(stack)-1]
			goto closed
		}
	member:
		if stack[len(stack)-1] == '{' {
			if i >= len(b) || b[i] != '"' {
				return 0, false
			}
			end, _, ok := skipString(b, i)
			if !ok {
				return 0, false
			}
			i = skipWS(b, end)
			if i >= len(b) || b[i] != ':' {
				return 0, false
			}
			i = skipWS(b, i+1)
		}
		if i < len(b) && (b[i] == '{' || b[i] == '[') {
			continue
		}
		{
			end, ok := skipValue(b, i)
			if !ok {
				return 0, false
			}
			i = end
		}
	closed:
		for {
			if len(stack) == 0 {
				return i, true
			}
			i = skipWS(b, i)
			if i >= len(b) {
				return 0, false
			}
			top := stack[len(stack)-1]
			if b[i] == ',' {
				i = skipWS(b, i+1)
				goto member
			}
			if (top == '{' && b[i] == '}') || (top == '[' && b[i] == ']') {
				stack = stack[:len(stack)-1]
				i++
				continue
			}
			return 0, false
		}
	}
}

// members calls fn with each member of the object b (a whole value, '{' first) in order; false where b is not valid.
func members(b []byte, fn func(key []byte, esc bool, value []byte)) bool {
	_, ok := membersEnd(b, fn)
	return ok
}

// membersEnd is members, with where the object ends.
func membersEnd(b []byte, fn func(key []byte, esc bool, value []byte)) (int, bool) {
	i := skipWS(b, 1)
	if i < len(b) && b[i] == '}' {
		return i + 1, true
	}
	for {
		if i >= len(b) || b[i] != '"' {
			return 0, false
		}
		kend, esc, ok := skipString(b, i)
		if !ok {
			return 0, false
		}
		key := b[i+1 : kend-1]
		i = skipWS(b, kend)
		if i >= len(b) || b[i] != ':' {
			return 0, false
		}
		i = skipWS(b, i+1)
		vend, ok := skipValue(b, i)
		if !ok {
			return 0, false
		}
		fn(key, esc, b[i:vend])
		i = skipWS(b, vend)
		if i >= len(b) {
			return 0, false
		}
		if b[i] == '}' {
			return i + 1, true
		}
		if b[i] != ',' {
			return 0, false
		}
		i = skipWS(b, i+1)
	}
}

// keyIs compares a member's raw key with a name.
func keyIs(key []byte, esc bool, name string) bool {
	if !esc {
		return string(key) == name
	}
	var s string
	if json.Unmarshal(append(append([]byte{'"'}, key...), '"'), &s) != nil {
		return false
	}
	return s == name
}

// wholeObject is a line holding one JSON object and nothing else; false for anything else (every parser here skipped
// non-objects).
func wholeObject(line []byte) ([]byte, bool) {
	i := skipWS(line, 0)
	if i >= len(line) || line[i] != '{' {
		return nil, false
	}
	end, ok := skipContainer(line, i)
	if !ok || skipWS(line, end) != len(line) {
		return nil, false
	}
	return line[i:end], true
}

// lineMembers is members of a line holding one JSON object and nothing else, in one pass; false for anything else.
// The members are handed over only once the whole line is known good.
func lineMembers(line []byte, fn func(key []byte, esc bool, value []byte)) bool {
	i := skipWS(line, 0)
	if i >= len(line) || line[i] != '{' {
		return false
	}
	type member struct {
		key, value []byte
		esc        bool
	}
	var buf [12]member
	list := buf[:0]
	end, ok := membersEnd(line[i:], func(key []byte, esc bool, value []byte) { list = append(list, member{key, value, esc}) })
	if !ok || skipWS(line, i+end) != len(line) {
		return false
	}
	for _, m := range list {
		fn(m.key, m.esc, m.value)
	}
	return true
}

// value decodes a member's raw bytes: strings without escapes directly, the rest as JSON.parse would.
func value(raw []byte) any {
	if len(raw) >= 2 && raw[0] == '"' && bytes.IndexByte(raw, '\\') < 0 {
		return string(raw[1 : len(raw)-1])
	}
	switch raw[0] {
	case 'n':
		return nil
	case 't':
		return true
	case 'f':
		return false
	}
	var v any
	if decodeLine(raw, &v) {
		return v
	}
	return nil
}

func scanClaudeEntry(line []byte, e *claudeEntry) bool {
	return lineMembers(line, func(key []byte, esc bool, raw []byte) {
		switch {
		case keyIs(key, esc, "timestamp"):
			e.Timestamp = value(raw)
		case keyIs(key, esc, "type"):
			e.Type = value(raw)
		case keyIs(key, esc, "sessionId"):
			e.SessionID = value(raw)
		case keyIs(key, esc, "cwd"):
			e.Cwd = value(raw)
		case keyIs(key, esc, "uuid"):
			e.UUID = value(raw)
		case keyIs(key, esc, "message"):
			e.Message = raw
		}
	})
}

// scanMessage reads an assistant message's members (raw is a checked value).
func scanMessage(raw json.RawMessage) (claudeMessage, bool) {
	var m claudeMessage
	if len(raw) == 0 || raw[0] != '{' {
		return m, false
	}
	return m, members(raw, func(key []byte, esc bool, v []byte) {
		switch {
		case keyIs(key, esc, "id"):
			m.ID = value(v)
		case keyIs(key, esc, "model"):
			m.Model = value(v)
		case keyIs(key, esc, "usage"):
			m.Usage = value(v)
		case keyIs(key, esc, "content"):
			m.Content = v
		}
	})
}

// countToolUses counts the content's objects whose type is "tool_use" (raw is a checked value).
func countToolUses(raw json.RawMessage) int {
	if len(raw) == 0 || raw[0] != '[' || !bytes.Contains(raw, []byte("tool_use")) {
		return 0
	}
	n := 0
	i := skipWS(raw, 1)
	for i < len(raw) && raw[i] != ']' {
		end, ok := skipValue(raw, i)
		if !ok {
			return n
		}
		if raw[i] == '{' {
			var typ any
			members(raw[i:end], func(key []byte, esc bool, v []byte) {
				if keyIs(key, esc, "type") {
					typ = value(v)
				}
			})
			if typ == "tool_use" {
				n++
			}
		}
		i = skipWS(raw, end)
		if i < len(raw) && raw[i] == ',' {
			i = skipWS(raw, i+1)
		}
	}
	return n
}

func scanCodexLine(line []byte, l *codexLineJSON) bool {
	return lineMembers(line, func(key []byte, esc bool, raw []byte) {
		switch {
		case keyIs(key, esc, "timestamp"):
			l.Timestamp = value(raw)
		case keyIs(key, esc, "type"):
			l.Type = value(raw)
		case keyIs(key, esc, "payload"):
			l.Payload = raw
		}
	})
}

func scanPayload(raw json.RawMessage, p *codexPayload) bool {
	return members(raw, func(key []byte, esc bool, v []byte) {
		switch {
		case keyIs(key, esc, "id"):
			p.ID = value(v)
		case keyIs(key, esc, "timestamp"):
			p.Timestamp = value(v)
		case keyIs(key, esc, "cwd"):
			p.Cwd = value(v)
		case keyIs(key, esc, "type"):
			p.Type = value(v)
		case keyIs(key, esc, "message"):
			p.Message = value(v)
		case keyIs(key, esc, "model"):
			p.Model = value(v)
		case keyIs(key, esc, "effort"):
			p.Effort = value(v)
		case keyIs(key, esc, "info"):
			p.Info = value(v)
		case keyIs(key, esc, "rate_limits"):
			p.RateLimits = value(v)
		}
	})
}

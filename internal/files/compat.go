// Package files answers the runtime's read-only file routes: a file of a tab's project for the editor, both sides of a
// changed file, a picture or document the agent produced, the «@»-context picker, a memory note the agent cited and an
// uploaded attachment. The answers keep the shape, the texts and the edge behaviour of the routes they replace, so the
// helpers here reproduce the few places where that behaviour comes from the old runtime's platform: how a query string
// and invalid UTF-8 are decoded, how JSON is written and an ETag computed, how names are sorted and paths split.
package files

import (
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// DecodeUTF8 turns bytes into text the way a WHATWG decoder does: each maximal invalid subsequence becomes one U+FFFD.
func DecodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b) + 8)
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			sb.WriteByte(c)
			i++
			continue
		}
		need, lo, hi := 0, byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lo = 0xA0
			} else if c == 0xED {
				hi = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lo = 0x90
			} else if c == 0xF4 {
				hi = 0x8F
			}
		default:
			sb.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j := i + 1
		ok := true
		for k := 0; k < need; k++ {
			if j >= len(b) {
				ok = false
				break
			}
			x := b[j]
			l, h := byte(0x80), byte(0xBF)
			if k == 0 {
				l, h = lo, hi
			}
			if x < l || x > h {
				ok = false
				break
			}
			j++
		}
		if !ok {
			sb.WriteRune(utf8.RuneError)
			i = j
			continue
		}
		sb.Write(b[i:j])
		i = j
	}
	return sb.String()
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// percentDecode decodes every valid %XX (and '+' as a space when plus is set), keeps a stray '%', then decodes the
// bytes as UTF-8 with replacement.
func percentDecode(s string, plus bool) string {
	if !strings.ContainsAny(s, "%+") {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '+' && plus {
			out = append(out, ' ')
			continue
		}
		if c == '%' && i+2 < len(s) {
			if h, l := unhex(s[i+1]), unhex(s[i+2]); h >= 0 && l >= 0 {
				out = append(out, byte(h<<4|l))
				i += 2
				continue
			}
		}
		out = append(out, c)
	}
	return DecodeUTF8(out)
}

// DecodeComponent is a path segment decoded as the old router did: percent-decoded when that is valid, as it was
// otherwise.
func DecodeComponent(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) {
				return s
			}
			h, l := unhex(s[i+1]), unhex(s[i+2])
			if h < 0 || l < 0 {
				return s
			}
			out = append(out, byte(h<<4|l))
			i += 2
			continue
		}
		out = append(out, s[i])
	}
	if !utf8.Valid(out) {
		return s
	}
	return string(out)
}

// Query is a parsed query string: every value of each key, in order.
type Query map[string][]string

// ParseQuery reads a raw query string the way the old runtime did: pairs split on '&' and the first '=', '+' is a
// space, valid escapes decoded, invalid ones kept, at most 1000 pairs.
func ParseQuery(raw string) Query {
	q := Query{}
	if raw == "" {
		return q
	}
	n := 0
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		if n >= 1000 {
			break
		}
		n++
		k, v, _ := strings.Cut(pair, "=")
		key := percentDecode(k, true)
		q[key] = append(q[key], percentDecode(v, true))
	}
	return q
}

// String is the value of a key given exactly once; a missing or repeated key is not a string.
func (q Query) String(key string) (string, bool) {
	if v := q[key]; len(v) == 1 {
		return v[0], true
	}
	return "", false
}

// requestTarget is the path and query as sent; a target in absolute form reaches the old runtime as its path.
func requestTarget(r *http.Request) string {
	if t := r.RequestURI; strings.HasPrefix(t, "/") {
		return t
	}
	return r.URL.RequestURI()
}

// RequestQuery is the request's query: what follows the first '?' of the target as it was sent.
func RequestQuery(r *http.Request) Query {
	target := requestTarget(r)
	target, _, _ = strings.Cut(target, "#")
	_, raw, _ := strings.Cut(target, "?")
	return ParseQuery(raw)
}

// APIParts is the request path as the old API router split it: "/agents" taken off, empty segments dropped, each
// segment percent-decoded on its own (so an encoded "/" stays inside its segment). ok is false for a path outside
// "/api".
func APIParts(r *http.Request) ([]string, string, bool) {
	target := requestTarget(r)
	p := target
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	const base = "/agents"
	if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && p != base && !strings.HasPrefix(p, base+"/") {
		p = base + p
	}
	p = p[min(len(base), len(p)):]
	if p == "" {
		p = "/"
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if p != "/api" && !strings.HasPrefix(p, "/api/") {
		return nil, "", false
	}
	var parts []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			parts = append(parts, DecodeComponent(s))
		}
	}
	return parts, p, true
}

// RouteIs: the request path is this fixed API route, as the old router matched it.
func RouteIs(r *http.Request, route string) bool {
	parts, _, ok := APIParts(r)
	if !ok {
		return false
	}
	want := strings.Split(strings.Trim(route, "/"), "/")
	if len(parts) != len(want) {
		return false
	}
	for i := range want {
		if parts[i] != want[i] {
			return false
		}
	}
	return true
}

// JSON values written the way the old runtime wrote them: object keys in the order given, nothing escaped that it
// did not escape.

// Field is one key of an Object.
type Field struct {
	K string
	V any
}

// Object is a JSON object with its keys in order.
type Object []Field

// MarshalJSON lets an Object be handed to encoding/json too.
func (o Object) MarshalJSON() ([]byte, error) { return AppendJSON(nil, o), nil }

// AppendJSON writes v (string, bool, int kinds, float64, []string, []any, []Object, Object, nil).
func AppendJSON(b []byte, v any) []byte {
	switch x := v.(type) {
	case nil:
		return append(b, "null"...)
	case string:
		return appendJSONString(b, x)
	case bool:
		return strconv.AppendBool(b, x)
	case int:
		return strconv.AppendInt(b, int64(x), 10)
	case int64:
		return strconv.AppendInt(b, x, 10)
	case float64:
		return strconv.AppendFloat(b, x, 'g', -1, 64)
	case Object:
		b = append(b, '{')
		for i, f := range x {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, f.K)
			b = append(b, ':')
			b = AppendJSON(b, f.V)
		}
		return append(b, '}')
	case []string:
		b = append(b, '[')
		for i, s := range x {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, s)
		}
		return append(b, ']')
	case []Object:
		b = append(b, '[')
		for i, o := range x {
			if i > 0 {
				b = append(b, ',')
			}
			b = AppendJSON(b, o)
		}
		return append(b, ']')
	case []any:
		b = append(b, '[')
		for i, e := range x {
			if i > 0 {
				b = append(b, ',')
			}
			b = AppendJSON(b, e)
		}
		return append(b, ']')
	}
	panic("files: no JSON form for this value")
}

const hexDigits = "0123456789abcdef"

func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b = append(b, '\\', '"')
		case c == '\\':
			b = append(b, '\\', '\\')
		case c >= 0x20:
			b = append(b, c)
		case c == '\b':
			b = append(b, '\\', 'b')
		case c == '\f':
			b = append(b, '\\', 'f')
		case c == '\n':
			b = append(b, '\\', 'n')
		case c == '\r':
			b = append(b, '\\', 'r')
		case c == '\t':
			b = append(b, '\\', 't')
		default:
			b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
		}
	}
	return append(b, '"')
}

// ETag is the old runtime's entity tag of a body.
func ETag(body []byte) string {
	sum := sha1.Sum(body)
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:])[:27] + `"`
}

var noCache = regexp.MustCompile(`(?:^|,)\s*no-cache\s*(?:,|$)`)

// Fresh: the caller already holds this entity (If-None-Match), and did not ask to revalidate with no-cache.
func Fresh(r *http.Request, etag string) bool {
	match := strings.Join(r.Header.Values("If-None-Match"), ", ")
	if match == "" || noCache.MatchString(strings.Join(r.Header.Values("Cache-Control"), ", ")) {
		return false
	}
	if match == "*" {
		return true
	}
	for _, tag := range strings.Split(match, ",") {
		if strings.TrimPrefix(strings.TrimSpace(tag), "W/") == etag {
			return true
		}
	}
	return false
}

// Answer is a whole JSON answer as the old runtime gave it: the value, its ETag, and 304 to a caller that holds it.
// Headers given are kept; a 304 keeps the JSON content type the old runtime had already set.
func Answer(r *http.Request, status int, v Object, h http.Header) (int, any, http.Header) {
	if h == nil {
		h = http.Header{}
	}
	etag := ETag(AppendJSON(nil, v))
	h.Set("ETag", etag)
	if Fresh(r, etag) {
		h.Set("Content-Type", "application/json; charset=utf-8")
		return http.StatusNotModified, nil, h
	}
	return status, v, h
}

// Error is {"error": text}.
func Error(text string) Object { return Object{{"error", text}} }

// ExtName is the extension of the last path segment, the way the old runtime cut it: from the last dot, "" for a
// name that only starts with a dot, trailing slashes ignored.
func ExtName(p string) string {
	startDot, startPart, end := -1, 0, -1
	matchedSlash := true
	preDotState := 0
	for i := len(p) - 1; i >= 0; i-- {
		c := p[i]
		if c == '/' {
			if !matchedSlash {
				startPart = i + 1
				break
			}
			continue
		}
		if end == -1 {
			matchedSlash = false
			end = i + 1
		}
		if c == '.' {
			if startDot == -1 {
				startDot = i
			} else if preDotState != 1 {
				preDotState = 1
			}
		} else if startDot != -1 {
			preDotState = -1
		}
	}
	if startDot == -1 || end == -1 || preDotState == 0 || (preDotState == 1 && startDot == end-1 && startDot == startPart+1) {
		return ""
	}
	return p[startDot:end]
}

// BaseName is the last segment of an absolute path, "" for "/".
func BaseName(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p[strings.LastIndexByte(p, '/')+1:]
}

// EncodeURIComponent percent-encodes all but the unreserved marks, as browsers' encodeURIComponent.
func EncodeURIComponent(s string) string {
	const keep = "-_.!~*'()"
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(keep, c) >= 0 {
			sb.WriteByte(c)
			continue
		}
		sb.WriteByte('%')
		sb.WriteByte("0123456789ABCDEF"[c>>4])
		sb.WriteByte("0123456789ABCDEF"[c&0xF])
	}
	return sb.String()
}

// utf16Units is the text as UTF-16 code units, the unit the old runtime's string lengths and offsets count.
func utf16Units(s string) []uint16 { return utf16.Encode([]rune(s)) }

// lowerText lower-cases the way the old runtime did, where it differs from Go's simple mapping: a capital I with a dot
// keeps its dot as a combining mark.
func lowerText(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "İ", "i̇"))
}

// isSpace is the old runtime's whitespace: \s, which trim also strips.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

func trimSpace(s string) string { return strings.TrimFunc(s, isSpace) }

// jsSpaceClass and jsDotClass are \s and . of the old runtime's patterns, for Go regular expressions.
const (
	jsSpaceClass = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
	jsDotClass   = `[^\n\r\x{2028}\x{2029}]`
)

// asciiFold turns the letters of a pattern into [xX] classes: case-insensitive for ASCII only, as the old patterns
// were (Go's (?i) would also fold the Kelvin sign and the long s); a bare '.' stops at line terminators as theirs did.
func asciiFold(pattern string) string {
	var sb strings.Builder
	inClass := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '\\' && i+1 < len(pattern):
			sb.WriteByte(c)
			i++
			sb.WriteByte(pattern[i])
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '.' && !inClass:
			sb.WriteString(jsDotClass)
			continue
		}
		if c >= 'a' && c <= 'z' {
			if inClass {
				// a-z inside a class gains A-Z.
				if i+2 < len(pattern) && pattern[i+1] == '-' && pattern[i+2] >= 'a' && pattern[i+2] <= 'z' {
					sb.WriteString(pattern[i:i+3] + string(c-32) + "-" + string(pattern[i+2]-32))
					i += 2
					continue
				}
				sb.WriteByte(c)
				sb.WriteByte(c - 32)
				continue
			}
			sb.WriteString("[" + string(c) + string(c-32) + "]")
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

// sortStable sorts in the old runtime's locale order (en-US) where less compares text, stably: equal items keep
// their order.
func sortStable[T any](items []T, less func(c *collate.Collator, a, b T) int) {
	c := collate.New(language.AmericanEnglish)
	sort.SliceStable(items, func(i, j int) bool { return less(c, items[i], items[j]) < 0 })
}

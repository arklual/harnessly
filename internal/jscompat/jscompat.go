// Package jscompat reproduces the few JavaScript behaviours the runtime's answers depend on — string trimming and
// length in UTF-16 units, parseInt, Date parsing and printing, truthiness of JSON values, Node's query strings and
// path.basename — so a route moved to Go answers byte for byte as the one it replaces.
package jscompat

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// IsSpace is JavaScript's WhiteSpace or LineTerminator (what \s and String.prototype.trim treat as space).
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.TrimFunc(s, IsSpace) }

// SpaceClass is a regexp character class matching what JavaScript's \s matches.
const SpaceClass = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// Len is a string's length in UTF-16 code units, as JavaScript counts it.
func Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 && r <= utf8.MaxRune {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// Slice is s.slice(0, n) in UTF-16 units. A surrogate pair cut in half leaves U+FFFD, which is what a JSON reader makes
// of the lone surrogate JavaScript would print.
func Slice(s string, n int) string {
	if Len(s) <= n {
		return s
	}
	return string(utf16.Decode(utf16.Encode([]rune(s))[:n]))
}

// Truncate is the runtime's preview of a message: at most n units, an ellipsis after a cut.
func Truncate(s string, n int) string {
	if Len(s) <= n {
		return s
	}
	return Slice(s, n) + "…"
}

// Truthy is JavaScript truthiness of a value decoded from JSON.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// String is String(v) for a JSON scalar; objects and arrays print as JavaScript prints them only roughly.
func String(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return Number(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = String(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// Number prints a number as JavaScript does.
func Number(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e21 || abs < 1e-6 {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		mant, exp, _ := strings.Cut(s, "e")
		if exp[0] == '+' {
			return mant + "e+" + strings.TrimLeft(exp[1:], "0")
		}
		return mant + "e-" + strings.TrimLeft(exp[1:], "0")
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// ParseInt is parseInt(s, radix); radix 0 is parseInt without one (a 0x prefix reads hex). NaN when nothing parses.
func ParseInt(s string, radix int) float64 {
	s = strings.TrimLeftFunc(s, IsSpace)
	sign := 1.0
	if s != "" && (s[0] == '-' || s[0] == '+') {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	if (radix == 0 || radix == 16) && len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s, radix = s[2:], 16
	}
	if radix == 0 {
		radix = 10
	}
	n, digits := 0.0, 0
	if radix == 10 {
		// decimal digits convert exactly, as JavaScript reads them
		end := 0
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		if end == 0 {
			return math.NaN()
		}
		f, _ := strconv.ParseFloat(s[:end], 64)
		return sign * f
	}
	for _, c := range s {
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'z':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			d = int(c-'A') + 10
		default:
			d = 99
		}
		if d >= radix {
			break
		}
		n = n*float64(radix) + float64(d)
		digits++
	}
	if digits == 0 {
		return math.NaN()
	}
	return sign * n
}

// TimeMs is a file time as Node's fs.Stats gives it in *Ms: milliseconds with the fraction of the nanoseconds.
func TimeMs(t time.Time) float64 {
	return float64(t.Unix())*1000 + float64(t.Nanosecond())/1e6
}

// DateMs is the time of the Date Node makes of a file time: the milliseconds rounded (Math.round).
func DateMs(ms float64) float64 { return math.Floor(ms + 0.5) }

const maxTime = 8.64e15

// clip is TimeClip: NaN outside the range of a Date, the fraction dropped.
func clip(ms float64) float64 {
	if math.IsNaN(ms) || math.Abs(ms) > maxTime {
		return math.NaN()
	}
	return math.Trunc(ms) + 0 // +0 turns -0 into 0
}

// DateValue is new Date(v).getTime() for a value decoded from JSON: NaN for what is not a date.
func DateValue(v any) float64 {
	switch x := v.(type) {
	case float64:
		return clip(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case nil:
		return 0
	case string:
		return ParseDate(x)
	}
	return math.NaN()
}

// ParseDate is Date.parse for the forms the runtime meets: ISO 8601 dates and date-times (lower case "t"/"z", a space
// for "T", "+hhmm" offsets and day overflow tolerated as V8 tolerates them). A date alone is UTC, a date-time without
// an offset is local time. Anything else is NaN.
func ParseDate(s string) float64 {
	p := dateParser{s: Trim(s)}
	ms, ok := p.parse()
	if !ok {
		return math.NaN()
	}
	return clip(ms)
}

type dateParser struct {
	s string
	i int
}

func (p *dateParser) digits(min, max int) (int, bool) {
	start := p.i
	for p.i < len(p.s) && p.i-start < max && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	if p.i-start < min {
		return 0, false
	}
	n, _ := strconv.Atoi(p.s[start:p.i])
	return n, true
}

func (p *dateParser) eat(c byte) bool {
	if p.i < len(p.s) && p.s[p.i] == c {
		p.i++
		return true
	}
	return false
}

func (p *dateParser) parse() (float64, bool) {
	var year int
	var ok bool
	if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		neg := p.s[p.i] == '-'
		p.i++
		if year, ok = p.digits(6, 6); !ok {
			return 0, false
		}
		if neg {
			if year == 0 {
				return 0, false
			}
			year = -year
		}
	} else if year, ok = p.digits(4, 4); !ok {
		return 0, false
	}
	month, day := 1, 1
	if p.eat('-') {
		if month, ok = p.digits(2, 2); !ok || month < 1 || month > 12 {
			return 0, false
		}
		if p.eat('-') {
			if day, ok = p.digits(2, 2); !ok || day < 1 || day > 31 {
				return 0, false
			}
		}
	}
	if p.i == len(p.s) {
		return float64(time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC).UnixMilli()), true
	}
	if c := p.s[p.i]; c != 'T' && c != 't' && c != ' ' {
		return 0, false
	}
	p.i++
	hour, ok1 := p.digits(2, 2)
	ok2 := p.eat(':')
	minute, ok3 := p.digits(2, 2)
	if !ok1 || !ok2 || !ok3 || hour > 24 || minute > 59 {
		return 0, false
	}
	sec, ms := 0, 0.0
	if p.eat(':') {
		if sec, ok = p.digits(2, 2); !ok || sec > 59 {
			return 0, false
		}
		if p.eat('.') || p.eat(',') {
			start := p.i
			if _, ok = p.digits(1, 64); !ok {
				return 0, false
			}
			frac := p.s[start:p.i]
			if len(frac) > 3 {
				frac = frac[:3]
			}
			for len(frac) < 3 {
				frac += "0"
			}
			n, _ := strconv.Atoi(frac)
			ms = float64(n)
		}
	}
	if hour == 24 && (minute != 0 || sec != 0 || ms != 0) {
		return 0, false
	}
	loc := time.Local
	offset := 0
	if p.i < len(p.s) {
		switch c := p.s[p.i]; c {
		case 'Z', 'z':
			p.i++
			loc = time.UTC
		case '+', '-':
			p.i++
			oh, ok := p.digits(2, 2)
			if !ok {
				return 0, false
			}
			p.eat(':')
			om, ok := p.digits(2, 2)
			if !ok || oh > 23 || om > 59 {
				return 0, false
			}
			offset = oh*3600 + om*60
			if c == '-' {
				offset = -offset
			}
			loc = time.UTC
		default:
			return 0, false
		}
	}
	if p.i != len(p.s) {
		return 0, false
	}
	t := time.Date(year, time.Month(month), day, hour, minute, sec, 0, loc)
	return float64(t.UnixMilli()-int64(offset)*1000) + ms, true
}

// ISOString is Date.prototype.toISOString; ok is false where it throws (an invalid date).
func ISOString(ms float64) (string, bool) {
	ms = clip(ms)
	if math.IsNaN(ms) {
		return "", false
	}
	t := time.UnixMilli(int64(ms)).UTC()
	y := t.Year()
	var year string
	switch {
	case y >= 0 && y <= 9999:
		year = pad(y, 4)
	case y < 0:
		year = "-" + pad(-y, 6)
	default:
		year = "+" + pad(y, 6)
	}
	return year + t.Format("-01-02T15:04:05.000Z"), true
}

func pad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// Query is Node's querystring.parse of a raw query: pairs split on "&" only, "+" as space, bad escapes kept as they
// are, every value of a repeated key kept.
func Query(raw string) url.Values {
	out := url.Values{}
	if raw == "" {
		return out
	}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		k, v = unescape(k), unescape(v)
		out[k] = append(out[k], v)
	}
	return out
}

// unescape is querystring.unescape: decodeURIComponent, else byte by byte keeping what does not decode.
func unescape(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	if !strings.Contains(s, "%") {
		return s
	}
	if d, err := url.PathUnescape(s); err == nil && utf8.ValidString(d) {
		return d
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b = append(b, byte(n))
			i += 2
			continue
		}
		b = append(b, s[i])
	}
	return strings.ToValidUTF8(string(b), "�")
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// Param is req.query[name] the way a handler reading it "as string" sees it: absent, one value, or several (an array,
// which as a string is the values joined by commas).
type Param struct {
	Values []string
}

func (q Param) Present() bool { return len(q.Values) > 0 }
func (q Param) Array() bool   { return len(q.Values) > 1 }

// String is the parameter as String(value) would print it, "" when absent.
func (q Param) String() string { return strings.Join(q.Values, ",") }

// Truthy: a present value that is not "" (an array always is).
func (q Param) Truthy() bool { return len(q.Values) > 1 || len(q.Values) == 1 && q.Values[0] != "" }

// Basename is Node's path.basename(p, ext).
func Basename(p, ext string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	base := p[strings.LastIndexByte(p, '/')+1:]
	if ext != "" && base != ext && strings.HasSuffix(base, ext) {
		return base[:len(base)-len(ext)]
	}
	return base
}

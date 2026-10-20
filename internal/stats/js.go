package stats

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// nullNum is a number read back as JSON null: arithmetic takes it as 0 (null + 1 is 1 in JavaScript), while a NaN
// computed from a bad value stays NaN. Both are written as null.
var nullNum = math.Float64frombits(0x7ff8000000000bad)

func isNull(f float64) bool { return math.Float64bits(f) == 0x7ff8000000000bad }

// z is a number as an operand of + or a comparison: null counts as 0.
func z(f float64) float64 {
	if isNull(f) {
		return 0
	}
	return f
}

// add is a + b over numbers that may be null.
func add(a, b float64) float64 { return z(a) + z(b) }

// numOr is `v ?? 0` for a field the old handlers summed as it was.
func numOr(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case json.Number:
		return numberText(string(x))
	}
	return math.NaN()
}

// toNum is Number(v ?? 0) for a value decoded from a JSON line.
func toNum(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case json.Number:
		return numberText(string(x))
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		return jsNumber(x)
	case []any:
		return jsNumber(jsStr(x))
	}
	return math.NaN()
}

func numberText(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !math.IsInf(f, 0) && f != 0 {
		return math.NaN()
	}
	return f
}

var decimalLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// jsNumber is Number(s).
func jsNumber(s string) float64 {
	s = jscompat.Trim(s)
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok || strings.ContainsAny(s[2:], "_+-") {
				return math.NaN()
			}
			f, _ := new(big.Float).SetInt(n).Float64()
			return f
		}
	}
	switch s {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if !decimalLiteral.MatchString(s) {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && f == 0 && !strings.ContainsAny(s, "eE") {
		return math.NaN()
	}
	return f
}

// jsStr is String(v ?? ”) for a value decoded from a JSON line.
func jsStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return jscompat.Number(x)
	case json.Number:
		return jscompat.Number(numberText(string(x)))
	case bool:
		return strconv.FormatBool(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = jsStr(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// truthy is JavaScript truthiness of a decoded JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case json.Number:
		f := numberText(string(x))
		return f != 0 && !math.IsNaN(f)
	case string:
		return x != ""
	}
	return true
}

// rawTruthy is the truthiness of a raw JSON value.
func rawTruthy(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case 'n', 'f':
		return false
	case '"':
		return len(raw) > 2
	case '{', '[', 't':
		return true
	}
	f := numberText(string(raw))
	return f != 0 && !math.IsNaN(f)
}

// decodeLine is JSON.parse of a line into the fields a parser reads; false where JSON.parse throws or the value is not an
// object (every parser here skipped those). Numbers too large for a float64 decode as JSON.parse reads them.
func decodeLine(line []byte, into any) bool {
	if json.Unmarshal(line, into) == nil {
		return true
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	if dec.Decode(into) != nil {
		return false
	}
	_, err := dec.Token()
	return err == io.EOF
}

// lessJS compares strings as JavaScript's < does: by UTF-16 code units.
func lessJS(a, b string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < 0x80 && b[i] < 0x80 {
				return a[i] < b[i]
			}
			return compareUTF16(a[i:], b[i:]) < 0
		}
	}
	return len(a) < len(b)
}

func compareUTF16(a, b string) int {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	return len(ua) - len(ub)
}

// slice16 is s.slice(from, to) in UTF-16 units, for the short prefixes the parsers cut (a date, an hour).
func slice16(s string, from, to int) string {
	ascii := true
	for i := 0; i < len(s) && i < to; i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		if from > len(s) {
			from = len(s)
		}
		if to > len(s) {
			to = len(s)
		}
		if from >= to {
			return ""
		}
		return s[from:to]
	}
	u := utf16.Encode([]rune(s))
	if from > len(u) {
		from = len(u)
	}
	if to > len(u) {
		to = len(u)
	}
	if from >= to {
		return ""
	}
	return string(utf16.Decode(u[from:to]))
}

// jsRound is Math.round.
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	f := math.Floor(x)
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}

// toInt32 is ToInt32 (x | 0).
func toInt32(x float64) int64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	t := math.Trunc(x)
	m := math.Mod(t, 4294967296)
	if m < 0 {
		m += 4294967296
	}
	u := uint32(m)
	return int64(int32(u))
}

// omap is a JavaScript object of values of one type: keys enumerate as JavaScript enumerates them, array indices
// ascending first, then the others in insertion order.
type omap[V any] struct {
	keys []string
	m    map[string]V
}

func newOmap[V any]() *omap[V] { return &omap[V]{m: map[string]V{}} }

func (o *omap[V]) get(k string) (V, bool) {
	v, ok := o.m[k]
	return v, ok
}

func (o *omap[V]) set(k string, v V) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *omap[V]) len() int { return len(o.keys) }

func (o *omap[V]) ordered() []string {
	var idx, rest []string
	for _, k := range o.keys {
		if isIndex(k) {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(idx) == 0 {
		return o.keys
	}
	sort.Slice(idx, func(i, j int) bool {
		a, _ := strconv.ParseUint(idx[i], 10, 64)
		b, _ := strconv.ParseUint(idx[j], 10, 64)
		return a < b
	})
	return append(idx, rest...)
}

func isIndex(k string) bool {
	if k == "" || len(k) > 10 || (k[0] == '0' && k != "0") {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, _ := strconv.ParseUint(k, 10, 64)
	return n < 4294967295
}

// counts is an object of numbers, written as JavaScript wrote it.
func countsValue(o *omap[float64]) *jsvalue.Object {
	out := jsvalue.NewObject()
	for _, k := range o.ordered() {
		out.Set(k, o.m[k])
	}
	return out
}

// lineReader yields the lines of a stream as Node's readline does: split at \n, \r\n or a lone \r, the last line
// without its end included, bytes decoded as UTF-8 with bad sequences replaced.
type lineReader struct {
	r          io.Reader
	buf        []byte
	start, end int
	eof        bool
	readErr    error // a failure other than the end
	skipLF     bool  // the previous line ended in \r: a \n right after it belongs to it
}

func newLineReader(r io.Reader) *lineReader { return newLineReaderSize(r, 256<<10) }

func newLineReaderSize(r io.Reader, size int) *lineReader {
	return &lineReader{r: r, buf: make([]byte, size)}
}

func (l *lineReader) fill() {
	if l.start > 0 {
		n := copy(l.buf, l.buf[l.start:l.end])
		l.start, l.end = 0, n
	}
	if l.end == len(l.buf) {
		grown := make([]byte, 2*len(l.buf))
		copy(grown, l.buf[:l.end])
		l.buf = grown
	}
	n, err := l.r.Read(l.buf[l.end:])
	l.end += n
	if err != nil {
		l.eof = true
		if err != io.EOF {
			l.readErr = err
		}
	}
}

// next is the next line (valid until the next call); false at the end.
func (l *lineReader) next() ([]byte, bool) {
	for {
		if l.skipLF {
			if l.start < l.end {
				if l.buf[l.start] == '\n' {
					l.start++
				}
				l.skipLF = false
			} else if !l.eof {
				l.fill()
				continue
			} else {
				l.skipLF = false
			}
		}
		b := l.buf[l.start:l.end]
		i := bytes.IndexByte(b, '\n')
		lim := b
		if i >= 0 {
			lim = b[:i]
		}
		if j := bytes.IndexByte(lim, '\r'); j >= 0 {
			l.start += j + 1
			l.skipLF = true
			return decodeText(b[:j]), true
		}
		if i >= 0 {
			l.start += i + 1
			return decodeText(b[:i]), true
		}
		if l.eof {
			if l.start < l.end {
				l.start = l.end
				return decodeText(b), true
			}
			return nil, false
		}
		l.fill()
	}
}

func decodeText(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	return []byte(jscompat.DecodeUTF8(b))
}

// splitLines is readline over a whole text.
func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			out = append(out, data[start:i])
			start = i + 1
		case '\r':
			out = append(out, data[start:i])
			if i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// newCollator orders strings as localeCompare did (ICU, en-US). Not safe for concurrent use.
func newCollator() *collate.Collator { return collate.New(language.AmericanEnglish) }

// localDay is a time's calendar day in the local zone, as YYYY-MM-DD (dayjs format).
func localDay(t time.Time) string {
	y, m, d := t.Date()
	return pad4(y) + "-" + pad2(int(m)) + "-" + pad2(d)
}

func pad2(n int) string {
	if n < 10 && n >= 0 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func pad4(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for len(s) < 4 {
		s = "0" + s
	}
	if neg {
		return "-" + s
	}
	return s
}

// msTime is the local time of a Date's value.
func msTime(ms float64) time.Time { return time.UnixMilli(int64(ms)).In(time.Local) }

// startOfDay is dayjs(t).startOf('day').
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// addDays is dayjs(t).add(n, 'day'): the same wall time n calendar days on.
func addDays(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, m, d+n, h, mi, s, t.Nanosecond(), time.Local)
}

var dayjsPattern = regexp.MustCompile(`^(\d{4})[-/]?(\d{1,2})?[-/]?(\d{0,2})[Tt\s]*(\d{1,2})?:?(\d{1,2})?:?(\d{1,2})?[.:]?(\d+)?$`)

// dayjsParse is dayjs(s).valueOf(): its own pattern read as local time, anything else as new Date(s). NaN for an
// invalid date.
func dayjsParse(s string) float64 {
	if !strings.HasSuffix(s, "Z") && !strings.HasSuffix(s, "z") {
		if m := dayjsPattern.FindStringSubmatch(s); m != nil {
			num := func(i int, def int) int {
				if m[i] == "" {
					return def
				}
				n, _ := strconv.Atoi(m[i])
				return n
			}
			year := num(1, 0)
			month := 0
			if m[2] != "" {
				month = num(2, 1) - 1
			}
			day := num(3, 1)
			ms := m[7]
			if len(ms) > 3 {
				ms = ms[:3]
			}
			msN := 0
			if ms != "" {
				msN, _ = strconv.Atoi(ms)
			}
			if year >= 0 && year <= 99 {
				year += 1900
			}
			t := time.Date(year, time.Month(month+1), day, num(4, 0), num(5, 0), num(6, 0), msN*1e6, time.Local)
			return float64(t.UnixMilli())
		}
	}
	return jscompat.ParseDate(s)
}

package transcript

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// JavaScript semantics the Codex parser leans on: decoding bytes as Buffer.toString does, property reads that tell
// undefined from null, String() and Number() of JSON values, and the throws they make.

// codexUndef is undefined: a property that is not there.
type codexUndef struct{}

var codexUndefined any = codexUndef{}

// codexThrow is a TypeError the previous runtime threw (String() of an object whose own toString is not a function); it
// unwinds to where that runtime caught it.
type codexThrow struct{}

// decodeUTF8 is Buffer.toString('utf-8'): every maximal invalid subpart becomes one U+FFFD (the WHATWG decoder), not
// every invalid run as strings.ToValidUTF8 does.
func decodeUTF8(b []byte) string {
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
		need, lower, upper := 0, byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lower = 0xA0
			} else if c == 0xED {
				upper = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lower = 0x90
			} else if c == 0xF4 {
				upper = 0x8F
			}
		default:
			sb.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j := i + 1
		ok := true
		for k := 0; k < need; k++ {
			if j >= len(b) || b[j] < lower || b[j] > upper {
				ok = false
				break
			}
			lower, upper = 0x80, 0xBF
			j++
		}
		if ok {
			sb.Write(b[i:j])
		} else {
			sb.WriteRune(utf8.RuneError)
		}
		i = j
	}
	return sb.String()
}

// prop is v[k] for a JSON value: undefined unless v is an object holding k.
func prop(v any, k string) any {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return codexUndefined
	}
	if x, has := o.Get(k); has {
		return x
	}
	return codexUndefined
}

func codexNullish(v any) bool { return v == nil || v == codexUndefined }

// codexCoalesce is a ?? b.
func codexCoalesce(a, b any) any {
	if codexNullish(a) {
		return b
	}
	return a
}

func codexTruthy(v any) bool { return v != codexUndefined && jscompat.Truthy(v) }

func isObjectLike(v any) bool {
	switch v.(type) {
	case *jsvalue.Object, []any:
		return true
	}
	return false
}

func safeString(v any) string {
	s, _ := v.(string)
	return s
}

func safeNumber(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && !math.IsInf(f, 0) && !math.IsNaN(f)
}

// optString is the value when it is a string, else undefined.
func optString(v any) any {
	if s, ok := v.(string); ok {
		return s
	}
	return jsvalue.Undefined
}

// orUndefined is `s || undefined`.
func orUndefined(s string) any {
	if s == "" {
		return jsvalue.Undefined
	}
	return s
}

// jsToString is String(v); false where JavaScript throws (an object whose own toString or valueOf leaves no
// primitive, which for JSON data is one with its own "toString" key).
func jsToString(v any) (string, bool) {
	switch x := v.(type) {
	case codexUndef:
		return "undefined", true
	case nil:
		return "null", true
	case bool, float64, string:
		return jscompat.String(x), true
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if codexNullish(e) {
				continue
			}
			s, ok := jsToString(e)
			if !ok {
				return "", false
			}
			parts[i] = s
		}
		return strings.Join(parts, ","), true
	case *jsvalue.Object:
		if x.Has("toString") {
			return "", false
		}
		return "[object Object]", true
	}
	return "", false
}

// mustString is String(v), throwing where JavaScript throws.
func mustString(v any) string {
	s, ok := jsToString(v)
	if !ok {
		panic(codexThrow{})
	}
	return s
}

// joinSpace is array.join(' ').
func joinSpace(a []any) string {
	parts := make([]string, len(a))
	for i, e := range a {
		if !codexNullish(e) {
			parts[i] = mustString(e)
		}
	}
	return strings.Join(parts, " ")
}

var (
	codexDecimalLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
	radixLiteral        = regexp.MustCompile(`^0(?:[xX][0-9a-fA-F]+|[oO][0-7]+|[bB][01]+)$`)
)

// stringToNumber is Number(s).
func stringToNumber(s string) float64 {
	s = jscompat.Trim(s)
	switch s {
	case "":
		return 0
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if codexDecimalLiteral.MatchString(s) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return math.NaN()
		}
		return f
	}
	if radixLiteral.MatchString(s) {
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[s[1]]
		n, ok := new(big.Int).SetString(s[2:], base)
		if !ok {
			return math.NaN()
		}
		f, _ := new(big.Float).SetInt(n).Float64()
		return f
	}
	return math.NaN()
}

// jsToNumber is Number(v) for a JSON value; false where JavaScript throws.
func jsToNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case codexUndef:
		return math.NaN(), true
	case nil:
		return 0, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case float64:
		return x, true
	case string:
		return stringToNumber(x), true
	}
	s, ok := jsToString(v)
	if !ok {
		return 0, false
	}
	return stringToNumber(s), true
}

// jsMax is Math.max(a, b) (NaN wins, +0 above -0).
func jsMax(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	if a == b {
		if math.Signbit(a) {
			return b
		}
		return a
	}
	return math.Max(a, b)
}

// jsRound is Math.round.
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	r := math.Floor(x)
	if x-r >= 0.5 {
		r++
	}
	return r
}

// oneLineSummary is the text on one line, at most limit UTF-16 units with an ellipsis after a cut.
func oneLineSummary(text string, limit int) string {
	var b strings.Builder
	space := false
	units := 0
	for _, r := range text {
		if jscompat.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
			units++
		}
		space = false
		b.WriteRune(r)
		if units++; r >= 0x10000 {
			units++
		}
		if units > limit {
			break // the rest cannot change what is kept
		}
	}
	compact := b.String()
	if jscompat.Len(compact) <= limit {
		return compact
	}
	return jscompat.Slice(compact, limit-1) + "…"
}

// firstLine is s.split('\n')[0].
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// parseJSON is JSON.parse; false where it throws.
func parseJSON(s string) (any, bool) { return parseJSONFast(s) }

// stringify is JSON.stringify of a JSON value.
func stringify(v any) string { return string(jsvalue.Stringify(v, "")) }

package transcript

import (
	"fmt"
	"math"
	"math/big"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

// The JavaScript semantics the Claude parser leans on. Values are what jsvalue.Parse makes, plus jsvalue.Undefined.

var undef any = jsvalue.Undefined

// claudeThrow is a TypeError the previous runtime would have thrown; the readers catch it where it caught one.
type claudeThrow struct{ msg string }

func claudeFail(format string, args ...any) { panic(claudeThrow{fmt.Sprintf(format, args...)}) }

// claudeCatch runs f and says whether it threw as JavaScript would have.
func claudeCatch(f func()) (threw bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(claudeThrow); !ok {
				panic(r)
			}
			threw = true
		}
	}()
	f()
	return false
}

func nullish(v any) bool { return v == nil || v == undef }

// get is v[k] for a property name that no prototype defines: null and undefined throw.
func get(v any, k string) any {
	switch x := v.(type) {
	case nil:
		claudeFail("Cannot read properties of null (reading '%s')", k)
	case *jsvalue.Object:
		if val, ok := x.Get(k); ok {
			return val
		}
		return undef
	case string:
		if k == "length" {
			return float64(jscompat.Len(x))
		}
	case []any:
		if k == "length" {
			return float64(len(x))
		}
	}
	if v == undef {
		claudeFail("Cannot read properties of undefined (reading '%s')", k)
	}
	return undef
}

// opt is v?.[k].
func opt(v any, k string) any {
	if nullish(v) {
		return undef
	}
	return get(v, k)
}

func coalesce(v, def any) any {
	if nullish(v) {
		return def
	}
	return v
}

func isStr(v any, s string) bool {
	x, ok := v.(string)
	return ok && x == s
}

func truthy(v any) bool { return v != undef && jsvalue.Truthy(v) }

// jsString is String(v).
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return jscompat.Number(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if !nullish(e) {
				parts[i] = jsString(e)
			}
		}
		return strings.Join(parts, ",")
	case *jsvalue.Object:
		return "[object Object]"
	}
	return "undefined"
}

// keys is Object.keys(v).
func keys(v any) []string {
	switch x := v.(type) {
	case *jsvalue.Object:
		return x.Keys()
	case []any:
		out := make([]string, len(x))
		for i := range x {
			out[i] = strconv.Itoa(i)
		}
		return out
	case string:
		out := make([]string, jscompat.Len(x))
		for i := range out {
			out[i] = strconv.Itoa(i)
		}
		return out
	}
	if nullish(v) {
		claudeFail("Cannot convert undefined or null to object")
	}
	return nil
}

// index is v[k] for a key Object.keys gave.
func index(v any, k string) any {
	switch x := v.(type) {
	case *jsvalue.Object:
		return get(x, k)
	case []any:
		i, _ := strconv.Atoi(k)
		return x[i]
	case string:
		i, _ := strconv.Atoi(k)
		return string(utf16.Decode(utf16.Encode([]rune(x))[i : i+1]))
	}
	return undef
}

func sliceJS(s string, n int) string { return jscompat.Slice(s, n) }

func trimStart(s string) string { return strings.TrimLeftFunc(s, jscompat.IsSpace) }
func trimEnd(s string) string   { return strings.TrimRightFunc(s, jscompat.IsSpace) }

// claudeDecode is Buffer.toString('utf-8'): every maximal ill-formed subsequence becomes one U+FFFD (WHATWG).
func claudeDecode(b []byte) string {
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

// posixJoin is path.posix.join(dir, p) for two non-empty parts.
func posixJoin(dir, p string) string {
	joined := dir + "/" + p
	out := path.Clean(joined)
	if strings.HasSuffix(joined, "/") && out != "/" {
		out += "/"
	}
	return out
}

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

var decimalLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// claudeDiffCount is the number of lines diffLines (npm diff) reports added and removed: its Myers search is minimal,
// so both follow from the length of the longest common run of line tokens.
func claudeDiffCount(oldStr, newStr string) (added, removed int) {
	a, b := claudeLineTokens(oldStr), claudeLineTokens(newStr)
	d := claudeEditDistance(a, b)
	common := (len(a) + len(b) - d) / 2
	return len(b) - common, len(a) - common
}

var claudeLineSplit = regexp.MustCompile(`\n|\r\n`)

func claudeLineTokens(s string) []string {
	parts := []string{}
	last := 0
	for _, loc := range claudeLineSplit.FindAllStringIndex(s, -1) {
		parts = append(parts, s[last:loc[0]], s[loc[0]:loc[1]])
		last = loc[1]
	}
	parts = append(parts, s[last:])
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	var lines []string
	for i, p := range parts {
		if i%2 == 1 {
			lines[len(lines)-1] += p
		} else {
			lines = append(lines, p)
		}
	}
	out := lines[:0]
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func claudeEditDistance(a, b []string) int {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return n + m
	}
	ids := map[string]int{}
	id := func(s string) int {
		if v, ok := ids[s]; ok {
			return v
		}
		ids[s] = len(ids)
		return ids[s]
	}
	x, y := make([]int, n), make([]int, m)
	for i, s := range a {
		x[i] = id(s)
	}
	for i, s := range b {
		y[i] = id(s)
	}
	limit := n + m
	v := make([]int, 2*limit+2)
	for d := 0; d <= limit; d++ {
		for k := -d; k <= d; k += 2 {
			var i int
			if k == -d || (k != d && v[limit+k-1] < v[limit+k+1]) {
				i = v[limit+k+1]
			} else {
				i = v[limit+k-1] + 1
			}
			j := i - k
			for i < n && j < m && x[i] == y[j] {
				i++
				j++
			}
			v[limit+k] = i
			if i >= n && j >= m {
				return d
			}
		}
	}
	return limit
}

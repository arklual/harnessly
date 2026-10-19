package gitread

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The answers must match what the previous runtime computed with JavaScript's string functions, whose idea of
// whitespace, number parsing and string length differs from Go's in the corners. These helpers are those functions.

// jsSpace is JavaScript's whitespace and line terminators (String.prototype.trim, \s).
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

// jsFields is s.trim().split(/\s+/) without the single empty field an empty string gives.
func jsFields(s string) []string { return strings.FieldsFunc(s, jsSpace) }

// jsParseInt is parseInt(s, 10): leading whitespace, a sign and the longest run of digits; ok is false for NaN.
func jsParseInt(s string) (float64, bool) {
	s = strings.TrimLeftFunc(s, jsSpace)
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n == 0 {
		return math.NaN(), false
	}
	v, _ := strconv.ParseFloat(s[:n], 64)
	if neg {
		v = -v
	}
	return v, true
}

// jsIntOr is parseInt(s, 10) || fallback, as an int.
func jsIntOr(s string, fallback int) int {
	v, ok := jsParseInt(s)
	if !ok || v == 0 {
		return fallback
	}
	return int(v)
}

// jsMillis is parseInt(s, 10) * 1000; nil stands for NaN, which JSON.stringify writes as null.
func jsMillis(s string) *float64 {
	v, ok := jsParseInt(s)
	if !ok {
		return nil
	}
	v *= 1000
	return &v
}

// jsNumber is String(n) for a non-negative integer-valued number.
func jsNumber(v float64) string {
	if v < 1e21 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'e', -1, 64)
}

// jsPrefix is s.slice(0, n): the first n UTF-16 code units.
func jsPrefix(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if units+w > n {
			if units < n { // the cut splits a surrogate pair; JavaScript keeps the lone half
				return s[:i] + string(utf8.RuneError)
			}
			return s[:i]
		}
		units += w
	}
	return s
}

// jsLength is s.length: UTF-16 code units.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// jsLastIndexOf is s.lastIndexOf(sub, from) in UTF-16 code units, -1 when absent.
func jsLastIndexOf(s, sub string, from int) int {
	u := utf16.Encode([]rune(s))
	w := utf16.Encode([]rune(sub))
	start := min(from, len(u)-len(w))
	for i := start; i >= 0; i-- {
		match := true
		for j := range w {
			if u[i+j] != w[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// decodeUTF8 turns process output into a string as Node did: each maximal invalid subsequence becomes one U+FFFD
// (the WHATWG decoder), where Go's own conversion would give one per byte.
func decodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b) + 16)
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			sb.WriteByte(c)
			i++
			continue
		}
		need, lower, upper := 0, byte(0x80), byte(0xbf)
		switch {
		case c >= 0xc2 && c <= 0xdf:
			need = 1
		case c >= 0xe0 && c <= 0xef:
			need = 2
			if c == 0xe0 {
				lower = 0xa0
			} else if c == 0xed {
				upper = 0x9f
			}
		case c >= 0xf0 && c <= 0xf4:
			need = 3
			if c == 0xf0 {
				lower = 0x90
			} else if c == 0xf4 {
				upper = 0x8f
			}
		default:
			sb.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j, ok := i+1, true
		for k := 0; k < need; k++ {
			lo, hi := byte(0x80), byte(0xbf)
			if k == 0 {
				lo, hi = lower, upper
			}
			if j >= len(b) || b[j] < lo || b[j] > hi {
				ok = false
				break
			}
			j++
		}
		if ok {
			sb.Write(b[i:j])
		} else {
			sb.WriteRune(utf8.RuneError) // the offending byte, not consumed, is read again
		}
		i = j
	}
	return sb.String()
}

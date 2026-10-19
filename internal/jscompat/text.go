package jscompat

import (
	"strings"
	"unicode/utf16"
)

// LastIndexOf is s.lastIndexOf(sub, from) in UTF-16 code units, -1 when absent.
func LastIndexOf(s, sub string, from int) int {
	if !strings.Contains(s, sub) {
		return -1
	}
	u := utf16.Encode([]rune(s))
	w := utf16.Encode([]rune(sub))
	start := min(from, len(u)-len(w))
	if start < 0 {
		start = 0
	}
	for i := start; i >= 0 && i+len(w) <= len(u); i-- {
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

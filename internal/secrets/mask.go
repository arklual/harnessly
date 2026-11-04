package secrets

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

// Mask replaces known literal values and common wire encodings in one longest-match pass.
// A replacement is never matched again, and a short prefix cannot expose a longer value's suffix.
func Mask(text string, values []string) string {
	unique := map[string]bool{}
	for _, value := range values {
		if value == "" {
			continue
		}
		encoded, _ := json.Marshal(value)
		for _, pattern := range []string{value, url.QueryEscape(value), url.PathEscape(value), string(encoded[1 : len(encoded)-1])} {
			unique[pattern] = true
		}
	}
	if len(unique) == 0 {
		return text
	}
	patterns := make([]string, 0, len(unique))
	for pattern := range unique {
		patterns = append(patterns, pattern)
	}
	sort.Slice(patterns, func(i, j int) bool {
		if len(patterns[i]) != len(patterns[j]) {
			return len(patterns[i]) > len(patterns[j])
		}
		return patterns[i] < patterns[j]
	})
	pairs := make([]string, 0, len(patterns)*2)
	for _, pattern := range patterns {
		pairs = append(pairs, pattern, "[redacted]")
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

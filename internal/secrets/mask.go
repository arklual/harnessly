package secrets

import (
	"encoding/base64"
	"encoding/json"
	"html"
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
		form := encodeSecret(value, true)
		// Chrome's URL-as-title fallback displays the unreserved tilde decoded, unlike the URL itself.
		patterns := []string{value, url.QueryEscape(value), url.PathEscape(value), form,
			strings.ReplaceAll(form, "%7E", "~"), encodeSecret(value, false), html.EscapeString(value)}
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding,
			base64.URLEncoding, base64.RawURLEncoding} {
			patterns = append(patterns, encoding.EncodeToString([]byte(value)))
		}
		for _, pattern := range patterns {
			unique[pattern] = true
			encoded, _ := json.Marshal(pattern)
			unique[string(encoded[1:len(encoded)-1])] = true
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

// encodeSecret follows WHATWG form serialization or JavaScript encodeURIComponent on UTF-8 bytes.
func encodeSecret(value string, form bool) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for n := 0; n < len(value); n++ {
		b := value[n]
		safe := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("*-._", rune(b))
		if !form {
			safe = safe || strings.ContainsRune("!~'()", rune(b))
		}
		switch {
		case safe:
			out.WriteByte(b)
		case form && b == ' ':
			out.WriteByte('+')
		default:
			out.WriteByte('%')
			out.WriteByte(hex[b>>4])
			out.WriteByte(hex[b&15])
		}
	}
	return out.String()
}

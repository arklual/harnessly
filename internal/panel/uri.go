package panel

import (
	"strings"
	"unicode/utf8"
)

// decodeURI is JavaScript's decodeURI: escapes become characters, except those of the reserved set and "#", which
// stay as typed; false for a broken escape or bytes that are not UTF-8.
func decodeURI(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '%' {
			b.WriteByte(s[i])
			i++
			continue
		}
		start := i
		var buf []byte
		for i < len(s) && s[i] == '%' {
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return "", false
			}
			buf = append(buf, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 3
			if utf8.FullRune(buf) {
				break
			}
		}
		r, size := utf8.DecodeRune(buf)
		if (r == utf8.RuneError && size <= 1) || size != len(buf) {
			return "", false
		}
		if r < 0x80 && strings.ContainsRune(";/?:@&=+$,#", r) {
			b.WriteString(s[start:i])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String(), true
}

// encodeURI is JavaScript's encodeURI: everything but letters, digits and -_.!~*'();/?:@&=+$,# is percent-encoded.
func encodeURI(s string) string {
	const keep = "-_.!~*'();/?:@&=+$,#"
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&15])
	}
	return b.String()
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func unhex(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

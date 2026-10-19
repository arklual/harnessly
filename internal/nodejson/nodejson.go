// Package nodejson writes JSON byte for byte as the previous runtime did (JavaScript's JSON.stringify) and gives an
// answer the ETag that runtime's dispatcher put on it, so a page's conditional request keeps working across the switch.
package nodejson

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// Marshal encodes v as JSON.stringify would: no HTML escaping, U+2028 and U+2029 left as they are. Struct field order is
// the key order, so a struct must declare its fields in the order the old handler built its object. Strings must be
// valid UTF-8 (JavaScript strings had already replaced invalid bytes when they were read).
func Marshal(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte("null")
	}
	return unescapeLineSeparators(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// unescapeLineSeparators turns the \u2028 and \u2029 escapes encoding/json always writes back into the characters. A
// backslash in encoder output always starts an escape, so escapes are skipped as pairs.
func unescapeLineSeparators(b []byte) []byte {
	if !bytes.Contains(b, []byte(`\u202`)) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' || i+1 >= len(b) {
			out = append(out, b[i])
			continue
		}
		if i+5 < len(b) && b[i+1] == 'u' && string(b[i+2:i+5]) == "202" && (b[i+5] == '8' || b[i+5] == '9') {
			if b[i+5] == '8' {
				out = append(out, "\u2028"...)
			} else {
				out = append(out, "\u2029"...)
			}
			i += 5
			continue
		}
		out = append(out, b[i], b[i+1])
		i++
	}
	return out
}

// ETag is the old dispatcher's tag of a payload: a quoted, unpadded base64url SHA-1.
func ETag(payload []byte) string {
	sum := sha1.Sum(payload)
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:])[:27] + `"`
}

var noCache = regexp.MustCompile(`(?:^|,)\s*no-cache\s*(?:,|$)`)

// Fresh says whether the request's If-None-Match already names the tag (the answer is then 304).
func Fresh(h http.Header, etag string) bool {
	match := strings.Join(h.Values("If-None-Match"), ", ")
	if match == "" || noCache.MatchString(strings.Join(h.Values("Cache-Control"), ", ")) {
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

// Answer is a JSON route's answer the way the old dispatcher sent it: the ETag of the exact bytes, and 304 when the
// caller already holds them. The value is handed on as raw JSON.
func Answer(r *http.Request, status int, v any, h http.Header) (int, any, http.Header) {
	raw := Marshal(v)
	tag := ETag(raw)
	if h == nil {
		h = http.Header{}
	}
	h.Set("ETag", tag)
	if Fresh(r.Header, tag) {
		return http.StatusNotModified, nil, h
	}
	return status, json.RawMessage(raw), h
}

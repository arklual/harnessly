package prompts

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"harnessly/internal/jscompat"
)

// Whitespace means what JavaScript's \s means, so text is judged as the previous runtime judged it.
const (
	sp  = jscompat.SpaceClass
	nsp = `[^\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
)

var spaceRun = regexp.MustCompile(sp + `+`)

// collapse folds whitespace runs into one space and trims the ends.
func collapse(s string) string { return jscompat.Trim(spaceRun.ReplaceAllString(s, " ")) }

// noSpace drops every whitespace character.
func noSpace(s string) string { return spaceRun.ReplaceAllString(s, "") }

// lastUnits is the last n UTF-16 units of s, as s.slice(-n) is.
func lastUnits(s string, n int) string {
	u := utf16.Encode([]rune(s))
	if len(u) <= n {
		return s
	}
	return string(utf16.Decode(u[len(u)-n:]))
}

// sha1Hex is the first n hex characters of the text's SHA-1.
func sha1Hex(text string, n int) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])[:n]
}

func appendJSONString(b []byte, s string) []byte {
	raw, _ := json.Marshal(s)
	return append(b, raw...)
}

func appendInt(b []byte, n int64) []byte { return strconv.AppendInt(b, n, 10) }

func blank(s string) bool { return strings.TrimFunc(s, jscompat.IsSpace) == "" }

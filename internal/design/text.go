package design

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"harnessly/internal/jscompat"
)

func validUTF8(s string) bool { return utf8.ValidString(s) }

var (
	spaceRun  = regexp.MustCompile(jscompat.SpaceClass + `+`)
	controls  = regexp.MustCompile("[\x00-\x08\x0b-\x1a\x1c-\x1f\x7f-\u009f]")
	spoofing  = regexp.MustCompile("[\u200b-\u200f\u202a-\u202e\u2060-\u2069\ufeff]")
	escapeKey = "␛"
)

// StripControlChars is text bound for a terminal prompt: ESC stays visible as ␛, other controls go, tabs and newlines
// stay.
func StripControlChars(s string) string {
	return controls.ReplaceAllString(strings.ReplaceAll(s, "\x1b", escapeKey), "")
}

// OneLineSafe is the same for a value that must stay on one line.
func OneLineSafe(s string) string {
	return jscompat.Trim(StripControlChars(spaceRun.ReplaceAllString(s, " ")))
}

// CleanText is a line of text from a shared file or an agent: one line, no controls, no direction tricks, cut to max;
// "" when nothing is left (null in the old runtime).
func CleanText(v any, max int) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	t := jscompat.Trim(jscompat.Slice(OneLineSafe(spoofing.ReplaceAllString(s, "")), max))
	return t, t != ""
}

var (
	ruMu       sync.Mutex
	ruNumeric  = collate.New(language.Russian, collate.Numeric)
	ruCollator = collate.New(language.Russian)
)

// compareRu is localeCompare(b, 'ru', { numeric: true }): the Russian collation puts Cyrillic before Latin, which the
// collator here does not, so Latin letters are compared as letters of a script that sorts after Cyrillic.
func compareRu(a, b string) int {
	x, y := latinAfterCyrillic(a), latinAfterCyrillic(b)
	ruMu.Lock()
	defer ruMu.Unlock()
	return ruNumeric.CompareString(x, y)
}

// compareRuPlain is localeCompare(b, 'ru').
func compareRuPlain(a, b string) int {
	x, y := latinAfterCyrillic(a), latinAfterCyrillic(b)
	ruMu.Lock()
	defer ruMu.Unlock()
	return ruCollator.CompareString(x, y)
}

// latinAfterCyrillic spells Latin letters (and their compatibility forms) with the letters of a script the collator
// sorts after Cyrillic, case kept, and drops format characters, which the Russian collation ignores.
func latinAfterCyrillic(s string) string {
	var out strings.Builder
	for _, r := range norm.NFKD.String(s) {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(0x0561 + (r - 'a'))
		case r >= 'A' && r <= 'Z':
			out.WriteRune(0x0531 + (r - 'A'))
		case r == 'ß':
			out.WriteString(strings.Repeat(string(rune(0x0561+('s'-'a'))), 2))
		case unicode.Is(unicode.Cf, r):
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

package spaces

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const (
	slugMin = 3
	slugMax = 40
)

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y",
	'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f",
	'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	// Ukrainian and Belarusian letters people also type.
	'і': "i", 'ї': "yi", 'є': "ye", 'ґ': "g", 'ў': "u",
}

// slugBase makes the part of a slug a name gives: Latin letters and digits, words joined by single dashes. The
// result is 3–40 characters of [a-z0-9-], never starting or ending with a dash.
func slugBase(name string) string {
	var b strings.Builder
	dash := true // no dash at the start
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if t, ok := translit[r]; ok {
				if t != "" {
					b.WriteString(t)
					dash = false
				}
				continue
			}
			if !dash && (unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsLetter(r)) {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := trimDash(b.String())
	if s == "" {
		s = "space"
	}
	if len(s) > slugMax {
		s = trimDash(s[:slugMax])
	}
	if len(s) < slugMin {
		s += "-space"
	}
	return s
}

func trimDash(s string) string { return strings.Trim(s, "-") }

// slugCandidate is the n-th try for a base: the base itself, then base-2, base-3, … shortened to fit. Past a few
// tries a random tail ends the race of many equal names.
func slugCandidate(base string, n int) string {
	suffix := ""
	switch {
	case n > 20:
		suffix = "-" + store.RandomBase32(3)[:4]
	case n > 1:
		suffix = "-" + strconv.Itoa(n)
	}
	if len(base)+len(suffix) > slugMax {
		base = trimDash(base[:slugMax-len(suffix)])
	}
	return base + suffix
}

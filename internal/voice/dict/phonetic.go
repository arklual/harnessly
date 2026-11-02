package dict

import (
	"strings"
	"unicode"
)

const (
	// threshold is the minimal similarity (1 - weighted edits / length) of a
	// fuzzy match. For 5..9 letter words it allows one edit; exact sound-keys
	// are also accepted for 4-letter tokens ("клод").
	threshold = 0.8
	// margin is how much better the best dictionary word must score than the
	// runner-up, otherwise the token is ambiguous and left alone.
	margin = 0.1
	// minToken is the minimal token length for a fuzzy match.
	minToken = 5
	maxCands = 48
)

// phon holds Russian sound-alike keys of a Latin dictionary word.
type phon struct {
	word string
	keys [][]rune
}

func newPhon(word string) *phon {
	w := strings.ToLower(word)
	if len(w) < 3 {
		return nil
	}
	for _, r := range w {
		if r < 'a' || r > 'z' {
			return nil
		}
	}
	p := &phon{word: word}
	seen := map[string]bool{}
	for _, c := range translit(w) {
		k := string(soundKey(c))
		if !seen[k] {
			seen[k] = true
			p.keys = append(p.keys, []rune(k))
		}
	}
	return p
}

var (
	finalRules = map[string][]string{
		"ly": {"ли", "лай", "ле"}, "es": {"ес", "ис"}, "ia": {"ия"},
		"y": {"и", "ай"}, "e": {"", "е"},
	}
	rules3 = map[string][]string{"sch": {"ш"}, "tch": {"ч"}, "igh": {"ай"}}
	rules2 = map[string][]string{
		"th": {"т", "з"}, "ch": {"ч", "к", "ш"}, "sh": {"ш"}, "ph": {"ф"}, "ck": {"к"},
		"ks": {"кс"}, "ou": {"у", "ау"}, "oo": {"у"}, "ee": {"и"}, "au": {"о", "ау"},
		"ea": {"и"}, "ey": {"и", "ей"}, "ai": {"ай", "е"}, "ay": {"ей"}, "qu": {"кв"},
		"wh": {"в"}, "ie": {"и"}, "ia": {"ия", "иа"}, "ee ": {"и"},
	}
	rules1 = map[byte]string{
		'a': "а", 'b': "б", 'c': "к", 'd': "д", 'e': "е", 'f': "ф", 'g': "г", 'h': "х",
		'i': "и", 'j': "дж", 'k': "к", 'l': "л", 'm': "м", 'n': "н", 'o': "о", 'p': "п",
		'q': "к", 'r': "р", 's': "с", 't': "т", 'u': "у", 'v': "в", 'w': "в", 'x': "кс",
		'y': "и", 'z': "з",
	}
)

// translit returns several Russian renderings of a lowercase Latin word.
func translit(w string) []string {
	cands := []string{""}
	extend := func(alts []string) {
		var next []string
		for _, c := range cands {
			for _, a := range alts {
				next = append(next, c+a)
			}
		}
		if len(next) > maxCands {
			next = next[:maxCands]
		}
		cands = next
	}
	for i := 0; i < len(w); {
		rest := w[i:]
		switch {
		case len(rest) <= 2 && finalRules[rest] != nil:
			extend(finalRules[rest])
			i = len(w)
		case len(rest) == 1 && finalRules[rest] != nil:
			extend(finalRules[rest])
			i++
		case len(rest) >= 3 && rules3[rest[:3]] != nil:
			extend(rules3[rest[:3]])
			i += 3
		case len(rest) >= 2 && rules2[rest[:2]] != nil:
			extend(rules2[rest[:2]])
			i += 2
		case rest[0] == 'c' && len(rest) > 1 && strings.IndexByte("eiy", rest[1]) >= 0:
			extend([]string{"с"})
			i++
		default:
			extend([]string{rules1[rest[0]]})
			i++
		}
	}
	return cands
}

var voiceless = map[rune]rune{'б': 'п', 'д': 'т', 'г': 'к', 'в': 'ф', 'з': 'с', 'ж': 'ш'}

// soundKey normalizes a lowercase Cyrillic string to a rough sound form:
// ё/э/ы/й/ю/я folded, signs dropped, doubles collapsed, final consonant devoiced.
func soundKey(s string) []rune {
	var out []rune
	for _, r := range s {
		switch r {
		case 'ё', 'э':
			r = 'е'
		case 'ы', 'й':
			r = 'и'
		case 'ю':
			r = 'у'
		case 'я':
			r = 'а'
		case 'ь', 'ъ':
			continue
		}
		if len(out) > 0 && out[len(out)-1] == r {
			continue
		}
		out = append(out, r)
	}
	if n := len(out); n > 0 {
		if v, ok := voiceless[out[n-1]]; ok {
			out[n-1] = v
		}
	}
	return out
}

const vowels = "аеиоуаыэюя"

// class gives the sound class of a key's first letter (vowels share one).
func class(r rune) rune {
	if strings.ContainsRune(vowels, r) {
		return 'а'
	}
	if v, ok := voiceless[r]; ok {
		return v
	}
	return r
}

var weakPairs = map[[2]rune]bool{
	{'б', 'п'}: true, {'д', 'т'}: true, {'г', 'к'}: true, {'в', 'ф'}: true, {'з', 'с'}: true,
	{'ж', 'ш'}: true, {'и', 'е'}: true, {'о', 'а'}: true,
}

func subCost(a, b rune) float64 {
	if a == b {
		return 0
	}
	if weakPairs[[2]rune{a, b}] || weakPairs[[2]rune{b, a}] {
		return 0.5
	}
	return 1
}

// similarity is 1 - weighted Levenshtein distance / longer length.
func similarity(a, b []rune) float64 {
	prev := make([]float64, len(b)+1)
	cur := make([]float64, len(b)+1)
	for j := range prev {
		prev[j] = float64(j)
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = float64(i)
		for j := 1; j <= len(b); j++ {
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+subCost(a[i-1], b[j-1]))
		}
		prev, cur = cur, prev
	}
	n := max(len(a), len(b))
	return 1 - prev[len(b)]/float64(n)
}

var endings = []string{
	"ией", "ого", "ому", "ями", "ами", "ию", "ии", "ия", "ей", "ой", "ою", "ом", "ем",
	"ам", "ям", "ах", "ях", "ов", "ев", "ы", "и", "у", "ю", "а", "я", "е", "о",
}

func isCyrillic(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Cyrillic, r) {
			return false
		}
	}
	return s != ""
}

// stems returns the token itself plus the token with one inflection ending cut.
func stems(t string) []string {
	out := []string{t}
	for _, e := range endings {
		if strings.HasSuffix(t, e) && len([]rune(t))-len([]rune(e)) >= minToken-1 {
			out = append(out, strings.TrimSuffix(t, e))
		}
	}
	return out
}

// score is the best similarity between a Cyrillic token and a dictionary word,
// or 0 when the token is too short or starts with a different sound.
func (p *phon) score(tok string, joined bool) float64 {
	best := 0.0
	ss := stems(tok)
	if joined {
		ss = ss[:1] // no inflection guessing across a split word
	}
	for _, st := range ss {
		k := soundKey(st)
		if len(k) < 4 {
			continue
		}
		for _, c := range p.keys {
			if len(c) < 4 || class(k[0]) != class(c[0]) {
				continue
			}
			s := similarity(k, c)
			if joined && (s < threshold || len(k) < 6) {
				continue
			}
			if s < 1 && (len(k) < minToken || len(c) < minToken || len([]rune(tok)) < minToken) {
				continue
			}
			best = max(best, s)
		}
	}
	return best
}

// bestWord picks the dictionary word clearly closest to tok.
func bestWord(tok string, latin []*phon, joined bool) string {
	if !isCyrillic(tok) {
		return ""
	}
	top, second, word := 0.0, 0.0, ""
	for _, p := range latin {
		s := p.score(tok, joined)
		if s > top {
			top, second, word = s, top, p.word
		} else if s > second {
			second = s
		}
	}
	if top < threshold || top-second < margin {
		return ""
	}
	return word
}

func matchPhonetic(text string, toks []token, i int, latin []*phon) (int, string) {
	if len(latin) == 0 {
		return 0, ""
	}
	if w := bestWord(toks[i].fold, latin, false); w != "" {
		return 1, w
	}
	// Two adjacent tokens may be one word split by the recognizer ("хар несли").
	if spaced(text, toks, i, 2) && isCyrillic(toks[i].fold) {
		if w := bestWord(toks[i].fold+toks[i+1].fold, latin, true); w != "" {
			return 2, w
		}
	}
	return 0, ""
}

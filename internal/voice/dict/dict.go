// Package dict applies a personal word dictionary to speech-recognition text.
// Russian recognizers write foreign brand names in Cyrillic ("синопия"); the
// dictionary maps them back ("Sinopia") by explicit "heard" variants and by a
// conservative phonetic match for Latin dictionary words.
package dict

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxEntries = 200
	MaxWordLen = 64
	maxHeard   = 10
)

// Entry is one dictionary word with optional "how it sounds" variants.
type Entry struct {
	Word  string   `json:"word"`
	Heard []string `json:"heard"`
}

// Clean trims, drops empty/too long words, merges duplicates (case-insensitive)
// and caps the sizes. It never mutates the input.
func Clean(in []Entry) []Entry {
	out := make([]Entry, 0, len(in))
	idx := map[string]int{}
	for _, e := range in {
		w := strings.TrimSpace(e.Word)
		if w == "" || utf8.RuneCountInString(w) > MaxWordLen {
			continue
		}
		k := fold(w)
		i, ok := idx[k]
		if !ok {
			if len(out) >= MaxEntries {
				continue
			}
			i = len(out)
			idx[k] = i
			out = append(out, Entry{Word: w, Heard: []string{}})
		}
		for _, h := range e.Heard {
			h = strings.Join(strings.Fields(h), " ")
			if h == "" || utf8.RuneCountInString(h) > MaxWordLen || len(out[i].Heard) >= maxHeard {
				continue
			}
			if !contains(out[i].Heard, h) {
				out[i].Heard = append(out[i].Heard, h)
			}
		}
	}
	return out
}

// Hints returns the cleaned words, for cloud recognizer prompts.
func Hints(entries []Entry) []string {
	cl := Clean(entries)
	out := make([]string, len(cl))
	for i, e := range cl {
		out[i] = e.Word
	}
	return out
}

type token struct {
	start, end int // byte span in the text
	fold       string
}

func tokenize(s string) []token {
	var toks []token
	start := -1
	for i, r := range s {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			toks = append(toks, token{start, i, fold(s[start:i])})
			start = -1
		}
	}
	if start >= 0 {
		toks = append(toks, token{start, len(s), fold(s[start:])})
	}
	return toks
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// fold lowercases and treats ё as е.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		if r == 'ё' || r == 'Ё' {
			return 'е'
		}
		return unicode.ToLower(r)
	}, s)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

type variant struct {
	parts []string
	word  string
	size  int
}

// Apply rewrites text using the dictionary: explicit Heard variants first
// (longest wins), then phonetic matches for Latin words. Everything else,
// including spacing and punctuation, is kept byte for byte.
func Apply(text string, entries []Entry) string {
	entries = Clean(entries)
	if len(entries) == 0 || text == "" {
		return text
	}
	var variants []variant
	var latin []*phon
	for _, e := range entries {
		for _, h := range e.Heard {
			parts := strings.Fields(strings.Join(tokensOf(h), " "))
			if len(parts) > 0 {
				variants = append(variants, variant{parts, e.Word, len(strings.Join(parts, " "))})
			}
		}
		if p := newPhon(e.Word); p != nil {
			latin = append(latin, p)
		}
	}
	sort.SliceStable(variants, func(i, j int) bool {
		a, b := variants[i], variants[j]
		if len(a.parts) != len(b.parts) {
			return len(a.parts) > len(b.parts)
		}
		return a.size > b.size
	})

	toks := tokenize(text)
	var sb strings.Builder
	last := 0
	for i := 0; i < len(toks); {
		n, word := matchVariant(text, toks, i, variants)
		if n == 0 {
			n, word = matchPhonetic(text, toks, i, latin)
		}
		if n == 0 {
			i++
			continue
		}
		sb.WriteString(text[last:toks[i].start])
		sb.WriteString(word)
		last = toks[i+n-1].end
		i += n
	}
	sb.WriteString(text[last:])
	return sb.String()
}

func tokensOf(s string) []string {
	var out []string
	for _, t := range tokenize(s) {
		out = append(out, t.fold)
	}
	return out
}

// spaced reports whether tokens i..i+n-1 are separated by whitespace only.
func spaced(text string, toks []token, i, n int) bool {
	if i+n > len(toks) {
		return false
	}
	for k := i; k < i+n-1; k++ {
		if strings.TrimSpace(text[toks[k].end:toks[k+1].start]) != "" {
			return false
		}
	}
	return true
}

func matchVariant(text string, toks []token, i int, vs []variant) (int, string) {
	for _, v := range vs {
		n := len(v.parts)
		if !spaced(text, toks, i, n) {
			continue
		}
		ok := true
		for k, p := range v.parts {
			if toks[i+k].fold != p {
				ok = false
				break
			}
		}
		if ok {
			return n, v.word
		}
	}
	return 0, ""
}

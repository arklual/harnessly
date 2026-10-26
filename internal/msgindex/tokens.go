package msgindex

import (
	"strings"
	"unicode"
)

// Words longer than this (hashes, encoded blobs) are not indexed; a text is indexed up to TextMax runes.
const (
	wordMax = 40
	TextMax = 16000
)

// foldRune lowercases a rune and folds ё into е; the result is always one rune, so positions in the folded text are
// positions in the original.
func foldRune(r rune) rune {
	r = unicode.ToLower(r)
	if r == 'ё' {
		return 'е'
	}
	return r
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// span is a word of a text: [from, to) in runes.
type span struct{ from, to int }

// wordsOf cuts folded runes into words.
func wordsOf(folded []rune) []span {
	var out []span
	start := -1
	for i, r := range folded {
		switch {
		case isWordRune(r) && start < 0:
			start = i
		case !isWordRune(r) && start >= 0:
			out = append(out, span{start, i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, span{start, len(folded)})
	}
	return out
}

func foldAll(text string) []rune {
	rs := []rune(text)
	for i, r := range rs {
		rs[i] = foldRune(r)
	}
	return rs
}

// Tokens are the distinct words of a text, folded.
func Tokens(text string) []string {
	folded := foldAll(text)
	seen := map[string]struct{}{}
	var out []string
	for _, w := range wordsOf(folded) {
		if w.to-w.from > wordMax {
			continue
		}
		word := string(folded[w.from:w.to])
		if _, ok := seen[word]; !ok {
			seen[word] = struct{}{}
			out = append(out, word)
		}
	}
	return out
}

// queryTokens are the words of a query, folded and distinct.
func queryTokens(q string) []string { return Tokens(strings.TrimSpace(q)) }

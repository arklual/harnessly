package typist

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// What of the box is ours. Claude reads a big picture late: a picture the typist pasted for a message lands after the
// message's text as its path, so the box holds our text and our path together («…где эмэры.⏎⏎/…/up_2BF4….png»). Read
// with the path in it, the text was nobody's, and the line stopped for good behind a box of our own making (28.10).

// pieceMin: a piece of an earlier message of ours, found in the box, is that long at least (white space aside) — a
// short draft of the person's that happens to be part of one is theirs.
const pieceMin = 24

// pathPattern matches a picture path as the box shows it: escaped as pasted or bare, wrapped anywhere.
func pathPattern(path string) *regexp.Regexp {
	var b strings.Builder
	for i, r := range path {
		if i > 0 {
			b.WriteString(sp + `*`)
		}
		b.WriteString(`\\?`)
		b.WriteString(regexp.QuoteMeta(string(r)))
	}
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

// withoutPaths is the look with the picture paths of ours taken out of its box: what is left is what the box holds of
// text (and references).
func withoutPaths(s Screen, paths []string) Screen {
	if !s.Readable || s.Box == "" || len(paths) == 0 {
		return s
	}
	box := s.Box
	for _, path := range paths {
		if path == "" {
			continue
		}
		if re := pathPattern(path); re != nil {
			box = re.ReplaceAllString(box, " ")
		}
	}
	if box == s.Box {
		return s
	}
	s.Box = box
	if blank(box) {
		s.Box, s.Input, s.Pictures, s.OnlyPictures = "", "", 0, false
		return s
	}
	s.Input = strings.TrimFunc(box, isSpaceRune)
	s.Pictures, s.OnlyPictures = CountImageRefs(box), imageTokensOnly.MatchString(box)
	return s
}

// ownPaths are the picture paths the box may hold of ours for a message: its own and the ones pasted before.
func ownPaths(m Message) []string {
	return append(append([]string(nil), m.Images...), m.Leftovers...)
}

// TypedLeftover is the earlier message of ours the box holds what is left of (its paths and references aside): its
// start, or a piece of it pieceMin long at least. "" when the box is not that.
func TypedLeftover(s Screen, provider wire.Provider, texts []string) string {
	if provider == wire.ProviderCodex || !s.Readable || s.Input == "" {
		return ""
	}
	box := noSpace(imageRef.ReplaceAllString(unwrapRefs(s.Box), " "))
	if box == "" {
		return ""
	}
	for _, text := range texts {
		if blank(text) {
			continue
		}
		if StillInBox(s, provider, text) {
			return text
		}
		whole := noSpace(imageRef.ReplaceAllString(text, " "))
		if strings.Contains(whole, box) && (box == whole || utf8.RuneCountInString(box) >= pieceMin) {
			return text
		}
	}
	return ""
}

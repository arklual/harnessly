// Package catalog reads, from the machine's files, what a composer offers for an agent CLI (Claude Code or Codex):
// its slash commands and skills for a folder, and the models and reasoning efforts it can be started with. The
// answers keep the shapes, texts and ordering of the previous runtime, which built them in JavaScript; the helpers
// here reproduce the few JavaScript string rules that change a result (what counts as whitespace, UTF-16 lengths,
// locale ordering, UTF-8 decoding with replacement).
package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// isSpace is JavaScript's whitespace: what \s matches and trim() removes.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// trim is String.prototype.trim.
func trim(s string) string { return strings.TrimFunc(s, isSpace) }

// isLineTerminator: what JavaScript's `.` does not match.
func isLineTerminator(r rune) bool { return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 }

// collapseSpace is s.replace(/\s+/g, ' ').
func collapseSpace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if isSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// utf16Len is a JavaScript string's length.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// utf16Prefix is s.slice(0, n) in UTF-16 units; half a surrogate pair at the cut becomes U+FFFD, as it does once
// JSON is read back.
func utf16Prefix(s string, n int) string {
	units := utf16.Encode([]rune(s))
	if len(units) <= n {
		return s
	}
	return string(utf16.Decode(units[:n]))
}

// decodeUTF8 decodes bytes as a JavaScript runtime does (WHATWG): each maximal invalid subpart becomes one U+FFFD,
// so a character cut at the end of a read window is one replacement character.
func decodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		need, lower, upper := 0, byte(0x80), byte(0xbf)
		switch {
		case c >= 0xc2 && c <= 0xdf:
			need = 1
		case c >= 0xe0 && c <= 0xef:
			need = 2
			if c == 0xe0 {
				lower = 0xa0
			} else if c == 0xed {
				upper = 0x9f
			}
		case c >= 0xf0 && c <= 0xf4:
			need = 3
			if c == 0xf0 {
				lower = 0x90
			} else if c == 0xf4 {
				upper = 0x8f
			}
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j, ok := i+1, true
		for k := 0; k < need; k++ {
			if j >= len(b) || b[j] < lower || b[j] > upper {
				ok = false
				break
			}
			lower, upper = 0x80, 0xbf
			j++
		}
		if !ok {
			out.WriteRune(utf8.RuneError)
			i = j
			continue
		}
		out.Write(b[i:j])
		i = j
	}
	return out.String()
}

// isInside: target is root or below it. A first segment starting with ".." counts as outside, as it did before.
func isInside(root, target string) bool {
	root, _ = filepath.Abs(root)
	target, _ = filepath.Abs(target)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// realpath resolves symlinks of a path that must exist, like realpath(3): "a/link/.." is the parent of the link's
// target, and "file/.." does not exist.
func realpath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		p = wd + "/" + p
	}
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	return real, true
}

// newCollator orders names as the previous runtime did with localeCompare (ICU, en-US). Not safe for concurrent use.
func newCollator() *collate.Collator { return collate.New(language.AmericanEnglish) }

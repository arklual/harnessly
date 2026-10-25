package shots

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The pictures a message names: «Скриншот: /tmp/claude-1001/…/scratchpad/home.png» at the end of an agent's answer, the
// path of a picture pasted into the old view (…/uploads/…/1792894270382-e2af4dde-image.png) in the person's message.
// They are found in the conversation's own texts only, and served only from the folders a conversation may show:
// its working tree, Claude's scratchpads, the uploads. The page asks for one by its id, never by its path.

// namedRE is an absolute path to a picture as a message writes it: no spaces, quotes, brackets or markdown marks in it.
var namedRE = regexp.MustCompile("/[^\\s\"'<>()\\[\\]{}\\\\`*|,;:«»]+\\.(?i:png|jpe?g|gif|webp)")

// NamedMax is how many pictures one text names, at most.
const NamedMax = 12

// Named is the absolute paths of pictures a text names, in order, each once, exactly as the text writes them. A path
// inside a word or a link (https://host/a.png, ./a.png, ~/a.png) is not one.
func Named(text string) []string {
	var out []string
	for _, m := range namedRE.FindAllStringIndex(text, -1) {
		if !boundary(text, m[0], m[1]) {
			continue
		}
		p := text[m[0]:m[1]]
		if !contains(out, p) {
			if out = append(out, p); len(out) == NamedMax {
				break
			}
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// boundary: the path starts the text or follows a space or an opening mark, and no letter or digit goes on after it.
func boundary(text string, from, to int) bool {
	if from > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:from])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_./~:-", r) {
			return false
		}
	}
	if to < len(text) {
		r, n := utf8.DecodeRuneInString(text[to:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '/' {
			return false
		}
		// «a.png.bak» is another file; «a.png.» ends a sentence.
		if next, _ := utf8.DecodeRuneInString(text[to+n:]); r == '.' && (unicode.IsLetter(next) || unicode.IsDigit(next)) {
			return false
		}
	}
	return true
}

// ScratchRoot is where Claude Code keeps the scratchpads of this user's sessions.
func ScratchRoot() string { return fmt.Sprintf("/tmp/claude-%d", os.Getuid()) }

// Allowed is the picture file a path names when it may be shown: an absolute path to an image by its name that, its
// links followed, is a plain non-empty file of at most MaxBytes inside one of the roots (the roots' own links followed
// too). It gives the real path and the file's stat; ok is false for anything else, a file that is not there included.
func Allowed(path string, roots []string) (real string, st os.FileInfo, ok bool) {
	if !filepath.IsAbs(path) || !IsImage(path) {
		return "", nil, false
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil || !IsImage(real) || !within(real, roots) {
		return "", nil, false
	}
	st, err = os.Lstat(real)
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > MaxBytes {
		return "", nil, false
	}
	return real, st, true
}

// within: the real path is inside one of the real roots (not a root itself).
func within(real string, roots []string) bool {
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		r, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil || r == "/" {
			continue
		}
		if rel, err := filepath.Rel(r, real); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}

// OpenIn opens a picture a message named, as Open does, after Allowed has said it may be shown; the file opened must
// be the one checked, so a link swapped in between is not followed out of the roots.
func OpenIn(path string, roots []string) (File, error) {
	real, st, ok := Allowed(path, roots)
	if !ok {
		return File{}, ErrNotServed
	}
	f, err := Open(real)
	if err != nil {
		return File{}, err
	}
	got, err := f.Body.Stat()
	if err != nil || !os.SameFile(got, st) {
		f.Body.Close()
		return File{}, ErrNotServed
	}
	return f, nil
}

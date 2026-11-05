package files

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// «Найти в файлах» of «Файлы»: the lines of the text files under a folder that hold a piece of text. The walk
// is the «@» picker's: no .git, node_modules or build folders, no folders starting with a dot, no files that look like
// credentials, and no links (a link to a folder is not entered, a link to a file is not read). A file that is too big
// or not text is passed over; the search stops at a number of lines or of files, or when its time is up, and says so.

const (
	searchMaxHits     = 300
	searchMaxPerFile  = 50
	searchMaxFiles    = 20_000
	searchMaxFileSize = 1 << 20
	searchMaxLine     = 240
	// SearchMaxQuery is how long a query may be, in bytes.
	SearchMaxQuery = 200
)

// TextHit is one line holding the query: the file (relative to the folder searched, with slashes), the line and the
// column from 1 (the column in characters), and the line itself, cut around the match when it is long.
type TextHit struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Text string `json:"text"`
}

// SearchText finds the query under root. The case is ignored unless the query has a capital letter. secret, when set,
// says a file (by its absolute path) is not to be read. truncated is true when the search stopped before the end.
func SearchText(ctx context.Context, root, query string, secret func(abs string) bool) (hits []TextHit, truncated bool) {
	if query == "" {
		return nil, false
	}
	fold := !hasUpper(query)
	needle := []byte(query)
	if fold {
		needle = bytes.ToLower(needle)
	}
	files := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil || len(hits) >= searchMaxHits || files >= searchMaxFiles {
			truncated = true
			return fs.SkipAll
		}
		if err != nil {
			if d != nil && d.IsDir() && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if p == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if ignoredDirs[name] || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || secretFile.MatchString(name) || (secret != nil && secret(p)) {
			return nil
		}
		files++
		rel, _ := filepath.Rel(root, p)
		hits = append(hits, searchFile(p, filepath.ToSlash(rel), needle, fold, searchMaxHits-len(hits))...)
		return nil
	})
	if len(hits) > searchMaxHits {
		hits, truncated = hits[:searchMaxHits], true
	}
	return hits, truncated
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// searchFile is the lines of one file holding the needle, at most limit of them (and searchMaxPerFile).
func searchFile(abs, rel string, needle []byte, fold bool, limit int) []TextHit {
	f, err := os.Open(abs)
	if err != nil {
		return nil
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > searchMaxFileSize {
		return nil
	}
	r := bufio.NewReaderSize(f, 64<<10)
	if head, _ := r.Peek(8000); bytes.IndexByte(head, 0) >= 0 {
		return nil
	}
	limit = min(limit, searchMaxPerFile)
	var out []TextHit
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), searchMaxFileSize+1)
	for n := 1; sc.Scan(); n++ {
		line := sc.Bytes()
		hay := line
		if fold {
			hay = bytes.ToLower(line)
		}
		at := bytes.Index(hay, needle)
		if at < 0 {
			continue
		}
		// bytes.ToLower keeps the length for most letters; where it does not, the column is counted on the folded line.
		if len(hay) != len(line) {
			line = hay
		}
		out = append(out, TextHit{Path: rel, Line: n, Col: utf8.RuneCount(line[:at]) + 1, Text: lineAround(line, at)})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// lineAround is the line with its indent dropped, cut to searchMaxLine characters so that the match shows.
func lineAround(line []byte, at int) string {
	text := strings.ToValidUTF8(string(line), "�")
	lead := len(text) - len(strings.TrimLeft(text, " \t"))
	if at >= lead {
		text, at = text[lead:], at-lead
	}
	text = strings.TrimRight(text, " \t\r")
	if utf8.RuneCountInString(text) <= searchMaxLine {
		return text
	}
	// Some context before the match, then as much after it as fits.
	from := max(0, at-40)
	for from > 0 && !utf8.RuneStart(text[from]) {
		from--
	}
	runes := []rune(text[from:])
	if len(runes) > searchMaxLine {
		runes = runes[:searchMaxLine]
	}
	cut := string(runes)
	if from > 0 {
		cut = "…" + cut
	}
	return cut + "…"
}

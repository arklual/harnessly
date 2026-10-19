package files

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Memory notes the agent cites by bare file name: Claude Code keeps memory per project, and sessions may keep synced
// copies of the same files. The freshest copy wins.

var memoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.md$`)

var frontMatter = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n?`)

// memoryDirs are <parent>/<entry>/memory for every entry of every parent, parents in order, entries by name.
func memoryDirs(parents []string) []string {
	var dirs []string
	for _, parent := range parents {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, e := range entries {
			dirs = append(dirs, filepath.Join(parent, DecodeUTF8([]byte(e.Name())), "memory"))
		}
	}
	return dirs
}

// ReadMemoryNote is the freshest copy of the named memory under the parents, or ok false.
func ReadMemoryNote(name string, parents []string) (Object, bool) {
	if !memoryName.MatchString(name) {
		return nil, false
	}
	type candidate struct {
		file  string
		mtime float64
	}
	var found []candidate
	for _, dir := range memoryDirs(parents) {
		file := filepath.Join(dir, name)
		st, err := os.Stat(file)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		t := st.ModTime()
		// As Node computes mtimeMs: seconds and nanoseconds apart, so the sum is the same double.
		mtime := float64(t.Unix())*1e3 + float64(t.Nanosecond())/1e6
		found = append(found, candidate{file, mtime})
	}
	if len(found) == 0 {
		return nil, false
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].mtime > found[j].mtime })
	raw, err := os.ReadFile(found[0].file)
	if err != nil {
		return nil, false
	}
	return ParseMemoryNote(name, DecodeUTF8(raw)), true
}

// ParseMemoryNote reads the title (the first heading, else the front matter's name, else the file name), the
// description and the text of a note.
func ParseMemoryNote(name, raw string) Object {
	front := frontMatter.FindStringSubmatch(raw)
	field := func(key string) string {
		if front == nil {
			return ""
		}
		v, ok := lineValue(front[1], key+":", 0)
		if !ok {
			return ""
		}
		return stripQuotes(trimSpace(v))
	}
	body := trimSpace(raw)
	if front != nil {
		body = trimSpace(raw[len(front[0]):])
	}
	heading, _ := lineValue(body, "#", 1)
	heading = trimSpace(heading)
	title := heading
	if title == "" {
		title = field("name")
	}
	if title == "" {
		title = strings.TrimSuffix(name, ".md")
	}
	return Object{{"name", name}, {"title", title}, {"description", field("description")}, {"body", body}}
}

func isLineEnd(r rune) bool { return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 }

// lineValue finds the first line starting with prefix, then at least minSpace whitespace characters (which may run
// over line ends), then the value up to the end of its line — the pattern ^prefix\s*(.+)$ of the old notes, multiline.
func lineValue(text, prefix string, minSpace int) (string, bool) {
	for p := 0; p <= len(text); {
		at := strings.Index(text[p:], prefix)
		if at < 0 {
			return "", false
		}
		p += at
		lineStart := p == 0
		if !lineStart {
			r, _ := utf8.DecodeLastRuneInString(text[:p])
			lineStart = isLineEnd(r)
		}
		if lineStart {
			if v, ok := valueAfter(text, p+len(prefix), minSpace); ok {
				return v, true
			}
		}
		p++
	}
	return "", false
}

// valueAfter matches \s{minSpace,}(.+)$ at i, backtracking the way a regular expression would.
func valueAfter(text string, i, minSpace int) (string, bool) {
	// The whitespace run and where each of its characters starts.
	var starts []int
	j := i
	for j < len(text) {
		r, size := utf8.DecodeRuneInString(text[j:])
		if !isSpace(r) {
			break
		}
		starts = append(starts, j)
		j += size
	}
	if j < len(text) && len(starts) >= minSpace {
		return text[j:lineEndFrom(text, j)], true
	}
	// Nothing but whitespace follows: the value is the last whitespace character that does not end a line.
	for k := len(starts) - 1; k >= minSpace; k-- {
		r, _ := utf8.DecodeRuneInString(text[starts[k]:])
		if !isLineEnd(r) {
			return text[starts[k]:lineEndFrom(text, starts[k])], true
		}
	}
	return "", false
}

func lineEndFrom(text string, i int) int {
	for j, r := range text[i:] {
		if isLineEnd(r) {
			return i + j
		}
	}
	return len(text)
}

// stripQuotes takes one quote off the start and one off the end.
func stripQuotes(s string) string {
	start, end := 0, len(s)
	if end > 0 && (s[0] == '"' || s[0] == '\'') {
		start = 1
	}
	if end > start && (s[end-1] == '"' || s[end-1] == '\'') {
		end--
	}
	return s[start:end]
}

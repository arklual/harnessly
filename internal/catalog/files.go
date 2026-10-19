package catalog

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/collate"
)

const (
	// headBytes: only the head of a command or skill file is read; bodies are never loaded, let alone served.
	headBytes = 8 * 1024
	// A runaway tree (a linked repository, node_modules) stops the walk here.
	maxWalkEntries = 4000
	maxWalkDepth   = 4
)

// mtimeMs is a path's modification time in milliseconds as the previous runtime computed it, 0 when it is missing.
func mtimeMs(p string) float64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	t := st.ModTime()
	return float64(t.Unix())*1e3 + float64(t.Nanosecond())/1e6
}

// readJSON: the parsed file; ok is false when it cannot be read or parsed.
func readJSON(p string) (v any, ok bool) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	return v, true
}

func sortedEntries(dir string, col *collate.Collator) ([]os.DirEntry, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	sort.SliceStable(entries, func(i, j int) bool { return col.CompareString(entries[i].Name(), entries[j].Name()) < 0 })
	return entries, true
}

// fileCommand is a command file and its name: sub-folders become ":" namespaces, as Claude names them.
type fileCommand struct{ file, name string }

// listMarkdownCommands: the "*.md" files under root. Linked folders are not followed; the walk is bounded.
func listMarkdownCommands(root string, col *collate.Collator) []fileCommand {
	var found []fileCommand
	seen := 0
	var walk func(dir string, prefix []string, depth int) bool
	walk = func(dir string, prefix []string, depth int) bool {
		entries, ok := sortedEntries(dir, col)
		if !ok {
			return true
		}
		for _, e := range entries {
			seen++
			if seen > maxWalkEntries {
				return false
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			full := filepath.Join(dir, name)
			mode := e.Type()
			if mode.IsDir() && depth < maxWalkDepth {
				if !walk(full, append(append([]string{}, prefix...), name), depth+1) {
					return false
				}
			} else if (mode.IsRegular() || mode&os.ModeSymlink != 0) && strings.HasSuffix(name, ".md") {
				found = append(found, fileCommand{full, strings.Join(append(append([]string{}, prefix...), strings.TrimSuffix(name, ".md")), ":")})
			}
		}
		return true
	}
	walk(root, nil, 0)
	return found
}

// listSkillFiles: "<root>/<dir>/SKILL.md" of every skill folder, then of the nested roots (Codex's ".system").
func listSkillFiles(root string, col *collate.Collator, nested ...string) []fileCommand {
	var found []fileCommand
	scan := func(dir string) {
		entries, ok := sortedEntries(dir, col)
		if !ok {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || !(e.IsDir() || e.Type()&os.ModeSymlink != 0) {
				continue
			}
			file := filepath.Join(dir, name, "SKILL.md")
			if _, err := os.Stat(file); err == nil {
				found = append(found, fileCommand{file, name})
			}
		}
	}
	scan(root)
	for _, sub := range nested {
		scan(filepath.Join(root, sub))
	}
	return found
}

// readHead: the first 8 KiB of file, if it really lives under root (a link pointing out of a scanned root is skipped).
func readHead(file, root string) (string, bool) {
	realRoot, ok := realpath(root)
	if !ok {
		return "", false
	}
	realFile, ok := realpath(file)
	if !ok || !isInside(realRoot, realFile) {
		return "", false
	}
	f, err := os.Open(realFile)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, headBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", false
	}
	return decodeUTF8(buf[:n]), true
}

// splitLines is text.split(/\r?\n/).
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// keyValue matches a frontmatter line `key: value`: the key from the line start, then ":" and either nothing or
// whitespace and a value on one line.
func keyValue(line string) (key, value string, ok bool) {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return "", "", false
	}
	for _, c := range line[:colon] {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "", "", false
		}
	}
	rest := line[colon+1:]
	if rest == "" {
		return line[:colon], "", true
	}
	if !startsWithSpace(rest) {
		return "", "", false
	}
	value = strings.TrimLeftFunc(rest, isSpace)
	if strings.ContainsFunc(value, isLineTerminator) {
		return "", "", false
	}
	return line[:colon], value, true
}

func unquote(value string) string {
	text := trim(value)
	if len(text) >= 2 && strings.HasPrefix(text, `"`) && strings.HasSuffix(text, `"`) {
		s := text[1 : len(text)-1]
		s = strings.ReplaceAll(s, `\"`, `"`)
		s = strings.ReplaceAll(s, `\n`, "\n")
		return strings.ReplaceAll(s, `\\`, `\`)
	}
	if len(text) >= 2 && strings.HasPrefix(text, "'") && strings.HasSuffix(text, "'") {
		return strings.ReplaceAll(text[1:len(text)-1], "''", "'")
	}
	return text
}

func isBlockMarker(v string) bool {
	if v == "" || (v[0] != '|' && v[0] != '>') {
		return false
	}
	return len(v) == 1 || (len(v) == 2 && (v[1] == '+' || v[1] == '-'))
}

func startsWithSpace(s string) bool {
	for _, r := range s {
		return isSpace(r)
	}
	return false
}

// parseFrontmatter: the top-level `key: value` pairs of a Markdown frontmatter (plain and quoted scalars, "|" and ">"
// blocks; nested maps and lists are skipped) and the body after it.
func parseFrontmatter(text string) (map[string]string, string) {
	lines := splitLines(strings.TrimPrefix(text, "\uFEFF"))
	data := map[string]string{}
	if trim(lines[0]) != "---" {
		return data, text
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if trim(lines[i]) == "---" {
			end = i
			break
		}
	}
	// An unterminated block (or one longer than the read window) yields what was parsed so far and no body.
	headEnd := end
	if end == -1 {
		headEnd = len(lines)
	}
	for i := 1; i < headEnd; i++ {
		key, raw, ok := keyValue(lines[i])
		if !ok {
			continue
		}
		value := trim(raw)
		if isBlockMarker(value) {
			var block []string
			for i+1 < headEnd && (trim(lines[i+1]) == "" || startsWithSpace(lines[i+1])) {
				i++
				block = append(block, trim(lines[i]))
			}
			if value[0] == '|' {
				data[key] = trim(strings.Join(block, "\n"))
			} else {
				var kept []string
				for _, l := range block {
					if l != "" {
						kept = append(kept, l)
					}
				}
				data[key] = trim(strings.Join(kept, " "))
			}
			continue
		}
		if value == "" {
			continue
		}
		data[key] = unquote(value)
	}
	if end == -1 {
		return data, ""
	}
	return data, strings.Join(lines[end+1:], "\n")
}

// firstBodyLine: the first non-empty line that is not a heading, the description of a file without one.
func firstBodyLine(body string) string {
	for _, l := range splitLines(body) {
		if t := trim(l); t != "" && !strings.HasPrefix(t, "#") {
			return t
		}
	}
	return ""
}

// commandMeta is what a command or skill file says about itself.
type commandMeta struct {
	name          string // "" when not given
	description   string
	argumentHint  *string
	userInvocable bool
}

func readCommandMeta(file, root string) (commandMeta, bool) {
	head, ok := readHead(file, root)
	if !ok {
		return commandMeta{}, false
	}
	data, body := parseFrontmatter(head)
	m := commandMeta{name: trim(data["name"]), description: data["description"], userInvocable: true}
	if m.description == "" {
		m.description = firstBodyLine(body)
	}
	if hint := trim(data["argument-hint"]); hint != "" {
		m.argumentHint = strPtr(hint)
	}
	if v, set := data["user-invocable"]; set && strings.ToLower(trim(v)) == "false" {
		m.userInvocable = false
	}
	return m, true
}

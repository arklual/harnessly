package agentcfg

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

// A small editor of TOML text for the few scalar keys the page writes. Codex's config.toml is the person's file, full of
// comments and tables nobody here models, so it is edited where it stands: a line replaced, a line added, a line
// removed; everything else keeps its bytes. The caller reads the result again with a real parser and compares it with
// what it meant to write, so a file this editor does not understand is refused, not damaged.

type tomlDoc struct {
	lines []string
}

func parseTOMLText(text string) *tomlDoc {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return &tomlDoc{}
	}
	return &tomlDoc{lines: strings.Split(text, "\n")}
}

func (d *tomlDoc) String() string {
	if len(d.lines) == 0 {
		return ""
	}
	return strings.Join(d.lines, "\n") + "\n"
}

// tomlKey is a bare or quoted key at the start of s, and the rest after it.
func tomlKey(s string) (key, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return "", "", false
	}
	switch s[0] {
	case '"':
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '"' {
				k, err := strconv.Unquote(s[:i+1])
				if err != nil {
					return "", "", false
				}
				return k, s[i+1:], true
			}
		}
		return "", "", false
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", "", false
		}
		return s[1 : 1+end], s[2+end:], true
	}
	i := 0
	for i < len(s) && (s[i] == '_' || s[i] == '-' || s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
		i++
	}
	if i == 0 {
		return "", "", false
	}
	return s[:i], s[i:], true
}

// header is the table a `[a.b]` line opens; array is true for `[[a.b]]`.
func header(line string) (path []string, array, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "[") {
		return nil, false, false
	}
	array = strings.HasPrefix(s, "[[")
	rest := strings.TrimPrefix(strings.TrimPrefix(s, "["), "[")
	for {
		k, after, kok := tomlKey(rest)
		if !kok {
			return nil, false, false
		}
		path = append(path, k)
		after = strings.TrimLeft(after, " \t")
		switch {
		case strings.HasPrefix(after, "."):
			rest = after[1:]
		case strings.HasPrefix(after, "]"):
			return path, array, true
		default:
			return nil, false, false
		}
	}
}

// valueEnd is the index of the last line of the value that starts on line i after its `=`: a string or an array may go on
// over several lines.
func (d *tomlDoc) valueEnd(i int, from string) int {
	depth := 0
	var quote string // the closing delimiter of a string that is open
	text := from
	for n := i; ; {
		for j := 0; j < len(text); j++ {
			c := text[j]
			if quote != "" {
				switch {
				case strings.HasPrefix(text[j:], quote):
					j += len(quote) - 1
					quote = ""
				case c == '\\' && strings.HasPrefix(quote, `"`):
					j++
				}
				continue
			}
			switch c {
			case '#':
				j = len(text)
			case '"', '\'':
				if strings.HasPrefix(text[j:], strings.Repeat(string(c), 3)) {
					quote = strings.Repeat(string(c), 3)
					j += 2
				} else {
					quote = string(c)
				}
			case '[', '{':
				depth++
			case ']', '}':
				depth--
			}
			// A single-line string ends with its line.
		}
		if len(quote) == 1 {
			quote = ""
		}
		if depth <= 0 && quote == "" {
			return n
		}
		n++
		if n >= len(d.lines) {
			return len(d.lines) - 1
		}
		text = d.lines[n]
	}
}

// entry is where a key sits: the lines it takes.
type entry struct{ start, end int }

// scan walks the document: for each table path the index of its header (-1 for the top), the lines of its keys, and the
// last line the table takes.
type tableSpan struct {
	path   []string
	header int
	keys   map[string]entry
	last   int // the last line of the table's keys, or its header; -1 for an empty top
	array  bool
}

func (d *tomlDoc) scan() []*tableSpan {
	spans := []*tableSpan{{header: -1, keys: map[string]entry{}, last: -1}}
	cur := spans[0]
	for i := 0; i < len(d.lines); i++ {
		line := d.lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if path, array, ok := header(line); ok {
			cur = &tableSpan{path: path, header: i, keys: map[string]entry{}, last: i, array: array}
			spans = append(spans, cur)
			continue
		}
		key, rest, ok := tomlKey(line)
		if !ok {
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		end := d.valueEnd(i, rest[1:])
		cur.keys[key] = entry{i, end}
		cur.last = end
		i = end
	}
	return spans
}

func (d *tomlDoc) table(path []string) *tableSpan {
	for _, sp := range d.scan() {
		if !sp.array && slices.Equal(sp.path, path) {
			return sp
		}
	}
	return nil
}

func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tomlLiteral writes a value the page may write: a string, a switch or a whole number.
func tomlLiteral(v any) (string, error) {
	switch x := v.(type) {
	case string:
		if strings.ContainsAny(x, "\r\n\x00") {
			return "", errors.New("agentcfg: a line break in a value")
		}
		return tomlQuote(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		return strconv.FormatInt(int64(x), 10), nil
	}
	return "", errors.New("agentcfg: not a value")
}

func keyText(k string) string {
	if k != "" && strings.Trim(k, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == "" {
		return k
	}
	return tomlQuote(k)
}

func headerText(path []string) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = keyText(p)
	}
	return "[" + strings.Join(parts, ".") + "]"
}

// set gives a key of a table a value: the key's line replaced, or the key added at the end of the table, or the table
// added at the end of the file.
func (d *tomlDoc) set(path []string, key string, literal string) {
	line := keyText(key) + " = " + literal
	sp := d.table(path)
	if sp != nil {
		if e, ok := sp.keys[key]; ok {
			// The comment at the end of the old line is kept.
			comment := ""
			if e.start == e.end {
				comment = trailingComment(d.lines[e.start])
			}
			d.lines = slices.Concat(d.lines[:e.start], []string{line + comment}, d.lines[e.end+1:])
			return
		}
		at := sp.last + 1
		if sp.header < 0 && sp.last < 0 {
			at = 0
		}
		d.lines = slices.Insert(d.lines, at, line)
		if sp.header < 0 && sp.last < 0 && len(d.lines) > 1 && strings.TrimSpace(d.lines[1]) != "" {
			d.lines = slices.Insert(d.lines, 1, "")
		}
		return
	}
	if len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) != "" {
		d.lines = append(d.lines, "")
	}
	d.lines = append(d.lines, headerText(path), line)
}

// trailingComment is the spaces and the `# comment` at the end of a single-line key's line, "" when there is none.
func trailingComment(line string) string {
	_, rest, ok := tomlKey(line)
	if !ok {
		return ""
	}
	rest = strings.TrimLeft(rest, " \t")
	rest = strings.TrimPrefix(rest, "=")
	var quote byte
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			gap := len(strings.TrimRight(rest[:i], " \t"))
			return rest[gap:]
		}
	}
	return ""
}

// remove takes a key out of a table.
func (d *tomlDoc) remove(path []string, key string) {
	sp := d.table(path)
	if sp == nil {
		return
	}
	if e, ok := sp.keys[key]; ok {
		d.lines = slices.Concat(d.lines[:e.start], d.lines[e.end+1:])
	}
}

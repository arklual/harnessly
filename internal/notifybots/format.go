package notifybots

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MessageLimit is Telegram's longest message text, in characters after the entities are parsed; the HTML of a part is
// kept under it so that its visible text always is.
const MessageLimit = 4096

// MaxText is the longest text an agent may send at once (a few messages).
const MaxText = 16000

var (
	boldRe   = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	codeRe   = regexp.MustCompile("`([^`\n]+)`")
	linkRe   = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^\s)]+)\)`)
	headerRe = regexp.MustCompile(`(?m)^#{1,6}[ \t]+(.+)$`)
)

// ToHTML is the agent's markdown-ish text as Telegram HTML: everything escaped, then **bold**, `code`, ```blocks```,
// [links](https://…) and # headings made into the few tags Telegram takes. Whatever else is left as written.
func ToHTML(text string) string {
	var b strings.Builder
	parts := strings.Split(text, "```")
	// An unclosed fence is text, not the start of a block.
	closed := len(parts)%2 == 1
	for i, part := range parts {
		inBlock := i%2 == 1 && (closed || i < len(parts)-1)
		switch {
		case inBlock:
			// The fence's first line names the language: Telegram has no use for it.
			if nl := strings.IndexByte(part, '\n'); nl >= 0 && !strings.ContainsAny(part[:nl], " \t") {
				part = part[nl+1:]
			}
			b.WriteString("<pre>" + html.EscapeString(strings.TrimRight(part, "\n")) + "</pre>")
		case i%2 == 1:
			// The text after a fence that never closes, with the fence as written.
			b.WriteString(inline(html.EscapeString("```" + part)))
		default:
			b.WriteString(inline(html.EscapeString(part)))
		}
	}
	return b.String()
}

// inline marks up escaped text (escaping leaves *, ` and [ ] ( ) as they were).
func inline(s string) string {
	s = codeRe.ReplaceAllString(s, "<code>$1</code>")
	s = boldRe.ReplaceAllString(s, "<b>$1</b>")
	s = headerRe.ReplaceAllString(s, "<b>$1</b>")
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		// The address was escaped with the rest; an attribute takes it as it is now.
		return `<a href="` + strings.ReplaceAll(sub[2], `"`, "&quot;") + `">` + sub[1] + "</a>"
	})
	return s
}

// Split cuts a text into parts whose HTML stays under the limit: at blank lines, then at lines, then at spaces, and
// a word longer than a message is cut where it has to be.
func Split(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if fits(text, limit) {
		return []string{text}
	}
	for _, sep := range []string{"\n\n", "\n", " "} {
		if !strings.Contains(text, sep) {
			continue
		}
		pieces := strings.Split(text, sep)
		var out []string
		cur := ""
		for _, p := range pieces {
			next := p
			if cur != "" {
				next = cur + sep + p
			}
			if fits(next, limit) {
				cur = next
				continue
			}
			if cur != "" {
				out = append(out, cur)
			}
			if fits(p, limit) {
				cur = p
				continue
			}
			sub := Split(p, limit)
			out = append(out, sub[:len(sub)-1]...)
			cur = sub[len(sub)-1]
		}
		if strings.TrimSpace(cur) != "" {
			out = append(out, cur)
		}
		return trimAll(out)
	}
	// One long word: cut by characters, small enough that even fully escaped it fits.
	var out []string
	step := limit / 6
	for len(text) > 0 {
		n, i := 0, 0
		for i < len(text) && n < step {
			_, size := utf8.DecodeRuneInString(text[i:])
			i += size
			n++
		}
		out = append(out, text[:i])
		text = text[i:]
	}
	return out
}

func fits(s string, limit int) bool { return len([]rune(ToHTML(s))) <= limit }

func trimAll(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

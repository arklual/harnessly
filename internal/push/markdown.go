package push

import (
	"regexp"
	"strings"

	"harnessly/internal/jscompat"
)

var (
	mdFence     = regexp.MustCompile("(?s)```.*?```")
	mdCode      = regexp.MustCompile("`([^`]+)`")
	mdImage     = regexp.MustCompile(`!\[.*?\]\(.*?\)`)
	mdLink      = regexp.MustCompile(`\[([^\]]+)\]\(.*?\)`)
	mdHeading   = regexp.MustCompile(`(?m)^#{1,6}` + jscompat.SpaceClass + `+`)
	mdStrike    = regexp.MustCompile(`~~(.*?)~~`)
	mdQuote     = regexp.MustCompile(`(?m)^[>` + jscompat.SpaceClass[1:len(jscompat.SpaceClass)-1] + `]*>`)
	mdBullet    = regexp.MustCompile(`(?m)^[-*+]` + jscompat.SpaceClass + `+`)
	mdNumbered  = regexp.MustCompile(`(?m)^\d+\.` + jscompat.SpaceClass + `+`)
	mdRule      = regexp.MustCompile(`(?m)^---+$`)
	mdParagraph = regexp.MustCompile(`\n{2,}`)
)

// pairs replaces d…d with what is between, for each delimiter in order at every position, the way the regular
// expression `(d1|d2)(.*?)\1` does (no line break inside).
func pairs(s string, delims ...string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		matched := false
		for _, d := range delims {
			if !strings.HasPrefix(s[i:], d) {
				continue
			}
			rest := s[i+len(d):]
			end := strings.Index(rest, d)
			if end < 0 || strings.ContainsAny(rest[:end], "\n\r  ") {
				continue
			}
			b.WriteString(rest[:end])
			i += len(d) + end + len(d)
			matched = true
			break
		}
		if !matched {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// StripMarkdown is an answer as plain text for a notification: code blocks dropped, links and emphasis unwrapped,
// headings, quotes and list marks removed, everything on one line.
func StripMarkdown(text string) string {
	s := mdFence.ReplaceAllString(text, "")
	s = mdCode.ReplaceAllString(s, "$1")
	s = mdImage.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdHeading.ReplaceAllString(s, "")
	s = pairs(s, "**", "__")
	s = pairs(s, "*", "_")
	s = mdStrike.ReplaceAllString(s, "$1")
	s = mdQuote.ReplaceAllString(s, "")
	s = mdBullet.ReplaceAllString(s, "")
	s = mdNumbered.ReplaceAllString(s, "")
	s = mdRule.ReplaceAllString(s, "")
	s = mdParagraph.ReplaceAllString(s, " ")
	return jscompat.Trim(spaces.ReplaceAllString(s, " "))
}

package connections

import (
	"html"
	"regexp"
	"strings"
)

var (
	htmlBlock  = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6]|/tr|/pre|/blockquote)\b[^>]*>`)
	htmlItem   = regexp.MustCompile(`(?i)<\s*li\b[^>]*>`)
	htmlDrop   = regexp.MustCompile(`(?is)<\s*(script|style)\b.*?</\s*(script|style)\s*>`)
	htmlTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	blankLines = regexp.MustCompile(`\n{3,}`)
	spaceRuns  = regexp.MustCompile(`[ \t\x{00a0}]+`)
)

// HTMLText is the readable text of an HTML fragment: paragraphs and list items on their own lines, tags dropped,
// entities decoded. Good enough for a description or a page body an agent reads; not a renderer.
func HTMLText(s string) string {
	s = htmlDrop.ReplaceAllString(s, "")
	s = htmlItem.ReplaceAllString(s, "\n- ")
	s = htmlBlock.ReplaceAllString(s, "\n")
	s = htmlTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRuns.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	return strings.TrimSpace(blankLines.ReplaceAllString(s, "\n\n"))
}

// Clip cuts s to at most n runes, with an ellipsis when it cut.
func Clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

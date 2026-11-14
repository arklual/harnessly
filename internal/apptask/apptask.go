// Package apptask is the envelope of a task an app gives an agent in the person's name: an inbox that hands
// a tracker card to a new chat. The agent reads who set the task; the feed draws it as the app's card, not as words the
// person typed.
//
//	<app-task app="inbox" label="Inbox">
//	…the task…
//	</app-task>
package apptask

import (
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Tag is the envelope's element.
const Tag = "app-task"

// tagRE finds the envelope's own tag inside the task text.
var tagRE = regexp.MustCompile(`<(/?)` + Tag)

func attr(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t' || r == '\u0085' || r == ' ' || r == ' ':
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	return html.EscapeString(strings.TrimSpace(b.String()))
}

// Wrap is the task as the agent gets it. The attributes are escaped; a tag of the envelope inside the text gets a
// zero-width space, so the text cannot end its own envelope. A nil from leaves the text as it is.
func Wrap(from *wire.ChatFrom, text string) string {
	from = from.Clean()
	if from == nil {
		return text
	}
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(text))
	text = tagRE.ReplaceAllString(text, "<${1}"+Tag+"\u200b")
	return "<" + Tag + ` app="` + attr(from.App) + `" label="` + attr(from.Label) + `">` + "\n" + text + "\n</" + Tag + ">"
}

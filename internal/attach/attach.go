// Package attach writes the places a message points at (wire.MessageRef: a design, a document, a board, a file of the
// project, a merge request, a task) into the words the agent gets: one block under the person's text, a line per place,
// each led by a tag the page reads back into a chip. The block is plain enough for the agent to read and stable enough
// for the page to strip: `<attached>`, a header, `<ref kind=".." id=".." title=".."/> line`, `</attached>`.
package attach

import (
	"os"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	// Open and Close bound the block.
	Open  = "<attached>"
	Close = "</attached>"
)

// Item is a place and the line the agent reads about it.
type Item struct {
	Ref  wire.MessageRef
	Line string
}

var (
	controls = regexp.MustCompile(`[\x00-\x1f\x7f-\x9f\x{2028}\x{2029}]+`)
	spaces   = regexp.MustCompile(`\s+`)
	// tags: the words of a place cannot close the block or open another line of it.
	tags = regexp.MustCompile(`(?i)<(/?)(attached|ref)\b`)
)

// OneLine is s on one line, without control characters and without the block's own tags.
func OneLine(s string) string {
	s = spaces.ReplaceAllString(controls.ReplaceAllString(s, " "), " ")
	return strings.TrimSpace(tags.ReplaceAllString(s, `<\$1$2`))
}

func attr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(OneLine(s))
}

// Tag is the line's lead the page reads the chip from.
func Tag(r wire.MessageRef) string {
	return `<ref kind="` + string(r.Kind) + `" id="` + attr(r.ID) + `" title="` + attr(r.Title) + `"/>`
}

// Block is the places as the agent reads them; "" for none.
func Block(items []Item) string {
	if len(items) == 0 {
		return ""
	}
	lines := []string{Open, locale.Pick("Attached to the message:", "Прикреплено к сообщению:")}
	for _, it := range items {
		lines = append(lines, Tag(it.Ref)+" "+OneLine(it.Line))
	}
	return strings.Join(append(lines, Close), "\n")
}

// With is the person's words with the block under them.
func With(text, block string) string {
	text = strings.TrimRight(text, " \t\r\n")
	switch {
	case block == "":
		return text
	case text == "":
		return block
	}
	return text + "\n\n" + block
}

func quoted(title string) string {
	return locale.Pick(`"`+OneLine(title)+`"`, "«"+OneLine(title)+"»")
}

// LineOf is the line of a place the runtime needs nothing more to describe: everything but a design.
func LineOf(r wire.MessageRef) string {
	target := OneLine(r.Target)
	if target == "" {
		target = OneLine(r.ID)
	}
	switch r.Kind {
	case wire.RefDoc:
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			return locale.Pick("Document ", "Документ ") + quoted(r.Title) + " — " + target +
				locale.Pick(" (read it before you answer).", " (прочитай его, прежде чем отвечать).")
		}
		return locale.Pick("Project document ", "Документ проекта ") + quoted(r.Title) + " — " + target
	case wire.RefBoard:
		// The boards CLI of the deployment (RUNTIME_BOARDS_CLI) reads a board as text; without one the link is all.
		line := locale.Pick("Board ", "Доска ") + quoted(r.Title) + " — " + target
		if cli := strings.TrimSpace(os.Getenv("RUNTIME_BOARDS_CLI")); cli != "" {
			return line + locale.Pick(" (read it as text: `"+cli+" read <link>`).", " (текстом: `"+cli+" read <ссылка>`).")
		}
		return line
	case wire.RefFile:
		return locale.Pick("Project file — ", "Файл проекта — ") + target
	case wire.RefMR:
		return locale.Pick("Merge request ", "Merge request ") + quoted(r.Title) + " — " + target
	case wire.RefTask:
		return locale.Pick("Task ", "Задача ") + quoted(r.Title) + " — " + target
	}
	return quoted(r.Title) + " — " + target
}

// blockAtEnd is the block as With leaves it: at the very end of the text, after a blank line or alone.
var blockAtEnd = regexp.MustCompile(`(?:^|\n\n)<attached>\n(?s:.*?)\n</attached>\s*$`)

// Strip is the person's words without the block: what a title or a summary is made of.
func Strip(text string) string {
	return strings.TrimRight(blockAtEnd.ReplaceAllString(text, ""), " \t\r\n")
}

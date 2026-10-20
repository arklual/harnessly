package timeline

import (
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/screen"
)

// The reply Claude has printed in its terminal but not yet written to the transcript: Claude Code records a message
// only once the whole API message is done, so a long tool call after a paragraph keeps that paragraph off the timeline
// while the person reads it in the terminal. Read from the screen:
//
//	❯ the prompt            ← the last prompt the agent took
//	● text of the reply      ← a reply block; its next lines are indented by two spaces
//	⎿ …                      ← a tool call's result lines
//	✻ Effecting… (42s)       ← the spinner
//	──────────               ← the prompt box
//
// Only the text after the last tool call counts: what came before it is in the transcript already.

var (
	livePromptRE   = regexp.MustCompile(`^❯[ \x{a0}]`)
	liveBoxRuleRE  = regexp.MustCompile(`^─{8,}`)
	liveBlockRE    = regexp.MustCompile(`^●[ \x{a0}]`)
	liveToolRE     = regexp.MustCompile(`^●[ \x{a0}][A-Za-z][\w.:-]*\(`)
	liveSpinnerRE  = regexp.MustCompile(`^[·✢✳✶✻✽*][ \x{a0}]` + nonSpace)
	liveResultRE   = regexp.MustCompile(`^` + jscompat.SpaceClass + `+⎿`)
	liveListRE     = regexp.MustCompile(`^(?:[-*•]|\d+[.)])` + jscompat.SpaceClass)
	liveHeadingRE  = regexp.MustCompile(`^#{1,6}` + jscompat.SpaceClass)
	liveRightRE    = regexp.MustCompile(`^ {24,}` + nonSpace)
	liveIndentRE   = regexp.MustCompile(`^ {2}`)
	liveTrailingRE = regexp.MustCompile(` {8,}` + nonSpace + `[^\n\r\x{2028}\x{2029}]*$`)
	liveBlanksRE   = regexp.MustCompile(`\n{3,}`)
)

var nonSpace = `[^` + jscompat.SpaceClass[1:len(jscompat.SpaceClass)-1] + `]`

const maxLiveChars = 4000

type liveBlock struct {
	tool  bool
	lines []string
}

func trimEndJS(s string) string   { return strings.TrimRightFunc(s, jscompat.IsSpace) }
func trimStartJS(s string) string { return strings.TrimLeftFunc(s, jscompat.IsSpace) }

// unwrap joins the hard-wrapped lines of a paragraph; list items and blank lines keep their own line.
func unwrap(lines []string) string {
	var out []string
	for _, line := range lines {
		text := trimEndJS(liveTrailingRE.ReplaceAllString(liveIndentRE.ReplaceAllString(line, ""), ""))
		last := len(out) - 1
		switch {
		case text == "":
			out = append(out, "")
		case last < 0 || out[last] == "" || liveListRE.MatchString(text) || liveHeadingRE.MatchString(text):
			out = append(out, text)
		default:
			out[last] = out[last] + " " + trimStartJS(text)
		}
	}
	return jscompat.Trim(liveBlanksRE.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
}

// readLiveReply is the reply text after the last tool call of the last prompt on screen, "" when there is none.
func readLiveReply(scr string) string {
	lines := strings.Split(strings.ReplaceAll(screen.StripANSI(scr), "\r", ""), "\n")
	end := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if !liveBoxRuleRE.MatchString(lines[i]) {
			continue
		}
		found := false
		for j := i + 1; j < i+3 && j < len(lines); j++ {
			if livePromptRE.MatchString(lines[j]) {
				found = true
			}
		}
		if found {
			end = i
			break
		}
	}
	start := -1
	for i := end - 1; i >= 0; i-- {
		if livePromptRE.MatchString(lines[i]) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var blocks []*liveBlock
	for i := start + 1; i < end; i++ {
		line := lines[i]
		if liveBlockRE.MatchString(line) {
			blocks = append(blocks, &liveBlock{tool: liveToolRE.MatchString(line), lines: []string{liveBlockRE.ReplaceAllString(line, "")}})
			continue
		}
		if liveSpinnerRE.MatchString(line) || liveRightRE.MatchString(line) {
			continue
		}
		if len(blocks) == 0 {
			continue
		}
		b := blocks[len(blocks)-1]
		if liveResultRE.MatchString(line) {
			b.tool = true
		} else if strings.HasPrefix(line, "  ") || jscompat.Trim(line) == "" {
			b.lines = append(b.lines, line)
		}
	}
	first := len(blocks)
	for first > 0 && !blocks[first-1].tool {
		first--
	}
	var parts []string
	for _, b := range blocks[first:] {
		if t := unwrap(b.lines); t != "" {
			parts = append(parts, t)
		}
	}
	text := strings.Join(parts, "\n\n")
	if jscompat.Len(text) > maxLiveChars {
		return jscompat.Slice(text, maxLiveChars) + "…"
	}
	return text
}

package screen

import (
	"strconv"
	"strings"

	"harnessly/internal/jscompat"
)

// Options is a prompt's choices as drawn on screen: labels in screen order, the focused one, and the text above them.
// Title is set only when the block above starts with a checkbox heading (AskUserQuestion's ☐ tab); Request is the
// paragraph right above the options. Both are "" when absent.
type Options struct {
	Options      []string `json:"options"`
	FocusedIndex int      `json:"focusedIndex"`
	Title        string   `json:"title,omitempty"`
	Request      string   `json:"request,omitempty"`
}

// optionKeywords start the labels of unnumbered prompts (and of Claude's ink select dialogs).
var optionKeywords = []string{
	"Yes", "Yes,", "No",
	"Accept", "Decline",
	"Open System Settings", "Try again",
	"Use this", "Continue without",
	"Resume from summary", "Resume full session",
	"Continue this conversation", "Send message as",
	"Don't ask me again",
}

var (
	indicatorRE    = re(`^` + sp + `*(?:[❯›>]` + sp + `+)?(` + dot + `+)$`)
	focusedRE      = re(`^` + sp + `*[❯›>]` + sp + `+`)
	numberPrefixRE = re(`^(\d+)\.` + sp + `+`)
	// A narrow pane renders "2. Yes" as "2Yes": the period and the space are optional.
	numberedLineRE = re(`^` + sp + `*([❯›>])?` + sp + `*(?:[↓↑]` + sp + `*)?(\d+)\.?` + sp + `*(` + nsp + dot + `*)$`)
	checkboxRE     = re(`^[☐☑]` + sp + `*`)
	titleLineRE    = re(`^` + sp + `*[☐☑]` + sp + `*` + nsp)
	// "Yes, and don't ask: <cmd>" lost its "again for" in a damaged capture.
	damagedDontAskRE = re(`^(Yes,` + sp + `*and` + sp + `+don[\x{2019}']?t` + sp + `+ask)` + sp + `*:` + sp + `*(` + dot + `+)$`)
)

// StripNumberPrefix drops a label's "1. ".
func StripNumberPrefix(label string) string { return numberPrefixRE.ReplaceAllString(label, "") }

// OptionNumber is the number a label starts with ("2" for "2. No"); false when it has none.
func OptionNumber(label string) (string, bool) {
	m := numberPrefixRE.FindStringSubmatch(label)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func hasOption(options []string, prefix string) bool {
	for _, o := range options {
		if strings.HasPrefix(StripNumberPrefix(o), prefix) {
			return true
		}
	}
	return false
}

// separatorLine: a line made only of whitespace, rules and dashes.
func separatorLine(line string) bool {
	if line == "" {
		return false
	}
	for _, r := range line {
		if !isSpace(r) && r != '─' && r != '━' && r != '-' {
			return false
		}
	}
	return true
}

func cleanPromptLine(line string) string { return trim(checkboxRE.ReplaceAllString(trim(line), "")) }

// startsWord: s starts with word followed by something other than a comma or whitespace ("Yescurrent status").
func startsWord(s, word string) bool {
	rest, ok := strings.CutPrefix(s, word)
	if !ok || rest == "" {
		return false
	}
	r := []rune(rest)[0]
	return r != ',' && !isSpace(r)
}

// normalizeOption repairs what a damaged capture did to a label: another UI region glued to "Yes"/"No", or the lost
// "again for" of "Yes, and don't ask again for: <cmd>" (a canonical label is left alone, its quote included).
func normalizeOption(text string) string {
	if m := damagedDontAskRE.FindStringSubmatch(text); m != nil {
		return m[1] + " again for: " + trim(m[2])
	}
	if startsWord(text, "Yes") {
		return "Yes"
	}
	if startsWord(text, "No") {
		return "No"
	}
	return text
}

func isKnownPromptPattern(options []string) bool {
	if len(options) < 2 {
		return false
	}
	has := func(p string) bool { return hasOption(options, p) }
	return (has("Yes") && has("No")) ||
		(has("Accept") && has("Decline")) ||
		has("Open System Settings") ||
		(has("Use this") && has("Continue without")) ||
		(has("Resume from summary") && has("Resume full session")) ||
		(has("Continue this conversation") && has("Send message as"))
}

// withPromptText fills Request (the paragraph right above the options) and Title (the paragraph above that, when it
// is a checkbox heading).
func withPromptText(parsed Options, lines []string, optionStart int) Options {
	if optionStart <= 0 {
		return parsed
	}
	gap := func(i int) bool { return blank(lines[i]) || separatorLine(lines[i]) }
	cursor := optionStart - 1
	for cursor >= 0 && gap(cursor) {
		cursor--
	}
	var request []string
	for cursor >= 0 && !gap(cursor) {
		request = append([]string{cleanPromptLine(lines[cursor])}, request...)
		cursor--
	}
	for cursor >= 0 && gap(cursor) {
		cursor--
	}
	var title []string
	heading := false
	for cursor >= 0 && !gap(cursor) {
		title = append([]string{cleanPromptLine(lines[cursor])}, title...)
		heading = heading || titleLineRE.MatchString(lines[cursor])
		cursor--
	}
	parsed.Request = trim(strings.Join(request, "\n"))
	if heading {
		parsed.Title = trim(strings.Join(title, "\n"))
	}
	return parsed
}

func number(digits string) float64 {
	f, _ := strconv.ParseFloat(digits, 64)
	return f
}

// parseNumberedOptions reads "1. … 2. …" blocks numbered from 1 without gaps and keeps the last one: scrollback may
// still hold an earlier prompt. Blank lines between options (a damaged capture) do not end a block; a line indented
// deeper than its option continues the soft-wrapped label.
func parseNumberedOptions(lines []string) Options {
	type block struct {
		raw     []string
		focused int
		start   int
	}
	var blocks []block
	var raw []string
	focused, expected, lastIndent, start := 0, 1, 0, -1
	flush := func() {
		if len(raw) >= 2 {
			blocks = append(blocks, block{raw: raw, focused: focused, start: start})
		}
		raw = nil
		focused, expected, lastIndent, start = 0, 1, 0, -1
	}

	for i, line := range lines {
		if blank(line) {
			continue
		}
		if m := numberedLineRE.FindStringSubmatch(line); m != nil {
			marker, num, rest := m[1], number(m[2]), trim(m[3])
			if rest != "" {
				if num == 1 {
					flush()
					raw = append(raw, rest)
					if marker != "" {
						focused = 0
					}
					lastIndent, start, expected = leadingSpaces(line), i, 2
					continue
				}
				if num == float64(expected) {
					if marker != "" {
						focused = len(raw)
					}
					raw = append(raw, rest)
					lastIndent = leadingSpaces(line)
					expected++
					continue
				}
			}
		}
		if len(raw) > 0 {
			if leadingSpaces(line) > lastIndent {
				raw[len(raw)-1] += trimStart(line)
				continue
			}
			if indentedText(line) {
				continue
			}
			flush()
		}
	}
	flush()

	if len(blocks) == 0 {
		return Options{Options: []string{}}
	}
	best := blocks[len(blocks)-1]
	options := make([]string, len(best.raw))
	for i, r := range best.raw {
		options[i] = strconv.Itoa(i+1) + ". " + normalizeOption(r)
	}
	return withPromptText(Options{Options: options, FocusedIndex: best.focused}, lines, best.start)
}

// parseKeywordOptions reads the first run of unnumbered option lines (focused or indented) whose labels start with a
// known keyword.
func parseKeywordOptions(lines []string) Options {
	options := []string{}
	focused, start, found := 0, -1, false
	for i, line := range lines {
		if blank(line) {
			if found {
				break
			}
			continue
		}
		isFocused := focusedRE.MatchString(line)
		if !isFocused && !indentedText(line) {
			if found {
				break
			}
			continue
		}
		m := indicatorRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		label := trim(m[1])
		stripped := StripNumberPrefix(label)
		for _, kw := range optionKeywords {
			if strings.HasPrefix(stripped, kw) {
				if len(options) == 0 {
					start = i
				}
				if isFocused {
					focused = len(options)
				}
				options = append(options, label)
				found = true
				break
			}
		}
	}
	return withPromptText(Options{Options: options, FocusedIndex: focused}, lines, start)
}

// ParseChoiceOptions reads the last numbered choice block of a pane that has a focus marker (❯, › or >) and at least
// two options: AskUserQuestion, model pickers, Codex menus. Numbers are kept as drawn (a scrolled list skips some); a
// number not above the previous one starts a new block. Nothing found gives no options and focus 0.
func ParseChoiceOptions(pane string) Options {
	lines := strings.Split(pane, "\n")
	type block struct {
		options []string
		focused int
		start   int
	}
	var blocks []block
	var options []string
	focused, start := -1, -1
	last := 0.0
	flush := func() {
		if len(options) >= 2 && focused >= 0 {
			blocks = append(blocks, block{options: options, focused: focused, start: start})
		}
		options = nil
		focused, start, last = -1, -1, 0
	}

	for i, line := range lines {
		if blank(line) || separatorLine(line) {
			continue
		}
		if m := numberedLineRE.FindStringSubmatch(line); m != nil {
			marker, num, rest := m[1], number(m[2]), trim(m[3])
			if len(options) > 0 && num <= last {
				flush()
			}
			if rest != "" {
				if len(options) == 0 {
					start = i
				}
				if marker != "" {
					focused = len(options)
				}
				options = append(options, jscompat.Number(num)+". "+normalizeOption(rest))
				last = num
				continue
			}
		}
		if len(options) > 0 && !indentedText(line) {
			flush()
		}
	}
	flush()

	if len(blocks) == 0 {
		return Options{Options: []string{}}
	}
	best := blocks[len(blocks)-1]
	return withPromptText(Options{Options: best.options, FocusedIndex: best.focused}, lines, best.start)
}

// ParsePermissionOptions reads a permission prompt: numbered options first, unnumbered keyword options second, and
// only when they form a known pattern (Yes/No, Accept/Decline, Open System Settings, Use this/Continue without, the
// resume and idle-return dialogs). Anything else gives no options and focus 0.
func ParsePermissionOptions(pane string) Options {
	lines := strings.Split(pane, "\n")
	if numbered := parseNumberedOptions(lines); len(numbered.Options) >= 2 && isKnownPromptPattern(numbered.Options) {
		return numbered
	}
	if keyword := parseKeywordOptions(lines); isKnownPromptPattern(keyword.Options) {
		return keyword
	}
	return Options{Options: []string{}}
}

// HasPermissionPrompt: the pane shows a permission prompt.
func HasPermissionPrompt(pane string) bool { return len(ParsePermissionOptions(pane).Options) > 0 }

// PromptOptions is what the permission-options route answers for a pane.
type PromptOptions struct {
	Options
	// BypassPrompt: the pane is Claude's "Bypass Permissions" warning.
	BypassPrompt bool `json:"isBypassPrompt,omitempty"`
}

// ReadPromptOptions is a permission prompt when there is one, otherwise any choice menu.
func ReadPromptOptions(pane string) PromptOptions {
	parsed := ParsePermissionOptions(pane)
	if len(parsed.Options) == 0 {
		parsed = ParseChoiceOptions(pane)
	}
	return PromptOptions{Options: parsed, BypassPrompt: strings.Contains(pane, "Bypass Permissions")}
}

var (
	planIndicatorRE = re(`^` + sp + `*(?:[❯›>]` + sp + `+)?(?:\d+\.` + sp + `+)?(` + dot + `+)$`)
	planMarkerRE    = re(`^` + sp + `*[❯›>]`)
	planKeywords    = []string{"Yes,", "Yes ", "No,", "No ", "Tell Claude"}
)

func isPlanOption(label string) bool {
	for _, kw := range planKeywords {
		if strings.HasPrefix(label, kw) {
			return true
		}
	}
	return false
}

// ParsePlanOptions reads the options of Claude's plan approval prompt: the first run of option lines (indented or
// marked) whose labels start with "Yes", "No" or "Tell Claude", number prefixes dropped.
func ParsePlanOptions(pane string) []string {
	options := []string{}
	found := false
	for _, line := range strings.Split(pane, "\n") {
		if blank(line) {
			if found {
				break
			}
			continue
		}
		if !indentedText(line) && !planMarkerRE.MatchString(line) {
			if found {
				break
			}
			continue
		}
		m := planIndicatorRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		label := trim(m[1])
		if isPlanOption(label) {
			options = append(options, label)
			found = true
		} else if found {
			break
		}
	}
	return options
}

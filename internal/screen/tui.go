package screen

import (
	"strings"
	"unicode/utf8"
)

const (
	busyTailLines  = 12
	menuScanLines  = 25
	panelHintLines = 20
	panelLines     = 30
	headingLines   = 4
)

var (
	escToInterruptRE = re(`(?i)esc to interrupt`)
	// Claude: "✽ Contemplating… (2s · ↓ 20 tokens)" while working, "✻ Baked for 5s" (no ellipsis) when done.
	claudeSpinnerRE = re(`^[·✢✳✶✻✽*]` + sp + `+` + nsp + `+…`)
)

func busyLine(line string) bool {
	return escToInterruptRE.MatchString(line) || claudeSpinnerRE.MatchString(line)
}

// IsBusy: the bottom of a plain screen says the agent is working — Claude's spinner line, or "esc to interrupt"
// (Codex "Working (3s • esc to interrupt)", older Claude). While Claude streams text the spinner may be off screen,
// so callers also look at the hook-driven state.
func IsBusy(plain string) bool {
	for _, line := range tail(trimEndLines(plain), busyTailLines) {
		if busyLine(line) {
			return true
		}
	}
	return false
}

// IsBusyAnywhere is IsBusy over the whole screen: a long todo list pushes the spinner above the last lines.
func IsBusyAnywhere(plain string) bool {
	for _, line := range trimEndLines(plain) {
		if busyLine(line) {
			return true
		}
	}
	return false
}

// Menu is a numbered choice block with a focus marker near the bottom of the screen, with its title and question.
type Menu struct {
	Title        *string  `json:"title"`
	Request      *string  `json:"request"`
	Options      []string `json:"options"`
	FocusedIndex int      `json:"focusedIndex"`
}

var (
	menuSeparatorRE = re(`^` + sp + `*[▔─━]+` + sp + `*$`)
	boxEdgeRE       = re(`^` + sp + `*[│╰╭╮╯┃]`)
	optionNumberRE  = re(`^\d+\.` + sp + `*`)
	yesOptionRE     = re(`^1\.` + sp + `*Yes`)
)

// menuHeading is the lines just above the first option, up to a rule, a box edge or unindented text: the menu's title
// and question. Claude puts blank lines between them, Codex does not.
func menuHeading(lines []string, firstOption string) []string {
	label := optionNumberRE.ReplaceAllString(firstOption, "")
	start := -1
	for i, line := range lines {
		if strings.ContainsAny(line, "0123456789") && strings.Contains(line, label) {
			start = i
			break
		}
	}
	var heading []string
	for i := start - 1; i >= 0 && len(heading) < headingLines; i-- {
		line := lines[i]
		if blank(line) {
			continue
		}
		if menuSeparatorRE.MatchString(line) || boxEdgeRE.MatchString(line) || !startsWithSpace(line) {
			break
		}
		heading = append([]string{trim(line)}, heading...)
	}
	return heading
}

// FindMenu is the choice block in the last 25 lines of a plain screen; nil when there is none.
func FindMenu(plain string) *Menu {
	lines := tail(trimEndLines(plain), menuScanLines)
	parsed := ParseChoiceOptions(strings.Join(lines, "\n"))
	if len(parsed.Options) < 2 {
		return nil
	}
	var heading []string
	if parsed.Title != "" {
		for _, l := range strings.Split(parsed.Title, "\n") {
			if l != "" {
				heading = append(heading, l)
			}
		}
	} else {
		heading = menuHeading(lines, parsed.Options[0])
	}
	menu := &Menu{Options: parsed.Options, FocusedIndex: parsed.FocusedIndex}
	if len(heading) > 0 {
		menu.Title = &heading[0]
	}
	if len(heading) > 1 {
		request := strings.Join(heading[1:], "\n")
		menu.Request = &request
	}
	return menu
}

// YesFocused: the focus is on the first option and it reads "1. Yes…" — Claude's "Switch model?" asked with its
// default answer.
func (m *Menu) YesFocused() bool {
	return m != nil && m.FocusedIndex == 0 && len(m.Options) > 0 && yesOptionRE.MatchString(m.Options[0])
}

// Footers of TUI panels and pickers. Case matters: Claude's "(shift+tab to cycle)" and "esc to interrupt" are no panels.
var panelHintRE = re(`Esc to (?:cancel|close|exit|go back|dismiss)|esc to (?:close|go back|cancel|exit|dismiss)|Enter to (?:select|confirm|set|continue)|enter to (?:select|confirm|continue)|↑/↓|←/→|Tab to (?:switch|cycle)`)

// CountPanelHints counts the panel footer lines among the last 20 lines of a plain screen.
func CountPanelHints(plain string) int {
	n := 0
	for _, line := range tail(trimEndLines(plain), panelHintLines) {
		if panelHintRE.MatchString(line) {
			n++
		}
	}
	return n
}

var (
	panelTopRE  = re(`^` + sp + `*▔{8,}` + sp + `*$`)
	panelRuleRE = re(`^(?:[▔─━]|` + sp + `)+$`)
)

// PanelSnapshot is the panel part of a plain screen: below Claude's ▔▔▔ top rule when there is one (the history above
// it is not the panel), without blank lines and rules, at most the last 30 lines.
func PanelSnapshot(plain string) string {
	lines := trimEndLines(plain)
	top := lastIndex(lines, panelTopRE.MatchString)
	var kept []string
	for _, line := range lines[top+1:] {
		if !blank(line) && !panelRuleRE.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.Join(tail(kept, panelLines), "\n")
}

// sgrRE is one SGR sequence; parameter 2 is "dim", which both CLIs draw their input placeholder with.
var sgrRE = re(`^\x1b\[([0-9;]*)m`)

func hasParam(params, want string) bool {
	for _, p := range strings.Split(params, ";") {
		if p == want {
			return true
		}
	}
	return false
}

// inputMarker: the line starts with ❯ (Claude) or › (Codex) followed by a space, a no-break space, an escape or
// nothing; the rest of the line comes back.
func inputMarker(line string, escapeEnds bool) (string, bool) {
	r, size := utf8.DecodeRuneInString(line)
	if r != '❯' && r != '›' {
		return "", false
	}
	rest := line[size:]
	if rest == "" {
		return rest, true
	}
	next, _ := utf8.DecodeRuneInString(rest)
	return rest, next == ' ' || next == ' ' || (escapeEnds && next == '\x1b')
}

// ReadInputLine reads the text typed into the TUI's own input line from a styled capture: the last line that starts
// at column 0 with ❯ or › (leading SGR resets aside). An input showing its dim placeholder reads "". ok is false when
// no input line is on screen. Only the input line itself counts: a wrapped draft is judged by its first line.
func ReadInputLine(styled string) (text string, ok bool) {
	lines := trimEndLines(styled)
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		// Leading attribute changes belong to the previous line's styling.
		for {
			m := sgrRE.FindStringIndex(line)
			if m == nil {
				break
			}
			line = line[m[1]:]
		}
		rest, found := inputMarker(line, true)
		if !found {
			continue
		}
		dim := false
		for {
			if m := sgrRE.FindStringSubmatch(rest); m != nil {
				switch {
				case hasParam(m[1], "2"):
					dim = true
				case m[1] == "" || hasParam(m[1], "0") || hasParam(m[1], "22"):
					dim = false
				}
				rest = rest[len(m[0]):]
				continue
			}
			if strings.HasPrefix(rest, " ") {
				rest = rest[1:]
				continue
			}
			if strings.HasPrefix(rest, " ") {
				rest = rest[len(" "):]
				continue
			}
			break
		}
		if dim {
			return "", true
		}
		return trim(strings.ReplaceAll(StripANSI(rest), " ", " ")), true
	}
	return "", false
}

// State is one look at a styled capture.
type State struct {
	// Plain is the capture without escape sequences.
	Plain string
	Busy  bool
	Menu  *Menu
	// Panel: a panel or picker footer is among the last lines.
	Panel bool
	// Input is the typed text of the input line ("" when empty or when there is no input line); InputLine says
	// whether an input line is on screen at all.
	Input     string
	InputLine bool
}

// Read classifies a styled capture.
func Read(styled string) State {
	plain := StripANSI(styled)
	input, ok := ReadInputLine(styled)
	return State{
		Plain:     plain,
		Busy:      IsBusy(plain),
		Menu:      FindMenu(plain),
		Panel:     CountPanelHints(plain) > 0,
		Input:     input,
		InputLine: ok,
	}
}

var (
	launchConfirmRE = re(`Switch model\?|Change effort level\?`)
	modelSetRE      = re(`Set model to\b`)
	// "/effort ultracode" answers "Ultracode on (this session only): …".
	effortSetRE = re(`Set effort level to\b|Ultracode on\b`)
	keptRE      = re(`Kept (?:model|effort)\b`)
)

// IsLaunchConfirm: Claude asks "Switch model?" or "Change effort level?".
func IsLaunchConfirm(plain string) bool { return launchConfirmRE.MatchString(plain) }

// CountModelSet counts Claude's "Set model to" lines; a new one means a /model change was applied.
func CountModelSet(plain string) int { return len(modelSetRE.FindAllStringIndex(plain, -1)) }

// CountEffortSet counts Claude's "Set effort level to" and "Ultracode on" lines.
func CountEffortSet(plain string) int { return len(effortSetRE.FindAllStringIndex(plain, -1)) }

// CountKept counts Claude's "Kept model"/"Kept effort" lines; a new one means the change was declined.
func CountKept(plain string) int { return len(keptRE.FindAllStringIndex(plain, -1)) }

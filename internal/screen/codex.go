package screen

import (
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

const composerTailLines = 8

var codexDisabledInputRE = re(`(?m)^` + sp + `*›?` + sp + `*Input disabled\.`)

// composerLine: a line of Codex's composer, "›" alone or followed by text that is not a numbered option ("› 1. Yes")
// or a checklist item ("› [x] …") of a popup.
func composerLine(line string) bool {
	rest, ok := strings.CutPrefix(trimStart(line), "›")
	if !ok {
		return false
	}
	text := trimStart(rest)
	if text == "" {
		return true
	}
	switch spaces := utf8.RuneCountInString(rest) - utf8.RuneCountInString(text); {
	case spaces == 0:
		return false
	case spaces > 1:
		// Past the first space the text still starts with a space, which is neither a number nor a checkbox.
		return true
	}
	return !numberedOptionStart(text) && !checkboxStart(text)
}

func numberedOptionStart(s string) bool {
	digits := len(s) - len(strings.TrimLeft(s, "0123456789"))
	return digits > 0 && strings.HasPrefix(s[digits:], ".")
}

func checkboxStart(s string) bool {
	return len(s) >= 3 && s[0] == '[' && (s[1] == ' ' || s[1] == 'x' || s[1] == '-') && s[2] == ']'
}

// IsCodexComposerReady: a plain capture shows Codex's composer taking input — a composer line among the last 8
// non-blank-ended lines, no "Input disabled." and no update prompt. Codex reports its session only after the first
// prompt, so this is how a fresh Codex tab is known to be up.
func IsCodexComposerReady(content string) bool {
	if content == "" {
		return false
	}
	if u := MatchCodexUpdatePrompt(content); u != nil && u.Status == CodexUpdatePrompt {
		return false
	}
	if codexDisabledInputRE.MatchString(lineTerminators.Replace(content)) {
		return false
	}
	lines := strings.Split(content, "\n")
	end := len(lines)
	for end > 0 && blank(lines[end-1]) {
		end--
	}
	if end == 0 {
		return false
	}
	for _, line := range lines[max(0, end-composerTailLines):end] {
		for _, part := range strings.Split(lineTerminators.Replace(line), "\n") {
			if composerLine(part) {
				return true
			}
		}
	}
	return false
}

// Codex update screen states.
const (
	CodexUpdatePrompt   = "prompt"
	CodexUpdateUpdating = "updating"
	CodexUpdateSuccess  = "success"
)

// CodexUpdate is Codex's own update screen.
type CodexUpdate struct {
	Status         string  `json:"status"`
	CurrentVersion *string `json:"currentVersion"`
	LatestVersion  *string `json:"latestVersion"`
	UpdateCommand  *string `json:"updateCommand"`
}

var (
	updateSuccessRE  = re(`(?i)Update ran successfully! Please restart Codex\.`)
	updateVersionRE  = re(`(?i)Update available!` + sp + `+([0-9]` + nsp + `*)` + sp + `+->` + sp + `+([0-9]` + nsp + `*)`)
	updateNotesRE    = re(`(?i)Release notes:`)
	updateSkipRE     = re(`(?i)Skip until next version`)
	updatingRE       = re("(?i)Updating Codex via `")
	updateCommandRE  = re("(?i)Update now" + sp + "+\\(runs `([^`]+)`\\)")
	updateWhitespace = re(sp + `+`)
)

// MatchCodexUpdatePrompt reads Codex's update screen from terminal text (wrapped lines are joined first): the prompt
// with both versions and the update command, the update running, or done; nil for anything else.
func MatchCodexUpdatePrompt(text string) *CodexUpdate {
	flat := updateWhitespace.ReplaceAllString(text, " ")
	if updateSuccessRE.MatchString(flat) {
		return &CodexUpdate{Status: CodexUpdateSuccess}
	}
	version := updateVersionRE.FindStringSubmatch(flat)
	choices := updateNotesRE.MatchString(flat) && updateSkipRE.MatchString(flat)
	if version == nil || !choices {
		if updatingRE.MatchString(flat) {
			return &CodexUpdate{Status: CodexUpdateUpdating}
		}
		return nil
	}
	u := &CodexUpdate{Status: CodexUpdatePrompt, CurrentVersion: &version[1], LatestVersion: &version[2]}
	if updatingRE.MatchString(flat) {
		u.Status = CodexUpdateUpdating
	}
	if m := updateCommandRE.FindStringSubmatch(flat); m != nil {
		u.UpdateCommand = &m[1]
	}
	return u
}

const codexProbeChars = 24

var (
	codexInputRE  = re(`^` + sp + `*›` + sp + `?` + dot + `*$`)
	codexBoxStart = re(`^` + sp + `*›` + sp + `?`)
	pastedTokenRE = re(`\[Pasted Content`)
)

// CodexStillInBox: Codex's prompt box (its last › line and the 5 lines under it) still holds the message — its first
// 24 characters, whitespace collapsed, or a "[Pasted Content …]" token. A placeholder or a steer list reads as gone.
func CodexStillInBox(screen, text string) bool {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	index := lastIndex(lines, codexInputRE.MatchString)
	if index < 0 {
		return false
	}
	box := collapse(codexBoxStart.ReplaceAllString(strings.Join(lines[index:min(len(lines), index+6)], " "), ""))
	if pastedTokenRE.MatchString(box) {
		return true
	}
	probe := jscompat.Slice(collapse(text), codexProbeChars)
	return probe != "" && strings.Contains(box, probe)
}

var (
	claudePastedRE = re(`\[Pasted text #\d+`)
	// claudeImageRE: a reference the box wrapped midway too ("[Image" at a line's end, "#104]" next).
	claudeImageRE = re(`\[Image` + sp + `+#\d+\]`)
)

// ClaudeStillInBox: the text on Claude Code's input line (Read's Input, its first line) is this message — a
// "[Pasted text #n …]" token, or the message's start: the line's first 24 characters, image tokens and whitespace
// aside, begin the message, or the message's first 24 characters are on the line — or, for a message too long for the
// box, a piece of it at least 24 characters long. An empty line reads as gone.
func ClaudeStillInBox(input, text string) bool {
	box := collapse(claudeImageRE.ReplaceAllString(input, " "))
	if box == "" {
		return false
	}
	if claudePastedRE.MatchString(box) {
		return true
	}
	msg := collapse(text)
	if msg == "" {
		return false
	}
	// The line starts with the message, or (a wrapped message) the whole line is the message's start, long enough not to
	// be someone's short draft that happens to begin the same way.
	probe := jscompat.Slice(msg, codexProbeChars)
	if strings.HasPrefix(box, probe) {
		return true
	}
	if utf8.RuneCountInString(box) < codexProbeChars {
		return false
	}
	// A long message does not fit the box: Claude shows the rows around the cursor, so the box's first visible row (the
	// one with ❯) starts in the middle of the message. The box is then a piece of the message, compared without white
	// space (the box wraps anywhere and joins its rows with spaces).
	return strings.HasPrefix(msg, box) || strings.Contains(noSpace(msg), noSpace(box))
}

// SameMessage: a user entry of the transcript is this message — the same text, picture references and white space
// aside, or (a message long enough not to be a coincidence) the entry carries it whole, as Claude may wrap it.
func SameMessage(entry, text string) bool {
	e := noSpace(claudeImageRE.ReplaceAllString(entry, " "))
	m := noSpace(claudeImageRE.ReplaceAllString(text, " "))
	if m == "" {
		return false
	}
	return e == m || utf8.RuneCountInString(m) >= codexProbeChars && strings.Contains(e, m)
}

// ClaudeBoxPieceOf: what is left in Claude's box (picture references aside) is a piece of text, however short — the box
// being cleared of a message of ours row by row.
func ClaudeBoxPieceOf(input, text string) bool {
	box := noSpace(claudeImageRE.ReplaceAllString(input, " "))
	return box != "" && strings.Contains(noSpace(claudeImageRE.ReplaceAllString(text, " ")), box)
}

// noSpace drops every white space character.
func noSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if jscompat.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

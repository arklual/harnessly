package screen

import "strings"

const promptBoxTailLines = 8

var (
	// Claude Code draws this footer under its prompt box once the box takes input.
	promptBoxFooterRE = re(`\? for shortcuts|bypass permissions|accept edits|plan mode|shift\+tab`)
	// The launch line the runtime types: "claude … --settings <hooks.json> …" or "claude --resume <id> …".
	launchLineRE = re(`--settings` + sp + `|claude` + sp + `+--resume\b|--dangerously-skip-permissions`)
)

// IsPromptBoxReady: Claude's prompt box footer is among the last 8 non-empty lines of a plain screen (notices and the
// agents list may sit below it).
func IsPromptBoxReady(pane string) bool {
	return promptBoxFooterRE.MatchString(strings.Join(tail(trimmedNonEmpty(pane), promptBoxTailLines), " "))
}

// IsLaunchedPromptBoxReady is IsPromptBoxReady for an agent that was just launched in a pane that may still show the
// footer of the agent that ran there before: the footer counts only when it comes after the last launch line.
func IsLaunchedPromptBoxReady(pane string) bool {
	lines := tail(trimmedNonEmpty(pane), promptBoxTailLines)
	footer := lastIndex(lines, promptBoxFooterRE.MatchString)
	return footer >= 0 && lastIndex(lines, launchLineRE.MatchString) < footer
}

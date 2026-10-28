package screen

import "strings"

// Aside is a screen of Claude Code the person opened in the terminal and left over the input box: a message from the
// chat cannot go in while it is up, and Esc closes it without changing anything.
type Aside string

const (
	NoAside     Aside = ""
	AsideLogin  Aside = "login"  // /login: «Select login method» or «Paste code here if prompted»
	AsideModel  Aside = "model"  // the /model picker
	AsideEffort Aside = "effort" // the /effort slider
)

// asideLines is how far above the footer the dialog's own lines are looked for.
const asideLines = 12

// ClaudeAside reads which aside is up at the bottom of a plain screen. Only screens whose own footer says Esc cancels
// them count, and only these: the trust dialog and the first-run login say «Esc to cancel» too, and there Esc quits
// Claude or does nothing — those are never closed. The footer has to be the screen's last line: with the input box up
// the last lines are the box and its status, so words the agent printed into the transcript never read as a dialog.
func ClaudeAside(plain string) Aside {
	lines := trimmedNonEmpty(plain)
	if len(lines) == 0 {
		return NoAside
	}
	footer := lines[len(lines)-1]
	switch {
	case strings.Contains(footer, "Enter to set as default") && strings.HasSuffix(footer, "Esc to cancel"):
		return AsideModel
	case strings.Contains(footer, "to adjust") && strings.HasSuffix(footer, "Esc to cancel"):
		return AsideEffort
	case footer != "Esc to cancel":
		return NoAside
	}
	for _, text := range tail(lines[:len(lines)-1], asideLines) {
		if strings.HasPrefix(text, "Select login method:") || strings.HasPrefix(text, "Paste code here if prompted") {
			return AsideLogin
		}
	}
	return NoAside
}

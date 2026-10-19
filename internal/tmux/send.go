package tmux

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// Between typed text and its Enter: a TUI that gets both in one burst reads them as a paste with a newline.
	enterAfterText = 50 * time.Millisecond
	// Between the two Enters after a bracketed paste: Claude Code may ask to confirm a long paste.
	secondEnterAfterPaste = 600 * time.Millisecond
	killCheckEvery        = 200 * time.Millisecond

	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

// EscapeMark stands for ESC in text bound for a terminal, so it stays visible but cannot start a sequence.
const EscapeMark = "␛"

// StripControlChars makes text safe to type into a terminal prompt: ESC becomes EscapeMark (one `ESC [ 2 0 1 ~` would end
// a bracketed paste early and the rest would be typed as keys), every other C0 control except tab and newline, DEL and
// the C1 controls are dropped. Bytes that are not UTF-8 become U+FFFD.
func StripControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == 0x1b:
			b.WriteString(EscapeMark)
		case r == '\t' || r == '\n':
			b.WriteRune(r)
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
		default:
			b.WriteRune(r) // an invalid byte decodes to RuneError and is written as U+FFFD
		}
	}
	return b.String()
}

// OneLineSafe is StripControlChars for a value that must stay on one line: runs of white space become one space, and
// the ends are trimmed.
func OneLineSafe(s string) string {
	return strings.TrimSpace(StripControlChars(strings.Join(strings.Fields(s), " ")))
}

// LiteralArg protects a trailing `;`: tmux ends a command at an argument ending in `;` and drops it, even in
// `send-keys -l`; `\;` stands for a literal one.
func LiteralArg(s string) string {
	if strings.HasSuffix(s, ";") {
		return s[:len(s)-1] + `\;`
	}
	return s
}

// sendKeys leaves copy mode (where tmux would swallow the keys) and runs send-keys with args.
func (t *Tmux) sendKeys(name string, args ...string) error {
	t.ExitCopyMode(name)
	return t.sendKeysInPlace(name, args...)
}

// sendKeysInPlace is send-keys without leaving copy mode first, for the second half of a two-part send.
func (t *Tmux) sendKeysInPlace(name string, args ...string) error {
	return t.on(name, append([]string{"send-keys", "-t", name}, args...)...).err
}

// ExitCopyMode leaves copy mode in the session's pane, so typed text reaches the program and not the scrollback.
func (t *Tmux) ExitCopyMode(name string) { t.on(name, "copy-mode", "-q", "-t", name) }

// SendKeys sends a line and Enter in one send-keys call; tmux reads the line as key names where it can (a launch or
// resume line into a shell).
func (t *Tmux) SendKeys(name, line string) error { return t.sendKeys(name, line, "Enter") }

// SendKeysSeparated sends text (as key names where tmux can), then Enter 50 ms later as its own call, so a TUI that
// reads one burst as a paste sees a typed line and a submit.
func (t *Tmux) SendKeysSeparated(name, text string) error {
	if err := t.sendKeys(name, text); err != nil {
		return err
	}
	t.sleep(enterAfterText)
	return t.sendKeysInPlace(name, "Enter")
}

// SendLiteralThenEnter types text literally (a line that looks like a key name, such as `Enter`, is typed as words),
// without control characters, then presses Enter 50 ms later.
func (t *Tmux) SendLiteralThenEnter(name, text string) error {
	if err := t.SendLiteral(name, text); err != nil {
		return err
	}
	t.sleep(enterAfterText)
	return t.sendKeysInPlace(name, "Enter")
}

// SendLiteral types text literally, without control characters, and presses nothing: for an input a TUI shows in place.
func (t *Tmux) SendLiteral(name, text string) error {
	return t.sendKeys(name, "-l", LiteralArg(StripControlChars(text)))
}

// SendRawKeys sends keys as tmux reads them: a key name (`Escape`, `Enter`, `C-g`) or a string. Nothing is stripped.
func (t *Tmux) SendRawKeys(name, keys string) error { return t.sendKeys(name, keys) }

// SendKeySequence sends several keys in one call, so a chord such as `C-x C-s` arrives back to back.
func (t *Tmux) SendKeySequence(name string, keys ...string) error { return t.sendKeys(name, keys...) }

// SendBracketedPasteOnly pastes text into a TUI's input as one bracketed paste and presses nothing.
func (t *Tmux) SendBracketedPasteOnly(name, text string) error {
	return t.sendKeys(name, "-l", pasteStart+StripControlChars(text)+pasteEnd)
}

// SendBracketedPaste pastes text, presses Enter, and Enter again 600 ms later.
func (t *Tmux) SendBracketedPaste(name, text string) error {
	if err := t.SendBracketedPasteOnly(name, text); err != nil {
		return err
	}
	if err := t.sendKeysInPlace(name, "Enter"); err != nil {
		return err
	}
	t.sleep(secondEnterAfterPaste)
	return t.sendKeysInPlace(name, "Enter")
}

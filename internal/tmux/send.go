package tmux

import (
	"fmt"
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

	// literalChunk is the most text bytes one `send-keys -l` carries. tmux refuses a command over its 16 KB
	// client-to-server message with «command too long» (exit 1, nothing typed), so a longer text goes in several calls
	// back to back.
	literalChunk = 8 << 10
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

// sendKeysInPlace is send-keys without leaving copy mode first, for the second half of a two-part send. A refusal
// carries what tmux said (`exit status 1` alone told nothing of a «command too long»).
func (t *Tmux) sendKeysInPlace(name string, args ...string) error {
	r := t.onWrite(name, append([]string{"send-keys", "-t", name}, args...)...)
	if r.timedOut {
		return &SlowError{Command: "send-keys", After: r.took}
	}
	if said := strings.TrimSpace(r.stderr); r.err != nil && said != "" {
		return fmt.Errorf("%w: %s", r.err, said)
	}
	return r.err
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
	return t.sendLiteral(name, "", StripControlChars(text), "")
}

// sendLiteral types prefix+text+suffix literally, in as many `send-keys -l` calls as text needs (see literalChunk):
// prefix goes with the first part and suffix with the last, so an escape sequence of either is never split. `--` keeps
// a part that starts with `-` from being read as an option.
func (t *Tmux) sendLiteral(name, prefix, text, suffix string) error {
	parts := splitLiteral(text, literalChunk)
	parts[0] = prefix + parts[0]
	parts[len(parts)-1] += suffix
	send := t.sendKeys
	for _, part := range parts {
		if err := send(name, "-l", "--", LiteralArg(part)); err != nil {
			return err
		}
		send = t.sendKeysInPlace
	}
	return nil
}

// splitLiteral cuts text into parts of at most limit bytes, never inside a UTF-8 character; an empty text is one empty
// part.
func splitLiteral(text string, limit int) []string {
	var parts []string
	for len(text) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		if cut == 0 {
			cut = limit
		}
		parts = append(parts, text[:cut])
		text = text[cut:]
	}
	return append(parts, text)
}

// SendRawKeys sends keys as tmux reads them: a key name (`Escape`, `Enter`, `C-g`) or a string. Nothing is stripped.
func (t *Tmux) SendRawKeys(name, keys string) error { return t.sendKeys(name, keys) }

// SendKeySequence sends several keys in one call, so a chord such as `C-x C-s` arrives back to back.
func (t *Tmux) SendKeySequence(name string, keys ...string) error { return t.sendKeys(name, keys...) }

// SendBracketedPasteOnly pastes text into a TUI's input as one bracketed paste and presses nothing. A long text goes in
// several send-keys calls inside the one pair of paste brackets: the program reads it as a single paste all the same.
func (t *Tmux) SendBracketedPasteOnly(name, text string) error {
	return t.sendLiteral(name, pasteStart, StripControlChars(text), pasteEnd)
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

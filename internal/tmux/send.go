package tmux

import (
	"context"
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

// ProbeTimeout is how long the server may take to answer before keys are typed into a pane.
const ProbeTimeout = 2 * time.Second

// Responsive asks the session's server a question that costs it nothing and says whether the answer came in
// ProbeTimeout: a *SlowError when it did not. Keys typed into a server this slow arrive apart, later than the waits
// between them, so a writer asks first and types later. A session that is gone is not slow: the write says so itself.
func (t *Tmux) Responsive(name string) error {
	start := time.Now()
	r := t.on(name, "display-message", "-p", "-t", name, "#{pane_id}")
	took := max(r.took, time.Since(start))
	if r.timedOut || took > ProbeTimeout {
		return &SlowError{Command: "display-message", After: took}
	}
	return nil
}

// Keys types into panes for one writer: every call is bound to the writer's context — the hold of the pane's lock —
// so a call made after the hold ended types nothing, and one under way is cut short with it.
type Keys struct {
	t   *Tmux
	ctx context.Context
}

// With is the key sender of a writer whose hold of a pane lives as long as ctx.
func (t *Tmux) With(ctx context.Context) Keys { return Keys{t: t, ctx: ctx} }

// keys is the sender of a caller that names no hold: its calls end by their own timeouts only.
func (t *Tmux) keys() Keys { return Keys{t: t, ctx: context.Background()} }

// ended is why the writer's hold is over, nil while it lasts.
func (k Keys) ended() error {
	if k.ctx.Err() == nil {
		return nil
	}
	return context.Cause(k.ctx)
}

func (k Keys) exec(run runner, timeout time.Duration, name string, args ...string) result {
	if err := k.ended(); err != nil {
		return result{err: err}
	}
	full := append([]string{"-u", "-L", k.t.SocketOf(name)}, args...)
	if k.t.runCtx == nil || k.ctx.Done() == nil {
		return run("", "tmux", full...)
	}
	r := k.t.runCtx(k.ctx, timeout, "", "tmux", full...)
	if err := k.ended(); err != nil {
		// Cut short with the hold: not a slow server, and what tmux said of it is of no use.
		return result{err: err, took: r.took}
	}
	return r
}

func (k Keys) on(name string, args ...string) result {
	return k.exec(k.t.run, cmdTimeout, name, args...)
}

func (k Keys) onWrite(name string, args ...string) result {
	return k.exec(k.t.runWrite, writeTimeout, name, args...)
}

// sleep is the pause between two keys of one send; it ends early with the hold.
func (k Keys) sleep(d time.Duration) {
	if k.ended() == nil {
		k.t.sleep(d)
	}
}

// sendKeys leaves copy mode (where tmux would swallow the keys) and runs send-keys with args.
func (k Keys) sendKeys(name string, args ...string) error {
	k.ExitCopyMode(name)
	return k.sendKeysInPlace(name, args...)
}

// sendKeysInPlace is send-keys without leaving copy mode first, for the second half of a two-part send. A refusal
// carries what tmux said (`exit status 1` alone told nothing of a «command too long»).
func (k Keys) sendKeysInPlace(name string, args ...string) error {
	r := k.onWrite(name, append([]string{"send-keys", "-t", name}, args...)...)
	if r.timedOut {
		return &SlowError{Command: "send-keys", After: r.took}
	}
	if said := strings.TrimSpace(r.stderr); r.err != nil && said != "" {
		return fmt.Errorf("%w: %s", r.err, said)
	}
	return r.err
}

// ExitCopyMode leaves copy mode in the session's pane, so typed text reaches the program and not the scrollback.
func (k Keys) ExitCopyMode(name string) { k.on(name, "copy-mode", "-q", "-t", name) }

// SendKeys sends a line and Enter in one send-keys call; tmux reads the line as key names where it can (a launch or
// resume line into a shell).
func (k Keys) SendKeys(name, line string) error { return k.sendKeys(name, line, "Enter") }

// SendKeysSeparated sends text (as key names where tmux can), then Enter 50 ms later as its own call, so a TUI that
// reads one burst as a paste sees a typed line and a submit.
func (k Keys) SendKeysSeparated(name, text string) error {
	if err := k.sendKeys(name, text); err != nil {
		return err
	}
	k.sleep(enterAfterText)
	return k.sendKeysInPlace(name, "Enter")
}

// SendLiteralThenEnter types text literally (a line that looks like a key name, such as `Enter`, is typed as words),
// without control characters, then presses Enter 50 ms later.
func (k Keys) SendLiteralThenEnter(name, text string) error {
	if err := k.SendLiteral(name, text); err != nil {
		return err
	}
	k.sleep(enterAfterText)
	return k.sendKeysInPlace(name, "Enter")
}

// SendLiteral types text literally, without control characters, and presses nothing: for an input a TUI shows in place.
func (k Keys) SendLiteral(name, text string) error {
	return k.sendLiteral(name, "", StripControlChars(text), "")
}

// sendLiteral types prefix+text+suffix literally, in as many `send-keys -l` calls as text needs (see literalChunk):
// prefix goes with the first part and suffix with the last, so an escape sequence of either is never split. `--` keeps
// a part that starts with `-` from being read as an option.
func (k Keys) sendLiteral(name, prefix, text, suffix string) error {
	parts := splitLiteral(text, literalChunk)
	parts[0] = prefix + parts[0]
	parts[len(parts)-1] += suffix
	send := k.sendKeys
	for _, part := range parts {
		if err := send(name, "-l", "--", LiteralArg(part)); err != nil {
			return err
		}
		send = k.sendKeysInPlace
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
func (k Keys) SendRawKeys(name, keys string) error { return k.sendKeys(name, keys) }

// SendKeySequence sends several keys in one call, so a chord such as `C-x C-s` arrives back to back.
func (k Keys) SendKeySequence(name string, keys ...string) error { return k.sendKeys(name, keys...) }

// SendBracketedPasteOnly pastes text into a TUI's input as one bracketed paste and presses nothing. A long text goes in
// several send-keys calls inside the one pair of paste brackets: the program reads it as a single paste all the same.
func (k Keys) SendBracketedPasteOnly(name, text string) error {
	return k.sendLiteral(name, pasteStart, StripControlChars(text), pasteEnd)
}

// SendBracketedPaste pastes text, presses Enter, and Enter again 600 ms later.
func (k Keys) SendBracketedPaste(name, text string) error {
	if err := k.SendBracketedPasteOnly(name, text); err != nil {
		return err
	}
	if err := k.sendKeysInPlace(name, "Enter"); err != nil {
		return err
	}
	k.sleep(secondEnterAfterPaste)
	return k.sendKeysInPlace(name, "Enter")
}

// The same sends for a caller that names no hold of the pane.

func (t *Tmux) ExitCopyMode(name string)         { t.keys().ExitCopyMode(name) }
func (t *Tmux) SendKeys(name, line string) error { return t.keys().SendKeys(name, line) }
func (t *Tmux) SendKeysSeparated(name, text string) error {
	return t.keys().SendKeysSeparated(name, text)
}
func (t *Tmux) SendLiteralThenEnter(name, text string) error {
	return t.keys().SendLiteralThenEnter(name, text)
}
func (t *Tmux) SendLiteral(name, text string) error { return t.keys().SendLiteral(name, text) }
func (t *Tmux) SendRawKeys(name, keys string) error { return t.keys().SendRawKeys(name, keys) }
func (t *Tmux) SendKeySequence(name string, keys ...string) error {
	return t.keys().SendKeySequence(name, keys...)
}
func (t *Tmux) SendBracketedPasteOnly(name, text string) error {
	return t.keys().SendBracketedPasteOnly(name, text)
}
func (t *Tmux) SendBracketedPaste(name, text string) error {
	return t.keys().SendBracketedPaste(name, text)
}

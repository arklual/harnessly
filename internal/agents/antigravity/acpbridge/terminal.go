package acpbridge

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// terminal draws the chat: the log scrolls above, and the frame stays at the bottom — an input box between two rules
// and a footer naming the model — the layout the runtime's screen reader takes for Antigravity's own (typist): a
// footer with «esc to interrupt» is a turn at work, a box starting «>» is the prompt with what it holds.
type terminal struct {
	out io.Writer
	raw bool

	mu     sync.Mutex
	width  func() int
	input  []rune
	drawn  int // lines of the frame on the screen, the cursor on its last
	pend   strings.Builder
	think  bool
	closed bool

	// the last frame, drawn again after each log
	model, effort string
	busy          bool
	extra         []string
	ask           string
}

func newTerminal(out io.Writer, raw bool) *terminal {
	if out == nil {
		out = io.Discard
	}
	t := &terminal{out: out, raw: raw, width: func() int { return 100 }}
	if raw {
		t.width = termWidth
		_, _ = io.WriteString(out, "\x1b[?2004h") // bracketed paste: a pasted message arrives whole
	}
	return t
}

func (t *terminal) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.flushTextLocked()
	t.eraseLocked()
	if t.raw {
		_, _ = io.WriteString(t.out, "\x1b[?2004l")
	}
	t.closed = true
}

// nl is a line break in raw output, where \n alone does not return the carriage.
func (t *terminal) nl(s string) string {
	if !t.raw {
		return s
	}
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// eraseLocked takes the frame off the screen: the cursor goes to its first line, and the lines below are cleared.
func (t *terminal) eraseLocked() {
	if t.drawn == 0 {
		return
	}
	if t.raw {
		s := "\r"
		if t.drawn > 1 {
			s += "\x1b[" + strconv.Itoa(t.drawn-1) + "A"
		}
		_, _ = io.WriteString(t.out, s+"\x1b[J")
	}
	t.drawn = 0
}

// log puts lines above the frame.
func (t *terminal) log(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		_, _ = io.WriteString(t.out, t.nl(s+"\n"))
		return
	}
	t.eraseLocked()
	_, _ = io.WriteString(t.out, t.nl(s+"\n"))
	t.drawLocked()
}

// text is a chunk of the agent's answer: whole lines are shown as they come, the rest when the answer pauses.
func (t *terminal) text(chunk string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.think = false
	t.pend.WriteString(chunk)
	s := t.pend.String()
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return
	}
	t.pend.Reset()
	t.pend.WriteString(s[i+1:])
	t.eraseLocked()
	_, _ = io.WriteString(t.out, t.nl(s[:i+1]))
	t.drawLocked()
}

func (t *terminal) thinking() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.think = true
}

func (t *terminal) flushText() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pend.Len() == 0 {
		return
	}
	t.eraseLocked()
	t.flushTextLocked()
	t.drawLocked()
}

func (t *terminal) flushTextLocked() {
	if t.pend.Len() == 0 {
		return
	}
	_, _ = io.WriteString(t.out, t.nl(t.pend.String()+"\n"))
	t.pend.Reset()
}

// frame draws the frame anew with these settings.
func (t *terminal) frame(model, effort string, busy bool, extra []string, ask string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.model, t.effort, t.busy, t.extra, t.ask = model, effort, busy, extra, ask
	if t.closed {
		return
	}
	t.eraseLocked()
	t.drawLocked()
}

func (t *terminal) drawLocked() {
	if !t.raw || t.closed {
		return
	}
	w := t.width()
	if w < 20 {
		w = 20
	}
	rule := strings.Repeat("─", w-1)
	var lines []string
	if t.ask != "" {
		lines = append(lines, styleAsk(clip(t.ask, w-1)))
	}
	lines = append(lines, rule)
	lines = append(lines, boxLines(string(t.input), w-1)...)
	lines = append(lines, rule)
	model := t.model
	if model == "" {
		model = "starting"
	}
	footer := "  Antigravity · " + model + " · " + t.effort
	for _, e := range t.extra {
		footer += " · " + e
	}
	// While an approval waits the turn is not drawn at work: the screen reader would take the agent for past it.
	if t.busy && t.ask == "" {
		if t.think {
			footer += " · thinking"
		}
		footer += " · esc to interrupt"
	}
	lines = append(lines, clip(footer, w-1))
	_, _ = io.WriteString(t.out, strings.Join(lines, "\r\n"))
	t.drawn = len(lines)
}

// boxLines are the box's lines: «> » before the first, two spaces before the next, wrapped at spaces where it can.
func boxLines(input string, w int) []string {
	var out []string
	for i, para := range strings.Split(input, "\n") {
		prefix := "  "
		if i == 0 {
			prefix = "> "
		}
		if para == "" {
			out = append(out, strings.TrimRight(prefix, " "))
			continue
		}
		words := strings.Fields(para)
		line := ""
		for _, word := range words {
			for utf8.RuneCountInString(word) > w-2 {
				r := []rune(word)
				if line != "" {
					out = append(out, prefix+line)
					prefix, line = "  ", ""
				}
				out = append(out, prefix+string(r[:w-2]))
				prefix = "  "
				word = string(r[w-2:])
			}
			switch {
			case line == "":
				line = word
			case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= w-2:
				line += " " + word
			default:
				out = append(out, prefix+line)
				prefix, line = "  ", word
			}
		}
		out = append(out, prefix+line)
	}
	return out
}

func clip(s string, w int) string {
	if w <= 0 || utf8.RuneCountInString(s) <= w {
		return s
	}
	return string([]rune(s)[:w-1]) + "…"
}

// --- input ---

// edit changes the box; redraw follows.
func (t *terminal) edit(f func(in []rune) []rune) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.input = f(t.input)
	t.eraseLocked()
	t.drawLocked()
}

func (t *terminal) box() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.input)
}

// readKeys takes what the person types in the terminal: a line is a message (sent at Enter, joining a running turn),
// Escape stops the turn, y/a/n answer the oldest approval while the box is empty, ^C clears the box or stops the turn.
func (b *Bridge) readKeys(in io.Reader) {
	r := bufio.NewReader(in)
	keys := make(chan rune, 256)
	go func() {
		defer close(keys)
		for {
			c, _, err := r.ReadRune()
			if err != nil {
				return
			}
			keys <- c
		}
	}()
	pasting := false
	for c := range keys {
		switch {
		case c == 0x1b:
			// An escape alone is the key; one followed at once by more is a sequence (an arrow, a paste's mark).
			var seq []rune
			timeout := time.After(25 * time.Millisecond)
		collect:
			for {
				select {
				case n, ok := <-keys:
					if !ok {
						break collect
					}
					seq = append(seq, n)
					if len(seq) == 1 && n != '[' && n != 'O' {
						break collect
					}
					if len(seq) > 1 && n >= 0x40 && n <= 0x7e {
						break collect
					}
					timeout = time.After(25 * time.Millisecond)
				case <-timeout:
					break collect
				}
			}
			switch string(seq) {
			case "":
				b.cancel()
			case "[200~":
				pasting = true
			case "[201~":
				pasting = false
			}
		case c == '\r' || c == '\n':
			if pasting {
				b.term.edit(func(in []rune) []rune { return append(in, '\n') })
				continue
			}
			text := b.term.box()
			if strings.TrimSpace(text) == "" {
				continue
			}
			b.mu.Lock()
			b.ttyN++
			id := "tty-" + strconv.Itoa(b.ttyN) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
			b.mu.Unlock()
			b.term.edit(func([]rune) []rune { return nil })
			if err := b.prompt(id, text, nil, nil); err != nil {
				b.term.log(styleErr("not sent: " + err.Error()))
			}
		case c == 0x7f || c == 0x08:
			b.term.edit(func(in []rune) []rune {
				if len(in) > 0 {
					return in[:len(in)-1]
				}
				return in
			})
		case c == 0x03: // ^C
			if b.term.box() != "" {
				b.term.edit(func([]rune) []rune { return nil })
			} else {
				b.cancel()
			}
		case c == 0x15: // ^U
			b.term.edit(func([]rune) []rune { return nil })
		case c == '\t' || c >= 0x20:
			if !pasting && b.term.box() == "" {
				if a := b.oldestAsk(); a != nil {
					switch c {
					case 'y', 'Y':
						_ = b.decide(a.p.ID, "allow")
						continue
					case 'a', 'A':
						if a.p.Always {
							_ = b.decide(a.p.ID, "always")
							continue
						}
					case 'n', 'N':
						_ = b.decide(a.p.ID, "deny")
						continue
					}
				}
			}
			b.term.edit(func(in []rune) []rune { return append(in, c) })
		}
	}
}

// --- styles ---

func styleUser(s string) string { return "\x1b[1m" + s + "\x1b[0m" }
func styleDim(s string) string  { return "\x1b[2m" + s + "\x1b[0m" }
func styleErr(s string) string  { return "\x1b[31m" + s + "\x1b[0m" }
func styleTool(s string) string { return "\x1b[36m" + s + "\x1b[0m" }
func styleAsk(s string) string  { return "\x1b[33m" + s + "\x1b[0m" }

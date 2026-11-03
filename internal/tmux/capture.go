package tmux

import (
	"strconv"
	"strings"
	"time"
)

// Capture is the visible screen of the session's pane as text; false when tmux failed.
func (t *Tmux) Capture(name string) (string, bool) {
	return t.capture(name, "-p")
}

// CaptureStyled is the visible screen with its SGR attributes (`-e`), which tell a TUI's dim placeholder from typed
// text.
func (t *Tmux) CaptureStyled(name string) (string, bool) {
	return t.capture(name, "-p", "-e")
}

// CaptureHistory is the visible screen and up to lines lines of scrollback above it.
func (t *Tmux) CaptureHistory(name string, lines int) (string, bool) {
	return t.capture(name, "-p", "-S", "-"+strconv.Itoa(lines))
}

func (t *Tmux) capture(name string, flags ...string) (string, bool) {
	r := t.on(name, append(append([]string{"capture-pane"}, flags...), "-t", name)...)
	if r.err != nil {
		return "", false
	}
	return r.stdout, true
}

// GrowDetached makes the window of a session no client is attached to at least cols×rows and gives the program time to
// redraw; true when it grew. An attached terminal keeps the size it set. resize-window turns the window's window-size to
// manual, so that option is unset again: a terminal attached later sets the size as before.
func (t *Tmux) GrowDetached(name string, cols, rows int) bool {
	r := t.on(name, "display-message", "-p", "-t", name, "#{session_attached} #{window_width} #{window_height}")
	if r.err != nil {
		return false
	}
	fields := strings.Fields(r.stdout)
	if len(fields) != 3 || fields[0] != "0" {
		return false
	}
	curCols, errCols := strconv.Atoi(fields[1])
	curRows, errRows := strconv.Atoi(fields[2])
	if errCols != nil || errRows != nil || curCols >= cols && curRows >= rows {
		return false
	}
	if t.on(name, "resize-window", "-t", name, "-x", strconv.Itoa(max(curCols, cols)), "-y", strconv.Itoa(max(curRows, rows))).err != nil {
		return false
	}
	_ = t.on(name, "set-window-option", "-t", name, "-u", "window-size")
	t.sleep(resizeSettle)
	return true
}

// CaptureStyledTall is the styled screen at no less than cols×rows with the page's terminal attached: it is paused, made
// that large, captured and given its size back, about 0.6 s the person can see. false with no such terminal, or one
// that large already.
func (t *Tmux) CaptureStyledTall(term Terminal, name string, cols, rows int) (string, bool) {
	if term == nil {
		return "", false
	}
	curCols, curRows, known := term.Size(name)
	if !known || curCols >= cols && curRows >= rows {
		return "", false
	}
	origCols, origRows, ok := term.Pause(name)
	if !ok {
		return "", false
	}
	defer func() {
		term.Resize(name, origCols, origRows)
		t.sleep(resizeSettle)
		term.Resume(name)
	}()
	term.Resize(name, max(curCols, cols), max(curRows, rows))
	t.sleep(resizeSettle)
	return t.CaptureStyled(name)
}

// Terminal is the page's terminal attached to a session (internal/terminal's Server is one).
type Terminal interface {
	// Size is the size the page gave the terminal; false when no page has it open.
	Size(session string) (cols, rows int, ok bool)
	// Pause stops output to the page and answers the size to restore; false when there is no terminal or it is paused.
	Pause(session string) (cols, rows int, ok bool)
	// Resize sets the terminal's size, whatever the page asked.
	Resize(session string, cols, rows int)
	Resume(session string)
}

const (
	// A terminal wider than this shows a TUI's options on whole lines; the capture then needs no resize.
	narrowColsThreshold = 50
	atWidthHistoryLines = 50
	// Time for tmux and the program to redraw after a resize, before the capture and after the size is restored.
	resizeSettle = 300 * time.Millisecond
)

// CaptureAtWidth is the pane's screen as a program drawing at least cols×rows would show it, for parsing a TUI's
// options. When the page's terminal is wider than 50 columns and at least rows tall, the screen with 50 lines of
// scrollback is enough. Otherwise the terminal is paused, resized to at least cols×rows, captured after 300 ms, restored
// and resumed 300 ms later: the person's terminal visibly changes size for about 0.6 s. With no terminal open (or one
// already paused) it is the plain screen at the pane's own size. term may be nil.
func (t *Tmux) CaptureAtWidth(term Terminal, name string, cols, rows int) (string, bool) {
	var curCols, curRows int
	known := false
	if term != nil {
		curCols, curRows, known = term.Size(name)
	}
	if known && curCols > narrowColsThreshold && curRows >= rows {
		return t.CaptureHistory(name, atWidthHistoryLines)
	}
	if term == nil {
		return t.Capture(name)
	}
	origCols, origRows, ok := term.Pause(name)
	if !ok {
		return t.Capture(name)
	}
	defer func() {
		term.Resize(name, origCols, origRows)
		t.sleep(resizeSettle)
		term.Resume(name)
	}()
	term.Resize(name, max(curCols, cols), max(curRows, rows))
	t.sleep(resizeSettle)
	return t.Capture(name)
}

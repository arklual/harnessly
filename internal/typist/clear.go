package typist

import (
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Clearing the box. Claude Code's ctrl+u deletes from the cursor to the start of the row (a wrapped row, or a newline),
// so a long text goes row by row; it never interrupts a turn and never exits, where ctrl+c, in a working agent,
// interrupts the turn and keeps the text (verified on Claude Code 2.1.310). Only text of ours is cleared: a message a
// try left in the box, or a command of the harness that did not go through. Someone's draft is never touched.

const (
	clearStep    = 60 * time.Millisecond
	clearMaxKeys = 40
)

// harnessCommand is a setting command the harness types (SetModel, SetEffort): left in the box, it is ours to clear.
var harnessCommand = regexp.MustCompile(`^/(?:model|effort)(?:` + sp + `+` + nsp + `+)?$`)

// HarnessLeftover: the box holds nothing but a setting command of the harness.
func HarnessLeftover(s Screen, provider wire.Provider) bool {
	return provider != wire.ProviderCodex && s.Readable && s.Input != "" &&
		harnessCommand.MatchString(strings.TrimFunc(s.Box, isSpaceRune))
}

func isSpaceRune(r rune) bool { return blank(string(r)) }

// PathLeftover: the box holds nothing but picture paths the harness pasted (escaped as pasted, or bare; the box wraps
// them anywhere), with picture references among them or not — a paste the typist gave up on that landed later.
func PathLeftover(s Screen, provider wire.Provider, paths []string) bool {
	if provider == wire.ProviderCodex || !s.Readable || s.Input == "" || len(paths) == 0 {
		return false
	}
	rest := noSpace(imageRef.ReplaceAllString(unwrapRefs(s.Box), " "))
	found := false
	for _, path := range paths {
		for _, form := range []string{EscapePathForPrompt(path), path} {
			if f := noSpace(form); f != "" && strings.Contains(rest, f) {
				rest, found = strings.ReplaceAll(rest, f, ""), true
			}
		}
	}
	return found && rest == ""
}

func noSpace(s string) string { return strings.Map(dropSpace, s) }

func dropSpace(r rune) rune {
	if isSpaceRune(r) {
		return -1
	}
	return r
}

// ClearOwn empties Claude's box of text — what it holds must be a piece of text (with pictures: picture references of
// ours too), else nothing is pressed — row by row with ctrl+u, a reference ctrl+u leaves by Backspace, looking after
// every key: it stops as soon as the box holds anything that is not ours (the person started typing). Never while
// Claude still reads a paste of pictures: their references would land after the keys. True when the box ended empty.
// paths are picture paths of ours the box may hold besides (pasted, read late): they are cleared with the text.
func ClearOwn(p Pane, c Clock, provider wire.Provider, text string, pictures bool, paths ...string) bool {
	if provider == wire.ProviderCodex {
		return false
	}
	return clearRows(p, c, provider, func(s Screen) bool {
		left := withoutPaths(s, paths)
		return blank(left.Box) || ours(left, text, pictures)
	})
}

// wipeMaxKeys bounds the keys a wipe presses: a box of a few screens of text, row by row.
const wipeMaxKeys = 120

// Wipe empties the box whatever it holds — only on the person's word («Очистить поле и отправить»), never by itself:
// Claude's row by row with ctrl+u (Backspace for a reference ctrl+u leaves), Codex's by ctrl+u. Never ctrl+c: in a
// working agent it interrupts the turn. True when the box ended empty.
func Wipe(p Pane, c Clock, provider wire.Provider) bool {
	if provider == wire.ProviderCodex {
		for range 3 {
			view := Look(p, provider)
			if !view.Readable || !view.InputLine {
				return false
			}
			if blank(view.Input) {
				return true
			}
			if p.Keys("C-u") != nil {
				return false
			}
			c.Sleep(clearStep)
		}
		return blank(Look(p, provider).Input)
	}
	return clearRowsUpTo(p, c, provider, wipeMaxKeys, func(Screen) bool { return true })
}

// clearRows presses ctrl+u row by row while the box holds what mine says is ours, and stops at anything else.
func clearRows(p Pane, c Clock, provider wire.Provider, mine func(Screen) bool) bool {
	return clearRowsUpTo(p, c, provider, clearMaxKeys, mine)
}

func clearRowsUpTo(p Pane, c Clock, provider wire.Provider, maxKeys int, mine func(Screen) bool) bool {
	last := ""
	for keys := 0; ; keys++ {
		view := Look(p, provider)
		switch {
		case !view.Readable || !view.InputLine:
			return false
		case Pasting(view.Plain):
			if keys >= maxKeys+pastingLooks {
				return false
			}
			c.Sleep(imagePoll)
			continue
		case blank(view.Input):
			return true
		case keys >= maxKeys || !mine(view):
			return false
		}
		key := "C-u"
		if view.Box == last {
			key = "BSpace" // ctrl+u took nothing: a reference at the row's start
		}
		last = view.Box
		if p.Keys(key) != nil {
			return false
		}
		c.Sleep(clearStep)
	}
}

// ours: the box holds a piece of text, or (pictures) only picture references, or both.
func ours(s Screen, text string, pictures bool) bool {
	if pictures && s.OnlyPictures {
		return true
	}
	box := unwrapRefs(s.Box)
	if !pictures && CountImageRefs(box) > 0 {
		return false
	}
	return screen.ClaudeBoxPieceOf(box, text)
}

// pastingRE: Claude's footer while it reads a paste (pictures turned into references), the box not final yet.
var pastingRE = regexp.MustCompile(`^` + sp + `*Pasting…` + sp + `*$`)

// pastingLooks bounds the looks a clear waits for a paste to end.
const pastingLooks = 400

// Pasting: the footer under the box says Claude is still reading a paste.
func Pasting(plain string) bool {
	seen := 0
	lines := strings.Split(plain, "\n")
	for i := len(lines) - 1; i >= 0 && seen < 4; i-- {
		if blank(lines[i]) {
			continue
		}
		if seen++; pastingRE.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

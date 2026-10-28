package typist

import (
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Whitespace means what JavaScript's \s means, as everywhere the screen is read; nsp is its complement.
const (
	sp  = jscompat.SpaceClass
	nsp = `[^\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
)

// MenuKind is what the menu on a screen asks.
type MenuKind string

const (
	NoMenu         MenuKind = ""
	MenuQuestion   MenuKind = "question"   // Claude's AskUserQuestion dialog
	MenuPermission MenuKind = "permission" // a tool permission prompt
	MenuPlan       MenuKind = "plan"       // the plan approval prompt
	MenuOther      MenuKind = "other"      // any other numbered choice block (a picker, a trust dialog)
)

// Screen is one look at an agent's pane. Input is meaningful only with InputLine; Box is the whole input box (its
// wrapped lines too), Pictures the [Image #n] references in it, OnlyPictures that it holds nothing else.
type Screen struct {
	Readable bool
	Plain    string
	Busy     bool
	Menu     MenuKind
	// Panel: a TUI panel or picker is open over the box (Claude's /effort slider, its /model picker).
	Panel bool
	// Aside: one of those the person left open (Claude's /login, /model, /effort), which Esc closes safely.
	Aside     screen.Aside
	Options   []string // the menu's options as the screen shows them
	Input     string
	InputLine bool
	Box       string
	Pictures  int
	// OnlyPictures: the box holds picture references and nothing else.
	OnlyPictures bool
	// Queued: Claude shows messages waiting in its own queue (a steered message not handed over yet).
	Queued bool
	// Word: the reply Claude is writing, off the screen, while it works (empty: none shown).
	Word string
	// styled is the capture the screen was read from, colours and all.
	styled string
}

// PromptReady: the agent shows an input line and is not working.
func (s Screen) PromptReady() bool { return s.Readable && s.InputLine && !s.Busy && s.Menu == NoMenu }

var (
	// imageRef: the box wraps a long line anywhere, a reference too ("[Image" at a line's end, "#104]" next).
	imageRef      = regexp.MustCompile(`\[Image` + sp + `+#\d+\]`)
	imageRefSpace = regexp.MustCompile(sp + `+`)
	// imageTokensOnly: a box of picture references alone, one wrapped midway included.
	imageTokensOnly = regexp.MustCompile(`^` + sp + `*(?:\[Image` + sp + `+#\d+\]` + sp + `*)+$`)
	boxRule         = regexp.MustCompile(`^` + sp + `*─{3,}` + sp + `*$`)
)

// Read classifies a styled capture of an agent's pane.
func Read(styled string, provider wire.Provider) Screen {
	st := screen.Read(styled)
	s := Screen{Readable: true, Plain: st.Plain, Busy: st.Busy, Panel: st.Panel, Input: st.Input, InputLine: st.InputLine,
		styled: styled}
	s.Menu, s.Options = menuOf(st)
	if st.Busy && provider != wire.ProviderCodex {
		s.Word = liveReply(st.Plain)
	}
	// The input line reads empty with its placeholder shown; only typed input has a box worth reading.
	if st.Input != "" {
		s.Box = st.Input
		if provider != wire.ProviderCodex {
			s.Box = inputBoxText(st.Plain)
		}
		s.Pictures, s.OnlyPictures = CountImageRefs(s.Box), imageTokensOnly.MatchString(s.Box)
	}
	if provider != wire.ProviderCodex {
		s.Queued = screen.QueueInPromptBox(styled)
		s.Aside = screen.ClaudeAside(st.Plain)
	}
	return s
}

// Look reads the pane; a pane that cannot be read is a Screen that is not Readable.
func Look(p Pane, provider wire.Provider) Screen {
	styled, ok := p.CaptureStyled()
	if !ok {
		return Screen{}
	}
	return Read(styled, provider)
}

func menuOf(st screen.State) (MenuKind, []string) {
	switch {
	case screen.IsQuestionOnScreen(st.Plain):
		opts := []string{}
		if st.Menu != nil {
			opts = st.Menu.Options
		}
		return MenuQuestion, opts
	case st.Menu == nil:
		return NoMenu, nil
	}
	if plan := screen.ParsePlanOptions(st.Plain); len(plan) > 0 && planPrompt(st.Plain) {
		return MenuPlan, plan
	}
	if perm := screen.ParsePermissionOptions(st.Plain); len(perm.Options) > 0 {
		return MenuPermission, perm.Options
	}
	return MenuOther, st.Menu.Options
}

var planPromptRE = regexp.MustCompile(`(?i)plan|auto-accept edits`)

// planPrompt: the menu is Claude's plan approval — its heading or options speak of the plan.
func planPrompt(plain string) bool {
	lines := strings.Split(plain, "\n")
	return planPromptRE.MatchString(strings.Join(lines[max(0, len(lines)-25):], "\n"))
}

var toolBullet = regexp.MustCompile(`^● [A-Za-z_][\w:.\-]*\(`)

// liveReplyMax is how much of the reply "now" carries.
const liveReplyMax = 160

// liveReply is the last text bullet of a plain screen — "● …" that is not a tool call — with the lines it wraps onto,
// joined by spaces and cut to liveReplyMax characters.
func liveReply(plain string) string {
	lines := strings.Split(plain, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		rest, ok := strings.CutPrefix(lines[i], "● ")
		if !ok || toolBullet.MatchString(lines[i]) {
			continue
		}
		parts := []string{strings.TrimSpace(rest)}
		for _, l := range lines[i+1:] {
			if !strings.HasPrefix(l, "  ") || strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "⎿") {
				break
			}
			parts = append(parts, strings.TrimSpace(l))
		}
		text := strings.Join(parts, " ")
		if rs := []rune(text); len(rs) > liveReplyMax {
			text = string(rs[:liveReplyMax-1]) + "…"
		}
		return text
	}
	return ""
}

// inputBoxText is the text of Claude's input box on a plain screen: its input line (❯ at the line's start) and the
// lines it wraps onto, up to the box's bottom rule, joined by spaces.
func inputBoxText(plain string) string {
	lines := strings.Split(plain, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		rest, ok := strings.CutPrefix(lines[i], "❯")
		if !ok {
			continue
		}
		parts := []string{rest}
		for _, l := range lines[i+1:] {
			if boxRule.MatchString(l) {
				break
			}
			parts = append(parts, l)
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// CountImageRefs counts the `[Image #n]` references in a text, wrapped ones too.
func CountImageRefs(text string) int { return len(imageRef.FindAllStringIndex(text, -1)) }

// NewImageRefs counts the references on a screen that the earlier one did not have, reference by reference: older
// ones that scrolled away meanwhile do not hide new ones, and a number shown again after a reset counts once more.
func NewImageRefs(before, now string) int {
	seen := map[string]int{}
	for _, r := range imageRef.FindAllString(before, -1) {
		seen[imageRefSpace.ReplaceAllString(r, " ")]++
	}
	n := 0
	for _, r := range imageRef.FindAllString(now, -1) {
		if r = imageRefSpace.ReplaceAllString(r, " "); seen[r] > 0 {
			seen[r]--
			continue
		}
		n++
	}
	return n
}

// Decision is whether a message may be typed now.
type Decision string

const (
	DecideSend        Decision = "send"
	DecideWait        Decision = "wait"
	DecideBlockedMenu Decision = "blocked-menu"
	DecideBlockedText Decision = "blocked-text"
)

// Deliverable decides from the screen: nothing readable or no input line, or a working agent → wait; a menu (it would
// take the keys as an answer) → blocked-menu; text in the box (it would go along with ours) → blocked-text; else send.
func Deliverable(s Screen) Decision {
	switch {
	case !s.Readable || s.Menu == NoMenu && !s.InputLine:
		return DecideWait
	case s.Menu != NoMenu:
		return DecideBlockedMenu
	case s.Busy:
		return DecideWait
	case !blank(s.Input):
		return DecideBlockedText
	}
	return DecideSend
}

// StillInBox: the box still holds text, the message's start (Claude: the whole box, its wrapped lines and wrapped
// picture references too; Codex: its prompt box).
func StillInBox(s Screen, provider wire.Provider, text string) bool {
	if provider == wire.ProviderCodex {
		return screen.CodexStillInBox(s.Plain, text)
	}
	return !blank(text) && screen.ClaudeStillInBox(unwrapRefs(s.Box), text)
}

// unwrapRefs joins references the box wrapped midway, so the screen package reads them as references.
func unwrapRefs(box string) string {
	return imageRef.ReplaceAllStringFunc(box, func(r string) string { return imageRefSpace.ReplaceAllString(r, " ") })
}

func blank(s string) bool { return strings.TrimFunc(s, jscompat.IsSpace) == "" }

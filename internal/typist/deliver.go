package typist

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Message is what one try types: pictures (absolute paths) before the text.
type Message struct {
	Text   string
	Images []string
	// Steer types into a working agent: its turn takes the text (Claude queues it until the next step). A menu or
	// foreign text still stops it.
	Steer bool
	// HandOver: the running tool is one Claude can hand to the background (Bash, an MCP call), so a steered message
	// goes in now by Claude's chord instead of waiting for the tool to end.
	HandOver bool
	// HandOverOK, when set, is asked right before the chord: the tool may have ended while the text was typed, and then
	// the chord would cut the model's answer short instead of handing a call over. Nil: go ahead.
	HandOverOK func() bool
	// EnterOnly: a restart found the text in the box; only its Enter is pressed, and a box without it types nothing.
	EnterOnly bool
	// ClearOnly: the message is not to be sent but taken out of the box, where an earlier try left it (it stalled):
	// Sent when the box no longer holds it, BlockedText when it could not be cleared, Wait when it was not there.
	ClearOnly bool
	// Leftovers are the picture paths the harness pasted for its messages (this one's and earlier ones'). Claude reads
	// a big picture late: a paste the typist gave up on lands after it, as path text, and is ours to clear.
	Leftovers []string
	// Typed are the texts of the earlier messages the harness typed (not this one's): what is left of one in the box,
	// its paths and references aside, is ours to clear.
	Typed []string
	// Wipe: the person said to clear the box and send («Очистить поле и отправить»): text in the box that is not ours
	// is taken out too, row by row, and the message goes.
	Wipe bool
}

// Outcome is how one try ended.
type Outcome string

const (
	// Sent: the text left the input box (or the screen could not be read to say otherwise) — not yet accepted.
	Sent Outcome = "sent"
	// Wait: no input line or a working agent; nothing was typed.
	Wait Outcome = "wait"
	// BlockedMenu, BlockedText: a menu or someone else's text; nothing was typed.
	BlockedMenu Outcome = "blocked-menu"
	BlockedText Outcome = "blocked-text"
	// Failed: a paste or a key did not go in, or a picture did not show up. Nothing reached the agent for sure: the
	// next try finds what did reach the box as its own and types only the rest.
	Failed Outcome = "failed"
	// Stuck: typed, Entered three times, still in the box.
	Stuck Outcome = "stuck"
)

// Result is one try: its outcome, the screen it started from, and what it did on the way.
type Result struct {
	Outcome Outcome
	Before  Screen
	// Typed: keys went in (Sent, Stuck, a Failed after the first key). Own: the box held this message's pictures or
	// text from an earlier try, and only the rest was typed. ChatAbout: the question dialog was closed first.
	Typed, ChatAbout, HandedOver bool
	// Cleared: the screen the person left over the box (their /login, a picker) was closed by Esc first.
	Cleared screen.Aside
	Own     bool
	// AsPath: Claude did not show a picture as a reference, and the message went with its path as text.
	AsPath bool
	// Wiped: the box held text not of ours, and the person's word cleared it (Message.Wipe).
	Wiped bool
	Err   error
}

const (
	pasteSettle      = 250 * time.Millisecond // Claude reads a paste as one block; an Enter on its heels lands too early
	enterAfterText   = 50 * time.Millisecond  // a typed line's Enter as its own key, not part of the burst
	pasteReadPoll    = 100 * time.Millisecond // how often the box is looked at for the end of a paste
	pasteReadLimit   = 3 * time.Second        // the longest an Enter waits for the box to read a paste to its end
	confirmPoll      = 150 * time.Millisecond
	confirmRetry     = 700 * time.Millisecond
	confirmMaxEnters = 3
	confirmLimit     = 4000 * time.Millisecond
	// confirmUnseen: a box that never showed the message is not yet proof that it left. Under load Claude draws a long
	// typed line late, its Enter taken into it as a newline: the box reads empty for the first looks, then holds the
	// text. Only a box free of it this long after the Enter counts as sent.
	confirmUnseen  = 600 * time.Millisecond
	chatAboutMoves = 8
	chatAboutStep  = 120 * time.Millisecond
	dismissKeys    = 2
	dismissStep    = 300 * time.Millisecond
)

// errPicture: the agent did not show a reference for every picture pasted.
var errPicture = errors.New("typist: a picture did not show up")

// Plan is what one look at the screen lets a message do: the decision, and what of the message the box holds
// already from an earlier try — Landed of its pictures, or its text (OwnText).
type Plan struct {
	Decision Decision
	Landed   int
	OwnText  bool
	// Nothing: an Enter-only try finds the box without its text — there is nothing left to type.
	Nothing bool
	// ChatAbout: the question dialog is to be closed by «Chat about this» before the text goes in as the answer.
	ChatAbout bool
	// Dismiss: a screen the person left open in the terminal (/login, /model) is closed by Esc first.
	Dismiss screen.Aside
	// Clear: the box holds a setting command of the harness that did not go through, or picture paths the harness
	// pasted that never became pictures, or what is left of an earlier message of ours; it is cleared first, with its
	// picture references when ClearRefs.
	Clear     string
	ClearRefs bool
	// Wipe: the box holds text not of ours, and the message says to clear it all the same (Message.Wipe).
	Wipe bool
}

// Own: the box holds some of the message already.
func (p Plan) Own() bool { return p.Landed > 0 || p.OwnText }

// Decide reads a look for a message, as Type does before any key: the outbox asks it first so a try the screen would
// refuse is not started at all.
func Decide(m Message, s Screen, provider wire.Provider) Plan {
	// Claude still reads a paste of pictures: the box is not final, its references land later — nothing goes in now.
	if provider != wire.ProviderCodex && s.Readable && Pasting(s.Plain) {
		return Plan{Decision: DecideWait}
	}
	// The person opened /login or a picker in the terminal and left it: it is closed, then the message goes in.
	if s.Readable && !s.Busy && s.Aside != screen.NoAside && provider != wire.ProviderCodex && !m.EnterOnly {
		return Plan{Decision: DecideSend, Dismiss: s.Aside}
	}
	// The agent asks a question and the person wrote instead of picking: «Chat about this» closes the dialog and the
	// message goes in as the reply.
	if s.Menu == MenuQuestion && !blank(m.Text) && provider != wire.ProviderCodex && !m.EnterOnly {
		return Plan{Decision: DecideSend, ChatAbout: true}
	}
	// An earlier try may have left this message's pictures in the box ([Image #1]) before the agent confirmed them,
	// all or the first of them: they are ours, so the box counts as empty and only the missing ones are pasted. More
	// pictures than the message has, or other text after them, are someone else's.
	ownPictures := s.Readable && len(m.Images) > 0 && s.OnlyPictures && s.Pictures <= len(m.Images)
	// A message an earlier try typed whose Enter was lost sits in the box (behind its pictures when it has them): it
	// is ours too, and only its Enter is pressed again — it is never typed twice. A path of its pictures Claude read
	// late may sit in the box with it: the path goes with the Enter, and Claude reads the picture by it.
	own := withoutPaths(s, ownPaths(m))
	ownText := own.Readable && own.Input != "" && StillInBox(own, provider, m.Text)
	typed := ""
	if !ownPictures && !ownText && !m.EnterOnly {
		typed = TypedLeftover(own, provider, m.Typed)
	}
	leftover := !ownPictures && !ownText && !m.EnterOnly &&
		(HarnessLeftover(s, provider) || PathLeftover(s, provider, m.Leftovers) || typed != "")
	view := s
	if ownPictures || ownText || leftover {
		view.Input = ""
	}
	if m.Steer {
		view.Busy = false
	}
	p := Plan{Decision: Deliverable(view), OwnText: ownText}
	if leftover {
		p.Clear, p.ClearRefs = s.Box, CountImageRefs(s.Box) > 0
		if typed != "" {
			p.Clear = typed
		}
	}
	if m.Wipe && !m.EnterOnly && p.Decision == DecideBlockedText {
		p.Decision, p.Wipe = DecideSend, true
	}
	if ownPictures && !ownText {
		p.Landed = s.Pictures
	}
	if m.EnterOnly && !ownText && p.Decision == DecideSend {
		p.Nothing = true
	}
	return p
}

// Type tries one message once: it reads the screen, closes a question dialog by «Chat about this» when the text is
// the person's answer in words, decides whether it may type now, and types what the box does not hold yet. The caller
// holds the pane's lock.
func Type(m Message, provider wire.Provider, p Pane, c Clock) Result {
	view := Look(p, provider)
	res := Result{Before: view}
	if m.ClearOnly {
		switch {
		case !view.Readable || view.Input == "" || !StillInBox(withoutPaths(view, ownPaths(m)), provider, m.Text):
			res.Outcome = Wait
		case ClearOwn(p, c, provider, m.Text, len(m.Images) > 0, ownPaths(m)...):
			res.Outcome = Sent
		default:
			res.Outcome = BlockedText
		}
		return res
	}
	plan := Decide(m, view, provider)
	if plan.Dismiss != screen.NoAside {
		if dismiss(p, c) {
			res.Cleared = plan.Dismiss
		}
		view = Look(p, provider)
		plan = Decide(m, view, provider)
		if plan.Dismiss != screen.NoAside {
			res.Outcome = BlockedMenu
			return res
		}
	}
	if plan.ChatAbout {
		if chatAbout(p, c) {
			res.ChatAbout = true
			view = Look(p, provider)
		}
		plan = Decide(Message{Text: m.Text, Images: m.Images, Steer: m.Steer}, view, provider)
		if plan.ChatAbout {
			res.Outcome = BlockedMenu
			return res
		}
	}
	if plan.Clear != "" && plan.Decision == DecideSend {
		// A command of ours that did not go through sits in the box: it goes, then the box is looked at again.
		if !ClearOwn(p, c, provider, plan.Clear, plan.ClearRefs, ownPaths(m)...) {
			res.Outcome = BlockedText
			return res
		}
		plan = Decide(m, Look(p, provider), provider)
	}
	if plan.Wipe {
		// The person's word: the box is theirs to give up. It is emptied, then looked at again — for the message only,
		// so nothing typed after the look is wiped in its turn.
		if !Wipe(p, c, provider) {
			res.Outcome = BlockedText
			return res
		}
		res.Wiped = true
		once := m
		once.Wipe = false
		plan = Decide(once, Look(p, provider), provider)
	}
	res.Own = plan.Own()
	switch plan.Decision {
	case DecideWait:
		res.Outcome = Wait
		return res
	case DecideBlockedMenu:
		res.Outcome = BlockedMenu
		return res
	case DecideBlockedText:
		res.Outcome = BlockedText
		return res
	}
	if plan.Nothing {
		res.Outcome = Sent
		return res
	}
	res.Outcome, res.Err = typeRest(m, provider, p, c, plan.Landed, plan.OwnText, &res.Typed, &res.AsPath)
	if res.Outcome == Sent && m.Steer && m.HandOver && provider != wire.ProviderCodex && (m.HandOverOK == nil || m.HandOverOK()) {
		res.HandedOver = handOver(p, c)
	}
	return res
}

// typeRest types what the box does not have yet: landed pictures of the message are there already, and ownText says
// the text is too.
func typeRest(m Message, provider wire.Provider, p Pane, c Clock, landed int, ownText bool, typed, asPath *bool) (Outcome, error) {
	if ownText && provider != wire.ProviderCodex && holdEnter(m.Text, p, c, ownPaths(m)...) != nil {
		// The box holds the message cut short (an Enter took a part of it, a paste was read half): an Enter would send
		// it so. It is cleared, its late paths too, and typed whole.
		if !pastingEnds(p, c) || !ClearOwn(p, c, provider, m.Text, len(m.Images) > 0, ownPaths(m)...) {
			return Failed, errPasteUnread
		}
		*typed = true
		ownText, landed = false, 0
	}
	if len(m.Images) > landed && !ownText {
		*typed = true
		if err := pasteImages(m.Images[landed:], provider != wire.ProviderCodex, landed, p, c, provider); err != nil {
			if errors.Is(err, errPicture) && provider != wire.ProviderCodex {
				*asPath = true
				return sendAsPath(m, landed, p, c)
			}
			if errors.Is(err, errPicture) && provider == wire.ProviderCodex {
				// The paths stay in the box as text otherwise, and the next message finds the box taken by someone
				// else's text: the box held nothing of anyone's before (else it was blocked), so it is wiped whole.
				_ = p.Keys("C-u")
			}
			return Failed, err
		}
	}
	text := m.Text
	switch {
	case ownText:
	case provider == wire.ProviderCodex:
		*typed = true
		if err := p.Paste(text); err != nil {
			return Failed, err
		}
		c.Sleep(pasteSettle)
	case len(m.Images) == 0 && SubmitsAsTypedLine(text):
		*typed = true
		if err := typeLine(text, p, c); err != nil {
			return Failed, err
		}
		return confirmLeftBox(text, false, provider, p, c, true)
	case !blank(text):
		if len(m.Images) > 0 {
			text = " " + text
		}
		*typed = true
		if err := p.Paste(text); err != nil {
			return Failed, err
		}
		c.Sleep(pasteSettle)
		if err := holdEnter(text, p, c); err != nil {
			return Failed, err
		}
	}
	*typed = true
	if err := p.Keys("Enter"); err != nil {
		return Failed, err
	}
	return confirmLeftBox(m.Text, len(m.Images) > 0, provider, p, c, true, ownPaths(m)...)
}

// typeLine types a line and its Enter. A pane that types the keys alone gets its Enter once the box has read the line
// to its end: a working Claude on a loaded machine takes a burst of keys as a paste, and an Enter 50 ms behind it
// queued a message of 26.10 cut short at its 102nd character.
func typeLine(text string, p Pane, c Clock) error {
	lt, ok := p.(LineTyper)
	if !ok {
		return p.TypeLine(text)
	}
	if err := lt.TypeOnly(text); err != nil {
		return err
	}
	c.Sleep(enterAfterText)
	waitPasteRead(text, p, c)
	return p.Keys("Enter")
}

// waitPasteRead holds the Enter until Claude's box has read the paste to its end: a working Claude on a loaded machine
// reads a paste slowly, and an Enter before its end queues the message cut short (the rest is lost). A screen that
// cannot be read, or a box still short of the end past pasteReadLimit, holds it no longer.
func waitPasteRead(text string, p Pane, c Clock) {
	for waited := time.Duration(0); waited < pasteReadLimit; waited += pasteReadPoll {
		view := Look(p, wire.ProviderClaude)
		if !view.Readable || !view.InputLine || screen.ClaudeBoxHoldsEnd(view.Box, text) {
			return
		}
		c.Sleep(pasteReadPoll)
	}
}

// errPasteUnread: Claude's box did not read the paste to its end (or still reads it): no Enter is pressed, the box
// keeps what it has, and the next try finds it as the message's own and presses only its Enter.
var errPasteUnread = errors.New("typist: the box did not read the paste to its end")

// pastingEnds waits while Claude says it reads a paste («Pasting…»), up to imagePastingMax. False: it still does.
func pastingEnds(p Pane, c Clock) bool {
	for waited := time.Duration(0); ; waited += imagePoll {
		view := Look(p, wire.ProviderClaude)
		if !view.Readable || !Pasting(view.Plain) {
			return true
		}
		if waited >= imagePastingMax {
			return false
		}
		c.Sleep(imagePoll)
	}
}

// holdEnter is the gate before a Claude message's Enter: never while Claude says it reads a paste («Pasting…»), and
// not while the box shows text that stops short of the message's end (its last character too) — an Enter then submits
// the box cut short and leaves the rest in it, in the way of every message after. A box that shows no text of it at all
// (the screen read without it, pictures only) holds the Enter pasteReadLimit, as before; one that shows it cut short
// holds it pasteCutLimit, then the try fails without an Enter and the next one finds the text as its own. A screen
// that cannot be read does not hold the Enter back.
func holdEnter(text string, p Pane, c Clock, paths ...string) error {
	if !pastingEnds(p, c) {
		return errPasteUnread
	}
	for waited := time.Duration(0); ; waited += pasteReadPoll {
		view := withoutPaths(Look(p, wire.ProviderClaude), paths)
		switch {
		case !view.Readable || blank(text):
			return nil
		case Pasting(view.Plain):
			if waited >= imagePastingMax {
				return errPasteUnread
			}
		case !view.InputLine:
			return nil
		case screen.ClaudeBoxHoldsEnd(view.Box, text):
			return nil
		case blank(view.Input) || view.OnlyPictures:
			if waited >= pasteReadLimit {
				return nil
			}
		case waited >= pasteCutLimit:
			return errPasteUnread
		}
		c.Sleep(pasteReadPoll)
	}
}

// pasteCutLimit: the longest an Enter waits for a box that shows the message cut short to read it to its end.
const pasteCutLimit = 4 * pasteReadLimit

// sendAsPath sends a Claude message whose pictures did not show as references with their paths as text: Claude reads
// a file by its path all the same, and a reference it makes of one late is as good. Paths the box lost (no path text,
// no late reference) are pasted again, then the text, one Enter, and the whole line is what must leave the box.
func sendAsPath(m Message, landed int, p Pane, c Clock) (Outcome, error) {
	escaped := make([]string, 0, len(m.Images)-landed)
	for _, path := range m.Images[landed:] {
		escaped = append(escaped, EscapePathForPrompt(path))
	}
	paths := strings.Join(escaped, " ")
	// A paste Claude still reads is not over: its paths land in the box later. Pasting them again meanwhile doubled
	// them, and the Enter that followed took the box half read — a message of only «…/up_FQ5I5U64QSREFY7F.pn» went to
	// the agent, the other paths left in the box (28.10).
	if !pastingEnds(p, c) {
		return Failed, errPasteUnread
	}
	view := Look(p, wire.ProviderClaude)
	if !strings.Contains(noSpace(view.Box), noSpace(paths)) && CountImageRefs(view.Box) <= landed {
		if err := p.Paste(paths); err != nil {
			return Failed, err
		}
		c.Sleep(pasteSettle)
	}
	full := paths
	if !blank(m.Text) {
		full = paths + " " + m.Text
		if err := p.Paste(" " + m.Text); err != nil {
			return Failed, err
		}
		c.Sleep(pasteSettle)
	}
	if err := holdEnter(full, p, c); err != nil {
		return Failed, err
	}
	if err := p.Keys("Enter"); err != nil {
		return Failed, err
	}
	return confirmLeftBox(full, true, wire.ProviderClaude, p, c, true)
}

// confirmLeftBox: a message counts as sent only once it has left the input box. The box is read every 150 ms for up
// to 4 s; while the message is still there, Enter is pressed again 700 ms after the last one, three Enters in all. A
// message the box never showed is sent only once the box stayed free of it for confirmUnseen.
// Still there after that: stuck — the next try finds the text as its own and presses only its Enter. A screen that
// cannot be read is not held against the send: whether the agent took it is for the hook and the transcript to say.
// A stuck text is cleared out of the box, row by row (ClearOwn), and the retry types it whole.
func confirmLeftBox(text string, pictures bool, provider wire.Provider, p Pane, c Clock, entered bool, paths ...string) (Outcome, error) {
	if blank(text) {
		return Sent, nil
	}
	enters, sinceEnter, read, seen := 0, time.Duration(0), false, false
	if entered {
		enters = 1
	}
	for waited := time.Duration(0); waited < confirmLimit; waited += confirmPoll {
		c.Sleep(confirmPoll)
		sinceEnter += confirmPoll
		view := Look(p, provider)
		if !view.Readable {
			continue
		}
		read = true
		if !StillInBox(withoutPaths(view, paths), provider, text) {
			if seen || waited+confirmPoll >= confirmUnseen {
				return Sent, nil
			}
			continue
		}
		seen = true
		if sinceEnter >= confirmRetry && enters < confirmMaxEnters {
			if err := p.Keys("Enter"); err != nil {
				return Failed, err
			}
			enters++
			sinceEnter = 0
		}
	}
	if !read {
		return Sent, nil
	}
	// Three Enters and still there: the message fails (the person retries it), and the box is not left dirty — a text
	// left behind would go with the next message, or reach the agent late, on someone else's Enter, as a copy.
	ClearOwn(p, c, provider, text, pictures, paths...)
	return Stuck, nil
}

// chatAbout moves the question dialog's cursor to «Chat about this», presses Enter and waits for the dialog to go.
func chatAbout(p Pane, c Clock) bool {
	for moves := 0; ; moves++ {
		plain, ok := p.Capture()
		if !ok {
			return false
		}
		present, selected := screen.ChatAboutRow(plain)
		if !present {
			return false
		}
		if selected {
			if p.Keys("Enter") != nil {
				return false
			}
			break
		}
		if moves >= chatAboutMoves || p.Keys("Down") != nil {
			return false
		}
		c.Sleep(chatAboutStep)
	}
	return screen.WaitForQuestionGone(p.Capture, c.Sleep)
}

// dismiss presses Esc on an aside the person left over the box, at most twice and only while it is still up: a third
// Esc would reach the agent itself, which takes it as «stop». True when it pressed Esc and the box is back.
func dismiss(p Pane, c Clock) bool {
	pressed := false
	for range dismissKeys {
		plain, ok := p.Capture()
		if !ok {
			return false
		}
		if screen.ClaudeAside(plain) == screen.NoAside {
			return pressed
		}
		if p.Keys("Escape") != nil {
			return false
		}
		pressed = true
		c.Sleep(dismissStep)
	}
	plain, ok := p.Capture()
	return ok && screen.ClaudeAside(plain) == screen.NoAside
}

// handOver presses Claude's chord ctrl+x ctrl+s once while its queue shows: a running Bash or MCP call goes to the
// background and the queued message reaches the turn now. It is pressed only for such a tool; while the model writes
// the chord would cut the answer short.
func handOver(p Pane, c Clock) bool {
	c.Sleep(confirmPoll)
	styled, ok := p.CaptureStyled()
	if !ok || !Read(styled, wire.ProviderClaude).Queued {
		return false
	}
	return p.Keys("C-x", "C-s") == nil
}

// LongPromptText is the longest text Claude Code takes as typed; a longer typed burst reads as a paste too.
const LongPromptText = 800

var (
	modeOpener     = regexp.MustCompile(`^[!#/&]`)
	completionTail = regexp.MustCompile(`(?:^|` + sp + `)[@/]` + nsp + `*$`)
)

// SubmitsAsTypedLine: a line Claude Code takes as typed and submits on an Enter right behind it — short and single,
// not opening a mode with its first character (! shell, # memory, / command, & background) and not ending in an @ or
// / word, where Claude's completion list is open and would take the Enter.
func SubmitsAsTypedLine(text string) bool {
	line := jscompat.Trim(text)
	return line != "" && !strings.Contains(text, "\n") && jscompat.Len(text) <= LongPromptText &&
		!modeOpener.MatchString(line) && !completionTail.MatchString(line)
}

package typist

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The keys of the agent's dialogs: a question answered, a permission or a plan decided. Like a message they go in
// under the pane's lock, and they count only once the dialog has left the screen.

// ErrDialogStayed: the keys went in and the dialog is still on the screen.
var ErrDialogStayed = errors.New("typist: the dialog is still on the screen")

// ChatAbout closes a question dialog by its "Chat about this" row; false when the row is not there or the dialog
// stayed.
func ChatAbout(p Pane, c Clock) bool { return chatAbout(p, c) }

// Answer types the answers of a question dialog, one group of keys per question, and waits for it to go.
func Answer(p Pane, c Clock, shapes []screen.QuestionShape, choices []screen.QuestionChoice) error {
	if err := screen.ValidateAnswerRequest(shapes, choices); err != nil {
		return err
	}
	if err := screen.PlayAnswerSteps(screen.BuildAnswerSteps(shapes, choices), keyIO{p, c}); err != nil {
		return err
	}
	if !screen.WaitForQuestionGone(p.Capture, c.Sleep) {
		return ErrDialogStayed
	}
	return nil
}

// pickConfirm is how long a decided menu has to leave the screen.
const pickConfirm = 3 * time.Second

// ErrMenuChanged: the menu on the screen is not the one the person decided on — another tool call asks, or the
// options differ — so no key was pressed.
var ErrMenuChanged = errors.New("typist: the menu on the screen is not the one decided on")

// PickWant is the menu a person decided on and the option they chose, as the gate showed them.
type PickWant struct {
	// N is the option's number (1-based) and Label its text without the number, as the screen showed them.
	N     int
	Label string
	// Plan: the menu is a plan's approval, not a tool's permission.
	Plan bool
	// Command is the tool call the permission asks about ("" for a plan, or when the gate did not know it).
	Command string
	// Cwd is the session's working directory: the one a «cd <dir> &&» of the command may be left out for.
	Cwd string
}

// OptionLabel is a menu line's text without its number: "2. Yes, and don't ask again" → "Yes, and don't ask again".
func OptionLabel(line string) string {
	label := strings.TrimSpace(line)
	if num, rest, ok := strings.Cut(label, ". "); ok {
		if _, err := strconv.Atoi(num); err == nil {
			return strings.TrimSpace(rest)
		}
	}
	return label
}

// optionOf is the label of the option numbered n on the screen: the line that carries the number, else the nth line.
func optionOf(options []string, n int) (string, bool) {
	for _, line := range options {
		t := strings.TrimSpace(line)
		if num, _, ok := strings.Cut(t, ". "); ok && num == strconv.Itoa(n) {
			return OptionLabel(t), true
		}
	}
	if n >= 1 && n <= len(options) {
		return OptionLabel(options[n-1]), true
	}
	return "", false
}

// The command of a shell call as the dialog draws it, checked whole. Claude 2.1.311 draws it under the dialog's solid
// rule, in a gutter («│ » on every line) or indented, wrapped at the pane's width — at a space, which the break eats,
// or inside a word — and above the question's options. Nothing else is vouched for: the command must be on the screen
// from its first character to its last, line after line, every character equal; only where a line breaks may differ
// from the summary (a space eaten or not, a newline). A command the dialog cuts or rewrites is the person's to decide
// in the terminal. A leading «cd <dir> &&» is dropped from the summary only when <dir> is the session's own directory,
// the one Claude leaves out.
var (
	gutterRE    = regexp.MustCompile(`^` + sp + `*│` + sp + `?`)
	firstOptRE  = regexp.MustCompile(`^` + sp + `*(?:[❯›>]` + sp + `*)?1\.` + sp + `*\S`)
	cdCommandRE = regexp.MustCompile(`^cd` + sp + `+("[^"]*"|'[^']*'|[^\s&;"']+)` + sp + `*&&` + sp + `*`)
)

// shellCommand is the command of a shell summary ("$ rm -rf build"), whole.
func shellCommand(summary string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(summary), "$ "))
}

// withoutOwnCd drops a leading «cd <cwd> &&»; a cd anywhere else stays.
func withoutOwnCd(command, cwd string) (string, bool) {
	m := cdCommandRE.FindStringSubmatch(command)
	if m == nil || cwd == "" || strings.Trim(m[1], `"'`) != cwd {
		return "", false
	}
	return command[len(m[0]):], true
}

// commandBlock is the first run of gutter lines under the dialog's last solid rule and above its first option: the
// box Claude draws the command in (the warnings and the timer come in a box of their own after it). Gutters are off.
func commandBlock(plain string) []string {
	lines := strings.Split(plain, "\n")
	start := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if solidRuleRE.MatchString(lines[i]) {
			start = i + 1
			break
		}
	}
	var block []string
	for _, l := range lines[start:] {
		if firstOptRE.MatchString(l) {
			break
		}
		if !gutterRE.MatchString(l) {
			if len(block) > 0 {
				break
			}
			continue
		}
		block = append(block, strings.TrimSpace(gutterRE.ReplaceAllString(l, "")))
	}
	return block
}

// unwraps: the lines of the block, joined as the dialog wraps them (a break eats a space or none), are exactly the
// command — no line before, none after, no character more or less.
func unwraps(block []string, command string) bool {
	if len(block) == 0 {
		return false
	}
	rest := command
	for _, line := range block {
		if line == "" || !strings.HasPrefix(rest, line) {
			return false
		}
		rest = strings.TrimLeftFunc(rest[len(line):], jscompat.IsSpace)
	}
	return rest == ""
}

// shellDrawn: the dialog on the screen draws this shell command, whole and alone in its box.
func shellDrawn(plain, summary, cwd string) bool {
	command := shellCommand(summary)
	if command == "" {
		return false
	}
	block := commandBlock(plain)
	if unwraps(block, command) {
		return true
	}
	rest, ok := withoutOwnCd(command, cwd)
	return ok && rest != "" && unwraps(block, rest)
}

// A summary that names its tool first ("Read /a/b.png") is not what the dialog draws. Claude 2.1.310 draws the
// dialog of such a call in a fixed layout: a solid rule, the dialog's title in bold in the rule's colour, a dashed
// rule, the call alone on its line («Read(/a/b.png)»), a dashed rule, then a body. The agent's own text can draw
// rules and words, not that styling, so the call is taken only when the styled screen holds exactly one solid rule,
// exactly two dashed rules, the layout right below the rule, a title known for the tool word for word, and the call
// whole. A call the dialog cuts («Read(/a/b…») is never decided from the page: the person decides in the terminal.
var (
	toolSummaryRE = regexp.MustCompile(`^([A-Z][A-Za-z]+) (.+)$`)
	solidRuleRE   = regexp.MustCompile(`^` + sp + `*─{20,}` + sp + `*$`)
	dashedRuleRE  = regexp.MustCompile(`^` + sp + `*╌{20,}` + sp + `*$`)
	ansiRE        = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// ruleStyleRE is the colour a dialog's solid rule is drawn in; the title is bold in the same colour.
	ruleStyleRE = regexp.MustCompile(`^\x1b\[38;5;(\d+)m─`)
)

// dialogTitles are the titles of the permission dialogs Claude 2.1.310 draws for a tool's call, by tool.
var dialogTitles = map[string][]string{
	"Read": {"Read outside the working directories"},
}

// toolCall is the tool and the argument of a summary that names its tool first; false for a shell command's.
func toolCall(summary string) (tool, arg string, ok bool) {
	line, _, _ := strings.Cut(strings.TrimSpace(summary), "\n")
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "$") {
		return "", "", false
	}
	m := toolSummaryRE.FindStringSubmatch(line)
	if m == nil || strings.TrimSpace(m[2]) == "" {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// drawnCall: the styled screen shows Claude's dialog about this very call, in the layout above, and nothing that
// looks like a second one.
func drawnCall(styled, tool, arg string) bool {
	titles := dialogTitles[tool]
	if len(titles) == 0 {
		return false
	}
	raw := strings.Split(styled, "\n")
	plain := make([]string, len(raw))
	rule, rules, dashed := -1, 0, 0
	for i, l := range raw {
		plain[i] = ansiRE.ReplaceAllString(l, "")
		switch {
		case solidRuleRE.MatchString(plain[i]):
			rule, rules = i, rules+1
		case dashedRuleRE.MatchString(plain[i]):
			dashed++
		}
	}
	if rules != 1 || dashed != 2 || rule+4 >= len(raw) {
		return false
	}
	color := ruleStyleRE.FindStringSubmatch(strings.TrimLeft(raw[rule], " "))
	if color == nil || !dashedRuleRE.MatchString(plain[rule+2]) || !dashedRuleRE.MatchString(plain[rule+4]) {
		return false
	}
	title := strings.TrimSpace(plain[rule+1])
	styledTitle := "\x1b[1m\x1b[38;5;" + color[1] + "m" + title + "\x1b[0m"
	if !slices.Contains(titles, title) || !strings.Contains(raw[rule+1], styledTitle) {
		return false
	}
	return strings.TrimSpace(plain[rule+3]) == tool+"("+arg+")"
}

// matches: the screen shows the menu the person decided on.
func (w PickWant) matches(s Screen) error {
	kind := MenuPermission
	if w.Plan {
		kind = MenuPlan
	}
	if !s.Readable || s.Menu != kind {
		return ErrMenuChanged
	}
	label, ok := optionOf(s.Options, w.N)
	if !ok || strings.Join(strings.Fields(label), " ") != strings.Join(strings.Fields(w.Label), " ") {
		return ErrMenuChanged
	}
	if tool, arg, ok := toolCall(w.Command); ok {
		if !drawnCall(s.styled, tool, arg) {
			return ErrMenuChanged
		}
	} else if strings.TrimSpace(w.Command) != "" && !shellDrawn(s.Plain, w.Command, w.Cwd) {
		return ErrMenuChanged
	}
	return nil
}

// Shows reports whether the screen shows the menu the person decided on — the check Pick makes before it presses a key
// (ErrMenuChanged when it does not). The runtime asks it of an open gate as the screen changes, so a card whose menu
// cannot be matched says so at once and not after a press that did nothing.
func Shows(s Screen, want PickWant) error { return want.matches(s) }

// Pick presses the chosen option of a permission or a plan menu and waits for the menu to go. It looks at the screen
// first, under the lock the caller holds: a menu that changed since the person decided (another tool call, other
// options) gets no key, because the number would approve what they did not see.
func Pick(p Pane, c Clock, provider wire.Provider, want PickWant) error {
	if err := want.matches(Look(p, provider)); err != nil {
		return err
	}
	if err := p.Keys(strconv.Itoa(want.N)); err != nil {
		return err
	}
	for waited := time.Duration(0); waited < pickConfirm; waited += confirmPoll {
		c.Sleep(confirmPoll)
		styled, ok := p.CaptureStyled()
		if !ok {
			continue
		}
		if s := Read(styled, provider); s.Menu != MenuPermission && s.Menu != MenuPlan {
			return nil
		}
	}
	return ErrDialogStayed
}

// interruptTries bounds the presses of an Escape whose key did not go in (tmux failing under load), backing off from
// interruptBase to interruptMax between them.
const (
	interruptTries = 10
	interruptBase  = 100 * time.Millisecond
	interruptMax   = time.Second
)

// Interrupt presses Escape once; a key that did not go in is pressed again, interruptTries in all.
func Interrupt(p Pane, c Clock) error {
	var err error
	for try := 0; try < interruptTries; try++ {
		if err = p.Keys("Escape"); err == nil {
			return nil
		}
		c.Sleep(min(interruptBase<<try, interruptMax))
	}
	return err
}

type keyIO struct {
	p Pane
	c Clock
}

func (k keyIO) Key(name string) error  { return k.p.Keys(name) }
func (k keyIO) Text(text string) error { return k.p.Paste(text) }
func (k keyIO) Sleep(d time.Duration)  { k.c.Sleep(d) }

package typist

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

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

// commandKeys are the pieces of a tool call's summary a dialog shows: the start of its first line, and, for a path the
// dialog shortens from the left, its end. A dialog that wraps or truncates a long command still carries one of them.
func commandKeys(summary string) []string {
	line, _, _ := strings.Cut(strings.TrimSpace(summary), "\n")
	rs := []rune(strings.Join(strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "$ ")), " "))
	if len(rs) == 0 {
		return nil
	}
	head := string(rs[:min(len(rs), 40)])
	if len(rs) <= 40 {
		return []string{head}
	}
	return []string{head, string(rs[len(rs)-25:])}
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
	} else if keys := commandKeys(w.Command); len(keys) > 0 {
		plain := strings.Join(strings.Fields(s.Plain), " ")
		if !slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(plain, k) }) {
			return ErrMenuChanged
		}
	}
	return nil
}

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

package typist

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
)

// NativeOwner is checked immediately before each native write. A recognized input
// frame alone is never sufficient: a crashed CLI may have returned the pane to sh.
type NativeOwner interface{ NativeAgentAlive() bool }

var nativeModelFooter = regexp.MustCompile(`(?i)(?:Gemini|Claude|GPT[- ]OSS|Antigravity ·)[^\n]*·\s*(?:low|medium|high|xhigh|max|none)`)

func nativeSameText(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

func readAntigravity(styled string) Screen {
	shared := screen.Read(styled)
	plain := shared.Plain
	s := Screen{Readable: true, Plain: plain, styled: styled}
	if shared.Menu != nil {
		s.Menu = MenuOther
		return s
	}
	lines := strings.Split(plain, "\n")
	bottom := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if boxRule.MatchString(strings.TrimSpace(lines[i])) {
			bottom = i
			break
		}
	}
	if bottom < 2 {
		return s
	}
	footer := strings.Join(lines[bottom+1:], "\n")
	if !strings.Contains(footer, "? for shortcuts") && !nativeModelFooter.MatchString(footer) {
		return s
	}
	lower := strings.ToLower(footer)
	if strings.Contains(lower, "esc") && (strings.Contains(lower, "interrupt") || strings.Contains(lower, "stop") || strings.Contains(lower, "cancel")) {
		s.Busy = true
		return s
	}
	top := -1
	for i := bottom - 1; i >= 0; i-- {
		if boxRule.MatchString(strings.TrimSpace(lines[i])) {
			top = i
			break
		}
	}
	if top < 0 || top+1 >= bottom {
		return s
	}
	first := strings.TrimRight(lines[top+1], " \t")
	var text string
	switch {
	case first == ">":
		text = ""
	case strings.HasPrefix(first, "> "):
		text = strings.TrimPrefix(first, "> ")
	default:
		return s
	}
	for _, line := range lines[top+2 : bottom] {
		if strings.TrimSpace(line) == "" {
			text += "\n"
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			return s
		}
		text += "\n" + strings.TrimPrefix(line, "  ")
	}
	s.InputLine = true
	s.Input = strings.TrimRight(text, "\n")
	s.Box = s.Input
	return s
}

func nativePlan(m Message, s Screen) Plan {
	if len(m.Images) > 0 || m.Steer || m.HandOver || m.ClearOnly || m.Wipe || !s.PromptReady() {
		return Plan{Decision: DecideWait}
	}
	if s.Input != "" {
		if !nativeSameText(s.Input, m.Text) {
			return Plan{Decision: DecideBlockedText}
		}
		return Plan{Decision: DecideSend, OwnText: true}
	}
	if m.EnterOnly {
		return Plan{Decision: DecideWait}
	}
	return Plan{Decision: DecideSend}
}

func typeAntigravity(m Message, p Pane, c Clock) Result {
	owner, ok := p.(NativeOwner)
	if !ok || !owner.NativeAgentAlive() {
		return Result{Outcome: Wait}
	}
	view := Look(p, "antigravity")
	res := Result{Before: view}
	plan := nativePlan(m, view)
	if plan.Decision != DecideSend {
		res.Outcome = Wait
		if plan.Decision == DecideBlockedText {
			res.Outcome = BlockedText
		}
		return res
	}
	if !plan.OwnText {
		if !owner.NativeAgentAlive() {
			res.Outcome = Wait
			return res
		}
		if err := p.Paste(m.Text); err != nil {
			res.Outcome = Failed
			res.Err = err
			return res
		}
		res.Typed = true
		// Native bracketed paste is read asynchronously; unlike Claude, do not
		// dismiss menus or retry Enter when its acceptance is uncertain.
		c.Sleep(300 * time.Millisecond)
		ready := Look(p, "antigravity")
		if !ready.PromptReady() || !nativeSameText(ready.Box, m.Text) {
			res.Outcome = Wait
			return res
		}
	}
	if !owner.NativeAgentAlive() {
		res.Outcome = Failed
		res.Err = errors.New("native agent no longer owns the pane")
		return res
	}
	if err := p.Keys("Enter"); err != nil {
		res.Outcome = Failed
		res.Err = err
		return res
	}
	res.Typed = true
	res.Own = plan.OwnText
	res.Outcome = Sent
	// Sent is provisional; the outbox confirms the actual USER_INPUT record.
	return res
}

package status

import (
	"math"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// pollChecks runs the poll's checks on an existing tab in order; true when one of them changed the tab's state, which
// ends the tab's poll step.
func (m *Manager) pollChecks(tabID string, e *Entry, p Provider, pane *tmux.PaneInfo, now float64, agentRunning func() bool) bool {
	m.mu.Lock()
	state, lastAt, hasEvent := e.CliState, e.lastEventAt(), e.LastEvent.Has()
	m.mu.Unlock()

	// A busy tab quiet for ten minutes whose agent is gone by two looks.
	if state == Busy && hasEvent && now-lastAt > ms(busyStuck) {
		if !agentRunning() {
			if fresh := m.probeRunning(tabID); fresh != nil && !*fresh {
				m.d.Log("info", "busy stuck — agent process gone, forcing idle", "tabId", tabID)
				d := now - lastAt
				m.mismatch(Mismatch{Kind: "busy-without-agent", Shown: Busy, Actual: Idle, Provider: idOf(p), DurationMs: &d})
				m.mu.Lock()
				if e.CliState == Busy {
					m.applyCliState(tabID, e, Idle, applyOpts{silent: true})
					m.persistToLayout(e)
					m.broadcastUpdate(tabID, e, nil)
				}
				m.mu.Unlock()
				return true
			}
		}
	}

	m.mu.Lock()
	checkHooks := p != nil && p.ID() == agents.Claude && pane != nil && pane.PID != 0 && (e.CliState == Busy || e.CliState == Unknown || e.HooksMissing)
	m.mu.Unlock()
	if checkHooks {
		m.refreshHooksPresence(tabID, e, p, pane.PID, now)
	}

	m.mu.Lock()
	recheck := ms(stuckRecheck)
	if e.StuckBackoffMs > 0 {
		recheck = e.StuckBackoffMs
	}
	if e.HooksMissing {
		recheck = ms(stuckUnhookedRecheck)
	}
	candidate := p != nil && stuckCandidate(e) && now-e.StuckCheckedAt >= recheck
	var quietEnough bool
	if candidate {
		e.StuckCheckedAt = now
		limit := ms(stuckQuiet)
		if e.HooksMissing {
			limit = ms(stuckUnhookedQuiet)
		}
		quietEnough = e.CliState != Busy || !e.LastEvent.Has() || now-e.lastEventAt() >= limit
	}
	m.mu.Unlock()
	if candidate && quietEnough {
		m.mu.Lock()
		before := e.CliState
		m.mu.Unlock()
		decision := m.checkStuck(tabID, e, false)
		m.mu.Lock()
		if decision == "" && e.CliState == Unknown && e.PanelType.Value() == agents.CodexPanel {
			cur := e.StuckBackoffMs
			if cur == 0 {
				cur = ms(stuckRecheck)
			}
			e.StuckBackoffMs = math.Min(cur*2, ms(stuckBackoffMax))
		}
		changed := e.CliState != before
		m.mu.Unlock()
		if changed {
			return true
		}
	}

	// Gone: three witnesses must agree (shell in the foreground, no agent in the poll's look, none in a fresh one).
	m.mu.Lock()
	guarded := p != nil && (e.CliState == Busy || e.CliState == Idle || e.CliState == NeedsInput || e.CliState == ReadyForReview)
	inGrace := e.LastResumeOrStartedAt != nil && now-*e.LastResumeOrStartedAt < ms(agentLaunchGrace)
	m.mu.Unlock()
	foreground := ""
	if pane != nil {
		foreground = pane.Command
	}
	if guarded && !inGrace && shellForeground(foreground) && !agentRunning() {
		fresh := m.probeRunning(tabID)
		if mayMarkGone(foreground, false, fresh) {
			m.mu.Lock()
			m.d.Log("info", "agent process gone — transitioning to inactive", "tabId", tabID, "prevState", e.CliState)
			m.applyCliState(tabID, e, Inactive, applyOpts{silent: true})
			g := m.now()
			e.GoneSince = &g
			m.persistToLayout(e)
			m.broadcastUpdate(tabID, e, nil)
			m.mu.Unlock()
			return true
		}
		if fresh != nil && *fresh {
			m.mu.Lock()
			shown := e.CliState
			m.mu.Unlock()
			m.mismatch(Mismatch{Kind: "gone-refuted", Shown: shown, Actual: shown, Provider: idOf(p)})
		}
	}

	m.mu.Lock()
	heal := p != nil && p.ID() == agents.Claude && e.CliState == Inactive && e.LaunchingSince == nil
	codexReady := p != nil && e.CliState == Inactive && e.PanelType.Value() == agents.CodexPanel
	session := e.TmuxSession
	m.mu.Unlock()
	if heal && !shellForeground(foreground) && agentRunning() && m.healInactive(tabID, e, p) {
		return true
	}
	if codexReady && agentRunning() {
		// Codex fires SessionStart only after the first prompt: a ready composer moves the tab to idle meanwhile.
		if content, ok := m.d.Tmux.CaptureAtWidth(session, 80, 24); ok && content != "" && screen.IsCodexComposerReady(content) {
			m.updateFromHook(session, EventSessionStart, "")
			return true
		}
	}
	return false
}

func idOf(p Provider) string {
	if p == nil {
		return ""
	}
	return p.ID()
}

func (m *Manager) mismatch(x Mismatch) {
	if m.d.Mismatch != nil {
		m.d.Mismatch(x)
	}
}

// probeRunning is a fresh look at a tab's agent right before telling the person it is gone; nil when tmux did not
// answer, so nothing is decided on a failed look.
func (m *Manager) probeRunning(tabID string) *bool {
	m.mu.Lock()
	e := m.tabs[tabID]
	if e == nil {
		m.mu.Unlock()
		return nil
	}
	p, session := m.provider(e), e.TmuxSession
	m.mu.Unlock()
	if p == nil {
		return nil
	}
	panes, ok := m.d.Tmux.ReadAllPanes()
	if !ok {
		return nil
	}
	r := false
	if pane, has := panes[session]; has && pane.PID != 0 {
		r = p.Running(pane.PID, m.d.Procs.Children(pane.PID))
	}
	return &r
}

// refreshHooksPresence: whether the Claude in the tab runs with the platform's hooks, looked up again when the pane's
// processes change (at most every 30 s). Without them its status comes from the screen and the transcript only.
func (m *Manager) refreshHooksPresence(tabID string, e *Entry, p Provider, panePID int, now float64) {
	m.mu.Lock()
	if e.HooksCheckedAt != nil && now-*e.HooksCheckedAt < ms(hooksRecheck) {
		m.mu.Unlock()
		return
	}
	e.HooksCheckedAt = &now
	m.mu.Unlock()
	children := m.d.Procs.Children(panePID)
	sig := joinInts(children)
	m.mu.Lock()
	same := e.HooksCheckedFor != nil && *e.HooksCheckedFor == sig
	m.mu.Unlock()
	if same {
		return
	}
	hooked, found := p.RunsWithHooks(children)
	if !found {
		return // no Claude among the children yet: look again next time
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e.HooksCheckedFor = &sig
	if e.HooksMissing == !hooked {
		return
	}
	e.HooksMissing = !hooked
	if e.HooksMissing {
		m.d.Log("info", "claude runs without the platform hooks — status from screen and transcript", "tabId", tabID)
	}
	m.broadcastUpdate(tabID, e, nil)
}

func joinInts(xs []int) string {
	var b strings.Builder
	for i, x := range xs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(itoa(x))
	}
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}

func stuckCandidate(e *Entry) bool {
	if e.CliState == Unknown {
		return e.PanelType.Value() == agents.CodexPanel || e.JSONLPath != ""
	}
	return e.CliState == Busy && e.PanelType.Value() != agents.CodexPanel && e.JSONLPath != ""
}

// readStuck reads the two witnesses: a plain capture (the watchdog never resizes a pane someone looks at) and the
// transcript. ok false when the transcript could not be read.
func (m *Manager) readStuck(e *Entry, now float64) (stuckInput, bool) {
	m.mu.Lock()
	state, session, panel, transcript, hooksMissing, lastAt := e.CliState, e.TmuxSession, e.PanelType.Value(), e.JSONLPath, e.HooksMissing, e.lastEventAt()
	p := m.provider(e)
	m.mu.Unlock()
	in := stuckInput{state: state}
	if d := m.d.Tmux.PaneDetail(session); d.Width >= stuckMinCols && d.Height >= stuckMinRows {
		if content, ok := m.d.Tmux.Capture(session); ok {
			in.screen = &stuckScreen{busy: screen.IsBusyAnywhere(content), menu: screen.FindMenu(content) != nil,
				promptReady: panel != agents.CodexPanel && screen.IsPromptBoxReady(content)}
		}
	}
	var lastEntry float64
	if p != nil && transcript != "" {
		s, err := p.Snapshot(transcript, false)
		if err != nil {
			return in, false
		}
		in.transcript = &stuckTranscript{idle: s.Idle, stale: s.Stale, hasAnswer: s.LastAssistantSnippet != nil}
		if s.LastEntryTs != nil {
			lastEntry = *s.LastEntryTs
		}
	}
	in.quiet = time.Duration((now - math.Max(lastAt, lastEntry)) * float64(time.Millisecond))
	if hooksMissing {
		in.quietLimit = stuckUnhookedQuiet
	}
	return in, true
}

// checkStuck decides on a stuck tab and applies the decision; a hook or state change during the reads discards it.
func (m *Manager) checkStuck(tabID string, e *Entry, ignoreQuiet bool) string {
	m.mu.Lock()
	beforeState, beforeSeq := e.CliState, e.lastEventSeq()
	m.mu.Unlock()
	in, ok := m.readStuck(e, m.now())
	if !ok {
		return ""
	}
	if ignoreQuiet {
		in.quiet = time.Duration(math.MaxInt64)
	}
	decision := decideStuck(in)
	m.mu.Lock()
	defer m.mu.Unlock()
	if decision == "" || e.CliState != beforeState || e.lastEventSeq() != beforeSeq || m.tabs[tabID] != e {
		return ""
	}
	m.transitionFromScreen(tabID, e, decision)
	return decision
}

// transitionFromScreen applies what the screen showed. Called with the lock held.
func (m *Manager) transitionFromScreen(tabID string, e *Entry, decision string) {
	if decision == NeedsInput {
		e.EventSeq++
		e.LastEvent = Some(LastEvent{Name: EventNotification, At: m.now(), Seq: e.EventSeq})
	}
	m.d.Log("info", "stuck "+e.CliState+"→"+decision, "tabId", tabID)
	if e.CliState == Busy || e.CliState == Unknown {
		var d *float64
		if at := e.lastEventAt(); at != 0 {
			x := m.now() - at
			d = &x
		}
		go m.mismatch(Mismatch{Kind: "stuck-settled", Shown: e.CliState, Actual: decision, Provider: e.AgentProviderID.Value(), DurationMs: d, Unhooked: e.HooksMissing})
	}
	e.StuckBackoffMs = 0
	m.applyCliState(tabID, e, decision, applyOpts{silent: true})
	m.persistToLayout(e)
	m.broadcastUpdate(tabID, e, nil)
}

// RecheckStuck runs the watchdog on a tab now, whatever its quiet time (after a command ran in it).
func (m *Manager) RecheckStuck(tabID string) string {
	m.mu.Lock()
	e := m.tabs[tabID]
	ok := e != nil && stuckCandidate(e)
	m.mu.Unlock()
	if !ok {
		return ""
	}
	return m.checkStuck(tabID, e, true)
}

var menuCapable = map[string]bool{Busy: true, Unknown: true, Idle: true, ReadyForReview: true}

// MarkNeedsInputFromScreen: after a command left a menu open, the agent waits for an answer whatever state it was in.
func (m *Manager) MarkNeedsInputFromScreen(tabID string) bool {
	m.mu.Lock()
	e := m.tabs[tabID]
	if e == nil || !menuCapable[e.CliState] {
		m.mu.Unlock()
		return false
	}
	session := e.TmuxSession
	m.mu.Unlock()
	d := m.d.Tmux.PaneDetail(session)
	if d.Width < stuckMinCols || d.Height < stuckMinRows {
		return false
	}
	content, ok := m.d.Tmux.Capture(session)
	if !ok || content == "" || screen.FindMenu(content) == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tabs[tabID] != e || !menuCapable[e.CliState] {
		return false
	}
	m.transitionFromScreen(tabID, e, NeedsInput)
	return true
}

// RecoverUnknown: an unknown tab whose screen shows a permission prompt is waiting for an answer.
func (m *Manager) RecoverUnknown(tabID string) (bool, string) {
	m.mu.Lock()
	e := m.tabs[tabID]
	if e == nil {
		m.mu.Unlock()
		return false, "no-entry"
	}
	if e.CliState != Unknown {
		m.mu.Unlock()
		return false, "not-unknown"
	}
	session := e.TmuxSession
	m.mu.Unlock()
	content, ok := m.d.Tmux.CaptureAtWidth(session, 120, 50)
	if !ok || content == "" {
		return false, "capture-failed"
	}
	if len(screen.ParsePermissionOptions(content).Options) == 0 {
		return false, "no-options"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tabs[tabID] != e {
		return false, "no-entry"
	}
	e.EventSeq++
	e.LastEvent = Some(LastEvent{Name: EventNotification, At: m.now(), Seq: e.EventSeq})
	m.applyCliState(tabID, e, NeedsInput, applyOpts{silent: true})
	m.persistToLayout(e)
	m.broadcastUpdate(tabID, e, nil)
	return true, ""
}

// healInactive: an inactive Claude tab whose agent runs (a wrong "gone", or an idle agent through a restart) gets back
// the state its transcript shows.
func (m *Manager) healInactive(tabID string, e *Entry, p Provider) bool {
	m.mu.Lock()
	transcript, seq := e.JSONLPath, e.EventSeq
	m.mu.Unlock()
	if transcript == "" {
		return false
	}
	s, err := p.Snapshot(transcript, false)
	if err != nil {
		return false
	}
	m.mu.Lock()
	if e.CliState != Inactive || e.EventSeq != seq || m.tabs[tabID] != e {
		m.mu.Unlock()
		return false
	}
	now := m.now()
	turnRunning := !s.Idle && !s.Stale && s.LastEntryTs != nil && now-*s.LastEntryTs < ms(healTurnFresh)
	next := healedState(turnRunning, e.lastEventName(), e.DismissedAt.Has() && e.DismissedAt.Value() != 0)
	gone := e.GoneSince
	e.GoneSince = nil
	m.d.Log("info", "agent found running in an inactive tab", "tabId", tabID, "next", next)
	if gone != nil {
		d := now - *gone
		go m.mismatch(Mismatch{Kind: "gone-while-running", Shown: Inactive, Actual: next, Provider: p.ID(), DurationMs: &d})
	}
	if next == Busy {
		session := e.TmuxSession
		m.mu.Unlock()
		m.updateFromHook(session, EventPromptSubmit, "")
		return true
	}
	m.applyCliState(tabID, e, next, applyOpts{silent: true, restored: true})
	m.persistToLayout(e)
	m.broadcastUpdate(tabID, e, nil)
	m.mu.Unlock()
	return true
}

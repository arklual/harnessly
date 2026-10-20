package status

import (
	"encoding/json"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/background"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/sockets"
)

type applyOpts struct {
	silent bool // no web push
	// restored: the state the agent was already in before a wrong inactive, not a turn that ended now.
	restored bool
}

// applyCliState moves a tab to a new state with its side effects, in the old order. Callers persist and broadcast.
// Called with the lock held.
func (m *Manager) applyCliState(tabID string, e *Entry, next string, o applyOpts) {
	prev := e.CliState
	if prev == next {
		return
	}
	prevBusySince := e.BusySince
	now := m.now()
	e.CliState = next
	e.ReadyForReviewAt, e.BusySince = Null[float64](), Null[float64]()
	if next == ReadyForReview {
		e.ReadyForReviewAt = Some(now)
	}
	if next == Busy {
		e.BusySince = Some(now)
		e.DismissedAt = Null[float64]()
		// A new turn starts with nothing in hand: the previous reply shown under busy reads as the agent repeating itself.
		e.CurrentAction = Null[agents.CurrentAction]()
	}
	if (next == ReadyForReview || next == Idle) && m.d.Queue != nil {
		m.d.Queue.Schedule(tabID, queueSettle)
	}
	if prev == NeedsInput && e.PermissionRequest.Has() {
		e.PermissionRequest = Null[json.RawMessage]()
	}
	if next == ReadyForReview && e.JSONLPath != "" && !o.restored {
		time.AfterFunc(historySaveDelay, func() { m.saveHistory(tabID, e, prevBusySince, false) })
	}
	if turnEnd(prev, next) && (e.PanelType.Value() == agents.ClaudePanel || e.PanelType.Value() == agents.CodexPanel) && m.d.TurnEnd != nil {
		session := e.TmuxSession
		time.AfterFunc(turnEndSnapshotDelay, func() { m.d.TurnEnd(tabID, m.d.Tmux.SessionCwd(session)) })
	}
	if !o.silent && (next == ReadyForReview || next == NeedsInput) {
		go m.push(tabID, e, next)
	}
	// Idle tabs stay watched too: a turn started by a background task or another session fires no prompt hook.
	if e.JSONLPath != "" && next != Inactive {
		m.watch.start(tabID, e.JSONLPath)
	} else if next == Inactive {
		m.watch.stop(tabID)
	}
	m.touchBackground(tabID, e, true)
}

// push waits for the transcript to have the answer or the question, then sends a web push if the tab is still there.
func (m *Manager) push(tabID string, e *Entry, state string) {
	if m.d.Push == nil {
		return
	}
	time.Sleep(pushSettle)
	m.mu.Lock()
	still := e.CliState == state
	snapshot := *e
	m.mu.Unlock()
	if !still {
		return
	}
	kind := "done"
	if state == NeedsInput {
		kind = "question"
	}
	m.d.Push.Push(tabID, snapshot, kind)
}

func (m *Manager) saveHistory(tabID string, e *Entry, prevBusySince Opt[float64], cancelled bool) {
	if m.d.History == nil {
		return
	}
	m.mu.Lock()
	lastUser := e.LastUserMessage
	if !lastUser.Has() || lastUser.Value() == "" {
		m.mu.Unlock()
		return
	}
	providerID := e.AgentProviderID.Value()
	p := m.d.Providers[providerID]
	if p == nil {
		p = m.provider(e)
	}
	transcript, wsID, sessionID := e.JSONLPath, e.WorkspaceID, e.AgentSessionID
	m.mu.Unlock()
	var stats *agents.HistoryStats
	if transcript != "" && p != nil {
		stats, _ = p.HistoryStats(transcript)
	}
	var ws *Workspace
	for _, w := range m.d.Layouts.Workspaces() {
		if w.ID == wsID {
			w := w
			ws = &w
			break
		}
	}
	now := m.now()
	startedAt := now
	if prevBusySince.Has() {
		startedAt = prevBusySince.Value()
	}
	if stats != nil && stats.FirstUserTs != nil {
		startedAt = *stats.FirstUserTs
	}
	completedAt := now
	if !cancelled && stats != nil && stats.LastAssistantTs != nil {
		completedAt = *stats.LastAssistantTs
	}
	duration := completedAt - startedAt
	if !cancelled && stats != nil && stats.TurnDurationMs != nil {
		duration = *stats.TurnDurationMs
	}
	entry := history.Entry{TabID: tabID, WorkspaceID: wsID, WorkspaceName: wsID, ProviderID: agents.Claude,
		StartedAt: history.Number(startedAt), CompletedAt: history.Number(completedAt), Duration: history.Number(duration),
		ToolUsage: json.RawMessage("{}"), TouchedFiles: json.RawMessage("[]"), Cancelled: cancelled}
	if m.d.NewID != nil {
		entry.ID = m.d.NewID()
	}
	if providerID == agents.Codex {
		entry.ProviderID = agents.Codex
	}
	if ws != nil {
		entry.WorkspaceName = ws.Name
		if len(ws.Directories) > 0 {
			entry.WorkspaceDir = &ws.Directories[0]
		}
	}
	if sessionID.Has() {
		id := sessionID.Value()
		entry.AgentSessionID = &id
	}
	prompt := lastUser.Value()
	entry.Prompt = &prompt
	if stats != nil {
		if stats.LastUserText != nil {
			entry.Prompt = stats.LastUserText
		}
		entry.Result = stats.LastAssistantText
		if raw, err := json.Marshal(stats.ToolUsage); err == nil {
			entry.ToolUsage = raw
		}
		if stats.TouchedFiles != nil {
			raw, _ := json.Marshal(stats.TouchedFiles)
			entry.TouchedFiles = raw
		}
	}
	// The old runtime stored the turn as already dismissed (completedAt), so a later dismissal never found it; kept
	// that way for now, the page's notification list relies on it.
	dismissed := history.Number(completedAt)
	entry.DismissedAt = &dismissed
	if err := m.d.History.Add(entry); err != nil {
		m.d.Log("warn", "failed to save session history", "err", err)
		return
	}
	m.broadcast(map[string]any{"type": "session-history:update", "entry": entry.Clip()}, nil)
}

// Dismiss: the person opened the finished answer. A page's own socket gets no echo of it; the relay does.
func (m *Manager) Dismiss(tabID string, exclude *sockets.Client) {
	m.mu.Lock()
	e := m.tabs[tabID]
	if e == nil || e.CliState != ReadyForReview {
		m.mu.Unlock()
		return
	}
	at := m.now()
	m.applyCliState(tabID, e, Idle, applyOpts{silent: true})
	e.DismissedAt = Some(at)
	m.persistToLayout(e)
	m.broadcastUpdate(tabID, e, exclude)
	m.mu.Unlock()
	if m.d.History == nil {
		return
	}
	go func() {
		if updated, err := m.d.History.Dismiss(tabID, at); err == nil && updated != nil {
			m.broadcast(map[string]any{"type": "session-history:update", "entry": updated.Clip()}, nil)
		}
	}()
}

// Ack: the page answered the question its last notification asked.
func (m *Manager) Ack(tabID string, seq float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tabs[tabID]
	if e == nil || e.CliState != NeedsInput || e.lastEventName() != EventNotification || float64(e.lastEventSeq()) != seq {
		return
	}
	m.applyCliState(tabID, e, Busy, applyOpts{})
	m.persistToLayout(e)
	m.broadcastUpdate(tabID, e, nil)
}

// IntentsApplied and Warn complete the status socket's source; pane intents are wired by the caller.
func (m *Manager) Warn(msg string) { m.d.Log("warn", msg) }

var workStates = map[string]bool{EventSessionStart: true, EventPromptSubmit: true, EventNotification: true, EventStop: true, EventStopFailure: true, EventInterrupt: true}

// updateFromHook applies an event (a hook, or one the keeper stands in for) to the tab of a tmux session.
func (m *Manager) updateFromHook(session, event, notificationType string) {
	m.mu.Lock()
	tabID, e := m.findBySession(session)
	if e == nil {
		m.mu.Unlock()
		return
	}
	if event == EventPreCompact || event == EventPostCompact {
		var since *float64
		if event == EventPreCompact {
			n := m.now()
			since = &n
		}
		m.setCompacting(tabID, e, since)
		m.mu.Unlock()
		return
	}
	if !workStates[event] || event == EventNotification && notificationType != "" && !agents.NotificationNeedsInput(notificationType) {
		m.mu.Unlock()
		return
	}
	now := m.now()
	e.EventSeq++
	e.LastEvent = Some(LastEvent{Name: event, At: now, Seq: e.EventSeq})
	if event == EventSessionStart {
		e.LastResumeOrStartedAt = &now
	}
	msg := orderedObject{{"type", "status:hook-event"}, {"tabId", tabID}, {"event", *e.LastEvent.Val}}
	if event == EventPromptSubmit && m.d.PromptEnter != nil {
		if accept, ok := m.d.PromptEnter(session, now); ok {
			msg = append(msg, kv{"acceptMs", accept})
		}
	}
	m.broadcast(msg, nil)
	if event == EventSessionStart && e.LaunchingSince != nil {
		m.launchEnded(tabID, e)
	}
	prev := e.CliState
	next := deriveState(event, prev)
	if prev != next {
		m.applyCliState(tabID, e, next, applyOpts{})
		m.persistToLayout(e)
		m.broadcastUpdate(tabID, e, nil)
	} else {
		m.persistToLayout(e)
		m.touchBackground(tabID, e, true)
	}
	resolve := (next == Busy || next == NeedsInput) && e.JSONLPath == ""
	refresh := (event == EventStop || event == EventStopFailure) && e.JSONLPath != ""
	transcript, p := e.JSONLPath, m.provider(e)
	m.mu.Unlock()
	if resolve {
		go m.resolveTranscript(tabID, session)
	}
	if refresh && p != nil {
		go m.refreshSnippet(tabID, e, p, transcript, false)
		time.AfterFunc(500*time.Millisecond, func() { m.refreshSnippet(tabID, e, p, transcript, true) })
	}
}

// refreshSnippet reads the last answer and action after a stop.
func (m *Manager) refreshSnippet(tabID string, e *Entry, p Provider, transcript string, force bool) {
	s, err := p.Snapshot(transcript, force)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.applySnapshot(e, s) && m.tabs[tabID] == e {
		m.broadcastUpdate(tabID, e, nil)
	}
}

// applySnapshot takes the last answer and action from a transcript snapshot; true when they changed. Called with the
// lock held.
func (m *Manager) applySnapshot(e *Entry, s agents.Snapshot) bool {
	changed := false
	if s.Reset {
		if !e.CurrentAction.Set || e.CurrentAction.Has() {
			e.CurrentAction, changed = Null[agents.CurrentAction](), true
		}
		if !e.LastAssistantMessage.Set || e.LastAssistantMessage.Has() {
			e.LastAssistantMessage, changed = Null[string](), true
		}
		return changed
	}
	if s.CurrentAction != nil && (!e.CurrentAction.Has() || s.CurrentAction.Summary != e.CurrentAction.Val.Summary) {
		e.CurrentAction, changed = Some(*s.CurrentAction), true
	}
	if s.LastAssistantSnippet != nil && !optEq(e.LastAssistantMessage, Some(*s.LastAssistantSnippet)) {
		e.LastAssistantMessage, changed = Some(*s.LastAssistantSnippet), true
	}
	return changed
}

// HandleProviderEvent is a hook's event. firedAt (ms) is when it fired on the machine: the hook reports in the
// background, so two reports can arrive out of order, and one fired before the last applied one is dropped.
func (m *Manager) HandleProviderEvent(providerID, session string, ev agents.Event, firedAt *float64) bool {
	m.mu.Lock()
	tabID, e := m.findBySession(session)
	if e == nil {
		m.mu.Unlock()
		return false
	}
	if expected := providerOfPanel(e.PanelType); expected != "" && expected != providerID {
		m.mu.Unlock()
		return false
	}
	if firedAt != nil {
		if e.LastHookFiredAt != nil && *firedAt < *e.LastHookFiredAt {
			m.mu.Unlock()
			return false
		}
		f := *firedAt
		e.LastHookFiredAt = &f
	}
	// A real hook proves the platform's hooks are there; the keeper's own events never come this way.
	if e.HooksMissing {
		e.HooksMissing = false
		m.broadcastUpdate(tabID, e, nil)
	}
	switch ev.Kind {
	case agents.SummaryUpdate:
		next := Null[string]()
		if ev.Summary != nil {
			next = Some(*ev.Summary)
		}
		if !optEq(e.AgentSummary, next) {
			e.AgentSummary = next
			m.broadcastUpdate(tabID, e, nil)
		}
		m.mu.Unlock()
	case agents.LastUserMsg:
		if !optEq(e.LastUserMessage, Some(ev.Message)) {
			e.LastUserMessage = Some(ev.Message)
			m.broadcastUpdate(tabID, e, nil)
		}
		m.mu.Unlock()
	default:
		m.mu.Unlock()
		m.updateFromHook(session, string(ev.Kind), ev.NotificationType)
	}
	return true
}

// SynthesizeEvent applies an event the runtime inferred itself (a resumed agent whose SessionStart hook never came) the
// way a hook's event is applied, without what only a real hook proves (the hook order, the hooks being there).
func (m *Manager) SynthesizeEvent(session, event string) { m.updateFromHook(session, event, "") }

// HookMeta is what a Codex hook says about the conversation; a nil field is not mentioned.
type HookMeta struct {
	SessionID, JSONLPath, LastUserMessage, AgentSummary *Opt[string]
	PermissionRequest                                   *json.RawMessage
	ClearMessages                                       bool
}

// ApplyHookMeta takes a Codex hook's conversation details; ok false for an unknown session.
func (m *Manager) ApplyHookMeta(providerID, session string, meta HookMeta) (tabID, cliState string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tabID, e := m.findBySession(session)
	if e == nil {
		return "", "", false
	}
	if expected := providerOfPanel(e.PanelType); expected != "" && expected != providerID {
		return "", "", false
	}
	changed := false
	if !optEq(e.AgentProviderID, Some(providerID)) {
		e.AgentProviderID, changed = Some(providerID), true
	}
	if meta.SessionID != nil && !optEq(e.AgentSessionID, *meta.SessionID) {
		e.AgentSessionID, changed = *meta.SessionID, true
	}
	if meta.JSONLPath != nil && e.JSONLPath != meta.JSONLPath.Value() {
		e.JSONLPath, changed = meta.JSONLPath.Value(), true
		m.touchBackground(tabID, e, true)
	}
	if meta.ClearMessages {
		for _, f := range []*Opt[string]{&e.AgentSummary, &e.LastUserMessage, &e.LastAssistantMessage} {
			if !f.Set || f.Has() {
				*f, changed = Null[string](), true
			}
		}
	}
	if meta.LastUserMessage != nil && !optEq(e.LastUserMessage, *meta.LastUserMessage) {
		e.LastUserMessage, changed = *meta.LastUserMessage, true
	}
	if meta.AgentSummary != nil && !optEq(e.AgentSummary, *meta.AgentSummary) {
		e.AgentSummary, changed = *meta.AgentSummary, true
	}
	if meta.PermissionRequest != nil && (!e.PermissionRequest.Has() || string(*e.PermissionRequest.Val) != string(*meta.PermissionRequest)) {
		e.PermissionRequest, changed = Some(*meta.PermissionRequest), true
	}
	if changed {
		var sid, path, summary, last Opt[string]
		if meta.SessionID != nil {
			sid = *meta.SessionID
		}
		if meta.JSONLPath != nil {
			path = *meta.JSONLPath
		}
		if meta.AgentSummary != nil || meta.ClearMessages {
			summary = Null[string]()
			if !meta.ClearMessages && meta.AgentSummary != nil {
				summary = *meta.AgentSummary
			}
		}
		if meta.LastUserMessage != nil || meta.ClearMessages {
			last = Null[string]()
			if !meta.ClearMessages && meta.LastUserMessage != nil {
				last = *meta.LastUserMessage
			}
		}
		m.storeAgentState(e.TmuxSession, providerID, sid, path, summary, last)
		m.persistToLayout(e)
		m.broadcastUpdate(tabID, e, nil)
	}
	return tabID, e.CliState, true
}

// setCompacting marks the context compaction under way; it clears itself after a minute. Called with the lock held.
func (m *Manager) setCompacting(tabID string, e *Entry, since *float64) {
	if t := m.compactTimers[tabID]; t != nil {
		t.Stop()
		delete(m.compactTimers, tabID)
	}
	cur := e.CompactingSince
	if since == nil && !cur.Has() || since != nil && cur.Has() && *since == cur.Value() {
		return
	}
	if since == nil {
		e.CompactingSince = Null[float64]()
	} else {
		e.CompactingSince = Some(*since)
	}
	m.broadcastUpdate(tabID, e, nil)
	if since != nil {
		at := *since
		m.compactTimers[tabID] = time.AfterFunc(compactStale, func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			delete(m.compactTimers, tabID)
			if cur := m.tabs[tabID]; cur == nil || !cur.CompactingSince.Has() || cur.CompactingSince.Value() != at {
				return
			}
			e.CompactingSince = Null[float64]()
			m.broadcastUpdate(tabID, e, nil)
		})
	}
}

var agentPanels = map[string]bool{agents.ClaudePanel: true, agents.CodexPanel: true}

// touchBackground asks the feed to look at an agent tab again. Called with the lock held.
func (m *Manager) touchBackground(tabID string, e *Entry, event bool) {
	if !agentPanels[e.PanelType.Value()] {
		return
	}
	if e.CliState == Inactive {
		go m.feed.Remove(tabID)
		return
	}
	src := background.Source{TmuxSession: e.TmuxSession, Transcript: e.JSONLPath, Provider: e.AgentProviderID.Value()}
	go m.feed.Touch(tabID, src, event)
}

// syncBackground drops tabs that are gone from the feed and touches those it reads under an old source. Called with the
// lock held.
func (m *Manager) syncBackground() {
	for _, id := range m.feed.TabIDs() {
		if m.tabs[id] == nil {
			go m.feed.Remove(id)
		}
	}
	for _, id := range m.order {
		e := m.tabs[id]
		known, ok := m.feed.SourceOf(id)
		stale := !ok || known.Transcript != e.JSONLPath || known.TmuxSession != e.TmuxSession || known.Provider != e.AgentProviderID.Value()
		if stale || e.CliState == Inactive {
			m.touchBackground(id, e, true)
		}
	}
}

// RemoveTab forgets a closed tab; a turn cut short goes to the history as cancelled.
func (m *Manager) RemoveTab(tabID string) {
	m.mu.Lock()
	e := m.tabs[tabID]
	if e != nil && (e.CliState == Busy || e.CliState == NeedsInput) && e.LastUserMessage.Has() && e.LastUserMessage.Value() != "" {
		busy := e.BusySince
		go m.saveHistory(tabID, e, busy, true)
	}
	m.watch.stop(tabID)
	go m.feed.Remove(tabID)
	if t := m.compactTimers[tabID]; t != nil {
		t.Stop()
		delete(m.compactTimers, tabID)
	}
	m.del(tabID)
	m.broadcastRemove(tabID)
	m.mu.Unlock()
	if m.d.TabRemoved != nil {
		m.d.TabRemoved(tabID)
	}
}

// ReconcileWorkspaceTabs forgets the tabs of a workspace its layout no longer has.
func (m *Manager) ReconcileWorkspaceTabs(wsID string, valid []string) {
	keep := map[string]bool{}
	for _, id := range valid {
		keep[id] = true
	}
	m.mu.Lock()
	var gone []string
	for _, id := range m.order {
		if m.tabs[id].WorkspaceID == wsID && !keep[id] {
			gone = append(gone, id)
		}
	}
	m.mu.Unlock()
	for _, id := range gone {
		m.RemoveTab(id)
	}
}

// RemoveWorkspaceTabs forgets every tab of a removed workspace.
func (m *Manager) RemoveWorkspaceTabs(wsID string) { m.ReconcileWorkspaceTabs(wsID, nil) }

// RegisterTab adds a tab a route just created.
func (m *Manager) RegisterTab(tabID string, e Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.set(tabID, &e)
	m.broadcastUpdate(tabID, &e, nil)
}

// MarkAgentGone: the agent was killed from the platform; say so now instead of waiting for the poll.
func (m *Manager) MarkAgentGone(tabID string, endLaunch bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tabs[tabID]
	// A kill ends a launch under way; an agent merely seen missing for a moment (a launcher handing over) does not.
	if e != nil && endLaunch && e.LaunchingSince != nil {
		e.LaunchingSince, e.LaunchFailed = nil, false
		m.broadcastUpdate(tabID, e, nil)
	}
	if e == nil || e.CliState == Inactive {
		return
	}
	m.applyCliState(tabID, e, Inactive, applyOpts{silent: true})
	m.persistToLayout(e)
	m.broadcastUpdate(tabID, e, nil)
}

// NotifyLastUserMessage: the timeline saw a new prompt in the tab of a tmux session.
func (m *Manager) NotifyLastUserMessage(session, message string) {
	_, _, tabID, ok := m.d.Tmux.ParseSessionName(session)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tabs[tabID]
	if e == nil || optEq(e.LastUserMessage, Some(message)) {
		return
	}
	e.LastUserMessage = Some(message)
	m.broadcastUpdate(tabID, e, nil)
}

// IsSessionBusy: whether the agent in a tmux session is working (the timeline reads its live reply only then).
func (m *Manager) IsSessionBusy(session string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, e := m.findBySession(session)
	return e != nil && e.CliState == Busy
}

// Interrupt is the Stop button: Escape, then the state settles at once (an agent interrupted before its first output
// writes nothing to its transcript).
func (m *Manager) Interrupt(tabID string) bool {
	m.mu.Lock()
	e := m.tabs[tabID]
	session := ""
	if e != nil {
		session = e.TmuxSession
	}
	m.mu.Unlock()
	if session == "" {
		return false
	}
	_ = m.d.Tmux.SendRawKeys(session, "Escape")
	m.Interrupted(session)
	return true
}

// Interrupted settles the state of a Stop whose Escape was already sent (by the prompt routes): an interrupt hook, so
// the tab goes idle and its queue goes out 700 ms later.
func (m *Manager) Interrupted(session string) { m.updateFromHook(session, EventInterrupt, "") }

// recoverEntrySession gives a Claude tab that lost its session id the one from its transcript or history.
func (m *Manager) recoverEntrySession(tabID string) bool {
	m.mu.Lock()
	e := m.tabs[tabID]
	if e == nil || providerOfPanel(e.PanelType) != agents.Claude {
		m.mu.Unlock()
		return false
	}
	session := e.TmuxSession
	m.mu.Unlock()
	id, ok := m.d.Layouts.RecoverSession(session)
	if !ok {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e.AgentSessionID = Some(id)
	m.broadcastUpdate(tabID, e, nil)
	return true
}

// RecoverSession is recoverEntrySession for the queue.
func (m *Manager) RecoverSession(tabID string) bool { return m.recoverEntrySession(tabID) }

// AckCurrent acknowledges the tab's current question, whatever its number (the shadow mirrors the person's answer).
func (m *Manager) AckCurrent(tabID string) {
	m.mu.Lock()
	e := m.tabs[tabID]
	seq := 0
	if e != nil {
		seq = e.lastEventSeq()
	}
	m.mu.Unlock()
	m.Ack(tabID, float64(seq))
}

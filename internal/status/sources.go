package status

// AgentSource is an agent tab as idle sleep and background activity read it.
type AgentSource struct {
	TabID, TmuxSession, Transcript, Provider, PanelType, CliState, LastEventName, AgentSessionID string
	EventSeq                                                                                     int
	LastEventAt, LastStartedAt                                                                   *float64
}

// AgentSources are the agent tabs.
func (m *Manager) AgentSources() []AgentSource {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AgentSource
	for _, id := range m.order {
		e := m.tabs[id]
		if !agentPanels[e.PanelType.Value()] {
			continue
		}
		s := AgentSource{TabID: id, TmuxSession: e.TmuxSession, Transcript: e.JSONLPath, Provider: e.AgentProviderID.Value(),
			PanelType: e.PanelType.Value(), CliState: e.CliState, LastEventName: e.lastEventName(), AgentSessionID: e.AgentSessionID.Value(),
			EventSeq: e.EventSeq, LastStartedAt: e.LastResumeOrStartedAt}
		if at := e.lastEventAt(); at != 0 {
			s.LastEventAt = &at
		}
		out = append(out, s)
	}
	return out
}

// TabInfo is what the prompt queue reads of a tab.
type TabInfo struct {
	CliState, Provider, PanelType, TmuxSession, Transcript, AgentSessionID string
	Launching, LaunchFailed                                                bool
}

// Tab is the queue's view of a tab; ok false when unknown.
func (m *Manager) Tab(tabID string) (TabInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tabs[tabID]
	if e == nil {
		return TabInfo{}, false
	}
	return TabInfo{CliState: e.CliState, Provider: e.AgentProviderID.Value(), PanelType: e.PanelType.Value(), TmuxSession: e.TmuxSession,
		Transcript: e.JSONLPath, AgentSessionID: e.AgentSessionID.Value(), Launching: e.LaunchingSince != nil && !e.LaunchFailed,
		LaunchFailed: e.LaunchFailed}, true
}

// HooksMissing is whether the agent of a tab runs without the platform's hooks.
func (m *Manager) HooksMissing(tabID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tabs[tabID]
	return e != nil && e.HooksMissing
}

// EntryOfSession is the tab of a tmux session and a copy of its entry; false when the keeper has none.
func (m *Manager) EntryOfSession(session string) (string, Entry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, e := m.findBySession(session)
	if e == nil {
		return "", Entry{}, false
	}
	return id, *e, true
}

// ChatState is where a chat's actor stands, in the keeper's words: CliState empty leaves the state as it is, Event
// names the hook the change stands for ("" none).
type ChatState struct {
	CliState, Event            string
	Transcript, AgentSessionID string
}

// MirrorChat puts the tab of a chat's tmux session where the chat's actor is, with the side effects a change of state
// has (the turn's history, the queue, the background feed); the tab of a session that is no chat's is left alone.
func (m *Manager) MirrorChat(session string, s ChatState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tabID, e := m.findBySession(session)
	if e == nil || !m.chat(e) {
		return
	}
	changed := false
	if s.Transcript != "" && e.JSONLPath != s.Transcript {
		e.JSONLPath = s.Transcript
		changed = true
	}
	if s.AgentSessionID != "" && e.AgentSessionID.Value() != s.AgentSessionID {
		e.AgentSessionID = Some(s.AgentSessionID)
		changed = true
	}
	if s.CliState != "" && s.CliState != e.CliState {
		if s.Event != "" {
			e.EventSeq++
			e.LastEvent = Some(LastEvent{Name: s.Event, At: m.now(), Seq: e.EventSeq})
		}
		m.applyCliState(tabID, e, s.CliState, applyOpts{mirror: true})
		changed = true
	}
	if changed {
		m.persistToLayout(e)
		m.broadcastUpdate(tabID, e, nil)
	}
}

// chat says the entry's tmux session is a chat's, whose actor follows the agent itself. Called with the lock held:
// ChatSession only reads the chats' own index.
func (m *Manager) chat(e *Entry) bool {
	return m.d.ChatSession != nil && e.TmuxSession != "" && m.d.ChatSession(e.TmuxSession)
}

// TabOfSession is the agent tab of a tmux session ("" when none).
func (m *Manager) TabOfSession(session string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		if e := m.tabs[id]; e.TmuxSession == session && agentPanels[e.PanelType.Value()] {
			return id
		}
	}
	return ""
}

// ClientView is what a page reads of a tab: its kind, state, names and workspace.
type ClientView struct {
	TabID, PanelType, CliState, AgentSummary, TabName, WorkspaceID, TmuxSession, Transcript string
	// LastEventAt is 0 when the tab has seen no event.
	LastEventAt float64
}

// ClientViews are all tabs, in the keeper's order.
func (m *Manager) ClientViews() []ClientView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ClientView, 0, len(m.order))
	for _, id := range m.order {
		e := m.tabs[id]
		out = append(out, ClientView{TabID: id, PanelType: e.PanelType.Value(), CliState: e.CliState, AgentSummary: e.AgentSummary.Value(),
			TabName: e.TabName, WorkspaceID: e.WorkspaceID, TmuxSession: e.TmuxSession, Transcript: e.JSONLPath, LastEventAt: e.lastEventAt()})
	}
	return out
}

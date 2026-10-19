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

package status

import (
	"context"
	"os"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/state"
	"harnessly/internal/tmux"
)

type metadata struct {
	snippet    Opt[string]
	action     Opt[agents.CurrentAction]
	transcript string
}

func emptyMetadata() metadata {
	return metadata{snippet: Null[string](), action: Null[agents.CurrentAction]()}
}

func fromSnapshot(s agents.Snapshot, transcript string) metadata {
	md := metadata{snippet: optPtr(s.LastAssistantSnippet), action: Null[agents.CurrentAction](), transcript: transcript}
	if s.CurrentAction != nil {
		md.action = Some(*s.CurrentAction)
	}
	return md
}

// readMetadata is what a tab's agent shows: its transcript and the last answer and action in it. Nothing when no agent
// runs in the pane.
func (m *Manager) readMetadata(pane *tmux.PaneInfo, p Provider, tab *Tab) metadata {
	if pane == nil || pane.PID == 0 || p == nil {
		return emptyMetadata()
	}
	children := m.d.Procs.Children(pane.PID)
	if !p.Running(pane.PID, children) {
		return emptyMetadata()
	}
	if tab != nil {
		if stored := p.ReadJSONLPath(tab.Agent); stored != "" {
			if _, err := os.Stat(stored); err == nil {
				if s, err := p.Snapshot(stored, false); err == nil {
					return fromSnapshot(s, stored)
				}
			}
		}
		if id := p.ReadSessionID(tab.Agent); id != "" && p.ID() == agents.Codex {
			if found := p.FindSession(id); found != "" {
				if s, err := p.Snapshot(found, false); err == nil {
					return fromSnapshot(s, found)
				}
			}
		}
	}
	info := p.Detect(pane.PID, children)
	if info.Status != agents.Running || info.JSONLPath == "" {
		md := emptyMetadata()
		md.transcript = info.JSONLPath
		return md
	}
	s, err := p.Snapshot(info.JSONLPath, false)
	if err != nil {
		md := emptyMetadata()
		md.transcript = info.JSONLPath
		return md
	}
	return fromSnapshot(s, info.JSONLPath)
}

func (m *Manager) terminalStatus(pane *tmux.PaneInfo) (string, []int) {
	if pane == nil || pane.PID == 0 {
		return "idle", []int{}
	}
	if ports := m.d.Procs.ListeningPorts(pane.PID); len(ports) > 0 {
		return "server", ports
	}
	if tmux.SafeShells[pane.Command] {
		return "idle", []int{}
	}
	return "running", []int{}
}

// agentFields are the agent columns of a new entry.
func agentFields(e *Entry, p Provider, tab *Tab, transcript string) {
	if p == nil {
		e.AgentSummary, e.AgentSessionID = Null[string](), Null[string]()
		return
	}
	e.AgentProviderID = Some(p.ID())
	e.AgentSummary = optOf(p.ReadSummary(tab.Agent))
	e.AgentSessionID = optOf(sessionIDOf(p, tab, transcript))
}

func sessionIDOf(p Provider, tab *Tab, transcript string) string {
	if transcript != "" {
		if id := p.SessionIDFromPath(transcript); id != "" {
			return id
		}
	}
	return p.ReadSessionID(tab.Agent)
}

func tabName(tab *Tab, paneTitle Opt[string]) string {
	if tab.Name != "" {
		return tab.Name
	}
	if paneTitle.Has() {
		return state.FormatTabTitle(paneTitle.Value(), tab.PanelType.Value())
	}
	return ""
}

func persistedState(tab *Tab) string {
	s := tab.CliState
	if s == "" {
		s = Idle
	}
	if s == Busy { // a turn that was running at the restart is unknown until something definite says otherwise
		return Unknown
	}
	return s
}

func (m *Manager) restoredLastEvent(state string, tab *Tab) Opt[LastEvent] {
	// Events live in memory only. A restored needs-input gets one so the page's acknowledgement (seq 0) matches.
	if state == NeedsInput {
		return Some(LastEvent{Name: EventNotification, At: m.now(), Seq: 0})
	}
	if tab.LastEventName != "" {
		return Some(LastEvent{Name: tab.LastEventName, At: 0, Seq: 0})
	}
	return Null[LastEvent]()
}

type scannedTab struct {
	ws   string
	tab  Tab
	pane *tmux.PaneInfo
}

func (m *Manager) layoutTabs() ([]scannedTab, map[string]tmux.PaneInfo, bool) {
	panes, ok := m.d.Tmux.ReadAllPanes()
	var out []scannedTab
	for _, ws := range m.d.Layouts.Workspaces() {
		tabs, ok := m.d.Layouts.Tabs(ws.ID)
		if !ok {
			continue
		}
		for _, t := range tabs {
			st := scannedTab{ws: ws.ID, tab: t}
			if p, has := panes[t.SessionName]; has {
				st.pane = &p
			}
			out = append(out, st)
		}
	}
	return out, panes, ok
}

// scanAll rebuilds every entry from the layouts. A rescan (tmux reset) tells the sockets the new picture, which the
// old runtime did not.
func (m *Manager) scanAll() {
	tabs, _, _ := m.layoutTabs()
	type prepared struct {
		st     scannedTab
		md     metadata
		status string
		ports  []int
	}
	var ready []prepared
	for _, st := range tabs {
		p := m.d.Providers[providerOfPanel(st.tab.PanelType)]
		pr := prepared{st: st, md: m.readMetadata(st.pane, p, &st.tab), status: "idle", ports: []int{}}
		if p == nil {
			pr.status, pr.ports = m.terminalStatus(st.pane)
		}
		ready = append(ready, pr)
	}
	m.mu.Lock()
	rescan := m.scanned
	m.watch.closeAll()
	m.tabs, m.order = map[string]*Entry{}, nil
	var unknown []string
	for _, pr := range ready {
		tab := pr.st.tab
		p := m.d.Providers[providerOfPanel(tab.PanelType)]
		state := persistedState(&tab)
		e := &Entry{CliState: state, WorkspaceID: pr.st.ws, TmuxSession: tab.SessionName, PanelType: tab.PanelType,
			TerminalStatus: Some(pr.status), ListeningPorts: Some(pr.ports), LastUserMessage: tab.LastUserMessage,
			LastAssistantMessage: pr.md.snippet, CurrentAction: pr.md.action, BusySince: Null[float64](),
			JSONLPath: pr.md.transcript, LastEvent: m.restoredLastEvent(state, &tab)}
		if pr.st.pane != nil {
			e.CurrentProcess = Some(pr.st.pane.Command)
			e.PaneTitle = Some(pr.st.pane.Command + "|" + pr.st.pane.Path)
		}
		e.TabName = tabName(&tab, e.PaneTitle)
		agentFields(e, p, &tab, pr.md.transcript)
		e.ReadyForReviewAt = Null[float64]()
		if state == ReadyForReview {
			e.ReadyForReviewAt = Some(m.now())
		}
		e.DismissedAt = Null[float64]()
		if tab.DismissedAt.Has() {
			e.DismissedAt = tab.DismissedAt
		}
		m.set(tab.ID, e)
		if p != nil && p.ID() == agents.Claude && !storedSessionID(&tab) {
			m.idHeal = append(m.idHeal, tab.ID)
		}
		if e.JSONLPath != "" {
			m.watch.start(tab.ID, e.JSONLPath)
		}
		if state == Unknown {
			unknown = append(unknown, tab.ID)
		}
		if rescan {
			m.broadcastUpdate(tab.ID, e, nil)
		}
	}
	m.syncBackground()
	m.mu.Unlock()
	for _, id := range unknown {
		go m.resolveUnknown(id)
	}
}

func storedSessionID(tab *Tab) bool {
	if st := tab.Agent.AgentState; st != nil && st.SessionID != nil {
		return true
	}
	return tab.Agent.ClaudeSessionID != nil
}

// Rescan rebuilds every entry (after a tmux reset).
func (m *Manager) Rescan() { m.scanAll() }

// resolveUnknown settles a tab whose turn was running at the restart, only on something definite: no agent in the
// pane, or a transcript that finished with an answer.
func (m *Manager) resolveUnknown(tabID string) {
	m.mu.Lock()
	e := m.tabs[tabID]
	if e == nil || e.CliState != Unknown {
		m.mu.Unlock()
		return
	}
	p, session, transcript := m.provider(e), e.TmuxSession, e.JSONLPath
	m.mu.Unlock()
	panes, ok := m.d.Tmux.ReadAllPanes()
	if !ok {
		return // tmux did not answer: the stuck watchdog settles the tab later
	}
	running := false
	if pane, has := panes[session]; has && pane.PID != 0 && p != nil {
		running = p.Running(pane.PID, m.d.Procs.Children(pane.PID))
	}
	settle := ""
	if !running {
		settle = Idle
	} else if p != nil && transcript != "" {
		if s, err := p.Snapshot(transcript, false); err == nil && s.Idle && !s.Stale && s.LastAssistantSnippet != nil {
			settle = ReadyForReview
		}
	}
	if settle == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e = m.tabs[tabID]; e == nil || e.CliState != Unknown {
		return
	}
	m.applyCliState(tabID, e, settle, applyOpts{silent: true})
	m.persistToLayout(e)
	m.broadcastUpdate(tabID, e, nil)
}

func (m *Manager) pollIntervalFor(n int) time.Duration {
	switch {
	case n >= tabsLarge:
		return pollLarge
	case n >= tabsMedium:
		return pollMedium
	}
	return pollSmall
}

func (m *Manager) pollLoop(ctx context.Context) {
	m.mu.Lock()
	interval := m.pollIntervalFor(len(m.tabs))
	m.mu.Unlock()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-m.pollReset:
			t.Reset(d)
		case <-t.C:
			m.Poll()
		}
	}
}

// Poll looks at every tab once. A poll asked for while one runs runs once more after it, instead of being lost.
func (m *Manager) Poll() {
	m.pollMu.Lock()
	if m.polling {
		m.pollAgain = true
		m.pollMu.Unlock()
		return
	}
	m.polling = true
	m.pollMu.Unlock()
	for {
		m.pollOnce()
		m.pollMu.Lock()
		if !m.pollAgain {
			m.polling = false
			m.pollMu.Unlock()
			return
		}
		m.pollAgain = false
		m.pollMu.Unlock()
	}
}

func (m *Manager) pollOnce() {
	tabs, _, ok := m.layoutTabs()
	if !ok {
		m.d.Log("warn", "tmux did not list its panes — poll skipped, no process is judged on an empty answer")
		return
	}
	known := map[string]bool{}
	m.mu.Lock()
	before := append([]string(nil), m.order...)
	m.mu.Unlock()
	now := m.now()
	for i := range tabs {
		known[tabs[i].tab.ID] = true
		m.pollTab(&tabs[i], now)
	}
	m.mu.Lock()
	for _, id := range before {
		if !known[id] && m.tabs[id] != nil {
			m.watch.stop(id)
			m.del(id)
			m.broadcastRemove(id)
		}
	}
	m.syncBackground()
	n := len(m.tabs)
	m.mu.Unlock()
	if d := m.pollIntervalFor(n); d != m.pollInterval {
		m.pollInterval = d
		select {
		case m.pollReset <- d:
		default:
		}
	}
}

func (m *Manager) pollTab(st *scannedTab, now float64) {
	tab := &st.tab
	p := m.d.Providers[providerOfPanel(tab.PanelType)]
	status, ports := "idle", []int{}
	if p == nil {
		status, ports = m.terminalStatus(st.pane)
	}
	var process, title Opt[string]
	if st.pane != nil {
		process, title = Some(st.pane.Command), Some(st.pane.Command+"|"+st.pane.Path)
	}
	m.mu.Lock()
	existing := m.tabs[tab.ID]
	m.mu.Unlock()
	md := m.readMetadata(st.pane, p, tab)

	if existing == nil {
		state := persistedState(tab)
		e := &Entry{CliState: state, WorkspaceID: st.ws, CurrentProcess: process, PaneTitle: title, TmuxSession: tab.SessionName,
			PanelType: tab.PanelType, TerminalStatus: Some(status), ListeningPorts: Some(ports), LastUserMessage: tab.LastUserMessage,
			LastAssistantMessage: md.snippet, CurrentAction: md.action, JSONLPath: md.transcript,
			// The old runtime restored no event name here, only at the start; a tab first seen by a poll showed as
			// never answered. Both paths restore it now.
			LastEvent: m.restoredLastEvent(state, tab)}
		e.TabName = tabName(tab, title)
		agentFields(e, p, tab, md.transcript)
		m.mu.Lock()
		if m.tabs[tab.ID] != nil {
			m.mu.Unlock()
			return
		}
		m.set(tab.ID, e)
		m.persistToLayout(e)
		m.bindDetectedSession(tab, p, md.transcript)
		m.broadcastUpdate(tab.ID, e, nil)
		m.mu.Unlock()
		if state == Unknown {
			go m.resolveUnknown(tab.ID)
		}
		return
	}

	m.mu.Lock()
	e := m.tabs[tab.ID]
	if e == nil {
		m.mu.Unlock()
		return
	}
	processChanged := !optEq(e.CurrentProcess, process)
	messageChanged := !optEq(e.LastUserMessage, tab.LastUserMessage)
	panelChanged := !optEq(e.PanelType, tab.PanelType)
	e.TabName = tabName(tab, title)
	e.CurrentProcess, e.PaneTitle, e.WorkspaceID, e.PanelType = process, title, st.ws, tab.PanelType
	if p != nil {
		e.AgentProviderID = Some(p.ID())
		e.AgentSessionID = optOf(sessionIDOf(p, tab, md.transcript))
	} else {
		e.AgentProviderID, e.AgentSessionID = Opt[string]{}, Null[string]()
	}
	if md.transcript != "" {
		e.JSONLPath = md.transcript
	}
	e.LastUserMessage = tab.LastUserMessage
	m.bindDetectedSession(tab, p, md.transcript)
	if processChanged {
		e.ProcessRetries = processRetryCount
	}
	retry := !processChanged && e.ProcessRetries > 0
	if retry {
		e.ProcessRetries--
	}
	terminalChanged := !e.TerminalStatus.Set || e.TerminalStatus.Value() != status || !sameInts(e.ListeningPorts.Value(), ports) || !e.ListeningPorts.Set
	if terminalChanged {
		e.TerminalStatus, e.ListeningPorts = Some(status), Some(ports)
	}
	tabSummary := Null[string]()
	if p != nil {
		tabSummary = optOf(p.ReadSummary(tab.Agent))
	}
	working := e.CliState == Busy || e.CliState == NeedsInput
	session := tab.SessionName
	m.mu.Unlock()

	summaryChanged := false
	if working {
		var live *string
		if p != nil {
			live = p.ParsePaneTitle(m.d.Tmux.PaneTitle(session))
		}
		next := tabSummary
		if live != nil {
			next = Some(*live)
		}
		m.mu.Lock()
		if !optEq(next, e.AgentSummary) {
			e.AgentSummary = next
			summaryChanged = true
			if live != nil && p != nil {
				m.storeAgentState(session, p.ID(), Opt[string]{}, Opt[string]{}, Some(*live), Opt[string]{})
			}
		}
		m.mu.Unlock()
	} else {
		m.mu.Lock()
		if !optEq(e.AgentSummary, tabSummary) {
			e.AgentSummary = tabSummary
			summaryChanged = true
		}
		m.mu.Unlock()
	}

	var runningCache *bool
	agentRunning := func() bool {
		if runningCache != nil {
			return *runningCache
		}
		r := false
		if st.pane != nil && st.pane.PID != 0 && p != nil {
			r = p.Running(st.pane.PID, m.d.Procs.Children(st.pane.PID))
		}
		runningCache = &r
		return r
	}

	if m.pollChecks(tab.ID, e, p, st.pane, now, agentRunning) {
		return
	}
	if summaryChanged || terminalChanged || processChanged || retry || messageChanged || panelChanged {
		m.mu.Lock()
		if m.tabs[tab.ID] == e {
			m.broadcastUpdate(tab.ID, e, nil)
		}
		m.mu.Unlock()
	}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bindDetectedSession stores a transcript the layout lacks: a conversation started from outside is never opened in a
// browser first, so nothing else would. Fills the gap, never overwrites. Called with the lock held.
func (m *Manager) bindDetectedSession(tab *Tab, p Provider, transcript string) {
	if p == nil || transcript == "" || p.ReadJSONLPath(tab.Agent) != "" {
		return
	}
	sessionID := Opt[string]{}
	if p.ReadSessionID(tab.Agent) == "" {
		sessionID = optOf(p.SessionIDFromPath(transcript))
	}
	m.storeAgentState(tab.SessionName, p.ID(), sessionID, Some(transcript), Opt[string]{}, Opt[string]{})
}

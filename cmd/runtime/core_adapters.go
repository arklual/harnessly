package main

import (
	"os"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/terminal"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// The core's collaborators, adapted to what the status keeper asks.

type coreLayouts struct {
	store   *state.Store
	claude  *claude.Detector
	history *history.Store
}

func (l coreLayouts) Workspaces() []status.Workspace {
	var out []status.Workspace
	view := l.store.Workspaces()
	for _, v := range view.Array("workspaces") {
		ws, ok := v.(*jsvalue.Object)
		if !ok {
			continue
		}
		w := status.Workspace{ID: ws.String("id"), Name: ws.String("name")}
		for _, d := range ws.Array("directories") {
			if s, ok := d.(string); ok {
				w.Directories = append(w.Directories, s)
			}
		}
		out = append(out, w)
	}
	return out
}

func (l coreLayouts) Tabs(wsID string) ([]status.Tab, bool) {
	layout := l.store.Layout(wsID)
	if layout == nil {
		return nil, false
	}
	var out []status.Tab
	for _, t := range state.AllTabs(layout) {
		out = append(out, status.TabFromLayout(t))
	}
	return out, true
}

func (l coreLayouts) UpdateTabCliStatus(session, cliState string, dismissedAt status.Opt[float64], lastEventName string) error {
	var dismissed *any
	if dismissedAt.Set {
		var v any
		if dismissedAt.Has() {
			v = dismissedAt.Value()
		}
		dismissed = &v
	}
	var event any
	if lastEventName != "" {
		event = lastEventName
	}
	return l.store.UpdateTabCliStatus(session, cliState, dismissed, event)
}

func optAny(o status.Opt[string]) *any {
	if !o.Set {
		return nil
	}
	if o.Has() {
		return state.Value(o.Value())
	}
	return state.Value(nil)
}

func (l coreLayouts) UpdateTabAgentState(session, provider string, sessionID, jsonlPath, summary, lastUser status.Opt[string]) error {
	return l.store.UpdateTabAgentState(session, state.Provider(provider), state.AgentState{
		SessionID: optAny(sessionID), JsonlPath: optAny(jsonlPath), Summary: optAny(summary), LastUserMessage: optAny(lastUser)})
}

func (l coreLayouts) RecoverSession(session string) (string, bool) {
	wsID, _, _, ok := tmuxNames.ParseSessionName(session)
	if !ok {
		return "", false
	}
	var tab *status.Tab
	if tabs, ok := l.Tabs(wsID); ok {
		for i := range tabs {
			if tabs[i].SessionName == session {
				tab = &tabs[i]
			}
		}
	}
	if tab == nil {
		return "", false
	}
	found := agents.RecoverTabSession(tab.Agent, claude.Tabs{}, agents.RecoveryDeps{
		Exists:         func(p string) bool { _, err := os.Stat(p); return err == nil },
		FindTranscript: func(id string) string { return claude.FindTranscript(l.claude.ProjectsDir(), id) },
		History: func() ([]agents.HistoryEntry, error) {
			var out []agents.HistoryEntry
			if l.history == nil {
				return nil, nil
			}
			for _, e := range l.history.All() {
				out = append(out, agents.HistoryEntry{TabID: e.TabID, ProviderID: e.ProviderID, AgentSessionID: e.AgentSessionID, CompletedAt: float64(e.CompletedAt)})
			}
			return out, nil
		},
	})
	if found == nil {
		return "", false
	}
	if err := l.store.StoreRecoveredAgentSession(session, state.Claude, found.SessionID, found.JSONLPath); err != nil {
		return "", false
	}
	return found.SessionID, true
}

// tmuxNames parses session names before the core's tmux exists (set in main).
var tmuxNames *tmux.Tmux

type coreTmux struct {
	*tmux.Tmux
	term *terminal.Server
}

func (t coreTmux) CaptureAtWidth(session string, cols, rows int) (string, bool) {
	return t.Tmux.CaptureAtWidth(t.term, session, cols, rows)
}
func (t coreTmux) SessionCwd(session string) string { return machine.SessionDir(t.Tmux, session) }

type coreProcs struct{}

func (coreProcs) Children(pid int) []int       { return proc.ChildPIDs(pid) }
func (coreProcs) ListeningPorts(pid int) []int { return proc.TreeListeningPorts(pid) }

type claudeProvider struct {
	det   *claude.Detector
	snaps *claude.SnapshotReader
	hooks string // the hooks.json path agents are launched with
}

func (p claudeProvider) ID() string                         { return agents.Claude }
func (p claudeProvider) Running(_ int, children []int) bool { return p.det.IsRunning(children) }
func (p claudeProvider) Detect(_ int, children []int) agents.SessionInfo {
	info, _ := p.det.Detect(children)
	return info
}
func (p claudeProvider) Snapshot(t string, force bool) (agents.Snapshot, error) {
	return p.snaps.Read(t, force), nil
}
func (p claudeProvider) HistoryStats(t string) (*agents.HistoryStats, error) {
	s := claude.ReadHistoryStats(t)
	return &s, nil
}
func (p claudeProvider) ParsePaneTitle(title string) *string { return claude.ParsePaneTitle(title) }
func (p claudeProvider) SessionIDFromPath(t string) string   { return claude.SessionIDFromPath(t) }
func (p claudeProvider) ReadSessionID(t agents.TabAgent) string {
	return claude.Tabs{}.ReadSessionID(t)
}
func (p claudeProvider) ReadJSONLPath(t agents.TabAgent) string {
	return claude.Tabs{}.ReadJSONLPath(t)
}
func (p claudeProvider) ReadSummary(t agents.TabAgent) string { return claude.Tabs{}.ReadSummary(t) }
func (p claudeProvider) FindSession(string) string            { return "" }
func (p claudeProvider) TranscriptIn(cwd, id string) string   { return p.det.TranscriptIn(cwd, id) }
func (p claudeProvider) RunsWithHooks(children []int) (bool, bool) {
	return p.det.RunsWithHooks(children, p.hooks, "status-hook.sh")
}

type codexProvider struct {
	det   *codex.Detector
	snaps *codex.SnapshotReader
}

func (p codexProvider) ID() string                         { return agents.Codex }
func (p codexProvider) Running(_ int, children []int) bool { return p.det.IsRunning(children) }
func (p codexProvider) Detect(_ int, children []int) agents.SessionInfo {
	return p.det.Detect(children, false)
}
func (p codexProvider) Snapshot(t string, force bool) (agents.Snapshot, error) {
	return p.snaps.Read(t, force), nil
}
func (p codexProvider) HistoryStats(t string) (*agents.HistoryStats, error) {
	s := codex.ReadHistoryStats(t)
	return &s, nil
}
func (p codexProvider) ParsePaneTitle(title string) *string    { return codex.ParsePaneTitle(title) }
func (p codexProvider) SessionIDFromPath(t string) string      { return codex.SessionIDFromPath(t) }
func (p codexProvider) ReadSessionID(t agents.TabAgent) string { return codex.Tabs{}.ReadSessionID(t) }
func (p codexProvider) ReadJSONLPath(t agents.TabAgent) string { return codex.Tabs{}.ReadJSONLPath(t) }
func (p codexProvider) ReadSummary(t agents.TabAgent) string   { return codex.Tabs{}.ReadSummary(t) }
func (p codexProvider) FindSession(id string) string {
	if m, ok := codex.FindSessionByID(p.det.SessionsRoot(), id, time.Now(), codex.ScanDays); ok {
		return m.JSONLPath
	}
	return ""
}
func (p codexProvider) TranscriptIn(string, string) string { return "" }
func (p codexProvider) RunsWithHooks([]int) (bool, bool)   { return false, false }

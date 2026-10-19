package agents

import "slices"

// AgentState is the agent part of a layout tab (`tab.agentState`).
type AgentState struct {
	ProviderID string  `json:"providerId"`
	SessionID  *string `json:"sessionId"`
	JSONLPath  *string `json:"jsonlPath"`
	Summary    *string `json:"summary"`
}

// TabAgent is what a layout tab holds about its agent: the provider-neutral state and the fields older layouts kept
// for Claude.
type TabAgent struct {
	ID              string
	AgentState      *AgentState
	ClaudeSessionID *string
	ClaudeJSONLPath *string
	ClaudeSummary   *string
}

// TabReader reads a tab's stored session the way one provider does.
type TabReader interface {
	ProviderID() string
	ReadSessionID(t TabAgent) string
	ReadJSONLPath(t TabAgent) string
}

// HistoryEntry is the part of a session-history entry recovery reads.
type HistoryEntry struct {
	TabID           string
	ProviderID      string
	AgentSessionID  *string
	ClaudeSessionID *string
	CompletedAt     float64
}

// RecoveryDeps is what recovery looks at on disk.
type RecoveryDeps struct {
	Exists func(path string) bool
	// FindTranscript is a session's transcript in whatever project folder holds it, "" when none.
	FindTranscript func(sessionID string) string
	History        func() ([]HistoryEntry, error)
}

// Recovered is a session found for a tab.
type Recovered struct {
	SessionID string `json:"sessionId"`
	JSONLPath string `json:"jsonlPath"`
}

// RecoverTabSession is the session a tab stands for, from the first source with a transcript on disk: the transcript
// path the tab kept (its file name is the id), the session id it kept, the newest session of its own history. Nil when
// the tab never had one; an unreadable history is not an error.
func RecoverTabSession(t TabAgent, p TabReader, deps RecoveryDeps) *Recovered {
	storedPath := p.ReadJSONLPath(t)
	if fromPath := SessionIDFromTranscript(storedPath); storedPath != "" && fromPath != "" && deps.Exists(storedPath) {
		return &Recovered{SessionID: fromPath, JSONLPath: storedPath}
	}
	stored := p.ReadSessionID(t)
	if stored != "" {
		if found := deps.FindTranscript(stored); found != "" {
			return &Recovered{SessionID: stored, JSONLPath: found}
		}
	}
	all, err := deps.History()
	if err != nil {
		return nil
	}
	var mine []HistoryEntry
	for _, e := range all {
		if e.TabID == t.ID && e.ProviderID == p.ProviderID() {
			mine = append(mine, e)
		}
	}
	slices.SortStableFunc(mine, func(a, b HistoryEntry) int {
		switch {
		case a.CompletedAt > b.CompletedAt:
			return -1
		case a.CompletedAt < b.CompletedAt:
			return 1
		}
		return 0
	})
	for _, e := range mine {
		id := e.AgentSessionID
		if id == nil {
			id = e.ClaudeSessionID
		}
		if id == nil || *id == "" || *id == stored {
			continue
		}
		if found := deps.FindTranscript(*id); found != "" {
			return &Recovered{SessionID: *id, JSONLPath: found}
		}
	}
	return nil
}

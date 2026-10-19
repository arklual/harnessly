// Package codex is the runtime's side of the OpenAI Codex CLI: whether it runs in a pane and which session, what its
// rollout transcript says about the turn, what its hook payloads mean, and the launcher line and arguments that start
// or resume it.
package codex

import (
	"regexp"
	"strings"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
)

const (
	ProviderID  = agents.Codex
	DisplayName = "Codex"
	PanelType   = agents.CodexPanel
	Binary      = "codex"
)

// MatchesProcess tells Codex by a pane's foreground command and, for node, the process's arguments.
func MatchesProcess(command string, args []string) bool {
	if command == Binary {
		return true
	}
	if command == "node" {
		for _, a := range args {
			if strings.HasSuffix(a, "codex.js") {
				return true
			}
		}
	}
	return false
}

// ParsePaneTitle: Codex sets no conversation title in the pane.
func ParsePaneTitle(string) *string { return nil }

var trailingUUID = regexp.MustCompile(`(?i)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

// SessionIDFromPath is the session id at the end of a rollout file's name, "" when there is none.
func SessionIDFromPath(jsonlPath string) string {
	if jsonlPath == "" {
		return ""
	}
	if m := trailingUUID.FindStringSubmatch(jscompat.Basename(jsonlPath, ".jsonl")); m != nil {
		return m[1]
	}
	return ""
}

// Tabs reads a layout tab's stored Codex session (only the provider-neutral state, when it is Codex's).
type Tabs struct{}

func (Tabs) ProviderID() string { return ProviderID }

func field(t agents.TabAgent, pick func(*agents.AgentState) *string) string {
	if t.AgentState == nil || t.AgentState.ProviderID != ProviderID {
		return ""
	}
	if v := pick(t.AgentState); v != nil {
		return *v
	}
	return ""
}

func (Tabs) ReadSessionID(t agents.TabAgent) string {
	return field(t, func(s *agents.AgentState) *string { return s.SessionID })
}

func (Tabs) ReadJSONLPath(t agents.TabAgent) string {
	return field(t, func(s *agents.AgentState) *string { return s.JSONLPath })
}

func (Tabs) ReadSummary(t agents.TabAgent) string {
	return field(t, func(s *agents.AgentState) *string { return s.Summary })
}

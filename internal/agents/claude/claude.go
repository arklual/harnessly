// Package claude is the runtime's side of Claude Code: whether it runs in a pane and which conversation, what its
// transcript says about the turn, what its hooks mean, and the command line that starts or resumes it.
package claude

import (
	"regexp"
	"strings"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
)

const (
	ProviderID  = agents.Claude
	DisplayName = "Claude Code"
	PanelType   = agents.ClaudePanel
	// Binary is the CLI's name, also what a process is recognised by.
	Binary = "claude"
)

// MatchesProcess tells Claude by a pane's foreground command (tmux's #{pane_current_command}) and, for node, the
// arguments of the process.
func MatchesProcess(command string, args []string) bool {
	if command == Binary {
		return true
	}
	if command == "node" {
		for _, a := range args {
			if strings.HasSuffix(a, "claude.js") || strings.HasSuffix(a, Binary) {
				return true
			}
		}
	}
	return false
}

var titleRe = regexp.MustCompile(`^[✳⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⠐⠈]` + jscompat.SpaceClass + `+`)

// ParsePaneTitle is the conversation's live title from the pane title Claude sets while it works (a spinner, then the
// title); nil when the title is not Claude's.
func ParsePaneTitle(title string) *string {
	if title == "" || !titleRe.MatchString(title) {
		return nil
	}
	cleaned := jscompat.Trim(titleRe.ReplaceAllString(title, ""))
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

// SessionIDFromPath is the session id of a transcript path: its file name without .jsonl.
func SessionIDFromPath(jsonlPath string) string {
	if jsonlPath == "" {
		return ""
	}
	return jscompat.Basename(jsonlPath, ".jsonl")
}

// Tabs reads a layout tab's stored Claude session: the provider-neutral state when it is Claude's, else the fields
// older layouts kept.
type Tabs struct{}

func (Tabs) ProviderID() string { return ProviderID }

func field(t agents.TabAgent, pick func(*agents.AgentState) *string, legacy *string) string {
	if t.AgentState != nil && t.AgentState.ProviderID == ProviderID {
		if v := pick(t.AgentState); v != nil {
			return *v
		}
	}
	if legacy != nil {
		return *legacy
	}
	return ""
}

// ReadSessionID is the stored id, else the id in the stored transcript's file name (a tab that lost its id).
func (Tabs) ReadSessionID(t agents.TabAgent) string {
	if id := field(t, func(s *agents.AgentState) *string { return s.SessionID }, t.ClaudeSessionID); id != "" {
		return id
	}
	return agents.SessionIDFromTranscript(Tabs{}.ReadJSONLPath(t))
}

// ReadJSONLPath is the stored transcript path.
func (Tabs) ReadJSONLPath(t agents.TabAgent) string {
	return field(t, func(s *agents.AgentState) *string { return s.JSONLPath }, t.ClaudeJSONLPath)
}

// ReadSummary is the stored conversation title.
func (Tabs) ReadSummary(t agents.TabAgent) string {
	return field(t, func(s *agents.AgentState) *string { return s.Summary }, t.ClaudeSummary)
}

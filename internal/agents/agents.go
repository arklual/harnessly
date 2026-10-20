// Package agents holds what the coding agents the runtime drives (Claude Code in internal/agents/claude, Codex in
// internal/agents/codex) have in common: the session a pane runs, the work-state events their hooks translate to, the
// snapshot of a transcript's tail, the stats of a finished turn, and the pieces of a launch line.
package agents

import (
	"bytes"
	"errors"
	"regexp"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// Provider ids and panel types.
const (
	Claude = "claude"
	Codex  = "codex"

	ClaudePanel = "claude-code"
	CodexPanel  = "codex-cli"
)

// DisplayName is how the platform names an agent CLI.
func DisplayName(provider string) string {
	switch provider {
	case Claude:
		return "Claude Code"
	case Codex:
		return "Codex"
	}
	return ""
}

// PanelOf is the panel type of an agent CLI's tabs.
func PanelOf(provider string) string {
	switch provider {
	case Claude:
		return ClaudePanel
	case Codex:
		return CodexPanel
	}
	return ""
}

// Unavailable is why an agent CLI cannot start (answered with 409).
type Unavailable struct {
	Code                                       string // agent-not-installed or agent-path-missing
	ProviderID, ProviderDisplayName, PanelType string
	SuggestedCommand                           any // "claude", "claude-path" or nil
}

// ProviderOfPanel is the agent a tab's panel type runs, "" for a panel that runs none (a terminal).
func ProviderOfPanel(panelType string) string {
	switch panelType {
	case ClaudePanel:
		return Claude
	case CodexPanel:
		return Codex
	}
	return ""
}

var uuidRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsUUID: session ids the CLIs accept for a resume (and the only ones typed into a shell).
func IsUUID(s string) bool { return uuidRe.MatchString(s) }

// SessionIDFromTranscript is the session a Claude transcript belongs to: its file name, when that is a UUID. A tab that
// lost its stored id but kept its transcript path still knows its session.
func SessionIDFromTranscript(jsonlPath string) string {
	if jsonlPath == "" {
		return ""
	}
	if id := jscompat.Basename(jsonlPath, ".jsonl"); IsUUID(id) {
		return id
	}
	return ""
}

// SessionStatus is what detection found in a pane.
type SessionStatus string

const (
	Running        SessionStatus = "running"
	NotRunning     SessionStatus = "not-running"
	NotInstalled   SessionStatus = "not-installed"
	NotInitialized SessionStatus = "not-initialized"
)

// SessionInfo is the agent session a pane runs.
type SessionInfo struct {
	Status SessionStatus
	// SessionID is the conversation's id, "" when not known.
	SessionID string
	// RawSessionID is the id exactly as the agent recorded it (a JSON value, a string in practice); nil when none.
	RawSessionID any
	// JSONLPath is the transcript, "" when it is not on disk (yet).
	JSONLPath string
	PID       int
	// StartedAt is the session's start in epoch milliseconds, when the agent recorded it.
	StartedAt *float64
	Cwd       string
}

// Info is a session without a process.
func Info(status SessionStatus) SessionInfo { return SessionInfo{Status: status} }

func strOrNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// MarshalJSON writes the session as the timeline reads it: {status, sessionId, jsonlPath, pid, startedAt, cwd}.
func (s SessionInfo) MarshalJSON() ([]byte, error) {
	var id any = strOrNull(s.SessionID)
	if s.RawSessionID != nil {
		id = s.RawSessionID
	}
	var pid any
	if s.PID > 0 {
		pid = s.PID
	}
	var started any
	if s.StartedAt != nil {
		started = *s.StartedAt
	}
	return nodejson.Marshal(struct {
		Status    SessionStatus `json:"status"`
		SessionID any           `json:"sessionId"`
		JSONLPath any           `json:"jsonlPath"`
		PID       any           `json:"pid"`
		StartedAt any           `json:"startedAt"`
		Cwd       any           `json:"cwd"`
	}{s.Status, id, strOrNull(s.JSONLPath), pid, started, strOrNull(s.Cwd)}), nil
}

// EventKind is a work-state event an agent's hooks (or the runtime's own watchers) report.
type EventKind string

const (
	SessionStart  EventKind = "session-start"
	PromptSubmit  EventKind = "prompt-submit"
	Notification  EventKind = "notification"
	Stop          EventKind = "stop"
	StopFailure   EventKind = "stop-failure"
	Interrupt     EventKind = "interrupt"
	PreCompact    EventKind = "pre-compact"
	PostCompact   EventKind = "post-compact"
	SummaryUpdate EventKind = "summary-update"
	LastUserMsg   EventKind = "last-user-message"
)

// HookEventKinds are the events a hook may name: the state machine maps session-start → idle, prompt-submit → busy,
// notification → needs-input (when NotificationNeedsInput), stop and stop-failure → ready-for-review, interrupt →
// idle, the compact pair → the compacting flag only.
var HookEventKinds = []EventKind{SessionStart, PromptSubmit, Notification, Stop, StopFailure, Interrupt, PreCompact, PostCompact}

// Event is one work-state event.
type Event struct {
	Kind EventKind `json:"kind"`
	// NotificationType of a notification, "" when the hook gave none.
	NotificationType string `json:"notificationType,omitempty"`
	// Summary of a summary-update (nil clears it); Message of a last-user-message.
	Summary *string `json:"summary,omitempty"`
	Message string  `json:"message,omitempty"`
}

// NotificationNeedsInput: only a notification asking for permission (or one without a type) needs the person; the
// others (idle reminders, auth, elicitation…) change nothing.
func NotificationNeedsInput(notificationType string) bool {
	return notificationType == "" || notificationType == "permission_prompt" || notificationType == "worker_permission_prompt"
}

// CurrentAction is what the agent is doing now: a tool call (ToolName set) or the text it is writing.
type CurrentAction struct {
	ToolName *string `json:"toolName"`
	Summary  string  `json:"summary"`
}

// Tool is a current action for a tool call.
func Tool(name, summary string) *CurrentAction {
	return &CurrentAction{ToolName: &name, Summary: summary}
}

// Text is a current action for written text.
func Text(summary string) *CurrentAction { return &CurrentAction{Summary: summary} }

// Snapshot is what the tail of a transcript says about the agent's turn.
type Snapshot struct {
	// Idle: the turn is over (or the agent has been quiet for longer than StaleMs while waiting).
	Idle bool `json:"idle"`
	// Stale: the last entry leaves the turn open, so Idle is a guess from the file's age.
	Stale                bool           `json:"stale"`
	LastAssistantSnippet *string        `json:"lastAssistantSnippet"`
	CurrentAction        *CurrentAction `json:"currentAction"`
	// Reset: a prompt is newer than the last answer, so the answer shown belongs to an older turn.
	Reset bool `json:"reset"`
	// LastEntryTs is the deciding (Claude) or newest (Codex) entry's time in epoch ms.
	LastEntryTs *float64 `json:"lastEntryTs"`
	StaleMs     int64    `json:"staleMs"`
	Interrupted bool     `json:"interrupted"`
}

// Counts are counts by name, written in the order the names were first counted (JavaScript's object order).
type Counts struct{ o *Obj }

// Add counts one more of name.
func (c *Counts) Add(name string) {
	if c.o == nil {
		c.o = NewObj()
	}
	n, _ := c.o.Val(name).(float64)
	c.o.Set(name, n+1)
}

// Get is the count of name.
func (c Counts) Get(name string) int {
	n, _ := c.o.Val(name).(float64)
	return int(n)
}

// Names are the counted names in output order.
func (c Counts) Names() []string { return c.o.Keys() }

// MarshalJSON writes {name: count} in order.
func (c Counts) MarshalJSON() ([]byte, error) {
	if c.o == nil {
		return []byte("{}"), nil
	}
	return []byte(Stringify(c.o)), nil
}

// UnmarshalJSON reads {name: count}, keeping the order.
func (c *Counts) UnmarshalJSON(b []byte) error {
	v, err := Decode(bytes.TrimSpace(b))
	if err != nil {
		return err
	}
	c.o = AsObj(v)
	if c.o == nil {
		return errors.New("counts: not an object")
	}
	return nil
}

// HistoryStats is what a finished turn's transcript tail gives the session history.
type HistoryStats struct {
	ToolUsage         Counts   `json:"toolUsage"`
	TouchedFiles      []string `json:"touchedFiles"`
	LastAssistantText *string  `json:"lastAssistantText"`
	LastUserText      *string  `json:"lastUserText"`
	FirstUserTs       *float64 `json:"firstUserTs"`
	LastAssistantTs   *float64 `json:"lastAssistantTs"`
	TurnDurationMs    *float64 `json:"turnDurationMs"`
}

// EmptyHistoryStats is the stats of an unreadable or empty transcript.
func EmptyHistoryStats() HistoryStats { return HistoryStats{TouchedFiles: []string{}} }

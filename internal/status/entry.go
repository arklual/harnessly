package status

import (
	"bytes"
	"encoding/json"

	"harnessly/internal/agents"
	"harnessly/internal/nodejson"
)

// Opt is a field the page tells apart in three ways: absent, null, or a value. The old runtime built its entries
// differently on different paths, and the page reads an absent key and a null key differently.
type Opt[T any] struct {
	Set bool
	Val *T
}

func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, Val: &v} }
func Null[T any]() Opt[T]    { return Opt[T]{Set: true} }

// Value is the value, or the zero value when absent or null.
func (o Opt[T]) Value() T {
	var zero T
	if o.Val == nil {
		return zero
	}
	return *o.Val
}

// Has is whether there is a value (not absent, not null).
func (o Opt[T]) Has() bool { return o.Val != nil }

func optEq[T comparable](a, b Opt[T]) bool {
	if a.Set != b.Set || (a.Val == nil) != (b.Val == nil) {
		return false
	}
	return a.Val == nil || *a.Val == *b.Val
}

// optOf is a nullable string from the agents packages: "" reads as null.
func optOf(s string) Opt[string] {
	if s == "" {
		return Null[string]()
	}
	return Some(s)
}

func optPtr(p *string) Opt[string] {
	if p == nil {
		return Null[string]()
	}
	return Some(*p)
}

// LastEvent is the last event applied to a tab.
type LastEvent struct {
	Name string  `json:"name"`
	At   float64 `json:"at"`
	Seq  int     `json:"seq"`
}

// Entry is everything the keeper knows about one tab.
type Entry struct {
	CliState             string
	WorkspaceID          string
	TabName              string
	CurrentProcess       Opt[string]
	PaneTitle            Opt[string]
	TmuxSession          string
	PanelType            Opt[string]
	TerminalStatus       Opt[string]
	ListeningPorts       Opt[[]int]
	AgentProviderID      Opt[string]
	AgentSummary         Opt[string]
	LastUserMessage      Opt[string]
	LastAssistantMessage Opt[string]
	CurrentAction        Opt[agents.CurrentAction]
	ReadyForReviewAt     Opt[float64]
	BusySince            Opt[float64]
	DismissedAt          Opt[float64]
	AgentSessionID       Opt[string]
	CompactingSince      Opt[float64]
	PermissionRequest    Opt[json.RawMessage]
	LastEvent            Opt[LastEvent]
	EventSeq             int
	LaunchingSince       *float64
	LaunchFailed         bool
	HooksMissing         bool

	// Kept on the server only.
	JSONLPath             string
	ProcessRetries        int
	StuckCheckedAt        float64
	StuckBackoffMs        float64
	LastHookFiredAt       *float64
	HooksCheckedFor       *string
	HooksCheckedAt        *float64
	GoneSince             *float64
	LastInterruptTs       float64
	LastResumeOrStartedAt *float64
}

func (e *Entry) lastEventAt() float64 {
	if e.LastEvent.Has() {
		return e.LastEvent.Val.At
	}
	return 0
}

func (e *Entry) lastEventName() string {
	if e.LastEvent.Has() {
		return e.LastEvent.Val.Name
	}
	return ""
}

func (e *Entry) lastEventSeq() int {
	if e.LastEvent.Has() {
		return e.LastEvent.Val.Seq
	}
	return 0
}

// clientJSON is the entry as the page gets it, keys in the order the old runtime wrote them, absent fields left out.
// prefix is written first (the type and tab id of a status:update).
func (e *Entry) clientJSON(prefix ...[2]any) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	put := func(k string, v any) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.Write(nodejson.Marshal(k))
		b.WriteByte(':')
		b.Write(nodejson.Marshal(v))
	}
	opt := func(k string, set bool, v any) {
		if set {
			put(k, v)
		}
	}
	for _, kv := range prefix {
		put(kv[0].(string), kv[1])
	}
	put("cliState", e.CliState)
	put("workspaceId", e.WorkspaceID)
	put("tabName", e.TabName)
	opt("currentProcess", e.CurrentProcess.Set, e.CurrentProcess.Val)
	opt("paneTitle", e.PaneTitle.Set, e.PaneTitle.Val)
	opt("panelType", e.PanelType.Set, e.PanelType.Val)
	opt("terminalStatus", e.TerminalStatus.Set, e.TerminalStatus.Val)
	opt("listeningPorts", e.ListeningPorts.Set, e.ListeningPorts.Val)
	opt("agentProviderId", e.AgentProviderID.Set, e.AgentProviderID.Val)
	opt("agentSummary", e.AgentSummary.Set, e.AgentSummary.Val)
	opt("lastUserMessage", e.LastUserMessage.Set, e.LastUserMessage.Val)
	opt("lastAssistantMessage", e.LastAssistantMessage.Set, e.LastAssistantMessage.Val)
	opt("currentAction", e.CurrentAction.Set, e.CurrentAction.Val)
	opt("readyForReviewAt", e.ReadyForReviewAt.Set, e.ReadyForReviewAt.Val)
	opt("busySince", e.BusySince.Set, e.BusySince.Val)
	opt("dismissedAt", e.DismissedAt.Set, e.DismissedAt.Val)
	opt("agentSessionId", e.AgentSessionID.Set, e.AgentSessionID.Val)
	opt("compactingSince", e.CompactingSince.Set, e.CompactingSince.Val)
	opt("permissionRequest", e.PermissionRequest.Set, e.PermissionRequest.Val)
	opt("lastEvent", e.LastEvent.Set, e.LastEvent.Val)
	put("eventSeq", e.EventSeq)
	put("launchingSince", e.LaunchingSince)
	put("launchFailed", e.LaunchFailed)
	put("hooksMissing", e.HooksMissing)
	b.WriteByte('}')
	return b.Bytes()
}

// ClientEntry is an entry ready to be written as JSON.
type ClientEntry json.RawMessage

func (c ClientEntry) MarshalJSON() ([]byte, error) { return c, nil }

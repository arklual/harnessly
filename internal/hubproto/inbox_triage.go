package hubproto

// The machine agent signs a short-lived grant for this operation only; it is not a page session.
const (
	OpInboxTriageGrant  = "runtime.inbox-triage.grant"
	InboxTriageAudience = "inbox-triage"
	InboxTriageSuffix   = "/inbox/triage"
	InboxTriageMaxBody  = 160 << 10
	InboxTriageMaxText  = 24 << 10
	InboxTriageMaxReply = 256 << 10
)

type InboxTriageInput struct {
	System string `json:"system"`
	Prompt string `json:"prompt"`
}

func (v InboxTriageInput) Valid() bool {
	return len(v.System) > 0 && len(v.System) <= 8<<10 && len(v.Prompt) > 0 && len(v.Prompt) <= InboxTriageMaxText
}

type InboxTriageOutput struct {
	Used  bool   `json:"used"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

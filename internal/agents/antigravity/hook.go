package antigravity

import (
	"encoding/json"
	"errors"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// HookPayload is the session binding subset of a native hook. Event names are
// intentionally not mapped until validated against the installed CLI's hooks.
type HookPayload struct {
	SessionID      string   `json:"conversationId"`
	TranscriptPath string   `json:"transcriptPath"`
	WorkspacePaths []string `json:"workspacePaths"`
	FullyIdle      bool     `json:"fullyIdle"`
	LastUserInput  string   `json:"lastUserInput"`
}

// ParseHookPayload validates explicit session identity and a home-bound native
// transcript path. It never guesses a conversation from cwd or latest history.
func ParseHookPayload(home string, data []byte) (HookPayload, error) {
	var p HookPayload
	if len(data) > 64<<10 {
		return p, errors.New("Antigravity hook payload too large")
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if !agents.IsUUID(p.SessionID) {
		return HookPayload{}, errors.New("Antigravity hook has no valid session UUID")
	}
	if p.TranscriptPath != "" && !ValidateTranscriptPath(home, p.TranscriptPath, p.SessionID) {
		return HookPayload{}, errors.New("Antigravity hook transcript does not match session")
	}
	return p, nil
}

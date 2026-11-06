package apps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// The app's agent.context: a tool of the app (named "<server>.<tool>") whose answer the agent gets as context. The
// chat's hooks (harnessly hook app-context) call it on SessionStart and UserPromptSubmit, and hand the answer to the
// agent as additionalContext only when it differs from the last one sent to that chat, so the conversation log carries
// the app's context once per change, not once per prompt. Only items with mode on-activation are sent; on-demand ones
// are for the agent to ask.

// ContextMaxBytes bounds what one context item may add: a tool that answers more is cut, as an app's instructions are.
const ContextMaxBytes = 16 << 10

// ContextItems are the app's context items the hooks send.
func ContextItems(m *Manifest) []ContextItem {
	if m == nil || m.Agent == nil {
		return nil
	}
	var out []ContextItem
	for _, c := range m.Agent.Context {
		if c.Mode == "on-activation" {
			out = append(out, c)
		}
	}
	return out
}

// ContextText is the text of a tools/call result: its text parts joined, cut at ContextMaxBytes; "" for an error
// result or one without text.
func ContextText(result json.RawMessage) string {
	var r struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(result, &r) != nil || r.IsError {
		return ""
	}
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			parts = append(parts, strings.TrimSpace(c.Text))
		}
	}
	text := strings.Join(parts, "\n\n")
	if len(text) > ContextMaxBytes {
		text = strings.ToValidUTF8(text[:ContextMaxBytes], "")
	}
	return text
}

// ContextDigest is the hash kept per chat to tell whether a context changed.
func ContextDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

package budget

import (
	"encoding/json"
	"os"

	"github.com/ly-solutions/harnessly/internal/limits"
)

// Accounts says which account each CLI runs as on this machine, as the hub names it (accountRef, docs/hub/usage.md).
// Only subscription logins are known here: a chat on an API key has no account, so an entry that names an account never
// matches it (an entry for any account still does).
//
// TODO(HRN-588): take the account from the usage collector's record of the chat (usagefeed.Local) once it lands; it
// knows keys and other endpoints too.
type Accounts struct {
	// ClaudeJSON is the file Claude Code keeps its account in (~/.claude.json, or $CLAUDE_CONFIG_DIR/.claude.json).
	ClaudeJSON string
	// CodexHome is the Codex CLI's home (~/.codex).
	CodexHome string
}

// Of is the accountRef of the provider ("claude" or "codex"), "" for unknown.
func (a Accounts) Of(provider string) string {
	switch provider {
	case "claude":
		raw, err := os.ReadFile(a.ClaudeJSON)
		if err != nil {
			return ""
		}
		var f struct {
			OAuthAccount *struct {
				AccountUUID string `json:"accountUuid"`
			} `json:"oauthAccount"`
		}
		if json.Unmarshal(raw, &f) != nil || f.OAuthAccount == nil || f.OAuthAccount.AccountUUID == "" {
			return ""
		}
		return "claude:" + f.OAuthAccount.AccountUUID
	case "codex":
		if l := limits.ReadCodexLogin(a.CodexHome); l != nil && l.AccountID != "" {
			return "codex:" + l.AccountID
		}
	}
	return ""
}

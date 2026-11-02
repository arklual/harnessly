package usagefeed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Identity is what the machine can say of the account its agent CLI uses now: never a key or a token, only a stable
// name of the account (docs/hub/usage.md, accountRef), how it signs in, and where its calls go.
type Identity struct {
	AccountRef  string
	AuthMode    string // subscription, api_key, ""
	BaseURLHost string // "" for the CLI's default
}

// Local reads the identity from what the CLIs keep on this machine. Getenv is the environment a CLI would be started
// with (the agent's env files over its own); Home is the account's home.
type Local struct {
	Home   string
	Getenv func(provider, name string) string
}

func (l Local) env(provider, name string) string {
	if l.Getenv == nil {
		return os.Getenv(name)
	}
	return strings.TrimSpace(l.Getenv(provider, name))
}

// KeyRef is how a key is named in a record: a hash computed here, so the key itself never leaves the process.
func KeyRef(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "key:sha256:" + hex.EncodeToString(sum[:])[:16]
}

// HostOf is the host of a base URL, "" for the providers' own endpoints.
func HostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	if h == "api.anthropic.com" || h == "api.openai.com" {
		return ""
	}
	return h
}

// Identity of the account the provider's CLI runs as.
func (l Local) Identity(provider string) Identity {
	switch provider {
	case "claude":
		return l.claude()
	case "codex":
		return l.codex()
	}
	return Identity{}
}

func readJSON(path string, into any) bool {
	raw, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(raw, into) == nil
}

func (l Local) claude() Identity {
	dir := l.env("claude", "CLAUDE_CONFIG_DIR")
	cfgFile := filepath.Join(l.Home, ".claude.json")
	if dir != "" {
		cfgFile = filepath.Join(dir, ".claude.json")
	} else {
		dir = filepath.Join(l.Home, ".claude")
	}
	// The CLI's own settings can carry the key and the endpoint too, under "env".
	var settings struct {
		Env map[string]any `json:"env"`
	}
	readJSON(filepath.Join(dir, "settings.json"), &settings)
	get := func(name string) string {
		if v := l.env("claude", name); v != "" {
			return v
		}
		s, _ := settings.Env[name].(string)
		return strings.TrimSpace(s)
	}
	id := Identity{BaseURLHost: HostOf(get("ANTHROPIC_BASE_URL"))}
	if key := get("ANTHROPIC_API_KEY"); key != "" {
		id.AccountRef, id.AuthMode = KeyRef(key), "api_key"
		return id
	}
	if key := get("ANTHROPIC_AUTH_TOKEN"); key != "" {
		id.AccountRef, id.AuthMode = KeyRef(key), "api_key"
		return id
	}
	var home struct {
		OAuthAccount *struct {
			AccountUUID string `json:"accountUuid"`
		} `json:"oauthAccount"`
	}
	if readJSON(cfgFile, &home) && home.OAuthAccount != nil && plainID(home.OAuthAccount.AccountUUID) {
		id.AccountRef, id.AuthMode = "claude:"+home.OAuthAccount.AccountUUID, "subscription"
	}
	return id
}

func (l Local) codex() Identity {
	dir := l.env("codex", "CODEX_HOME")
	if dir == "" {
		dir = filepath.Join(l.Home, ".codex")
	}
	id := Identity{BaseURLHost: HostOf(l.env("codex", "OPENAI_BASE_URL"))}
	var auth struct {
		Mode   string  `json:"auth_mode"`
		Key    *string `json:"OPENAI_API_KEY"`
		Tokens *struct {
			AccountID string `json:"account_id"`
		} `json:"tokens"`
	}
	readJSON(filepath.Join(dir, "auth.json"), &auth)
	if auth.Tokens != nil && auth.Mode != "apikey" && plainID(auth.Tokens.AccountID) {
		id.AccountRef, id.AuthMode = "codex:"+auth.Tokens.AccountID, "subscription"
		return id
	}
	key := l.env("codex", "OPENAI_API_KEY")
	if key == "" {
		key = l.env("codex", "CODEX_API_KEY")
	}
	if key == "" && auth.Key != nil {
		key = strings.TrimSpace(*auth.Key)
	}
	if key != "" {
		id.AccountRef, id.AuthMode = KeyRef(key), "api_key"
	}
	return id
}

// plainID: an account id as the CLIs write it (a uuid or a short token), nothing that could be text.
func plainID(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

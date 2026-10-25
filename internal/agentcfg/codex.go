package agentcfg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Codex keeps its settings in <codex home>/config.toml.

// codexKey is where Codex keeps an option: the table (nil for the top of the file) and the key.
type codexAt struct {
	table []string
	key   string
}

var codexKey = map[wire.AgentOptionKey]codexAt{
	wire.OptModel:       {nil, "model"},
	wire.OptEffort:      {nil, "model_reasoning_effort"},
	wire.OptBaseURL:     {nil, "openai_base_url"},
	wire.OptSummary:     {nil, "model_reasoning_summary"},
	wire.OptFast:        {nil, "service_tier"}, // "fast" is on, the key's other values are off
	wire.OptSandboxMode: {nil, "sandbox_mode"},
	wire.OptNetwork:     {[]string{"sandbox_workspace_write"}, "network_access"},
	wire.OptApproval:    {nil, "approval_policy"},
	wire.OptSearch:      {nil, "web_search"},
	wire.OptHistory:     {[]string{"history"}, "persistence"}, // "none" is off, "save-all" on
}

func tomlMap(text string) (map[string]any, error) {
	var out map[string]any
	if _, err := toml.Decode(text, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func tomlDig(m map[string]any, path []string, key string) (any, bool) {
	cur := m
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	v, ok := cur[key]
	return v, ok
}

func (s *Store) readCodex() (wire.AgentSettings, error) {
	path, _ := s.file(wire.ProviderCodex)
	raw, err := readFile(path)
	if err != nil {
		return wire.AgentSettings{}, err
	}
	m, err := tomlMap(string(raw))
	if err != nil {
		return wire.AgentSettings{}, fmt.Errorf("agentcfg: %s does not read: %w", s.display(path), err)
	}
	return s.codexView(path, raw, m), nil
}

func (s *Store) codexView(path string, raw []byte, m map[string]any) wire.AgentSettings {
	values := map[wire.AgentOptionKey]any{}
	for key, at := range codexKey {
		v, ok := tomlDig(m, at.table, at.key)
		if !ok {
			continue
		}
		switch key {
		case wire.OptFast:
			if tier, isText := v.(string); isText {
				values[key] = tier == "fast"
			}
		case wire.OptHistory:
			if mode, isText := v.(string); isText {
				values[key] = mode != "none"
			}
		default:
			values[key] = v
		}
	}
	mcp := []wire.AgentMcp{}
	servers, _ := m["mcp_servers"].(map[string]any)
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		server, _ := servers[name].(map[string]any)
		entry := wire.AgentMcp{Name: name, Kind: wire.McpStdio, Enabled: true}
		if command, ok := server["command"].(string); ok {
			entry.Target = filepath.Base(command)
		}
		if address, ok := server["url"].(string); ok {
			entry.Kind, entry.Target = wire.McpHTTP, hostOf(address)
		}
		if on, ok := server["enabled"].(bool); ok {
			entry.Enabled = on
		}
		if entry.Target == "" || entry.Target == "." || entry.Validate() != nil {
			continue
		}
		mcp = append(mcp, entry)
	}
	return wire.AgentSettings{
		Provider: wire.ProviderCodex, Path: s.display(path), Rev: revOf(raw), Account: s.codexAccount(m),
		Options: options(wire.ProviderCodex, values), Mcp: mcp,
	}
}

// codexAccount: Codex keeps its sign-in in auth.json, either a ChatGPT login (whose id token names the person and the
// plan) or an API key. Only the method, the plan and the e-mail leave this function.
func (s *Store) codexAccount(_ map[string]any) wire.AgentAccount {
	raw, err := readFile(filepath.Join(s.cfg.CodexDir, "auth.json"))
	if err != nil || len(raw) == 0 {
		return s.codexEnvAccount()
	}
	var auth struct {
		Mode   string  `json:"auth_mode"`
		Key    *string `json:"OPENAI_API_KEY"`
		Tokens *struct {
			IDToken string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &auth) != nil {
		return wire.AgentAccount{}
	}
	if auth.Tokens != nil && auth.Tokens.IDToken != "" && auth.Mode != "apikey" {
		account := wire.AgentAccount{SignedIn: true, Method: ptr(wire.AccountSubscription)}
		if claims := jwtClaims(auth.Tokens.IDToken); claims != nil {
			if claims.Email != "" {
				account.Who = ptr(claims.Email)
			}
			if plan := title(claims.Auth.Plan); plan != "" {
				account.Plan = ptr(plan)
			}
		}
		return account
	}
	if auth.Key != nil && *auth.Key != "" {
		return wire.AgentAccount{SignedIn: true, Method: ptr(wire.AccountKey)}
	}
	return s.codexEnvAccount()
}

func (s *Store) codexEnvAccount() wire.AgentAccount {
	if s.cfg.Getenv("OPENAI_API_KEY") != "" || s.cfg.Getenv("CODEX_API_KEY") != "" {
		return wire.AgentAccount{SignedIn: true, Method: ptr(wire.AccountKey)}
	}
	return wire.AgentAccount{}
}

type idClaims struct {
	Email string `json:"email"`
	Auth  struct {
		Plan string `json:"chatgpt_plan_type"`
	} `json:"https://api.openai.com/auth"`
}

// jwtClaims reads the payload of a token; it checks nothing, it only reads what the person's own CLI stored.
func jwtClaims(token string) *idClaims {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	var c idClaims
	if json.Unmarshal(payload, &c) != nil {
		return nil
	}
	return &c
}

// codexLiteral is the TOML an option's value is written as; ok is false for a value that unsets the key.
func codexLiteral(key wire.AgentOptionKey, v any) (string, bool, error) {
	if v == nil {
		return "", false, nil
	}
	switch key {
	case wire.OptFast:
		if on, _ := v.(bool); on {
			lit, err := tomlLiteral("fast")
			return lit, true, err
		}
		lit, err := tomlLiteral("default")
		return lit, true, err
	case wire.OptHistory:
		if on, _ := v.(bool); on {
			lit, err := tomlLiteral("save-all")
			return lit, true, err
		}
		lit, err := tomlLiteral("none")
		return lit, true, err
	}
	lit, err := tomlLiteral(v)
	return lit, true, err
}

func (s *Store) patchCodex(rev string, patch wire.AgentSettingsPatch) (wire.AgentSettings, error) {
	path, _ := s.file(wire.ProviderCodex)
	raw, err := readFile(path)
	if err != nil {
		return wire.AgentSettings{}, err
	}
	if rev != revOf(raw) {
		return wire.AgentSettings{}, wire.ErrAgentSettingsConflict
	}
	before, err := tomlMap(string(raw))
	if err != nil {
		return wire.AgentSettings{}, fmt.Errorf("%w: %s does not read", wire.ErrAgentSettingsRefused, s.display(path))
	}
	doc := parseTOMLText(string(raw))
	// want is the file read as a parser would after the change: the old one with these keys changed.
	want, err := tomlMap(string(raw))
	if err != nil {
		return wire.AgentSettings{}, err
	}
	change := func(table []string, key string, lit string, set bool, value any) {
		if set {
			doc.set(table, key, lit)
		} else {
			doc.remove(table, key)
		}
		setIn(want, table, key, value, set)
	}
	for _, o := range patch.Options {
		at := codexKey[o.Key]
		lit, set, err := codexLiteral(o.Key, o.Value)
		if err != nil {
			return wire.AgentSettings{}, fmt.Errorf("%w: %v", wire.ErrAgentSettingsRefused, err)
		}
		var value any
		if set {
			value = parsedValue(lit)
		}
		change(at.table, at.key, lit, set, value)
	}
	if patch.Mcp != nil {
		table := []string{"mcp_servers", patch.Mcp.Name}
		if _, ok := tomlDig(before, []string{"mcp_servers"}, patch.Mcp.Name); !ok {
			return wire.AgentSettings{}, fmt.Errorf("%w: no server %q", wire.ErrAgentSettingsRefused, patch.Mcp.Name)
		}
		if patch.Mcp.Enabled {
			change(table, "enabled", "", false, nil)
		} else {
			change(table, "enabled", "false", true, false)
		}
	}
	text := doc.String()
	got, err := tomlMap(text)
	if err != nil || !reflect.DeepEqual(got, want) {
		return wire.AgentSettings{}, fmt.Errorf("%w: %s is written in a way this page cannot change safely", wire.ErrAgentSettingsRefused, s.display(path))
	}
	out := []byte(text)
	if text != string(raw) {
		if err := writeFile(path, out); err != nil {
			return wire.AgentSettings{}, err
		}
	} else {
		out = raw
	}
	return s.codexView(path, out, got), nil
}

// parsedValue is a literal as the TOML parser reads it.
func parsedValue(lit string) any {
	m, err := tomlMap("v = " + lit)
	if err != nil {
		return nil
	}
	return m["v"]
}

// setIn sets or removes a key of a table in a decoded document, making the tables on the way.
func setIn(m map[string]any, table []string, key string, v any, set bool) {
	cur := m
	for _, k := range table {
		next, ok := cur[k].(map[string]any)
		if !ok {
			if !set {
				return
			}
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	if set {
		cur[key] = v
	} else {
		delete(cur, key)
	}
}

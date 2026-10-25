package agentcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Claude Code keeps its settings in <config>/settings.json: a JSON object of which the page models a few keys.

const baseURLVar = "ANTHROPIC_BASE_URL"

// claudeKey is where Claude Code keeps an option: a path of keys in settings.json.
var claudeKey = map[wire.AgentOptionKey][]string{
	wire.OptModel:          {"model"},
	wire.OptEffort:         {"effortLevel"},
	wire.OptUltracode:      {"ultracode"},
	wire.OptBaseURL:        {"env", baseURLVar},
	wire.OptFallbackModel:  {"fallbackModel"},
	wire.OptThinking:       {"alwaysThinkingEnabled"},
	wire.OptOutputStyle:    {"outputStyle"},
	wire.OptPermissionMode: {"permissions", "defaultMode"},
	wire.OptSandbox:        {"sandbox", "enabled"},
	wire.OptProjectMcp:     {"enableAllProjectMcpServers"},
	wire.OptCompactWindow:  {"autoCompactWindow"},
	wire.OptMemory:         {"autoMemoryEnabled"},
	wire.OptKeepDays:       {"cleanupPeriodDays"},
	wire.OptHooks:          {"disableAllHooks"}, // inverted: the page's switch says the hooks run
	wire.OptCoauthor:       {"includeCoAuthoredBy"},
	wire.OptUpdates:        {"autoUpdatesChannel"},
}

// jsonObject reads a settings file as an object; a missing or empty file is an empty object, anything else that is not
// an object is an error and is never written over.
func jsonObject(raw []byte) (*jsvalue.Object, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return jsvalue.NewObject(), nil
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return nil, errors.New("agentcfg: the settings are not an object")
	}
	return o, nil
}

// dig is the value at a path of keys; ok is false when a key on the way is absent or not an object.
func dig(o *jsvalue.Object, path ...string) (any, bool) {
	cur := o
	for i, k := range path {
		v, ok := cur.Get(k)
		if !ok {
			return nil, false
		}
		if i == len(path)-1 {
			return v, true
		}
		next, ok := v.(*jsvalue.Object)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// put sets the value at a path, making the objects on the way; one that is there and is not an object is replaced.
func put(o *jsvalue.Object, v any, path ...string) {
	cur := o
	for _, k := range path[:len(path)-1] {
		next := cur.Object(k)
		if next == nil {
			next = jsvalue.NewObject()
			cur.Set(k, next)
		}
		cur = next
	}
	cur.Set(path[len(path)-1], v)
}

// drop removes the value at a path and the objects it leaves empty.
func drop(o *jsvalue.Object, path ...string) {
	chain := []*jsvalue.Object{o}
	for _, k := range path[:len(path)-1] {
		next := chain[len(chain)-1].Object(k)
		if next == nil {
			return
		}
		chain = append(chain, next)
	}
	chain[len(chain)-1].Delete(path[len(path)-1])
	for i := len(chain) - 1; i > 0; i-- {
		if chain[i].Len() > 0 {
			return
		}
		chain[i-1].Delete(path[i-1])
	}
}

func stringList(v any) []string {
	list, _ := v.([]any)
	out := []string{}
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (s *Store) readClaude() (wire.AgentSettings, error) {
	path, _ := s.file(wire.ProviderClaude)
	raw, err := readFile(path)
	if err != nil {
		return wire.AgentSettings{}, err
	}
	obj, err := jsonObject(raw)
	if err != nil {
		return wire.AgentSettings{}, fmt.Errorf("agentcfg: %s does not read: %w", s.display(path), err)
	}
	return s.claudeView(path, raw, obj), nil
}

func (s *Store) claudeView(path string, raw []byte, obj *jsvalue.Object) wire.AgentSettings {
	values := map[wire.AgentOptionKey]any{}
	for key, at := range claudeKey {
		v, ok := dig(obj, at...)
		if !ok {
			continue
		}
		if key == wire.OptHooks {
			if off, isBool := v.(bool); isBool {
				values[key] = !off
			}
			continue
		}
		values[key] = v
	}
	rules := &wire.AgentRules{Allow: []string{}, Ask: []string{}, Deny: []string{}}
	if v, ok := dig(obj, "permissions", "allow"); ok {
		rules.Allow = stringList(v)
	}
	if v, ok := dig(obj, "permissions", "ask"); ok {
		rules.Ask = stringList(v)
	}
	if v, ok := dig(obj, "permissions", "deny"); ok {
		rules.Deny = stringList(v)
	}
	env := []wire.AgentEnvVar{}
	if e := obj.Object("env"); e != nil {
		for _, k := range e.Keys() {
			v, _ := e.Get(k)
			text, isText := v.(string)
			if !isText || k == baseURLVar || !isEnvName(k) {
				continue
			}
			secret := wire.SecretName.MatchString(k)
			if secret {
				text = tail(text)
			}
			env = append(env, wire.AgentEnvVar{Key: k, Value: text, Secret: secret})
		}
	}
	account := s.claudeAccount(obj)
	return wire.AgentSettings{
		Provider: wire.ProviderClaude, Path: s.display(path), Rev: revOf(raw), Account: account,
		Options: options(wire.ProviderClaude, values), Rules: rules, Env: env, Mcp: s.claudeMcp(rules),
	}
}

func isEnvName(k string) bool {
	return (wire.AgentEnvVar{Key: k}).Validate() == nil
}

// claudeAccount: a subscription when Claude Code holds a sign-in, a key when the settings or the environment name one.
func (s *Store) claudeAccount(obj *jsvalue.Object) wire.AgentAccount {
	var home struct {
		OAuthAccount *struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if raw, err := readFile(s.cfg.ClaudeJSON); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &home)
	}
	var who *string
	if home.OAuthAccount != nil && home.OAuthAccount.EmailAddress != "" {
		who = ptr(home.OAuthAccount.EmailAddress)
	}
	var creds struct {
		ClaudeAiOauth *struct {
			SubscriptionType string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if raw, err := readFile(filepath.Join(s.cfg.ClaudeDir, ".credentials.json")); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &creds)
	}
	if creds.ClaudeAiOauth != nil {
		var plan *string
		if p := title(creds.ClaudeAiOauth.SubscriptionType); p != "" {
			plan = ptr(p)
		}
		return wire.AgentAccount{SignedIn: true, Method: ptr(wire.AccountSubscription), Plan: plan, Who: who}
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if v, ok := dig(obj, "env", name); ok {
			if str, isText := v.(string); isText && str != "" {
				return wire.AgentAccount{SignedIn: true, Method: ptr(wire.AccountKey)}
			}
		}
		if s.cfg.Getenv(name) != "" {
			return wire.AgentAccount{SignedIn: true, Method: ptr(wire.AccountKey)}
		}
	}
	return wire.AgentAccount{}
}

// claudeMcp lists the servers Claude Code has for the person (`claude mcp add --scope user`); one a deny rule names is off.
func (s *Store) claudeMcp(rules *wire.AgentRules) []wire.AgentMcp {
	out := []wire.AgentMcp{}
	raw, err := readFile(s.cfg.ClaudeJSON)
	if err != nil || len(raw) == 0 {
		return out
	}
	var file struct {
		McpServers map[string]struct {
			Command string `json:"command"`
			URL     string `json:"url"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return out
	}
	names := make([]string, 0, len(file.McpServers))
	for name := range file.McpServers {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		server := file.McpServers[name]
		m := wire.AgentMcp{Name: name, Kind: wire.McpStdio, Target: filepath.Base(server.Command), Enabled: !slices.Contains(rules.Deny, "mcp__"+name)}
		if server.URL != "" {
			m.Kind, m.Target = wire.McpHTTP, hostOf(server.URL)
		}
		if m.Target == "" || m.Target == "." || m.Validate() != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (s *Store) patchClaude(rev string, patch wire.AgentSettingsPatch) (wire.AgentSettings, error) {
	path, _ := s.file(wire.ProviderClaude)
	raw, err := readFile(path)
	if err != nil {
		return wire.AgentSettings{}, err
	}
	if rev != revOf(raw) {
		return wire.AgentSettings{}, wire.ErrAgentSettingsConflict
	}
	obj, err := jsonObject(raw)
	if err != nil {
		return wire.AgentSettings{}, fmt.Errorf("%w: %s does not read", wire.ErrAgentSettingsRefused, s.display(path))
	}
	for _, o := range patch.Options {
		at := claudeKey[o.Key]
		switch {
		case o.Value == nil:
			drop(obj, at...)
		case o.Key == wire.OptHooks:
			// The hooks run unless disableAllHooks says otherwise: «on» is the key's absence.
			if on, _ := o.Value.(bool); on {
				drop(obj, at...)
			} else {
				put(obj, true, at...)
			}
		default:
			put(obj, o.Value, at...)
		}
	}
	if patch.Rules != nil {
		for _, kind := range []struct {
			name string
			list []string
		}{{"allow", patch.Rules.Allow}, {"ask", patch.Rules.Ask}, {"deny", patch.Rules.Deny}} {
			if len(kind.list) == 0 {
				drop(obj, "permissions", kind.name)
				continue
			}
			put(obj, jsvalue.StringArray(kind.list), "permissions", kind.name)
		}
	}
	for _, k := range patch.EnvUnset {
		if k == baseURLVar {
			return wire.AgentSettings{}, fmt.Errorf("%w: %s is the option baseUrl", wire.ErrAgentSettingsRefused, k)
		}
		drop(obj, "env", k)
	}
	for _, e := range patch.EnvSet {
		put(obj, e.Value, "env", e.Key)
	}
	if patch.Mcp != nil {
		deny := stringList(mustDig(obj, "permissions", "deny"))
		rule := "mcp__" + patch.Mcp.Name
		deny = slices.DeleteFunc(deny, func(r string) bool { return r == rule })
		if !patch.Mcp.Enabled {
			deny = append(deny, rule)
		}
		if len(deny) == 0 {
			drop(obj, "permissions", "deny")
		} else {
			put(obj, jsvalue.StringArray(deny), "permissions", "deny")
		}
	}
	out := append(jsvalue.Stringify(obj, "  "), '\n')
	if string(out) != string(raw) {
		if err := writeFile(path, out); err != nil {
			return wire.AgentSettings{}, err
		}
	} else {
		out = raw
	}
	return s.claudeView(path, out, obj), nil
}

func mustDig(o *jsvalue.Object, path ...string) any {
	v, _ := dig(o, path...)
	return v
}

package wirefake

import (
	"context"
	"slices"
	"sync"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// agentSettings is the fake machine's Settings files: Claude Code's and Codex's options, held in memory. A write follows
// the same rules as the runtime's: a patch made on another revision is a conflict, a null unsets an option, and
// `POST /api/v2/fake/reset` puts the files back.
type agentSettings struct {
	mu    sync.Mutex
	files map[wire.Provider]*fakeFile
}

type fakeFile struct {
	rev     int
	options map[wire.AgentOptionKey]any
	rules   wire.AgentRules
	env     []wire.AgentEnvVar
	mcp     []wire.AgentMcp
}

var _ wire.AgentSettingsSource = (*agentSettings)(nil)

// AgentSettings is the fake machine's Settings files.
func (m *Machine) AgentSettings() wire.AgentSettingsSource { return m.settings }

func newAgentSettings() *agentSettings {
	a := &agentSettings{}
	a.reset()
	return a
}

func (a *agentSettings) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.files = map[wire.Provider]*fakeFile{
		wire.ProviderClaude: {
			rev: 1,
			options: map[wire.AgentOptionKey]any{
				wire.OptModel: "opus", wire.OptEffort: "high", wire.OptThinking: true, wire.OptPermissionMode: "acceptEdits",
				wire.OptMemory: true, wire.OptKeepDays: float64(90), wire.OptCoauthor: false,
			},
			rules: wire.AgentRules{Allow: []string{"Bash(npm run *)", "Bash(git diff *)"}, Ask: []string{}, Deny: []string{"Read(./.env)"}},
			env:   []wire.AgentEnvVar{{Key: "BASH_DEFAULT_TIMEOUT_MS", Value: "120000"}, {Key: "GITLAB_TOKEN", Value: "••••c7f2", Secret: true}},
			mcp: []wire.AgentMcp{
				{Name: "angular-cli", Kind: wire.McpStdio, Target: "npx", Enabled: true},
				{Name: "tracker", Kind: wire.McpHTTP, Target: "tracker.example.com", Enabled: true},
			},
		},
		wire.ProviderCodex: {
			rev: 1,
			options: map[wire.AgentOptionKey]any{
				wire.OptModel: "gpt-5.5", wire.OptEffort: "medium", wire.OptSandboxMode: "workspace-write", wire.OptApproval: "on-request",
				wire.OptSearch: "cached", wire.OptHistory: true,
			},
			mcp: []wire.AgentMcp{{Name: "docs", Kind: wire.McpHTTP, Target: "docs.example.com", Enabled: true}},
		},
	}
}

func (f *fakeFile) view(p wire.Provider) wire.AgentSettings {
	out := wire.AgentSettings{
		Provider: p, Rev: revText(f.rev), Mcp: slices.Clone(f.mcp),
		Account: wire.AgentAccount{SignedIn: true, Method: ptr(wire.AccountSubscription), Plan: ptr("Max"), Who: ptr("person@example.com")},
	}
	if p == wire.ProviderClaude {
		out.Path = "~/.claude/settings.json"
		rules := wire.AgentRules{Allow: slices.Clone(f.rules.Allow), Ask: slices.Clone(f.rules.Ask), Deny: slices.Clone(f.rules.Deny)}
		out.Rules, out.Env = &rules, slices.Clone(f.env)
	} else {
		out.Path = "~/.codex/config.toml"
		out.Account.Plan = ptr("Plus")
	}
	for _, spec := range wire.AgentOptionSpecs(p) {
		out.Options = append(out.Options, wire.AgentOption{Key: spec.Key, Value: f.options[spec.Key]})
	}
	return out
}

func revText(n int) string { return "r" + string(rune('0'+n%10)) + string(rune('a'+n/10%26)) }

func (a *agentSettings) AgentSettings(_ context.Context, p wire.Provider) (wire.AgentSettings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.files[p]
	if !ok {
		return wire.AgentSettings{}, wire.ErrAgentSettingsRefused
	}
	return f.view(p), nil
}

func (a *agentSettings) PatchAgentSettings(_ context.Context, p wire.Provider, rev string, patch wire.AgentSettingsPatch) (wire.AgentSettings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.files[p]
	if !ok {
		return wire.AgentSettings{}, wire.ErrAgentSettingsRefused
	}
	if rev != revText(f.rev) {
		return wire.AgentSettings{}, wire.ErrAgentSettingsConflict
	}
	for _, o := range patch.Options {
		if o.Value == nil {
			delete(f.options, o.Key)
		} else {
			f.options[o.Key] = o.Value
		}
	}
	if patch.Rules != nil {
		f.rules = *patch.Rules
	}
	for _, k := range patch.EnvUnset {
		f.env = slices.DeleteFunc(f.env, func(e wire.AgentEnvVar) bool { return e.Key == k })
	}
	for _, e := range patch.EnvSet {
		v := wire.AgentEnvVar{Key: e.Key, Value: e.Value}
		if wire.SecretName.MatchString(e.Key) {
			v.Secret, v.Value = true, "••••"+e.Value[max(0, len(e.Value)-4):]
		}
		if i := slices.IndexFunc(f.env, func(x wire.AgentEnvVar) bool { return x.Key == e.Key }); i >= 0 {
			f.env[i] = v
		} else {
			f.env = append(f.env, v)
		}
	}
	if patch.Mcp != nil {
		i := slices.IndexFunc(f.mcp, func(m wire.AgentMcp) bool { return m.Name == patch.Mcp.Name })
		if i < 0 {
			return wire.AgentSettings{}, wire.ErrAgentSettingsRefused
		}
		f.mcp[i].Enabled = patch.Mcp.Enabled
	}
	f.rev++
	return f.view(p), nil
}

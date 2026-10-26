package providers

import (
	"fmt"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Launcher is what a launch on a backend profile asks of the profiles (agents.ProfileLauncher), over a Store.
type Launcher struct {
	Store *Store
	// Relay is where Codex reaches a Relayed profile; nil sends it straight to the vendor with the saved key.
	Relay *RelayEndpoint
}

// RelayEndpoint is the runtime's relay as Codex is told about it: the address of a profile's relay is
// <Base>/<profile id>/v1, and Codex proves it is the runtime's own with the CLI token, read from TokenEnv into Header
// (as for the runtime's MCP server), so the address is of no use to anything else on the machine.
type RelayEndpoint struct {
	Base     string
	Header   string
	TokenEnv string
}

// RelayPath is the path of a profile's relay under the runtime's relay prefix.
func RelayPath(prefix, profile string) string { return prefix + "/" + profile + "/v1" }

var _ agents.ProfileLauncher = Launcher{}

// ClaudeSettings is the settings file of a Claude Code chat on a profile and a model (Store.SettingsFile).
func (l Launcher) ClaudeSettings(profile, model, hooksPath string) (string, error) {
	return l.Store.SettingsFile(profile, model, hooksPath)
}

// Codex points Codex at the profile's provider: the address, the variable the key is read from, the Responses API. The
// key is not here; the launch line fills the variable from the key's file.
func (l Launcher) Codex(profile string) (agents.CodexProfile, error) {
	p, ok := Find(profile)
	if !ok {
		return agents.CodexProfile{}, fmt.Errorf("providers: no profile %q", profile)
	}
	if p.Via != wire.ProviderCodex {
		return agents.CodexProfile{}, fmt.Errorf("providers: %s does not run through Codex", profile)
	}
	prefix := "model_providers." + p.ID + "."
	if r := l.Relay; p.Relayed && r != nil && r.Base != "" && r.Header != "" && r.TokenEnv != "" {
		// No key in Codex's environment at all: the relay puts the credential on each request.
		return agents.CodexProfile{Args: []string{
			"-c", prefix + "name=" + strconv.Quote(p.Label),
			"-c", prefix + "base_url=" + strconv.Quote(RelayPath(r.Base, p.ID)),
			"-c", prefix + "env_http_headers={" + strconv.Quote(r.Header) + "=" + strconv.Quote(r.TokenEnv) + "}",
			"-c", prefix + `wire_api="responses"`,
			"-c", "model_provider=" + strconv.Quote(p.ID),
		}}, nil
	}
	return agents.CodexProfile{
		Args: []string{
			"-c", prefix + "base_url=" + strconv.Quote(p.BaseURL),
			"-c", prefix + "env_key=" + strconv.Quote(p.KeyEnv),
			"-c", prefix + `wire_api="responses"`,
			"-c", "model_provider=" + strconv.Quote(p.ID),
		},
		KeyEnv:  p.KeyEnv,
		KeyFile: l.Store.KeyFile(p.ID),
	}, nil
}

// Lookup says what a conversation may start on for the profile: the CLI it runs through, whether it is ready (a key is
// saved, or the vendor CLI is signed in), and
// the ids of its models (convo.Profiles).
func (l Launcher) Lookup(id string) (via wire.Provider, configured bool, models []string, ok bool) {
	p, found := Find(id)
	if !found {
		return "", false, nil, false
	}
	for _, m := range p.Models {
		models = append(models, m.ID)
	}
	return p.Via, l.Store.Ready(id), models, true
}

// Model is the wire capability record of one of the profile's models.
func (Launcher) Model(profile, id string) (wire.AgentModel, bool) {
	p, ok := Find(profile)
	if !ok {
		return wire.AgentModel{}, false
	}
	for _, m := range p.AgentModels() {
		if m.ID == id {
			return m, true
		}
	}
	return wire.AgentModel{}, false
}

// Window is the context window of a model of the profile; 0 for one it does not know.
func (Launcher) Window(profile, model string) int {
	if p, ok := Find(profile); ok {
		if m, ok := p.ModelOf(model); ok {
			return m.Window
		}
	}
	return 0
}

// OwnsModel says whether the model id is one of some profile's: a conversation on the CLI's own account is not to be
// started on it or switched to it.
func (Launcher) OwnsModel(model string) bool {
	for _, p := range presets {
		if _, ok := p.ModelOf(model); ok {
			return true
		}
	}
	return false
}

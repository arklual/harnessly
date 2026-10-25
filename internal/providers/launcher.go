package providers

import (
	"fmt"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Launcher is what a launch on a backend profile asks of the profiles (agents.ProfileLauncher), over a Store.
type Launcher struct{ Store *Store }

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

// Lookup says what a conversation may start on for the profile: the CLI it runs through, whether a key is saved, and
// the ids of its models (convo.Profiles).
func (l Launcher) Lookup(id string) (via wire.Provider, configured bool, models []string, ok bool) {
	p, found := Find(id)
	if !found {
		return "", false, nil, false
	}
	for _, m := range p.Models {
		models = append(models, m.ID)
	}
	return p.Via, l.Store.Configured(id), models, true
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

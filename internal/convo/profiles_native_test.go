package convo

import (
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// collidingProfiles are fakeProfiles plus profiles whose ids are the CLIs' own too (GitHub #42): the built-in GPT
// profile runs through Claude Code with the ids Codex signed in with ChatGPT lists as its own, a custom profile of Codex
// on an API key lists one of them and a vendor-only id, and an Anthropic-compatible proxy re-lists a Claude alias.
type collidingProfiles struct{ fakeProfiles }

func (f collidingProfiles) Lookup(id string) (wire.Provider, bool, []string, bool) {
	switch id {
	case "gpt":
		return wire.ProviderClaude, f.configured, []string{"gpt-6.1-sol", "gpt-6-luna"}, true
	case "openai":
		return wire.ProviderCodex, f.configured, []string{"gpt-6.1-sol", "o-vendor"}, true
	case "claude-proxy":
		return wire.ProviderClaude, f.configured, []string{"sonnet"}, true
	}
	return f.fakeProfiles.Lookup(id)
}

func (f collidingProfiles) Model(profile, model string) (wire.AgentModel, bool) {
	_, _, models, ok := f.Lookup(profile)
	if !ok || !slices.Contains(models, model) {
		return wire.AgentModel{}, false
	}
	if m, ok := f.fakeProfiles.Model(profile, model); ok {
		return m, true
	}
	return wire.AgentModel{ID: model, Efforts: []wire.Effort{wire.EffortLow, wire.EffortHigh}}, true
}

func (f collidingProfiles) OwnsModel(m string) bool {
	for _, id := range []string{"gpt", "openai", "claude-proxy"} {
		if _, _, models, _ := f.Lookup(id); slices.Contains(models, m) {
			return true
		}
	}
	return f.fakeProfiles.OwnsModel(m)
}

// nativeModels: what each CLI's own account lists (Codex from models_cache.json on a ChatGPT sign-in).
func nativeModels(p wire.Provider) []wire.AgentModel {
	if p == wire.ProviderCodex {
		return []wire.AgentModel{{ID: "gpt-6.1-sol", Efforts: wire.Efforts}, {ID: "gpt-6-luna", Efforts: wire.Efforts}}
	}
	return []wire.AgentModel{{ID: "opus", Efforts: wire.Efforts}, {ID: "sonnet", Efforts: wire.Efforts}}
}

func collidingRig(t *testing.T, configured bool) *rig {
	r := profileRig(t, fakeProfiles{via: wire.ProviderClaude, configured: configured})
	r.rt.d.Profiles = collidingProfiles{fakeProfiles{via: wire.ProviderClaude, configured: configured}}
	r.rt.d.Models = nativeModels
	return r
}

// createStep is a conversation to create, the profile it is to run on and the refusal expected ("" for none).
type createStep struct {
	name    string
	cmd     wire.ConversationCreateCmd
	profile string
	code    wire.RefuseCode
}

// A model the CLI's own account lists runs on that account, whatever profile also lists the id and whether that
// profile is configured; a named profile keeps its routing; an id only a profile has stays refused without it.
func TestNativeModelsCollidingWithProfiles(t *testing.T) {
	for _, configured := range []bool{true, false} {
		name := map[bool]string{true: "configured", false: "unconfigured"}[configured]
		steps := []createStep{
			{"native codex model", wire.ConversationCreateCmd{Provider: wire.ProviderCodex, Model: &wire.Model{ID: "gpt-6.1-sol"}}, "", ""},
			{"native codex model, another one", wire.ConversationCreateCmd{Provider: wire.ProviderCodex, Model: &wire.Model{ID: "gpt-6-luna"}}, "", ""},
			{"native claude alias a proxy re-lists", wire.ConversationCreateCmd{Provider: wire.ProviderClaude, Model: &wire.Model{ID: "sonnet"}}, "", ""},
			{"vendor-only id of a codex profile", wire.ConversationCreateCmd{Provider: wire.ProviderCodex, Model: &wire.Model{ID: "o-vendor"}}, "", wire.RefuseOptionUnknown},
			{"gpt id on claude's own account", wire.ConversationCreateCmd{Provider: wire.ProviderClaude, Model: &wire.Model{ID: "gpt-6.1-sol"}}, "", wire.RefuseOptionUnknown},
			{"another vendor's id on codex", wire.ConversationCreateCmd{Provider: wire.ProviderCodex, Model: &wire.Model{ID: "kimi-k3"}}, "", wire.RefuseOptionUnknown},
		}
		if configured {
			steps = append(steps,
				createStep{"the codex profile named", wire.ConversationCreateCmd{Provider: wire.ProviderCodex, Profile: "openai", Model: &wire.Model{ID: "gpt-6.1-sol"}}, "openai", ""},
				createStep{"the gpt profile named", wire.ConversationCreateCmd{Provider: wire.ProviderClaude, Profile: "gpt", Model: &wire.Model{ID: "gpt-6.1-sol"}}, "gpt", ""},
			)
		} else {
			steps = append(steps, createStep{"the codex profile named without a key", wire.ConversationCreateCmd{Provider: wire.ProviderCodex, Profile: "openai", Model: &wire.Model{ID: "gpt-6.1-sol"}}, "", wire.RefuseUnsupported})
		}
		for _, c := range steps {
			r := collidingRig(t, configured)
			c.cmd.OpID, c.cmd.Cwd = "c1", "/w"
			res := r.do(c.cmd)
			label := name + ": " + c.name
			if c.code != "" {
				if res.R != wire.ResultRefused || res.Code != c.code || len(r.launcher.launched) != 0 {
					t.Errorf("%s: %+v", label, res)
				}
				continue
			}
			if res.R != wire.ResultAccepted || len(r.launcher.launched) != 1 {
				t.Errorf("%s: %+v", label, res)
				continue
			}
			got := r.launcher.launched[0]
			if got.Provider != c.cmd.Provider || got.Model == nil || got.Model.ID != c.cmd.Model.ID || got.Model.Profile != c.profile {
				t.Errorf("%s: launched %+v %+v", label, got, got.Model)
			}
		}
	}
}

// A plain Claude chat switches to a model of its own account that a profile re-lists, not to an id only a profile has.
func TestAPlainChatSwitchesToItsOwnModelAProfileAlsoLists(t *testing.T) {
	for _, configured := range []bool{true, false} {
		r := collidingRig(t, configured)
		r.open()
		r.settle(wire.StateReady)
		for model, want := range map[string]wire.RefuseCode{
			"sonnet":      "",
			"opus":        "",
			"gpt-6.1-sol": wire.RefuseOptionUnknown,
			"o-vendor":    wire.RefuseOptionUnknown,
			"glm-5":       wire.RefuseOptionUnknown,
		} {
			var code wire.RefuseCode
			if err := r.rt.on(r.rt.actor(r.id), func(cv *conv, _ time.Time) { code = cv.modelFits(wire.ModelCmd{Model: model}) }); err != nil {
				t.Fatal(err)
			}
			if code != want {
				t.Errorf("configured %v, %s: %q", configured, model, code)
			}
		}
	}
}

// A handoff to Codex on its own account takes the native id; to the profile it keeps the profile; a vendor-only id
// without its profile is refused.
func TestHandoffToANativeModelAProfileAlsoLists(t *testing.T) {
	for _, c := range []struct {
		name    string
		cmd     wire.ConversationHandoffCmd
		profile string
		code    wire.RefuseCode
	}{
		{"to codex's own account", wire.ConversationHandoffCmd{Provider: wire.ProviderCodex, Model: &wire.Model{ID: "gpt-6.1-sol"}}, "", ""},
		{"to the codex profile", wire.ConversationHandoffCmd{Provider: wire.ProviderCodex, Profile: "openai", Model: &wire.Model{ID: "gpt-6.1-sol"}}, "openai", ""},
		{"to the gpt profile", wire.ConversationHandoffCmd{Provider: wire.ProviderClaude, Profile: "gpt"}, "gpt", ""},
		{"a vendor-only id on codex", wire.ConversationHandoffCmd{Provider: wire.ProviderCodex, Model: &wire.Model{ID: "o-vendor"}}, "", wire.RefuseOptionUnknown},
		{"a gpt id on claude's own account", wire.ConversationHandoffCmd{Provider: wire.ProviderClaude, Model: &wire.Model{ID: "gpt-6-luna"}}, "", wire.RefuseOptionUnknown},
	} {
		r := collidingRig(t, true)
		r.twoTurns()
		c.cmd.OpID, c.cmd.ID, c.cmd.Mode, c.cmd.Text = "h1", r.id, wire.HandoffExcerpt, "context"
		res := r.do(c.cmd)
		if c.code != "" {
			if res.R != wire.ResultRefused || res.Code != c.code || len(r.launcher.launched) != 0 {
				t.Errorf("%s: %+v", c.name, res)
			}
			continue
		}
		if res.R != wire.ResultAccepted || res.ID == nil {
			t.Errorf("%s: %+v", c.name, res)
			continue
		}
		got := r.launcher.launched[len(r.launcher.launched)-1]
		if got.Provider != c.cmd.Provider || got.Model == nil || got.Model.Profile != c.profile || profileOf(r.recordOf(*res.ID).Model) != c.profile {
			t.Errorf("%s: launched %+v %+v", c.name, got, got.Model)
		}
	}
}

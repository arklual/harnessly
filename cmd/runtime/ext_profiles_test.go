package main

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestExtProfileOfAProvider(t *testing.T) {
	p := extproviders.Provider{ID: "corp", Name: "Corp proxy", BaseURL: "https://llm.corp.example/anthropic", API: extproviders.APIAnthropic,
		KeyRef: "provider-corp-key", Models: []extproviders.Model{{ID: "corp-large", Label: "Large"}, {ID: "has space"}, {ID: "corp-small"}},
		Env: []extproviders.EnvVar{{Name: "ANTHROPIC_MODEL", Value: "corp-large"}, {Name: "ANTHROPIC_AUTH_TOKEN", Secret: true, Ref: "provider-corp-env-anthropic-auth-token"}}}
	pr, ok := extProfile(p)
	if !ok {
		t.Fatal("an Anthropic-compatible provider is a profile of Claude Code")
	}
	if pr.ID != "x-corp" || pr.Label != "Corp proxy" || pr.Via != wire.ProviderClaude || !pr.Custom || pr.KeyRef != "provider-corp-key" {
		t.Fatalf("profile: %+v", pr)
	}
	// The typed model is the only one offered, under the name the provider's list gives it.
	if len(pr.Models) != 1 || pr.Models[0].ID != "corp-large" || pr.Models[0].Label != "Large" || pr.Small != "corp-large" {
		t.Fatalf("models: %+v", pr.Models)
	}
	free := p
	free.Env = nil
	if fr, _ := extProfile(free); len(fr.Models) != 2 || fr.Models[1].Label != "corp-small" {
		t.Fatalf("models without a typed one: %+v", fr.Models)
	}
	free.Env = []extproviders.EnvVar{{Name: "ANTHROPIC_MODEL", Value: "corp-other"}}
	if fr, _ := extProfile(free); len(fr.Models) != 1 || fr.Models[0].Label != "corp-other" {
		t.Fatalf("a typed model the list does not have: %+v", fr.Models)
	}
	if pr.Env["ANTHROPIC_MODEL"] != "corp-large" || pr.SecretEnv["ANTHROPIC_AUTH_TOKEN"] != "provider-corp-env-anthropic-auth-token" || len(pr.Env) != 1 {
		t.Fatalf("env: %v, secret %v", pr.Env, pr.SecretEnv)
	}

	// An OpenAI-compatible provider runs through the runtime's proxy: its models by their aliases, no key of its own.
	p.API = extproviders.APIOpenAI
	op, ok := extProfile(p)
	if !ok || op.Proxied != "corp" || op.KeyRef != "" || len(op.Models) != 1 || op.Models[0].ID != "corp--corp-large" || op.Env["ANTHROPIC_MODEL"] != "corp--corp-large" ||
		len(op.SecretEnv) != 0 {
		t.Fatalf("an OpenAI-compatible provider: %+v", op)
	}
	long := "a-provider-with-a-very-long-id-0123"
	if id := extProfileID(long); !wire.ValidProfileID(id) || id == extProfileID(long+"x") {
		t.Fatalf("the id of a long provider: %q", id)
	}
}

// A provider that came with the sign-in of a Claude subscription in a variable in plain sight (an item of the hub's
// environment, a file an agent wrote) is no profile: the machine has one such sign-in, the CLI's own. A key of the
// API in the same place is a profile like any other.
func TestAProviderWithASubscriptionsTokenIsNoProfile(t *testing.T) {
	p := extproviders.Provider{ID: "second", Name: "Second", BaseURL: "https://proxy.example/anthropic", API: extproviders.APIAnthropic,
		Models: []extproviders.Model{{ID: "m"}},
		Env:    []extproviders.EnvVar{{Name: "MY_TOKEN", Value: "sk-ant-" + "oat01-not-a-token-of-anyone"}}}
	if _, ok := extProfile(p); ok {
		t.Fatal("a provider with a subscription's token is a profile")
	}
	p.Env = []extproviders.EnvVar{{Name: "MY_TOKEN", Value: "sk-ant-" + "api03-not-a-key-of-anyone"}}
	if _, ok := extProfile(p); !ok {
		t.Fatal("a provider with a key of the API is no profile")
	}
}

// A model's efforts, typed by the person, are what the picker offers for it; the proxy knows each model of an
// OpenAI-compatible provider by its alias.
func TestExtProfileModelsCarryTheirEfforts(t *testing.T) {
	p := extproviders.Provider{ID: "ds", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", API: extproviders.APIOpenAI,
		Models: []extproviders.Model{{ID: "deepseek-reasoner", Label: "R1", Efforts: []string{"low", "high"}, DefaultEffort: "high", Window: 131072}, {ID: "deepseek-chat"}}}
	pr, ok := extProfile(p)
	if !ok || len(pr.Models) != 2 {
		t.Fatalf("profile: %+v", pr)
	}
	if pr.Models[0].Window != 131072 || pr.ClaudeEnv(pr.Models[0])["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "131072" {
		t.Fatalf("the window does not reach Claude Code: %+v", pr.Models[0])
	}
	if _, set := pr.ClaudeEnv(pr.Models[1])["CLAUDE_CODE_MAX_CONTEXT_TOKENS"]; set {
		t.Fatal("an unknown window is set")
	}
	m := pr.AgentModels()[0]
	if m.ID != "ds--deepseek-reasoner" || m.Label != "R1" || len(m.Efforts) != 2 || m.Efforts[1] != wire.EffortHigh || m.DefaultEffort == nil || *m.DefaultEffort != wire.EffortHigh {
		t.Fatalf("model: %+v", m)
	}
	if n := pr.AgentModels()[1]; len(n.Efforts) != 0 || n.DefaultEffort != nil {
		t.Fatalf("a model without efforts: %+v", n)
	}
	up := extUpstreams([]extproviders.Provider{p, {ID: "an", API: extproviders.APIAnthropic, Models: []extproviders.Model{{ID: "x"}}}})
	if len(up) != 1 || up[0].ID != "ds" || up[0].Models[0].Name != "deepseek-reasoner" || up[0].Models[0].Alias != "ds--deepseek-reasoner" || len(up[0].Models[0].Efforts) != 2 {
		t.Fatalf("upstreams: %+v", up)
	}
}

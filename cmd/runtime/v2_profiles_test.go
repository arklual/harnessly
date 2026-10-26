package main

import (
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/providers"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A machine lists under each CLI the profiles that run through it, and says again when a key is saved or removed.
func TestMachineOffersProfiles(t *testing.T) {
	store := &providers.Store{Dir: filepath.Join(t.TempDir(), "providers")}
	m := newLiveMachine("m", "v", nil, t.TempDir(), nil, nil, nil)
	m.offers = profileOffers(store)
	m.machine.Agents = m.withOffers([]wire.MachineAgent{{Provider: wire.ProviderClaude}, {Provider: wire.ProviderCodex}})
	var heard []wire.MachineStatusEv
	m.subs[1] = func(e wire.Event) { heard = append(heard, e.(wire.MachineStatusEv)) }
	ids := func(a wire.MachineAgent) (out []string) {
		for _, p := range a.Profiles {
			out = append(out, p.ID)
		}
		return
	}
	if got := ids(m.machine.Agents[1]); len(got) != 1 || got[0] != "xai" || m.machine.Agents[1].Profiles[0].Configured {
		t.Fatalf("codex: %v", got)
	}
	if len(ids(m.machine.Agents[0])) < 2 {
		t.Fatalf("claude: %v", ids(m.machine.Agents[0]))
	}
	src := &providers.Source{Store: store, OnChange: m.republishProfiles}
	if _, err := src.SetProfileKey(t.Context(), "xai", "sk-test-0000-abcdef123456"); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 1 || !heard[0].Agents[1].Profiles[0].Configured {
		t.Fatalf("%+v", heard)
	}
	if _, err := src.RemoveProfileKey(t.Context(), "xai"); err != nil || len(heard) != 2 || heard[1].Agents[1].Profiles[0].Configured {
		t.Fatalf("%v %+v", err, heard)
	}
	for _, a := range m.machine.Agents {
		for _, p := range a.Profiles {
			if err := p.Validate(); err != nil {
				t.Errorf("%s: %v", p.ID, err)
			}
		}
	}
}

// A conversation on a profile whose key was removed is not resumed to fail on every request.
func TestResumeNeedsTheProfilesKey(t *testing.T) {
	store := &providers.Store{Dir: filepath.Join(t.TempDir(), "providers")}
	conv := wire.Conversation{Model: &wire.Model{ID: "kimi-k3.1-pro", Profile: "kimi"}}
	if err := profileKeyGone(store, conv); err == nil {
		t.Fatal("no key, resumed")
	}
	if _, err := store.Set("kimi", "sk-test-0000-abcdef123456"); err != nil {
		t.Fatal(err)
	}
	if err := profileKeyGone(store, conv); err != nil {
		t.Fatal(err)
	}
	if err := profileKeyGone(store, wire.Conversation{Model: &wire.Model{ID: "opus"}}); err != nil {
		t.Fatal("a chat on the CLI's own account needs no key")
	}
}

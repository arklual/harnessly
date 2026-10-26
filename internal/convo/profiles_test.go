package convo

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/handoff"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type fakeProfiles struct {
	via        wire.Provider
	configured bool
}

func (f fakeProfiles) Lookup(id string) (wire.Provider, bool, []string, bool) {
	switch id {
	case "glm":
		return f.via, f.configured, []string{"glm-5", "glm-4.5-air"}, true
	case "kimi":
		return wire.ProviderClaude, f.configured, []string{"kimi-k3"}, true
	case "xai":
		return wire.ProviderCodex, f.configured, []string{"grok-build"}, true
	}
	return "", false, nil, false
}

func (fakeProfiles) Window(profile, model string) int {
	if profile == "glm" && model == "glm-5" {
		return 32_000
	}
	return 0
}

func (fakeProfiles) OwnsModel(m string) bool {
	return slices.Contains([]string{"glm-5", "glm-4.5-air", "kimi-k3", "grok-build"}, m)
}

func profileRig(t *testing.T, f fakeProfiles) *rig {
	r := newRig(t)
	r.rt.d.Profiles = f
	return r
}

func TestCreateOnAProfile(t *testing.T) {
	r := profileRig(t, fakeProfiles{via: wire.ProviderClaude, configured: true})
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude, Profile: "glm"})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("%+v", res)
	}
	if got := r.launcher.launched; len(got) != 1 || got[0].Model == nil || got[0].Model.ID != "glm-5" || got[0].Model.Profile != "glm" {
		t.Fatalf("%+v", got)
	}
	r.id = *res.ID
	if m := r.record().Model; m == nil || m.Profile != "glm" {
		t.Fatalf("the record keeps the profile: %+v", m)
	}
}

func TestCreateOnAProfileRefusals(t *testing.T) {
	high := wire.EffortHigh
	for name, c := range map[string]struct {
		f    fakeProfiles
		cmd  wire.ConversationCreateCmd
		code wire.RefuseCode
	}{
		"unknown profile":                   {fakeProfiles{wire.ProviderClaude, true}, wire.ConversationCreateCmd{Profile: "nope"}, wire.RefuseNotFound},
		"no key":                            {fakeProfiles{wire.ProviderClaude, false}, wire.ConversationCreateCmd{Profile: "glm"}, wire.RefuseUnsupported},
		"other cli":                         {fakeProfiles{wire.ProviderCodex, true}, wire.ConversationCreateCmd{Profile: "glm"}, wire.RefuseUnsupported},
		"foreign model":                     {fakeProfiles{wire.ProviderClaude, true}, wire.ConversationCreateCmd{Profile: "glm", Model: &wire.Model{ID: "opus"}}, wire.RefuseOptionUnknown},
		"effort":                            {fakeProfiles{wire.ProviderClaude, true}, wire.ConversationCreateCmd{Profile: "glm", Model: &wire.Model{ID: "glm-5", Effort: &high}}, wire.RefuseOptionUnknown},
		"vendor model on the cli's account": {fakeProfiles{wire.ProviderClaude, true}, wire.ConversationCreateCmd{Model: &wire.Model{ID: "glm-5"}}, wire.RefuseOptionUnknown},
	} {
		r := profileRig(t, c.f)
		c.cmd.OpID, c.cmd.Cwd = "c1", "/w"
		if c.cmd.Provider == "" {
			c.cmd.Provider = wire.ProviderClaude
		}
		if res := r.do(c.cmd); res.R != wire.ResultRefused || res.Code != c.code || len(r.launcher.launched) != 0 {
			t.Errorf("%s: %+v", name, res)
		}
	}
	r := newRig(t)
	if res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude, Profile: "glm"}); res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported {
		t.Fatalf("a machine without profiles: %+v", res)
	}
}

func TestAProfileChatSwitchesOnlyAmongItsModels(t *testing.T) {
	r := profileRig(t, fakeProfiles{via: wire.ProviderClaude, configured: true})
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude, Profile: "glm"})
	if res.ID == nil {
		t.Fatalf("%+v", res)
	}
	r.id = *res.ID
	if got := r.do(wire.ModelCmd{OpID: "m1", ID: r.id, Model: "opus"}); got.R != wire.ResultRefused || got.Code != wire.RefuseOptionUnknown {
		t.Errorf("a profile chat to Claude's own model: %+v", got)
	}
	high := wire.EffortHigh
	if got := r.do(wire.ModelCmd{OpID: "m2", ID: r.id, Model: "glm-4.5-air", Effort: &high}); got.R != wire.ResultRefused || got.Code != wire.RefuseOptionUnknown {
		t.Errorf("an effort on a profile chat: %+v", got)
	}
}

func TestAPlainChatDoesNotSwitchToAVendorModel(t *testing.T) {
	r := profileRig(t, fakeProfiles{via: wire.ProviderClaude, configured: true})
	r.open()
	r.settle(wire.StateReady)
	if got := r.do(wire.ModelCmd{OpID: "m1", ID: r.id, Model: "glm-5"}); got.R != wire.ResultRefused || got.Code != wire.RefuseOptionUnknown {
		t.Errorf("%+v", got)
	}
}

// A chat moves to any other: to a profile of the same CLI, to another profile, back to the CLI's own account, to the
// other CLI and to a profile of that. The new chat names its profile, or none; it never takes the source's.
func TestHandoffAmongProfiles(t *testing.T) {
	steps := []struct {
		name string
		cmd  wire.ConversationHandoffCmd
		want string // profile of the new chat
		code wire.RefuseCode
	}{
		{"claude to kimi", wire.ConversationHandoffCmd{Provider: wire.ProviderClaude, Profile: "kimi"}, "kimi", ""},
		{"claude to glm", wire.ConversationHandoffCmd{Provider: wire.ProviderClaude, Profile: "glm", Model: &wire.Model{ID: "glm-4.5-air"}}, "glm", ""},
		{"codex to grok", wire.ConversationHandoffCmd{Provider: wire.ProviderCodex, Profile: "xai"}, "xai", ""},
		{"claude to the CLI's own account", wire.ConversationHandoffCmd{Provider: wire.ProviderClaude, Model: &wire.Model{ID: "opus"}}, "", ""},
		{"to codex", wire.ConversationHandoffCmd{Provider: wire.ProviderCodex, Model: &wire.Model{ID: "gpt-5.4"}}, "", ""},
		{"a profile of the other CLI", wire.ConversationHandoffCmd{Provider: wire.ProviderClaude, Profile: "xai"}, "", wire.RefuseUnsupported},
		{"a vendor model without its profile", wire.ConversationHandoffCmd{Provider: wire.ProviderClaude, Model: &wire.Model{ID: "kimi-k3"}}, "", wire.RefuseOptionUnknown},
		{"an unknown profile", wire.ConversationHandoffCmd{Provider: wire.ProviderClaude, Profile: "gemini"}, "", wire.RefuseNotFound},
	}
	for _, from := range []struct {
		name  string
		model *wire.Model
	}{{"the CLI's own account", nil}, {"kimi", &wire.Model{ID: "kimi-k3", Profile: "kimi"}}} {
		for _, c := range steps {
			r := profileRig(t, fakeProfiles{via: wire.ProviderClaude, configured: true})
			r.twoTurns()
			if from.model != nil {
				a := r.rt.actor(r.id)
				if err := r.rt.on(a, func(cv *conv, _ time.Time) { cv.rec.Model = from.model; cv.touch(); a.publish() }); err != nil {
					t.Fatal(err)
				}
			}
			c.cmd.OpID, c.cmd.ID, c.cmd.Mode, c.cmd.Text = "h1", r.id, wire.HandoffExcerpt, "context"
			res := r.do(c.cmd)
			name := from.name + " -> " + c.name
			if c.code != "" {
				if res.R != wire.ResultRefused || res.Code != c.code || len(r.launcher.launched) != 0 {
					t.Errorf("%s: %+v", name, res)
				}
				continue
			}
			if res.R != wire.ResultAccepted || res.ID == nil {
				t.Errorf("%s: %+v", name, res)
				continue
			}
			got := r.launcher.launched[len(r.launcher.launched)-1]
			if got.Provider != c.cmd.Provider || (got.Model != nil && got.Model.Profile != c.want) || (got.Model == nil && c.want != "") {
				t.Errorf("%s: launched %+v", name, got)
			}
			rec := r.recordOf(*res.ID)
			o := rec.Origin
			if o == nil || o.K != wire.OriginHandoff || o.FromProvider != wire.ProviderClaude || profileOf(o.FromModel) != profileOf(from.model) ||
				profileOf(rec.Model) != c.want {
				t.Errorf("%s: origin %+v model %+v", name, o, rec.Model)
			}
		}
	}
}

// A handoff to a profile whose key was removed is refused, not launched to fail.
func TestHandoffToAProfileWithoutAKey(t *testing.T) {
	r := profileRig(t, fakeProfiles{via: wire.ProviderClaude, configured: false})
	r.twoTurns()
	res := r.do(wire.ConversationHandoffCmd{OpID: "h1", ID: r.id, Provider: wire.ProviderClaude, Profile: "glm", Mode: wire.HandoffExcerpt, Text: "x"})
	if res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported || len(r.launcher.launched) != 0 {
		t.Fatalf("%+v", res)
	}
}

// The excerpt is cut to the window of the target model: a profile's own, not a guess by provider.
func TestExcerptBudgetUsesTheProfilesWindow(t *testing.T) {
	r := profileRig(t, fakeProfiles{via: wire.ProviderClaude, configured: true})
	if got := r.rt.budgetFor(wire.ProviderClaude, "glm-5", "glm"); got != 8_000 {
		t.Errorf("glm-5: %d", got)
	}
	if got := r.rt.budgetFor(wire.ProviderClaude, "glm-4.5-air", "glm"); got != handoff.DefaultBudget {
		t.Errorf("a model the profile has no window for falls back to the provider's: %d", got)
	}
	if got := r.rt.budgetFor(wire.ProviderClaude, "opus", ""); got != handoff.DefaultBudget {
		t.Errorf("no profile: %d", got)
	}
	r.twoTurns()
	if _, err := r.rt.Excerpt(r.id, wire.ProviderClaude, "glm-5", "glm"); err != nil {
		t.Fatal(err)
	}
}

// A fork keeps the profile of the chat it branches off.
func TestForkKeepsTheProfile(t *testing.T) {
	r := profileRig(t, fakeProfiles{via: wire.ProviderClaude, configured: true})
	u1, _ := r.twoTurns()
	if err := r.rt.call(context.Background(), r.id, func(cv *conv, _ time.Time) { cv.rec.Model = &wire.Model{ID: "glm-5", Profile: "glm"}; cv.touch() }); err != nil {
		t.Fatal(err)
	}
	first := wire.ClaudeEntryID(u1, 0)
	res := r.do(wire.ConversationForkCmd{OpID: "f1", ID: r.id, AtTurn: &first})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("%+v", res)
	}
	got := r.launcher.launched[len(r.launcher.launched)-1]
	if got.Model == nil || got.Model.Profile != "glm" || profileOf(r.recordOf(*res.ID).Model) != "glm" {
		t.Fatalf("launched %+v", got)
	}
}

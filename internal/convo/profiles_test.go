package convo

import (
	"slices"
	"testing"

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
	}
	return "", false, nil, false
}

func (fakeProfiles) OwnsModel(m string) bool {
	return slices.Contains([]string{"glm-5", "glm-4.5-air"}, m)
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

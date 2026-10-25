package wire_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeProfiles keeps keys in memory, as the runtime keeps them in files: it answers a tail, never the key.
type fakeProfiles struct {
	mu   sync.Mutex
	keys map[string]string
}

func (f *fakeProfiles) key(id string) (wire.ProfileKey, error) {
	if id != "kimi" && id != "xai" {
		return wire.ProfileKey{}, wire.ErrProfileUnknown
	}
	k, ok := f.keys[id]
	if !ok {
		return wire.ProfileKey{}, nil
	}
	tail := "…" + k[len(k)-4:]
	return wire.ProfileKey{Configured: true, Tail: &tail}, nil
}

func (f *fakeProfiles) Profiles(context.Context) (wire.ProfileList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []wire.ProfileInfo
	for id, via := range map[string]wire.Provider{"kimi": wire.ProviderClaude, "xai": wire.ProviderCodex} {
		k, _ := f.key(id)
		vendor := wire.VendorKimi
		if id == "xai" {
			vendor = wire.VendorGrok
		}
		out = append(out, wire.ProfileInfo{ID: id, Label: strings.ToUpper(id), Vendor: vendor, Via: via, Host: "api.example", Key: k,
			Models: []wire.AgentModel{{ID: id + "-m1", Label: "M1", Efforts: []wire.Effort{}}}})
	}
	return wire.ProfileList{Profiles: out}, nil
}

func (f *fakeProfiles) SetProfileKey(_ context.Context, id, key string) (wire.ProfileKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.key(id); err != nil {
		return wire.ProfileKey{}, err
	}
	f.keys[id] = key
	return f.key(id)
}

func (f *fakeProfiles) RemoveProfileKey(_ context.Context, id string) (wire.ProfileKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.key(id); err != nil {
		return wire.ProfileKey{}, err
	}
	delete(f.keys, id)
	return f.key(id)
}

func (f *fakeProfiles) TestProfile(_ context.Context, id string) (wire.ProfileTest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, err := f.key(id)
	if err != nil {
		return wire.ProfileTest{}, err
	}
	if !k.Configured {
		r := wire.TestNoKey
		return wire.ProfileTest{Reason: &r}, nil
	}
	st := 200
	return wire.ProfileTest{OK: true, Status: &st, Ms: 12}, nil
}

// The profile pages list the presets, save and remove a key and test the connection; nothing they answer holds the key.
func TestProfilePages(t *testing.T) {
	g := newRig(t, time.Hour)
	if code := g.do("GET", "/api/v2/providers", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a machine without profiles answers %d", code)
	}
	g.srv.Profiles = &fakeProfiles{keys: map[string]string{}}

	var list wire.ProfileList
	if code := g.do("GET", "/api/v2/providers", nil, &list); code != 200 || list.Validate() != nil || len(list.Profiles) != 2 {
		t.Fatalf("list: %d %v %+v", code, list.Validate(), list)
	}
	const secret = "sk-" + "test-0000-abcdef123456" // joined: no provider-shaped key literal in the tree
	var saved wire.ProfileKey
	raw := new(strings.Builder)
	_ = json.NewEncoder(raw).Encode(wire.ProfileKeyEdit{Key: secret})
	if code := g.do("PUT", "/api/v2/providers/kimi/key", []byte(raw.String()), &saved); code != 200 || saved.Validate() != nil || !saved.Configured || *saved.Tail != "…3456" {
		t.Fatalf("save: %d %+v", code, saved)
	}
	var again wire.ProfileList
	g.do("GET", "/api/v2/providers", nil, &again)
	out, _ := json.Marshal(again)
	if strings.Contains(string(out), secret) {
		t.Fatal("the list holds the key")
	}
	var test wire.ProfileTest
	if code := g.do("POST", "/api/v2/providers/kimi/test", nil, &test); code != 200 || test.Validate() != nil || !test.OK {
		t.Fatalf("test: %d %+v", code, test)
	}
	var removed wire.ProfileKey
	if code := g.do("DELETE", "/api/v2/providers/kimi/key", nil, &removed); code != 200 || removed.Configured || removed.Tail != nil {
		t.Fatalf("remove: %d %+v", code, removed)
	}
	if code := g.do("POST", "/api/v2/providers/kimi/test", nil, &test); code != 200 || test.Validate() != nil || test.OK || *test.Reason != wire.TestNoKey {
		t.Fatalf("test without a key: %d %+v", code, test)
	}

	for name, c := range map[string]struct {
		method, path string
		body         any
		want         int
	}{
		"unknown profile":    {"PUT", "/api/v2/providers/gemini/key", wire.ProfileKeyEdit{Key: secret}, http.StatusNotFound},
		"unknown page":       {"GET", "/api/v2/providers/kimi/other", nil, http.StatusNotFound},
		"bad id":             {"PUT", "/api/v2/providers/Kimi%20x/key", wire.ProfileKeyEdit{Key: secret}, http.StatusNotFound},
		"short key":          {"PUT", "/api/v2/providers/kimi/key", wire.ProfileKeyEdit{Key: "short"}, http.StatusBadRequest},
		"key with a space":   {"PUT", "/api/v2/providers/kimi/key", wire.ProfileKeyEdit{Key: "sk-test 0000-abcdef"}, http.StatusBadRequest},
		"key with a newline": {"PUT", "/api/v2/providers/kimi/key", wire.ProfileKeyEdit{Key: "sk-test-0000\nabcdef"}, http.StatusBadRequest},
		"unknown field":      {"PUT", "/api/v2/providers/kimi/key", map[string]string{"key": secret, "url": "https://evil.example"}, http.StatusBadRequest},
		"GET a key":          {"GET", "/api/v2/providers/kimi/key", nil, http.StatusMethodNotAllowed},
		"GET a test":         {"GET", "/api/v2/providers/kimi/test", nil, http.StatusMethodNotAllowed},
		"POST the list":      {"POST", "/api/v2/providers", nil, http.StatusMethodNotAllowed},
	} {
		if code := g.do(c.method, c.path, c.body, nil); code != c.want {
			t.Errorf("%s: %d, want %d", name, code, c.want)
		}
	}
}

func TestProfileContractRefusesWhatItDoesNotHave(t *testing.T) {
	create := wire.ConversationCreateCmd{OpID: "a", Cwd: "/tmp", Provider: wire.ProviderClaude, Model: &wire.Model{ID: "kimi-k3.1-pro"}, Profile: "kimi"}
	if err := create.Validate(); err != nil {
		t.Fatal(err)
	}
	create.Profile = "Kimi"
	if create.Validate() == nil {
		t.Fatal("a profile id is lower case")
	}
	create.Profile = ""
	create.Model.Profile = "kimi"
	if create.Validate() == nil {
		t.Fatal("a command names the profile beside the model")
	}
	hand := wire.ConversationHandoffCmd{OpID: "a", ID: "tab-k3Jx9QmZ", Provider: wire.ProviderCodex, Mode: wire.HandoffNote, Text: "x", Profile: "x ai"}
	if hand.Validate() == nil {
		t.Fatal("a handoff takes a profile id")
	}
	tail := "…34"
	if (wire.ProfileKey{Configured: true, Tail: &tail}).Validate() == nil {
		t.Fatal("a tail is four characters")
	}
	if (wire.ProfileKey{Tail: &tail}).Validate() == nil {
		t.Fatal("no key, no tail")
	}
	reason := wire.TestUnauthorized
	if (wire.ProfileTest{OK: true, Reason: &reason}).Validate() == nil || (wire.ProfileTest{}).Validate() == nil {
		t.Fatal("a pass has no reason and a failure has one")
	}
}

// A page that does not read profiles gets the machine's agents and a conversation's model without them, seq for seq;
// one that names the cap gets them whole, live, from the ring and in a snapshot.
func TestProfilesGoOnlyToAPageThatReadsThem(t *testing.T) {
	g := newRig(t, time.Hour)
	old, now := g.dialWith("delegation,workflows"), g.dialWith("delegation,workflows,profiles")
	for _, s := range []*sock{old, now} {
		s.hello()
		s.send(wire.SubFrame{Ch: wire.ChannelMachine})
		s.snapshot(wire.ChannelMachine)
		s.send(wire.SubFrame{Ch: wire.ChannelConvs})
		s.snapshot(wire.ChannelConvs)
	}
	status := fixtureEvent(t, "ev.machine_status-profiles.json").(wire.MachineStatusEv)
	upsert := fixtureEvent(t, "ev.conversation_upsert-profile.json").(wire.ConversationUpsertEv)
	mseq := g.hub.Publish(wire.ChannelMachine, status)
	cseq := g.hub.Publish(wire.ChannelConvs, upsert)

	any := func(wire.Event) bool { return true }
	if got := old.until(wire.ChannelMachine, mseq-1, any)[0].E.(wire.MachineStatusEv); got.Agents[0].Profiles != nil || got.Agents[1].Profiles != nil || len(got.Agents[0].Models) == 0 {
		t.Fatalf("a page without the cap got %+v", got.Agents)
	}
	if got := now.until(wire.ChannelMachine, mseq-1, any)[0].E.(wire.MachineStatusEv); len(got.Agents[0].Profiles) != 2 || len(got.Agents[1].Profiles) != 1 {
		t.Fatalf("a page with the cap got %+v", got.Agents)
	}
	if got := old.until(wire.ChannelConvs, cseq-1, any)[0].E.(wire.ConversationUpsertEv).Conv; got.Model == nil || got.Model.Profile != "" || got.Model.ID != "kimi-k3.1-pro" {
		t.Fatalf("a page without the cap got %+v", got.Model)
	}
	if got := now.until(wire.ChannelConvs, cseq-1, any)[0].E.(wire.ConversationUpsertEv).Conv; got.Model.Profile != "kimi" {
		t.Fatalf("a page with the cap got %+v", got.Model)
	}
	if status.Agents[0].Profiles == nil || upsert.Conv.Model.Profile != "kimi" {
		t.Fatal("the event itself changed")
	}

	// Back from the ring, each page gets the event as it went to it.
	epoch, after := g.hub.Epoch(), cseq-1
	for _, s := range []*sock{old, now} {
		s.send(wire.SubFrame{Ch: wire.ChannelConvs, Epoch: epoch, AfterSeq: &after})
	}
	if got := old.until(wire.ChannelConvs, after, any)[0].E.(wire.ConversationUpsertEv).Conv; got.Model.Profile != "" {
		t.Fatal("the ring replays the profile to a page without the cap")
	}
	if got := now.until(wire.ChannelConvs, after, any)[0].E.(wire.ConversationUpsertEv).Conv; got.Model.Profile != "kimi" {
		t.Fatal("the ring lost the profile for a page with the cap")
	}
}

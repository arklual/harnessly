package hubproto

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

const envGolden = "testdata/env.json"

func envItems() []EnvItem {
	return []EnvItem{
		{ItemID: "i2mcptrackeraaaaaaaa", Kind: EnvKindMCP, Name: "Tracker", Layer: "tenant", Source: json.RawMessage(`{"url":"https://tracker.example.org/mcp","secretRefs":["TRACKER_TOKEN"]}`)},
		{ItemID: "i1skillreviewaaaaaaa", Kind: EnvKindSkill, Name: "Review", Layer: "org", Pinned: true, Source: json.RawMessage(`{"content":"Check the diff."}`)},
	}
}

func envFrames() map[string]any {
	items := envItems()
	SortEnvItems(items)
	hash := EnvHash(items)
	return map[string]any{
		"request env.set": frame(OpEnvSet, 31, EnvSet{Rev: 5, Hash: hash, Items: items}),
		"result env.set": map[string]any{"id": 31, "ok": true, "result": EnvState{Rev: 5, Hash: hash, Applied: []EnvApplied{
			{ItemID: "i1skillreviewaaaaaaa", Status: EnvOK},
			{ItemID: "i2mcptrackeraaaaaaaa", Status: EnvPending, Message: "kept; this runtime does not load MCP servers from the hub yet"},
		}}},
		"request env.set empty": frame(OpEnvSet, 32, EnvSet{Rev: 6, Hash: EnvHash(nil), Items: []EnvItem{}}),
		"event env.state":       map[string]any{"event": EventEnvState, "state": EnvState{Rev: 5, Hash: hash, Applied: []EnvApplied{{ItemID: "i2mcptrackeraaaaaaaa", Status: EnvError, Message: "secret TRACKER_TOKEN is not set"}}}},
		"hello with environment": Hello{Protocol: Version, AgentVersion: "0.17.0", Features: []string{FeatureUsage, FeatureEnvironment},
			MachineID: "h2runneraaaaaaaaaaaa", EnvRev: 5, EnvHash: hash},
		"usage tool": UsageRecord{Seq: 9, Kind: UsageTool, At: 1_793_455_200_000, Tool: &UsageToolFact{Provider: "claude", SessionKey: "s1",
			Day: "2026-10-30", ToolKind: "mcp", ToolName: "tracker", Calls: 7, Errors: 1}},
	}
}

// TestEnvGolden: the Go types make exactly testdata/env.json; HUBPROTO_UPDATE=1 rewrites it.
func TestEnvGolden(t *testing.T) {
	got, err := json.MarshalIndent(envFrames(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("HUBPROTO_UPDATE") == "1" {
		if err := os.WriteFile(envGolden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(envGolden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the environment envelopes changed; HUBPROTO_UPDATE=1 go test ./internal/hubproto rewrites %s\n%s", envGolden, got)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(want, &back); err != nil {
		t.Fatal(err)
	}
	var set EnvSet
	if err := json.Unmarshal(back["request env.set"], &set); err != nil {
		t.Fatal(err)
	}
	if set.Rev != 5 || len(set.Items) != 2 || set.Items[0].Kind != EnvKindMCP || !set.Items[1].Pinned || EnvHash(set.Items) != set.Hash {
		t.Errorf("read back %+v", set)
	}
	// An agent without the feature sends no envRev at all: the old hello stays byte for byte.
	raw, _ := json.Marshal(Hello{Protocol: Version})
	if bytes.Contains(raw, []byte("envRev")) || bytes.Contains(raw, []byte("envHash")) {
		t.Errorf("%s", raw)
	}
}

func TestEnvHash(t *testing.T) {
	a := envItems()
	b := []EnvItem{a[1], a[0]}
	b[1].Source = json.RawMessage(`{ "secretRefs": ["TRACKER_TOKEN"], "url": "https://tracker.example.org/mcp" }`)
	if EnvHash(a) != EnvHash(b) {
		t.Error("the order of items and of keys must not change the hash")
	}
	c := envItems()
	c[0].Pinned = true
	if EnvHash(a) == EnvHash(c) {
		t.Error("a pin changes the list")
	}
	if EnvHash(nil) != EnvHash([]EnvItem{}) {
		t.Error("an empty list has one hash")
	}
	if string(CanonicalJSON(nil)) != "{}" || string(CanonicalJSON(json.RawMessage("null"))) != "{}" {
		t.Error("no source is an empty object")
	}
}

// The organization's rule for apps takes part in the hash only when it is auto (HRN-915): a list with the rule ask holds
// the hash an older agent computes, and a change of the rule changes the hash the way a change of the list does.
func TestEnvSetHashCarriesTheAppsRule(t *testing.T) {
	items := EnvHash(envItems())
	if EnvSetHash(items, "") != items || EnvSetHash(items, AppsConsentAsk) != items {
		t.Fatal("the rule ask must leave the items' hash as it is")
	}
	auto := EnvSetHash(items, AppsConsentAuto)
	if auto == items || len(auto) != 64 {
		t.Fatalf("the rule auto must change the hash: %q", auto)
	}
	if EnvSetHash(items, AppsConsentAuto) != auto {
		t.Fatal("the hash must be stable")
	}
	if EnvSetHash(EnvHash(nil), AppsConsentAuto) == auto {
		t.Fatal("another list with the same rule must hash otherwise")
	}
}

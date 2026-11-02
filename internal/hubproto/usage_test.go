package hubproto

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// usageFrames are the envelopes of docs/hub/usage.md, by name: what testdata/usage.json holds.
func usageFrames() map[string]any {
	ended := int64(1_761_900_000_000)
	return map[string]any{
		"event usage.available": map[string]any{"event": EventUsageAvailable, "epoch": "e7k2", "seq": 42},
		"request usage.read":    frame(OpUsageRead, 7, UsageReadParams{Epoch: "e7k2", After: 40, Max: 500}),
		"result usage.read": map[string]any{"id": 7, "ok": true, "result": UsageReadResult{Epoch: "e7k2", Last: 44, More: false, Records: []UsageRecord{
			{Seq: 41, Kind: UsageCall, At: 1_761_899_000_100, Call: &UsageCallFact{
				Provider: ProviderClaude, MsgKey: "msg_01AbC", SessionKey: "3f1c2a9e-0000-4000-8000-000000000001", Ts: 1_761_899_000_000,
				Model: "claude-sonnet-4-6", BaseURLHost: "api.anthropic.com", Input: 12, Output: 830, CacheRead: 41_200, CacheWrite5m: 1_900, CacheWrite1h: 0,
			}},
			{Seq: 42, Kind: UsageCall, At: 1_761_899_100_000, Call: &UsageCallFact{
				Provider: ProviderClaude, MsgKey: "msg_01AbD", SessionKey: "3f1c2a9e-0000-4000-8000-000000000002", Ts: 1_761_899_099_000,
				Model: "kimi-k2-turbo", BaseURLHost: "api.kimi.com", Input: 3, Output: 120, CacheRead: 9_000,
			}},
			{Seq: 43, Kind: UsageSession, At: 1_761_900_000_100, Session: &UsageSessionFact{
				Provider: ProviderClaude, SessionKey: "3f1c2a9e-0000-4000-8000-000000000002", ParentKey: "3f1c2a9e-0000-4000-8000-000000000001",
				StartedAt: 1_761_899_000_000, EndedAt: &ended, Origin: OriginLive, Subject: "anna@example.com",
				AccountRef: "claude:7d0c6f0e-0000-4000-8000-00000000000a", AuthMode: "subscription", BaseURLHost: "api.anthropic.com",
				Attribution: AttributionExact, Metrics: map[string]float64{"turns": 14, "waitMs": 61_000},
			}},
			{Seq: 44, Kind: UsageWindow, At: 1_761_900_000_200, Window: &UsageWindowFact{
				Provider: ProviderClaude, AccountRef: "claude:7d0c6f0e-0000-4000-8000-00000000000a", Name: "five_hour", Scope: "all",
				LengthSec: 18_000, Unit: "pct", UsedPct: ptr(37.5), ResetsAt: ptr(int64(1_761_910_000_000)),
				MeasuredAt: 1_761_900_000_000, Source: "oauth-usage", Confidence: "undocumented",
			}},
		}}},
		"result usage.read balance": map[string]any{"id": 8, "ok": true, "result": UsageReadResult{Epoch: "e7k2", Last: 45, Records: []UsageRecord{
			{Seq: 45, Kind: UsageBalance, At: 1_761_900_100_000, Balance: &UsageBalanceFact{
				Provider: "openrouter", AccountRef: "key:sha256:9f86d081884c7d65", Currency: "USD", Available: ptr(41.2),
				SpendCap: ptr(100.0), MeasuredAt: 1_761_900_100_000, Source: "openrouter-key",
			}},
		}}},
		"result usage.read empty": map[string]any{"id": 9, "ok": true, "result": UsageReadResult{Epoch: "e7k2", Last: 45, Records: []UsageRecord{}}},
		"request usage.ack":       frame(OpUsageAck, 10, UsageAckParams{Epoch: "e7k2", Seq: 45}),
		"result usage.ack":        map[string]any{"id": 10, "ok": true, "result": map[string]any{}},
	}
}

// frame is a request: op, id and the params flattened beside them, as the link sends it.
func frame(op string, id int, params any) map[string]any {
	raw, _ := json.Marshal(params)
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	m["op"], m["id"] = op, id
	return m
}

const usageGolden = "testdata/usage.json"

// TestUsageGolden: the Go types make exactly the envelopes of testdata/usage.json, and read them back unchanged.
// HUBPROTO_UPDATE=1 rewrites the file (then read the diff: it is the contract).
func TestUsageGolden(t *testing.T) {
	frames := usageFrames()
	got, err := json.MarshalIndent(frames, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("HUBPROTO_UPDATE") == "1" {
		if err := os.WriteFile(usageGolden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(usageGolden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the usage envelopes changed; HUBPROTO_UPDATE=1 go test ./internal/hubproto rewrites %s\n%s", usageGolden, got)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(want, &back); err != nil {
		t.Fatal(err)
	}
	var res struct {
		Result UsageReadResult `json:"result"`
	}
	if err := json.Unmarshal(back["result usage.read"], &res); err != nil {
		t.Fatal(err)
	}
	orig := frames["result usage.read"].(map[string]any)["result"].(UsageReadResult)
	if !reflect.DeepEqual(res.Result, orig) {
		t.Errorf("read back differs:\n%+v\n%+v", res.Result, orig)
	}
	var params UsageReadParams
	if err := json.Unmarshal(back["request usage.read"], &params); err != nil || params != (UsageReadParams{Epoch: "e7k2", After: 40, Max: 500}) {
		t.Errorf("params %+v %v", params, err)
	}
}

// TestUsageFactsCarryNoText guards the rule that no fact carries conversation text: the fields of the facts are exactly
// these, and adding one is a decision of the contract (usage.md), not of the code.
func TestUsageFactsCarryNoText(t *testing.T) {
	want := map[string]string{
		"UsageCallFact":    "baseUrlHost cacheRead cacheWrite1h cacheWrite5m input model msgKey output provider sessionKey ts",
		"UsageSessionFact": "accountRef attribution authMode baseUrlHost endedAt metrics origin parentKey provider sessionKey startedAt subject",
		"UsageWindowFact":  "accountRef confidence lengthSec limit measuredAt name provider resetsAt scope source unit used usedPct",
		"UsageBalanceFact": "accountRef available currency measuredAt provider source spendCap spentInPeriod",
	}
	for _, v := range []any{UsageCallFact{}, UsageSessionFact{}, UsageWindowFact{}, UsageBalanceFact{}} {
		tp := reflect.TypeOf(v)
		var names []string
		for i := 0; i < tp.NumField(); i++ {
			name, _, _ := strings.Cut(tp.Field(i).Tag.Get("json"), ",")
			names = append(names, name)
			if tp.Field(i).Type.Kind() == reflect.String {
				switch strings.ToLower(name) {
				case "text", "prompt", "content", "title", "message", "summary", "cwd", "path":
					t.Errorf("%s.%s is text of a conversation", tp.Name(), name)
				}
			}
		}
		sort.Strings(names)
		if got := strings.Join(names, " "); got != want[tp.Name()] {
			t.Errorf("%s fields: %s\nwant: %s", tp.Name(), got, want[tp.Name()])
		}
	}
}

package usagefeed

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/usagereport"
)

func TestSessionMetrics(t *testing.T) {
	fa := &usagereport.Facts{
		Prompts: []usagereport.PromptRow{{Ts: 100}, {Ts: 200}, {Ts: 100000}},
		Turns:   []usagereport.TurnRow{{Ts: 150, Ms: 40000}, {Ts: 260, Ms: 50000}},
		Tools:   []usagereport.ToolRow{{Error: 0}, {Error: 1}, {Error: 1}, {Error: 0}},
		// One of the two errors is the person's refusal.
		Denials: []usagereport.EventRow{{Ts: 120}},
		Messages: []usagereport.MsgRow{
			{Ts: 101, Input: 1000, W5m: 9000},
			{Ts: 102, Input: 10, CacheRead: 10000, W5m: 50},
			{Ts: 300, Input: 20, W1h: 12000},
		},
		PRs: []string{"a!1", "a!1", "b!2"},
	}
	var m metrics
	m.add(fa)
	got := m.values(false)
	want := map[string]float64{
		"turns": 3, "agentMs": 90000, "activeIntervals": 2,
		// 150 → 200 waits 50 s; 260 → 100000 is the person away.
		"waits": 1, "waitMs": 50000,
		"toolCalls": 4, "toolErrors": 1,
		"inputTokens": 1030, "cacheReadTokens": 10000, "cacheWriteTokens": 21050,
		// Call 2 paid 60 again of the 10000 before it; call 3 paid 12020 of the 10060 before it.
		"carryTokens": 60 + 10060,
		"firstCtx":    10000,
		"mrs":         2,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if signature(got, nil) == signature(map[string]float64{}, nil) {
		t.Fatal("signature does not tell metrics apart")
	}
}

func TestSkillMetricsAreOptional(t *testing.T) {
	fa := &usagereport.Facts{
		SkillsListed: 12,
		ToolCalls: []usagereport.ToolCall{
			{Ts: 100, Kind: usagereport.ToolSkill, Name: "a"},
			{Ts: 101, Kind: usagereport.ToolSkill, Name: "a"},
			{Ts: 102, Kind: usagereport.ToolSkill, Name: "b"},
			{Ts: 103, Kind: usagereport.ToolMCP, Name: "srv"},
		},
	}
	var m metrics
	m.add(fa)
	if v := m.values(false); v["skillsListed"] != 0 || v["skillsLoaded"] != 0 {
		t.Fatalf("flag off must send no skill keys: %v", v)
	}
	if v := m.values(true); v["skillsListed"] != 12 || v["skillsLoaded"] != 2 {
		t.Fatalf("flag on: %v", v)
	}
	var none metrics
	none.add(&usagereport.Facts{ToolCalls: fa.ToolCalls})
	if v := none.values(true); len(v) != len(none.values(false)) {
		t.Fatalf("a session without a listing must send no skill keys: %v", v)
	}
}

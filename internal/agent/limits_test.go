package agent

import (
	"testing"
	"time"
)

func TestParseProviderWindows(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	raw := []byte(`{"machines":[{"id":"a","limits":{
		"claude":{"fiveHour":{"usedPct":12.5,"resetsAt":1700003600,"forecast":{}},"sevenDay":{"usedPct":140,"resetsAt":1700500000}},
		"codex":{"fiveHour":null,"sevenDay":{"usedPct":null,"resetsAt":5}},
		"claudeSpend":{"usd":3}}},
		{"id":"b","limits":{"claude":{"sevenDay":{"usedPct":30,"resetsAt":9}}}}]}`)
	w, ok := parseProviderWindows(raw, now)
	if !ok || w.At != now.UnixMilli() {
		t.Fatalf("windows %+v ok %v", w, ok)
	}
	c := w.Providers["claude"]
	if c.FiveHour == nil || c.FiveHour.UsedPct != 12.5 || c.FiveHour.ResetsAt != 1700003600 {
		t.Fatalf("five hour %+v", c.FiveHour)
	}
	// The fullest reading wins and a percentage is held to 0..100.
	if c.SevenDay == nil || c.SevenDay.UsedPct != 100 || c.SevenDay.ResetsAt != 1700500000 {
		t.Fatalf("seven day %+v", c.SevenDay)
	}
	if _, has := w.Providers["codex"]; has {
		t.Fatalf("codex has no numbers: %+v", w.Providers["codex"])
	}
	if _, ok := parseProviderWindows([]byte(`{"machines":[{"limits":{}}]}`), now); ok {
		t.Fatal("an empty answer is no reading")
	}
	if _, ok := parseProviderWindows([]byte(`nope`), now); ok {
		t.Fatal("not JSON")
	}
}

package analytics

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func TestWindowHistories(t *testing.T) {
	acc := []store.Account{{ID: "a1", Provider: "anthropic", Kind: "subscription", Label: "Max"}, {ID: "a2", Provider: "openai", Kind: "subscription", Label: "Idle"}}
	day := int64(24 * 3600 * 1000)
	cs := []store.WindowCycle{
		{AccountID: "a1", Scope: "all", Name: "five_hour", ResetsAt: day, LengthSec: 18000, PeakPct: 40, Measures: 5},
		{AccountID: "a1", Scope: "all", Name: "seven_day", ResetsAt: 2 * day, LengthSec: 604800, PeakPct: 97.44, Measures: 90},
		{AccountID: "a1", Scope: "all", Name: "seven_day", ResetsAt: 9 * day, LengthSec: 604800, PeakPct: 50, Measures: 80},
	}
	got := windowHistories(acc, cs)
	if len(got) != 1 || got[0].AccountID != "a1" || len(got[0].Windows) != 2 {
		t.Fatalf("accounts %+v", got)
	}
	week := got[0].Windows[0]
	// The week comes first; its two cycles, one run out.
	if week.Name != "seven_day" || len(week.Cycles) != 2 || week.FullCycles != 1 || week.AvgPeakPct != 73.7 || week.Cycles[0].PeakPct != 97.4 {
		t.Errorf("week %+v", week)
	}
	if got[0].Windows[1].Name != "five_hour" || got[0].Windows[1].FullCycles != 0 {
		t.Errorf("five_hour %+v", got[0].Windows[1])
	}
}

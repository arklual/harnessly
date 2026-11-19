package analytics

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func TestDailyLines(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	sp := newSpan(time.Date(2026, 11, 1, 0, 0, 0, 0, msk), time.Date(2026, 11, 4, 0, 0, 0, 0, msk), msk)
	at := func(d, h int) int64 { return time.Date(2026, 11, d, h, 0, 0, 0, msk).UnixMilli() }
	rows := []store.RollupRow{
		{Hour: at(1, 23), TeamID: "front", ListCostMicros: 2_000_000, Funding: store.FundingOrg},
		{Hour: at(2, 0), TeamID: "front", ListCostMicros: 1_000_000, Funding: store.FundingOrg},
		{Hour: at(2, 5), TeamID: "back", ListCostMicros: 5_000_000, Funding: store.FundingOrg},
		{Hour: at(3, 5), TeamID: "back", ListCostMicros: 0, Funding: store.FundingOrg},
		{Hour: at(3, 6), TeamID: "gone", ListCostMicros: 9_000_000, Funding: "personal"},
		{Hour: at(5, 6), TeamID: "back", ListCostMicros: 7_000_000, Funding: store.FundingOrg},
	}
	days, lines := dailyLines(rows, func(r store.RollupRow) bool { return r.Funding == store.FundingOrg }, sp, "team")
	if len(days) != 3 || days[0] != "2026-11-01" || days[2] != "2026-11-03" {
		t.Fatalf("days %v", days)
	}
	if len(lines) != 2 || lines[0].Key != "back" || lines[1].Key != "front" {
		t.Fatalf("lines %+v", lines)
	}
	// The hour after midnight in Moscow is the next day there; a day outside the period and a free hour are not counted.
	if got := lines[1].Values; got[0] != 2 || got[1] != 1 || got[2] != 0 {
		t.Errorf("front %v", got)
	}
	if got := lines[0].Values; got[0] != 0 || got[1] != 5 || got[2] != 0 {
		t.Errorf("back %v", got)
	}
}

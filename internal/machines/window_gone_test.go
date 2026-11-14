package machines

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/limits"
)

// Claude stopped giving a five-hour window (the usage answer has five_hour: null, the status line carries
// seven_day alone), so rate-limits.json kept the last five-hour copy of 13.11 and the snapshot rolled it over to «0 %
// used»: the chip said «5ч 100%». A window whose reset has passed and that the provider was measured again without is gone.
func TestWindowTheVendorDroppedIsGone(t *testing.T) {
	const now = 1794669600.0
	raw, err := jsvalue.Parse([]byte(`{"ts":1794669558.584,
		"five_hour":{"used_percentage":11,"resets_at":1794558600,"at":1794548516.684},
		"seven_day":{"used_percentage":10,"resets_at":1795258800,"at":1794669558.584}}`))
	if err != nil {
		t.Fatal(err)
	}
	none := func(string) []limits.Sample { return nil }
	lim := BuildProviderLimits(raw, none, now, nil)
	if lim.FiveHour != nil {
		t.Fatalf("five-hour window the vendor no longer gives is shown: %+v", *lim.FiveHour)
	}
	if lim.SevenDay == nil || float64(lim.SevenDay.UsedPct) != 10 {
		t.Fatalf("weekly window: %+v", lim.SevenDay)
	}
}

// A window not measured again since its reset still restarts from zero: the CLI was idle, the vendor still has it.
func TestIdleWindowStillRollsOver(t *testing.T) {
	const now = 1794669600.0
	raw, err := jsvalue.Parse([]byte(`{"ts":1794548516.684,
		"five_hour":{"used_percentage":11,"resets_at":1794558600,"at":1794548516.684}}`))
	if err != nil {
		t.Fatal(err)
	}
	lim := BuildProviderLimits(raw, func(string) []limits.Sample { return nil }, now, nil)
	if lim.FiveHour == nil || float64(lim.FiveHour.UsedPct) != 0 || float64(lim.FiveHour.ResetsAt) <= now {
		t.Fatalf("idle window: %+v", lim.FiveHour)
	}
}

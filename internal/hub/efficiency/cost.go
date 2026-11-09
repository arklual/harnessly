package efficiency

import (
	"context"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// RollupCost reads the organization's spend of [from, to) from usage_rollup_hour once and answers per day of loc.
// The money is the list price in US dollars (what the same calls cost on API prices): it needs no account and is the same
// whoever pays, so a subscription's fee in another currency never mixes in. A day without a single call is unknown, not
// zero, so the figure is never invented; calls the hub could not price yet add nothing to the sum.
func RollupCost(ctx context.Context, st store.Store, orgID string, from, to time.Time, loc *time.Location) (CostFn, error) {
	if loc == nil {
		loc = time.UTC
	}
	rows, err := st.RollupRows(ctx, orgID, store.HourMs(from.UnixMilli()), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	micros := map[string]int64{}
	for _, r := range rows {
		if r.Calls == 0 {
			continue
		}
		micros[time.UnixMilli(r.Hour).In(loc).Format("2006-01-02")] += r.ListCostMicros
	}
	return func(_ context.Context, day string, _ *time.Location) (float64, bool) {
		v, ok := micros[day]
		return float64(v) / 1e6, ok
	}, nil
}

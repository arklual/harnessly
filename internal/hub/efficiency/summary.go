package efficiency

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Day is one calendar day of the organization's time zone.
type Day struct {
	Day     string   `json:"day"`
	MRs     int      `json:"mrs"`
	Reverts int      `json:"reverts"`
	CostUSD *float64 `json:"costUsd"`
	// USDPerMR is the cost of the day's work over the requests merged that day; null when either is unknown.
	USDPerMR *float64 `json:"usdPerMr"`
}

// SourceState is how a collector stands, for the screen's «data is late» marks.
type SourceState struct {
	Source string `json:"source"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Facts  int    `json:"facts"`
	OKAt   *int64 `json:"okAt"`
	At     int64  `json:"collectedAt"`
}

// Summary is the answer of the summary endpoint.
//
// The fields scope, metrics, fixes, changes and scannedAt (and sessions, readingShare in totals) are what the screen's
// types/efficiency.ts IEffSummary reads, so the screen needs no adapter. The hub has no transcript scanner yet: fixes and
// changes are empty and sessions is 0 until one feeds them.
type Summary struct {
	Scope     string        `json:"scope"`
	From      string        `json:"from"`
	To        string        `json:"to"` // the last day of the period, inclusive
	Days      []Day         `json:"days"`
	Totals    Totals        `json:"totals"`
	Metrics   []Metric      `json:"metrics"`
	Fixes     []struct{}    `json:"fixes"`
	Changes   []struct{}    `json:"changes"`
	ScannedAt *string       `json:"scannedAt"`
	Sources   []SourceState `json:"sources"`
}

// Totals are the sums over the period.
type Totals struct {
	MRs          int      `json:"mrs"`
	Reverts      int      `json:"reverts"`
	CostUSD      *float64 `json:"costUsd"`
	USDPerMR     *float64 `json:"usdPerMr"`
	Sessions     int      `json:"sessions"`
	ReadingShare *float64 `json:"readingShare"`
}

// CostFn gives the cost in US dollars of the work done on a day (YYYY-MM-DD in loc), false when it is not known.
type CostFn func(ctx context.Context, day string, loc *time.Location) (float64, bool)

// IsRevert says that a request undoes another: GitLab titles it `Revert "<title>"`.
func IsRevert(title string) bool {
	return strings.HasPrefix(title, `Revert "`) && strings.HasSuffix(title, `"`)
}

// Compute counts the merged requests of [from, to) per day and divides the cost by them.
//
// The denominator is every request the code host reports as merged, whoever created it and however: the form, a push
// option or the host's API. It is never the requests sessions mention. Agents create and merge requests through the API,
// no session notices them, and counting by what sessions saw made the money per request read several times too high
// (an earlier scanner counted 120 requests in a week when 713 had merged). A revert is not a delivered
// task: it is counted on its own and kept out of the denominator.
func Compute(ctx context.Context, st store.Store, from, to time.Time, loc *time.Location, cost CostFn) (Summary, error) {
	if loc == nil {
		loc = time.UTC
	}
	facts, err := st.EffFactsBetween(ctx, KindMR, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return Summary{}, err
	}
	days := map[string]*Day{}
	var order []string
	for d := from.In(loc).Truncate(24 * time.Hour); d.Before(to); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		if _, ok := days[k]; !ok {
			days[k] = &Day{Day: k}
			order = append(order, k)
		}
	}
	for _, f := range facts {
		var m MR
		if json.Unmarshal([]byte(f.Data), &m) != nil {
			continue
		}
		d := days[time.UnixMilli(f.At).In(loc).Format("2006-01-02")]
		if d == nil {
			continue
		}
		if IsRevert(m.Title) {
			d.Reverts++
		} else {
			d.MRs++
		}
	}
	out := Summary{Scope: "team", From: from.In(loc).Format("2006-01-02"), To: to.In(loc).AddDate(0, 0, -1).Format("2006-01-02"),
		Fixes: []struct{}{}, Changes: []struct{}{}, Days: make([]Day, 0, len(order))}
	var costSum float64
	costKnown := false
	for _, k := range order {
		d := days[k]
		if cost != nil {
			if v, ok := cost(ctx, k, loc); ok {
				d.CostUSD = &v
				costSum += v
				costKnown = true
				d.USDPerMR = ratio(v, d.MRs)
			}
		}
		out.Totals.MRs += d.MRs
		out.Totals.Reverts += d.Reverts
		out.Days = append(out.Days, *d)
	}
	if costKnown {
		out.Totals.CostUSD = &costSum
		out.Totals.USDPerMR = ratio(costSum, out.Totals.MRs)
	}
	states, err := st.EffSourceStates(ctx)
	if err != nil {
		return Summary{}, err
	}
	out.Sources = make([]SourceState, 0, len(states))
	for _, s := range states {
		x := SourceState{Source: s.Source, OK: s.OK, Error: s.Error, Facts: s.Facts, At: s.CollectedAt}
		if s.OKAt > 0 {
			v := s.OKAt
			x.OKAt = &v
		}
		out.Sources = append(out.Sources, x)
	}
	out.Metrics = metrics(out.Days)
	for _, s := range out.Sources {
		if s.OKAt != nil && (out.ScannedAt == nil || *s.OKAt > latestMs(*out.ScannedAt)) {
			v := time.UnixMilli(*s.OKAt).UTC().Format(time.RFC3339)
			out.ScannedAt = &v
		}
	}
	return out, nil
}

func latestMs(rfc string) int64 {
	t, _ := time.Parse(time.RFC3339, rfc)
	return t.UnixMilli()
}

func ratio(cost float64, n int) *float64 {
	if n == 0 {
		return nil
	}
	v := cost / float64(n)
	return &v
}

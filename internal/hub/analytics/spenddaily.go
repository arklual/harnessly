package analytics

import (
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The spend by day (GET …/spend/daily): the hourly rollup of the period summed per day of the organization's time zone and
// split by team or model, at API prices. One read of usage_rollup_hour, like …/spend; no sessions are touched.

// DailyLine is one team's or model's spend, a value per day of DailySpend.Days.
type DailyLine struct {
	Key string `json:"key"`
	// USD at API prices.
	Values []float64 `json:"values"`
}

// DailySpend is the answer of …/spend/daily.
type DailySpend struct {
	Scope  orgs.ScopeView `json:"scope"`
	Period Period         `json:"period"`
	Split  string         `json:"split"`
	// Days are the period's days, oldest first, YYYY-MM-DD in the organization's time zone.
	Days  []string    `json:"days"`
	Lines []DailyLine `json:"lines"`
	// CmpTotalUSD is the comparison period's spend at API prices; null unless cmpFrom/cmpTo were asked.
	CmpTotalUSD *float64 `json:"cmpTotalUsd"`
}

func (s *Service) mountSpendDaily(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/spend/daily", s.gate(s.cached(s.spendDaily)))
}

// dailyLines sums the rows by day and by the split's key; lines are ordered by their total, the largest first.
func dailyLines(rows []store.RollupRow, in func(store.RollupRow) bool, sp span, split string) ([]string, []DailyLine) {
	var days []string
	at := map[string]int{}
	for d := sp.from; d.Before(sp.to); d = d.AddDate(0, 0, 1) {
		at[d.Format(day)] = len(days)
		days = append(days, d.Format(day))
	}
	sums := map[string][]float64{}
	for _, r := range rows {
		if !in(r) || r.ListCostMicros == 0 {
			continue
		}
		i, ok := at[time.UnixMilli(r.Hour).In(sp.loc).Format(day)]
		if !ok {
			continue
		}
		k := keyOf(split, r)
		if sums[k] == nil {
			sums[k] = make([]float64, len(days))
		}
		sums[k][i] += float64(r.ListCostMicros) / 1e6
	}
	lines := make([]DailyLine, 0, len(sums))
	for k, v := range sums {
		lines = append(lines, DailyLine{Key: k, Values: v})
	}
	total := func(l DailyLine) (t float64) {
		for _, v := range l.Values {
			t += v
		}
		return t
	}
	slices.SortFunc(lines, func(a, b DailyLine) int {
		if d := total(b) - total(a); d != 0 {
			if d > 0 {
				return 1
			}
			return -1
		}
		return compareStrings(a.Key, b.Key)
	})
	return days, lines
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (s *Service) spendDaily(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.Money)
	if err != nil {
		return err
	}
	split := r.URL.Query().Get("split")
	if split == "" {
		split = "team"
	}
	if split != "team" && split != "model" {
		return httpx.Invalid("split", "team or model")
	}
	sp, err := parsePeriod(r.URL.Query(), x.loc, x.now)
	if err != nil {
		return err
	}
	from, to := sp.ms()
	rows, err := st.RollupRows(ctx, x.org.ID, from, to)
	if err != nil {
		return err
	}
	out := DailySpend{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID}, Period: sp.period(), Split: split}
	out.Days, out.Lines = dailyLines(rows, x.in, sp, split)
	q := r.URL.Query()
	if q.Get("cmpFrom") != "" || q.Get("cmpTo") != "" {
		cs, err := comparison(q, sp)
		if err != nil {
			return err
		}
		cf, ct := cs.ms()
		prev, err := st.RollupRows(ctx, x.org.ID, cf, ct)
		if err != nil {
			return err
		}
		var sum int64
		for _, r := range prev {
			if x.in(r) {
				sum += r.ListCostMicros
			}
		}
		v := float64(sum) / 1e6
		out.CmpTotalUSD = &v
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

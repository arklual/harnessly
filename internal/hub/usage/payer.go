package usage

import (
	"context"
	"math"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Payer computes paid (docs/hub/analytics.md, «Money and periods»): on a key or a prepaid balance it is the list price;
// on a subscription, the account's monthly fee split in proportion to everyone's list on it in that calendar month;
// nothing on an account the hub does not know.
type Payer struct {
	st       store.Store
	loc      *time.Location
	accounts map[string]store.Account
	totals   map[string]map[int64]int64 // account → month start (ms) → everyone's list in the month
}

// NewPayer is a payer over these accounts, with calendar months cut in loc.
func NewPayer(ctx context.Context, st store.Store, loc *time.Location, accountIDs []string) (*Payer, error) {
	ids := make([]string, 0, len(accountIDs))
	seen := map[string]bool{}
	for _, id := range accountIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	accounts, err := st.AccountsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	return &Payer{st: st, loc: loc, accounts: accounts, totals: map[string]map[int64]int64{}}, nil
}

// Account is one of the payer's accounts.
func (p *Payer) Account(id string) (store.Account, bool) {
	a, ok := p.accounts[id]
	return a, ok
}

// MonthStart is the midnight of the first day of t's month in loc.
func MonthStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
}

// Paid is what list micro-dollars spent at `at` (epoch ms) on the account cost.
func (p *Payer) Paid(ctx context.Context, accountID string, at, list int64) (int64, error) {
	a, ok := p.accounts[accountID]
	if !ok {
		return 0, nil
	}
	switch a.Kind {
	case "api_key", "prepaid":
		return list, nil
	case "subscription":
	default:
		return 0, nil
	}
	if a.MonthlyFeeMicros <= 0 || list <= 0 {
		return 0, nil
	}
	m := MonthStart(time.UnixMilli(at), p.loc)
	key := m.UnixMilli()
	byMonth := p.totals[accountID]
	if byMonth == nil {
		byMonth = map[int64]int64{}
		p.totals[accountID] = byMonth
	}
	total, ok := byMonth[key]
	if !ok {
		sums, err := p.st.AccountListTotals(ctx, []string{accountID}, key, m.AddDate(0, 1, 0).UnixMilli())
		if err != nil {
			return 0, err
		}
		total = sums[accountID]
		byMonth[key] = total
	}
	if total <= 0 {
		return 0, nil
	}
	return int64(math.Round(float64(a.MonthlyFeeMicros) * float64(list) / float64(total))), nil
}

// AccountLabel is the account's name, or its provider and plan while nobody has named it.
func AccountLabel(a store.Account, ok bool) string {
	switch {
	case !ok:
		return "Неизвестный аккаунт"
	case a.Label != "":
		return a.Label
	case a.Plan != "":
		return a.Provider + " " + a.Plan
	case a.Kind == "api_key":
		return a.Provider + " ключ"
	case a.Kind == "subscription":
		return a.Provider + " подписка"
	}
	return a.Provider
}

package analytics

import (
	"context"

	"github.com/ly-solutions/harnessly/internal/hub/rates"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

// Currency is the rate the shown amounts of an answer use.
type Currency = rates.Currency

// Money is an amount in USD and in the shown currency.
type Money = rates.Money

// currency is the rate of code for the month the span starts in (analytics.md, «Money and periods»).
func (s *Service) currency(ctx context.Context, code string, sp span) Currency {
	if code == "" {
		code = "RUB"
	}
	return s.rates.For(ctx, code, monthStart(sp.from, sp.loc))
}

func fundingOf(f string) string {
	if f == store.FundingOrg {
		return store.FundingOrg
	}
	return store.FundingPersonal
}

func accountLabel(a store.Account, ok bool) string { return usage.AccountLabel(a, ok) }

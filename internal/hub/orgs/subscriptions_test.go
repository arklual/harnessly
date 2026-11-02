package orgs_test

import (
	"context"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// subscribed uses «Claude Max №2» ($200 a month) in October: boris $60 on the 10th and $30 in the current window,
// vera $10 in it. So the fee splits 90/10 over the month ($180 and $20) and 75/25 over the window (92% × share).
func (v veraCase) subscribed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	st := v.s.DB.S()
	n := 0
	use := func(user string, when time.Time, dollars float64) {
		n++
		key := "sub" + string(rune('a'+n))
		ended := when.Add(time.Hour).UnixMilli()
		if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: key, UserID: user, SpaceID: v.tenant,
			TeamID: v.team.ID, AccountID: v.max2, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: when.UnixMilli(),
			EndedAt: &ended, Metrics: "{}", Seq: 1, UpdatedAt: ended}); err != nil {
			t.Fatal(err)
		}
		list := int64(dollars * 1e6)
		if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: v.org, Provider: "claude", MsgKey: key + "m", SessionKey: key, Ts: when.UnixMilli(),
			Model: "claude-opus-4-6", Input: 100, ListCostMicros: &list, ReceivedAt: when.UnixMilli()}); err != nil {
			t.Fatal(err)
		}
		if err := st.Rollup(ctx, v.org, []int64{store.HourMs(when.UnixMilli())}); err != nil {
			t.Fatal(err)
		}
	}
	use(v.boris.ID, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC), 60)
	use(v.boris.ID, time.Date(2026, 10, 31, 13, 0, 0, 0, time.UTC), 30)
	use(v.vera.ID, time.Date(2026, 10, 31, 13, 0, 0, 0, time.UTC), 10)
	// An unused subscription of the organization, one of another tenant, and a personal one (not the organization's).
	for _, a := range []store.Account{
		{ID: "a2pro", OrgKey: v.org, ScopeKind: store.ScopeOrg, ScopeID: v.org, Provider: "anthropic", Kind: "subscription", AccountRef: "pro-ref",
			Label: "Claude Pro", Plan: "pro", Funding: store.FundingOrg, MonthlyFeeMicros: 20e6},
		{ID: "a2other", OrgKey: v.org, ScopeKind: store.ScopeTenant, ScopeID: v.otherTenant, Provider: "openai", Kind: "subscription", AccountRef: "other-ref",
			Label: "ChatGPT Pro", Funding: store.FundingOrg, MonthlyFeeMicros: 100e6},
		{ID: "a2own", OrgKey: v.org, ScopeKind: store.ScopeOrg, ScopeID: v.org, Provider: "anthropic", Kind: "subscription", AccountRef: "own-ref",
			Label: "Личная", Funding: store.FundingPersonal, OwnerUserID: v.vera.ID, MonthlyFeeMicros: 20e6},
	} {
		if err := st.CreateAccount(ctx, &a); err != nil {
			t.Fatal(err)
		}
	}
}

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestSubscriptionsOfTheOrganization(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	v.subscribed(t)

	r := v.anna.Do("GET", v.url("/subscriptions"), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("subscriptions: %s", r)
	}
	var got analytics.SubscriptionsResponse
	r.JSON(&got)
	if got.Scope.Kind != "org" || got.Period.From != "2026-10-01" || got.Period.To != "2026-11-01" {
		t.Errorf("scope %+v period %+v", got.Scope, got.Period)
	}
	// By fee, highest first: Max №2 ($200), ChatGPT Pro ($100), Claude Pro ($20); the personal one is not there.
	if len(got.Subscriptions) != 3 || got.Subscriptions[0].AccountID != v.max2 || got.Subscriptions[1].AccountID != "a2other" ||
		got.Subscriptions[2].AccountID != "a2pro" {
		t.Fatalf("subscriptions = %+v", got.Subscriptions)
	}
	max2 := got.Subscriptions[0]
	if max2.Label != "Claude Max №2" || max2.Provider != "anthropic" || max2.MonthlyFee.USD != 200 || !max2.Preliminary || len(max2.Shares) != 2 {
		t.Fatalf("max2 = %+v", max2)
	}
	b, vr := max2.Shares[0], max2.Shares[1]
	if b.Person.UserID != v.boris.ID || b.Amount.USD != 180 || !closeTo(b.Share, 0.9) || vr.Person.UserID != v.vera.ID || vr.Amount.USD != 20 ||
		!closeTo(vr.Share, 0.1) {
		t.Errorf("shares = %+v", max2.Shares)
	}
	if b.Person.Name == "" || b.Amount.Shown != b.Amount.USD {
		t.Errorf("boris = %+v", b)
	}
	if b.WindowPct == nil || vr.WindowPct == nil || !closeTo(*b.WindowPct, 69) || !closeTo(*vr.WindowPct, 23) {
		t.Errorf("window pct = %v %v", b.WindowPct, vr.WindowPct)
	}
	if len(max2.Windows) != 1 {
		t.Fatalf("windows = %+v", max2.Windows)
	}
	w := max2.Windows[0]
	if w.Name != "five_hour" || w.LengthSec != 5*3600 || w.UsedPct == nil || *w.UsedPct != 92 || w.ResetsAt == nil ||
		*w.ResetsAt != "2026-10-31T17:00:00Z" || w.Waiting || w.MeasuredAt != "2026-10-31T16:00:00Z" {
		t.Errorf("window = %+v", w)
	}
	// 92% four hours in: full at 16:20:52, before the reset at 17:00.
	if w.ForecastFullAt == nil || *w.ForecastFullAt != "2026-10-31T16:20:52Z" {
		t.Errorf("forecast full = %v", w.ForecastFullAt)
	}
	// The shares add up to the fee and are what spend by person counts as paid beside the key (boris $1176 with the paced $70, vera $84).
	var sum float64
	for _, s := range max2.Shares {
		sum += s.Amount.USD
	}
	if !closeTo(sum, 200) {
		t.Errorf("shares sum to %v", sum)
	}
	var spend analytics.SpendResponse
	if r := v.anna.Do("GET", v.url("/spend?by=person"), nil); r.Code != http.StatusOK {
		t.Fatalf("spend: %s", r)
	} else {
		r.JSON(&spend)
	}
	paid := map[string]float64{}
	for _, row := range spend.Rows {
		paid[row.Key] = row.Paid.USD
	}
	if !closeTo(paid[v.boris.ID], 1176+b.Amount.USD) || !closeTo(paid[v.vera.ID], 84+vr.Amount.USD) {
		t.Errorf("spend by person = %v against shares %v and %v", paid, b.Amount.USD, vr.Amount.USD)
	}

	// Unused subscriptions are there with no shares and no windows.
	for _, s := range got.Subscriptions[1:] {
		if len(s.Shares) != 0 || len(s.Windows) != 0 || s.Shares == nil || s.Windows == nil {
			t.Errorf("unused %s = %+v", s.Label, s)
		}
	}
	if got.Subscriptions[2].Plan != "pro" || got.Subscriptions[2].MonthlyFee.USD != 20 {
		t.Errorf("pro = %+v", got.Subscriptions[2])
	}

	// A closed month where nobody used it: no shares and nothing preliminary.
	var sep analytics.SubscriptionsResponse
	v.anna.Do("GET", v.url("/subscriptions?from=2026-09-01&to=2026-10-01"), nil).JSON(&sep)
	if len(sep.Subscriptions) != 3 || sep.Subscriptions[0].Preliminary || len(sep.Subscriptions[0].Shares) != 0 {
		t.Errorf("september = %+v", sep.Subscriptions)
	}

	// The overview carries the same list.
	var o analytics.OverviewResponse
	v.anna.Do("GET", v.url("/overview"), nil).JSON(&o)
	if len(o.Subscriptions) != 3 || o.Subscriptions[0].AccountID != v.max2 || len(o.Subscriptions[0].Shares) != 2 ||
		o.Subscriptions[0].Shares[0].Amount.USD != 180 {
		t.Errorf("overview subscriptions = %+v", o.Subscriptions)
	}
}

func TestSubscriptionsOfALead(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	v.subscribed(t)

	var got analytics.SubscriptionsResponse
	r := v.boris.Do("GET", v.url("/subscriptions?scope=team:"+v.team.ID), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("lead subscriptions: %s", r)
	}
	r.JSON(&got)
	// The team sees the organization's subscriptions, not the other tenant's.
	if got.Scope.Kind != "team" || len(got.Subscriptions) != 2 || got.Subscriptions[0].AccountID != v.max2 || got.Subscriptions[1].AccountID != "a2pro" {
		t.Fatalf("subscriptions = %+v", got)
	}
	if s := got.Subscriptions[0].Shares; len(s) != 2 || s[0].Amount.USD != 180 || s[1].Amount.USD != 20 {
		t.Errorf("team shares = %+v", s)
	}

	// Only a manager sees money: the lead outside his team and a member do not.
	if r := v.boris.Do("GET", v.url("/subscriptions"), nil); r.Code != http.StatusForbidden {
		t.Errorf("lead on the organization: %s", r)
	}
	if r := v.vera.Do("GET", v.url("/subscriptions?scope=team:"+v.team.ID), nil); r.Code != http.StatusForbidden {
		t.Errorf("member: %s", r)
	}
	if r := v.vera.Do("GET", v.url("/subscriptions"), nil); r.Code != http.StatusForbidden {
		t.Errorf("member on the organization: %s", r)
	}
	if r := v.anna.Do("GET", v.url("/subscriptions?period=year"), nil); r.Code != http.StatusBadRequest {
		t.Errorf("bad period: %s", r)
	}
}

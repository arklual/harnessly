package orgs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// paced adds boris's $70 on 30 October: a pace of $10 a day for the forecast, $1260 in October in all.
func (v veraCase) paced(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	st := v.s.DB.S()
	when := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	ended := when.Add(time.Hour).UnixMilli()
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "paced", UserID: v.boris.ID, SpaceID: v.tenant,
		TeamID: v.team.ID, AccountID: v.key1, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: when.UnixMilli(),
		EndedAt: &ended, Metrics: `{"mrsMerged":2}`, Seq: 1, UpdatedAt: ended}); err != nil {
		t.Fatal(err)
	}
	list := int64(70e6)
	if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: v.org, Provider: "claude", MsgKey: "pacedm", SessionKey: "paced", Ts: when.UnixMilli(),
		Model: "claude-opus-4-6", Input: 100, ListCostMicros: &list, ReceivedAt: when.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Rollup(ctx, v.org, []int64{store.HourMs(when.UnixMilli())}); err != nil {
		t.Fatal(err)
	}
}

func near(a, b float64) bool { return a-b < 0.01 && b-a < 0.01 }

func TestOverviewOfTheOrganization(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	orgRule := v.put(t, v.anna, map[string]any{"scope": scope("org", v.org), "amount": 1261}).ID

	r := v.anna.Do("GET", v.url("/overview"), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("overview: %s", r)
	}
	var o analytics.OverviewResponse
	r.JSON(&o)
	if o.Scope.Kind != "org" || o.Period.From != "2026-10-01" || o.Currency.Source != "none" {
		t.Errorf("scope, period, currency = %+v %+v %+v", o.Scope, o.Period, o.Currency)
	}
	// $1260 paid; 5 hours left at $10 a day.
	if o.Spend.Paid.USD != 1260 || o.Spend.Forecast == nil || !near(o.Spend.Forecast.USD, 1262.08) {
		t.Errorf("spend = %+v forecast %+v", o.Spend, o.Spend.Forecast)
	}
	// The same pace at API prices: the screens put it next to the list spend. API keys pay the list, so it is the same.
	if o.Spend.ForecastList == nil || !near(o.Spend.ForecastList.USD, o.Spend.List.USD+2.08) {
		t.Errorf("forecast at list = %+v, list %+v", o.Spend.ForecastList, o.Spend.List)
	}
	if o.Spend.Limit == nil || o.Spend.Limit.RuleID != orgRule || o.Spend.Limit.SetBy == nil || o.Spend.Limit.SetBy.UserID != v.anna.ID {
		t.Errorf("limit = %+v", o.Spend.Limit)
	}
	if o.SpendBy.By != "team" || len(o.SpendBy.Rows) != 1 || o.SpendBy.Rows[0].Label != "Платформа" || o.SpendBy.Rows[0].Paid.USD != 1260 ||
		o.SpendBy.Rows[0].Limit == nil || o.SpendBy.Rows[0].Limit.Amount.USD != 1500 || o.SpendBy.Rows[0].Limit.Pct != 0.84 {
		t.Errorf("spendBy = %+v", o.SpendBy)
	}
	tl := o.Tiles
	if *tl.ActivePeople.Value != 2 || *tl.ActivePeople.CmpValue != 1 || *tl.Sessions.Value != 3 || *tl.MRs.Value != 2 ||
		tl.MoneyPerMR.Value == nil || *tl.MoneyPerMR.Value != 630 || tl.MoneyPerMR.CmpValue != nil || tl.MoneyPerMR.Unit != "usd" {
		t.Errorf("tiles = %+v %+v %+v %+v", tl.ActivePeople, tl.Sessions, tl.MRs, tl.MoneyPerMR)
	}
	// The budget runs out, the organization is at 99.9%, the team and vera at 84%.
	rules := map[string]bool{}
	for _, s := range o.Attention {
		rules[s.Rule+":"+s.Subject.Kind] = true
		if s.Action == nil || s.Action.Kind != "raise_limit" || s.Title == "" || s.Severity != "warn" {
			t.Errorf("signal = %+v", s)
		}
	}
	if len(o.Attention) != 4 || o.Attention[0].Rule != "budget_runs_out" || !rules["over_80:org"] || !rules["over_80:team"] || !rules["over_80:user"] {
		t.Errorf("attention = %+v", o.Attention)
	}
	for _, s := range o.Attention {
		if s.Subject.Kind == "user" && (s.Subject.ID != v.vera.ID || s.Action.UserID == nil || *s.Action.UserID != v.vera.ID) {
			t.Errorf("vera's signal = %+v", s)
		}
	}
	if o.Subscriptions == nil {
		t.Error("subscriptions is null")
	}

	// A closed month: no forecast, September's $500. Signals are about now, not about the period shown: the same four.
	var sep analytics.OverviewResponse
	v.anna.Do("GET", v.url("/overview?from=2026-09-01&to=2026-10-01"), nil).JSON(&sep)
	if sep.Spend.Paid.USD != 500 || sep.Spend.Forecast != nil || sep.Spend.ForecastList != nil || len(sep.Attention) != 4 {
		t.Errorf("september = %+v attention %d", sep.Spend, len(sep.Attention))
	}
}

func TestOverviewOfALead(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	r := v.boris.Do("GET", v.url("/overview?scope=team:"+v.team.ID), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("lead overview: %s", r)
	}
	var o analytics.OverviewResponse
	r.JSON(&o)
	if o.SpendBy.By != "person" || len(o.SpendBy.Rows) != 2 || o.SpendBy.Rows[0].Key != v.boris.ID || o.SpendBy.Rows[0].Paid.USD != 1176 ||
		o.SpendBy.Rows[0].Limit != nil || o.SpendBy.Rows[1].Key != v.vera.ID || o.SpendBy.Rows[1].Limit == nil ||
		o.SpendBy.Rows[1].Limit.RuleID != v.veraRule || o.SpendBy.Rows[1].Limit.Status != "warn" {
		t.Errorf("spendBy = %+v", o.SpendBy)
	}
	if o.Spend.Limit == nil || o.Spend.Limit.RuleID != v.teamRule || len(o.Attention) != 2 {
		t.Errorf("spend %+v attention %+v", o.Spend, o.Attention)
	}
	// The lead sees his team only; vera and gleb manage nothing.
	for _, c := range []struct {
		p   *hubtest.Person
		url string
	}{{v.boris, "/overview"}, {v.boris, "/overview?scope=tenant:" + v.tenant}, {v.vera, "/overview"},
		{v.vera, "/spend?scope=team:" + v.team.ID}, {v.gleb, "/overview"}} {
		if r := c.p.Do("GET", v.url(c.url), nil); r.Code/100 != 4 {
			t.Errorf("%s %s: %s", c.p.Email, c.url, r)
		}
	}
}

func TestSpendBreakdowns(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	get := func(q string) analytics.SpendResponse {
		t.Helper()
		r := v.anna.Do("GET", v.url("/spend?"+q), nil)
		if r.Code != http.StatusOK {
			t.Fatalf("spend %s: %s", q, r)
		}
		var s analytics.SpendResponse
		r.JSON(&s)
		return s
	}
	if s := get("by=model"); len(s.Rows) != 2 || s.Rows[0].Label != "claude-sonnet-4-6" || s.Rows[0].Paid.USD != 1190 || s.Rows[0].Calls != 2 ||
		s.Rows[1].Label != "claude-opus-4-6" || s.Rows[0].Tokens.Input != 200 || s.Total.Paid.USD != 1260 {
		t.Errorf("by model = %+v", s)
	}
	if s := get("by=account"); len(s.Rows) != 1 || s.Rows[0].Key != v.key1 || s.Rows[0].Label == "" {
		t.Errorf("by account = %+v", s.Rows)
	}
	if s := get("by=tenant"); len(s.Rows) != 1 || s.Rows[0].Label != "Платформа" {
		t.Errorf("by tenant = %+v", s.Rows)
	}
	if s := get("by=person&scope=tenant:" + v.tenant); len(s.Rows) != 2 || s.Scope.Kind != "tenant" || s.Rows[1].Limit == nil ||
		s.Rows[1].Limit.Amount.USD != 100 {
		// vera's tightest rule in the tenant is her team's $100 (84%), not the tenant's $150 (56%).
		t.Errorf("tenant by person = %+v", s.Rows)
	}
	if s := get("scope=tenant:" + v.otherTenant); len(s.Rows) != 0 || s.Rows == nil || s.Total.Paid.USD != 0 {
		t.Errorf("empty tenant = %+v", s)
	}
	for _, bad := range []string{"by=day", "scope=team", "scope=space:x", "period=year"} {
		if r := v.anna.Do("GET", v.url("/spend?"+bad), nil); r.Code != http.StatusBadRequest {
			t.Errorf("%s: %s", bad, r)
		}
	}
}

func TestOverviewInRoubles(t *testing.T) {
	asked := 0
	cbr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.URL.Query().Get("date_req") != "01/10/2026" {
			http.Error(w, "date", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="windows-1251"?><ValCurs Date="01.10.2026"><Valute ID="R01235"><NumCode>840</NumCode>` +
			`<CharCode>USD</CharCode><Nominal>1</Nominal><Name>x</Name><Value>81,5000</Value></Valute></ValCurs>`))
	}))
	defer cbr.Close()
	v := newVera(t, hubtest.Env("HUB_RATES_URL", cbr.URL))
	for range 2 {
		var o analytics.OverviewResponse
		v.anna.Do("GET", v.url("/overview"), nil).JSON(&o)
		if o.Currency.Code != "RUB" || o.Currency.PerUSD != 81.5 || o.Currency.Source != "cbr" || o.Currency.RateDate != "2026-10-01" ||
			o.Spend.Paid.Shown != 1190*81.5 {
			t.Errorf("currency = %+v paid %+v", o.Currency, o.Spend.Paid)
		}
	}
	if asked != 1 {
		t.Errorf("the CBR was asked %d times, the rate is kept", asked)
	}
	// The limits screen shows the same rate.
	r := v.anna.Do("GET", v.url("/limits"), nil)
	if !strings.Contains(string(r.Body), `"perUsd":81.5`) {
		t.Errorf("limits currency: %s", r)
	}
}

func TestOrgKPIs(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	// vera's own account merges three MRs: they are not the organization's money and stay out of money_per_mr.
	ctx := context.Background()
	when := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	ended := when.Add(time.Hour).UnixMilli()
	for key, mrs := range map[string]string{"own": "3", "own2": "0"} {
		if _, err := v.s.DB.S().PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: key, UserID: v.vera.ID,
			SpaceID: v.tenant, TeamID: v.team.ID, Funding: store.FundingPersonal, Origin: "live", Attribution: "exact",
			StartedAt: when.UnixMilli(), EndedAt: &ended, Metrics: `{"mrsMerged":` + mrs + `}`, Seq: 1, UpdatedAt: ended}); err != nil {
			t.Fatal(err)
		}
	}
	kpi := func(kv []analytics.KpiValue, name string) *analytics.KpiValue {
		for i := range kv {
			if kv[i].Kpi == name {
				return &kv[i]
			}
		}
		return nil
	}

	r := v.anna.Do("GET", v.url("/kpis?series=team"), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("kpis: %s", r)
	}
	var k struct {
		Scope  struct{ Kind, ID string }
		Period analytics.Period
		Kpis   []analytics.KpiValue
		Series []analytics.KpiSeries
	}
	r.JSON(&k)
	if k.Scope.Kind != "org" || k.Period.From != "2026-10-01" || len(k.Kpis) != 15 {
		t.Errorf("scope %+v period %+v kpis %d", k.Scope, k.Period, len(k.Kpis))
	}
	if m := kpi(k.Kpis, "money_per_mr"); m == nil || m.Value == nil || *m.Value != 630 {
		t.Errorf("money_per_mr = %+v", m)
	}
	if len(k.Series) != 1 || k.Series[0].Key != v.team.ID || k.Series[0].Label != "Платформа" || len(k.Series[0].Kpis) != 15 {
		t.Errorf("series = %+v", k.Series)
	}

	// Without ?series it is null; the lead sees his team; vera manages nothing; only team splits.
	var plain analytics.KpisResponse
	v.boris.Do("GET", v.url("/kpis?scope=team:"+v.team.ID), nil).JSON(&plain)
	if plain.Series != nil || len(plain.Kpis) != 15 {
		t.Errorf("lead kpis = %+v", plain)
	}
	for _, c := range []struct {
		p    *hubtest.Person
		url  string
		code int
	}{{v.vera, "/kpis", 403}, {v.boris, "/kpis", 403}, {v.anna, "/kpis?series=person", 400}} {
		if r := c.p.Do("GET", v.url(c.url), nil); r.Code != c.code {
			t.Errorf("%s %s: %s", c.p.Email, c.url, r)
		}
	}
}

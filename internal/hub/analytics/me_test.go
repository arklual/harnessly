package analytics_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const orgKey = "space:test"

func at(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

func usd(v float64) *int64 {
	m := int64(v * 1e6)
	return &m
}

type fixture struct {
	t  *testing.T
	st store.Store
	n  int
}

func (f *fixture) session(user, key, parent, account, funding, origin, started, metrics string) {
	f.t.Helper()
	ended := at(started) + 3600_000
	if _, err := f.st.PutSession(context.Background(), store.AgentSession{OrgKey: orgKey, Provider: "claude", SessionKey: key, ParentKey: parent,
		UserID: user, AccountID: account, Funding: funding, Origin: origin, Attribution: "exact", StartedAt: at(started), EndedAt: &ended,
		Metrics: metrics, Seq: 1, UpdatedAt: ended}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) call(session, model, ts string, list *int64) {
	f.t.Helper()
	f.n++
	if _, err := f.st.AddCall(context.Background(), store.UsageCall{OrgKey: orgKey, Provider: "claude", MsgKey: fmt.Sprint("m", f.n),
		SessionKey: session, Ts: at(ts), Model: model, Input: 100, ListCostMicros: list, ReceivedAt: at(ts)}); err != nil {
		f.t.Fatal(err)
	}
	if err := f.st.Rollup(context.Background(), orgKey, []int64{store.HourMs(at(ts))}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) account(id, kind, funding, label string, fee float64) {
	f.t.Helper()
	if err := f.st.CreateAccount(context.Background(), &store.Account{ID: id, OrgKey: orgKey, ScopeKind: store.ScopeTenant, ScopeID: "test",
		Provider: "anthropic", Kind: kind, AccountRef: id, Label: label, Funding: funding, MonthlyFeeMicros: *usd(fee)}); err != nil {
		f.t.Fatal(err)
	}
}

// A month of anna's: a subscription the organization pays shared with boris, and her own key; days are Moscow's.
func spend(t *testing.T) (*hubtest.Stack, *hubtest.Person, *hubtest.Person) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com")
	anna := s.SignIn("anna@example.com")
	boris := s.SignIn("boris@example.com")
	f := &fixture{t: t, st: s.DB.S()}
	f.account("sub", "subscription", "org", "Claude Max №2", 200)
	f.account("key", "api_key", "personal", "", 0)
	f.session(anna.ID, "s1", "", "sub", "org", "live", "2026-10-05T07:00:00Z", `{"turns":23,"mrs":1}`)
	f.session(anna.ID, "s1/a", "s1", "sub", "org", "live", "2026-10-05T07:10:00Z", `{}`)
	f.session(anna.ID, "s2", "", "key", "personal", "live", "2026-10-05T22:00:00Z", `{"turns":8}`)
	f.session(boris.ID, "s3", "", "sub", "org", "live", "2026-10-07T09:00:00Z", `{}`)
	f.call("s1", "claude-opus-4-6", "2026-10-05T07:05:00Z", usd(30))
	f.call("s1/a", "claude-sonnet-4-6", "2026-10-05T07:15:00Z", usd(10))
	f.call("s1", "unknown-model", "2026-10-05T07:20:00Z", nil)
	f.call("s2", "claude-sonnet-4-6", "2026-10-05T22:30:00Z", usd(5)) // 01:30 on the 6th in Moscow
	f.call("s3", "claude-opus-4-6", "2026-10-07T09:05:00Z", usd(60))
	f.call("s3", "claude-opus-4-6", "2026-09-30T09:05:00Z", usd(1000)) // another month: not in October's split
	return s, anna, boris
}

func TestMeUsage(t *testing.T) {
	_, anna, _ := spend(t)
	r := anna.Do("GET", "/api/me/usage?from=2026-10-01&to=2026-11-01", nil)
	if r.Code != 200 {
		t.Fatalf("usage: %s", r)
	}
	var u analytics.MeUsageResponse
	r.JSON(&u)
	if u.Period != (analytics.Period{From: "2026-10-01", To: "2026-11-01", TZ: "Europe/Moscow"}) || u.Currency.Source != "none" || u.Currency.PerUSD != 1 {
		t.Errorf("period, currency = %+v %+v", u.Period, u.Currency)
	}
	// Her 40 of everyone's 100 on the subscription in October is 40% of its $200 fee; the key is paid at list.
	if u.Total.List.USD != 45 || u.Total.Paid.USD != 85 {
		t.Errorf("total = %+v", u.Total)
	}
	if len(u.ByDay) != 2 || u.ByDay[0].Day != "2026-10-05" || u.ByDay[0].Paid.USD != 80 || u.ByDay[1].Day != "2026-10-06" || u.ByDay[1].List.USD != 5 {
		t.Errorf("byDay = %+v", u.ByDay)
	}
	if len(u.ByAccount) != 2 || u.ByAccount[0].AccountID != "sub" || u.ByAccount[0].Label != "Claude Max №2" || u.ByAccount[0].Funding != "org" ||
		u.ByAccount[0].List.USD != 40 || u.ByAccount[0].Paid.USD != 80 || u.ByAccount[1].Label != "anthropic ключ" || u.ByAccount[1].Funding != "personal" {
		t.Errorf("byAccount = %+v", u.ByAccount)
	}
	if len(u.ByModel) != 3 || u.ByModel[0].Model != "claude-opus-4-6" || u.ByModel[0].List.USD != 30 || u.ByModel[1].List.USD != 15 ||
		u.ByModel[1].Calls != 2 || u.ByModel[2].Model != "unknown-model" || u.ByModel[2].Calls != 1 {
		t.Errorf("byModel = %+v", u.ByModel)
	}
	if u.AccountedUntil != nil {
		t.Errorf("no machine read yet, accountedUntil = %v", *u.AccountedUntil)
	}
	// A period of nothing.
	var empty analytics.MeUsageResponse
	anna.Do("GET", "/api/me/usage?from=2026-08-01&to=2026-09-01", nil).JSON(&empty)
	if empty.Total.List.USD != 0 || len(empty.ByDay) != 0 || empty.ByDay == nil {
		t.Errorf("empty = %+v", empty)
	}
	for _, bad := range []string{"period=year", "from=2026-10-01", "from=2026-10-02&to=2026-10-01", "from=2025-01-01&to=2026-10-01"} {
		if r := anna.Do("GET", "/api/me/usage?"+bad, nil); r.Code != 400 {
			t.Errorf("%s: %s", bad, r)
		}
	}
}

func TestMeSessionsPages(t *testing.T) {
	_, anna, boris := spend(t)
	var p1 analytics.MeSessionsResponse
	r := anna.Do("GET", "/api/me/sessions?from=2026-10-01&to=2026-11-01&limit=1", nil)
	if r.Code != 200 {
		t.Fatalf("sessions: %s", r)
	}
	r.JSON(&p1)
	if len(p1.Sessions) != 1 || p1.Sessions[0].SessionKey != "s2" || p1.Next == nil || p1.Sessions[0].Paid.USD != 5 {
		t.Fatalf("page 1 = %+v", p1)
	}
	var p2 analytics.MeSessionsResponse
	anna.Do("GET", "/api/me/sessions?from=2026-10-01&to=2026-11-01&limit=1&before="+url.QueryEscape(*p1.Next), nil).JSON(&p2)
	if len(p2.Sessions) != 1 || p2.Next != nil {
		t.Fatalf("page 2 = %+v", p2)
	}
	x := p2.Sessions[0]
	if x.SessionKey != "s1" || x.Subagents != 1 || x.List.USD != 40 || x.Paid.USD != 80 || x.Turns != 23 || x.MRs != 1 || x.Funding != "org" ||
		x.Machine != nil || x.EndedAt == nil || len(x.Models) != 3 || x.Models[0] != "claude-opus-4-6" || x.Provider != "claude" {
		t.Errorf("s1 = %+v", x)
	}
	// Only one's own.
	var b analytics.MeSessionsResponse
	boris.Do("GET", "/api/me/sessions?from=2026-10-01&to=2026-11-01", nil).JSON(&b)
	if len(b.Sessions) != 1 || b.Sessions[0].SessionKey != "s3" {
		t.Errorf("boris = %+v", b)
	}
	if r := anna.Do("GET", "/api/me/sessions?limit=500", nil); r.Code != 400 {
		t.Errorf("limit 500: %s", r)
	}
}

func TestMeFollowsTheFlag(t *testing.T) {
	s := hubtest.New(t)
	anna := s.SignIn("anna@example.com")
	for _, p := range []string{"/api/me/usage", "/api/me/sessions", "/api/me/kpis"} {
		if r := anna.Do("GET", p, nil); r.Code != 404 {
			t.Errorf("%s with HUB_ORGS off: %s", p, r)
		}
	}
	s = hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	if r := s.Browser().Do("GET", "/api/me/usage", nil); r.Code != 401 {
		t.Errorf("signed out: %s", r)
	}
}

func TestMeKPIs(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com")
	anna := s.SignIn("anna@example.com")
	f := &fixture{t: t, st: s.DB.S()}
	f.account("key", "api_key", "personal", "", 0)
	// October: six sessions, three with an MR; September: four, too few to compare.
	for i := range 6 {
		mrs := 0
		if i < 3 {
			mrs = 1
		}
		key := fmt.Sprint("oct", i)
		f.session(anna.ID, key, "", "key", "personal", "live", fmt.Sprintf("2026-10-%02dT09:00:00Z", 2+i),
			fmt.Sprintf(`{"turns":5,"mrs":%d,"mrsMerged":%d,"toolCalls":10,"toolErrors":1}`, mrs, mrs))
		f.call(key, "claude-opus-4-6", fmt.Sprintf("2026-10-%02dT09:05:00Z", 2+i), usd(2))
	}
	f.session(anna.ID, "eval", "", "key", "personal", "eval", "2026-10-20T09:00:00Z", `{"turns":5,"mrs":0}`)
	for i := range 4 {
		f.session(anna.ID, fmt.Sprint("sep", i), "", "key", "personal", "live", fmt.Sprintf("2026-09-%02dT09:00:00Z", 2+i), `{"turns":5,"mrs":1}`)
	}
	r := anna.Do("GET", "/api/me/kpis?from=2026-10-01&to=2026-11-01", nil)
	if r.Code != 200 {
		t.Fatalf("kpis: %s", r)
	}
	var k analytics.KpisResponse
	r.JSON(&k)
	if k.Scope != "me" || k.CmpPeriod.From != "2026-09-01" || k.CmpPeriod.To != "2026-10-01" || len(k.Kpis) != 12 {
		t.Fatalf("kpis = %+v", k)
	}
	by := map[string]analytics.KpiValue{}
	for _, v := range k.Kpis {
		by[v.Kpi] = v
	}
	// The eval session is not in KPI.
	c := by["completion"]
	if c.N != 6 || c.Value == nil || *c.Value != 0.5 || c.CmpN != 4 || c.CmpValue != nil || c.Verdict != "too_few" {
		t.Errorf("completion = %+v", c)
	}
	if m := by["money_per_mr"]; m.Value == nil || *m.Value != 4 || m.Unit != "usd" || m.BetterWhen != "down" {
		t.Errorf("money_per_mr = %+v", m)
	}
	if e := by["tool_errors"]; e.Value == nil || *e.Value != 0.1 {
		t.Errorf("tool_errors = %+v", e)
	}
	if v := by["cache_hit"]; v.Value != nil || v.Verdict != "too_few" {
		t.Errorf("cache_hit without token metrics = %+v", v)
	}
}

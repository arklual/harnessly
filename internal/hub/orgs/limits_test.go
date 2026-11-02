package orgs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// vera is the contract's example (docs/hub/limits.md, web/app/hub-api/fixtures/limits/vera.json): on 31 October at
// 19:00 Moscow vera has spent $84 of her $100 in the team «Платформа» (the team $1190 of $1500, her tenant ceiling $150),
// the organization's «Claude Max №2» is at 92% of its five-hour window against a 90% rule, and opus-4-1 is forbidden.
type veraCase struct {
	acme
	team               orgs.TeamView
	max2, key1         string
	teamRule, veraRule string
}

func newVera(t *testing.T, env ...hubtest.Option) veraCase {
	t.Helper()
	s := hubtest.New(t, append([]hubtest.Option{hubtest.Env("HUB_ORGS", "on")}, env...)...)
	s.Clock.Advance(time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC).Sub(s.Clock.Now()))
	s.SignIn("root@example.com")
	a := acme{s: s, anna: s.SignIn("anna@example.com"), boris: s.SignIn("boris@example.com"),
		vera: s.SignIn("vera@example.com"), gleb: s.SignIn("gleb@example.com")}
	a.tenant, _ = a.anna.NewSpace("Платформа")
	a.otherTenant, _ = a.anna.NewSpace("Продукт")
	a.anna.Join(a.tenant, a.boris)
	a.anna.Join(a.tenant, a.vera)
	a.anna.Join(a.otherTenant, a.gleb)
	a.org = create(t, a.anna, "Acme", a.tenant, a.otherTenant).Org.ID
	v := veraCase{acme: a, max2: "a2claudemax", key1: "a2key"}
	v.team = a.team(t, a.anna, a.tenant, "Платформа")
	for _, p := range []*hubtest.Person{a.boris, a.vera} {
		if r := a.anna.Do("PUT", a.url("/teams/"+v.team.ID+"/members/"+p.ID), map[string]any{}); r.Code/100 != 2 {
			t.Fatalf("team member: %s", r)
		}
	}
	if r := a.grant(a.anna, a.boris, "team", v.team.ID, "lead", true); r.Code/100 != 2 {
		t.Fatalf("lead: %s", r)
	}

	ctx := context.Background()
	st := s.DB.S()
	for _, acc := range []store.Account{
		{ID: v.max2, OrgKey: a.org, ScopeKind: store.ScopeOrg, ScopeID: a.org, Provider: "anthropic", Kind: "subscription", AccountRef: "max2-ref",
			Label: "Claude Max №2", Funding: store.FundingOrg, MonthlyFeeMicros: 200e6},
		{ID: v.key1, OrgKey: a.org, ScopeKind: store.ScopeTenant, ScopeID: a.tenant, Provider: "anthropic", Kind: "api_key", AccountRef: "key-ref",
			Funding: store.FundingOrg},
	} {
		if err := st.CreateAccount(ctx, &acc); err != nil {
			t.Fatal(err)
		}
	}
	used, resets := 92.0, time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := st.PutWindow(ctx, store.AccountWindow{AccountID: v.max2, Scope: "all", Name: store.PeriodFiveHour, LengthSec: 5 * 3600,
		Unit: "pct", UsedPct: &used, ResetsAt: &resets, MeasuredAt: s.Clock.Now().UnixMilli(), Source: "statusline", Confidence: "exact"}); err != nil {
		t.Fatal(err)
	}
	n := 0
	spend := func(user, account string, when time.Time, dollars float64) {
		n++
		key := "s" + string(rune('a'+n))
		ended := when.Add(time.Hour).UnixMilli()
		if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: a.org, Provider: "claude", SessionKey: key, UserID: user, SpaceID: a.tenant,
			TeamID: v.team.ID, AccountID: account, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: when.UnixMilli(),
			EndedAt: &ended, Metrics: "{}", Seq: 1, UpdatedAt: ended}); err != nil {
			t.Fatal(err)
		}
		list := int64(dollars * 1e6)
		if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: a.org, Provider: "claude", MsgKey: key + "m", SessionKey: key, Ts: when.UnixMilli(),
			Model: "claude-sonnet-4-6", Input: 100, ListCostMicros: &list, ReceivedAt: when.UnixMilli()}); err != nil {
			t.Fatal(err)
		}
		if err := st.Rollup(ctx, a.org, []int64{store.HourMs(when.UnixMilli())}); err != nil {
			t.Fatal(err)
		}
	}
	oct := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	spend(a.vera.ID, v.key1, oct, 84)
	spend(a.boris.ID, v.key1, oct.Add(time.Hour), 1106)
	spend(a.boris.ID, v.key1, time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC), 500) // September: not October's

	v.teamRule = v.put(t, a.anna, map[string]any{"scope": scope("team", v.team.ID), "amount": 1500}).ID
	v.veraRule = v.put(t, a.boris, map[string]any{"scope": scope("team", v.team.ID), "userId": a.vera.ID, "amount": 100}).ID
	v.put(t, a.anna, map[string]any{"scope": scope("tenant", a.tenant), "userId": a.vera.ID, "amount": 150})
	v.put(t, a.anna, map[string]any{"scope": scope("org", a.org), "modelPattern": "opus-4-1", "forbid": true})
	v.put(t, a.anna, map[string]any{"scope": scope("org", a.org), "accountId": v.max2, "period": "five_hour", "unit": "window_pct", "amount": 90})
	return v
}

func scope(kind, id string) map[string]string { return map[string]string{"kind": kind, "id": id} }

func (v veraCase) put(t *testing.T, p *hubtest.Person, body map[string]any) orgs.LimitRuleView {
	t.Helper()
	r := p.Do("PUT", v.url("/limits"), body)
	if r.Code != http.StatusOK {
		t.Fatalf("put %v: %s", body, r)
	}
	var out struct{ Rule orgs.LimitRuleView }
	r.JSON(&out)
	return out.Rule
}

func TestLimitsVeraExample(t *testing.T) {
	v := newVera(t)
	r := v.vera.Do("GET", "/api/me/limits", nil)
	if r.Code != 200 {
		t.Fatalf("me limits: %s", r)
	}
	var me orgs.MeLimitsResponse
	r.JSON(&me)
	if me.Mode != "off" || len(me.Rules) != 5 {
		t.Errorf("mode %q, %d rules", me.Mode, len(me.Rules))
	}
	got, _ := json.Marshal(me.Effective)
	want := []orgs.EffectiveView{
		{Account: &orgs.AccountRef{ID: v.max2, Label: "Claude Max №2"}, Status: "stop", Pct: 1.022, Reason: "Окно Claude Max №2 — 92%, сброс в 20:00"},
		{Status: "warn", Pct: 0.84, BindingRuleID: v.veraRule, Reason: "Личный лимит — 84% за октябрь"},
		{ModelPattern: "opus-4-1", Status: "stop", Pct: 1, Reason: "Модель opus-4-1 в организации запрещена"},
	}
	if len(me.Effective) != len(want) {
		t.Fatalf("effective = %s", got)
	}
	for i, w := range want {
		e := me.Effective[i]
		if (e.Account == nil) != (w.Account == nil) || (e.Account != nil && *e.Account != *w.Account) || e.ModelPattern != w.ModelPattern ||
			e.Status != w.Status || e.Pct != w.Pct || e.Reason != w.Reason || (w.BindingRuleID != "" && e.BindingRuleID != w.BindingRuleID) {
			t.Errorf("effective[%d] = %s", i, got)
		}
	}
	if ids := me.Effective[0].RuleIDs; len(ids) != 4 || ids[1] != v.veraRule || ids[2] != v.teamRule {
		t.Errorf("account entry rules = %v", ids)
	}
	if me.Effective[1].ResetsAt == nil || *me.Effective[1].ResetsAt != "2026-10-31T21:00:00Z" || me.Effective[2].ResetsAt != nil {
		t.Errorf("resets = %s", got)
	}

	// The team as its lead sees it: $100 of $1500 handed out to people.
	var team orgs.LimitsResponse
	r = v.boris.Do("GET", v.url("/limits?scope=team:"+v.team.ID), nil)
	if r.Code != 200 {
		t.Fatalf("team limits: %s", r)
	}
	r.JSON(&team)
	if team.Budget == nil || team.Budget.USD != 1500 || team.HandedOutPct == nil || *team.HandedOutPct != 0.067 || len(team.People) != 2 {
		t.Errorf("team = %+v", team)
	}
	for _, rl := range team.Rules {
		if rl.ID == v.teamRule && (rl.Used != 1190 || rl.Status != "ok" || rl.SetBy == nil || rl.SetBy.UserID != v.anna.ID) {
			t.Errorf("team rule = %+v", rl)
		}
	}
}

func TestLimitsRights(t *testing.T) {
	v := newVera(t)
	team := scope("team", v.team.ID)
	for _, c := range []struct {
		who  *hubtest.Person
		body map[string]any
		code int
		err  string
	}{
		{v.vera, map[string]any{"scope": team, "userId": v.vera.ID, "amount": 500}, 403, "self_change"},
		{v.boris, map[string]any{"scope": team, "amount": 5000}, 403, "self_change"},
		{v.boris, map[string]any{"scope": team, "userId": v.boris.ID, "amount": 5000}, 403, "self_change"},
		{v.vera, map[string]any{"scope": scope("tenant", v.tenant), "amount": 5}, 403, "forbidden_scope"},
		{v.boris, map[string]any{"scope": team, "accountId": v.max2, "amount": 5}, 400, "invalid"},
		{v.anna, map[string]any{"scope": team, "userId": v.gleb.ID, "amount": 5}, 400, "invalid"},
		{v.anna, map[string]any{"scope": team, "amount": 0}, 400, "invalid"},
		{v.anna, map[string]any{"scope": team, "forbid": true}, 400, "invalid"},
		{v.anna, map[string]any{"scope": team, "unit": "window_pct", "amount": 50}, 400, "invalid"},
		{v.anna, map[string]any{"scope": scope("org", v.org), "accountId": "nope", "amount": 5}, 400, "invalid"},
	} {
		r := c.who.Do("PUT", v.url("/limits"), c.body)
		if r.Code != c.code || (c.err != "invalid" && r.Err() != c.err) {
			t.Errorf("%s %v: %s", c.who.Email, c.body, r)
		}
	}

	// The organization's own limit is its admin's, marked in the journal; the same rule again is an update in place.
	org := v.put(t, v.anna, map[string]any{"scope": scope("org", v.org), "amount": 10000})
	again := v.put(t, v.anna, map[string]any{"scope": scope("org", v.org), "amount": 12000, "warnPct": 0.9})
	if again.ID != org.ID || again.Amount != 12000 || again.WarnPct != 0.9 {
		t.Errorf("upsert = %+v", again)
	}
	evs, _ := v.audit(t, v.anna, "")
	found := false
	for _, e := range evs {
		if e.Action == "limit.set" && e.Target != nil && e.Target.ID == org.ID && e.Self && e.Before != nil {
			found = true
			if e.Target.Label != "Acme" {
				t.Errorf("org rule label = %q", e.Target.Label)
			}
		}
		if e.Action == "limit.set" && e.Target != nil && e.Target.ID == v.veraRule && e.Target.Label == v.veraRule {
			t.Errorf("vera's rule is labelled by its id")
		}
	}
	if !found {
		t.Errorf("no self limit.set with «было» in the journal")
	}

	// Removing: the lead removes what the lead may set, not the team's budget.
	if r := v.boris.Do("DELETE", v.url("/limits/"+v.teamRule), nil); r.Code != 403 {
		t.Errorf("lead removes team budget: %s", r)
	}
	if r := v.boris.Do("DELETE", v.url("/limits/"+v.veraRule), nil); r.Code != 204 {
		t.Errorf("lead removes vera's: %s", r)
	}
	if r := v.boris.Do("DELETE", v.url("/limits/"+v.veraRule), nil); r.Code != 404 {
		t.Errorf("removed twice: %s", r)
	}

	// A team that goes takes its rules with it.
	if r := v.anna.Do("DELETE", v.url("/teams/"+v.team.ID), nil); r.Code != 204 {
		t.Fatalf("delete team: %s", r)
	}
	if _, err := v.s.DB.S().LimitRuleByID(context.Background(), v.teamRule); !store.IsNotFound(err) {
		t.Errorf("team rule left: %v", err)
	}
}

func TestLimitsPushedToMachine(t *testing.T) {
	v := newVera(t, hubtest.Env("HUB_LIMITS_ENFORCE", "warn"))
	a := v.s.NewAgent(nil)
	a.Hello.Features = append(slices.Clone(a.Hello.Features), hubproto.FeatureBudget)
	a.Handle(hubproto.OpBudgetSet, func(c hubtest.Call) (any, error) {
		set := budgetSet(t, c)
		return hubproto.BudgetSetResult{Version: set.Version}, nil
	})
	if r := a.Register(v.anna.NewCode(v.tenant, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	a.Connect()
	v.s.Eventually("budget.set", func() bool { return len(a.Calls(hubproto.OpBudgetSet)) > 0 })
	set := budgetSet(t, a.Calls(hubproto.OpBudgetSet)[0])
	var mine []hubproto.BudgetEntry
	for _, e := range set.Entries {
		if e.User == "vera@example.com" {
			mine = append(mine, e)
		}
	}
	if set.Mode != "warn" || set.Version == 0 || len(mine) != 3 || mine[0].Account != "max2-ref" || mine[0].Status != "stop" ||
		mine[0].ResetsAt != time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC).UnixMilli() || mine[1].Account != "" || mine[1].Status != "warn" ||
		!mine[2].Forbid || mine[2].ModelPattern != "opus-4-1" || mine[2].ResetsAt != 0 {
		t.Fatalf("set = %+v", set)
	}

	// A change on the hub reaches the machine with a newer version; nothing changed sends nothing.
	v.put(t, v.boris, map[string]any{"scope": scope("team", v.team.ID), "userId": v.vera.ID, "amount": 80})
	v.s.Eventually("second budget.set", func() bool { return len(a.Calls(hubproto.OpBudgetSet)) > 1 })
	next := budgetSet(t, a.Calls(hubproto.OpBudgetSet)[1])
	if next.Version <= set.Version {
		t.Errorf("version %d after %d", next.Version, set.Version)
	}
	if err := v.s.Hub.Limits.Push(context.Background()); err != nil || len(a.Calls(hubproto.OpBudgetSet)) != 2 {
		t.Errorf("an unchanged list was sent again: %v, %d calls", err, len(a.Calls(hubproto.OpBudgetSet)))
	}

	var me orgs.MeLimitsResponse
	v.vera.Do("GET", "/api/me/limits", nil).JSON(&me)
	if me.Mode != "warn" || len(me.Machines) != 1 || me.Machines[0].Version != next.Version {
		t.Errorf("machines = %+v", me.Machines)
	}
}

func budgetSet(t *testing.T, c hubtest.Call) hubproto.BudgetSet {
	raw, _ := json.Marshal(c.Params)
	var set hubproto.BudgetSet
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Error(err)
	}
	return set
}

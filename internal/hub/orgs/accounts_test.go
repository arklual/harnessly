package orgs_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A discovered account is registered from the screen: the organization pays for it from then on, its sessions move
// with their money, and the «unknown account» signal is gone.
func TestRegisterDiscoveredAccount(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	v.liveOn(t)
	if err := st.CreateAccount(ctx, &store.Account{ID: "a-found", OrgKey: v.org, ScopeKind: store.ScopeTenant, ScopeID: v.tenant, Provider: "anthropic",
		Kind: "subscription", AccountRef: "stranger", Funding: store.FundingPersonal, Discovered: true}); err != nil {
		t.Fatal(err)
	}
	when := v.s.Clock.Now().Add(-time.Hour)
	ended := when.Add(10 * time.Minute).UnixMilli()
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "found1", UserID: v.vera.ID, SpaceID: v.tenant,
		TeamID: v.team.ID, AccountID: "a-found", Funding: store.FundingPersonal, Origin: "live", Attribution: "exact", StartedAt: when.UnixMilli(),
		EndedAt: &ended, Metrics: "{}", Seq: 1, UpdatedAt: ended}); err != nil {
		t.Fatal(err)
	}
	v.check(t)
	if rawFind(v.rawSignals(t, v.anna, ""), "unknown_account", "a-found") == nil {
		t.Fatal("no unknown_account signal")
	}

	var list struct {
		Accounts []struct {
			ID         string
			Discovered bool
			Funding    string
			People     []struct{ UserID string }
		}
	}
	r := v.anna.Do("GET", v.url("/accounts"), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("accounts: %s", r)
	}
	r.JSON(&list)
	if len(list.Accounts) == 0 || list.Accounts[0].ID != "a-found" || !list.Accounts[0].Discovered {
		t.Fatalf("discovered first: %+v", list.Accounts)
	}

	body := map[string]any{"label": "Claude Max Веры", "plan": "max", "funding": "org", "monthlyFee": 200, "ownerUserId": v.vera.ID, "tenantId": v.tenant}
	if r := v.vera.Do("PATCH", v.url("/accounts/a-found"), body); r.Code != http.StatusForbidden {
		t.Fatalf("a member registered an account: %s", r)
	}
	if r := v.anna.Do("PATCH", v.url("/accounts/a-found"), map[string]any{"label": "", "funding": "org"}); r.Code != http.StatusBadRequest {
		t.Fatalf("empty label: %s", r)
	}
	if r := v.anna.Do("PATCH", v.url("/accounts/nope"), body); r.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %s", r)
	}
	r = v.anna.Do("PATCH", v.url("/accounts/a-found"), body)
	if r.Code != http.StatusOK {
		t.Fatalf("register: %s", r)
	}
	var got struct {
		Account struct {
			Discovered bool
			Funding    string
			Label      string
			MonthlyFee struct{ USD float64 }
			Owner      *struct{ UserID string }
		}
	}
	r.JSON(&got)
	if got.Account.Discovered || got.Account.Funding != "org" || got.Account.Label != "Claude Max Веры" || got.Account.MonthlyFee.USD != 200 ||
		got.Account.Owner == nil || got.Account.Owner.UserID != v.vera.ID {
		t.Fatalf("registered = %+v", got.Account)
	}
	a, err := st.AccountByID(ctx, "a-found")
	if err != nil || a.Discovered || a.Funding != store.FundingOrg {
		t.Fatalf("stored = %+v, %v", a, err)
	}
	s, err := st.SessionByKey(ctx, v.org, "claude", "found1")
	if err != nil || s.Funding != store.FundingOrg {
		t.Fatalf("session funding = %q, %v", s.Funding, err)
	}
	if v.countRule(t, "unknown_account") != 0 {
		t.Fatal("unknown_account still open")
	}
	ev, err := st.Audit(ctx, store.AuditFilter{OrgID: v.org})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range ev {
		found = found || e.Action == "account.update" && e.TargetID == "a-found"
	}
	if !found {
		t.Fatal("no audit of the registration")
	}
}

// a subscription the hub only discovered is registered as the organization's with a monthly fee; from then on
// its use is the organization's paid money (the fee split by the share of list) and a money limit counts it.
func TestRegisteredSubscriptionPaysAndLimitSees(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	if err := st.CreateAccount(ctx, &store.Account{ID: "a-sub", OrgKey: v.org, ScopeKind: store.ScopeTenant, ScopeID: v.tenant, Provider: "anthropic",
		Kind: "subscription", AccountRef: "claude:vera-max", Funding: store.FundingPersonal, OwnerUserID: v.vera.ID, Discovered: true}); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	ended := when.Add(time.Hour).UnixMilli()
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "sub-vera", UserID: v.vera.ID, SpaceID: v.tenant,
		TeamID: v.team.ID, AccountID: "a-sub", Funding: store.FundingPersonal, Origin: "live", Attribution: "exact", StartedAt: when.UnixMilli(),
		EndedAt: &ended, Metrics: "{}", Seq: 1, UpdatedAt: ended}); err != nil {
		t.Fatal(err)
	}
	list := int64(300e6)
	if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: v.org, Provider: "claude", MsgKey: "sub-vera-m", SessionKey: "sub-vera", Ts: when.UnixMilli(),
		Model: "claude-opus-4-6", Input: 100, ListCostMicros: &list, ReceivedAt: when.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Rollup(ctx, v.org, []int64{store.HourMs(when.UnixMilli())}); err != nil {
		t.Fatal(err)
	}
	personal := func() orgs.EffectiveView {
		t.Helper()
		r := v.vera.Do("GET", "/api/me/limits", nil)
		if r.Code != http.StatusOK {
			t.Fatalf("me limits: %s", r)
		}
		var me orgs.MeLimitsResponse
		r.JSON(&me)
		for _, e := range me.Effective {
			if e.BindingRuleID == v.veraRule {
				return e
			}
		}
		t.Fatalf("no personal limit in %+v", me.Effective)
		return orgs.EffectiveView{}
	}
	if e := personal(); !closeTo(e.Pct, 0.84) {
		t.Fatalf("before: a personal subscription counted, pct %v", e.Pct)
	}

	// A tenant administrator of another tenant has no money rights on this one.
	if r := v.grant(v.anna, v.gleb, "tenant", v.otherTenant, "admin", true); r.Code/100 != 2 {
		t.Fatalf("grant: %s", r)
	}
	body := map[string]any{"label": "Claude Max Веры", "plan": "Max 5x", "funding": "org", "monthlyFee": 50, "tenantId": v.tenant}
	for _, p := range []*hubtest.Person{v.vera, v.gleb} {
		if r := p.Do("PATCH", v.url("/accounts/a-sub"), body); r.Code != http.StatusForbidden {
			t.Fatalf("%s registered an account of another scope: %s", p.Email, r)
		}
	}
	if r := v.anna.Do("PATCH", v.url("/accounts/a-sub"), body); r.Code != http.StatusOK {
		t.Fatalf("register: %s", r)
	}
	// Vera is the only one on it in October: the whole $50 fee is hers, $84 + $50 of her $100.
	if e := personal(); !closeTo(e.Pct, 1.34) || e.Status != "stop" {
		t.Fatalf("after: %+v", e)
	}
	r := v.anna.Do("GET", v.url("/spend?by=account&period=month"), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("spend: %s", r)
	}
	if !strings.Contains(string(r.Body), "Claude Max Веры") {
		t.Fatalf("spend by account has no registered subscription: %s", r)
	}
}

// an account is registered before any machine used it; rights are money on the account's scope, so a tenant's
// administrator registers the tenant's accounts and not the organization's; the journal writes account.create.
func TestCreateAccountAhead(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	if r := v.grant(v.anna, v.gleb, "tenant", v.otherTenant, "admin", true); r.Code/100 != 2 {
		t.Fatalf("grant: %s", r)
	}
	body := func(ref, tenant string) map[string]any {
		return map[string]any{"provider": "codex", "ref": ref, "label": "ChatGPT Pro отдела", "plan": "pro", "funding": "org", "monthlyFee": 200,
			"tenantId": tenant}
	}
	if r := v.vera.Do("POST", v.url("/accounts"), body("codex:team-pro", v.tenant)); r.Code != http.StatusForbidden {
		t.Fatalf("a member created an account: %s", r)
	}
	if r := v.gleb.Do("POST", v.url("/accounts"), body("codex:team-pro", "")); r.Code != http.StatusForbidden {
		t.Fatalf("a tenant admin created an organization's account: %s", r)
	}
	if r := v.anna.Do("POST", v.url("/accounts"), body("codex team pro", v.tenant)); r.Code != http.StatusBadRequest {
		t.Fatalf("bad ref: %s", r)
	}
	r := v.gleb.Do("POST", v.url("/accounts"), body("codex:team-pro", v.otherTenant))
	if r.Code != http.StatusCreated {
		t.Fatalf("create: %s", r)
	}
	var got struct {
		Account struct {
			ID         string
			Provider   string
			Kind       string
			Funding    string
			Discovered bool
			MonthlyFee struct{ USD float64 }
			Scope      struct{ Kind, ID string }
		}
	}
	r.JSON(&got)
	a := got.Account
	if a.ID == "" || a.Provider != "openai" || a.Kind != "subscription" || a.Funding != "org" || a.Discovered || a.MonthlyFee.USD != 200 ||
		a.Scope.Kind != store.ScopeTenant || a.Scope.ID != v.otherTenant {
		t.Fatalf("created = %+v", a)
	}
	if r := v.anna.Do("POST", v.url("/accounts"), body("codex:team-pro", v.otherTenant)); r.Code != http.StatusConflict {
		t.Fatalf("twice: %s", r)
	}
	// A key is a key: no monthly fee, the list price is what it pays.
	r = v.anna.Do("POST", v.url("/accounts"), map[string]any{"provider": "claude", "ref": "key:abc123", "label": "Ключ CI", "funding": "org", "monthlyFee": 10})
	if r.Code != http.StatusCreated {
		t.Fatalf("key: %s", r)
	}
	r.JSON(&got)
	if got.Account.Kind != "api_key" || got.Account.MonthlyFee.USD != 0 || got.Account.Provider != "anthropic" {
		t.Fatalf("key = %+v", got.Account)
	}
	ev, err := st.Audit(ctx, store.AuditFilter{OrgID: v.org})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ev {
		if e.Action == "account.create" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("account.create in the journal %d times", n)
	}
}

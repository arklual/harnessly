package orgs_test

import (
	"context"
	"net/http"
	"testing"
	"time"

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

package seed_test

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/seed"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Two organizations of the shape of a real installation's seed: three people in the first, two of them in the second.
const twoOrgs = `
orgs:
  - slug: alpha
    name: Альфа
    admins: [anna@example.com]
    tenants:
      - slug: alpha-main
        name: Альфа
        owners: [anna@example.com]
        members: [Boris@Example.com, gleb@example.com]
        teams:
          - slug: general
            name: Общая
            members: [anna@example.com, boris@example.com, gleb@example.com]
  - slug: beta
    name: Бета
    timezone: Europe/Moscow
    currency: RUB
    admins: [anna@example.com, boris@example.com]
    tenants:
      - slug: beta-main
        name: Бета
        owners: [anna@example.com]
        members: [boris@example.com]
        teams:
          - slug: general
            name: Общая
            leads: [boris@example.com]
            members: [anna@example.com]
`

func parse(t *testing.T, s string) seed.File {
	t.Helper()
	f, err := seed.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSeedTwiceChangesNothing(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com")             // the instance administrator, not in the seed
	existing := s.SignIn("anna@example.com") // a person who signed in before the seed
	f := parse(t, twoOrgs)
	ctx := t.Context()

	dry, err := seed.Apply(ctx, s.DB, f, true, time.Now())
	if err != nil || len(dry) == 0 {
		t.Fatalf("dry run: %v %v", dry, err)
	}
	if orgs, _ := s.DB.S().AllOrgs(ctx); len(orgs) != 0 {
		t.Fatalf("a dry run wrote %d organizations", len(orgs))
	}
	first, err := seed.Apply(ctx, s.DB, f, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(first, "\n") != strings.Join(dry, "\n") {
		t.Errorf("the dry run said\n%s\nthe run did\n%s", strings.Join(dry, "\n"), strings.Join(first, "\n"))
	}
	for _, c := range first {
		if strings.Contains(c, "user anna@example.com") {
			t.Errorf("a person who signed in was made again: %s", c)
		}
	}
	again, err := seed.Apply(ctx, s.DB, f, false, time.Now())
	if err != nil || len(again) != 0 {
		t.Errorf("second run: %v %v", again, err)
	}
	if dry, err := seed.Apply(ctx, s.DB, f, true, time.Now()); err != nil || len(dry) != 0 {
		t.Errorf("dry run after: %v %v", dry, err)
	}

	// Signing in by the address joins the seeded person; each sees the organizations they are in.
	orgsOf := func(p *hubtest.Person) []string {
		t.Helper()
		var list struct{ Orgs []struct{ Slug, Role string } }
		if r := p.Do("GET", "/api/orgs", nil); r.Code != 200 {
			t.Fatalf("list: %s", r)
		} else {
			r.JSON(&list)
		}
		var out []string
		for _, o := range list.Orgs {
			out = append(out, o.Slug+":"+o.Role)
		}
		sort.Strings(out)
		return out
	}
	boris := s.SignIn("boris@example.com")
	gleb := s.SignIn("gleb@example.com")
	if got := strings.Join(orgsOf(existing), ","); got != "alpha:admin,beta:admin" {
		t.Errorf("anna sees %s", got)
	}
	if got := strings.Join(orgsOf(boris), ","); got != "alpha:member,beta:admin" {
		t.Errorf("boris sees %s", got)
	}
	if got := strings.Join(orgsOf(gleb), ","); got != "alpha:member" {
		t.Errorf("gleb sees %s", got)
	}
	// Exactly one tenant and one team in each, the team the primary of its people.
	var v struct {
		Tenants []struct{ Slug string }
		Teams   []struct {
			Slug    string
			LeadIDs []string
		}
		Me struct{ Teams []struct{ IsPrimary bool } }
	}
	boris.Do("GET", "/api/orgs/beta", nil).JSON(&v)
	if len(v.Tenants) != 1 || len(v.Teams) != 1 || len(v.Teams[0].LeadIDs) != 1 || v.Teams[0].LeadIDs[0] != boris.ID {
		t.Errorf("beta for boris = %+v", v)
	}
	if len(v.Me.Teams) != 1 || !v.Me.Teams[0].IsPrimary {
		t.Errorf("boris's team in beta = %+v", v.Me.Teams)
	}
}

func TestParseRefusesWhatItCannotMake(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":  "orgs:\n  - slug: alpha\n    name: A\n    owner: x\n",
		"bad slug":     "orgs:\n  - slug: Al\n    name: A\n",
		"bad email":    "orgs:\n  - slug: alpha\n    name: A\n    admins: [not-an-address]\n",
		"no owner":     "orgs:\n  - slug: alpha\n    name: A\n    tenants:\n      - slug: alpha-t\n        name: T\n",
		"not a member": "orgs:\n  - slug: alpha\n    name: A\n    tenants:\n      - slug: alpha-t\n        name: T\n        owners: [a@example.com]\n        teams:\n          - slug: team\n            name: Team\n            members: [b@example.com]\n",
		"bad zone":     "orgs:\n  - slug: alpha\n    name: A\n    timezone: Mars/Base\n",
	} {
		if _, err := seed.Parse(strings.NewReader(body)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestOffboardTakesThePersonOutOfEverything(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	ctx := t.Context()
	if _, err := seed.Apply(ctx, s.DB, parse(t, twoOrgs), false, time.Now()); err != nil {
		t.Fatal(err)
	}
	boris := s.SignIn("boris@example.com")

	// The last owner of a tenant is refused, and nothing is written.
	if _, err := seed.Apply(ctx, s.DB, parse(t, "offboard: [anna@example.com]\n"), false, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "last owner") {
		t.Fatalf("anna, the only owner: %v", err)
	}

	// Boris is an admin of beta, a lead and a member there and a member of alpha: all of it goes.
	f := parse(t, "offboard: [Boris@example.com, nobody@example.com]\n")
	dry, err := seed.Apply(ctx, s.DB, f, true, time.Now())
	if err != nil || len(dry) == 0 {
		t.Fatalf("dry run: %v %v", dry, err)
	}
	done, err := seed.Apply(ctx, s.DB, f, false, time.Now())
	if err != nil || strings.Join(done, "\n") != strings.Join(dry, "\n") {
		t.Fatalf("run %v %v, dry run said %v", done, err, dry)
	}
	if again, err := seed.Apply(ctx, s.DB, f, false, time.Now()); err != nil || len(again) != 0 {
		t.Errorf("second run: %v %v", again, err)
	}
	st := s.DB.S()
	if g, _ := st.GrantsOfUser(ctx, boris.ID); len(g) != 0 {
		t.Errorf("grants left: %+v", g)
	}
	if sp, _ := st.SpacesByUser(ctx, boris.ID); len(sp) != 0 {
		t.Errorf("tenants left: %+v", sp)
	}
	if tm, _ := st.TeamsOfUser(ctx, boris.ID); len(tm) != 0 {
		t.Errorf("teams left: %+v", tm)
	}
	if r := boris.Do("GET", "/api/orgs", nil); r.Code != 401 {
		t.Errorf("boris's session still works: %d", r.Code)
	}
}

func TestOffboardRefusesSomeoneTheFileAlsoSeeds(t *testing.T) {
	if _, err := seed.Parse(strings.NewReader(twoOrgs + "offboard: [gleb@example.com]\n")); err == nil {
		t.Error("parsed a person both seeded and offboarded")
	}
}

// An account the seed registers is paid by the organization: its sessions and their hours' money move to the
// organization, the owner's sessions that came with no account are given it when asked, and a later record of such a
// session that names no account keeps it.
func TestSeedRegistersAccounts(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	ctx := t.Context()
	if _, err := seed.Apply(ctx, s.DB, parse(t, twoOrgs), false, time.Now()); err != nil {
		t.Fatal(err)
	}
	st := s.DB.S()
	o, err := st.OrgBySlug(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	anna, err := st.UserByEmail(ctx, "anna@example.com")
	if err != nil {
		t.Fatal(err)
	}
	// A machine reported one session with its account (made discovered and personal) and one from before it knew it.
	disc, err := st.EnsureAccount(ctx, store.Account{OrgKey: o.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID, Provider: "anthropic",
		Kind: "subscription", AccountRef: "claude:u-1", Funding: store.FundingPersonal, OwnerUserID: anna.ID, Discovered: true})
	if err != nil {
		t.Fatal(err)
	}
	cost := int64(2_000_000)
	for i, x := range []struct{ key, account string }{{"new", disc.ID}, {"old", ""}} {
		if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: o.ID, Provider: "claude", SessionKey: x.key, UserID: anna.ID,
			AccountID: x.account, Funding: store.FundingPersonal, Origin: "live", Attribution: "exact", StartedAt: 1000, Seq: 1, UpdatedAt: 1000}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: o.ID, Provider: "claude", MsgKey: x.key, SessionKey: x.key, Ts: int64(i+1) * 3600_000,
			Model: "claude-opus-5-5", Input: 10, ListCostMicros: &cost, ReceivedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Rollup(ctx, o.ID, []int64{3600_000, 7200_000}); err != nil {
		t.Fatal(err)
	}
	orgMoney := func() (calls int64) {
		rows, err := st.RollupRows(ctx, o.ID, 0, 10*3600_000)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Funding == store.FundingOrg {
				calls += r.Calls
			}
		}
		return calls
	}
	if n := orgMoney(); n != 0 {
		t.Fatalf("organization calls before the seed: %d", n)
	}

	f := parse(t, twoOrgs+`
    accounts:
      - provider: claude
        ref: claude:u-1
        label: Claude Max
        plan: Max 20x
        monthlyFee: 200
        owner: Anna@example.com
        history: true
`)
	// The accounts go with the first organization of the file: move the block under alpha.
	f.Orgs[0].Accounts, f.Orgs[1].Accounts = f.Orgs[1].Accounts, nil
	dry, err := seed.Apply(ctx, s.DB, f, true, time.Now())
	if err != nil || len(dry) != 3 {
		t.Fatalf("dry run: %q %v", dry, err)
	}
	if n := orgMoney(); n != 0 {
		t.Fatalf("a dry run moved money: %d", n)
	}
	done, err := seed.Apply(ctx, s.DB, f, false, time.Now())
	if err != nil || strings.Join(done, "\n") != strings.Join(dry, "\n") {
		t.Fatalf("run %q %v, dry run said %q", done, err, dry)
	}
	a, err := st.AccountByID(ctx, disc.ID)
	if err != nil || a.Funding != store.FundingOrg || a.Discovered || a.MonthlyFeeMicros != 200_000_000 || a.Label != "Claude Max" {
		t.Fatalf("account %+v %v", a, err)
	}
	if n := orgMoney(); n != 2 {
		t.Fatalf("organization calls after the seed: %d, want 2", n)
	}
	// The old session's next record names no account: it keeps the one the seed gave it.
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: o.ID, Provider: "claude", SessionKey: "old", UserID: anna.ID,
		Funding: store.FundingPersonal, Origin: "live", Attribution: "exact", StartedAt: 1000, Seq: 2, UpdatedAt: 2000}); err != nil {
		t.Fatal(err)
	}
	if x, err := st.SessionByKey(ctx, o.ID, "claude", "old"); err != nil || x.AccountID != disc.ID || x.Funding != store.FundingOrg {
		t.Fatalf("old session after a record without an account: %+v %v", x, err)
	}
	if again, err := seed.Apply(ctx, s.DB, f, false, time.Now()); err != nil || len(again) != 0 {
		t.Errorf("second run: %q %v", again, err)
	}
}

func TestParseRefusesBadAccounts(t *testing.T) {
	head := "orgs:\n  - slug: alpha\n    name: A\n    accounts:\n      - "
	for name, body := range map[string]string{
		"provider":        "provider: gemini\n        ref: claude:x\n",
		"ref":             "provider: claude\n        ref: anything\n",
		"funding":         "provider: claude\n        ref: claude:x\n        funding: team\n",
		"fee":             "provider: claude\n        ref: claude:x\n        monthlyFee: -1\n",
		"history no user": "provider: claude\n        ref: claude:x\n        history: true\n",
	} {
		if _, err := seed.Parse(strings.NewReader(head + body)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

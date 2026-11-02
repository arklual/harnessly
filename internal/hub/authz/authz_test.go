package authz

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// world is one organization O with tenants T1 (teams A, B) and T2 (team C), and a space X of the old kind, with no
// organization. People:
//
//	oa, oa2  admins of O              ta    admin of T1 (grant)       tow  owner of T1 (membership)
//	la, la2  leads of A               lb    lead of B                 m    member of T1, in A (primary) and B
//	out      member of T2 only        xo    owner of X                inst instance administrator, no grants
type world struct {
	a            Authz
	o, t1, t2, x string
	ta, tb, tc   string
	users        map[string]string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := db.S()
	w := &world{a: New(s), users: map[string]string{}}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"oa", "oa2", "ta", "tow", "la", "la2", "lb", "m", "out", "xo", "inst"} {
		u := &store.User{Email: n + "@example.com"}
		must(s.CreateUser(ctx, u))
		w.users[n] = u.ID
	}
	u := w.users
	o := &store.Org{Slug: "o", Name: "O"}
	must(s.CreateOrg(ctx, o))
	w.o = o.ID
	space := func(slug, owner, org string) string {
		sp := &store.Space{Slug: slug, Name: slug, CreatedBy: owner, OrgID: org}
		must(s.CreateSpace(ctx, sp))
		return sp.ID
	}
	w.t1, w.t2, w.x = space("t1", u["tow"], o.ID), space("t2", u["out"], o.ID), space("x", u["xo"], "")
	// out owns T2 as its creator; make it a plain member there so it manages nothing.
	must(s.SetRole(ctx, w.t2, u["out"], store.RoleMember))
	for _, n := range []string{"ta", "la", "la2", "lb", "m"} {
		must(s.AddMember(ctx, w.t1, u[n], store.RoleMember))
	}
	team := func(space, slug string) string {
		tm := &store.Team{SpaceID: space, Slug: slug, Name: slug}
		must(s.CreateTeam(ctx, tm))
		return tm.ID
	}
	w.ta, w.tb, w.tc = team(w.t1, "a"), team(w.t1, "b"), team(w.t2, "c")
	now := time.Now()
	for _, x := range []struct {
		team, user string
		primary    bool
	}{{w.ta, "la", true}, {w.ta, "la2", true}, {w.ta, "m", true}, {w.tb, "m", false}, {w.tb, "lb", true}, {w.tc, "out", true}} {
		must(s.AddTeamMember(ctx, x.team, u[x.user], x.primary, now))
	}
	grant := func(user, kind, id, role string) {
		_, err := s.Grant(ctx, store.RoleGrant{UserID: u[user], ScopeKind: kind, ScopeID: id, Role: role})
		must(err)
	}
	grant("oa", store.ScopeOrg, o.ID, store.RoleAdmin)
	grant("oa2", store.ScopeOrg, o.ID, store.RoleAdmin)
	grant("ta", store.ScopeTenant, w.t1, store.RoleAdmin)
	grant("la", store.ScopeTeam, w.ta, store.RoleLead)
	grant("la2", store.ScopeTeam, w.ta, store.RoleLead)
	grant("lb", store.ScopeTeam, w.tb, store.RoleLead)
	return w
}

func code(err error) string {
	if err == nil {
		return "ok"
	}
	var he *httpx.Error
	if errors.As(err, &he) {
		if n, ok := he.Details["needs"].(string); ok {
			return he.Code + "/" + n
		}
		return he.Code
	}
	return err.Error()
}

func TestCan(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	org := Scope{store.ScopeOrg, w.o}
	t1 := Scope{store.ScopeTenant, w.t1}
	t2 := Scope{store.ScopeTenant, w.t2}
	x := Scope{store.ScopeTenant, w.x}
	ta := Scope{store.ScopeTeam, w.ta}
	tb := Scope{store.ScopeTeam, w.tb}
	tc := Scope{store.ScopeTeam, w.tc}
	u := w.users

	type tc_ struct {
		name  string
		who   string
		check Check
		want  string
		self  bool
	}
	grant := func(s Scope, role, target string) Check {
		return Check{Action: Grant, Scope: s, Role: role, Target: u[target]}
	}
	revoke := func(s Scope, role, target string) Check {
		return Check{Action: Revoke, Scope: s, Role: role, Target: u[target]}
	}
	limit := func(s Scope) Check { return Check{Action: SetLimit, Scope: s} }
	personal := func(s Scope, target string) Check {
		return Check{Action: SetPersonalLimit, Scope: s, Target: u[target]}
	}
	account := func(s Scope) Check { return Check{Action: SetAccountLimit, Scope: s} }
	view := func(s Scope) Check { return Check{Action: View, Scope: s} }
	money := func(s Scope) Check { return Check{Action: Money, Scope: s} }
	manage := func(s Scope) Check { return Check{Action: Manage, Scope: s} }

	cases := []tc_{
		// §2.3 «Админ организации: назначить, снять — админ организации».
		{name: "org admin grants an org admin", who: "oa", check: grant(org, store.RoleAdmin, "m"), want: "ok"},
		{name: "org admin removes another org admin", who: "oa", check: revoke(org, store.RoleAdmin, "oa2"), want: "ok"},
		{name: "tenant admin cannot grant an org admin", who: "ta", check: grant(org, store.RoleAdmin, "m"), want: "forbidden_scope/org_admin"},
		{name: "tenant admin cannot remove an org admin (never above)", who: "ta", check: revoke(org, store.RoleAdmin, "oa"), want: "forbidden_scope/org_admin"},
		{name: "lead cannot grant an org admin", who: "la", check: grant(org, store.RoleAdmin, "m"), want: "forbidden_scope/org_admin"},
		// §2.3 «Админ тенанта — админ тенанта и выше».
		{name: "tenant admin grants a tenant admin", who: "ta", check: grant(t1, store.RoleAdmin, "m"), want: "ok"},
		{name: "tenant owner grants a tenant admin", who: "tow", check: grant(t1, store.RoleAdmin, "m"), want: "ok"},
		{name: "org admin grants a tenant admin", who: "oa", check: grant(t1, store.RoleAdmin, "m"), want: "ok"},
		{name: "tenant admin removes a tenant admin at their level", who: "tow", check: revoke(t1, store.RoleAdmin, "ta"), want: "ok"},
		{name: "lead cannot grant a tenant admin", who: "la", check: grant(t1, store.RoleAdmin, "m"), want: "forbidden_scope/tenant_admin"},
		{name: "tenant admin of T1 has nothing in T2", who: "ta", check: grant(t2, store.RoleAdmin, "out"), want: "forbidden_scope/tenant_admin"},
		// §2.3 «Лид команды — лид этой команды и выше».
		{name: "lead grants a lead of their team", who: "la", check: grant(ta, store.RoleLead, "m"), want: "ok"},
		{name: "lead removes a co-lead of their team", who: "la", check: revoke(ta, store.RoleLead, "la2"), want: "ok"},
		{name: "tenant admin grants a lead", who: "ta", check: grant(ta, store.RoleLead, "m"), want: "ok"},
		{name: "org admin grants a lead", who: "oa", check: grant(tc, store.RoleLead, "out"), want: "ok"},
		{name: "lead of B cannot grant a lead of A", who: "lb", check: grant(ta, store.RoleLead, "m"), want: "forbidden_scope/team_lead"},
		{name: "member cannot grant a lead", who: "m", check: grant(ta, store.RoleLead, "la"), want: "forbidden_scope/team_lead"},
		{name: "a lead role does not exist on a tenant", who: "oa", check: grant(t1, store.RoleLead, "m"), want: "invalid_request"},
		// §2.3 «Лимит тенанта — админ организации (не админ тенанта)».
		{name: "org admin sets the tenant's limit", who: "oa", check: limit(t1), want: "ok"},
		{name: "tenant admin cannot raise their tenant's limit", who: "ta", check: limit(t1), want: "self_change/org_admin"},
		{name: "tenant owner cannot raise their tenant's limit", who: "tow", check: limit(t1), want: "self_change/org_admin"},
		{name: "lead cannot set the tenant's limit", who: "la", check: limit(t1), want: "forbidden_scope/org_admin"},
		// §2.3 «Лимит команды — админ тенанта и выше (не лид)».
		{name: "tenant admin sets a team's limit", who: "ta", check: limit(ta), want: "ok"},
		{name: "org admin sets a team's limit", who: "oa", check: limit(ta), want: "ok"},
		{name: "lead cannot raise their team's budget", who: "la", check: limit(ta), want: "self_change/tenant_admin"},
		{name: "lead of B cannot set A's budget", who: "lb", check: limit(ta), want: "forbidden_scope/tenant_admin"},
		// §2.3 «Личный лимит в команде — лид команды и выше».
		{name: "lead sets a member's limit in the team", who: "la", check: personal(ta, "m"), want: "ok"},
		{name: "tenant admin sets a member's limit in a team", who: "ta", check: personal(ta, "m"), want: "ok"},
		{name: "nobody sets their own limit", who: "m", check: personal(ta, "m"), want: "self_change/team_lead"},
		// §2.3 «Личный потолок в тенанте, личный лимит лида — админ тенанта и выше».
		{name: "tenant admin sets a personal ceiling in the tenant", who: "ta", check: personal(t1, "m"), want: "ok"},
		{name: "lead cannot set a personal ceiling in the tenant", who: "la", check: personal(t1, "m"), want: "forbidden_scope/tenant_admin"},
		{name: "a lead's own limit: not the lead", who: "la", check: personal(ta, "la"), want: "self_change/tenant_admin"},
		{name: "a lead's limit: not a co-lead", who: "la2", check: personal(ta, "la"), want: "forbidden_scope/tenant_admin"},
		{name: "a lead's limit: the tenant admin", who: "ta", check: personal(ta, "la"), want: "ok"},
		{name: "a tenant admin's ceiling: only from the org", who: "tow", check: personal(t1, "ta"), want: "forbidden_scope/org_admin"},
		{name: "a tenant admin's ceiling: the org admin", who: "oa", check: personal(t1, "ta"), want: "ok"},
		// §2.3 «Лимит организации, лимит на аккаунт организации — админ организации».
		{name: "org admin sets the organization's limit, marked self", who: "oa", check: limit(org), want: "ok", self: true},
		{name: "tenant admin cannot set the organization's limit", who: "ta", check: limit(org), want: "forbidden_scope/org_admin"},
		{name: "org admin sets the limit of an org account", who: "oa", check: account(org), want: "ok", self: true},
		{name: "tenant admin cannot set an org account's shared limit", who: "ta", check: account(org), want: "forbidden_scope/org_admin"},
		// Hole: one key for several tenants. A tenant's own rule on the account is the tenant admin's; another tenant's is not.
		{name: "tenant admin narrows an account in their tenant", who: "ta", check: account(t1), want: "ok"},
		{name: "tenant admin cannot narrow an account in another tenant", who: "ta", check: account(t2), want: "forbidden_scope/tenant_admin"},
		{name: "a lead cannot narrow an account", who: "la", check: account(t1), want: "forbidden_scope/tenant_admin"},
		// Hole: not oneself.
		{name: "an org admin cannot remove themselves", who: "oa", check: revoke(org, store.RoleAdmin, "oa"), want: "self_change/org_admin"},
		{name: "a tenant admin cannot make themselves a lead", who: "ta", check: grant(ta, store.RoleLead, "ta"), want: "self_change/team_lead"},
		// Hole: a person in two teams. B's lead sees and limits B, not A, though m is in both.
		{name: "lead of B sees B", who: "lb", check: view(tb), want: "ok"},
		{name: "lead of B does not see A", who: "lb", check: view(ta), want: "forbidden_scope/team_lead"},
		{name: "lead of B does not see A's money", who: "lb", check: money(ta), want: "forbidden_scope/team_lead"},
		{name: "lead of B limits m in B", who: "lb", check: personal(tb, "m"), want: "ok"},
		{name: "lead of B does not limit m in A", who: "lb", check: personal(ta, "m"), want: "forbidden_scope/team_lead"},
		// Viewing: one's scope and below; a member sees only their own (not the team).
		{name: "org admin sees a team", who: "oa", check: money(tc), want: "ok"},
		{name: "tenant admin sees the tenant's money", who: "ta", check: money(t1), want: "ok"},
		{name: "tenant admin does not see the organization", who: "ta", check: view(org), want: "forbidden_scope/org_admin"},
		{name: "member does not see the team's people", who: "m", check: view(ta), want: "forbidden_scope/team_lead"},
		{name: "member of T2 sees nothing of T1", who: "out", check: view(t1), want: "forbidden_scope/tenant_admin"},
		{name: "lead manages the team", who: "la", check: manage(ta), want: "ok"},
		{name: "lead does not manage the tenant", who: "la", check: manage(t1), want: "forbidden_scope/tenant_admin"},
		// A space with no organization: as before, its owner manages it; nobody but the instance admin is above.
		{name: "owner of an implicit org's tenant manages it", who: "xo", check: manage(x), want: "ok"},
		{name: "owner of an implicit org's tenant grants its admin", who: "xo", check: grant(x, store.RoleAdmin, "m"), want: "ok"},
		{name: "owner of an implicit org's tenant cannot set its limit", who: "xo", check: limit(x), want: "self_change/instance_admin"},
		{name: "instance admin sets an implicit tenant's limit", who: "inst", check: limit(x), want: "ok"},
		{name: "an org admin has nothing in a space outside the org", who: "oa", check: view(x), want: "forbidden_scope/tenant_admin"},
		// The instance administrator manages everything from the top, but is still not allowed changes about themselves.
		{name: "instance admin grants an org admin", who: "inst", check: grant(org, store.RoleAdmin, "m"), want: "ok"},
		{name: "instance admin cannot grant themselves", who: "inst", check: grant(org, store.RoleAdmin, "inst"), want: "self_change/org_admin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Principal{UserID: u[c.who], InstanceAdmin: c.who == "inst"}
			d, err := w.a.Can(ctx, p, c.check)
			if got := code(err); got != c.want {
				t.Fatalf("%s %s %s: %s, want %s", c.who, c.check.Action, c.check.Scope.Kind, got, c.want)
			}
			if err == nil && d.Self != c.self {
				t.Errorf("self = %v, want %v", d.Self, c.self)
			}
		})
	}
}

func TestLastAdmin(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	org := Scope{store.ScopeOrg, w.o}
	p := Principal{UserID: w.users["oa"]}
	if _, err := w.a.Can(ctx, p, Check{Action: Revoke, Scope: org, Role: store.RoleAdmin, Target: w.users["oa2"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.a.S.Revoke(ctx, w.users["oa2"], store.ScopeOrg, w.o, store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// oa is the last admin now: the instance administrator may not take them away either.
	inst := Principal{UserID: w.users["inst"], InstanceAdmin: true}
	_, err := w.a.Can(ctx, inst, Check{Action: Revoke, Scope: org, Role: store.RoleAdmin, Target: w.users["oa"]})
	if got := code(err); got != "last_admin" {
		t.Fatalf("removing the last admin: %s", got)
	}
}

func TestUnknownScopes(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	p := Principal{UserID: w.users["oa"]}
	for _, s := range []Scope{{store.ScopeTeam, "nope"}, {store.ScopeTenant, "nope"}, {store.ScopeOrg, "nope"}} {
		if _, err := w.a.Can(ctx, p, Check{Action: View, Scope: s}); code(err) != "not_found" {
			t.Errorf("%v: %s", s, code(err))
		}
	}
	if _, err := w.a.Can(ctx, p, Check{Action: View, Scope: Scope{"planet", "x"}}); code(err) != "invalid_request" {
		t.Errorf("bad kind: %s", code(err))
	}
}

func TestCapabilities(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	for _, c := range []struct {
		who            string
		s              Scope
		manage, setLim bool
		needsLimit     string
	}{
		{"oa", Scope{store.ScopeOrg, w.o}, true, true, ""},
		{"ta", Scope{store.ScopeTenant, w.t1}, true, false, NeedsOrgAdmin},
		{"la", Scope{store.ScopeTeam, w.ta}, true, false, NeedsTenantAdmin},
		{"m", Scope{store.ScopeTeam, w.ta}, false, false, NeedsTenantAdmin},
	} {
		got, err := w.a.CapabilitiesAt(ctx, Principal{UserID: w.users[c.who]}, c.s)
		if err != nil {
			t.Fatal(err)
		}
		if got.Manage != c.manage || got.SetLimit != c.setLim {
			t.Errorf("%s at %s: %+v", c.who, c.s.Kind, got)
		}
		if c.needsLimit != "" && (got.Needs.SetLimit == nil || *got.Needs.SetLimit != c.needsLimit) {
			t.Errorf("%s at %s: needs %v, want %s", c.who, c.s.Kind, got.Needs.SetLimit, c.needsLimit)
		}
	}
}

// With HUB_ORGS=off the grants and the organization are not consulted: a space's owner manages it, nobody else does.
func TestMembershipOnly(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	off := Authz{S: w.a.S, MembershipOnly: true}
	t1 := Scope{store.ScopeTenant, w.t1}
	for _, c := range []struct {
		who  string
		want string
	}{{"tow", "ok"}, {"ta", "forbidden_scope/tenant_admin"}, {"oa", "forbidden_scope/tenant_admin"}, {"m", "forbidden_scope/tenant_admin"}} {
		_, err := off.Can(ctx, Principal{UserID: w.users[c.who]}, Check{Action: Manage, Scope: t1})
		if got := code(err); got != c.want {
			t.Errorf("%s manages T1 with orgs off: %s, want %s", c.who, got, c.want)
		}
	}
}

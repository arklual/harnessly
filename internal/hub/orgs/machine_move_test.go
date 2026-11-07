package orgs_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Moving a machine to another tenant (GitHub issue 28): rights in both tenants, the machine keeps its identity, the old
// tenant's team and access go, the target's size is checked, a dry run changes nothing, a repeat is a no-op.

type moveCase struct {
	acme
	machine string
	team    string
}

func newMove(t *testing.T) moveCase {
	t.Helper()
	a := newAcme(t)
	team := a.team(t, a.anna, a.tenant, "Платформа-1")
	m := a.anna.Machine(a.tenant, nil)
	if r := a.anna.Do("PATCH", "/api/machines/"+m.MachineID, map[string]any{"teamId": team.ID}); r.Code != 200 {
		t.Fatalf("team: %s", r)
	}
	if r := a.anna.Do("PATCH", "/api/machines/"+m.MachineID, map[string]any{"access": []map[string]string{{"kind": "user", "id": a.boris.ID}, {"kind": "team", "id": team.ID}}}); r.Code != 200 {
		t.Fatalf("access: %s", r)
	}
	return moveCase{acme: a, machine: m.MachineID, team: team.ID}
}

func (c moveCase) move(p *hubtest.Person, body map[string]any) hubtest.Resp {
	return p.Do("POST", "/api/machines/"+c.machine+"/move", body)
}

func (c moveCase) row(t *testing.T) store.Machine {
	t.Helper()
	m, err := c.s.DB.S().MachineByID(t.Context(), c.machine)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (c moveCase) accessCount(t *testing.T) int {
	t.Helper()
	acc, err := c.s.DB.S().MachineAccess(t.Context(), []string{c.machine})
	if err != nil {
		t.Fatal(err)
	}
	return len(acc)
}

func (c moveCase) moveAudits(t *testing.T, org string) map[string]int {
	t.Helper()
	ev, err := c.s.DB.S().Audit(t.Context(), store.AuditFilter{OrgID: org, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, e := range ev {
		if e.Action == "machine.move" && e.TargetID == c.machine {
			out[e.SpaceID]++
		}
	}
	return out
}

func TestMachineMoveRights(t *testing.T) {
	c := newMove(t)
	body := map[string]any{"tenantId": c.otherTenant}
	outsider := c.s.SignIn("olga@example.com")
	if r := c.move(outsider, body); r.Code != 404 {
		t.Fatalf("outsider: %s", r)
	}
	// A member without manage on the source.
	if r := c.move(c.boris, body); r.Code != 403 {
		t.Fatalf("member of the source: %s", r)
	}
	// Manages the source, only a member of the target.
	zed := c.s.SignIn("zed@example.com")
	c.anna.Join(c.otherTenant, zed)
	if r := c.grant(c.anna, zed, "tenant", c.tenant, "admin", true); r.Code/100 != 2 {
		t.Fatalf("grant: %s", r)
	}
	if r := c.move(zed, body); r.Code != 403 {
		t.Fatalf("manage on the source only: %s", r)
	}
	// Manages the target only: the source refuses.
	yan := c.s.SignIn("yan@example.com")
	c.anna.Join(c.tenant, yan)
	c.anna.Join(c.otherTenant, yan)
	if r := c.grant(c.anna, yan, "tenant", c.otherTenant, "admin", true); r.Code/100 != 2 {
		t.Fatalf("grant: %s", r)
	}
	if r := c.move(yan, body); r.Code != 403 {
		t.Fatalf("manage on the target only: %s", r)
	}
	if r := c.move(c.anna, map[string]any{"tenantId": "no-such"}); r.Code != 404 {
		t.Fatalf("no such tenant: %s", r)
	}
	if got := c.row(t); got.SpaceID != c.tenant || got.TeamID != c.team {
		t.Fatalf("a refusal moved it: %+v", got)
	}
}

func TestMachineMoveKeepsIdentityAndRevokesAccess(t *testing.T) {
	c := newMove(t)
	before := c.row(t)

	// The dry run says what happens and changes nothing.
	r := c.move(c.anna, map[string]any{"tenantId": c.otherTenant, "dry_run": true})
	if r.Code != 200 {
		t.Fatalf("dry run: %s", r)
	}
	var dry struct {
		DryRun bool `json:"dry_run"`
		Plan   machines.MovePlan
	}
	r.JSON(&dry)
	if !dry.DryRun || dry.Plan.Noop || dry.Plan.TeamCleared == nil || dry.Plan.TeamCleared.ID != c.team || len(dry.Plan.AccessRevoked) != 2 {
		t.Fatalf("plan = %+v", dry.Plan)
	}
	if got := c.row(t); got.SpaceID != c.tenant || got.TeamID != c.team || c.accessCount(t) != 2 {
		t.Fatalf("the dry run changed it: %+v", got)
	}
	if n := len(c.moveAudits(t, c.org)); n != 0 {
		t.Fatalf("the dry run wrote the journal: %d", n)
	}

	r = c.move(c.anna, map[string]any{"tenantId": c.otherTenant})
	if r.Code != 200 {
		t.Fatalf("move: %s", r)
	}
	after := c.row(t)
	if after.ID != before.ID || after.KeyPub != before.KeyPub || after.CreatedAt != before.CreatedAt {
		t.Fatalf("identity changed: %+v -> %+v", before, after)
	}
	if after.SpaceID != c.otherTenant || after.TeamID != "" || c.accessCount(t) != 0 {
		t.Fatalf("after = %+v, access %d", after, c.accessCount(t))
	}
	// The journal of both tenants has it.
	au := c.moveAudits(t, c.org)
	if au[c.tenant] != 1 || au[c.otherTenant] != 1 {
		t.Fatalf("journal = %v", au)
	}

	// Again: 200, nothing moves, nothing is written.
	r = c.move(c.anna, map[string]any{"tenantId": c.otherTenant})
	var again struct {
		Moved bool
		Plan  machines.MovePlan
	}
	r.JSON(&again)
	if r.Code != 200 || again.Moved || !again.Plan.Noop {
		t.Fatalf("repeat: %s", r)
	}
	if au := c.moveAudits(t, c.org); au[c.tenant] != 1 || au[c.otherTenant] != 1 {
		t.Fatalf("the repeat wrote the journal: %v", au)
	}
	// The old tenant's members no longer see it in their list; the new one's do.
	if r := c.gleb.Do("GET", "/api/machines/"+c.machine, nil); r.Code != 200 {
		t.Fatalf("a member of the new tenant: %s", r)
	}
	if r := c.vera.Do("GET", "/api/machines/"+c.machine, nil); r.Code == 200 {
		t.Fatalf("a member of the old tenant still sees it")
	}
}

func TestMachineMoveToTargetTeam(t *testing.T) {
	c := newMove(t)
	if r := c.move(c.anna, map[string]any{"tenantId": c.otherTenant, "teamId": c.team}); r.Code != 400 {
		t.Fatalf("a team of the old tenant: %s", r)
	}
	team := c.team2(t)
	if r := c.move(c.anna, map[string]any{"tenantId": c.otherTenant, "teamId": team}); r.Code != 200 {
		t.Fatalf("move: %s", r)
	}
	if got := c.row(t); got.SpaceID != c.otherTenant || got.TeamID != team {
		t.Fatalf("row = %+v", got)
	}
}

func (c moveCase) team2(t *testing.T) string {
	return c.acme.team(t, c.anna, c.otherTenant, "Продукт-1").ID
}

// The hub caps spend, not machines: a move is never refused by how many machines the target has, and the plan names
// the limit rules new spend will count against.
func TestMachineMoveCountsAgainstTheTargetLimits(t *testing.T) {
	c := newMove(t)
	for range 3 {
		c.anna.Machine(c.otherTenant, nil)
	}
	if _, err := c.s.DB.S().PutLimitRule(t.Context(), store.LimitRule{OrgKey: c.org, ScopeKind: store.ScopeTenant,
		ScopeID: c.otherTenant, Period: store.PeriodMonth, Unit: store.UnitMoney, Amount: 1000, SetBy: c.anna.ID}); err != nil {
		t.Fatal(err)
	}

	r := c.move(c.anna, map[string]any{"tenantId": c.otherTenant, "dry_run": true})
	var dry struct{ Plan machines.MovePlan }
	r.JSON(&dry)
	if r.Code != 200 || dry.Plan.Limits.ToRules != 1 || dry.Plan.Limits.FromRules != 0 {
		t.Fatalf("dry run: %s", r)
	}
	if r = c.move(c.anna, map[string]any{"tenantId": c.otherTenant}); r.Code != 200 {
		t.Fatalf("move: %s", r)
	}
	if got := c.row(t); got.SpaceID != c.otherTenant {
		t.Fatalf("not moved: %+v", got)
	}
}

package orgs_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
)

// acme: anna owns the tenant «Платформа» in the organization (so she is its admin and the org's admin), boris and
// vera are members of the tenant, gleb is in another tenant of the same organization.
type acme struct {
	s                        *hubtest.Stack
	anna, boris, vera, gleb  *hubtest.Person
	org, tenant, otherTenant string
}

func newAcme(t *testing.T) acme {
	t.Helper()
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com") // the first account administers the instance
	a := acme{s: s, anna: s.SignIn("anna@example.com"), boris: s.SignIn("boris@example.com"),
		vera: s.SignIn("vera@example.com"), gleb: s.SignIn("gleb@example.com")}
	a.tenant, _ = a.anna.NewSpace("Платформа")
	a.otherTenant, _ = a.anna.NewSpace("Продукт")
	a.anna.Join(a.tenant, a.boris)
	a.anna.Join(a.tenant, a.vera)
	a.anna.Join(a.otherTenant, a.gleb)
	a.org = create(t, a.anna, "Acme", a.tenant, a.otherTenant).Org.ID
	return a
}

func (a acme) url(path string) string { return "/api/orgs/" + a.org + path }

func (a acme) team(t *testing.T, p *hubtest.Person, tenant, name string) orgs.TeamView {
	t.Helper()
	r := p.Do("POST", a.url("/teams"), map[string]any{"tenantId": tenant, "name": name})
	if r.Code != http.StatusCreated {
		t.Fatalf("create team %q: %s", name, r)
	}
	var out struct{ Team orgs.TeamView }
	r.JSON(&out)
	return out.Team
}

func (a acme) grant(p *hubtest.Person, user *hubtest.Person, kind, id, role string, granted bool) hubtest.Resp {
	return p.Do("PUT", a.url("/grants"), map[string]any{"userId": user.ID, "scope": map[string]string{"kind": kind, "id": id},
		"role": role, "granted": granted})
}

func (a acme) members(t *testing.T, p *hubtest.Person, kind, id string) []orgs.MemberView {
	t.Helper()
	r := p.Do("GET", a.url("/scopes/"+kind+"/"+id+"/members"), nil)
	if r.Code != 200 {
		t.Fatalf("members of %s:%s: %s", kind, id, r)
	}
	var out struct{ Members []orgs.MemberView }
	r.JSON(&out)
	return out.Members
}

func (a acme) audit(t *testing.T, p *hubtest.Person, query string) (evs []orgs.AuditEventView, next *string) {
	t.Helper()
	r := p.Do("GET", a.url("/audit"+query), nil)
	if r.Code != 200 {
		t.Fatalf("audit %s: %s", query, r)
	}
	var out struct {
		Events []orgs.AuditEventView
		Next   *string
	}
	r.JSON(&out)
	return out.Events, out.Next
}

func actions(evs []orgs.AuditEventView) map[string]int {
	m := map[string]int{}
	for _, e := range evs {
		m[e.Action]++
	}
	return m
}

func TestTeamsLifecycle(t *testing.T) {
	a := newAcme(t)
	// Only a tenant's admin and above makes teams.
	if r := a.boris.Do("POST", a.url("/teams"), map[string]any{"tenantId": a.tenant, "name": "Ядро"}); r.Code != 403 || r.Err() != "forbidden_scope" || r.Detail("needs") != "tenant_admin" {
		t.Errorf("a member makes a team: %s", r)
	}
	if r := a.anna.Do("POST", a.url("/teams"), map[string]any{"tenantId": a.tenant, "name": "  "}); r.Code != 400 {
		t.Errorf("an empty name: %s", r)
	}
	core := a.team(t, a.anna, a.tenant, "Ядро")
	if core.Slug == "" || core.TenantID != a.tenant || core.MemberCount != 0 || len(core.LeadIDs) != 0 {
		t.Errorf("team = %+v", core)
	}
	// The same name in the same tenant gets the next slug.
	if again := a.team(t, a.anna, a.tenant, "Ядро"); again.Slug == core.Slug {
		t.Errorf("slugs collide: %q", again.Slug)
	}
	// A tenant of another organization is not found.
	foreign, _ := a.gleb.NewSpace("Чужое")
	if r := a.anna.Do("POST", a.url("/teams"), map[string]any{"tenantId": foreign, "name": "X"}); r.Code != 404 {
		t.Errorf("a team in someone else's space: %s", r)
	}

	// boris becomes the lead: he renames the team but does not delete it.
	if r := a.grant(a.anna, a.boris, "team", core.ID, "lead", true); r.Code != 200 {
		t.Fatalf("grant lead: %s", r)
	}
	var renamed struct{ Team orgs.TeamView }
	if r := a.boris.Do("PATCH", a.url("/teams/"+core.ID), map[string]any{"name": "Ядро-2"}); r.Code != 200 {
		t.Fatalf("lead renames: %s", r)
	} else if r.JSON(&renamed); renamed.Team.Name != "Ядро-2" || len(renamed.Team.LeadIDs) != 1 || renamed.Team.LeadIDs[0] != a.boris.ID {
		t.Errorf("renamed = %+v", renamed.Team)
	}
	if r := a.vera.Do("PATCH", a.url("/teams/"+core.ID), map[string]any{"name": "Моё"}); r.Code != 403 {
		t.Errorf("a member renames: %s", r)
	}
	if r := a.boris.Do("DELETE", a.url("/teams/"+core.ID), nil); r.Code != 403 || r.Detail("needs") != "tenant_admin" {
		t.Errorf("a lead deletes: %s", r)
	}

	// The list: the tenant's admin sees both teams, a plain member of the tenant only theirs, an outsider nothing.
	var list struct{ Teams []orgs.TeamView }
	a.anna.Do("GET", a.url("/teams?tenant="+a.tenant), nil).JSON(&list)
	if len(list.Teams) != 2 {
		t.Errorf("admin's list = %+v", list.Teams)
	}
	if r := a.anna.Do("PUT", a.url("/teams/"+core.ID+"/members/"+a.vera.ID), map[string]any{}); r.Code != 200 {
		t.Fatalf("add vera: %s", r)
	}
	list.Teams = nil
	a.vera.Do("GET", a.url("/teams?tenant="+a.tenant), nil).JSON(&list)
	if len(list.Teams) != 1 || list.Teams[0].ID != core.ID {
		t.Errorf("member's list = %+v", list.Teams)
	}
	if r := a.gleb.Do("GET", a.url("/teams?tenant="+a.tenant), nil); r.Code != 403 {
		t.Errorf("another tenant's member lists: %s", r)
	}

	// A machine of the team keeps its tenant and loses the team; the lead loses the role.
	m := a.anna.Machine(a.tenant, nil)
	if err := a.s.DB.S().SetMachineTeam(t.Context(), m.MachineID, core.ID); err != nil {
		t.Fatal(err)
	}
	if r := a.anna.Do("DELETE", a.url("/teams/"+core.ID), nil); r.Code != 204 {
		t.Fatalf("delete: %s", r)
	}
	mach, err := a.s.DB.S().MachineByID(t.Context(), m.MachineID)
	if err != nil || mach.SpaceID != a.tenant || mach.TeamID != "" {
		t.Errorf("machine after delete = %+v, %v", mach, err)
	}
	if gs, _ := a.s.DB.S().GrantsOfUser(t.Context(), a.boris.ID); len(gs) != 0 {
		t.Errorf("boris keeps %+v", gs)
	}
	if r := a.anna.Do("PATCH", a.url("/teams/"+core.ID), map[string]any{"name": "x"}); r.Code != 404 {
		t.Errorf("a deleted team: %s", r)
	}
	evs, _ := a.audit(t, a.anna, "?scope=tenant:"+a.tenant)
	got := actions(evs)
	for _, want := range []string{"team.create", "team.update", "team.delete", "grant.add", "member.add"} {
		if got[want] == 0 {
			t.Errorf("no %s in the tenant's journal: %v", want, got)
		}
	}
}

func TestTeamMembersAndPrimary(t *testing.T) {
	a := newAcme(t)
	one := a.team(t, a.anna, a.tenant, "Один")
	two := a.team(t, a.anna, a.tenant, "Два")
	a.grant(a.anna, a.boris, "team", one.ID, "lead", true)

	// Only a member of the tenant can be added.
	if r := a.anna.Do("PUT", a.url("/teams/"+one.ID+"/members/"+a.gleb.ID), map[string]any{}); r.Code != 400 || r.Detail("field") != "userId" {
		t.Errorf("an outsider in a team: %s", r)
	}
	// The lead adds vera; her first team is primary.
	var put struct{ Member orgs.MemberView }
	if r := a.boris.Do("PUT", a.url("/teams/"+one.ID+"/members/"+a.vera.ID), map[string]any{}); r.Code != 200 {
		t.Fatalf("lead adds: %s", r)
	} else if r.JSON(&put); len(put.Member.Teams) != 1 || !put.Member.Teams[0].IsPrimary || put.Member.TenantRole == nil || *put.Member.TenantRole != "member" {
		t.Errorf("member = %+v", put.Member)
	}
	// The lead of one team does not manage another, and does not choose a primary team.
	if r := a.boris.Do("PUT", a.url("/teams/"+two.ID+"/members/"+a.vera.ID), map[string]any{}); r.Code != 403 {
		t.Errorf("lead of another team: %s", r)
	}
	a.anna.Do("PUT", a.url("/teams/"+two.ID+"/members/"+a.vera.ID), map[string]any{})
	if r := a.boris.Do("PUT", a.url("/teams/"+one.ID+"/members/"+a.vera.ID), map[string]any{"isPrimary": true}); r.Code != 403 || r.Detail("needs") != "tenant_admin" {
		t.Errorf("lead picks the primary: %s", r)
	}
	// The tenant's admin moves the primary to the second team.
	if r := a.anna.Do("PUT", a.url("/teams/"+two.ID+"/members/"+a.vera.ID), map[string]any{"isPrimary": true}); r.Code != 200 {
		t.Fatalf("primary: %s", r)
	} else if r.JSON(&put); len(put.Member.Teams) != 1 || put.Member.Teams[0].TeamID != two.ID || !put.Member.Teams[0].IsPrimary {
		t.Errorf("after primary (team scope) = %+v", put.Member.Teams)
	}
	for _, m := range a.members(t, a.anna, "tenant", a.tenant) {
		if m.UserID != a.vera.ID {
			continue
		}
		primary := map[string]bool{}
		for _, tr := range m.Teams {
			primary[tr.TeamID] = tr.IsPrimary
		}
		if len(primary) != 2 || primary[one.ID] || !primary[two.ID] {
			t.Errorf("vera's teams = %+v", m.Teams)
		}
	}

	// The team's people see each other; a plain member of the tenant does not read the tenant's list.
	if ms := a.members(t, a.vera, "team", one.ID); len(ms) != 1 || ms[0].UserID != a.vera.ID {
		t.Errorf("team one = %+v", ms)
	}
	if r := a.vera.Do("GET", a.url("/scopes/tenant/"+a.tenant+"/members"), nil); r.Code != 403 || r.Err() != "forbidden_scope" {
		t.Errorf("a member reads the tenant: %s", r)
	}
	if r := a.vera.Do("GET", a.url("/scopes/org/"+a.org+"/members"), nil); r.Code != 403 {
		t.Errorf("a member reads the org: %s", r)
	}
	// The organization's list: everyone of both tenants, once.
	if ms := a.members(t, a.anna, "org", a.org); len(ms) != 4 {
		t.Errorf("org members = %d: %+v", len(ms), ms)
	}

	// Removal: the lead takes vera out; twice is no error.
	for i := 0; i < 2; i++ {
		if r := a.boris.Do("DELETE", a.url("/teams/"+one.ID+"/members/"+a.vera.ID), nil); r.Code != 204 {
			t.Fatalf("remove #%d: %s", i, r)
		}
	}
	if ms := a.members(t, a.boris, "team", one.ID); len(ms) != 0 {
		t.Errorf("after removal = %+v", ms)
	}
}

func TestGrants(t *testing.T) {
	a := newAcme(t)
	team := a.team(t, a.anna, a.tenant, "Ядро")

	// One's own role: self_change.
	if r := a.grant(a.anna, a.anna, "org", a.org, "admin", false); r.Code != 403 || r.Err() != "self_change" {
		t.Errorf("own role: %s", r)
	}
	// A member gives nothing.
	if r := a.grant(a.vera, a.boris, "team", team.ID, "lead", true); r.Code != 403 || r.Err() != "forbidden_scope" {
		t.Errorf("a member grants: %s", r)
	}
	// A lead must be a member of the tenant.
	if r := a.grant(a.anna, a.gleb, "team", team.ID, "lead", true); r.Code != 400 || r.Detail("field") != "userId" {
		t.Errorf("an outsider as lead: %s", r)
	}
	// A tenant's admin from outside becomes its member.
	var g struct{ Grant *orgs.GrantView }
	if r := a.grant(a.anna, a.gleb, "tenant", a.tenant, "admin", true); r.Code != 200 {
		t.Fatalf("tenant admin: %s", r)
	} else if r.JSON(&g); g.Grant == nil || g.Grant.Role != "admin" || g.Grant.GrantedBy == nil || *g.Grant.GrantedBy != a.anna.ID {
		t.Errorf("grant = %+v", g.Grant)
	}
	if role, _ := a.s.DB.S().Role(t.Context(), a.tenant, a.gleb.ID); role != "member" {
		t.Errorf("gleb in the tenant: %q", role)
	}
	// A tenant's admin cannot make an organization's admin.
	if r := a.grant(a.gleb, a.boris, "org", a.org, "admin", true); r.Code != 403 || r.Detail("needs") != "org_admin" {
		t.Errorf("tenant admin makes org admin: %s", r)
	}
	// The wrong role for a scope.
	if r := a.grant(a.anna, a.boris, "team", team.ID, "admin", true); r.Code < 400 {
		t.Errorf("admin of a team: %s", r)
	}
	// boris becomes the org's admin; anna steps down through him; then he is the last and cannot be taken down by her.
	if r := a.grant(a.anna, a.boris, "org", a.org, "admin", true); r.Code != 200 {
		t.Fatalf("second org admin: %s", r)
	}
	if r := a.grant(a.boris, a.anna, "org", a.org, "admin", false); r.Code != 200 {
		t.Fatalf("revoke anna: %s", r)
	} else if r.JSON(&g); g.Grant != nil {
		t.Errorf("revoked grant = %+v", g.Grant)
	}
	if r := a.grant(a.gleb, a.boris, "org", a.org, "admin", false); r.Code != 403 {
		t.Errorf("tenant admin takes org admin: %s", r)
	}
	// Granting twice is one grant and one journal entry.
	a.grant(a.boris, a.vera, "team", team.ID, "lead", true)
	a.grant(a.boris, a.vera, "team", team.ID, "lead", true)
	evs, _ := a.audit(t, a.boris, "?scope=team:"+team.ID)
	if got := actions(evs); got["grant.add"] != 1 {
		t.Errorf("team journal = %v", got)
	}
}

func TestAuditScopesAndPaging(t *testing.T) {
	a := newAcme(t)
	one := a.team(t, a.anna, a.tenant, "Один")
	other := a.team(t, a.anna, a.otherTenant, "Чужая")
	a.anna.Do("PUT", a.url("/teams/"+one.ID+"/members/"+a.vera.ID), map[string]any{})
	a.anna.Do("PUT", a.url("/teams/"+one.ID+"/members/"+a.boris.ID), map[string]any{})
	a.grant(a.anna, a.boris, "team", one.ID, "lead", true)

	// The org's journal holds its descendants' events, with actor and target names.
	evs, _ := a.audit(t, a.anna, "")
	got := actions(evs)
	if got["team.create"] != 2 || got["member.add"] != 2 || got["grant.add"] != 1 || got["org.create"] != 1 {
		t.Errorf("org journal = %v", got)
	}
	for _, e := range evs {
		if e.Action == "grant.add" && (e.Actor == nil || e.Actor.UserID != a.anna.ID || e.Target == nil || e.Target.ID != a.boris.ID ||
			e.Target.Label == "" || e.Scope.Kind != "team" || e.After["role"] != "lead") {
			t.Errorf("grant.add = %+v", e)
		}
	}
	// A team's journal never has a neighbour's events.
	evs, _ = a.audit(t, a.anna, "?scope=team:"+other.ID)
	if len(evs) != 1 || evs[0].Action != "team.create" {
		t.Errorf("other team's journal = %+v", evs)
	}
	// The lead reads the team's journal in full; vera, a person of the team, only about herself.
	if evs, _ = a.audit(t, a.boris, "?scope=team:"+one.ID); len(evs) != 4 {
		t.Errorf("lead's team journal = %v", actions(evs))
	}
	evs, _ = a.audit(t, a.vera, "?scope=team:"+one.ID)
	if len(evs) != 1 || evs[0].Target == nil || evs[0].Target.ID != a.vera.ID {
		t.Errorf("vera's team journal = %+v", evs)
	}
	if evs, _ = a.audit(t, a.vera, ""); len(evs) != 1 {
		t.Errorf("vera's org journal = %v", actions(evs))
	}
	// gleb is not in the tenant: 403 on its journal.
	if r := a.gleb.Do("GET", a.url("/audit?scope=tenant:"+a.tenant), nil); r.Code != 403 {
		t.Errorf("gleb reads the tenant: %s", r)
	}
	if r := a.anna.Do("GET", a.url("/audit?scope=bogus"), nil); r.Code != 400 {
		t.Errorf("bad scope: %s", r)
	}

	// Paging: two at a time walks the whole journal with no repeats, newest first.
	all, _ := a.audit(t, a.anna, "")
	seen := map[string]bool{}
	var last time.Time
	q := "?limit=2"
	for pages := 0; pages < 20; pages++ {
		page, next := a.audit(t, a.anna, q)
		for _, e := range page {
			if seen[e.ID] {
				t.Errorf("repeated %s", e.ID)
			}
			seen[e.ID] = true
			at, _ := time.Parse(time.RFC3339Nano, e.At)
			if !last.IsZero() && at.After(last) {
				t.Errorf("not newest first at %s", e.At)
			}
			last = at
		}
		if next == nil {
			break
		}
		q = "?limit=2&before=" + *next
	}
	if len(seen) != len(all) {
		t.Errorf("paged %d of %d", len(seen), len(all))
	}
}

// The answers have the same fields as the web's fixtures (web/app/hub-api/fixtures/orgs), which contracts.test.ts
// checks against the TypeScript types.
func TestShapesMatchWebFixtures(t *testing.T) {
	a := newAcme(t)
	team := a.team(t, a.anna, a.tenant, "Ядро")
	a.anna.Do("PUT", a.url("/teams/"+team.ID+"/members/"+a.boris.ID), map[string]any{})
	a.grant(a.anna, a.boris, "team", team.ID, "lead", true)

	for _, c := range []struct{ fixture, path, list string }{
		{"members.json", "/scopes/tenant/" + a.tenant + "/members", "members"},
		{"audit.json", "/audit", "events"},
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "app", "hub-api", "fixtures", "orgs", c.fixture))
		if err != nil {
			t.Fatal(err)
		}
		var want map[string]any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		a.anna.Do("GET", a.url(c.path), nil).JSON(&got)
		if keys(got) != keys(want) {
			t.Errorf("%s: top keys %s, fixture %s", c.fixture, keys(got), keys(want))
		}
		gl, _ := got[c.list].([]any)
		wl, _ := want[c.list].([]any)
		if len(gl) == 0 || len(wl) == 0 {
			t.Fatalf("%s: empty list", c.fixture)
		}
		for _, g := range gl {
			if keys(g.(map[string]any)) != keys(wl[0].(map[string]any)) {
				t.Errorf("%s: item keys %s, fixture %s", c.fixture, keys(g.(map[string]any)), keys(wl[0].(map[string]any)))
			}
		}
	}
}

func keys(m map[string]any) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

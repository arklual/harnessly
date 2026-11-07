package orgs_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// «Новый тенант» of an organization (GitHub issue 23), the whole way: the tenant is the organization's, the author runs
// it, a team and a machine go into it at once; a refusal and a failure leave nothing, a retry makes no second one.

type madeTenant struct {
	Tenant orgs.TenantView
	Space  struct{ ID, Slug, Name, Role, OnboardingStep string }
}

func tenantsOf(t *testing.T, a acme) []orgs.TenantView {
	t.Helper()
	var v view
	if r := a.anna.Do("GET", a.url(""), nil); r.Code != 200 {
		t.Fatalf("org: %s", r)
	} else {
		r.JSON(&v)
	}
	return v.Tenants
}

func spacesNamed(t *testing.T, a acme, name string) []store.Space {
	t.Helper()
	all, err := a.s.DB.S().SpacesByUser(t.Context(), a.anna.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Space
	for _, sp := range all {
		if sp.Name == name {
			out = append(out, sp.Space)
		}
	}
	return out
}

func TestNewTenantBelongsToItsOrganization(t *testing.T) {
	a := newAcme(t)
	before := len(tenantsOf(t, a))

	// Refused: a member of a tenant is not an administrator of the organization; someone outside it does not see it.
	if r := a.vera.Do("POST", a.url("/tenants"), map[string]any{"name": "Чужой", "requestId": "vera-press-1"}); r.Code != 403 || r.Err() != "forbidden_scope" {
		t.Fatalf("a tenant member: %s", r)
	}
	outsider := a.s.SignIn("olga@example.com")
	if r := outsider.Do("POST", a.url("/tenants"), map[string]any{"name": "Чужой"}); r.Code != 404 {
		t.Fatalf("someone outside: %s", r)
	}
	if r := a.anna.Do("POST", a.url("/tenants"), map[string]any{"name": " ", "requestId": "anna-press-0"}); r.Code != 400 {
		t.Fatalf("an empty name: %s", r)
	}
	if n := len(tenantsOf(t, a)); n != before {
		t.Fatalf("refusals made tenants: %d, was %d", n, before)
	}

	// A failure inside the transaction leaves no space anywhere, neither in the organization nor of its own.
	orgs.FailTenantMade(t, errors.New("the binding broke"))
	if r := a.anna.Do("POST", a.url("/tenants"), map[string]any{"name": "Аналитика", "requestId": "anna-press-1"}); r.Code < 500 {
		t.Fatalf("a failed binding answered %s", r)
	}
	if got := spacesNamed(t, a, "Аналитика"); len(got) != 0 {
		t.Fatalf("a failed binding left %+v", got)
	}
	orgs.FailTenantMade(t, nil)

	// The retry of that press makes it; the next retry gets the same tenant back.
	r := a.anna.Do("POST", a.url("/tenants"), map[string]any{"name": "Аналитика", "requestId": "anna-press-1"})
	if r.Code != http.StatusCreated {
		t.Fatalf("create: %s", r)
	}
	var made madeTenant
	r.JSON(&made)
	if made.Tenant.OrgID != a.org || made.Tenant.ID == "" || made.Space.ID != made.Tenant.ID || made.Space.Role != "owner" {
		t.Fatalf("made = %+v", made)
	}
	r = a.anna.Do("POST", a.url("/tenants"), map[string]any{"name": "Аналитика", "requestId": "anna-press-1"})
	var again madeTenant
	if r.Code != http.StatusOK {
		t.Fatalf("retry: %s", r)
	}
	r.JSON(&again)
	if again.Tenant.ID != made.Tenant.ID {
		t.Fatalf("a retry made a second tenant: %s and %s", made.Tenant.ID, again.Tenant.ID)
	}
	if got := spacesNamed(t, a, "Аналитика"); len(got) != 1 || got[0].OrgID != a.org {
		t.Fatalf("spaces named so: %+v", got)
	}
	// The same key in another organization is not this request.
	other := create(t, a.anna, "Beta").Org.ID
	if r := a.anna.Do("POST", "/api/orgs/"+other+"/tenants", map[string]any{"name": "Аналитика", "requestId": "anna-press-1"}); r.Code != 409 || r.Detail("reason") != "request_reused" {
		t.Fatalf("a key of another organization: %s", r)
	}

	// The organization lists it, the author administers it, and a team and a machine go into it at once.
	found := false
	for _, tv := range tenantsOf(t, a) {
		found = found || tv.ID == made.Tenant.ID
	}
	if !found {
		t.Fatalf("the organization does not list %s", made.Tenant.ID)
	}
	var v view
	a.anna.Do("GET", a.url(""), nil).JSON(&v)
	if c := v.Me.Scopes["tenant:"+made.Tenant.ID]; !c.Manage {
		t.Errorf("the author's rights in the tenant: %+v", c)
	}
	team := a.team(t, a.anna, made.Tenant.ID, "Данные")
	if team.TenantID != made.Tenant.ID {
		t.Errorf("team = %+v", team)
	}
	m := a.anna.Machine(made.Tenant.ID, nil)
	mach, err := a.s.DB.S().MachineByID(t.Context(), m.MachineID)
	if err != nil || mach.SpaceID != made.Tenant.ID {
		t.Errorf("machine = %+v, %v", mach, err)
	}

	// The journal has the creation, in the organization.
	ev, err := a.s.DB.S().Audit(t.Context(), store.AuditFilter{OrgID: a.org, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	creates := 0
	for _, e := range ev {
		if e.Action == "tenant.create" && e.TargetID == made.Tenant.ID {
			creates++
		}
	}
	if creates != 1 {
		t.Errorf("tenant.create records: %d", creates)
	}
}

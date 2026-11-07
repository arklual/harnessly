package orgs_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Deleting an empty tenant (GitHub issue 28).

type deletePlan struct {
	DryRun  bool `json:"dry_run"`
	Deleted bool
	Plan    orgs.DeletePlan
}

func kinds(bs []orgs.Blocker) map[string]int {
	out := map[string]int{}
	for _, b := range bs {
		out[b.Kind] = b.Count
	}
	return out
}

func TestDeleteTenantRefusedWhileNotEmpty(t *testing.T) {
	a := newAcme(t)
	a.team(t, a.anna, a.tenant, "Платформа-1")
	a.anna.Machine(a.tenant, nil)
	path := a.url("/tenants/" + a.tenant)

	r := a.anna.Do("DELETE", path+"?dry_run=true", nil)
	var dry deletePlan
	r.JSON(&dry)
	k := kinds(dry.Plan.Blockers)
	if r.Code != 200 || dry.Plan.CanDelete || k["machines"] != 1 || k["members"] != 2 || k["teams"] != 1 {
		t.Fatalf("dry run: %s", r)
	}
	r = a.anna.Do("DELETE", path, nil)
	if r.Code != 409 || r.Detail("reason") != "tenant_not_empty" {
		t.Fatalf("delete: %s", r)
	}
	if _, err := a.s.DB.S().SpaceByID(t.Context(), a.tenant); err != nil {
		t.Fatalf("the tenant is gone: %v", err)
	}
	if ms, _ := a.s.DB.S().MachinesBySpace(t.Context(), a.tenant); len(ms) != 1 {
		t.Fatalf("machines = %d", len(ms))
	}
}

func TestDeleteTenantRights(t *testing.T) {
	a := newAcme(t)
	outsider := a.s.SignIn("olga@example.com")
	if r := outsider.Do("DELETE", a.url("/tenants/"+a.tenant), nil); r.Code != 404 {
		t.Fatalf("outsider: %s", r)
	}
	if r := a.vera.Do("DELETE", a.url("/tenants/"+a.tenant), nil); r.Code != 403 {
		t.Fatalf("member: %s", r)
	}
	// A tenant of another organization is not found under this one.
	other := create(t, a.anna, "Beta").Org.ID
	if r := a.anna.Do("DELETE", "/api/orgs/"+other+"/tenants/"+a.tenant, nil); r.Code != 404 {
		t.Fatalf("wrong organization: %s", r)
	}
}

func TestDeleteEmptyTenant(t *testing.T) {
	a := newAcme(t)
	r := a.anna.Do("POST", a.url("/tenants"), map[string]any{"name": "Лишний", "requestId": "anna-press-9"})
	var made madeTenant
	r.JSON(&made)
	id := made.Tenant.ID
	path := a.url("/tenants/" + id)

	// A machine that was revoked does not keep the tenant, and keeps its row.
	m := a.anna.Machine(id, nil)
	if r := a.anna.Do("DELETE", "/api/machines/"+m.MachineID, nil); r.Code != 204 {
		t.Fatalf("revoke: %s", r)
	}

	r = a.anna.Do("DELETE", path+"?dry_run=1", nil)
	var dry deletePlan
	r.JSON(&dry)
	if r.Code != 200 || !dry.DryRun || !dry.Plan.CanDelete || kinds(dry.Plan.Removes)["revoked_machines_detached"] != 1 {
		t.Fatalf("dry run: %s", r)
	}
	if _, err := a.s.DB.S().SpaceByID(t.Context(), id); err != nil {
		t.Fatalf("the dry run deleted it: %v", err)
	}

	r = a.anna.Do("DELETE", path, nil)
	if r.Code != 200 {
		t.Fatalf("delete: %s", r)
	}
	if _, err := a.s.DB.S().SpaceByID(t.Context(), id); !store.IsNotFound(err) {
		t.Fatalf("the tenant is still there: %v", err)
	}
	row, err := a.s.DB.S().MachineByID(t.Context(), m.MachineID)
	if err != nil || row.RevokedAt == nil || row.SpaceID != "" {
		t.Fatalf("the revoked machine = %+v, %v", row, err)
	}
	for _, tv := range tenantsOf(t, a) {
		if tv.ID == id {
			t.Fatalf("the organization still lists it")
		}
	}
	ev, err := a.s.DB.S().Audit(t.Context(), store.AuditFilter{OrgID: a.org, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ev {
		if e.Action == "tenant.delete" && e.TargetID == id {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("tenant.delete records: %d", n)
	}
	// Once more: it is gone.
	if r := a.anna.Do("DELETE", path, nil); r.Code != 404 {
		t.Fatalf("again: %s", r)
	}
}

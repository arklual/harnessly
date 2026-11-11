package orgs_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

type view = orgs.View

func create(t *testing.T, p *hubtest.Person, name string, spaceIDs ...string) view {
	t.Helper()
	if spaceIDs == nil {
		spaceIDs = []string{}
	}
	r := p.Do("POST", "/api/orgs", map[string]any{"name": name, "spaceIds": spaceIDs})
	if r.Code != http.StatusCreated {
		t.Fatalf("create org: %s", r)
	}
	var v view
	r.JSON(&v)
	return v
}

// The first account is the instance administrator; with HUB_ORGS=off nobody sees the endpoints.
func TestFlagOffHidesEverything(t *testing.T) {
	s := hubtest.New(t)
	admin := s.SignIn("admin@example.com")
	for _, c := range []struct{ method, path string }{{"GET", "/api/orgs"}, {"POST", "/api/orgs"}, {"GET", "/api/orgs/x"}} {
		if r := admin.Do(c.method, c.path, map[string]any{"name": "A", "spaceIds": []string{}}); r.Code != 404 || r.Err() != "not_found" {
			t.Errorf("%s %s with off: %s", c.method, c.path, r)
		}
	}
}

// An instance administrator has no view of their own: they list the organizations they have something in.
func TestInstanceAdminListsOnlyTheirOrganizations(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	admin := s.SignIn("admin@example.com")
	anna := s.SignIn("anna@example.com")
	v := create(t, anna, "Acme")
	if v.Org.Slug != "acme" || v.Org.Timezone != "Europe/Moscow" || v.Org.Currency != "RUB" {
		t.Errorf("org = %+v", v.Org)
	}
	var list struct {
		Orgs []struct{ ID, Role string }
	}
	admin.Do("GET", "/api/orgs", nil).JSON(&list)
	if len(list.Orgs) != 0 {
		t.Errorf("the administrator's list = %+v", list)
	}
	anna.Do("GET", "/api/orgs", nil).JSON(&list)
	if len(list.Orgs) != 1 || list.Orgs[0].ID != v.Org.ID || list.Orgs[0].Role != "admin" {
		t.Errorf("the author's list = %+v", list)
	}
}

func TestCreateMovesOwnSpacesOnly(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com") // the first account administers the instance
	anna := s.SignIn("anna@example.com")
	boris := s.SignIn("boris@example.com")
	mine, _ := anna.NewSpace("Платформа")
	joined, _ := boris.NewSpace("Чужое")
	boris.Join(joined, anna) // anna is a member, not the owner

	r := anna.Do("POST", "/api/orgs", map[string]any{"name": "Acme", "spaceIds": []string{mine, joined}})
	if r.Code != 403 || r.Err() != "forbidden_scope" || r.Detail("spaceId") != joined {
		t.Fatalf("someone else's space: %s", r)
	}
	// Nothing was made: neither the organization nor the move.
	var list struct {
		Orgs        []struct{ ID string }
		LooseSpaces []struct{ ID string }
	}
	anna.Do("GET", "/api/orgs", nil).JSON(&list)
	if len(list.Orgs) != 0 || len(list.LooseSpaces) != 2 {
		t.Fatalf("after the refusal: %+v", list)
	}

	// A machine of the space stays online through the move.
	a := anna.Machine(mine, nil)
	v := create(t, anna, "Acme", mine)
	if len(v.Tenants) != 1 || v.Tenants[0].ID != mine || v.Tenants[0].OrgID != v.Org.ID || v.Tenants[0].MemberCount != 1 {
		t.Errorf("tenants = %+v", v.Tenants)
	}
	if len(v.Me.Grants) != 1 || v.Me.Grants[0].Role != "admin" || v.Me.Grants[0].Scope.Kind != "org" {
		t.Errorf("grants = %+v", v.Me.Grants)
	}
	if c, ok := v.Me.Scopes["org:"+v.Org.ID]; !ok || !c.Manage || !c.SetLimit {
		t.Errorf("capabilities at the org = %+v", c)
	}
	var m struct{ Machine struct{ Status string } }
	if r := anna.Do("GET", "/api/machines/"+a.MachineID, nil); r.Code != 200 {
		t.Fatalf("machine after the move: %s", r)
	} else if r.JSON(&m); m.Machine.Status != "online" {
		t.Errorf("machine after the move is %q", m.Machine.Status)
	}
	// A second organization of the same name gets the next slug; a moved space cannot be moved by create again.
	if v2 := create(t, anna, "Acme"); v2.Org.Slug != "acme-2" {
		t.Errorf("second slug = %q", v2.Org.Slug)
	}
	if r := anna.Do("POST", "/api/orgs", map[string]any{"name": "Again", "spaceIds": []string{mine}}); r.Code != 403 {
		t.Errorf("a space already in an organization: %s", r)
	}

	// The journal has the creation and the move, in the organization.
	ev, err := s.DB.S().Audit(t.Context(), store.AuditFilter{OrgID: v.Org.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range ev {
		got[e.Action] = true
	}
	if !got["org.create"] || !got["tenant.move"] {
		t.Errorf("journal = %+v", ev)
	}
}

func TestViewByRole(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com") // the first account administers the instance
	anna := s.SignIn("anna@example.com")
	vera := s.SignIn("vera@example.com")
	stranger := s.SignIn("gleb@example.com")
	sp, _ := anna.NewSpace("Платформа")
	other, _ := anna.NewSpace("Продукт")
	anna.Join(sp, vera)
	v := create(t, anna, "Acme", sp, other)

	if r := stranger.Do("GET", "/api/orgs/"+v.Org.ID, nil); r.Code != 404 {
		t.Errorf("a stranger: %s", r)
	}
	var mine view
	if r := vera.Do("GET", "/api/orgs/"+v.Org.Slug, nil); r.Code != 200 {
		t.Fatalf("a member by slug: %s", r)
	} else {
		r.JSON(&mine)
	}
	if len(mine.Tenants) != 1 || mine.Tenants[0].ID != sp {
		t.Errorf("a member sees tenants %+v", mine.Tenants)
	}
	if len(mine.Me.Scopes) != 0 {
		t.Errorf("a member manages %+v", mine.Me.Scopes)
	}
	var list struct {
		Orgs []struct{ ID, Role string }
	}
	vera.Do("GET", "/api/orgs", nil).JSON(&list)
	if len(list.Orgs) != 1 || list.Orgs[0].Role != "member" {
		t.Errorf("member's list = %+v", list)
	}
	// Only the organization's admin edits it.
	if r := vera.Do("PATCH", "/api/orgs/"+v.Org.ID, map[string]any{"name": "Mine"}); r.Code != 403 || r.Err() != "forbidden_scope" {
		t.Errorf("a member edits: %s", r)
	}
	if r := anna.Do("PATCH", "/api/orgs/"+v.Org.ID, map[string]any{"timezone": "Mars/Base"}); r.Code != 400 {
		t.Errorf("bad zone: %s", r)
	}
	var patched struct{ Org orgs.OrgView }
	if r := anna.Do("PATCH", "/api/orgs/"+v.Org.ID, map[string]any{"timezone": "Asia/Yekaterinburg", "currency": "USD"}); r.Code != 200 {
		t.Fatalf("patch: %s", r)
	} else if r.JSON(&patched); patched.Org.Timezone != "Asia/Yekaterinburg" || patched.Org.Currency != "USD" {
		t.Errorf("patched = %+v", patched.Org)
	}
}

func TestMoveTenant(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com") // the first account administers the instance
	anna := s.SignIn("anna@example.com")
	boris := s.SignIn("boris@example.com")
	a1, _ := anna.NewSpace("Один")
	a2, _ := anna.NewSpace("Два")
	b1, _ := boris.NewSpace("Борин")
	acme := create(t, anna, "Acme", a1)
	beta := create(t, anna, "Beta")
	bor := create(t, boris, "Bor")

	// Someone else's loose space: no.
	if r := anna.Do("POST", "/api/orgs/"+acme.Org.ID+"/tenants/move", map[string]any{"spaceId": b1}); r.Code != 403 || r.Detail("needs") != "tenant_admin" {
		t.Errorf("someone else's space: %s", r)
	}
	// One's own loose space: yes.
	if r := anna.Do("POST", "/api/orgs/"+acme.Org.ID+"/tenants/move", map[string]any{"spaceId": a2}); r.Code != 200 {
		t.Errorf("own space: %s", r)
	}
	// Between two organizations one administers both of: yes, journalled in both.
	if r := anna.Do("POST", "/api/orgs/"+beta.Org.ID+"/tenants/move", map[string]any{"spaceId": a2}); r.Code != 200 {
		t.Errorf("between own orgs: %s", r)
	}
	for _, org := range []string{acme.Org.ID, beta.Org.ID} {
		ev, err := s.DB.S().Audit(t.Context(), store.AuditFilter{OrgID: org, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range ev {
			if e.Action == "tenant.move" && e.TargetID == a2 {
				n++
			}
		}
		if n == 0 {
			t.Errorf("no tenant.move of the space in %s: %+v", org, ev)
		}
	}
	// Out of an organization one does not administer: no, even as its space's owner.
	if r := boris.Do("POST", "/api/orgs/"+bor.Org.ID+"/tenants/move", map[string]any{"spaceId": a1}); r.Code != 403 {
		t.Errorf("out of someone else's org: %s", r)
	}
	// Into an organization one does not administer: not found (nothing in it).
	if r := boris.Do("POST", "/api/orgs/"+acme.Org.ID+"/tenants/move", map[string]any{"spaceId": b1}); r.Code != 404 {
		t.Errorf("into someone else's org: %s", r)
	}
}

// an organization made with a tenant name has its first tenant at once, the creator owns it, and a blank name
// gets the default; without the field the organization is made as before.
func TestCreateMakesTheFirstTenant(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	anna := s.SignIn("anna@example.com")

	r := anna.Do("POST", "/api/orgs", map[string]any{"name": "Acme", "tenantName": "Продажи"})
	if r.Code != http.StatusCreated {
		t.Fatalf("create: %s", r)
	}
	var v view
	r.JSON(&v)
	if len(v.Tenants) != 1 || v.Tenants[0].Name != "Продажи" {
		t.Fatalf("tenants = %+v", v.Tenants)
	}
	if r := anna.Do("GET", "/api/spaces/"+v.Tenants[0].ID+"/machines", nil); r.Code != 200 {
		t.Errorf("the creator does not own the first tenant: %s", r)
	}

	r = anna.Do("POST", "/api/orgs", map[string]any{"name": "Beta", "tenantName": "  "})
	if r.Code != http.StatusCreated {
		t.Fatalf("create blank: %s", r)
	}
	r.JSON(&v)
	if len(v.Tenants) != 1 || v.Tenants[0].Name != "Основное" {
		t.Errorf("default tenant = %+v", v.Tenants)
	}

	if v := create(t, anna, "Gamma"); len(v.Tenants) != 0 {
		t.Errorf("without tenantName an organization has tenants: %+v", v.Tenants)
	}
}

// The number of organizations one person may create is the hub's setting.
func TestOrgCapIsConfigured(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"), hubtest.Env("HUB_MAX_ORGS_PER_PERSON", "2"))
	anna := s.SignIn("anna@example.com")
	create(t, anna, "First")
	create(t, anna, "Second")
	if r := anna.Do("POST", "/api/orgs", map[string]any{"name": "Third", "spaceIds": []string{}}); r.Code != http.StatusConflict || !strings.Contains(string(r.Body), "org_limit") {
		t.Errorf("over the configured cap: %s", r)
	}
	// another person's count is their own
	create(t, s.SignIn("boris@example.com"), "Fourth")
}

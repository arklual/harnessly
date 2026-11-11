package orgs_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// Every route of one organization answers a person of another organization that it does not exist, by id and by slug,
// before it reads or changes anything.
func TestOrgRoutesRefuseOtherOrganizations(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com")
	anna, bob := s.SignIn("anna@example.com"), s.SignIn("bob@example.com")
	annaSpace, _ := anna.NewSpace("Anna")
	bobSpace, _ := bob.NewSpace("Bob")
	acme := create(t, anna, "Acme", annaSpace)
	create(t, bob, "Beta", bobSpace)
	for _, ref := range []string{acme.Org.ID, acme.Org.Slug} {
		o := "/api/orgs/" + ref
		for _, rt := range []struct {
			method, path string
			body         any
		}{
			{"GET", o, nil},
			{"PATCH", o, map[string]any{"name": "Чужая"}},
			{"POST", o + "/tenants", map[string]any{"name": "Чужой"}},
			{"POST", o + "/tenants/move", map[string]any{"spaceId": bobSpace}},
			{"DELETE", o + "/tenants/" + annaSpace, nil},
			{"GET", o + "/limits", nil},
			{"PUT", o + "/limits", map[string]any{}},
			{"DELETE", o + "/limits/r1", nil},
			{"GET", o + "/events", nil},
			{"GET", o + "/teams", nil},
			{"POST", o + "/teams", map[string]any{"name": "Чужая", "tenantId": annaSpace}},
			{"PATCH", o + "/teams/t1", map[string]any{"name": "x"}},
			{"DELETE", o + "/teams/t1", nil},
			{"GET", o + "/scopes/tenant/" + annaSpace + "/members", nil},
			{"PUT", o + "/teams/t1/members/" + bob.ID, map[string]any{}},
			{"DELETE", o + "/teams/t1/members/" + bob.ID, nil},
			{"PUT", o + "/grants", map[string]any{"userId": bob.ID, "scope": map[string]string{"kind": "org", "id": acme.Org.ID}, "role": "admin"}},
			{"GET", o + "/audit", nil},
			{"GET", o + "/features", nil},
			{"PUT", o + "/features/efficiency", map[string]any{"enabled": true}},
			{"GET", o + "/efficiency/summary", nil},
			{"GET", o + "/efficiency/pulse", nil},
			{"POST", o + "/efficiency/pulse", map[string]any{"speed": 3, "quality": 3, "friction": 3}},
		} {
			if r := bob.Do(rt.method, rt.path, rt.body); r.Code != 404 {
				t.Errorf("another organization %s %s: %s", rt.method, rt.path, r)
			}
		}
	}
	var got struct{ Org struct{ Name string } }
	if r := anna.Do("GET", "/api/orgs/"+acme.Org.ID, nil); r.Code != 200 {
		t.Fatalf("anna reads her organization: %s", r)
	} else {
		r.JSON(&got)
	}
	if got.Org.Name != "Acme" {
		t.Errorf("name = %q", got.Org.Name)
	}
	if r := bob.Do("GET", "/api/spaces/"+annaSpace, nil); r.Code != 403 {
		t.Errorf("the space stays anna's: %s", r)
	}
}

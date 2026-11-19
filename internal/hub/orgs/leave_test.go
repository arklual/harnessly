package orgs_test

import "testing"

func TestLeaveOrg(t *testing.T) {
	a := newAcme(t)
	team := a.team(t, a.anna, a.tenant, "Ядро")
	a.anna.Do("PUT", a.url("/teams/"+team.ID+"/members/"+a.vera.ID), map[string]any{})

	// The only administrator and owner cannot leave: nobody would manage the organization.
	if r := a.anna.Do("POST", a.url("/leave"), map[string]any{}); r.Code != 409 || r.Detail("reason") != "last_admin" {
		t.Errorf("last admin leaves: %s", r)
	}
	// A member leaves: out of the tenant and its teams, and the organization is gone from their view.
	if r := a.vera.Do("POST", a.url("/leave"), map[string]any{}); r.Code != 204 {
		t.Fatalf("vera leaves: %s", r)
	}
	if role, _ := a.s.DB.S().Role(t.Context(), a.tenant, a.vera.ID); role != "" {
		t.Errorf("vera still in the tenant: %q", role)
	}
	if teams, _ := a.s.DB.S().TeamsOfUser(t.Context(), a.vera.ID); len(teams) != 0 {
		t.Errorf("vera still in teams: %+v", teams)
	}
	if orgs, _ := a.s.DB.S().OrgsForUser(t.Context(), a.vera.ID); len(orgs) != 0 {
		t.Errorf("vera still sees %+v", orgs)
	}
	if r := a.vera.Do("POST", a.url("/leave"), map[string]any{}); r.Code < 400 || r.Code == 500 {
		t.Errorf("leaving twice: %s", r)
	}
	if evs, _ := a.audit(t, a.anna, ""); actions(evs)["member.leave"] != 1 {
		t.Errorf("audit: %v", actions(evs))
	}
}

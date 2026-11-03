package orgs_test

import (
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// accessCase: Acme's tenant with a machine of Anna's, the access list enforced.
type accessCase struct {
	veraCase
	machine string
	dan     *hubtest.Person // a member of the tenant, in no team
}

func newAccess(t *testing.T, mode string) accessCase {
	t.Helper()
	v := newVera(t, hubtest.Env("HUB_RUNTIME_DOMAIN", "run.example.org"), hubtest.Env("HUB_ACCESS_ENFORCE", mode))
	a := v.s.NewAgent(nil)
	if r := a.Register(v.anna.NewCode(v.tenant, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	a.Connect()
	ac := accessCase{veraCase: v, machine: a.MachineID, dan: v.s.SignIn("dan@example.com")}
	v.anna.Join(v.tenant, ac.dan)
	return ac
}

func (ac accessCase) share(t *testing.T, who *hubtest.Person, entries ...map[string]string) {
	t.Helper()
	if entries == nil {
		entries = []map[string]string{}
	}
	if r := who.Do("PATCH", "/api/machines/"+ac.machine, map[string]any{"access": entries}); r.Code != http.StatusOK {
		t.Fatalf("share: %s", r)
	}
}

func (ac accessCase) open(p *hubtest.Person) hubtest.Resp {
	return p.Do("GET", "/api/machines/"+ac.machine+"/open", nil)
}

// The four modes of the machine card, and who passes in each.
func TestMachineAccessModes(t *testing.T) {
	ac := newAccess(t, "on")
	user := func(p *hubtest.Person) map[string]string { return map[string]string{"kind": "user", "id": p.ID} }
	team := map[string]string{"kind": "team", "id": ac.team.ID}
	type want struct{ anna, boris, vera, dan bool } // anna: owner of the tenant and creator; boris: lead of a team the machine is not in; vera: member of that team; dan: tenant member
	cases := []struct {
		name    string
		entries []map[string]string
		want    want
	}{
		{"tenant (empty list)", nil, want{true, true, true, true}},
		{"owner (only the creator)", []map[string]string{user(ac.anna)}, want{true, false, false, false}},
		{"team", []map[string]string{team}, want{true, true, true, false}},
		{"pick", []map[string]string{user(ac.dan)}, want{true, false, false, true}},
		{"pick with a team", []map[string]string{user(ac.dan), team}, want{true, true, true, true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ac.share(t, ac.anna, c.entries...)
			// the proxy's belief is per request in this path: the open endpoint asks every time
			for who, p := range map[string]*hubtest.Person{"anna": ac.anna, "boris": ac.boris, "vera": ac.vera, "dan": ac.dan} {
				ok := map[string]bool{"anna": c.want.anna, "boris": c.want.boris, "vera": c.want.vera, "dan": c.want.dan}[who]
				r := ac.open(p)
				if ok && r.Code != http.StatusOK {
					t.Errorf("%s should pass: %s", who, r)
				}
				if !ok && (r.Code != http.StatusForbidden || r.Err() != "machine_access") {
					t.Errorf("%s should be refused: %s", who, r)
				}
			}
		})
	}
}

// The tenant's manager and the instance administrator pass whatever the list says; the lead of the machine's team does
// too, once the machine belongs to the team.
func TestMachineAccessManagersAlwaysPass(t *testing.T) {
	ac := newAccess(t, "on")
	root := ac.s.SignIn("root@example.com")
	ac.anna.Join(ac.tenant, root)
	other := ac.s.SignIn("eve@example.com")
	ac.anna.Join(ac.tenant, other)
	if r := ac.anna.Do("PATCH", "/api/machines/"+ac.machine, map[string]any{"teamId": ac.team.ID}); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	ac.share(t, ac.anna, map[string]string{"kind": "user", "id": ac.dan.ID})
	if r := ac.open(root); r.Code != http.StatusOK {
		t.Errorf("an instance administrator: %s", r)
	}
	if r := ac.open(other); r.Code != http.StatusForbidden || r.Err() != "machine_access" {
		t.Errorf("a member outside the list: %s", r)
	}
	if r := ac.open(ac.boris); r.Code != http.StatusOK {
		t.Errorf("the machine team's lead: %s", r)
	}
}

func TestMachineAccessWarnAndOff(t *testing.T) {
	for _, mode := range []string{"warn", "off"} {
		t.Run(mode, func(t *testing.T) {
			ac := newAccess(t, mode)
			ac.share(t, ac.anna, map[string]string{"kind": "user", "id": ac.anna.ID})
			if r := ac.open(ac.dan); r.Code != http.StatusOK {
				t.Errorf("%s: %s", mode, r)
			}
		})
	}
}

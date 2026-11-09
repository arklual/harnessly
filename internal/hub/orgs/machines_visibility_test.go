package orgs_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// seen says whether any list the person can read names the machine: the tenant's list, the organization's inventory and
// the fleet.
func (ac accessCase) seen(t *testing.T, p *hubtest.Person) (inList, inOrg, inFleet bool) {
	t.Helper()
	r := p.Do("GET", "/api/spaces/"+ac.tenant+"/machines", nil)
	if r.Code != http.StatusOK {
		t.Fatalf("list: %s", r)
	}
	inList = strings.Contains(string(r.Body), ac.machine)
	if r = p.Do("GET", ac.url("/machines"), nil); r.Code == http.StatusOK {
		inOrg = strings.Contains(string(r.Body), ac.machine)
	}
	if r = p.Do("GET", "/api/me/machines", nil); r.Code == http.StatusOK {
		inFleet = strings.Contains(string(r.Body), ac.machine)
	}
	return
}

// With the list enforced, a member outside it neither finds the machine in a list, nor opens its card, nor hears its
// live events, and a person taken off the list stops hearing on a stream that is already open.
func TestMachineVisibilityEnforced(t *testing.T) {
	ac := newAccess(t, "on")
	root := ac.s.SignIn("root@example.com")
	ac.anna.Join(ac.tenant, root)
	ac.share(t, ac.anna, map[string]string{"kind": "user", "id": ac.anna.ID}) // private

	for name, p := range map[string]*hubtest.Person{"dan": ac.dan, "vera": ac.vera} {
		if a, b, c := ac.seen(t, p); a || b || c {
			t.Errorf("%s sees a private machine: list=%v org=%v fleet=%v", name, a, b, c)
		}
		denied := p.Do("GET", "/api/machines/"+ac.machine, nil)
		gone := p.Do("GET", "/api/machines/m_nonexistent", nil)
		if denied.Code != http.StatusNotFound && denied.Code != gone.Code {
			t.Errorf("%s: detail of a private machine: %s", name, denied)
		}
		if strings.Contains(string(denied.Body), ac.machine) {
			t.Errorf("%s: detail leaks the machine: %s", name, denied)
		}
	}
	for name, p := range map[string]*hubtest.Person{"anna": ac.anna, "root": root} {
		if a, b, _ := ac.seen(t, p); !a || !b {
			t.Errorf("%s should see the machine: list=%v org=%v", name, a, b)
		}
		if r := p.Do("GET", "/api/machines/"+ac.machine, nil); r.Code != http.StatusOK {
			t.Errorf("%s: detail: %s", name, r)
		}
	}

	// Reassignment: Dan is put on the list, sees it and hears it; then he is taken off while his stream is open.
	dan := ac.dan.Events(ac.tenant)
	vera := ac.vera.Events(ac.tenant)
	ac.share(t, ac.anna, map[string]string{"kind": "user", "id": ac.dan.ID})
	if a, _, _ := ac.seen(t, ac.dan); !a {
		t.Fatal("dan, on the list, does not see the machine")
	}
	rename := func(name string) {
		t.Helper()
		if r := ac.anna.Do("PATCH", "/api/machines/"+ac.machine, map[string]any{"name": name}); r.Code != http.StatusOK {
			t.Fatal(r)
		}
	}
	rename("Shared")
	dan.Await("the machine event for dan", func(e hubtest.Event) bool { return e.Name == "machine" })

	ac.share(t, ac.anna, map[string]string{"kind": "user", "id": ac.anna.ID})
	before := len(dan.Named("machine"))
	rename("Private again")
	// a non-machine event still reaches both, and arrives after the machine events published before it
	newcomer := ac.s.SignIn("eve@example.com")
	ac.anna.Join(ac.tenant, newcomer)
	dan.Await("members for dan", func(e hubtest.Event) bool { return e.Name == "members" })
	vera.Await("members for vera", func(e hubtest.Event) bool { return e.Name == "members" })
	if n := len(dan.Named("machine")); n > before+1 { // the revoking PATCH may have been delivered before the revoke was visible
		t.Errorf("dan still hears the machine after the revoke: %d events, %d before", n, before)
	}
	for _, e := range dan.Named("machine") {
		if strings.Contains(string(e.Data), "Private again") {
			t.Errorf("dan got metadata after the revoke: %s", e.Data)
		}
	}
	if n := len(vera.Named("machine")); n != 0 {
		t.Errorf("vera, never on the list, got %d machine events", n)
	}
	if a, _, _ := ac.seen(t, ac.dan); a {
		t.Error("dan, taken off the list, still sees the machine")
	}
}

// Off and warn keep showing every machine to every member.
func TestMachineVisibilityOffAndWarn(t *testing.T) {
	for _, mode := range []string{"off", "warn"} {
		t.Run(mode, func(t *testing.T) {
			ac := newAccess(t, mode)
			ac.share(t, ac.anna, map[string]string{"kind": "user", "id": ac.anna.ID})
			// the organization's inventory is for its managers, so only the tenant's list is compared here
			if a, _, _ := ac.seen(t, ac.dan); !a {
				t.Errorf("%s: dan does not see the machine in the tenant list", mode)
			}
			if r := ac.dan.Do("GET", "/api/machines/"+ac.machine, nil); r.Code != http.StatusOK {
				t.Errorf("%s: detail: %s", mode, r)
			}
			dan := ac.dan.Events(ac.tenant)
			if r := ac.anna.Do("PATCH", "/api/machines/"+ac.machine, map[string]any{"name": "X"}); r.Code != http.StatusOK {
				t.Fatal(r)
			}
			dan.Await("machine event", func(e hubtest.Event) bool { return e.Name == "machine" })
		})
	}
}

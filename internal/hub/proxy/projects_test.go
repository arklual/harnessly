package proxy_test

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

type projectRows struct {
	Projects []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		MachineID string `json:"machineId"`
		Asleep    bool   `json:"asleep"`
		Label     string `json:"label"`
	} `json:"projects"`
}

func (f *fixture) putProject(t *testing.T, machine, id, name string) {
	t.Helper()
	if err := f.s.DB.S().PutMachineProject(context.Background(), store.MachineProject{MachineID: machine, ID: id, Name: name, Directories: []string{"/work/" + name}}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectsAreDarkWithoutTheSwitch(t *testing.T) {
	f := setup(t, fleetOn)
	if r := f.owner.Do("GET", "/api/me/projects", nil); r.Code != 404 || r.Err() != "projects_off" {
		t.Errorf("switch off: %s", r)
	}
}

// A machine that sleeps keeps its projects in the list, marked; a machine closed to the person gives none.
func TestProjectsOfASleepingMachineAreLabelledAndClosedOnesAreHidden(t *testing.T) {
	f := setup(t, fleetOn, orgsOn, hubtest.Env("HUB_ACCESS_ENFORCE", "on"))
	if err := f.s.Hub.Switches.Set(context.Background(), "hub-projects", "*", "*", true, "", "test"); err != nil {
		t.Fatal(err)
	}
	listed, other := f.s.SignIn("listed@example.com"), f.s.SignIn("other@example.com")
	f.owner.Join(f.spaceID, listed)
	f.owner.Join(f.spaceID, other)
	f.share(t, listed.ID)
	f.putProject(t, f.id, "p1", "alpha")

	var l projectRows
	r := listed.Do("GET", "/api/me/projects", nil)
	r.JSON(&l)
	if r.Code != 200 || len(l.Projects) != 1 || l.Projects[0].Name != "alpha" || l.Projects[0].Asleep {
		t.Fatalf("the person it is open to, machine on: %s", r)
	}

	var o projectRows
	r = other.Do("GET", "/api/me/projects", nil)
	r.JSON(&o)
	if r.Code != 200 || len(o.Projects) != 0 {
		t.Errorf("a machine closed to the person leaks projects: %s", r)
	}

	f.a.Close()
	f.s.Eventually("the machine sleeps", func() bool {
		var s projectRows
		x := listed.Do("GET", "/api/me/projects", nil)
		x.JSON(&s)
		return len(s.Projects) == 1 && s.Projects[0].Asleep && s.Projects[0].Label == "машина спит"
	})
	o = projectRows{}
	r = other.Do("GET", "/api/me/projects", nil)
	r.JSON(&o)
	if len(o.Projects) != 0 {
		t.Errorf("a sleeping closed machine leaks projects: %s", r)
	}
}

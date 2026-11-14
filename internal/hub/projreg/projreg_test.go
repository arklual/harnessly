package projreg_test

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/projreg"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestApplyKeepsTheRegistryOfAMachine(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	a := owner.Machine(space, nil)
	svc := &projreg.Service{DB: s.DB, Now: time.Now}
	ctx := context.Background()
	list := func() map[string]string {
		got, err := s.DB.S().MachineProjectsOf(ctx, []string{a.MachineID})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, p := range got[a.MachineID] {
			out[p.ID] = p.Name
		}
		return out
	}
	apply := func(e hubproto.ProjectsEvent) {
		t.Helper()
		if err := svc.Apply(ctx, a.MachineID, e); err != nil {
			t.Fatal(err)
		}
	}
	apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsFull, Projects: []hubproto.Project{{ID: "a", Name: "one"}, {ID: "b", Name: "two"}}})
	if g := list(); len(g) != 2 || g["a"] != "one" {
		t.Fatalf("full list: %v", g)
	}
	apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsRenamed, Project: &hubproto.Project{ID: "a", Name: "uno"}})
	apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsCreated, Project: &hubproto.Project{ID: "c", Name: "three", Directories: []string{"/w/c"}}})
	apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsDeleted, Project: &hubproto.Project{ID: "b"}})
	if g := list(); len(g) != 2 || g["a"] != "uno" || g["c"] != "three" {
		t.Fatalf("after events: %v", g)
	}
	apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsFull, Projects: []hubproto.Project{{ID: "z", Name: "only"}}})
	if g := list(); len(g) != 1 || g["z"] != "only" {
		t.Fatalf("a full list replaces: %v", g)
	}
}

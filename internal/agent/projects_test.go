package agent

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestProjectsCacheKeepsTheListForALateHub(t *testing.T) {
	var c projectsCache
	if _, ok := c.full(); ok {
		t.Fatal("a list nobody told is known")
	}
	c.apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsFull, Projects: []hubproto.Project{{ID: "a", Name: "one"}}})
	c.apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsCreated, Project: &hubproto.Project{ID: "b", Name: "two"}})
	c.apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsRenamed, Project: &hubproto.Project{ID: "a", Name: "uno"}})
	c.apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsDeleted, Project: &hubproto.Project{ID: "b"}})
	if c.apply(hubproto.ProjectsEvent{Kind: "nope"}) || c.apply(hubproto.ProjectsEvent{Kind: hubproto.ProjectsCreated}) {
		t.Error("a malformed event was taken")
	}
	e, ok := c.full()
	if !ok || e.Kind != hubproto.ProjectsFull || len(e.Projects) != 1 || e.Projects[0].Name != "uno" {
		t.Errorf("full list: %+v", e)
	}
}

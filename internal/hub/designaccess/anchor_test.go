package designaccess

import (
	"context"
	"testing"
)

// a hub route names a space and a project, not a machine; the machine that holds the project answers.
func TestHubRouteNeedsNoMachine(t *testing.T) {
	for rest, want := range map[string]string{
		"/api/ws/design/store/note-1/head":          "note-1",
		"/api/ws/design/store/note-1/k/ab/f/x.html": "note-1",
		"/api/ws/design/access/note-1/people":       "note-1",
		"/api/ws/design/store-library":              "",
		"/api/ws/design/store-library/changes":      "",
		"/api/ws/access/me":                         "",
		"/api/ws/chat/send":                         "!",
		"/api/ws/design/store-work-machine":         "",
	} {
		p, ok := HubRoute(rest)
		if want == "!" {
			if ok {
				t.Errorf("%s: a machine's route taken by the hub", rest)
			}
			continue
		}
		if !ok || p != want {
			t.Errorf("%s: project %q ok %v, want %q", rest, p, ok, want)
		}
	}
	ctx := context.Background()
	r := newSpaceRig(t, true)
	if done, err := r.svc.registerCreated(ctx, r.owner, "note-1", r.ownerU.Email); err != nil || !done {
		t.Fatalf("registerCreated: %v %v", done, err)
	}
	spaces := []string{r.owner.SpaceID}
	if got := r.svc.AnchorMachines(ctx, spaces, "/api/ws/design/store/note-1/head"); len(got) != 1 || got[0] != r.owner.ID {
		t.Fatalf("anchor = %q, want the machine holding the project %q", got, r.owner.ID)
	}
	if got := r.svc.AnchorMachines(ctx, spaces, "/api/ws/design/store/other-9/head"); len(got) != 0 {
		t.Fatalf("anchor of an unknown project = %q", got)
	}
	if got := r.svc.AnchorMachines(ctx, nil, "/api/ws/design/store/note-1/head"); len(got) != 0 {
		t.Fatalf("anchor with no spaces = %q", got)
	}
}

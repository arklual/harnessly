package design

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClaimOwnerlessProject(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "free", "Free", "")
	makeProject(t, root, "owned", "Owned", otherV1)
	makeProject(t, root, "rowed", "Rowed", "")
	l := accessLibrary(t, root)
	var events []string
	l.emitFn = func(kind, project string) { events = append(events, kind+" "+project) }
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"rowed": "none"})); err != nil {
		t.Fatal(err)
	}
	a := pagesAPI(l)
	claim := func(id string) int {
		r := httptest.NewRequest(http.MethodPost, "/claim", strings.NewReader(`{"project":"`+id+`"}`))
		r.Header.Set("Content-Type", "application/json")
		return a.ClaimProject(r).Status
	}
	if c := claim("nothing"); c != 404 {
		t.Errorf("missing: %d", c)
	}
	if c := claim("owned"); c != 409 {
		t.Errorf("owned: %d", c)
	}
	if c := claim("rowed"); c != 409 {
		t.Errorf("registered: %d", c)
	}
	if len(events) != 0 {
		t.Fatalf("events on refusals: %v", events)
	}
	if c := claim("free"); c != 200 {
		t.Fatalf("free: %d", c)
	}
	o, ok := l.ReadOwner("free")
	if !ok || o.V != 1 || o.User != "se" || o.Private {
		t.Fatalf("owner file: %+v %v", o, ok)
	}
	if len(events) != 1 || events[0] != "design.claim free" {
		t.Fatalf("events: %v", events)
	}
	if c := claim("free"); c != 409 {
		t.Errorf("twice: %d", c)
	}
}

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
	// HRN-675: the file names the person (a v1 file named only the Unix user, so another runtime of the library could own it).
	if !ok || o.V != 2 || o.User != "se" || o.Private || !o.Claimed || o.Person != "p.smirnov@example.com" {
		t.Fatalf("owner file: %+v %v", o, ok)
	}
	if len(events) != 1 || events[0] != "design.claim free" {
		t.Fatalf("events: %v", events)
	}
	if c := claim("free"); c != 409 {
		t.Errorf("twice: %d", c)
	}
}

// HRN-675: «take it» from a page session makes that session's person the owner, not the person the machine belongs to;
// the claim is told to the gateway again (design.claim, the team keeps editing) while the snapshot has no row.
func TestClaimOwnerlessNamesThePersonOfTheSession(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "free", "Free", "")
	l := accessLibrary(t, root)
	var events []string
	l.emitFn = func(kind, project string) { events = append(events, kind+" "+project) }
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	a := pagesAPI(l)
	a.Person = func(*http.Request) string { return "ann.example@example.com" }
	r := httptest.NewRequest(http.MethodPost, "/claim", strings.NewReader(`{"project":"free"}`))
	r.Header.Set("Content-Type", "application/json")
	if c := a.ClaimProject(r).Status; c != 200 {
		t.Fatalf("claim: %d", c)
	}
	o, ok := l.ReadOwner("free")
	if !ok || o.Person != "ann.example@example.com" || o.Name != "ann.example" || o.User != "se" || !o.Claimed {
		t.Fatalf("owner file: %+v %v", o, ok)
	}
	// Not this runtime's person: the runtime does not resend the claim in their name.
	l.ClaimOnce("free", o, Identity{Runtime: "se", PersonID: "p.smirnov@example.com"})
	if len(events) != 1 || events[0] != "design.claim free" {
		t.Fatalf("events: %v", events)
	}
}

// A claimed v2 file of this runtime's person is resent as a claim once, never as design.created (which would make it personal).
func TestClaimedOwnerFileIsResentAsClaim(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "free", "Free", `{"v":2,"user":"se","name":"Пётр","person":"p.smirnov@example.com","private":false,"claimed":true}`)
	l := accessLibrary(t, root)
	var events []string
	l.emitFn = func(kind, project string) { events = append(events, kind+" "+project) }
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	o, _ := l.ReadOwner("free")
	who, _ := l.Identity()
	l.ClaimOnce("free", o, who)
	l.ClaimOnce("free", o, who)
	l.ReannounceOwn()
	if len(events) != 1 || events[0] != "design.claim free" {
		t.Fatalf("events: %v", events)
	}
}

func TestSetOwnerAsNamesTheMaker(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "made", "Made", "")
	l := accessLibrary(t, root)
	if err := l.SetOwnerAs("made", " Ann.Example@Example.com "); err != nil {
		t.Fatal(err)
	}
	o, ok := l.ReadOwner("made")
	if !ok || o.V != 2 || !o.Private || o.Claimed || o.Person != "ann.example@example.com" || o.User != "se" {
		t.Fatalf("owner file: %+v %v", o, ok)
	}
	// No address: the runtime's own person, as before.
	makeProject(t, root, "mine", "Mine", "")
	if err := l.SetOwnerAs("mine", "not-an-address"); err != nil {
		t.Fatal(err)
	}
	if o, _ := l.ReadOwner("mine"); o.Person != "p.smirnov@example.com" {
		t.Fatalf("fallback: %+v", o)
	}
}

package design

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A shared machine's snapshot speaks for each person on their own: one person's grant opens nothing to another, the
// machine's person keeps the top level, and someone the snapshot does not name sees only what nobody registered yet.
func TestSharedMachineGivesEachPersonTheirOwnSection(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"mine", "theirs", "team", "unregistered"} {
		makeProject(t, root, id, id, otherV2)
	}
	makeProject(t, root, "open", "open", "")
	l := accessLibrary(t, root)
	snap := snapshot(map[string]Grant{"mine": {Role: "owner", Owner: "p.smirnov@example.com", OwnerName: "Пётр"}},
		map[string]string{"mine": "none", "theirs": "none", "team": "view"})
	snap.People = map[string]PersonSnapshot{
		"o.ivanov@example.com": {
			Projects: map[string]Grant{"theirs": {Role: "owner", Owner: "o.ivanov@example.com", OwnerName: "Олег"}},
			Team:     map[string]string{"mine": "none", "theirs": "none", "team": "comment"},
		},
	}
	if err := snap.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := l.SetAccess(snap); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		person, id string
		want       Role
	}{
		{"", "mine", RoleOwner},
		{"p.smirnov@example.com", "mine", RoleOwner},
		{"P.Smirnov@Example.com", "mine", RoleOwner},
		{"", "theirs", RoleNone},
		{"o.ivanov@example.com", "theirs", RoleOwner},
		{"o.ivanov@example.com", "mine", RoleNone},
		{"o.ivanov@example.com", "team", RoleComment},
		{"", "team", RoleView},
		{"stranger@example.com", "mine", RoleNone},
		{"stranger@example.com", "theirs", RoleNone},
		{"stranger@example.com", "team", RoleNone},
		// What no snapshot names is as it was before the gateway: a private owner file of someone else is closed, a
		// project without one is open.
		{"stranger@example.com", "unregistered", RoleNone},
		{"stranger@example.com", "open", RoleEdit},
	}
	for _, c := range cases {
		if got := l.RoleFor(c.id, c.person); got != c.want {
			t.Errorf("%q on %s = %q, want %q", c.person, c.id, got, c.want)
		}
	}
	var seen []string
	for _, a := range l.VisibleToFor(l.ListArtifacts(), "o.ivanov@example.com") {
		seen = append(seen, a.ID)
	}
	if len(seen) != 3 {
		t.Errorf("the second person sees %v, want open, team and theirs", seen)
	}

	// The route's gate takes the person from the request's session.
	a := &API{L: l, Person: func(r *http.Request) string { return r.Header.Get("X-Test-Person") }}
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Test-Person", "o.ivanov@example.com")
	if no := a.gate(r, "theirs", RoleEdit); no != nil {
		t.Errorf("the owner of theirs was refused: %+v", no)
	}
	if no := a.gate(r, "mine", RoleView); no == nil || no.Status != http.StatusNotFound {
		t.Errorf("someone else's personal project: %+v", no)
	}
	if no := a.gate(httptest.NewRequest(http.MethodGet, "/x", nil), "theirs", RoleView); no == nil {
		t.Error("the machine's person reads a project only the second person was given")
	}
}

func TestSnapshotRefusesBadPeople(t *testing.T) {
	base := func() AccessSnapshot {
		return AccessSnapshot{Enforce: true, At: accessNow, Projects: map[string]Grant{}, Team: map[string]string{}}
	}
	for label, people := range map[string]map[string]PersonSnapshot{
		"upper case":  {"O.Ivanov@example.com": {}},
		"no at":       {"oleg": {}},
		"spaces":      {" o@example.com": {}},
		"bad role":    {"o@example.com": {Projects: map[string]Grant{"p": {Role: "boss"}}}},
		"bad project": {"o@example.com": {Team: map[string]string{"../x": "view"}}},
	} {
		s := base()
		s.People = people
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
	s := base()
	s.People = map[string]PersonSnapshot{"o@example.com": {Projects: map[string]Grant{"p": {Role: "view"}}, Team: map[string]string{"p": "none"}}}
	if err := s.Validate(); err != nil {
		t.Errorf("a good person refused: %v", err)
	}
	_ = time.Second
}

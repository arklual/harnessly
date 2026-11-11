package machines_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// After a person left a space, no request of theirs to what the space holds is answered, and the machine they
// registered there is revoked: its link closes and it cannot dial again. A machine given to another member before
// the person left stays.
func TestLeaverKeepsNothingOfTheSpace(t *testing.T) {
	s := hubtest.New(t)
	owner, guest := s.SignIn("owner@example.test"), s.SignIn("guest@example.test")
	space, _ := owner.NewSpace("North")
	owner.Join(space, guest)
	shared, laptop, given := owner.Machine(space, nil), owner.Machine(space, nil), owner.Machine(space, nil)
	give := func(a *hubtest.Agent, to *hubtest.Person) {
		t.Helper()
		if r := owner.Do("POST", "/api/machines/"+a.MachineID+"/owner", map[string]string{"userId": to.ID}); r.Code != http.StatusOK {
			t.Fatalf("giving the machine to %s: %s", to.Email, r)
		}
	}
	// two machines become the guest's own; one of them is given to another member before the guest leaves
	give(laptop, guest)
	give(given, guest)
	give(given, owner)

	paths := []string{
		"/api/spaces/" + space, "/api/spaces/" + space + "/members", "/api/spaces/" + space + "/machines",
		"/api/machines/" + shared.MachineID, "/api/machines/" + shared.MachineID + "/open",
		"/api/machines/" + laptop.MachineID, "/api/machines/" + laptop.MachineID + "/open",
		"/api/machines/" + given.MachineID,
	}
	for _, p := range paths[:4] {
		if r := guest.Do("GET", p, nil); r.Code != http.StatusOK {
			t.Fatalf("a member reads %s: %s", p, r)
		}
	}
	if r := owner.Do("DELETE", "/api/spaces/"+space+"/members/"+guest.ID, nil); r.Code != http.StatusNoContent {
		t.Fatalf("removing the member: %s", r)
	}
	for _, p := range paths {
		if r := guest.Do("GET", p, nil); r.Code != http.StatusForbidden && r.Code != http.StatusNotFound {
			t.Errorf("the leaver reads %s: %s", p, r)
		}
	}
	if r := guest.Do("POST", "/api/spaces/"+space+"/machines/codes", map[string]string{"profile": "dir"}); r.Code != http.StatusForbidden && r.Code != http.StatusNotFound {
		t.Errorf("the leaver asks for a registration code: %s", r)
	}

	if code, _ := laptop.TryConnect(); code == http.StatusSwitchingProtocols || code == http.StatusOK {
		t.Errorf("the leaver's machine dialed again: %d", code)
	}
	ctx := context.Background()
	for name, tc := range map[string]struct {
		id      string
		revoked bool
	}{"the leaver's machine": {laptop.MachineID, true}, "the machine given away": {given.MachineID, false}, "the shared machine": {shared.MachineID, false}} {
		m, err := s.DB.S().MachineByID(ctx, tc.id)
		if err != nil || (m.RevokedAt != nil) != tc.revoked {
			t.Errorf("%s: revoked %v (%v), want %v", name, m.RevokedAt != nil, err, tc.revoked)
		}
	}
	// the space's own people still have it all
	for _, p := range []string{"/api/machines/" + shared.MachineID, "/api/machines/" + given.MachineID} {
		if r := owner.Do("GET", p, nil); r.Code != http.StatusOK {
			t.Errorf("the owner reads %s: %s", p, r)
		}
	}
}

// An account that was switched off holds no session and cannot sign in; signing out everywhere ends every session of
// the person.
func TestSwitchedOffAccountAndSignOutEverywhere(t *testing.T) {
	s := hubtest.New(t)
	admin, person := s.SignIn("admin@example.test"), s.SignIn("person@example.test")
	other := s.SignIn("person@example.test")
	for _, p := range []*hubtest.Person{person, other} {
		if r := p.Do("GET", "/api/me", nil); r.Code != http.StatusOK {
			t.Fatalf("signed in: %s", r)
		}
	}
	// only an administrator of the installation may, and not themselves
	if r := person.Do("PUT", "/api/admin/users/"+admin.ID+"/disabled", map[string]bool{"disabled": true}); r.Code != http.StatusNotFound {
		t.Errorf("a person switches an account off: %s", r)
	}
	if r := admin.Do("PUT", "/api/admin/users/"+admin.ID+"/disabled", map[string]bool{"disabled": true}); r.Code != http.StatusConflict {
		t.Errorf("an administrator switches their own account off: %s", r)
	}

	if r := person.Do("POST", "/api/auth/logout/all", map[string]string{}); r.Code != http.StatusNoContent {
		t.Fatalf("sign out everywhere: %s", r)
	}
	for _, p := range []*hubtest.Person{person, other} {
		if r := p.Do("GET", "/api/me", nil); r.Code != http.StatusUnauthorized {
			t.Errorf("after signing out everywhere: %s", r)
		}
	}

	again := s.SignIn("person@example.test")
	if r := admin.Do("PUT", "/api/admin/users/"+again.ID+"/disabled", map[string]bool{"disabled": true}); r.Code != http.StatusOK {
		t.Fatalf("switching the account off: %s", r)
	}
	if r := again.Do("GET", "/api/me", nil); r.Code != http.StatusUnauthorized {
		t.Errorf("a switched-off account's session: %s", r)
	}
	if r := s.Browser().Do("POST", "/api/auth/email/start", map[string]string{"email": "person@example.test", "intent": "signin"}); r.Code == http.StatusOK {
		b := s.Browser()
		if r := b.Do("POST", "/api/auth/email/verify", map[string]string{"email": "person@example.test", "code": s.Mail.Code("person@example.test")}); r.Code != http.StatusForbidden {
			t.Errorf("a switched-off account signs in: %s", r)
		}
	}
	if r := admin.Do("PUT", "/api/admin/users/"+again.ID+"/disabled", map[string]bool{"disabled": false}); r.Code != http.StatusOK {
		t.Fatalf("switching the account back on: %s", r)
	}
	if back := s.SignIn("person@example.test"); back.ID != again.ID {
		t.Errorf("the account after it was switched back on: %s, want %s", back.ID, again.ID)
	}
}

package resaccess

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// account makes a person who signs in under email; verified says whether their provider vouches for the address.
func account(t *testing.T, db *store.DB, email string, verified bool) Actor {
	t.Helper()
	ctx := context.Background()
	u := &store.User{Email: email}
	if err := db.S().CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := db.S().CreateIdentity(ctx, &store.Identity{UserID: u.ID, Provider: "test", Subject: email, Email: email, EmailVerified: verified}); err != nil {
		t.Fatal(err)
	}
	a := Actor{UserID: u.ID, Email: email}
	if verified {
		a.Verified = []string{email}
	}
	return a
}

func heldBy(t *testing.T, s *Service, email string) string {
	t.Helper()
	var id *string
	if err := s.db.QueryRow(context.Background(), `SELECT user_id FROM resource_access_people WHERE kind = ? AND id = ? AND email = ?`, ref.Kind, ref.ID, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id == nil {
		return ""
	}
	return *id
}

// A role given to an address waits as an invitation: an account that merely signs in under the address gets nothing,
// and the person who has proved the address gets the role, which is theirs from then on.
func TestARoleGivenToAnAddressWaitsForWhoProvesIt(t *testing.T) {
	s := newService(t)
	call(t, s, owner, "PUT", "team", `{"level":"none"}`, 200)
	claimed := account(t, s.db, "guest@example.com", false)
	v := call(t, s, owner, "PUT", "people/guest@example.com", `{"role":"edit"}`, 200)
	if ppl := v["people"].([]any); len(ppl) != 1 || ppl[0].(map[string]any)["pending"] != true {
		t.Fatalf("the role is shown as waiting: %v", v["people"])
	}
	if got := role(t, s, claimed); got != "" {
		t.Fatalf("an account with an address nobody vouches for got %q", got)
	}
	if got := heldBy(t, s, "guest@example.com"); got != "" {
		t.Fatalf("the invitation was taken by %q", got)
	}
	// The person who has proved the address, whatever address their account goes by.
	real := account(t, s.db, "real@example.com", true)
	real.Verified = append(real.Verified, "guest@example.com")
	if got := role(t, s, real); got != "edit" {
		t.Fatalf("the person who proved the address got %q", got)
	}
	if got := heldBy(t, s, "guest@example.com"); got != real.UserID {
		t.Fatalf("the role is held by %q, want the person who proved the address", got)
	}
	real.Verified = nil
	if got := role(t, s, real); got != "edit" {
		t.Errorf("the role stays with the account: %q", got)
	}
	if got := role(t, s, claimed); got != "" {
		t.Errorf("the other account got %q", got)
	}
	// A role given to an address somebody has proved already is theirs at once.
	mate := account(t, s.db, "mate@example.com", true)
	call(t, s, owner, "PUT", "people/mate@example.com", `{"role":"view"}`, 200)
	if got := heldBy(t, s, "mate@example.com"); got != mate.UserID {
		t.Errorf("a role given to a proved address is held by %q", got)
	}
	// A request is answered to the account that asked.
	asked := account(t, s.db, "asked@example.com", false)
	call(t, s, asked, "POST", "requests", `{"level":"comment"}`, 200)
	open := call(t, s, owner, "GET", "", "", 200)["requests"].([]any)
	call(t, s, owner, "POST", "requests/"+open[0].(map[string]any)["id"].(string), `{"decision":"allow"}`, 200)
	if got := role(t, s, asked); got != "comment" {
		t.Errorf("an allowed request gives the account that asked %q", got)
	}
	// An invitation is not taken by a request from an account that merely signs in under its address.
	invited := account(t, s.db, "invited@example.com", false)
	call(t, s, owner, "PUT", "people/invited@example.com", `{"role":"edit"}`, 200)
	call(t, s, invited, "POST", "requests", `{"level":"comment"}`, 200)
	reqs := call(t, s, owner, "GET", "", "", 200)["requests"].([]any)
	if len(reqs) != 1 || reqs[0].(map[string]any)["verified"] != false {
		t.Fatalf("the request of an account with an unproved address: %v", reqs)
	}
	call(t, s, owner, "POST", "requests/"+reqs[0].(map[string]any)["id"].(string), `{"decision":"allow"}`, 409)
	if got := role(t, s, invited); got != "" || heldBy(t, s, "invited@example.com") != "" {
		t.Errorf("the invitation was taken through a request: role %q", got)
	}
	// A request from an account that signs in under an address another person has proved is not granted in its name.
	squat := Actor{UserID: asked.UserID, Email: "mate@example.com"}
	err0 := s.write(context.Background(), func(tx *txn) error {
		return tx.grantTo(Ref{Kind: KindDoc, ID: "other"}, owner.Email, "mate@example.com", squat.UserID, "edit", ttl{none: true}, false)
	})
	if Status(err0) != 409 {
		t.Errorf("an address another person proved was tied to an account: %v", err0)
	}
	// A role one person holds under an address is not handed to another account that signs in under it.
	if _, err := s.db.Exec(context.Background(), `UPDATE resource_access_people SET user_id = ? WHERE email = 'asked@example.com'`, real.UserID); err != nil {
		t.Fatal(err)
	}
	err := s.write(context.Background(), func(tx *txn) error {
		return tx.grantTo(ref, owner.Email, "asked@example.com", asked.UserID, "edit", ttl{none: true}, false)
	})
	if Status(err) != 409 || heldBy(t, s, "asked@example.com") != real.UserID {
		t.Errorf("a role held by one person was handed to another: %v", err)
	}
	// Somebody who may share does not change their own role through another address of theirs.
	call(t, s, owner, "PUT", "settings", `{"editorsCanShare":true}`, 200)
	sharer := real
	sharer.Verified = []string{"real@example.com", "second@example.com"}
	call(t, s, sharer, "PUT", "people/second@example.com", `{"role":"edit"}`, 403)
	sharer.Verified = nil
	call(t, s, sharer, "PUT", "people/guest@example.com", `{"role":"view"}`, 403)
}

// An address two people have proved stands for neither of them.
func TestAnAddressTwoPeopleProvedIsNobodys(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	one, two := account(t, s.db, "one@example.com", true), account(t, s.db, "two@example.com", true)
	for _, a := range []Actor{one, two} {
		if err := s.db.S().CreateIdentity(ctx, &store.Identity{UserID: a.UserID, Provider: "other", Subject: a.Email, Email: "Shared@example.com", EmailVerified: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []Actor{one, two} {
		got, err := s.db.S().VerifiedEmails(ctx, a.UserID)
		if err != nil || len(got) != 1 || got[0] != a.Email {
			t.Errorf("%s has proved %v, want only their own address (%v)", a.Email, got, err)
		}
	}
}

// Roles given to addresses before roles were held by people: the migration loses none of them. One whose address a
// single person has proved goes to that person; the others wait as invitations.
func TestMigrationTurnsAddressRolesIntoInvitations(t *testing.T) {
	ctx := context.Background()
	url := "sqlite://" + filepath.Join(t.TempDir(), "hub.db")
	old, err := store.OpenWith(ctx, url, store.Options{Last: 50})
	if err != nil {
		t.Fatal(err)
	}
	proven, typed := account(t, old, "proven@example.com", true), account(t, old, "typed@example.com", false)
	for _, email := range []string{"proven@example.com", "typed@example.com", "nobody@example.com"} {
		if _, err := old.Exec(ctx, `INSERT INTO resource_access_people (kind, id, email, role, granted_at) VALUES ('doc', 'plan', ?, 'edit', 1)`, email); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := old.Exec(ctx, `INSERT INTO resource_access (kind, id, owner_email, team_level, created_at, updated_at) VALUES ('doc', 'plan', 'owner@example.com', 'none', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db, func() time.Time { return time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC) }, "https://hub.example.com")
	for email, want := range map[string]string{"proven@example.com": proven.UserID, "typed@example.com": "", "nobody@example.com": ""} {
		if got := heldBy(t, s, email); got != want {
			t.Errorf("the role of %s is held by %q, want %q", email, got, want)
		}
	}
	if got := role(t, s, proven); got != "edit" {
		t.Errorf("the person who proved their address keeps the role: %q", got)
	}
	if got := role(t, s, typed); got != "" {
		t.Errorf("an account with an address nobody vouches for got %q", got)
	}
	if ppl := call(t, s, owner, "GET", "", "", 200)["people"].([]any); len(ppl) != 3 {
		t.Errorf("the migration kept %d of 3 roles: %v", len(ppl), ppl)
	}
}

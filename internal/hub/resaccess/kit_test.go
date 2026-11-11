package resaccess

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var kitRef = KitRef("space1", "acme-ds")

func kitCall(t *testing.T, s *Service, a Actor, method, path, body string, want int) {
	t.Helper()
	r := httptest.NewRequest(method, "/x", strings.NewReader(body))
	w := httptest.NewRecorder()
	var rest []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			rest = append(rest, p)
		}
	}
	s.Serve(w, r, a, kitRef, rest, Info{Owner: "maker@example.com", OwnerName: "Maker"}, func(v any) (string, error) { return "", nil })
	if w.Code != want {
		t.Fatalf("%s %s: %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
}

func kitRole(t *testing.T, s *Service, email string) string {
	t.Helper()
	r, err := s.Role(context.Background(), kitRef, proved(email))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A kit nobody recorded is closed; the first claim makes one owner, private; the rest of the claims change nothing.
func TestKitClaimIsOneOwnerAndPrivate(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if kitRole(t, s, "a@example.com") != "" {
		t.Fatal("an unrecorded kit is open to someone")
	}
	got, err := s.ClaimKit(ctx, "space1", "acme-ds", "a@example.com", "A", false)
	if err != nil || got != "a@example.com" {
		t.Fatalf("first claim: %q %v", got, err)
	}
	got, err = s.ClaimKit(ctx, "space1", "acme-ds", "b@example.com", "B", false)
	if err != nil || got != "a@example.com" {
		t.Fatalf("second claim must report the first owner: %q %v", got, err)
	}
	if kitRole(t, s, "a@example.com") != "owner" || kitRole(t, s, "b@example.com") != "" {
		t.Fatalf("a new kit is private to its maker: a=%q b=%q", kitRole(t, s, "a@example.com"), kitRole(t, s, "b@example.com"))
	}
	view, err := s.KitView(ctx, "space1", proved("a@example.com"))
	if err != nil || !view["acme-ds"].Own || !view["acme-ds"].Edit {
		t.Fatalf("view: %+v %v", view, err)
	}
	if v, _ := s.KitView(ctx, "space1", proved("b@example.com")); v["acme-ds"].See {
		t.Fatal("b sees a private kit")
	}
	if v, _ := s.KitView(ctx, "space2", proved("a@example.com")); len(v) != 0 {
		t.Fatalf("another space's view: %+v", v)
	}
}

// A kit that was on the machines before kits were shared stays visible to the team, for viewing.
func TestLegacyKitIsViewableByTheTeam(t *testing.T) {
	s := newService(t)
	if _, err := s.ClaimKit(context.Background(), "space1", "old-kit", "a@example.com", "A", true); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Role(context.Background(), KitRef("space1", "old-kit"), proved("mate@example.com"))
	if r != "view" {
		t.Fatalf("a teammate's role in a legacy kit: %q", r)
	}
}

// The API gives a team or the company nothing above view for a kit, gives a person only view or edit, and an editor who may
// share cannot give a group an edit.
func TestKitCeilings(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.ClaimKit(ctx, "space1", "acme-ds", "maker@example.com", "Maker", false); err != nil {
		t.Fatal(err)
	}
	maker, editor := proved("maker@example.com"), proved("ed@example.com")
	// the company/team level
	for _, level := range []string{"comment", "edit"} {
		kitCall(t, s, maker, "PUT", "team", `{"level":"`+level+`"}`, 400)
	}
	kitCall(t, s, maker, "PUT", "team", `{"level":"view"}`, 200)
	if kitRole(t, s, "mate@example.com") != "view" {
		t.Fatalf("the team's view: %q", kitRole(t, s, "mate@example.com"))
	}
	// people: view or edit by name; no manage, no comment
	kitCall(t, s, maker, "PUT", "people/ed@example.com", `{"role":"manage"}`, 400)
	kitCall(t, s, maker, "PUT", "people/ed@example.com", `{"role":"comment"}`, 400)
	kitCall(t, s, maker, "PUT", "people/ed@example.com", `{"role":"edit"}`, 200)
	if kitRole(t, s, "ed@example.com") != "edit" {
		t.Fatalf("a named editor: %q", kitRole(t, s, "ed@example.com"))
	}
	if v, err := s.KitView(ctx, "space1", proved("ed@example.com")); err != nil || !v["acme-ds"].Edit || v["acme-ds"].Own {
		t.Fatalf("a named editor's view: %+v %v", v, err)
	}
	// a team can only be given view, never edit and never «edit and share»
	err := s.write(ctx, func(tx *txn) error {
		for _, c := range []struct {
			role  string
			share bool
		}{{"edit", false}, {"edit", true}, {"comment", false}} {
			if e := tx.grantTeam(kitRef, "maker@example.com", "team-1", c.role, c.share, ttl{none: true}); e == nil {
				t.Errorf("a team was given %s (share %v) on a kit", c.role, c.share)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// an editor who may share can name a person, but cannot lift a group
	kitCall(t, s, maker, "PUT", "settings", `{"editorsCanShare":true}`, 200)
	kitCall(t, s, editor, "PUT", "people/friend@example.com", `{"role":"view"}`, 200)
	kitRefused(t, s, editor, "PUT", "team", `{"level":"edit"}`)
	kitRefused(t, s, editor, "PUT", "teams/team-1", `{"role":"edit","canShare":true}`)
	// an editor does not change the owner
	kitCall(t, s, editor, "POST", "owner", `{"email":"ed@example.com"}`, 403)
	// no guest links
	kitCall(t, s, maker, "POST", "links", `{}`, http.StatusForbidden)
}

// A stale row cannot lift a ceiling: a team given edit by an older row counts for view.
func TestKitFoldIgnoresARowAboveTheCeiling(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.ClaimKit(ctx, "space1", "acme-ds", "maker@example.com", "Maker", false); err != nil {
		t.Fatal(err)
	}
	err := s.write(ctx, func(tx *txn) error {
		_, e := tx.q.Exec(tx.ctx, `UPDATE resource_access SET team_level = 'edit' WHERE kind = 'kit' AND id = ? AND COALESCE(space_id, '') = ?`, kitRef.ID, kitRef.Space)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := kitRole(t, s, "mate@example.com"); r != "view" {
		t.Fatalf("a team level of edit counted as %q", r)
	}
}

// kitRefused: the request is not taken (any 4xx).
func kitRefused(t *testing.T, s *Service, a Actor, method, path, body string) {
	t.Helper()
	r := httptest.NewRequest(method, "/x", strings.NewReader(body))
	w := httptest.NewRecorder()
	var rest []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			rest = append(rest, p)
		}
	}
	s.Serve(w, r, a, kitRef, rest, Info{Owner: "maker@example.com", OwnerName: "Maker"}, func(v any) (string, error) { return "", nil })
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("%s %s: %d, want a refusal: %s", method, path, w.Code, w.Body.String())
	}
}

// A kit belongs to its space: the same kit id in another space is another kit, neither seen nor claimed from here.
func TestKitOfAnotherSpaceIsNotSeenNorClaimed(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if owner, err := s.ClaimKit(ctx, "space1", "acme-ds", "a@example.com", "A", false); err != nil || owner != "a@example.com" {
		t.Fatalf("claim in space1: %q %v", owner, err)
	}
	if v, err := s.KitView(ctx, "space2", proved("a@example.com")); err != nil || len(v) != 0 {
		t.Fatalf("space2 sees the kit of space1: %+v %v", v, err)
	}
	if owner, err := s.ClaimKit(ctx, "space2", "acme-ds", "b@example.com", "B", false); err != nil || owner != "b@example.com" {
		t.Fatalf("claim of the same id in space2 must make its own kit: %q %v", owner, err)
	}
	if r, _ := s.Role(ctx, KitRef("space1", "acme-ds"), proved("a@example.com")); r != "owner" {
		t.Fatalf("the kit of space1 changed hands: a=%q", r)
	}
	if r, _ := s.Role(ctx, KitRef("space1", "acme-ds"), proved("b@example.com")); r != "" {
		t.Fatalf("b reaches the kit of space1: %q", r)
	}
	if v, _ := s.KitView(ctx, "space2", proved("a@example.com")); v["acme-ds"].See {
		t.Fatal("a sees the kit of space2")
	}

	// A row of space3 under the key a space1 kit would take: space1 neither lists nor takes it.
	err := s.write(ctx, func(tx *txn) error {
		_, e := tx.q.Exec(tx.ctx, `INSERT INTO resource_access (kind, id, space_id, owner_email, owner_name, home_team, team_level, editors_can_share, title, created_at, updated_at)
			VALUES ('kit', 'space1/stray', 'space3', 'c@example.com', 'C', '', 'view', 0, 'stray', 1, 1)`)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.KitView(ctx, "space1", proved("c@example.com")); v["stray"].See {
		t.Fatal("space1 lists a row of space3")
	}
	if owner, err := s.ClaimKit(ctx, "space1", "stray", "a@example.com", "A", false); err != nil || owner == "a@example.com" {
		t.Fatalf("space1 took a row of space3: %q %v", owner, err)
	}
	if err := s.OpenLegacyKit(ctx, "space1", "stray", "a@example.com"); err != nil {
		t.Fatal(err)
	}
	var owner, space string
	err = s.write(ctx, func(tx *txn) error {
		return tx.q.QueryRow(tx.ctx, `SELECT owner_email, space_id FROM resource_access WHERE kind = 'kit' AND id = 'space1/stray'`).Scan(&owner, &space)
	})
	if err != nil || owner != "c@example.com" || space != "space3" {
		t.Fatalf("the row of space3 was changed: owner=%q space=%q %v", owner, space, err)
	}
}

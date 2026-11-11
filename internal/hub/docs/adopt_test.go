package docs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The documents nothing ties to an organization are placed in a space by an administrator's word: a dry run writes
// nothing and says what would be placed and what is left; applying gives them to the people of the space and to
// nobody else, and a document that has a space is never moved.
func TestAdoptPlacesLooseDocumentsInASpace(t *testing.T) {
	s, who, logs := circleRig(t, true, false)
	ctx := context.Background()
	st := s.db.S()
	spaceOf := func(name string) string {
		t.Helper()
		spaces, err := st.SpacesByUser(ctx, who[name].User.ID)
		if err != nil || len(spaces) != 1 {
			t.Fatalf("the space of %s: %v %v", name, spaces, err)
		}
		return spaces[0].ID
	}
	ours, theirs := spaceOf("author"), spaceOf("stranger")
	records := func() int {
		t.Helper()
		var n int
		if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM doc_meta`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	slugs := func(rep adoptReport) string {
		var out []string
		for _, d := range rep.Adopted {
			out = append(out, d.Slug)
		}
		return strings.Join(out, ",")
	}

	if _, err := s.adopt(ctx, "nosuchspace", nil, true); !errors.Is(err, httpx.ErrNotFound) {
		t.Errorf("a space that is not there: %v", err)
	}
	// no author is listed: nothing to place, both are left
	rep, err := s.adopt(ctx, ours, nil, true)
	if err != nil || rep.Applied || len(rep.Adopted) != 0 || rep.LeftCount != 2 || len(rep.Left) != 2 || rep.Left[0].Author != "" || rep.Left[1].Author != "agent" || rep.Left[1].Member {
		t.Fatalf("nobody's documents: %+v %v", rep, err)
	}
	// the board's author goes by the account address of a member who has not proved it: the report says so, and
	// the board is not placed until the administrator lists the author
	u := &store.User{Email: "agent"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := st.AddMember(ctx, ours, u.ID, store.RoleMember); err != nil {
		t.Fatal(err)
	}
	rep, err = s.adopt(ctx, ours, []string{""}, true)
	if err != nil || !rep.Applied || slugs(rep) != "loose" || rep.LeftCount != 1 || rep.Left[0].Author != "agent" || !rep.Left[0].Member {
		t.Fatalf("an unproved address is a hint only: %+v %v", rep, err)
	}
	if got := seen(t, s, who["mate"]); strings.Contains(","+got+",", ",anon,") {
		t.Fatalf("a board was placed by an address nobody proved: %s", got)
	}
	before, sawBefore := records(), seen(t, s, who["mate"])
	rep, err = s.adopt(ctx, ours, []string{"Agent"}, false)
	if err != nil || rep.Applied || slugs(rep) != "anon" || rep.LeftCount != 0 {
		t.Fatalf("the dry run: %+v %v", rep, err)
	}
	if records() != before || seen(t, s, who["mate"]) != sawBefore {
		t.Errorf("the dry run wrote something: %d records, the mate sees %s", records(), seen(t, s, who["mate"]))
	}

	rep, err = s.adopt(ctx, ours, []string{"Agent"}, true)
	if err != nil || !rep.Applied || slugs(rep) != "anon" {
		t.Fatalf("placing: %+v %v", rep, err)
	}
	for _, slug := range []string{"loose", "anon"} {
		if got := seen(t, s, who["mate"]); !strings.Contains(","+got+",", ","+slug+",") {
			t.Errorf("a member of the space does not see %s: %s", slug, got)
		}
		if got := seen(t, s, who["stranger"]); strings.Contains(","+got+",", ","+slug+",") {
			t.Errorf("a person of another space sees %s: %s", slug, got)
		}
	}
	// what was placed stays where it is, and the start has nothing left to name
	for _, space := range []string{ours, theirs} {
		if rep, err = s.adopt(ctx, space, []string{"", "agent"}, true); err != nil || len(rep.Adopted) != 0 || rep.LeftCount != 0 {
			t.Errorf("a second run on %s: %+v %v", space, rep, err)
		}
	}
	logs.Reset()
	s.reportLoose(ctx)
	if strings.Contains(logs.String(), "belong to no organization") {
		t.Errorf("the start still names documents: %s", logs.String())
	}
}

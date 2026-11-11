package docs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A person finds, by two characters or more, the people they share a space with and nobody else.
func TestPeopleAreFoundWithinSharedSpaces(t *testing.T) {
	s, who, _ := circleRig(t, false, false)
	ctx := context.Background()
	found := func(p, q string) int {
		t.Helper()
		list, err := s.people(ctx, who[p], q)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	for _, c := range []struct {
		person, q string
		want      int
	}{
		{"mate", "au", 1},     // a mate of the same space
		{"mate", "AUTHOR", 1}, // whatever the case
		{"mate", "st", 0},     // a person of another organization
		{"stranger", "au", 0},
		{"stranger", "ma", 0},
		{"mate", "a", 0}, // one character is not a search
		{"mate", "", 0},
		{"mate", "%", 0},
		{"admin", "st", 1}, // an administrator of the installation
	} {
		if got := found(c.person, c.q); got != c.want {
			t.Errorf("%s searching %q finds %d, want %d", c.person, c.q, got, c.want)
		}
	}
}

func TestPeopleSearchIsPaced(t *testing.T) {
	s, who, _ := circleRig(t, false, false)
	ask := func(p string) error {
		return s.peopleRoute(httptest.NewRecorder(), httptest.NewRequest("GET", "/x?q=au", nil), who[p])
	}
	for i := 0; i < peoplePerMinute; i++ {
		if err := ask("mate"); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}
	var f *failure
	if err := ask("mate"); !errors.As(err, &f) || f.status != http.StatusTooManyRequests {
		t.Errorf("one search too many: %v", err)
	}
	if err := ask("author"); err != nil {
		t.Errorf("the pace of one person holds another: %v", err)
	}
}

// What the documents service says about an author is an address somebody typed: it makes a document the person's own
// only when they have proved the address.
func TestAnUnprovedAddressIsNotTheAuthor(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	ctx := context.Background()
	c := s.circleOf(who["stranger"])
	if c.holds(ctx, nil, "stranger@example.com") != true {
		t.Fatal("a person who proved the address is the author")
	}
	if _, err := s.db.Exec(ctx, `UPDATE identities SET email_verified = 0 WHERE user_id = ?`, who["stranger"].User.ID); err != nil {
		t.Fatal(err)
	}
	if s.circleOf(who["stranger"]).holds(ctx, nil, "stranger@example.com") {
		t.Error("an account whose address nobody vouches for is taken for the author")
	}
}

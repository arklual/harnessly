package docs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// An administrator of the installation who opens a document of a space they are not in leaves one line in that
// space's journal, however often they open it; a member of the space leaves none.
func TestOperatorOpeningADocumentIsInTheJournal(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := db.S()
	owner, boss := &store.User{Email: "owner@example.com"}, &store.User{Email: "boss@example.com"}
	for _, u := range []*store.User{owner, boss} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	sp := &store.Space{Slug: "north", Name: "North", CreatedBy: owner.ID}
	if err := st.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db}
	m := &meta{SpaceID: sp.ID}
	lines := func() []store.AuditEvent {
		t.Helper()
		out, err := st.Audit(ctx, store.AuditFilter{SpaceID: sp.ID})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, p := range []*auth.Principal{{User: *owner, Admin: true}, {User: *boss}} {
		if err := s.noteOperator(ctx, p, "plan", m); err != nil {
			t.Fatal(err)
		}
	}
	if got := lines(); len(got) != 0 {
		t.Fatalf("a member, or a person who is no administrator, left a line: %+v", got)
	}
	admin := &auth.Principal{User: *boss, Admin: true}
	for range 3 {
		if err := s.noteOperator(ctx, admin, "plan", m); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.noteOperator(ctx, admin, "notes", m); err != nil {
		t.Fatal(err)
	}
	got := lines()
	if len(got) != 2 {
		t.Fatalf("lines: %+v", got)
	}
	for _, e := range got {
		if e.ActorID != boss.ID || e.Action != "operator.read" || e.TargetKind != "doc" || e.SpaceID != sp.ID || e.Before != "" || e.After != "" {
			t.Errorf("line %+v", e)
		}
	}
}

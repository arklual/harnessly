package machines

import (
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestOwnerGitAuthorRequiresUnambiguousVerifiedPrimary(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := db.S()
	u := store.User{Name: "Person", Email: "personal@example.org"}
	if err := st.CreateUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	check := func(want hubproto.GitAuthor) {
		t.Helper()
		got, err := ownerGitAuthor(ctx, st, u.ID)
		if err != nil || got != want {
			t.Fatalf("author %+v want %+v: %v", got, want, err)
		}
	}
	check(hubproto.GitAuthor{})
	add := func(id, subject string, verified bool) {
		t.Helper()
		if err := st.CreateIdentity(ctx, &store.Identity{UserID: id, Provider: "oidc", Subject: subject, Email: u.Email, EmailVerified: verified}); err != nil {
			t.Fatal(err)
		}
	}
	add(u.ID, "self-reported", false)
	check(hubproto.GitAuthor{})
	add(u.ID, "verified", true)
	check(hubproto.GitAuthor{Name: u.Name, Email: u.Email})
	other := store.User{Name: "Another person", Email: "other@example.org"}
	if err := st.CreateUser(ctx, &other); err != nil {
		t.Fatal(err)
	}
	add(other.ID, "ambiguous", true)
	check(hubproto.GitAuthor{})
}

package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The levels of the journal follow the roles of the hub: a space to its members, an organization to its admins, all
// to a hub admin; a person who asks for more gets a refusal, not an empty page.
func TestScopesFollowRoles(t *testing.T) {
	b, _ := newBus(t)
	ctx := context.Background()
	st := b.db.S()
	for _, id := range []string{"boss", "ann", "bob", "root"} {
		u := store.User{ID: id, Email: id + "@example.com", Name: id}
		if err := st.CreateUser(ctx, &u); err != nil {
			t.Fatal(err)
		}
	}
	o := store.Org{ID: "o1", Slug: "acme", Name: "Acme", CreatedBy: "boss", CreatedAt: time.Now()}
	if err := st.CreateOrg(ctx, &o); err != nil {
		t.Fatal(err)
	}
	sp := store.Space{ID: "s1", Slug: "team", Name: "Team", CreatedBy: "boss", CreatedAt: time.Now(), OrgID: "o1"}
	if err := st.CreateSpace(ctx, &sp); err != nil {
		t.Fatal(err)
	}
	if err := st.AddMember(ctx, "s1", "ann", store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Grant(ctx, store.RoleGrant{UserID: "boss", ScopeKind: store.ScopeOrg, ScopeID: "o1", Role: store.RoleAdmin, GrantedBy: "boss", GrantedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	who := func(id string, admin bool) *auth.Principal {
		return &auth.Principal{User: store.User{ID: id, Name: id}, Admin: admin}
	}
	for _, c := range []struct {
		name, user, scope string
		admin, ok         bool
	}{
		{"own", "bob", "me", false, true},
		{"a space of mine", "ann", "space:s1", false, true},
		{"a space not mine", "bob", "space:s1", false, false},
		{"an org I administer", "boss", "org:o1", false, true},
		{"an org I am only in", "ann", "org:o1", false, false},
		{"all, not an admin", "ann", "all", false, false},
		{"all, a hub admin", "root", "all", true, true},
		{"the deployment's own, not an admin", "ann", "deployment", false, false},
		{"nonsense", "ann", "galaxy:1", false, false},
	} {
		_, err := b.scopeQuery(ctx, who(c.user, c.admin), c.scope)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.ok && c.scope != "galaxy:1" && !errors.Is(err, errScope) {
			t.Errorf("%s: want a refusal of the scope, got %v", c.name, err)
		}
	}
	levels, err := b.levels(ctx, who("boss", false))
	// boss made the space, so is in it too.
	if err != nil || len(levels) != 3 || levels[1].Scope != "space:s1" || levels[2].Scope != "org:o1" {
		t.Fatalf("boss's levels: %+v %v", levels, err)
	}
	levels, _ = b.levels(ctx, who("ann", false))
	if len(levels) != 2 || levels[1].Scope != "space:s1" {
		t.Fatalf("ann's levels: %+v", levels)
	}
}

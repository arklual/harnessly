package designaccess

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// with the hub's store the design library is the space's. A project recorded under its owner's machine counts
// on every machine of the space: a teammate's machine finds its rows, the store's view shows the project to the
// machines whose person has a role in it and hides it from the others, and an owner file reaching the store records a
// project the hub did not know.

type spaceRig struct {
	db                      *store.DB
	svc                     *Service
	owner, mate, stranger   store.Machine
	ownerU, mateU, strangeU store.User
}

func newSpaceRig(t *testing.T, shared bool) *spaceRig {
	t.Helper()
	ctx := context.Background()
	db := openRowDB(t)
	st := db.S()
	r := &spaceRig{db: db}
	r.ownerU = store.User{Email: "owner@example.com", Name: "Owner"}
	r.mateU = store.User{Email: "mate@example.com", Name: "Mate"}
	r.strangeU = store.User{Email: "stranger@example.com", Name: "Stranger"}
	for _, u := range []*store.User{&r.ownerU, &r.mateU, &r.strangeU} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	sp := &store.Space{Slug: "team", Name: "Team", CreatedBy: r.ownerU.ID}
	if err := st.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	for _, u := range []store.User{r.mateU, r.strangeU} {
		if err := st.AddMember(ctx, sp.ID, u.ID, store.RoleMember); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(name, key string, by store.User) store.Machine {
		m := store.Machine{SpaceID: sp.ID, Name: name, Profile: "p", Via: "v", KeyPub: key, EncPub: key + "e", CreatedBy: by.ID}
		if err := st.CreateMachine(ctx, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	r.owner, r.mate, r.stranger = mk("vm-owner", "k1", r.ownerU), mk("vm-mate", "k2", r.mateU), mk("vm-stranger", "k3", r.strangeU)
	r.svc = New(Deps{DB: db, Now: func() time.Time { return time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC) }, Shared: shared})
	return r
}

func TestSharedLibraryRowsCountOnEveryMachineOfTheSpace(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	// The owner's machine reports a personal project; the owner gives the mate «edit».
	if done, err := r.svc.registerCreated(ctx, r.owner, "secret", r.ownerU.Email); err != nil || !done {
		t.Fatalf("registerCreated: %v %v", done, err)
	}
	// A hub's new project is open to its maker's team (the space): the owner makes it personal and gives the mate «edit».
	if err := r.svc.write(ctx, r.owner, func(t *txn) error {
		if err := t.setTeam("secret", r.ownerU.Email, levelNone); err != nil {
			return err
		}
		return t.grant("secret", r.ownerU.Email, r.mateU.Email, roleEdit, ttl{none: true}, false)
	}); err != nil {
		t.Fatal(err)
	}

	// The mate's machine (its own machine id) finds the project's rows recorded under the owner's machine.
	tx, err := r.svc.read(ctx, r.mate)
	if err != nil {
		t.Fatal(err)
	}
	p, err := tx.getProject("secret", false)
	if err != nil || p == nil || p.Owner != r.ownerU.Email {
		t.Fatalf("the mate's machine does not see the project's row: %+v %v", p, err)
	}

	view := func(m store.Machine) StoreGrant {
		v, err := r.svc.StoreView(ctx, m)
		if err != nil {
			t.Fatal(err)
		}
		return v["secret"]
	}
	if g := view(r.owner); !g.See || !g.Edit {
		t.Fatalf("owner: %+v", g)
	}
	if g := view(r.mate); !g.See || !g.Edit {
		t.Fatalf("mate with edit: %+v", g)
	}
	if g := view(r.stranger); g.See || g.Edit {
		t.Fatalf("a stranger to a personal project sees it: %+v", g)
	}

	// A grant made through the mate's machine lands on the same rows: the stranger's machine sees the project now.
	if err := r.svc.write(ctx, r.mate, func(t *txn) error {
		return t.grant("secret", r.ownerU.Email, r.strangeU.Email, roleView, ttl{none: true}, false)
	}); err != nil {
		t.Fatal(err)
	}
	if g := view(r.stranger); !g.See || g.Edit {
		t.Fatalf("a viewer granted through another machine: %+v", g)
	}
}

func TestOwnerFileInTheStoreRecordsTheProject(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	file := []byte(`{"v":2,"user":"coder","name":"Owner","person":"owner@example.com","private":true}`)
	done, err := r.svc.RecordFromStore(ctx, r.owner, "fresh", file)
	if err != nil || !done {
		t.Fatalf("RecordFromStore: %v %v", done, err)
	}
	if again, err := r.svc.RecordFromStore(ctx, r.mate, "fresh", file); err != nil || again {
		t.Fatalf("a second owner file recorded the project again: %v %v", again, err)
	}
	// Recorded as a design event would record it: in its maker's team, so the space sees it until the owner says
	// otherwise; then the stranger's machine stops seeing it.
	if g := mustView(t, r, r.stranger)["fresh"]; !g.See {
		t.Fatalf("a team project is closed to the space: %+v", g)
	}
	if err := r.svc.write(ctx, r.owner, func(t *txn) error { return t.setTeam("fresh", r.ownerU.Email, levelNone) }); err != nil {
		t.Fatal(err)
	}
	if g, ok := mustView(t, r, r.stranger)["fresh"]; !ok || g.See {
		t.Fatalf("a personal project is open to the stranger: %+v %v", g, ok)
	}

	// A machine cannot make someone else the owner: the address must be one that may open the machine that sent it.
	foreign := []byte(`{"v":2,"user":"x","name":"X","person":"nobody@elsewhere.org","private":true}`)
	if done, err := r.svc.RecordFromStore(ctx, r.stranger, "theirs", foreign); err != nil || !done {
		t.Fatalf("RecordFromStore: %v %v", done, err)
	}
	tx, err := r.svc.read(ctx, r.stranger)
	if err != nil {
		t.Fatal(err)
	}
	p, err := tx.getProject("theirs", false)
	if err != nil || p == nil || p.Owner != r.strangeU.Email {
		t.Fatalf("owner of a project with a foreign owner file: %+v %v", p, err)
	}
}

func TestWithoutTheStoreRowsStayTheMachines(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, false)
	if _, err := r.svc.registerCreated(ctx, r.owner, "secret", r.ownerU.Email); err != nil {
		t.Fatal(err)
	}
	tx, err := r.svc.read(ctx, r.mate)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := tx.getProject("secret", false); err != nil || p != nil {
		t.Fatalf("another machine's row counted without a shared library: %+v %v", p, err)
	}
}

func mustView(t *testing.T, r *spaceRig, m store.Machine) map[string]StoreGrant {
	t.Helper()
	v, err := r.svc.StoreView(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

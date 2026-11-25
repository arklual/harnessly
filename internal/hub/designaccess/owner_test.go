package designaccess

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// with the switch hub-design-owner on, a machine that is off (or gone) does not take its projects out of the
// library: the hub lists every recorded project from its own rows, with the facts a record last gave it.
func TestHubKeepsTheProjectsOfAMachineThatIsOff(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	for _, id := range []string{"deck-one", "site-two"} {
		if done, err := r.svc.registerCreated(ctx, r.owner, id, r.ownerU.Email); err != nil || !done {
			t.Fatalf("registerCreated %s: %v %v", id, done, err)
		}
	}
	// The store has no file of them at all: the machine never synced, and it is off.
	r.svc.UseView(&fakeLibrary{})

	// Off (the default): the library is the store's files, nothing of the rows.
	if got := askLibrary(t, r, r.ownerU, r.owner).Artifacts; len(got) != 0 {
		t.Fatalf("the switch is off and the hub lists %+v", got)
	}

	r.svc.ownerOn = func(context.Context, string, string) bool { return true }
	if ok, err := r.svc.RecordMeta(ctx, r.owner, "deck-one", Meta{Title: "Питч", Kind: "deck", Preview: "cover.png", At: 1000}, MetaByMachine); err != nil || !ok {
		t.Fatalf("RecordMeta: %v %v", ok, err)
	}
	byID := map[string]storeArtifact{}
	for _, a := range askLibrary(t, r, r.ownerU, r.owner).Artifacts {
		byID[a.ID] = a
	}
	if len(byID) != 2 {
		t.Fatalf("the hub's library: %+v", byID)
	}
	d := byID["deck-one"]
	if d.Title != "Питч" || d.Kind != "slides" || d.Cover == nil || *d.Cover != "cover.png" || d.WorkMachine != r.owner.ID || d.Role != roleOwner {
		t.Fatalf("the deck: %+v", d)
	}
	if byID["site-two"].Title != "site-two" || byID["site-two"].Kind != "site" {
		t.Fatalf("a project nobody described: %+v", byID["site-two"])
	}

	// The hub's own edit stands against an older record of the machine; a newer one replaces it.
	if _, err := r.svc.RecordMeta(ctx, r.owner, "deck-one", Meta{Title: "Питч v2", At: 3000}, MetaByHub); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.svc.RecordMeta(ctx, r.owner, "deck-one", Meta{Title: "старое имя", At: 2000}, MetaByMachine); ok {
		t.Fatal("an older record of the machine replaced the hub's edit")
	}
	if ok, _ := r.svc.RecordMeta(ctx, r.owner, "ghost", Meta{Title: "x"}, MetaByMachine); ok {
		t.Fatal("a project nobody recorded got a row")
	}
	for _, a := range askLibrary(t, r, r.ownerU, r.owner).Artifacts {
		if a.ID == "deck-one" && a.Title != "Питч v2" {
			t.Fatalf("title %q", a.Title)
		}
	}
}

// the hub's side of the check names the projects no record of a machine has described yet.
func TestRecordsCheckNamesWhatNoRecordDescribed(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	for _, id := range []string{"deck-one", "site-two"} {
		if done, err := r.svc.registerCreated(ctx, r.owner, id, r.ownerU.Email); err != nil || !done {
			t.Fatalf("registerCreated %s: %v %v", id, done, err)
		}
	}
	rep, err := r.svc.RecordsCheck(ctx, r.ownerU, r.owner)
	if err != nil || rep.OK || rep.Projects != 2 || len(rep.Missing) != 2 {
		t.Fatalf("before any record: %+v %v", rep, err)
	}
	for _, id := range []string{"deck-one", "site-two"} {
		if ok, err := r.svc.RecordMeta(ctx, r.owner, id, Meta{Title: id, At: 5}, MetaByMachine); err != nil || !ok {
			t.Fatalf("RecordMeta %s: %v %v", id, ok, err)
		}
	}
	rep, err = r.svc.RecordsCheck(ctx, r.ownerU, r.owner)
	if err != nil || !rep.OK || rep.Described != 2 || len(rep.Missing) != 0 {
		t.Fatalf("after the records: %+v %v", rep, err)
	}
}

// with the owner machine off, a machine of an editor takes the working copy; a viewer cannot.
func TestTakeWorkMovesTheWorkingCopy(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	if done, err := r.svc.registerCreated(ctx, r.owner, "deck-one", r.ownerU.Email); err != nil || !done {
		t.Fatal(done, err)
	}
	if err := r.svc.TakeWork(ctx, r.ownerU, r.owner, "deck-one"); err == nil {
		t.Fatal("the switch is off and the working copy moved")
	}
	r.svc.ownerOn = func(context.Context, string, string) bool { return true }
	if err := r.svc.TakeWork(ctx, r.ownerU, r.owner, "ghost"); err == nil {
		t.Fatal("a project nobody recorded was taken")
	}
	if err := r.svc.TakeWork(ctx, r.ownerU, r.owner, "deck-one"); err != nil {
		t.Fatal(err)
	}
	mr, err := func() (metaRow, error) {
		tx, err := r.svc.read(ctx, r.owner)
		if err != nil {
			return metaRow{}, err
		}
		return tx.metaOf("deck-one")
	}()
	if err != nil || mr.WorkMachine != r.owner.ID {
		t.Fatalf("work machine %q %v", mr.WorkMachine, err)
	}
}

func TestOwnerEditorFindsPersonalSourceWithStoreOff(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, false)
	project := "qa-two-pages"
	if _, err := r.svc.registerCreated(ctx, r.owner, project, r.ownerU.Email); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.write(ctx, r.owner, func(tx *txn) error {
		if err := tx.setTeam(project, r.ownerU.Email, levelNone); err != nil {
			return err
		}
		return tx.grant(project, r.ownerU.Email, r.mateU.Email, roleView, ttl{none: true}, false)
	}); err != nil {
		t.Fatal(err)
	}
	get := func(u store.User) map[string]any {
		t.Helper()
		v, status, err := r.svc.oldRoute(httptest.NewRequest("GET", "/", nil), u, r.owner, []string{project})
		if status != 200 || err != nil {
			t.Fatalf("get %s: %d %v", u.Name, status, err)
		}
		return v.(map[string]any)
	}
	if get(r.ownerU)["ownerWorkMachine"] != nil {
		t.Fatal("unverified address named a machine")
	}
	for _, u := range []store.User{r.ownerU, r.mateU, r.strangeU} {
		if err := r.db.S().CreateIdentity(ctx, &store.Identity{UserID: u.ID, Provider: "test", Subject: u.ID, Email: u.Email, EmailVerified: true}); err != nil {
			t.Fatal(err)
		}
	}
	if get(r.ownerU)["ownerWorkMachine"] != r.owner.ID {
		t.Fatal("verified owner missed the source")
	}
	if v := get(r.mateU); v["role"] != roleView || v["ownerWorkMachine"] != nil {
		t.Fatal("project view granted a personal machine")
	}
	if v := get(r.strangeU); v["role"] != nil || v["ownerWorkMachine"] != nil {
		t.Fatal("private project gave an unrelated person a source")
	}
}

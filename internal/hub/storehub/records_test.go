package storehub

import (
	"context"
	"errors"
	"testing"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
)

// the collections of entity records follow the design access: a record of a project closed to a person
// does not reach them or take their write, a record committed reaches the hub's row of the project, and a path that is
// not `<id>.json` is not taken.
func TestEntityRecordsFollowTheProjectsAccess(t *testing.T) {
	ctx := context.Background()
	machines := fakeMachines{
		"owner": {ID: "owner", Name: "vm-owner", SpaceID: "sp1"},
		"mate":  {ID: "mate", Name: "vm-mate", SpaceID: "sp1"},
	}
	design := &fakeDesign{view: map[string]map[string]Grant{
		"owner": {"secret-deck": {See: true, Edit: true, Own: true}},
		"mate":  {"secret-deck": {See: false}},
	}}
	_, client := collectionStore(t, machines, design, filestore.KindDecks)
	owner, mate := client("owner"), client("mate")
	rec := func(id, title string) []byte {
		b, err := filestore.Record{ID: id, Title: title, Kind: "deck", At: 5}.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if _, err := owner.Put(ctx, "secret-deck.json", rec("secret-deck", "Pitch"), "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Put(ctx, "open-deck.json", rec("open-deck", "Open"), "t"); err != nil {
		t.Fatal(err)
	}
	ch, err := mate.Changes(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Entries) != 1 || ch.Entries[0].Path != "open-deck.json" {
		t.Fatalf("the mate sees %+v, want only the open deck's record", ch.Entries)
	}
	if _, err := mate.Put(ctx, "secret-deck.json", rec("secret-deck", "Mine"), "t"); !errors.Is(err, filesync.ErrRefused) {
		t.Fatalf("a write to the record of a closed project: %v", err)
	}
	if _, err := owner.Put(ctx, "notes/a.json", []byte("{}"), "t"); !errors.Is(err, filesync.ErrRefused) {
		t.Fatalf("a path that is not <id>.json: %v", err)
	}
	design.mu.Lock()
	defer design.mu.Unlock()
	if len(design.metas) != 2 || design.metas[0] != "owner:secret-deck:Pitch:deck" {
		t.Fatalf("the hub took %v of the records", design.metas)
	}
}

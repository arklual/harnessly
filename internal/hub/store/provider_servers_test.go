package store

import (
	"context"
	"path/filepath"
	"testing"
)

// A database where two machines name one server: the migration does not fail, the older machine keeps the server, the
// other is written down and loses the tie, and each server stays with the space of its machine.
func TestMigrationKeepsOneMachinePerServer(t *testing.T) {
	ctx := context.Background()
	url := "sqlite://" + filepath.Join(t.TempDir(), "hub.db")
	old, err := OpenWith(ctx, url, Options{Last: 49})
	if err != nil {
		t.Fatal(err)
	}
	s := old.S()
	u := &User{Email: "owner@example.com"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	sp := &Space{Slug: "north", Name: "North", CreatedBy: u.ID}
	if err := s.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for i, n := range []string{"first", "second", "alone", "bare"} {
		m := &Machine{SpaceID: sp.ID, Name: n, KeyPub: n, CreatedBy: u.ID, CreatedAt: fromMs(int64(1000 + i))}
		if err := s.CreateMachine(ctx, m); err != nil {
			t.Fatal(err)
		}
		ids[n] = m.ID
		ext := map[string]string{"first": "srv-1", "second": "srv-1", "alone": "srv-2", "bare": ""}[n]
		if err := s.PutResources(ctx, MachineResources{MachineID: m.ID, Provider: "cloud", ExternalID: ext, UpdatedAt: fromMs(int64(5000 - i))}); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("the migration over two machines on one server: %v", err)
	}
	defer db.Close()
	s = db.S()
	for n, want := range map[string]string{"first": "srv-1", "second": "", "alone": "srv-2", "bare": ""} {
		if r, err := s.ResourcesByMachine(ctx, ids[n]); err != nil || r.ExternalID != want {
			t.Errorf("%s is tied to %q, want %q (%v)", n, r.ExternalID, want, err)
		}
	}
	if list, err := s.ResourceConflicts(ctx); err != nil || len(list) != 1 || list[0].MachineID != ids["second"] || list[0].KeptMachineID != ids["first"] || list[0].ExternalID != "srv-1" {
		t.Errorf("the machines written down: %+v, %v", list, err)
	}
	for _, ext := range []string{"srv-1", "srv-2"} {
		if ok, err := s.ProviderServerOfSpace(ctx, sp.ID, "cloud", ext); err != nil || !ok {
			t.Errorf("%s is not with the space of its machine: %v", ext, err)
		}
	}
	// From now on a second machine cannot name a server another has.
	err = s.PutResources(ctx, MachineResources{MachineID: ids["bare"], Provider: "cloud", ExternalID: "srv-2", UpdatedAt: fromMs(9000)})
	if !IsUnique(err) {
		t.Errorf("a second machine on one server: %v", err)
	}
}

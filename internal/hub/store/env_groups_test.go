package store

import (
	"context"
	"testing"
	"time"
)

func TestEnvGroupsSQLite(t *testing.T)   { exerciseEnvGroups(t, openTest(t)) }
func TestEnvGroupsPostgres(t *testing.T) { exerciseEnvGroups(t, openPostgres(t)) }

func exerciseEnvGroups(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	now := time.UnixMilli(1_700_000_000_000).UTC()
	alice := &User{Email: "alice@example.com"}
	if err := s.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	g, ok, err := s.AddEnvGroup(ctx, EnvGroup{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", Name: "Бэкенд", DefaultOn: true, UpdatedAt: now})
	if err != nil || !ok {
		t.Fatalf("add %v %v", ok, err)
	}
	if _, ok, _ := s.AddEnvGroup(ctx, EnvGroup{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", Name: "Бэкенд", UpdatedAt: now}); ok {
		t.Fatal("a layer has one group of a name")
	}
	g.Required, g.Hint = true, "сервисы"
	if err := s.UpdateEnvGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.EnvGroupByID(ctx, g.ID); !got.Required || got.Hint != "сервисы" || !got.DefaultOn {
		t.Fatalf("%+v", got)
	}
	it, _ := s.AddCatalogItem(ctx, CatalogItem{OrgID: "org1", Kind: CatalogRepo, Name: "storefront", CreatedAt: now})
	e, _, err := s.AddEnvEntry(ctx, EnvEntry{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", ItemID: it.ID, Enabled: true, UpdatedAt: now, GroupID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.EnvEntryByID(ctx, e.ID); got.GroupID != g.ID {
		t.Fatalf("entry group %+v", got)
	}
	if err := s.SetEnvPersonGroup(ctx, alice.ID, "sp1", g.ID, false, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvPersonGroup(ctx, alice.ID, "sp1", g.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if ch, _ := s.EnvPersonGroups(ctx, alice.ID, "sp1"); len(ch) != 1 || !ch[g.ID] {
		t.Fatalf("choice %+v", ch)
	}
	if err := s.DeleteEnvGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.EnvEntryByID(ctx, e.ID); got.GroupID != "" {
		t.Fatalf("the entry stays without a group: %+v", got)
	}
	if ch, _ := s.EnvPersonGroups(ctx, alice.ID, "sp1"); len(ch) != 0 {
		t.Fatalf("choices go with the group: %+v", ch)
	}
	if all, _ := s.EnvGroupsOfOrg(ctx, "org1"); len(all) != 0 {
		t.Fatalf("%+v", all)
	}
}

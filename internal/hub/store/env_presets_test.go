package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestEnvPresetsSQLite(t *testing.T)   { exerciseEnvPresets(t, openTest(t)) }
func TestEnvPresetsPostgres(t *testing.T) { exerciseEnvPresets(t, openPostgres(t)) }

func exerciseEnvPresets(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	now := time.UnixMilli(1_700_000_000_000).UTC()
	a, _, err := s.AddEnvGroup(ctx, EnvGroup{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", Name: "Бэкенд", DefaultOn: true, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.AddEnvGroup(ctx, EnvGroup{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", Name: "Фронт", DefaultOn: true, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := s.AddEnvPreset(ctx, EnvPreset{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", Name: "Разработчик", GroupIDs: []string{a.ID, b.ID}, CreatedAt: now, UpdatedAt: now})
	if err != nil || !ok {
		t.Fatalf("add %v %v", ok, err)
	}
	if _, ok, _ := s.AddEnvPreset(ctx, EnvPreset{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", Name: "Разработчик", CreatedAt: now, UpdatedAt: now}); ok {
		t.Fatal("a layer has one preset of a name")
	}
	p.Hint, p.GroupIDs = "всё сразу", []string{b.ID}
	if err := s.UpdateEnvPreset(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.EnvPresetByID(ctx, p.ID); got.Hint != "всё сразу" || !slices.Equal(got.GroupIDs, []string{b.ID}) {
		t.Fatalf("%+v", got)
	}
	p.GroupIDs = []string{a.ID, b.ID}
	if err := s.UpdateEnvPreset(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEnvGroup(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.EnvPresetByID(ctx, p.ID); !slices.Equal(got.GroupIDs, []string{b.ID}) {
		t.Fatalf("a deleted group leaves the preset: %+v", got)
	}
	if err := s.DeleteEnvPreset(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.EnvPresetsOfOrg(ctx, "org1"); len(all) != 0 {
		t.Fatalf("%+v", all)
	}
}

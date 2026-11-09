package store

import (
	"context"
	"testing"
)

func TestEfficiencySQLite(t *testing.T)   { exerciseEfficiency(t, openTest(t)) }
func TestEfficiencyPostgres(t *testing.T) { exerciseEfficiency(t, openPostgres(t)) }

func exerciseEfficiency(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	if v, err := s.NewestEffFactAt(ctx, "mr"); err != nil || v != 0 {
		t.Fatalf("empty: %d %v", v, err)
	}
	facts := []EffFact{{Source: "mr", Key: "a!1", At: 100, Data: `{"x":1}`}, {Source: "mr", Key: "a!2", At: 200, Data: `{}`}, {Source: "comment", Key: "c", At: 150, Data: `{}`}}
	if err := s.UpsertEffFacts(ctx, facts, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEffFacts(ctx, []EffFact{{Source: "mr", Key: "a!1", At: 120, Data: `{"x":2}`}}, 2); err != nil {
		t.Fatal(err)
	}
	got, err := s.EffFactsBetween(ctx, "mr", 0, 300)
	if err != nil || len(got) != 2 || got[0].Data != `{"x":2}` || got[0].At != 120 {
		t.Fatalf("%v %+v", err, got)
	}
	if got, _ := s.EffFactsBetween(ctx, "mr", 130, 300); len(got) != 1 || got[0].Key != "a!2" {
		t.Fatalf("range: %+v", got)
	}
	if v, _ := s.NewestEffFactAt(ctx, "mr"); v != 200 {
		t.Fatalf("newest %d", v)
	}
	// A failure keeps the last success time.
	if err := s.RecordEffSource(ctx, EffSourceState{Source: "mrs", CollectedAt: 10, OK: true, Facts: 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordEffSource(ctx, EffSourceState{Source: "mrs", CollectedAt: 20, Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.EffSourceStates(ctx)
	if err != nil || len(st) != 1 || st[0].OK || st[0].OKAt != 10 || st[0].Error != "boom" || st[0].CollectedAt != 20 {
		t.Fatalf("%v %+v", err, st)
	}
	if err := s.PutEffPulse(ctx, "u", "2026-11-02", 6, 3, 3, 1); err == nil {
		t.Fatal("6 must be refused")
	}
	for i := 0; i < 2; i++ {
		if err := s.PutEffPulse(ctx, "u", "2026-11-02", 4, 3, 2, int64(i)); err != nil {
			t.Fatal(err)
		}
	}
}

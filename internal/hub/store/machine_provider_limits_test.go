package store

import (
	"context"
	"testing"
	"time"
)

func TestProviderLimitsSQLite(t *testing.T)   { exerciseProviderLimits(t, openTest(t)) }
func TestProviderLimitsPostgres(t *testing.T) { exerciseProviderLimits(t, openPostgres(t)) }

func exerciseProviderLimits(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	alice := &User{Email: "alice@example.com", Name: "Alice"}
	bob := &User{Email: "bob@example.com"}
	for _, u := range []*User{alice, bob} {
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	org := &Org{Slug: "acme", Name: "Acme", CreatedBy: alice.ID}
	if err := s.CreateOrg(ctx, org); err != nil {
		t.Fatal(err)
	}
	space := &Space{Slug: "acme-team", Name: "Acme", CreatedBy: alice.ID}
	if err := s.CreateSpace(ctx, space); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSpaceOrg(ctx, space.ID, org.ID); err != nil {
		t.Fatal(err)
	}
	mk := func(key, by string) *Machine {
		m := &Machine{SpaceID: space.ID, Profile: "dir", Via: "command", KeyPub: "k" + key, EncPub: "e" + key, CreatedBy: by}
		if err := s.CreateMachine(ctx, m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	m1, m2, mb := mk("1", alice.ID), mk("2", alice.ID), mk("3", bob.ID)

	if _, err := s.FullestProviderWindow(ctx, org.ID, alice.ID, "claude", "seven_day", 0); err != ErrNotFound {
		t.Fatalf("no reading yet: %v", err)
	}
	put := func(m *Machine, provider, win string, used float64, resets, at int64) {
		t.Helper()
		if err := s.PutProviderWindow(ctx, ProviderWindow{MachineID: m.ID, Provider: provider, Window: win, UsedPct: used, ResetsAt: resets, SampledAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	put(m1, "claude", "seven_day", 40, 1000, 10_000)
	put(m1, "claude", "seven_day", 42.5, 1100, 20_000) // replaces
	put(m2, "claude", "seven_day", 61, 1200, 20_000)
	put(m2, "codex", "seven_day", 99, 1300, 20_000)
	put(m2, "claude", "five_hour", 90, 1400, 20_000)
	put(mb, "claude", "seven_day", 95, 1500, 20_000) // another person's machine

	got, err := s.FullestProviderWindow(ctx, org.ID, alice.ID, "claude", "seven_day", 0)
	if err != nil || got.MachineID != m2.ID || got.UsedPct != 61 || got.ResetsAt != 1200 {
		t.Fatalf("fullest %+v, %v", got, err)
	}
	// A stale reading is not the person's limit now.
	got, err = s.FullestProviderWindow(ctx, org.ID, alice.ID, "claude", "seven_day", 20_001)
	if err != ErrNotFound {
		t.Fatalf("stale %+v, %v", got, err)
	}
	if _, err := s.FullestProviderWindow(ctx, "other-org", alice.ID, "claude", "seven_day", 0); err != ErrNotFound {
		t.Fatalf("other organization: %v", err)
	}
	all, err := s.ProviderWindowsOfMachine(ctx, m1.ID)
	if err != nil || len(all) != 1 || all[0].UsedPct != 42.5 {
		t.Fatalf("machine 1 windows %+v, %v", all, err)
	}
	// A revoked machine does not count.
	if _, err := s.RevokeMachine(ctx, m2.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err = s.FullestProviderWindow(ctx, org.ID, alice.ID, "claude", "seven_day", 0)
	if err != nil || got.MachineID != m1.ID {
		t.Fatalf("after revoke %+v, %v", got, err)
	}
}

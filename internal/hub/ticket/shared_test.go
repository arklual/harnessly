package ticket

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// twoReplicas opens the same database twice, as two replicas of the hub do: SQLite by default, a throwaway schema of
// HUB_TEST_POSTGRES_URL when it is set.
func twoReplicas(t *testing.T) (a, b *store.DB) {
	t.Helper()
	ctx := context.Background()
	dsn := "sqlite://" + filepath.Join(t.TempDir(), "hub.db")
	if pg := os.Getenv("HUB_TEST_POSTGRES_URL"); pg != "" {
		admin, err := sql.Open("pgx", pg)
		if err != nil {
			t.Fatal(err)
		}
		schema := "hubtest_" + store.RandomBase32(8)
		if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
		u, err := url.Parse(pg)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	}
	open := func() *store.DB {
		db, err := store.Open(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	return open(), open()
}

func TestTicketOfOneReplicaIsTradedOnceByAnother(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	dbA, dbB := twoReplicas(t)
	a, b := NewShared(dbA, clock), NewShared(dbB, clock)

	tk := a.Issue("u1", "m1")
	if u, ok := b.Redeem(tk, "m1"); !ok || u != "u1" {
		t.Fatalf("the other replica did not take the ticket: %q %v", u, ok)
	}
	if _, ok := a.Redeem(tk, "m1"); ok {
		t.Error("the replica that gave the ticket took it a second time")
	}
	if _, ok := b.Redeem(tk, "m1"); ok {
		t.Error("the replica that took the ticket took it again")
	}

	// A ticket shown for another machine is spent, on every replica.
	tk = a.Issue("u1", "m1")
	if _, ok := b.Redeem(tk, "m2"); ok {
		t.Error("redeemed for another machine")
	}
	if _, ok := a.Redeem(tk, "m1"); ok {
		t.Error("redeemed after a wrong try on another replica")
	}

	// A ticket older than its minute is refused by the replica that did not give it.
	tk = a.Issue("u1", "m1")
	now = now.Add(TTL + time.Second)
	if _, ok := b.Redeem(tk, "m1"); ok {
		t.Error("an old ticket was accepted")
	}
	if _, ok := b.Redeem("nonsense", "m1"); ok {
		t.Error("nonsense accepted")
	}
}

func TestTicketShownToBothReplicasAtOnceOpensOne(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	dbA, dbB := twoReplicas(t)
	stores := []*Store{NewShared(dbA, clock), NewShared(dbB, clock)}
	for round := 0; round < 20; round++ {
		tk := stores[round%2].Issue("u1", "m1")
		var wg sync.WaitGroup
		var mu sync.Mutex
		taken := 0
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(s *Store) {
				defer wg.Done()
				if _, ok := s.Redeem(tk, "m1"); ok {
					mu.Lock()
					taken++
					mu.Unlock()
				}
			}(stores[i%2])
		}
		wg.Wait()
		if taken != 1 {
			t.Fatalf("round %d: the ticket was taken %d times", round, taken)
		}
	}
}

func TestOpenedRecentlyIsSeenByEveryReplicaAndSwept(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	dbA, dbB := twoReplicas(t)
	a, b := NewShared(dbA, clock), NewShared(dbB, clock)

	if b.OpenedRecently("u1", "m1") {
		t.Fatal("opened before any ticket")
	}
	a.Issue("u1", "m1")
	if !b.OpenedRecently("u1", "m1") {
		t.Error("the other replica does not know the person opened the machine")
	}
	if b.OpenedRecently("u2", "m1") || b.OpenedRecently("u1", "m2") {
		t.Error("the note covers another person or another machine")
	}
	if a.Len() != 1 {
		t.Errorf("%d live tickets, want 1", a.Len())
	}
	now = now.Add(OpenedFor + time.Minute)
	if b.OpenedRecently("u1", "m1") {
		t.Error("the note outlived its twelve hours")
	}
	b.Sweep()
	var n int
	if err := dbA.QueryRow(context.Background(), `SELECT COUNT(*) FROM entry_tickets`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows left after the sweep (%v)", n, err)
	}
}

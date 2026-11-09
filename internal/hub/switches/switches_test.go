package switches

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func open(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMostSpecificRowWins(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	g := New(open(t), func() time.Time { return now })

	if g.On(ctx, "f", "a@example.com", "m1") {
		t.Fatal("no row must mean off")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(g.Set(ctx, "f", "", "", true, "everyone", "test"))
	if !g.On(ctx, "f", "a@example.com", "m1") {
		t.Fatal("the * row must turn it on")
	}
	// The person's row beats the machine's, the pair's beats both.
	must(g.Set(ctx, "f", "A@Example.com", "", false, "", "test"))
	must(g.Set(ctx, "f", "", "m2", true, "", "test"))
	if g.On(ctx, "f", "a@example.com", "m2") {
		t.Fatal("the person's row must beat the machine's")
	}
	must(g.Set(ctx, "f", "a@example.com", "m2", true, "", "test"))
	if !g.On(ctx, "f", "a@example.com", "m2") {
		t.Fatal("the pair's row must win")
	}
	if g.On(ctx, "f", "a@example.com", "m1") {
		t.Fatal("the person's off row must hold on another machine")
	}
	if !g.On(ctx, "f", "b@example.com", "m1") {
		t.Fatal("another person keeps the * row")
	}
	must(g.Remove(ctx, "f", "a@example.com", ""))
	if !g.On(ctx, "f", "a@example.com", "m1") {
		t.Fatal("without the person's row the * row decides again")
	}
}

func TestByPrefixAndCache(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	db := open(t)
	g := New(db, func() time.Time { return now })
	for _, r := range []struct {
		f, p string
		on   bool
	}{{"x-b", "", true}, {"x-a", "", true}, {"x-c", "", true}, {"x-c", "a@example.com", false}, {"y-d", "", true}} {
		if err := g.Set(ctx, r.f, r.p, "", r.on, "", "test"); err != nil {
			t.Fatal(err)
		}
	}
	if got := g.ByPrefix(ctx, "x-", "a@example.com", "m"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("a: %v", got)
	}
	if got := g.ByPrefix(ctx, "x-", "b@example.com", "m"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("b: %v", got)
	}
	g.On(ctx, "x-a", "b@example.com", "m")
	// Another replica's change is seen after the TTL, not before.
	if err := db.S().SetSwitch(ctx, store.Switch{Feature: "x-a", Enabled: false, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if !g.On(ctx, "x-a", "b@example.com", "m") {
		t.Fatal("the cached answer must hold within the TTL")
	}
	now = now.Add(CacheTTL + time.Second)
	if g.On(ctx, "x-a", "b@example.com", "m") {
		t.Fatal("after the TTL the table is read again")
	}
	rows, err := g.List(ctx, "y-")
	if err != nil || len(rows) != 1 || rows[0].Feature != "y-d" || rows[0].Person != "*" {
		t.Fatalf("list: %v %v", rows, err)
	}
}

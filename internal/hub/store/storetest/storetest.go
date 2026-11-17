// Package storetest gives a test a hub database of its own, ready to use: a copy of an SQLite file migrated once a
// process, or a throwaway schema of HUB_TEST_POSTGRES_URL when it is set.
//
// Migrating a database per test took 2 s of every test under -race (modernc.org/sqlite is Go, and the race detector
// slows it 25 times): the hub packages spent minutes on it. A copy of the migrated file opens in a fraction
// of that. The migrations and the schema check run once, on the template; a test of the migrations themselves opens
// its own database with store.Open.
package storetest

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

var (
	templateOnce  sync.Once
	templateBytes []byte
	templateErr   error
)

// template is an empty hub database with every migration applied and the schema checked, made once a process.
func template() ([]byte, error) {
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "storetest-template-")
		if err != nil {
			templateErr = err
			return
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "hub.db")
		db, err := store.OpenWith(context.Background(), "sqlite://"+path, store.Options{Check: store.CheckStrict})
		if err != nil {
			templateErr = err
			return
		}
		// Close folds the write-ahead log into the file, so the file alone is the whole database.
		if err := db.Close(); err != nil {
			templateErr = err
			return
		}
		templateBytes, templateErr = os.ReadFile(path)
	})
	return templateBytes, templateErr
}

// SQLiteURL writes a fresh copy of the migrated database into the test's temp dir and returns its sqlite:// URL, for
// code that opens the database by URL itself (a hub config, a command).
func SQLiteURL(t testing.TB) string {
	t.Helper()
	raw, err := template()
	if err != nil {
		t.Fatalf("storetest: migrate the template: %v", err)
	}
	path := filepath.Join(t.TempDir(), "hub.db")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return "sqlite://" + path
}

// Open returns a migrated, empty hub database closed when the test ends: SQLite by default, a schema of its own in
// HUB_TEST_POSTGRES_URL when that is set.
func Open(t testing.TB) *store.DB {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("HUB_TEST_POSTGRES_URL")
	if dsn == "" {
		// The copy is the template, checked when it was made: the check again per test is the time this package saves.
		db, err := store.OpenWith(ctx, SQLiteURL(t), store.Options{Check: store.CheckOff})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "hubtest_" + store.RandomBase32(8)
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

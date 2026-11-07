package designaccess

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Row ids of the audit log and of guest comments start at the millisecond and three random digits; a millisecond
// whose thousand ids are all taken must still take the write, and a foreign key must still fail it. With
// HUB_TEST_POSTGRES_URL set the same runs on Postgres, where a failed statement would abort the transaction.

func openRowDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("HUB_TEST_POSTGRES_URL")
	if dsn == "" {
		db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
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
	schema := "designaccess_" + store.RandomBase32(8)
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

func rowTx(t *testing.T, db *store.DB, m store.Machine, now time.Time, fn func(t *txn) error) error {
	t.Helper()
	ctx := context.Background()
	return db.Tx(ctx, func(st store.Store) error {
		return fn(&txn{ctx: ctx, st: st, q: st.Q(), pg: db.Driver() == store.Postgres, m: m, now: now})
	})
}

func TestRowIDsSurviveAFullMillisecond(t *testing.T) {
	db := openRowDB(t)
	ctx := context.Background()
	now := time.UnixMilli(1_790_000_000_123).UTC()
	m := store.Machine{ID: "m1"}
	if _, err := db.Exec(ctx, `INSERT INTO machines (id, profile, via, key_pub, enc_pub, created_at) VALUES (?, 'p', 'v', 'k1', 'e1', ?)`, m.ID, ms(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO design_share_links (id, machine_id, project_id, token_hash, rights, created_by, created_at) VALUES (?, ?, 'shop', ?, ?, 'owner@example.com', ?)`, "l0", m.ID, "h0", roleComment, ms(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO design_share_links (id, machine_id, project_id, token_hash, rights, created_by, created_at) VALUES (?, ?, 'shop', ?, ?, 'owner@example.com', ?)`, "l1", m.ID, "h1", roleComment, ms(now)); err != nil {
		t.Fatal(err)
	}
	// Every id of this millisecond is taken in both tables (the comments on another link, under its limit of comments).
	base := now.UnixMilli() * 1000
	err := rowTx(t, db, m, now, func(t *txn) error {
		for i := int64(0); i < 1000; i++ {
			if _, err := t.q.Exec(t.ctx, `INSERT INTO design_access_log (id, machine_id, project_id, actor, action, at) VALUES (?, ?, 'shop', 'a', 'fill', ?)`, base+i, m.ID, ms(now)); err != nil {
				return err
			}
			if _, err := t.q.Exec(t.ctx, `INSERT INTO design_guest_comments (id, link_id, board, text, guest_id, guest_name, created_at) VALUES (?, 'l0', 'b', 'fill', 'g', 'G', ?)`, base+i, ms(now)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// An audit record and a guest comment with its own audit record, all in this millisecond, in one transaction.
	var made guestComment
	err = rowTx(t, db, m, now, func(t *txn) error {
		if err := t.writeLog("shop", "owner@example.com", "grant", "member@example.com", nil); err != nil {
			return err
		}
		var err error
		made, err = t.addGuestComment(&shareTarget{ID: "l1", Project: "shop"}, commentInput{Name: "Guest", Text: "Hi", Board: "b", GuestID: "g1"})
		return err
	})
	if err != nil {
		t.Fatalf("writes in a full millisecond: %v", err)
	}
	if made.ID < base+1000 || made.ID > maxSafeID {
		t.Fatalf("comment id %d is not past the taken ones", made.ID)
	}
	var logs, comments int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM design_access_log WHERE action IN ('grant', 'share.comment')`).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM design_guest_comments WHERE text = 'Hi'`).Scan(&comments); err != nil {
		t.Fatal(err)
	}
	if logs != 2 || comments != 1 {
		t.Fatalf("kept %d audit records and %d comments, want 2 and 1", logs, comments)
	}

	// A foreign key is still an error, and so is a closed context.
	err = rowTx(t, db, store.Machine{ID: "no-such-machine"}, now, func(t *txn) error {
		return t.writeLog("shop", "a", "grant", "", nil)
	})
	if err == nil {
		t.Fatal("an audit record of a machine that does not exist was written")
	}
	err = rowTx(t, db, m, now, func(t *txn) error {
		_, err := t.addGuestComment(&shareTarget{ID: "no-such-link", Project: "shop"}, commentInput{Name: "G", Text: "x", Board: "b", GuestID: "g"})
		return err
	})
	if err == nil {
		t.Fatal("a comment on a link that does not exist was written")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	err = rowTx(t, db, m, now, func(t *txn) error {
		t.ctx = cctx
		return t.writeLog("shop", "a", "grant", "", nil)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a closed context: %v", err)
	}
}

func TestRowIDStopsAtJavaScriptsSafeIntegers(t *testing.T) {
	db := openRowDB(t)
	err := rowTx(t, db, store.Machine{ID: "m1"}, time.UnixMilli(maxSafeID/1000+1), func(t *txn) error {
		_, err := t.insertRow(`INSERT INTO design_access_log (id, machine_id, project_id, actor, action, at) VALUES (?, ?, 'p', 'a', 'x', 0)`, "m1")
		return err
	})
	if !errors.Is(err, errNoRowID) {
		t.Fatalf("an id past the safe integers: %v", err)
	}
}

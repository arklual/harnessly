package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// eachDB runs a test of the store's contract on SQLite and, when HUB_TEST_POSTGRES_URL is set, on Postgres: what the
// hub relies on must hold on both.
func eachDB(t *testing.T, fn func(t *testing.T, db *DB)) {
	t.Run("sqlite", func(t *testing.T) { fn(t, openTest(t)) })
	t.Run("postgres", func(t *testing.T) { fn(t, openPostgres(t)) })
}

// testMachine makes a machine: the cursors of spend belong to one.
func testMachine(t *testing.T, db *DB) string {
	t.Helper()
	m := Machine{Name: "m", Hostname: "box", Profile: "dir", Via: "command", KeyPub: RandomBase32(16), EncPub: RandomBase32(16)}
	if err := db.S().CreateMachine(context.Background(), &m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func TestTxRetryRunsALostTransactionAgain(t *testing.T) {
	eachDB(t, func(t *testing.T, db *DB) {
		ctx := context.Background()
		machine := testMachine(t, db)
		runs := 0
		err := db.TxRetry(ctx, func(s Store) error {
			runs++
			if err := s.SetCursor(ctx, Cursor{MachineID: machine, Stream: "try", Epoch: "e", Seq: int64(runs)}); err != nil {
				return err
			}
			if runs < 3 {
				return ErrConflict
			}
			return nil
		})
		if err != nil || runs != 3 {
			t.Fatalf("runs %d, err %v", runs, err)
		}
		// What the lost runs wrote was rolled back with them.
		if c, _ := db.S().CursorOf(ctx, machine, "try"); c.Seq != 3 {
			t.Errorf("cursor %d, want the last run's 3", c.Seq)
		}

		runs = 0
		err = db.TxRetry(ctx, func(Store) error { runs++; return ErrConflict })
		if !errors.Is(err, ErrConflict) || runs != txAttempts {
			t.Errorf("a transaction that always loses: runs %d (want %d), err %v", runs, txAttempts, err)
		}

		runs = 0
		other := errors.New("not a conflict")
		if err := db.TxRetry(ctx, func(Store) error { runs++; return other }); !errors.Is(err, other) || runs != 1 {
			t.Errorf("another error must not be retried: runs %d, err %v", runs, err)
		}
	})
}

func TestIngestCursorMovesOnlyForward(t *testing.T) {
	eachDB(t, func(t *testing.T, db *DB) {
		ctx := context.Background()
		machine := testMachine(t, db)
		s := db.S()
		set := func(epoch string, seq int64, acked *int64) {
			t.Helper()
			if err := s.SetCursor(ctx, Cursor{MachineID: machine, Stream: "usage", Epoch: epoch, Seq: seq, AckedAt: acked}); err != nil {
				t.Fatal(err)
			}
		}
		at := func() Cursor {
			t.Helper()
			c, err := s.CursorOf(ctx, machine, "usage")
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
		set("e1", 10, nil)
		set("e1", 4, nil)
		if c := at(); c.Seq != 10 {
			t.Errorf("a slower writer took the cursor back to %d", c.Seq)
		}
		acked := int64(77)
		set("e1", 10, &acked)
		if c := at(); c.AckedAt == nil || *c.AckedAt != 77 {
			t.Errorf("the ack of the same position was not kept: %+v", c)
		}
		set("e1", 12, nil)
		set("e2", 1, nil)
		if c := at(); c.Epoch != "e2" || c.Seq != 1 {
			t.Errorf("a new epoch starts a new count: %+v", c)
		}
	})
}

func TestHistoryCursorMovesOnlyFromWhatWasRead(t *testing.T) {
	eachDB(t, func(t *testing.T, db *DB) {
		ctx := context.Background()
		s := db.S()
		now := time.UnixMilli(1_800_000_000_000)
		if _, ok, err := s.HistoryCursorOf(ctx, "m", "c"); ok || err != nil {
			t.Fatalf("a channel never applied: ok %v, err %v", ok, err)
		}
		if err := s.MoveHistoryCursor(ctx, "m", "c", nil, HistoryCursor{Epoch: "e", Seq: 5}, now); err != nil {
			t.Fatal(err)
		}
		// Another transaction made the cursor first: the one that saw none loses.
		if err := s.MoveHistoryCursor(ctx, "m", "c", nil, HistoryCursor{Epoch: "e", Seq: 3}, now); !errors.Is(err, ErrConflict) {
			t.Errorf("a second first cursor: %v", err)
		}
		read := HistoryCursor{Epoch: "e", Seq: 5}
		if err := s.MoveHistoryCursor(ctx, "m", "c", &read, HistoryCursor{Epoch: "e", Seq: 9}, now); err != nil {
			t.Fatal(err)
		}
		// A transaction that read 5 while the cursor went to 9 must not put it at 7.
		if err := s.MoveHistoryCursor(ctx, "m", "c", &read, HistoryCursor{Epoch: "e", Seq: 7}, now); !errors.Is(err, ErrConflict) {
			t.Errorf("a stale move: %v", err)
		}
		if c, _, _ := s.HistoryCursorOf(ctx, "m", "c"); c.Seq != 9 {
			t.Errorf("cursor %d, want 9", c.Seq)
		}
	})
}

// Writers that read the cursor, add to it and move it, all at once: every one of them lands, none is lost, because a
// writer that lost the cursor runs again from its read.
func TestConcurrentCursorWritersAllLand(t *testing.T) {
	eachDB(t, func(t *testing.T, db *DB) {
		ctx := context.Background()
		now := time.UnixMilli(1_800_000_000_000)
		if err := db.S().MoveHistoryCursor(ctx, "m", "c", nil, HistoryCursor{Epoch: "e", Seq: 0}, now); err != nil {
			t.Fatal(err)
		}
		const writers, rounds = 4, 3
		var wg sync.WaitGroup
		errs := make(chan error, writers*rounds)
		for range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range rounds {
					// More tries than TxRetry gives one call: the test wants every writer to land in the end.
					var err error
					for range 20 {
						err = db.TxRetry(ctx, func(s Store) error {
							cur, _, err := s.HistoryCursorOf(ctx, "m", "c")
							if err != nil {
								return err
							}
							return s.MoveHistoryCursor(ctx, "m", "c", &cur, HistoryCursor{Epoch: "e", Seq: cur.Seq + 1}, now)
						})
						if !Retriable(err) {
							break
						}
					}
					if err != nil {
						errs <- err
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if c, _, _ := db.S().HistoryCursorOf(ctx, "m", "c"); c.Seq != writers*rounds {
			t.Errorf("cursor %d, want %d: a move was lost", c.Seq, writers*rounds)
		}
	})
}

// Notifications reach a listener of the same schema on Postgres; a SQLite file has one process and says so.
func TestNotifyReachesAListener(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		db := openTest(t)
		if db.CanListen() {
			t.Error("a SQLite file cannot carry notifications")
		}
		if err := db.Listen(context.Background(), nil, func(string) {}, nil); !errors.Is(err, ErrNoListen) {
			t.Errorf("Listen on SQLite: %v", err)
		}
	})
	t.Run("postgres", func(t *testing.T) {
		db, other := openPostgres(t), openPostgres(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		up, got, foreign := make(chan struct{}, 1), make(chan string, 4), make(chan string, 4)
		go func() { _ = db.Listen(ctx, func() { up <- struct{}{} }, func(p string) { got <- p }, nil) }()
		otherUp := make(chan struct{}, 1)
		go func() { _ = other.Listen(ctx, func() { otherUp <- struct{}{} }, func(p string) { foreign <- p }, nil) }()
		for _, ch := range []chan struct{}{up, otherUp} {
			select {
			case <-ch:
			case <-time.After(10 * time.Second):
				t.Fatal("the listener did not come up")
			}
		}
		if err := db.Notify(ctx, "hello"); err != nil {
			t.Fatal(err)
		}
		select {
		case p := <-got:
			if p != "hello" {
				t.Errorf("payload %q", p)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the notification did not arrive")
		}
		select {
		case p := <-foreign:
			t.Errorf("a hub of another schema heard %q", p)
		case <-time.After(300 * time.Millisecond):
		}
	})
}

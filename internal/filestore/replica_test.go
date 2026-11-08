package filestore

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Several hub replicas on one Postgres: the commit decision and the wake-ups must hold across processes, not only
// inside one.

func pgRig(t *testing.T) *rig {
	t.Helper()
	if os.Getenv(envPG) == "" {
		t.Skipf("%s unset: the multi-replica tests need a Postgres", envPG)
	}
	return newRig(t, combo{"postgres", "file"})
}

// replica is a second hub process: its own connections, its own notifier, the same database and blobs.
func (r *rig) replica(t *testing.T, noListen bool) *Store {
	t.Helper()
	m, err := openPostgresMetaOpts(context.Background(), r.dbURL, noListen, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	rm, err := m.Room(context.Background(), DefaultRoom)
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(DefaultRoom, rm, r.blobs, Design{})
}

func TestReplicasAgreeOnWhoWins(t *testing.T) {
	r := pgRig(t)
	stores := []*Store{r.store(DefaultRoom)}
	for i := 0; i < 3; i++ {
		stores = append(stores, r.replica(t, false))
	}
	e0, _ := write(t, stores[0], "p/hot.html", 0, "start")

	// Many writers on every replica, all on the same base: exactly one gets the revision.
	var wins, conflicts atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, conflict := write(t, stores[i%len(stores)], "p/hot.html", e0.Rev, fmt.Sprintf("writer %d", i))
			if conflict {
				conflicts.Add(1)
			} else {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || conflicts.Load() != 39 {
		t.Fatalf("across replicas: wins=%d conflicts=%d", wins.Load(), conflicts.Load())
	}

	// Distinct paths from every replica: revisions are handed out once each, with no gaps.
	revs := make(chan int64, 60)
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, conflict := write(t, stores[i%len(stores)], fmt.Sprintf("p/f%d.html", i), 0, fmt.Sprintf("c%d", i))
			if conflict {
				t.Errorf("distinct paths conflicted")
			}
			revs <- e.Rev
		}()
	}
	wg.Wait()
	close(revs)
	seen := map[int64]bool{}
	for rev := range revs {
		if seen[rev] {
			t.Fatalf("revision %d handed out twice", rev)
		}
		seen[rev] = true
	}
	for rev := int64(3); rev < 63; rev++ { // 1 start, 2 the winner, then 60
		if !seen[rev] {
			t.Fatalf("revision %d never handed out", rev)
		}
	}
	// A reader on any replica sees all of it.
	for i, s := range stores {
		ch, err := s.Changes(context.Background(), 0, 1000)
		if err != nil || len(ch.Entries) != 61 || ch.Rev != 62 {
			t.Fatalf("replica %d sees %d entries, rev %d: %v", i, len(ch.Entries), ch.Rev, err)
		}
	}
}

func TestRoomKeyIsMintedOnceAcrossReplicas(t *testing.T) {
	r := pgRig(t)
	open := func() *Store {
		m, err := openPostgresMetaOpts(context.Background(), r.dbURL, true, "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { m.Close() })
		rm, _ := m.Room(context.Background(), "uploads")
		return NewStore("uploads", rm, r.blobs, Files{})
	}
	a, b := open(), open()
	_ = a.RegisterMember(bg, "vm1/ann", "age1a")
	_ = a.RegisterMember(bg, "vm2/ann", "age1b")
	var ok atomic.Int32
	var wg sync.WaitGroup
	for _, c := range []struct {
		s  *Store
		me string
	}{{a, "vm1/ann"}, {b, "vm2/ann"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.s.PutKey(bg, c.me, "k-"+c.me, c.me) == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("two machines starting an empty room on two replicas must mint one key, minted %d", ok.Load())
	}
}

func waitWake(t *testing.T, ch <-chan struct{}, within time.Duration) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(within):
		return false
	}
}

func TestLongPollWakesAcrossReplicasByNotify(t *testing.T) {
	// Before the rig: its replicas read the interval when they open, and put it back only once they are closed.
	old := pgPollEvery
	pgPollEvery = time.Hour // only LISTEN/NOTIFY can wake in this test
	t.Cleanup(func() { pgPollEvery = old })
	r := pgRig(t)
	writer := r.replica(t, false)
	reader := r.replica(t, false)
	time.Sleep(500 * time.Millisecond) // LISTEN is up
	wake := reader.Wake()
	go func() { time.Sleep(100 * time.Millisecond); write(t, writer, "p/a.html", 0, "x") }()
	if !waitWake(t, wake, 3*time.Second) {
		t.Fatal("a commit on one replica must wake a long-poll on another through NOTIFY")
	}
}

func TestLongPollWakesAcrossReplicasByPollingWhenNotifyIsGone(t *testing.T) {
	// Before the rig: its replicas read the interval when they open, and put it back only once they are closed.
	old := pgPollEvery
	pgPollEvery = 100 * time.Millisecond
	t.Cleanup(func() { pgPollEvery = old })
	r := pgRig(t)
	writer := r.replica(t, true)
	reader := r.replica(t, true) // no LISTEN connection at all
	time.Sleep(300 * time.Millisecond)
	wake := reader.Wake()
	go func() { time.Sleep(100 * time.Millisecond); write(t, writer, "p/a.html", 0, "x") }()
	if !waitWake(t, wake, 3*time.Second) {
		t.Fatal("without NOTIFY the poll must still wake the long-poll")
	}
}

func TestReplicasStartingTogetherCreateTheSchemaOnce(t *testing.T) {
	r := pgRig(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := openPostgresMetaOpts(context.Background(), r.dbURL, true, "")
			if err != nil {
				errs <- err
				return
			}
			defer m.Close()
			for _, room := range []string{DefaultRoom, "profile-x"} {
				if _, err := m.Room(context.Background(), room); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

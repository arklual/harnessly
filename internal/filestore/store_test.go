package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var bg = context.Background()

func TestCommitIsCompareAndSwap(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		e1, conflict := write(t, s, "p/a.html", 0, "one")
		if conflict || e1.Rev != 1 {
			t.Fatalf("first write: %+v conflict=%v", e1, conflict)
		}
		// A write on a stale base is refused and answered with the current revision.
		cur, conflict := write(t, s, "p/a.html", 0, "stale")
		if !conflict || cur.Rev != 1 {
			t.Fatalf("stale write must conflict with rev 1, got %+v conflict=%v", cur, conflict)
		}
		e2, conflict := write(t, s, "p/a.html", 1, "two")
		if conflict || e2.Rev != 2 {
			t.Fatalf("second write: %+v conflict=%v", e2, conflict)
		}
		// Same contents again: no new revision.
		same, conflict := write(t, s, "p/a.html", 2, "two")
		if conflict || same.Rev != 2 {
			t.Fatalf("a write that changes nothing must answer rev 2, got %+v", same)
		}
		// Delete, then re-create from base 0 (a machine that never saw the file).
		del, _, err := s.Commit(bg, CommitRequest{Path: "p/a.html", Base: 2, Deleted: true}, "m1")
		if err != nil || !del.Deleted || del.Rev != 3 {
			t.Fatalf("delete: %+v %v", del, err)
		}
		back, conflict := write(t, s, "p/a.html", 0, "again")
		if conflict || back.Rev != 4 {
			t.Fatalf("re-create after delete: %+v conflict=%v", back, conflict)
		}
		// Deleting what never existed records nothing.
		gone, _, err := s.Commit(bg, CommitRequest{Path: "p/never.html", Deleted: true}, "m1")
		if err != nil || gone.Rev != 0 || !gone.Deleted {
			t.Fatalf("delete of nothing: %+v %v", gone, err)
		}
		// A writer on a revision this hub never had (a hub restored from an older backup) is taken.
		ahead, conflict := write(t, s, "p/b.html", 99, "from the future")
		if conflict || ahead.Rev != 5 {
			t.Fatalf("restored-hub write: %+v conflict=%v", ahead, conflict)
		}
		hist, err := s.History(bg, "p/a.html", 10)
		if err != nil || len(hist) != 4 || hist[0].Rev != 4 {
			t.Fatalf("history: %+v %v", hist, err)
		}
		// Refused writes.
		if _, _, err := s.Commit(bg, CommitRequest{Path: ".hidden", Sha: sum([]byte("x")), Size: 1}, "m"); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("hidden path: %v", err)
		}
		if _, _, err := s.Commit(bg, CommitRequest{Path: "p/c.html", Sha: sum([]byte("never uploaded")), Size: 1}, "m"); !errors.Is(err, ErrNoBlob) {
			t.Fatalf("missing blob: %v", err)
		}
	})
}

func TestRevisionsAreContiguousUnderConcurrentWriters(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		const writers = 24
		var wg sync.WaitGroup
		revs := make(chan int64, writers)
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				e, conflict := write(t, s, fmt.Sprintf("p/f%d.html", i), 0, fmt.Sprintf("content %d", i))
				if conflict {
					t.Errorf("distinct paths must not conflict")
				}
				revs <- e.Rev
			}()
		}
		wg.Wait()
		close(revs)
		seen := map[int64]bool{}
		for rev := range revs {
			seen[rev] = true
		}
		for rev := int64(1); rev <= writers; rev++ {
			if !seen[rev] {
				t.Fatalf("revision %d was never handed out: %v", rev, seen)
			}
		}
	})
}

func TestExactlyOneOfManyWritersOnTheSameBaseWins(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		e0, _ := write(t, s, "p/hot.html", 0, "start")
		const writers = 16
		var wins, conflicts atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, conflict := write(t, s, "p/hot.html", e0.Rev, fmt.Sprintf("writer %d", i))
				if conflict {
					conflicts.Add(1)
				} else {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		if wins.Load() != 1 || conflicts.Load() != writers-1 {
			t.Fatalf("wins=%d conflicts=%d, want 1 and %d", wins.Load(), conflicts.Load(), writers-1)
		}
	})
}

func TestChangesCursorAndWake(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		write(t, s, "p/a.html", 0, "a1")
		write(t, s, "p/b.html", 0, "b1")
		write(t, s, "p/a.html", 1, "a2") // a superseded: one entry
		ch, err := s.Changes(bg, 0, 100)
		if err != nil || len(ch.Entries) != 2 || ch.Rev != 3 || ch.More {
			t.Fatalf("changes: %+v %v", ch, err)
		}
		if ch.Entries[0].Path != "p/b.html" || ch.Entries[1].Path != "p/a.html" {
			t.Fatalf("oldest first expected: %+v", ch.Entries)
		}
		page, _ := s.Changes(bg, 0, 1)
		if len(page.Entries) != 1 || !page.More {
			t.Fatalf("a cut answer says More: %+v", page)
		}
		after, _ := s.Changes(bg, 3, 100)
		if len(after.Entries) != 0 || after.Rev != 3 {
			t.Fatalf("nothing after the cursor: %+v", after)
		}
		wake := s.Wake()
		select {
		case <-wake:
			t.Fatal("woke without a write")
		default:
		}
		go func() { time.Sleep(50 * time.Millisecond); write(t, s, "p/c.html", 0, "c1") }()
		select {
		case <-wake:
		case <-time.After(5 * time.Second):
			t.Fatal("a write must wake the waiters")
		}
	})
}

func TestMembersKeysAndApproval(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store("uploads")
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(s.RegisterMember(bg, "vm1/ann", "age1aaa"))
		must(s.RegisterMember(bg, "vm2/ann", "age1bbb"))
		if err := s.PutKey(bg, "vm2/ann", "wrapped-2", "vm2/ann"); err != nil {
			t.Fatalf("the first key of an empty room is self-minted: %v", err)
		}
		// The room has a key now: another member cannot mint its own.
		if err := s.PutKey(bg, "vm1/ann", "mine", "vm1/ann"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("a second self-minted key must be refused: %v", err)
		}
		// A holder wraps it for the other.
		must(s.PutKey(bg, "vm1/ann", "wrapped-1", "vm2/ann"))
		if err := s.PutKey(bg, "stranger", "x", "vm2/ann"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("not a member: %v", err)
		}
		ms, _ := s.Members(bg)
		if len(ms) != 2 || !ms[0].HasKey || !ms[1].HasKey {
			t.Fatalf("members: %+v", ms)
		}
		if k, _ := s.Key(bg, "vm1/ann"); k != "wrapped-1" {
			t.Fatalf("key: %q", k)
		}
		// A new recipient drops the member's wrapped key.
		must(s.RegisterMember(bg, "vm1/ann", "age1new"))
		if k, _ := s.Key(bg, "vm1/ann"); k != "" {
			t.Fatalf("key must be dropped with the old recipient, got %q", k)
		}
		must(s.RegisterMember(bg, "vm1/ann", "age1new")) // same again: nothing
		if a, _ := s.Approval(bg); a != "auto" {
			t.Fatalf("approval: %s", a)
		}
		must(s.RequireManualApproval(bg))
		if a, _ := s.Approval(bg); a != "manual" {
			t.Fatalf("approval: %s", a)
		}
	})
}

func TestBlobs(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		a, b := r.store("design"), r.store("other")
		sha := put(t, a, "hello")
		for i := 0; i < 2; i++ { // putting again is harmless
			if err := a.PutBlob(bg, sha, strings.NewReader("hello")); err != nil {
				t.Fatal(err)
			}
		}
		if ok, err := a.HasBlob(bg, sha); !ok || err != nil {
			t.Fatalf("has: %v %v", ok, err)
		}
		f, err := a.OpenBlob(bg, sha)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(f)
		f.Close()
		if string(got) != "hello" {
			t.Fatalf("read back %q", got)
		}
		// Contents that do not hash to their name are refused and leave nothing behind.
		bad := sum([]byte("something else"))
		if err := a.PutBlob(bg, bad, strings.NewReader("hello")); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("wrong hash must be a bad request: %v", err)
		}
		if ok, _ := a.HasBlob(bg, bad); ok {
			t.Fatal("a refused blob must not be stored")
		}
		if err := a.PutBlob(bg, "nothex", strings.NewReader("x")); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("bad sha: %v", err)
		}
		if _, err := a.OpenBlob(bg, sum([]byte("absent"))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing blob must be os.ErrNotExist: %v", err)
		}
		// Rooms do not share contents: a hash known in one room opens nothing in another.
		if ok, _ := b.HasBlob(bg, sha); ok {
			t.Fatal("blobs of one room visible in another")
		}
		// Too big.
		big := strings.Repeat("x", MaxFileSize+1)
		if err := a.PutBlob(bg, sum([]byte(big)), strings.NewReader(big)); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("a file over the limit must be refused: %v", err)
		}
	})
}

func TestLegacySQLiteFolderOpensAndKeepsItsRevisions(t *testing.T) {
	// A hub folder written by the version before the drivers: no tag column, no counters table.
	dir := t.TempDir()
	legacy, err := openLegacy(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cur, err := st.Current(bg, "p/old.html")
	if err != nil || cur == nil || cur.Rev != 7 {
		t.Fatalf("legacy file: %+v %v", cur, err)
	}
	e, conflict := write(t, st, "p/new.html", 0, "new")
	if conflict || e.Rev != 8 {
		t.Fatalf("the next revision continues after the legacy ones: %+v", e)
	}
}

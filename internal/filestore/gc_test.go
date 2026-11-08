package filestore

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func blobExists(t *testing.T, r *rig, room, data string) bool {
	t.Helper()
	ok, err := r.blobs.Has(context.Background(), room, sum([]byte(data)))
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestRetentionAndGarbageCollection(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		var base int64
		for i := 1; i <= 6; i++ { // path A: revisions 1..6, each with contents of its own
			e, _ := write(t, s, "p/a.html", base, fmt.Sprintf("a-v%d", i))
			base = e.Rev
		}
		bRev, _ := write(t, s, "p/b.html", 0, "b-only") // rev 7
		put(t, s, "orphan")                             // uploaded, never committed
		ret := Retention{Keep: 2, Grace: 20 * time.Millisecond}

		// Dry run changes nothing.
		if _, err := RunGC(bg, r.meta, r.blobs, ret, true); err != nil {
			t.Fatal(err)
		}
		if h, _ := s.History(bg, "p/a.html", 100); len(h) != 6 {
			t.Fatalf("dry run pruned history: %d", len(h))
		}

		// Pass 1: old revisions go, their blobs are only marked (inside the grace period).
		reps, err := RunGC(bg, r.meta, r.blobs, ret, false)
		if err != nil || len(reps) != 1 {
			t.Fatalf("gc: %+v %v", reps, err)
		}
		if reps[0].RevisionsPruned != 4 || reps[0].BlobsDeleted != 0 {
			t.Fatalf("pass 1: %+v", reps[0])
		}
		h, _ := s.History(bg, "p/a.html", 100)
		if len(h) != 2 || h[0].Rev != 6 || h[1].Rev != 5 {
			t.Fatalf("the newest two revisions stay: %+v", h)
		}
		for _, d := range []string{"a-v1", "a-v2", "a-v3", "a-v4", "orphan"} {
			if !blobExists(t, r, DefaultRoom, d) {
				t.Fatalf("%s deleted inside the grace period", d)
			}
		}
		// The agents' view is untouched: current files and the cursor.
		ch, _ := s.Changes(bg, 0, 100)
		if len(ch.Entries) != 2 || ch.Rev != bRev.Rev {
			t.Fatalf("changes after pruning: %+v", ch)
		}

		// A blob that gets referenced again inside the grace period is rescued.
		rescued := sum([]byte("a-v1"))
		if _, conflict, err := s.Commit(bg, commitOf("p/c.html", 0, rescued, 4), "m"); err != nil || conflict {
			t.Fatalf("re-reference: %v %v", conflict, err)
		}

		time.Sleep(40 * time.Millisecond)
		reps, err = RunGC(bg, r.meta, r.blobs, ret, false)
		if err != nil {
			t.Fatal(err)
		}
		if reps[0].BlobsDeleted != 4 { // a-v2, a-v3, a-v4, orphan
			t.Fatalf("pass 2: %+v", reps[0])
		}
		for _, d := range []string{"a-v2", "a-v3", "a-v4", "orphan"} {
			if blobExists(t, r, DefaultRoom, d) {
				t.Fatalf("%s should be collected", d)
			}
		}
		// Never a blob some kept revision refers to.
		for _, d := range []string{"a-v1", "a-v5", "a-v6", "b-only"} {
			if !blobExists(t, r, DefaultRoom, d) {
				t.Fatalf("%s is referenced and must stay", d)
			}
		}
		// Still works end to end after the collection.
		if e, conflict := write(t, s, "p/a.html", 6, "a-v7"); conflict || e.Rev != 9 {
			t.Fatalf("write after gc: %+v %v", e, conflict)
		}
	})
}

func TestDefaultRetentionKeepsEverything(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		var base int64
		for i := 1; i <= 5; i++ {
			e, _ := write(t, s, "p/a.html", base, fmt.Sprintf("v%d", i))
			base = e.Rev
		}
		for i := 0; i < 2; i++ {
			reps, err := RunGC(bg, r.meta, r.blobs, Retention{Grace: time.Millisecond}, false)
			if err != nil || reps[0].RevisionsPruned != 0 || reps[0].BlobsDeleted != 0 {
				t.Fatalf("nothing is dropped by default: %+v %v", reps, err)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if h, _ := s.History(bg, "p/a.html", 100); len(h) != 5 {
			t.Fatalf("history: %d", len(h))
		}
	})
}

func TestRetentionByAge(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		write(t, s, "p/a.html", 0, "old")
		write(t, s, "p/a.html", 1, "old2")
		write(t, s, "p/a.html", 2, "now")
		rm, _ := r.meta.Room(bg, DefaultRoom)
		// Pretend the cutoff is in the future: everything but the current revision is older than it.
		n, err := rm.Prune(bg, 0, time.Now().Add(time.Hour).UnixMilli())
		if err != nil || n != 2 {
			t.Fatalf("prune by age: %d %v", n, err)
		}
		// Count and age protect together: the newest 2 stay even though both are older than the cutoff.
		write(t, s, "p/a.html", 3, "now2")
		n, err = rm.Prune(bg, 2, time.Now().Add(time.Hour).UnixMilli())
		if err != nil || n != 0 {
			t.Fatalf("keep=2 over 2 revisions: %d %v", n, err)
		}
	})
}

func TestOnlyOneProcessCollectsAtATime(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		unlock, ok, err := r.meta.TryLock(bg, "gc")
		if err != nil || !ok {
			t.Fatalf("first lock: %v %v", ok, err)
		}
		reps, err := RunGC(bg, r.meta, r.blobs, Retention{}, false)
		if err != nil || reps != nil {
			t.Fatalf("a held lock makes the pass skip: %+v %v", reps, err)
		}
		unlock()
		if _, ok, _ := r.meta.TryLock(bg, "gc"); !ok {
			t.Fatal("lock must be free after unlock")
		}
	})
}

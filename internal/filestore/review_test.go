package filestore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// A commit that refers to a blob the collector is about to delete either wins (the blob stays) or is refused with
// ErrNoBlob; it never succeeds on a blob that is gone.
func TestCommitRacingTheCollectorNeverLeavesAMissingBlob(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		ret := Retention{Grace: time.Millisecond}
		kept, refused := 0, 0
		for i := 0; i < 15; i++ {
			data := fmt.Sprintf("candidate %d", i)
			sha := put(t, s, data)                                            // uploaded, not referenced: a candidate
			if _, err := RunGC(bg, r.meta, r.blobs, ret, false); err != nil { // pass 1 marks it
				t.Fatal(err)
			}
			time.Sleep(3 * time.Millisecond)
			var wg sync.WaitGroup
			var cerr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, err := RunGC(bg, r.meta, r.blobs, ret, false)
				if err != nil {
					t.Error(err)
				}
			}()
			go func() {
				defer wg.Done()
				_, _, cerr = s.Commit(bg, commitOf(fmt.Sprintf("p/f%d.html", i), 0, sha, int64(len(data))), "m")
			}()
			wg.Wait()
			ok, _ := s.HasBlob(bg, sha)
			switch {
			case cerr == nil && !ok:
				t.Fatalf("round %d: the commit succeeded but its blob is gone", i)
			case cerr == nil:
				kept++
			case cerr == ErrNoBlob:
				refused++
			default:
				t.Fatal(cerr)
			}
		}
		t.Logf("commit won %d times, was refused %d times", kept, refused)
	})
}

func TestDeleteUnreferencedChecksUnderTheLock(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		sha := put(t, s, "held")
		write(t, s, "p/a.html", 0, "held")
		rm, _ := r.meta.Room(bg, DefaultRoom)
		deleted, err := rm.DeleteUnreferenced(bg, sha, func(context.Context) error { t.Fatal("deleted a referenced blob"); return nil })
		if err != nil || deleted {
			t.Fatalf("referenced blob: %v %v", deleted, err)
		}
	})
}

// Two room names with the same long prefix (and any short hash of it) get schemas of their own.
func TestRoomsWithTheSamePrefixGetDifferentSchemas(t *testing.T) {
	r := pgRig(t)
	prefix := strings.Repeat("a", 45)
	a, b := prefix+"-one", prefix+"-two"
	pm := r.meta.(*pgMeta)
	sa, err := pm.schemaOf(bg, a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := pm.schemaOf(bg, b)
	if err != nil {
		t.Fatal(err)
	}
	if sa == sb {
		t.Fatalf("both rooms share schema %s", sa)
	}
	if again, _ := pm.schemaOf(bg, a); again != sa {
		t.Fatalf("a room keeps its schema: %s then %s", sa, again)
	}
	write(t, r.store(a), "p/a.html", 0, "in a")
	if ch, _ := r.store(b).Changes(bg, 0, 10); len(ch.Entries) != 0 {
		t.Fatal("room b sees room a's files")
	}
	if ch, _ := r.store(a).Changes(bg, 0, 10); len(ch.Entries) != 1 {
		t.Fatal("room a lost its file")
	}
}

// A wrapped key the source dropped (the member got a new recipient) does not survive a second migrate.
func TestMigrateAgainDropsKeysTheSourceDropped(t *testing.T) {
	src := newRig(t, combo{"sqlite", "file"})
	dst := newRig(t, combo{"sqlite", "file"})
	s := src.store("uploads")
	_ = s.RegisterMember(bg, "vm1/ann", "age1a")
	_ = s.PutKey(bg, "vm1/ann", "wrapped", "vm1/ann")
	migrateRigs(t, src, dst)
	_ = s.RegisterMember(bg, "vm1/ann", "age1new") // drops the key
	migrateRigs(t, src, dst)
	if k, _ := dst.store("uploads").Key(bg, "vm1/ann"); k != "" {
		t.Fatalf("stale wrapped key at the destination: %q", k)
	}
}

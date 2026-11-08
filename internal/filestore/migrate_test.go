package filestore

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// populate fills a rig with two rooms (a design library with history and a deletion, a room of files with members
// and a key, as a design-sync hub kept them), and a never-committed blob.
func populate(t *testing.T, r *rig) {
	t.Helper()
	d := r.store(DefaultRoom)
	var base int64
	for _, v := range []string{"v1", "v2", "v3"} {
		e, _ := write(t, d, "proj/index.html", base, v)
		base = e.Rev
	}
	write(t, d, "proj/_ds/kit.js", 0, strings.Repeat("kit ", 1000))
	e, _ := write(t, d, "proj/gone.png", 0, "png")
	if _, _, err := d.Commit(bg, CommitRequest{Path: "proj/gone.png", Base: e.Rev, Deleted: true}, "m1"); err != nil {
		t.Fatal(err)
	}
	put(t, d, "uploaded but never committed")

	p := r.store("uploads")
	write(t, p, ".claude/settings.json", 0, "sealed-1")
	for name, rec := range map[string]string{"vm1/ann": "age1aaa", "vm2/ann": "age1bbb"} {
		if err := p.RegisterMember(bg, name, rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.PutKey(bg, "vm1/ann", "wrapped-1", "vm1/ann"); err != nil {
		t.Fatal(err)
	}
	if err := p.RequireManualApproval(bg); err != nil {
		t.Fatal(err)
	}
}

type snapshot struct {
	changes map[string]Changes
	history map[string][]Entry
	state   map[string]RoomState
	blobs   map[string][]string
}

func snap(t *testing.T, r *rig) snapshot {
	t.Helper()
	s := snapshot{changes: map[string]Changes{}, history: map[string][]Entry{}, state: map[string]RoomState{}, blobs: map[string][]string{}}
	rooms, err := r.meta.Rooms(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, room := range rooms {
		st := r.store(room)
		ch, err := st.Changes(bg, 0, 100000)
		if err != nil {
			t.Fatal(err)
		}
		s.changes[room] = ch
		rm, _ := r.meta.Room(bg, room)
		s.history[room], err = rm.ExportHistory(bg, 0, 100000)
		if err != nil {
			t.Fatal(err)
		}
		s.state[room], err = rm.ExportState(bg)
		if err != nil {
			t.Fatal(err)
		}
		var shas []string
		if err := r.blobs.List(bg, room, func(b Blob) error { shas = append(shas, b.Sha); return nil }); err != nil {
			t.Fatal(err)
		}
		sortStrings(shas)
		s.blobs[room] = shas
	}
	return s
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func migrateRigs(t *testing.T, from, to *rig) MigrateReport {
	t.Helper()
	rep, err := Migrate(bg, MigrateOptions{FromMeta: from.meta, ToMeta: to.meta, FromBlobs: from.blobs, ToBlobs: to.blobs})
	if err != nil {
		t.Fatalf("migrate %s -> %s: %v", from.c, to.c, err)
	}
	return rep
}

func TestMigrateRoundTripBetweenEveryPairOfDrivers(t *testing.T) {
	cs := combos(t)
	if len(cs) == 1 {
		t.Logf("only %s is available here: the round trip runs between two instances of it", cs[0])
	}
	for _, c := range cs {
		for _, other := range append(cs, c) { // every pair, and each combination to a second instance of itself
			t.Run(c.String()+" to "+other.String(), func(t *testing.T) {
				src := newRig(t, c)
				populate(t, src)
				want := snap(t, src)

				dst := newRig(t, other)
				rep := migrateRigs(t, src, dst)
				if !rep.Verified || rep.Rooms != 2 || rep.History != 7 || rep.BlobsCopied != rep.Blobs || rep.Blobs == 0 {
					t.Fatalf("report: %+v", rep)
				}
				got := snap(t, dst)
				if !reflect.DeepEqual(want, got) {
					t.Fatalf("the destination differs from the source:\nwant %+v\ngot  %+v", want, got)
				}

				// Idempotent: a second run writes no blob and changes nothing.
				rep = migrateRigs(t, src, dst)
				if rep.BlobsCopied != 0 || !rep.Verified {
					t.Fatalf("second run: %+v", rep)
				}
				if again := snap(t, dst); !reflect.DeepEqual(want, again) {
					t.Fatal("the second run changed the destination")
				}

				// The destination takes over: revisions continue, a stale base still conflicts, files read back.
				d := dst.store(DefaultRoom)
				cur, _ := d.Current(bg, "proj/index.html")
				if cur.Rev != 3 {
					t.Fatalf("current: %+v", cur)
				}
				if _, conflict := write(t, d, "proj/index.html", 1, "stale"); !conflict {
					t.Fatal("a stale write must conflict on the destination")
				}
				e, conflict := write(t, d, "proj/index.html", 3, "v4")
				if conflict || e.Rev != 7 {
					t.Fatalf("the next revision continues the source's numbering: %+v %v", e, conflict)
				}
				f, err := d.OpenBlob(bg, cur.Sha)
				if err != nil {
					t.Fatal(err)
				}
				f.Close()
				if a, _ := dst.store("uploads").Approval(bg); a != "manual" {
					t.Fatal("manual approval must survive the move")
				}

				// And back again, to a third instance: the round trip.
				back := newRig(t, c)
				migrateRigs(t, dst, back)
				if !reflect.DeepEqual(snap(t, back), snap(t, dst)) {
					t.Fatal("round trip differs")
				}
			})
		}
	}
}

func TestMigrateRefusesACorruptBlob(t *testing.T) {
	src := newRig(t, combo{"sqlite", "file"})
	populate(t, src)
	sha := sum([]byte("v2"))
	path := filepath.Join(strings.TrimPrefix(src.storage, "file://"), "blobs", sha[:2], sha)
	if err := os.WriteFile(path, []byte("bit rot"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := newRig(t, combo{"sqlite", "file"})
	_, err := Migrate(bg, MigrateOptions{FromMeta: src.meta, ToMeta: dst.meta, FromBlobs: src.blobs, ToBlobs: dst.blobs})
	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("a blob that does not hash to its name must stop the migration, got %v", err)
	}
	if ok, _ := dst.blobs.Has(bg, DefaultRoom, sha); ok {
		t.Fatal("the corrupt blob was copied")
	}
}

func TestMigrateRefusesADestinationWithOtherHistory(t *testing.T) {
	src := newRig(t, combo{"sqlite", "file"})
	dst := newRig(t, combo{"sqlite", "file"})
	write(t, src.store(DefaultRoom), "p/a.html", 0, "source")
	write(t, dst.store(DefaultRoom), "p/a.html", 0, "somebody else's revision 1")
	if _, err := Migrate(bg, MigrateOptions{FromMeta: src.meta, ToMeta: dst.meta}); err == nil {
		t.Fatal("two different revision 1s must not be merged silently")
	}
}

func TestMigrateReportsRevisionsWhoseContentsTheSourceLost(t *testing.T) {
	src := newRig(t, combo{"sqlite", "file"})
	write(t, src.store(DefaultRoom), "p/a.html", 0, "gone")
	sha := sum([]byte("gone"))
	_ = src.blobs.Delete(bg, DefaultRoom, sha)
	dst := newRig(t, combo{"sqlite", "file"})
	rep := migrateRigs(t, src, dst)
	if rep.MissingInSrc != 1 {
		t.Fatalf("missing: %+v", rep)
	}
}

func TestBackupIsAHubFolderAndCopiesOnlyNewBlobs(t *testing.T) {
	src := newRig(t, combo{"sqlite", "file"})
	populate(t, src)
	want := snap(t, src)
	dir := filepath.Join(t.TempDir(), "backup")

	sm := src.meta
	rep, err := Backup(bg, sm, src.blobs, dir)
	if err != nil || rep.BlobsCopied == 0 || rep.Rooms != 2 {
		t.Fatalf("backup: %+v %v", rep, err)
	}
	// The backup is a data folder: open it as a hub would.
	bk := &rig{t: t, c: combo{"sqlite", "file"}, dbURL: "sqlite://" + dir, storage: "file://" + dir}
	bk.meta, bk.blobs = bk.open()
	defer bk.meta.Close()
	if got := snap(t, bk); !reflect.DeepEqual(want, got) {
		t.Fatalf("backup differs from the hub:\nwant %+v\ngot  %+v", want, got)
	}

	// Again with one new blob and one new revision: only the blob moves.
	write(t, src.store(DefaultRoom), "proj/index.html", 3, "v4")
	rep, err = Backup(bg, sm, src.blobs, dir)
	if err != nil || rep.BlobsCopied != 1 || rep.BlobsKept != len(want.blobs[DefaultRoom])+len(want.blobs["uploads"]) {
		t.Fatalf("second backup: %+v %v", rep, err)
	}
	bk.meta.Close() // a hub started from the backup opens the files as they are now
	bk.meta, bk.blobs = bk.open()
	if cur, _ := bk.store(DefaultRoom).Current(bg, "proj/index.html"); cur == nil || cur.Rev != 7 {
		t.Fatalf("the second backup carries the new revision: %+v", cur)
	}
}

func TestBackupRefusesPostgres(t *testing.T) {
	r := pgRig(t)
	if _, err := Backup(context.Background(), r.meta, r.blobs, t.TempDir()); err == nil {
		t.Fatal("backup of Postgres must say to use pg_dump")
	}
}

package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
)

// MigrateOptions says what to copy. FromMeta is always needed (it names the rooms); ToMeta nil copies contents only,
// ToBlobs nil (with FromBlobs) copies metadata only.
type MigrateOptions struct {
	FromMeta, ToMeta   MetaStore
	FromBlobs, ToBlobs BlobStore
	// Workers is how many blobs are copied at once (default 4).
	Workers int
	// Rooms, when set, are the only rooms copied (the others stay where they are).
	Rooms []string
	// Into names the destination room of a source room (nil: the same name): how a store that ran inside one
	// runtime moves into a hub, where its rooms are a space's.
	Into func(room string) string
}

// target is the destination room of a source room.
func (o MigrateOptions) target(room string) string {
	if o.Into != nil {
		return o.Into(room)
	}
	return room
}

// MigrateReport is what was copied and what the destination was then checked against.
type MigrateReport struct {
	Rooms        int
	History      int64
	Files        int64
	Members      int64
	Keys         int64
	Blobs        int64 // blobs in the source
	BlobsCopied  int64 // of them, written to the destination now (the rest were there already)
	BlobBytes    int64
	MissingInSrc int64 // referenced by a revision but absent from the source (the source was already missing them)
	Verified     bool
}

// Migrate copies a hub from one pair of drivers to another and checks the copy. It is safe to run again: it adds
// only what the destination lacks, and every run ends with the same checks (revisions equal row by row, blobs
// re-hashed on the destination, counts equal). It does not delete anything from either side. Run it with the source
// hub stopped for the final pass (or run it twice: a second pass copies what the first one missed).
func Migrate(ctx context.Context, o MigrateOptions) (MigrateReport, error) {
	var rep MigrateReport
	if o.FromMeta == nil || (o.FromBlobs == nil) != (o.ToBlobs == nil) {
		return rep, fmt.Errorf("migrate: a source database is needed, and both ends of the storage pair or neither")
	}
	if o.ToMeta == nil && o.ToBlobs == nil {
		return rep, fmt.Errorf("migrate: nothing to copy")
	}
	if o.Workers <= 0 {
		o.Workers = 4
	}
	rooms, err := o.FromMeta.Rooms(ctx)
	if err != nil {
		return rep, err
	}
	if o.Rooms != nil {
		want := map[string]bool{}
		for _, r := range o.Rooms {
			want[r] = true
		}
		kept := rooms[:0]
		for _, r := range rooms {
			if want[r] {
				kept = append(kept, r)
			}
		}
		rooms = kept
	}
	for _, room := range rooms {
		if t := o.target(room); !ValidRoom(t) {
			return rep, fmt.Errorf("room %s: the destination name %q is not a room name", room, t)
		}
	}
	for _, room := range rooms {
		rep.Rooms++
		if o.ToMeta != nil {
			if err := migrateMeta(ctx, o, room, &rep); err != nil {
				return rep, fmt.Errorf("room %s: %w", room, err)
			}
		}
		if o.FromBlobs != nil {
			if err := migrateBlobs(ctx, o, room, &rep); err != nil {
				return rep, fmt.Errorf("room %s: %w", room, err)
			}
		}
	}
	rep.Verified = true
	return rep, nil
}

const historyPage = 2000

func migrateMeta(ctx context.Context, o MigrateOptions, room string, rep *MigrateReport) error {
	src, err := o.FromMeta.Room(ctx, room)
	if err != nil {
		return err
	}
	dst, err := o.ToMeta.Room(ctx, o.target(room))
	if err != nil {
		return err
	}
	for after := int64(0); ; {
		page, err := src.ExportHistory(ctx, after, historyPage)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		if err := dst.ImportHistory(ctx, page); err != nil {
			return err
		}
		after = page[len(page)-1].Rev
	}
	st, err := src.ExportState(ctx)
	if err != nil {
		return err
	}
	if err := dst.ImportState(ctx, st); err != nil {
		return err
	}
	// Check: every revision of the source is in the destination, identical.
	for after := int64(0); ; {
		a, err := src.ExportHistory(ctx, after, historyPage)
		if err != nil {
			return err
		}
		if len(a) == 0 {
			break
		}
		b, err := exportExactly(ctx, dst, after, len(a))
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(a, b) {
			return fmt.Errorf("history differs after revision %d: the destination holds other data than the source", after)
		}
		after = a[len(a)-1].Rev
	}
	sc, err := src.Counts(ctx)
	if err != nil {
		return err
	}
	dc, err := dst.Counts(ctx)
	if err != nil {
		return err
	}
	// A destination that already took writes may hold more; it must never hold less.
	if dc.History < sc.History || dc.Files < sc.Files || dc.Members != sc.Members || dc.Keys != sc.Keys || dc.MaxRev < sc.MaxRev {
		return fmt.Errorf("counts differ: source %+v, destination %+v", sc, dc)
	}
	dstState, err := dst.ExportState(ctx)
	if err != nil {
		return err
	}
	if !sameMembersKeys(st, dstState) {
		return fmt.Errorf("members or wrapped keys differ from the source's after the copy")
	}
	for p, r := range st.Files {
		if dstState.Files[p] < r {
			return fmt.Errorf("file %s: destination at revision %d, source at %d", p, dstState.Files[p], r)
		}
	}
	rep.History += sc.History
	rep.Files += sc.Files
	rep.Members += sc.Members
	rep.Keys += sc.Keys
	slog.Info("migrated metadata", "room", room, "revisions", sc.History, "files", sc.Files)
	return nil
}

func exportExactly(ctx context.Context, m RoomMeta, after int64, n int) ([]Entry, error) {
	return m.ExportHistory(ctx, after, n)
}

func migrateBlobs(ctx context.Context, o MigrateOptions, room string, rep *MigrateReport) error {
	var list []Blob
	if err := o.FromBlobs.List(ctx, room, func(b Blob) error { list = append(list, b); return nil }); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, b := range list {
		have[b.Sha] = true
	}
	// Revisions that point at contents the source never had stay missing on both sides: report, do not fail.
	src, err := o.FromMeta.Room(ctx, room)
	if err != nil {
		return err
	}
	refs, err := src.ReferencedShas(ctx)
	if err != nil {
		return err
	}
	for sha := range refs {
		if !have[sha] {
			rep.MissingInSrc++
		}
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		first   error
		copied  int64
		bytes   int64
		jobs    = make(chan Blob)
		cctx, c = context.WithCancel(ctx)
	)
	defer c()
	fail := func(err error) {
		mu.Lock()
		if first == nil {
			first = err
			c()
		}
		mu.Unlock()
	}
	for i := 0; i < o.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range jobs {
				wrote, err := copyBlob(cctx, o, room, b)
				if err != nil {
					fail(fmt.Errorf("blob %s: %w", b.Sha, err))
					return
				}
				if wrote {
					atomic.AddInt64(&copied, 1)
				}
				atomic.AddInt64(&bytes, b.Size)
			}
		}()
	}
feed:
	for _, b := range list {
		select {
		case jobs <- b:
		case <-cctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if first != nil {
		return first
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rep.Blobs += int64(len(list))
	rep.BlobsCopied += copied
	rep.BlobBytes += bytes
	slog.Info("migrated blobs", "room", room, "blobs", len(list), "copied", copied)
	// Count check: everything the source listed is in the destination.
	n := 0
	if err := o.ToBlobs.List(ctx, o.target(room), func(b Blob) error {
		if have[b.Sha] {
			n++
		}
		return nil
	}); err != nil {
		return err
	}
	if n != len(list) {
		return fmt.Errorf("blob count differs: source %d, destination holds %d of them", len(list), n)
	}
	return nil
}

// hashOf reads contents through and returns their sha256.
func hashOf(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyBlob makes sure the destination holds the blob and that what it holds hashes to its name. The source is
// hashed too: a corrupt source blob must not be copied under a good name.
func copyBlob(ctx context.Context, o MigrateOptions, room string, b Blob) (wrote bool, err error) {
	src, err := o.FromBlobs.Open(ctx, room, b.Sha)
	if err != nil {
		return false, fmt.Errorf("read source: %w", err)
	}
	sum, err := hashOf(src)
	src.Close()
	if err != nil {
		return false, fmt.Errorf("read source: %w", err)
	}
	if sum != b.Sha {
		return false, fmt.Errorf("source blob is corrupt: contents hash to %s", sum)
	}
	ok, err := o.ToBlobs.Has(ctx, o.target(room), b.Sha)
	if err != nil {
		return false, err
	}
	if !ok {
		src, err := o.FromBlobs.Open(ctx, room, b.Sha)
		if err != nil {
			return false, fmt.Errorf("read source: %w", err)
		}
		err = o.ToBlobs.Put(ctx, o.target(room), b.Sha, src)
		src.Close()
		if err != nil {
			return false, fmt.Errorf("write destination: %w", err)
		}
		wrote = true
	}
	dst, err := o.ToBlobs.Open(ctx, o.target(room), b.Sha)
	if err != nil {
		return wrote, fmt.Errorf("read back destination: %w", err)
	}
	defer dst.Close()
	sum, err = hashOf(dst)
	if err != nil {
		return wrote, fmt.Errorf("read back destination: %w", err)
	}
	if sum != b.Sha {
		return wrote, fmt.Errorf("destination blob is corrupt: contents hash to %s", sum)
	}
	return wrote, nil
}

// sameMembersKeys: the destination's members and keys are exactly the source's (both listed in name order).
func sameMembersKeys(a, b RoomState) bool {
	if len(a.Members) != len(b.Members) || len(a.Keys) != len(b.Keys) {
		return false
	}
	for i := range a.Members {
		if a.Members[i] != b.Members[i] {
			return false
		}
	}
	for i := range a.Keys {
		if a.Keys[i] != b.Keys[i] {
			return false
		}
	}
	return true
}

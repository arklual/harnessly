package storehub

import (
	"context"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The store's retention, the same for `hub store gc` and the leader's collection (HUB_STORE_GC_HOURS): the newest
// GCKeep revisions of every file and those younger than GCDays stay, a blob nothing refers to goes after GCGrace.
const (
	GCKeep  = 20
	GCDays  = 30
	GCGrace = 24 * time.Hour
)

// gcFirst is how long after it starts leading a replica collects the store first: the hub is rolled out more often
// than it collects, and a timer that began anew on every leader would never come due.
const gcFirst = 10 * time.Minute

// RunGC collects the store until ctx ends, first gcFirst after it starts and then every `every`; the blobs the hub's
// database refers to stay (Pins). It is the leader's job: one replica collects, and the store's own lock keeps a
// `hub store gc` run at the same time out.
func (s *Store) RunGC(ctx context.Context, db *store.DB, every time.Duration) {
	ret := filestore.Retention{Keep: GCKeep, Days: GCDays, Grace: GCGrace}
	wait := min(gcFirst, every)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		start := time.Now()
		reps, err := filestore.RunGCPinned(ctx, s.index, s.reader, ret, Pins(db, s.legacy), false)
		var blobs, kept int
		var revs, freed int64
		for _, r := range reps {
			revs, blobs, kept, freed = revs+r.RevisionsPruned, blobs+r.BlobsDeleted, kept+r.BlobsKept, freed+r.BytesFreed
		}
		if err != nil {
			s.log.Error("store gc", "rooms", len(reps), "error", err)
			continue
		}
		s.log.Info("store gc", "rooms", len(reps), "revisions_pruned", revs, "blobs_deleted", blobs, "bytes_freed", freed,
			"blobs_in_grace", kept, "took", time.Since(start).Round(time.Millisecond))
	}
}

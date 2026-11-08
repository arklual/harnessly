package filestore

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Retention says how much history a hub keeps. The zero value keeps everything, which is what a hub did before it
// had the setting.
type Retention struct {
	// Keep: the newest N revisions of every path stay (0: no protection by count).
	Keep int
	// Days: revisions younger than this stay (0: no protection by age).
	Days int
	// Grace: a blob nothing references is deleted only after it has been unreferenced this long (default 24h).
	Grace time.Duration
}

// Active tells whether any history is ever dropped.
func (r Retention) Active() bool { return r.Keep > 0 || r.Days > 0 }

// GCReport is what one pass did.
type GCReport struct {
	Room            string
	RevisionsPruned int64
	BlobsSeen       int
	BlobsMarked     int
	BlobsDeleted    int
	BytesFreed      int64
	BlobsKept       int // unreferenced but still inside the grace period
}

// RunGC forgets old revisions (when retention is active) and deletes blobs no revision refers to. A blob is never
// deleted because it is old, only because nothing refers to it, and then only on a pass at least Grace after the
// first pass that saw it unreferenced and after one more look at the references right before the delete. A blob
// that a kept revision (current or in the history that stays) refers to is never touched. dry reports and changes
// nothing.
//
// It takes a lock first, so several replicas running it do not duplicate the work (the ones that lose skip it).
func RunGC(ctx context.Context, meta MetaStore, blobs BlobStore, ret Retention, dry bool) ([]GCReport, error) {
	unlock, ok, err := meta.TryLock(ctx, "gc")
	if err != nil {
		return nil, err
	}
	if !ok {
		slog.Info("gc: another process is running it")
		return nil, nil
	}
	defer unlock()
	if ret.Grace <= 0 {
		ret.Grace = 24 * time.Hour
	}
	rooms, err := meta.Rooms(ctx)
	if err != nil {
		return nil, err
	}
	var out []GCReport
	for _, room := range rooms {
		rep, err := gcRoom(ctx, meta, blobs, room, ret, dry)
		if err != nil {
			return out, fmt.Errorf("gc room %s: %w", room, err)
		}
		out = append(out, rep)
	}
	return out, nil
}

func gcRoom(ctx context.Context, meta MetaStore, blobs BlobStore, room string, ret Retention, dry bool) (GCReport, error) {
	rep := GCReport{Room: room}
	rm, err := meta.Room(ctx, room)
	if err != nil {
		return rep, err
	}
	now := time.Now()
	if ret.Active() && !dry {
		var before int64
		if ret.Days > 0 {
			before = now.Add(-time.Duration(ret.Days) * 24 * time.Hour).UnixMilli()
		}
		if rep.RevisionsPruned, err = rm.Prune(ctx, ret.Keep, before); err != nil {
			return rep, err
		}
	}
	// References first, listing second: a blob uploaded after the references were read is not in the set, but is
	// not old either, because its first mark is made now and it can only be deleted a grace period later.
	refs, err := rm.ReferencedShas(ctx)
	if err != nil {
		return rep, err
	}
	marks, err := rm.GCMarks(ctx)
	if err != nil {
		return rep, err
	}
	add := map[string]int64{}
	var drop []string
	var expired []Blob
	seen := map[string]bool{}
	err = blobs.List(ctx, room, func(b Blob) error {
		rep.BlobsSeen++
		seen[b.Sha] = true
		if _, ok := refs[b.Sha]; ok {
			if _, marked := marks[b.Sha]; marked {
				drop = append(drop, b.Sha) // referenced again: forgiven
			}
			return nil
		}
		since, marked := marks[b.Sha]
		switch {
		case !marked:
			add[b.Sha] = now.UnixMilli()
			rep.BlobsMarked++
		case now.Sub(time.UnixMilli(since)) >= ret.Grace:
			expired = append(expired, b)
		default:
			rep.BlobsKept++
		}
		return nil
	})
	if err != nil {
		return rep, err
	}
	for sha := range marks {
		if !seen[sha] {
			drop = append(drop, sha) // the blob is gone already
		}
	}
	for _, b := range expired {
		if dry {
			if used, err := rm.Referenced(ctx, b.Sha); err != nil {
				return rep, err
			} else if !used {
				rep.BlobsDeleted++
				rep.BytesFreed += b.Size
			}
			continue
		}
		// Check and delete under the room's write lock, the one a commit holds while it decides: a commit that
		// starts to refer to the blob either lands first (the blob stays) or finds it gone and is refused.
		gone, err := rm.DeleteUnreferenced(ctx, b.Sha, func(ctx context.Context) error {
			return blobs.Delete(ctx, room, b.Sha)
		})
		if err != nil {
			return rep, err
		}
		if gone {
			rep.BlobsDeleted++
			rep.BytesFreed += b.Size
		}
		drop = append(drop, b.Sha) // deleted, or referenced again: either way the mark is spent
	}
	if !dry {
		if err := rm.PutGCMarks(ctx, add, drop); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// GCLoop runs RunGC every interval until ctx ends.
func GCLoop(ctx context.Context, meta MetaStore, blobs BlobStore, ret Retention, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		reps, err := RunGC(ctx, meta, blobs, ret, false)
		if err != nil {
			slog.Error("gc", "err", err)
			continue
		}
		for _, r := range reps {
			if r.RevisionsPruned > 0 || r.BlobsDeleted > 0 {
				slog.Info("gc", "room", r.Room, "revisions_pruned", r.RevisionsPruned, "blobs_deleted", r.BlobsDeleted, "bytes_freed", r.BytesFreed)
			}
		}
	}
}

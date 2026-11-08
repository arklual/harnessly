package filestore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// BackupReport is what a backup pass wrote.
type BackupReport struct {
	Rooms       int
	BlobsCopied int
	BlobsKept   int // already in the backup
	BytesCopied int64
}

// Backup writes a consistent copy of a SQLite + folder hub into dir, which then is itself a data folder a hub (or
// `migrate`) can start from. Databases are copied with VACUUM INTO (a snapshot taken while the hub keeps writing);
// blobs are copied by hash, so only the ones the backup lacks cost anything. The databases go first: every blob a
// snapshot refers to is already stored, and a blob uploaded after the snapshot is only an extra.
func Backup(ctx context.Context, meta MetaStore, blobs BlobStore, dir string) (BackupReport, error) {
	var rep BackupReport
	sm, ok := meta.(*sqliteMeta)
	if !ok {
		return rep, errors.New("backup copies a SQLite hub; for Postgres use pg_dump and the bucket's own replication")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return rep, err
	}
	rooms, err := sm.Rooms(ctx)
	if err != nil {
		return rep, err
	}
	dest, err := NewFileBlobs(dir)
	if err != nil {
		return rep, err
	}
	for _, room := range rooms {
		file := filepath.Join(dir, "hub.db")
		if room != DefaultRoom {
			file = filepath.Join(dir, "rooms", room, "hub.db")
		}
		if err := sm.Snapshot(ctx, room, file); err != nil {
			return rep, err
		}
	}
	for _, room := range rooms {
		rep.Rooms++
		err := blobs.List(ctx, room, func(b Blob) error {
			if ok, _ := dest.Has(ctx, room, b.Sha); ok {
				rep.BlobsKept++
				return nil
			}
			src, err := blobs.Open(ctx, room, b.Sha)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil // deleted by a GC while we ran
				}
				return err
			}
			defer src.Close()
			if err := dest.Put(ctx, room, b.Sha, src); err != nil {
				return fmt.Errorf("blob %s: %w", b.Sha, err)
			}
			rep.BlobsCopied++
			rep.BytesCopied += b.Size
			return nil
		})
		if err != nil {
			return rep, err
		}
	}
	slog.Info("backup done", "dir", dir, "rooms", rep.Rooms, "blobs_copied", rep.BlobsCopied)
	return rep, nil
}

package sites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// MigrateReport is what Migrate did.
type MigrateReport struct {
	Found   int   // blob files in the folder
	Copied  int   // put into the destination
	Present int   // already there, left as they were
	Bytes   int64 // bytes copied
	Removed int   // source files removed (only with removeSource)
	Skipped []string
}

// Migrate copies every blob of the folder store at root into dst: the move of a hub's sites from its data folder to
// a bucket. It is safe to run again, and to run while the hub runs on the folder: a blob already in dst is left alone,
// and a file whose content does not hash to its name is skipped and reported, never copied. With dry nothing is
// written. With removeSource a blob is removed from the folder once the destination has it.
func Migrate(ctx context.Context, root string, dst Blobs, dry, removeSource bool) (MigrateReport, error) {
	var rep MigrateReport
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !d.Type().IsRegular() || !ValidHash(d.Name()) {
			return nil
		}
		hash := d.Name()
		rep.Found++
		has, err := dst.Has(ctx, hash)
		if err != nil {
			return fmt.Errorf("blob %s: %w", hash, err)
		}
		if !has {
			n, err := copyBlob(ctx, path, hash, dst, dry)
			if err != nil {
				return fmt.Errorf("blob %s: %w", hash, err)
			}
			if n < 0 {
				rep.Skipped = append(rep.Skipped, hash)
				return nil
			}
			rep.Copied++
			rep.Bytes += n
		} else {
			rep.Present++
		}
		if removeSource && !dry {
			if err := os.Remove(path); err != nil {
				return err
			}
			rep.Removed++
		}
		return nil
	})
	return rep, err
}

// copyBlob puts one file; -1 when the file is not what its name says.
func copyBlob(ctx context.Context, path, hash string, dst Blobs, dry bool) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return 0, err
	}
	if hex.EncodeToString(h.Sum(nil)) != hash {
		return -1, nil
	}
	if dry {
		return size, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	if err := dst.Put(ctx, hash, f, size); err != nil {
		return 0, err
	}
	return size, nil
}

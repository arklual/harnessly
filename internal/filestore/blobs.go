package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BlobStore keeps file contents by their sha256, one namespace per room (a member of one room never reads another
// room's contents by guessing a hash). Contents are immutable: a hash names the same bytes forever, so a store
// never needs a conditional write; putting what is already there is harmless.
type BlobStore interface {
	// Has tells whether the contents are stored.
	Has(ctx context.Context, room, sha string) (bool, error)
	// Open opens contents for reading; os.ErrNotExist when they are not stored.
	Open(ctx context.Context, room, sha string) (io.ReadCloser, error)
	// Put stores contents, refusing them (ErrBadRequest) when they do not hash to sha or are bigger than
	// MaxFileSize.
	Put(ctx context.Context, room, sha string, r io.Reader) error
	// Delete removes contents; deleting what is not there is not an error.
	Delete(ctx context.Context, room, sha string) error
	// List calls fn for every stored blob of the room.
	List(ctx context.Context, room string, fn func(Blob) error) error
	Close() error
}

// Blob is one stored contents as a listing shows it.
type Blob struct {
	Sha  string
	Size int64
	At   time.Time
}

// OpenBlobStore opens the store a URL names: `file:///path` (a folder) or
// `s3://bucket/prefix?endpoint=…&region=…&path_style=1`.
func OpenBlobStore(raw string) (BlobStore, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("storage %q: %w", redact(raw), err)
	}
	switch u.Scheme {
	case "file":
		if u.Path == "" {
			return nil, errors.New("storage file://: the folder is missing (file:///var/lib/harnessly-hub/store)")
		}
		return NewFileBlobs(u.Path)
	case "s3":
		return NewS3Blobs(u)
	}
	return nil, fmt.Errorf("storage %q: file:///path or s3://bucket/prefix?… expected", redact(raw))
}

// redact hides what a URL may carry in its user-info for error messages.
func redact(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		u.User = url.User("***")
		return u.String()
	}
	return raw
}

// copyVerified copies at most MaxFileSize bytes of r to w and checks they hash to sha.
func copyVerified(w io.Writer, r io.Reader, sha string) error {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return err
	}
	if n > MaxFileSize {
		return fmt.Errorf("%w: file bigger than %d bytes", ErrBadRequest, MaxFileSize)
	}
	if hex.EncodeToString(h.Sum(nil)) != sha {
		return fmt.Errorf("%w: contents do not hash to %s", ErrBadRequest, sha)
	}
	return nil
}

// --- a folder: blobs/<sha[:2]>/<sha>, written to a temp file, synced and renamed ---

type fileBlobs struct{ root string }

// NewFileBlobs keeps blobs under root: the room "design" in root/blobs (the layout of a design-sync hub), another room
// in root/rooms/<room>/blobs.
func NewFileBlobs(root string) (BlobStore, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &fileBlobs{root: root}, nil
}

func (f *fileBlobs) dir(room string) string {
	if room == DefaultRoom {
		return filepath.Join(f.root, "blobs")
	}
	return filepath.Join(f.root, "rooms", room, "blobs")
}

func (f *fileBlobs) path(room, sha string) string { return filepath.Join(f.dir(room), sha[:2], sha) }

func (f *fileBlobs) Close() error { return nil }

func (f *fileBlobs) Has(_ context.Context, room, sha string) (bool, error) {
	if !ValidSha(sha) {
		return false, nil
	}
	_, err := os.Stat(f.path(room, sha))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (f *fileBlobs) Open(_ context.Context, room, sha string) (io.ReadCloser, error) {
	if !ValidSha(sha) {
		return nil, os.ErrNotExist
	}
	return os.Open(f.path(room, sha))
}

func (f *fileBlobs) Put(ctx context.Context, room, sha string, r io.Reader) error {
	if !ValidSha(sha) {
		return fmt.Errorf("%w: sha", ErrBadRequest)
	}
	if ok, _ := f.Has(ctx, room, sha); ok {
		_, err := io.Copy(io.Discard, io.LimitReader(r, MaxFileSize+1))
		return err
	}
	dir := filepath.Dir(f.path(room, sha))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = copyVerified(tmp, r, sha)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path(room, sha))
}

func (f *fileBlobs) Delete(_ context.Context, room, sha string) error {
	if !ValidSha(sha) {
		return nil
	}
	err := os.Remove(f.path(room, sha))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (f *fileBlobs) List(ctx context.Context, room string, fn func(Blob) error) error {
	root := f.dir(room)
	shards, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, sh := range shards {
		if !sh.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, sh.Name()))
		if err != nil {
			return err
		}
		for _, e := range files {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !ValidSha(e.Name()) || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue // vanished under us
			}
			if err := fn(Blob{Sha: e.Name(), Size: info.Size(), At: info.ModTime()}); err != nil {
				return err
			}
		}
	}
	return nil
}

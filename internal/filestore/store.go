// Package filestore is the one place that knows the truth of a collection of files: every accepted revision of every
// file, and the contents by their sha256 (the center of design-sync, carried into harnessly).
//
// Where that truth lives is a choice made at start: the metadata (revisions, leases, members, keys) in SQLite or
// Postgres (MetaStore), the contents in a folder or an S3-compatible bucket (BlobStore). The wire API the machines
// speak is the same on every combination.
package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

var (
	// ErrBadRequest is a write that is malformed rather than stale.
	ErrBadRequest = errors.New("bad request")
	// ErrNoBlob is a write whose contents were never uploaded.
	ErrNoBlob = errors.New("blob not uploaded")
	// ErrForbidden is a key operation the requester may not do.
	ErrForbidden = errors.New("forbidden")
)

// Store is one room: its metadata and its contents, under the room's path policy.
type Store struct {
	room   string
	meta   RoomMeta
	blobs  BlobStore
	policy Policy
	// closer closes the drivers when the Store owns them (Open); a Server owns its own.
	closer func() error
}

// NewStore builds a room's store over opened drivers.
func NewStore(room string, meta RoomMeta, blobs BlobStore, policy Policy) *Store {
	return &Store{room: room, meta: meta, blobs: blobs, policy: policy}
}

// Open opens (creating) the design library's store in a folder: SQLite and blobs/ side by side, as every hub did
// before the drivers.
func Open(dir string) (*Store, error) {
	m, err := openSQLiteMeta(dir)
	if err != nil {
		return nil, err
	}
	b, err := NewFileBlobs(dir)
	if err != nil {
		m.Close()
		return nil, err
	}
	rm, err := m.Room(context.Background(), DefaultRoom)
	if err != nil {
		m.Close()
		return nil, err
	}
	st := NewStore(DefaultRoom, rm, b, Design{})
	st.closer = func() error { return m.Close() }
	return st, nil
}

// Close closes the drivers the Store opened itself.
func (s *Store) Close() error {
	if s.closer != nil {
		return s.closer()
	}
	return nil
}

// Wake is closed at the next accepted write (on any replica); waiters for changes select on it.
func (s *Store) Wake() <-chan struct{} { return s.meta.Wake() }

// HasBlob tells whether the contents with this sha256 are stored.
func (s *Store) HasBlob(ctx context.Context, sha string) (bool, error) {
	if !ValidSha(sha) {
		return false, nil
	}
	return s.blobs.Has(ctx, s.room, sha)
}

// OpenBlob opens stored contents for reading (os.ErrNotExist when absent).
func (s *Store) OpenBlob(ctx context.Context, sha string) (io.ReadCloser, error) {
	if !ValidSha(sha) {
		return nil, os.ErrNotExist
	}
	return s.blobs.Open(ctx, s.room, sha)
}

// PutBlob stores contents under their sha256, refusing them when they do not hash to sha or are too big.
func (s *Store) PutBlob(ctx context.Context, sha string, r io.Reader) error {
	if !ValidSha(sha) {
		return fmt.Errorf("%w: sha", ErrBadRequest)
	}
	return s.blobs.Put(ctx, s.room, sha, r)
}

// Current is the latest revision of a path, nil when the path never existed.
func (s *Store) Current(ctx context.Context, path string) (*Entry, error) {
	return s.meta.Current(ctx, path)
}

// Commit applies a write from machine. It returns the new (or, for a write that changes nothing, the current)
// revision, or conflict=true with the current revision when the write was made on top of a stale one.
func (s *Store) Commit(ctx context.Context, req CommitRequest, machine string) (*Entry, bool, error) {
	if !s.policy.Synced(req.Path) || req.Base < 0 || req.Tag != "" && !ValidSha(req.Tag) {
		return nil, false, fmt.Errorf("%w: path", ErrBadRequest)
	}
	if !req.Deleted && (!ValidSha(req.Sha) || req.Size < 0 || req.Size > MaxFileSize) {
		return nil, false, fmt.Errorf("%w: sha or size", ErrBadRequest)
	}
	if !req.Deleted {
		ok, err := s.HasBlob(ctx, req.Sha)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, ErrNoBlob
		}
	}
	// Checked again under the room lock: a collector may have deleted the blob since the check above.
	verify := func(ctx context.Context) error {
		ok, err := s.HasBlob(ctx, req.Sha)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNoBlob
		}
		return nil
	}
	return s.meta.Apply(ctx, req, machine, time.Now().UnixMilli(), verify)
}

// Changes returns the latest revision of every file changed after since, oldest first, at most limit of them.
func (s *Store) Changes(ctx context.Context, since int64, limit int) (Changes, error) {
	return s.meta.Changes(ctx, since, limit)
}

// History lists the revisions of one path, newest first.
func (s *Store) History(ctx context.Context, path string, limit int) ([]Entry, error) {
	return s.meta.History(ctx, path, limit)
}

// RegisterMember records a member's age recipient. A member may change its own recipient (a machine reinstalled
// with a new key); its wrapped room key is then dropped, since the old key can no longer open it.
func (s *Store) RegisterMember(ctx context.Context, name, recipient string) error {
	return s.meta.RegisterMember(ctx, name, recipient, time.Now().UnixMilli())
}

// Members lists the room's members and whether each has the room key.
func (s *Store) Members(ctx context.Context) ([]Member, error) { return s.meta.Members(ctx) }

// PutKey stores the room key wrapped for member, written by `by`. Anyone who already holds the key may wrap it for
// another member (every machine is trusted, and that is how a new machine gets in without a person). The very first
// key of a room is the only one a member may give itself, and only while the room has none: two machines starting
// an empty room at once cannot both mint one.
func (s *Store) PutKey(ctx context.Context, member, wrapped, by string) error {
	return s.meta.PutKey(ctx, member, wrapped, by, time.Now().UnixMilli())
}

// Key is the room key wrapped for member, "" when it has none yet.
func (s *Store) Key(ctx context.Context, member string) (string, error) {
	return s.meta.Key(ctx, member)
}

// Approval is how the room lets a new machine in: "auto" or "manual". Manual is one-way: once a member asked for it,
// no member can turn it back, so one machine left on auto cannot open a room that was set up to approve by hand.
func (s *Store) Approval(ctx context.Context) (string, error) { return s.meta.Approval(ctx) }

// RequireManualApproval switches the room to manual approval for good.
func (s *Store) RequireManualApproval(ctx context.Context) error {
	return s.meta.RequireManualApproval(ctx)
}

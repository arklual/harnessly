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
	"math"
	"os"
	"strings"
	"sync"
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

	sealMu    sync.Mutex
	sealCache map[string]KitSeal
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

// MissingBlobs tells which of the named contents the room does not hold, each once and in the order asked. Every name
// must be a sha256 and there may be at most MaxBlobCheck of them.
func (s *Store) MissingBlobs(ctx context.Context, shas []string) ([]string, error) {
	if len(shas) > MaxBlobCheck {
		return nil, fmt.Errorf("%w: more than %d hashes", ErrBadRequest, MaxBlobCheck)
	}
	missing := []string{}
	asked := make(map[string]bool, len(shas))
	for _, sha := range shas {
		if !ValidSha(sha) {
			return nil, fmt.Errorf("%w: sha", ErrBadRequest)
		}
		if asked[sha] {
			continue
		}
		asked[sha] = true
		ok, err := s.blobs.Has(ctx, s.room, sha)
		if err != nil {
			return nil, err
		}
		if !ok {
			missing = append(missing, sha)
		}
	}
	return missing, nil
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
	// A policy of files that never change (a published kit version): what is there is not overwritten or deleted. The
	// first write of a path is still decided under the room lock (a stale base), so two machines racing for it cannot
	// both win.
	if im, ok := s.policy.(Immutable); ok && im.Immutable() {
		cur, err := s.meta.Current(ctx, req.Path)
		if err != nil {
			return nil, false, err
		}
		if cur != nil && !cur.Deleted && (req.Deleted || req.Sha != cur.Sha || req.Exec != cur.Exec) {
			return cur, true, nil
		}
	}
	if _, ok := s.policy.(Kits); ok {
		if err := s.sealCheck(ctx, req); err != nil {
			return nil, false, err
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

// HistoryUnder is the revisions under a folder (prefix ends in "/"; "" for the whole room), newest first.
func (s *Store) HistoryUnder(ctx context.Context, prefix string, limit int) ([]Entry, error) {
	if prefix != "" && (!strings.HasSuffix(prefix, "/") || !SafeRel(strings.TrimSuffix(prefix, "/"))) {
		return nil, ErrBadRequest
	}
	return s.meta.HistoryUnder(ctx, prefix, limit)
}

// CountUnder counts the revisions under an ASCII folder in any letter case, up to limit.
func (s *Store) CountUnder(ctx context.Context, prefix string, limit int) (int, error) {
	if !strings.HasSuffix(prefix, "/") || !SafeRel(strings.TrimSuffix(prefix, "/")) || !isASCII(prefix) {
		return 0, ErrBadRequest
	}
	return s.meta.CountUnder(ctx, prefix, limit)
}

func isASCII(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Snapshot is a folder (prefix ends in "/") as it was at revision at, without the paths deleted by then.
func (s *Store) Snapshot(ctx context.Context, prefix string, at int64) (Snapshot, error) {
	out := Snapshot{At: at, Entries: []Entry{}}
	if !strings.HasSuffix(prefix, "/") || !SafeRel(strings.TrimSuffix(prefix, "/")) || at <= 0 {
		return out, ErrBadRequest
	}
	es, err := s.meta.Snapshot(ctx, prefix, at)
	if err != nil {
		return out, err
	}
	for _, e := range es {
		if !e.Deleted && s.policy.Synced(e.Path) {
			out.Entries = append(out.Entries, e)
		}
	}
	return out, nil
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

// sealCheck is the rule of a sealed kit version. Before the seal, files are added and none changes (Immutable
// above). The seal may land before its files (a machine sends in any order): it is taken when it is well formed, and
// from then on the folder takes only the files it lists, with the bytes it lists. Whether a version is whole is the
// readers' business: it counts as installed only with every listed file in place.
func (s *Store) sealCheck(ctx context.Context, req CommitRequest) error {
	prefix := KitSealPrefix(req.Path)
	if prefix == "" {
		return nil
	}
	sealPath := prefix + KitSealFile
	seal, err := s.meta.Current(ctx, sealPath)
	if err != nil {
		return err
	}
	sealed := seal != nil && !seal.Deleted
	if req.Path == sealPath {
		if sealed || req.Deleted {
			return nil // a second seal is the same bytes (a no-op) or a conflict, by the rule above
		}
		doc, err := s.readSeal(ctx, req.Sha)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrSealed, err)
		}
		// A seal closes the version over what is already there: every file the room holds in it must be listed, with
		// those bytes. (A file that lands between this check and the seal is harmless: the readers take a version as its
		// seal lists it and ignore the rest.)
		listed := make(map[string]KitSealedFile, len(doc.Files))
		for _, f := range doc.Files {
			listed[f.Path] = f
		}
		have, err := s.meta.Snapshot(ctx, prefix, math.MaxInt64>>1)
		if err != nil {
			return err
		}
		for _, e := range have {
			rel := strings.TrimPrefix(e.Path, prefix)
			if e.Deleted || rel == KitSealFile {
				continue
			}
			if f, ok := listed[rel]; !ok || f.Size != e.Size || f.SHA256 != e.Sha {
				return fmt.Errorf("%w: %s is in the room but not as the seal lists it", ErrSealed, e.Path)
			}
		}
		return nil
	}
	if !sealed {
		return nil
	}
	cur, err := s.meta.Current(ctx, req.Path)
	if err != nil {
		return err
	}
	if cur != nil && !cur.Deleted {
		return nil // a file the version has: changing or deleting it is refused as a conflict by the rule above
	}
	doc, err := s.readSeal(ctx, seal.Sha)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(req.Path, prefix)
	for _, f := range doc.Files {
		if f.Path == rel && !req.Deleted && f.Size == req.Size && f.SHA256 == req.Sha {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is not in the seal", ErrSealed, req.Path)
}

// readSeal reads and checks the seal stored as the blob sha.
func (s *Store) readSeal(ctx context.Context, sha string) (KitSeal, error) {
	s.sealMu.Lock()
	c, ok := s.sealCache[sha]
	s.sealMu.Unlock()
	if ok {
		return c, nil
	}
	rc, err := s.blobs.Open(ctx, s.room, sha)
	if err != nil {
		return KitSeal{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(rc, MaxKitSealBytes+1))
	rc.Close()
	if err != nil {
		return KitSeal{}, err
	}
	doc, err := ParseKitSeal(raw)
	if err != nil {
		return KitSeal{}, err
	}
	s.sealMu.Lock()
	if s.sealCache == nil || len(s.sealCache) > 64 {
		s.sealCache = map[string]KitSeal{}
	}
	s.sealCache[sha] = doc
	s.sealMu.Unlock()
	return doc, nil
}

package filestore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
)

// MetaStore is where the hub keeps everything but the contents: every room's revisions, leases, members and keys. Two
// drivers: SQLite (a folder, one writer process: the default) and Postgres (any number of hub replicas).
type MetaStore interface {
	// Room opens (creating) a room's metadata.
	Room(ctx context.Context, room string) (RoomMeta, error)
	// Rooms lists the rooms that exist.
	Rooms(ctx context.Context) ([]string, error)
	// TryLock takes a named lock for a periodic job that only one replica should run at a time. ok=false means
	// another holder has it.
	TryLock(ctx context.Context, name string) (unlock func(), ok bool, err error)
	Close() error
}

// RoomMeta is one room's metadata. Every method is safe for concurrent use, and, on Postgres, for use from several
// processes at once: the decision of a commit is taken under a lock on the room that lives in the database.
type RoomMeta interface {
	Current(ctx context.Context, path string) (*Entry, error)
	// Apply decides and records a commit: the new revision, the unchanged current one for a write that changes
	// nothing, or conflict=true with the current revision for a write made on top of a stale one.
	//
	// verify (may be nil) runs once the room is locked and before the decision: the commit's contents must still be
	// stored at that moment, since the garbage collector deletes under the same lock (see DeleteUnreferenced).
	Apply(ctx context.Context, req CommitRequest, machine string, now int64, verify func(context.Context) error) (e *Entry, conflict bool, err error)
	Changes(ctx context.Context, since int64, limit int) (Changes, error)
	History(ctx context.Context, path string, limit int) ([]Entry, error)
	// HistoryUnder is the revisions of every path under prefix ("" for the whole room), newest first.
	HistoryUnder(ctx context.Context, prefix string, limit int) ([]Entry, error)
	// CountUnder counts the revisions under an ASCII prefix in any letter case, up to limit.
	CountUnder(ctx context.Context, prefix string, limit int) (int, error)
	// Snapshot is the newest revision not newer than at of every path under prefix (a folder, ending in "/"),
	// deleted ones included, by path.
	Snapshot(ctx context.Context, prefix string, at int64) ([]Entry, error)

	RegisterMember(ctx context.Context, name, recipient string, now int64) error
	Members(ctx context.Context) ([]Member, error)
	PutKey(ctx context.Context, member, wrapped, by string, now int64) error
	Key(ctx context.Context, member string) (string, error)
	Approval(ctx context.Context) (string, error)
	RequireManualApproval(ctx context.Context) error

	// Leases (short claims on a board). LeaseTake gives the caller the lease on req.Key, or reports the live lease
	// of someone else (held). LeaseDrop lets go of the caller's lease, or of anyone's with Force; it returns what it
	// dropped, nil when there was nothing to drop. Leases are the live ones. A lease is not history: it is not
	// copied by migrate and a restored backup starts without any.
	LeaseTake(ctx context.Context, req LeaseRequest, machine string, now int64) (l Lease, held bool, err error)
	LeaseDrop(ctx context.Context, req LeaseRelease, machine string, now int64) (*Lease, error)
	Leases(ctx context.Context, now int64) ([]Lease, error)

	// Wake returns a channel that is closed after the next write to the room, by this process or (Postgres) any other.
	Wake() <-chan struct{}

	// Retention and garbage collection.
	Prune(ctx context.Context, keep int, before int64) (int64, error)
	ReferencedShas(ctx context.Context) (map[string]struct{}, error)
	Referenced(ctx context.Context, sha string) (bool, error)
	// DeleteUnreferenced runs del under the room's write lock, and only when no revision refers to sha. A commit
	// takes the same lock, so none can start referring to the blob between the check and the delete.
	DeleteUnreferenced(ctx context.Context, sha string, del func(context.Context) error) (bool, error)
	GCMarks(ctx context.Context) (map[string]int64, error)
	PutGCMarks(ctx context.Context, add map[string]int64, drop []string) error

	// Copying a room to another driver (migrate) and checking the copy.
	ExportHistory(ctx context.Context, after int64, limit int) ([]Entry, error)
	ImportHistory(ctx context.Context, es []Entry) error
	ExportState(ctx context.Context) (RoomState, error)
	ImportState(ctx context.Context, st RoomState) error
	Counts(ctx context.Context) (RoomCounts, error)

	// Uploads in parts (uploads.go). UploadOpen records u, unless the member has an upload of the same contents under
	// way: that one is returned (with its parts) and created is false. ErrTooManyUploads past the limits.
	UploadOpen(ctx context.Context, u Upload, lim OpenLimits, now int64) (out Upload, created bool, err error)
	// UploadGet is an upload with its parts; nil when there is none (or it expired).
	UploadGet(ctx context.Context, id string, now int64) (*Upload, error)
	// UploadBegun records the store's handle of the upload's assembly.
	UploadBegun(ctx context.Context, id, handle string) error
	// UploadPart records part p.N with the hash state of the contents up to its end and moves the expiry to expires;
	// false (and nothing recorded) unless p.N is the next part of an upload still receiving.
	UploadPart(ctx context.Context, id string, p UploadPart, hash string, expires int64) (bool, error)
	// UploadSetState moves an upload from one state to another; false when it was not in from.
	UploadSetState(ctx context.Context, id, from, to, errText string, expires int64) (bool, error)
	UploadDrop(ctx context.Context, id string) error
	// UploadsExpired lists the uploads whose term ended.
	UploadsExpired(ctx context.Context, now int64) ([]Upload, error)
}

// RoomState is a room's small tables: which revision is current for each path, members, wrapped keys, settings.
type RoomState struct {
	Files    map[string]int64  `json:"files"`
	Members  []MemberRow       `json:"members"`
	Keys     []KeyRow          `json:"keys"`
	Settings map[string]string `json:"settings"`
}

// MemberRow is a member as stored.
type MemberRow struct {
	Name      string `json:"name"`
	Recipient string `json:"recipient"`
	At        int64  `json:"at"`
}

// KeyRow is a wrapped room key as stored.
type KeyRow struct {
	Member  string `json:"member"`
	Wrapped string `json:"wrapped"`
	By      string `json:"by"`
	At      int64  `json:"at"`
}

// RoomCounts is what a copy is checked against.
type RoomCounts struct {
	History int64
	Files   int64
	Members int64
	Keys    int64
	MaxRev  int64
}

// OpenMetaStore opens the metadata driver a URL names: `sqlite:///path/to/data-folder` (the folder holds hub.db and
// rooms/<room>/hub.db) or `postgres://user@host/db` (a password belongs in PGPASSWORD or ~/.pgpass, not in the URL on
// a command line). A Postgres database opened this way has the layout of a design-sync hub: the registry ds_rooms and
// a schema r_<n> per room; OpenMetaStoreIn shares a database with other tables under a prefix of its own.
func OpenMetaStore(ctx context.Context, raw string) (MetaStore, error) {
	return OpenMetaStoreIn(ctx, raw, "")
}

// OpenMetaStoreIn is OpenMetaStore with the names of the Postgres registry and schemas led by prefix
// (`<prefix>rooms`, `<prefix>r_<n>`): how the store lives in the hub's own database next to its tables. An empty
// prefix is the design-sync layout. SQLite ignores it: a folder is the store's alone.
func OpenMetaStoreIn(ctx context.Context, raw, prefix string) (MetaStore, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("db %q: %w", redact(raw), err)
	}
	switch u.Scheme {
	case "sqlite":
		p := u.Path
		if p == "" {
			p = u.Host
		}
		if p == "" {
			return nil, errors.New("db sqlite://: the folder is missing (sqlite:///var/lib/harnessly-hub/store)")
		}
		return openSQLiteMeta(p)
	case "postgres", "postgresql":
		return openPostgresMetaOpts(ctx, raw, false, prefix)
	}
	return nil, fmt.Errorf("db %q: sqlite:///folder or postgres://… expected", redact(raw))
}

// --- the commit decision, shared by the drivers: they differ in how they lock, not in what they decide ---

type commitOutcome int

const (
	commitConflict commitOutcome = iota // stale base: answer with the current revision
	commitNoop                          // nothing changes: answer with the current revision (or a tombstone)
	commitWrite                         // take the next revision
)

// decide is the whole compare-and-swap. cur is the file's current revision, nil when it never existed. It must run
// while the room is locked against other writers.
func decide(cur *Entry, req CommitRequest) (commitOutcome, *Entry) {
	switch {
	case cur == nil && req.Base == 0:
	case cur != nil && cur.Rev == req.Base:
	case cur != nil && cur.Deleted && req.Base == 0:
	default:
		if cur == nil {
			// The writer believes in a revision the hub never had (a hub restored from an older backup): its copy
			// is the best there is, take it.
			break
		}
		return commitConflict, cur
	}
	// Nothing changes: keep the history free of no-op revisions.
	if cur != nil && cur.Deleted == req.Deleted && (req.Deleted || sameContents(*cur, req) && cur.Exec == req.Exec) {
		return commitNoop, cur
	}
	if cur == nil && req.Deleted {
		return commitNoop, &Entry{Path: req.Path, Deleted: true}
	}
	return commitWrite, nil
}

// sameContents compares by the plain fingerprint when the writer sent one: in an encrypted room the same contents
// seal to a different blob every time.
func sameContents(cur Entry, req CommitRequest) bool {
	if req.Tag != "" {
		return cur.Same(req.Tag)
	}
	return cur.Sha == req.Sha
}

// newEntry is the revision a write becomes (its Rev is set by the driver).
func newEntry(req CommitRequest, machine string, now int64) Entry {
	e := Entry{Path: req.Path, Sha: req.Sha, Tag: req.Tag, Size: req.Size, Exec: req.Exec, Deleted: req.Deleted,
		Author: req.Author, Machine: machine, At: now}
	if e.Deleted {
		e.Sha, e.Tag, e.Size, e.Exec = "", "", 0, false
	}
	return e
}

// --- waking long-polls ---

// notifier hands out a channel that is closed at the next broadcast.
type notifier struct {
	mu sync.Mutex
	ch chan struct{}
}

func newNotifier() *notifier { return &notifier{ch: make(chan struct{})} }

func (n *notifier) Wake() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ch
}

func (n *notifier) Broadcast() {
	n.mu.Lock()
	defer n.mu.Unlock()
	close(n.ch)
	n.ch = make(chan struct{})
}

var errInvalidRoom = errors.New("not a valid room name")

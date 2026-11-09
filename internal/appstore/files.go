package appstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// StorePrefix is the folder of the apps' areas in a space's apps collection of the store: storage/<Path>.
const StorePrefix = "storage/"

// Folder is areas' files in a folder of the machine. Its revisions are all 0: the writers of one folder are one
// process, which holds its own lock around a read and the write after it.
type Folder string

func (d Folder) file(rel string) string { return filepath.Join(string(d), filepath.FromSlash(rel)) }

func (d Folder) Read(_ context.Context, rel string) ([]byte, int64, error) {
	raw, err := os.ReadFile(d.file(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	return raw, 0, err
}

func (d Folder) Write(_ context.Context, rel string, _ int64, data []byte) error {
	file := d.file(rel)
	if data == nil {
		err := os.Remove(file)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return WriteFileAtomic(file, data)
}

func (d Folder) Weigh(_ context.Context, dir string) (int64, error) {
	var total int64
	_ = filepath.WalkDir(d.file(dir), func(_ string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if info, err := e.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, nil
}

// WriteFileAtomic writes a file through a temporary one beside it, so a reader never sees half of it.
func WriteFileAtomic(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Client is the store's API as a machine speaks it (filesync.Client).
type Client interface {
	Current(ctx context.Context, path string) (*filestore.Entry, error)
	GetBlob(ctx context.Context, sha string, w io.Writer) error
	PutBlob(ctx context.Context, sha string, data []byte) error
	Commit(ctx context.Context, req filestore.CommitRequest) (filestore.CommitResponse, error)
	Snapshot(ctx context.Context, prefix string, at int64) (filestore.Snapshot, error)
}

// Room is a room of the store as the hub reads it itself (filestore.Store).
type Room interface {
	Current(ctx context.Context, path string) (*filestore.Entry, error)
	OpenBlob(ctx context.Context, sha string) (io.ReadCloser, error)
	PutBlob(ctx context.Context, sha string, r io.Reader) error
	Commit(ctx context.Context, req filestore.CommitRequest, machine string) (*filestore.Entry, bool, error)
	Snapshot(ctx context.Context, prefix string, at int64) (filestore.Snapshot, error)
}

// ClientFiles is areas' files in the hub's store, as a machine reaches them; author names the writer of a revision.
func ClientFiles(c Client, author string) Files {
	return &storeFiles{author: author,
		current: c.Current,
		blob: func(ctx context.Context, sha string) ([]byte, error) {
			b := capped{max: MaxAreaBytes}
			err := c.GetBlob(ctx, sha, &b)
			return b.buf.Bytes(), err
		},
		put: c.PutBlob,
		commit: func(ctx context.Context, req filestore.CommitRequest) (bool, error) {
			out, err := c.Commit(ctx, req)
			return out.Conflict, err
		},
		snapshot: c.Snapshot,
	}
}

// RoomFiles is areas' files in a room of the hub's store, as the hub reaches them; machine is what its revisions are
// recorded under.
func RoomFiles(r Room, machine, author string) Files {
	return &storeFiles{author: author,
		current: r.Current,
		blob: func(ctx context.Context, sha string) ([]byte, error) {
			rc, err := r.OpenBlob(ctx, sha)
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, MaxAreaBytes+1))
		},
		put: func(ctx context.Context, sha string, data []byte) error {
			return r.PutBlob(ctx, sha, bytes.NewReader(data))
		},
		commit: func(ctx context.Context, req filestore.CommitRequest) (bool, error) {
			_, conflict, err := r.Commit(ctx, req, machine)
			return conflict, err
		},
		snapshot: r.Snapshot,
	}
}

// capped takes at most max bytes and refuses the rest.
type capped struct {
	buf bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.max {
		return 0, errors.New("apps: the store's area is larger than any area may be")
	}
	return c.buf.Write(p)
}

type storeFiles struct {
	author   string
	current  func(ctx context.Context, path string) (*filestore.Entry, error)
	blob     func(ctx context.Context, sha string) ([]byte, error)
	put      func(ctx context.Context, sha string, data []byte) error
	commit   func(ctx context.Context, req filestore.CommitRequest) (bool, error)
	snapshot func(ctx context.Context, prefix string, at int64) (filestore.Snapshot, error)

	// The contents read last, by sha: an area read again unchanged costs one question to the store, not two.
	mu    sync.Mutex
	cache map[string][]byte
	size  int
}

// cacheMax caps what the contents kept in memory weigh.
const cacheMax = 16 << 20

func (s *storeFiles) Read(ctx context.Context, rel string) ([]byte, int64, error) {
	e, err := s.current(ctx, StorePrefix+rel)
	if err != nil || e == nil {
		return nil, 0, err
	}
	if e.Deleted {
		return nil, e.Rev, nil
	}
	if e.Size > MaxAreaBytes {
		return nil, 0, errors.New("apps: the store's area is larger than any area may be")
	}
	s.mu.Lock()
	raw, ok := s.cache[e.Sha]
	s.mu.Unlock()
	if ok {
		return raw, e.Rev, nil
	}
	raw, err = s.blob(ctx, e.Sha)
	if err != nil {
		return nil, 0, err
	}
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != e.Sha {
		return nil, 0, errors.New("apps: the store's area is not what its revision says")
	}
	s.remember(e.Sha, raw)
	return raw, e.Rev, nil
}

func (s *storeFiles) remember(sha string, raw []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil || s.size+len(raw) > cacheMax {
		s.cache, s.size = map[string][]byte{}, 0
	}
	if len(raw) <= cacheMax {
		s.cache[sha] = raw
		s.size += len(raw)
	}
}

func (s *storeFiles) Write(ctx context.Context, rel string, base int64, data []byte) error {
	req := filestore.CommitRequest{Path: StorePrefix + rel, Base: base, Author: s.author}
	if data == nil {
		req.Deleted = true
	} else {
		sum := sha256.Sum256(data)
		req.Sha, req.Size = hex.EncodeToString(sum[:]), int64(len(data))
		if err := s.put(ctx, req.Sha, data); err != nil {
			return err
		}
	}
	conflict, err := s.commit(ctx, req)
	if err != nil {
		return err
	}
	if conflict {
		return ErrMoved
	}
	if data != nil {
		s.remember(req.Sha, data)
	}
	return nil
}

// headOfAll stands for "now" in a snapshot: every revision there is.
const headOfAll = int64(1) << 62

func (s *storeFiles) Weigh(ctx context.Context, dir string) (int64, error) {
	snap, err := s.snapshot(ctx, StorePrefix+dir+"/", headOfAll)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range snap.Entries {
		total += e.Size
	}
	return total, nil
}

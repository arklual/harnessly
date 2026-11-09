package designview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
)

// Writer is a space's design collection as the hub writes it: a hand edit of a board made in Sinopia lands in the
// store as a revision through the hub, the way a machine's sync sends it, and the machines take it from there.
type Writer interface {
	Room
	PutBlob(ctx context.Context, sha string, r io.Reader) error
	Current(ctx context.Context, path string) (*filestore.Entry, error)
	Commit(ctx context.Context, req filestore.CommitRequest, machine string) (*filestore.Entry, bool, error)
}

// HubMachine is the machine a revision made through the hub is recorded under.
const HubMachine = "hub"

// The errors and the outcome of a write are designaccess's, which takes them through its Viewer.
var (
	ErrConflict = designaccess.ErrConflict
	ErrBadPath  = designaccess.ErrBadPath
	ErrReadOnly = designaccess.ErrReadOnly
)

// Written is the outcome of a write: the file's revision now, and the project's newest one.
type Written = designaccess.Written

// Write commits a file of the project (rel under its folder) on top of base, the file's revision the editor read (0
// for a new file); data nil deletes it. A stale base is ErrConflict with the file's current revision in Written.Rev.
func (v *View) Write(ctx context.Context, spaceID, project, rel string, base int64, data []byte, author string) (Written, error) {
	if !design.IsLibraryID(project) || rel == "" || path.Clean(rel) != rel || strings.HasPrefix(rel, "../") || !filestore.SafeRel(project+"/"+rel) {
		return Written{}, ErrBadPath
	}
	if base < 0 {
		return Written{}, ErrBadPath
	}
	if int64(len(data)) > filestore.MaxFileSize {
		return Written{}, filestore.ErrBadRequest
	}
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return Written{}, err
	}
	w, ok := room.(Writer)
	if !ok {
		return Written{}, ErrReadOnly
	}
	full := project + "/" + rel
	req := filestore.CommitRequest{Path: full, Base: base, Author: author}
	if data == nil {
		req.Deleted = true
	} else {
		sum := sha256.Sum256(data)
		req.Sha = hex.EncodeToString(sum[:])
		req.Size = int64(len(data))
		if err := w.PutBlob(ctx, req.Sha, bytes.NewReader(data)); err != nil {
			return Written{}, err
		}
	}
	e, conflict, err := w.Commit(ctx, req, HubMachine)
	if err != nil {
		if errors.Is(err, filestore.ErrBadRequest) {
			return Written{}, ErrBadPath
		}
		return Written{}, err
	}
	out := Written{}
	if e != nil {
		out.Rev = e.Rev
	}
	if conflict {
		return out, ErrConflict
	}
	out.Head, _ = v.Head(ctx, spaceID, project)
	return out, nil
}

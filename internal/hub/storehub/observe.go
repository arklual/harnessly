package storehub

import (
	"context"
	"io"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// Observe makes fn hear of every write the store takes, with the space and the kind of the collection it is in and the
// path inside it (the search index keeps itself up to date from it). fn must not block. Call it before the
// store serves.
func (s *Store) Observe(fn func(spaceID, kind, path string, deleted bool)) {
	if fn == nil {
		s.srv.ObserveRooms(nil)
		return
	}
	s.srv.ObserveRooms(func(room, path string, deleted bool) {
		if space, kind, ok := s.splitRoom(room); ok {
			fn(space, kind, path, deleted)
		}
	})
}

// splitRoom is RoomName backwards.
func (s *Store) splitRoom(room string) (spaceID, kind string, ok bool) {
	for _, k := range append([]string{"design", "apps"}, filestore.RecordKinds...) {
		switch {
		case room == k && s.legacy != "":
			return s.legacy, k, true
		case strings.HasPrefix(room, k+"-"):
			return strings.TrimPrefix(room, k+"-"), k, true
		}
	}
	return "", "", false
}

// ProjectOf is the project folder a path of the design collection is in.
func ProjectOf(path string) (string, bool) { return projectOf(path) }

// Record is the record of an entity of the library as the space's collections keep it: the first of systems,
// videos and decks that has it. A collection the space has no room of is not opened.
func (s *Store) Record(ctx context.Context, spaceID, id string) (filestore.Record, bool, error) {
	for _, kind := range filestore.RecordKinds {
		spaces, err := s.spacesOf(ctx, kind)
		if err != nil {
			return filestore.Record{}, false, err
		}
		if !slices.Contains(spaces, spaceID) {
			continue
		}
		room, err := s.srv.Room(ctx, s.Room(spaceID, kind))
		if err != nil {
			return filestore.Record{}, false, err
		}
		e, err := room.Current(ctx, filestore.RecordPath(id))
		if err != nil {
			return filestore.Record{}, false, err
		}
		if e == nil || e.Deleted || e.Size > filestore.MaxRecordBytes {
			continue
		}
		rc, err := room.OpenBlob(ctx, e.Sha)
		if err != nil {
			return filestore.Record{}, false, err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, filestore.MaxRecordBytes+1))
		rc.Close()
		if err != nil {
			return filestore.Record{}, false, err
		}
		rec, err := filestore.ParseRecord(raw, id)
		if err != nil {
			continue
		}
		return rec, true, nil
	}
	return filestore.Record{}, false, nil
}

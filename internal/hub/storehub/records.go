package storehub

import (
	"context"
	"io"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// MetaRecorder is the part of the design access that takes an entity's record into the hub's row of its project
// A Design that implements it is told of every record the store commits; one that does not leaves the
// records in the store only.
type MetaRecorder interface {
	// RecordMeta takes the facts of a record a machine wrote; the hub applies its own rules (the switch, a newer edit).
	RecordMeta(ctx context.Context, m store.Machine, rec filestore.Record) error
}

// recordsFilter limits a machine to the records of the entities its person may open, as the design collection limits
// it to their projects: `<id>.json` is the record of the project `<id>`. A record of a project the hub has no row for
// is the machine's to put (the hub learns the project from its own events); a recorded one takes writes only from a
// person who may edit it.
func (s *Store) recordsFilter(ctx context.Context, m store.Machine, acc *filestore.Access) error {
	return s.entityFilter(ctx, m, acc, filestore.RecordID, true)
}

// coversFilter is the same for the covers collection: `<id>.jpg` follows the access of the project `<id>`, and no record is taken from it.
func (s *Store) coversFilter(ctx context.Context, m store.Machine, acc *filestore.Access) error {
	return s.entityFilter(ctx, m, acc, filestore.CoverID, false)
}

func (s *Store) entityFilter(ctx context.Context, m store.Machine, acc *filestore.Access, idOf func(string) (string, bool), records bool) error {
	view, scope, err := s.view(ctx, m)
	if err != nil {
		return err
	}
	closed := s.design.ClosedUnrecorded()
	acc.Scope = scope
	acc.Sees = func(p string) bool {
		id, ok := idOf(p)
		if !ok {
			return false
		}
		g, known := view[id]
		if !known {
			return !closed
		}
		return g.See
	}
	acc.Writes = func(p string) bool {
		id, ok := idOf(p)
		if !ok {
			return false
		}
		g, known := view[id]
		return !known || g.Edit
	}
	room := acc.Room
	rec, _ := s.design.(MetaRecorder)
	if !records {
		return nil
	}
	acc.Committed = func(ctx context.Context, e filestore.Entry) {
		id, ok := idOf(e.Path)
		if rec == nil || !ok || e.Deleted {
			return
		}
		rc, err := s.reader.Open(ctx, room, e.Sha)
		if err != nil {
			return
		}
		raw, err := io.ReadAll(io.LimitReader(rc, filestore.MaxRecordBytes+1))
		rc.Close()
		if err != nil {
			return
		}
		r, err := filestore.ParseRecord(raw, id)
		if err != nil {
			return
		}
		if err := rec.RecordMeta(ctx, m, r); err != nil {
			s.log.Warn("store: a record was not taken into the project's row", "project", id, "err", err)
		}
	}
	return nil
}

package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

// MemoryNote is one note of a person's agent memory (docs/hub/memory-sync.md).
type MemoryNote struct {
	UserID    string
	Project   string
	Name      string
	Body      string
	Version   string
	Deleted   bool
	MachineID string
	UpdatedAt time.Time
}

// ErrMemoryChanged: the note is not at the version the writer saw; the answer of PutMemoryNote carries it.
var ErrMemoryChanged = errors.New("store: the note changed")

// ErrMemoryFull: the project holds as many notes as it may.
var ErrMemoryFull = errors.New("store: too many notes")

const memoryCols = `user_id, project, name, body, version, deleted, machine_id, updated_at`

func scanMemory(r interface{ Scan(...any) error }) (MemoryNote, error) {
	var n MemoryNote
	var deleted int
	var at int64
	if err := r.Scan(&n.UserID, &n.Project, &n.Name, &n.Body, &n.Version, &deleted, &n.MachineID, &at); err != nil {
		return MemoryNote{}, err
	}
	n.Deleted, n.UpdatedAt = deleted != 0, fromMs(at)
	return n, nil
}

// MemoryNotes is every note (tombstones too) of the person's projects.
func (s Store) MemoryNotes(ctx context.Context, userID string, projects []string) ([]MemoryNote, error) {
	out := []MemoryNote{}
	if len(projects) == 0 {
		return out, nil
	}
	args := []any{userID}
	for _, p := range projects {
		args = append(args, p)
	}
	rows, err := s.q.Query(ctx, `SELECT `+memoryCols+` FROM memory_notes WHERE user_id = ? AND project IN (?`+
		strings.Repeat(", ?", len(projects)-1)+`) ORDER BY project, name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		n, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MemoryNote reads one note; ErrNotFound when there is none.
func (s Store) MemoryNote(ctx context.Context, userID, project, name string) (MemoryNote, error) {
	return scanMemory(s.q.QueryRow(ctx, `SELECT `+memoryCols+` FROM memory_notes WHERE user_id = ? AND project = ? AND name = ?`,
		userID, project, name))
}

// PutMemoryNote writes n if the kept note is at version base ("": there is none). A note at another version stops the
// write: the answer is that note and ErrMemoryChanged. Writing the version already kept changes nothing. maxNotes
// bounds the notes of a project (tombstones count).
func (s Store) PutMemoryNote(ctx context.Context, n MemoryNote, base string, maxNotes int) (MemoryNote, error) {
	deleted := 0
	if n.Deleted {
		deleted = 1
	}
	cur, err := s.MemoryNote(ctx, n.UserID, n.Project, n.Name)
	switch {
	case errors.Is(err, ErrNotFound):
		// Whatever the writer saw before (another hub, a lost row), a new note loses nothing.
		var count int
		if err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM memory_notes WHERE user_id = ? AND project = ?`, n.UserID, n.Project).Scan(&count); err != nil {
			return MemoryNote{}, err
		}
		if count >= maxNotes {
			return MemoryNote{}, ErrMemoryFull
		}
		res, err := s.q.Exec(ctx, `INSERT INTO memory_notes (`+memoryCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, project, name) DO NOTHING`,
			n.UserID, n.Project, n.Name, n.Body, n.Version, deleted, n.MachineID, Ms(n.UpdatedAt))
		if err != nil {
			return MemoryNote{}, err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			// Another machine wrote it in between.
			return s.raced(ctx, n)
		}
		return n, nil
	case err != nil:
		return MemoryNote{}, err
	}
	if cur.Version == n.Version && cur.Deleted == n.Deleted {
		return cur, nil
	}
	if cur.Version != base {
		return cur, ErrMemoryChanged
	}
	res, err := s.q.Exec(ctx, `UPDATE memory_notes SET body = ?, version = ?, deleted = ?, machine_id = ?, updated_at = ?
		WHERE user_id = ? AND project = ? AND name = ? AND version = ?`,
		n.Body, n.Version, deleted, n.MachineID, Ms(n.UpdatedAt), n.UserID, n.Project, n.Name, base)
	if err != nil {
		return MemoryNote{}, err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return s.raced(ctx, n)
	}
	return n, nil
}

func (s Store) raced(ctx context.Context, n MemoryNote) (MemoryNote, error) {
	cur, err := s.MemoryNote(ctx, n.UserID, n.Project, n.Name)
	if err != nil {
		return MemoryNote{}, err
	}
	if cur.Version == n.Version {
		return cur, nil
	}
	return cur, ErrMemoryChanged
}

// MemoryTombstoneVersion is the version of a deleted note (hubproto.MemoryTombstone).
const MemoryTombstoneVersion = "deleted"

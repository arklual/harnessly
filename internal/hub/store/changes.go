package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Change sources.
const (
	ChangeManual   = "manual"
	ChangeDetected = "detected"
)

// Change is a change of a scope of an organization the KPI are compared around.
type Change struct {
	ID        string
	OrgKey    string
	ScopeKind string
	ScopeID   string
	Kind      string // manual, claude_version, …
	Title     string
	At        time.Time
	AuthorID  string // "" for a detected change
	Source    string
	Machines  int
	CreatedAt time.Time
}

const changeCols = `id, org_key, scope_kind, scope_id, kind, title, at, author_id, source, machines, created_at`

func scanChange(row interface{ Scan(...any) error }) (Change, error) {
	var c Change
	var author sql.NullString
	var at, created int64
	if err := row.Scan(&c.ID, &c.OrgKey, &c.ScopeKind, &c.ScopeID, &c.Kind, &c.Title, &at, &author, &c.Source, &c.Machines,
		&created); err != nil {
		return Change{}, err
	}
	c.AuthorID, c.At, c.CreatedAt = author.String, fromMs(at), fromMs(created)
	return c, nil
}

// AddChange keeps a new change; c.ID is made when empty.
func (s Store) AddChange(ctx context.Context, c Change) (Change, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO org_changes (`+changeCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.OrgKey, c.ScopeKind, c.ScopeID, c.Kind, c.Title, Ms(c.At), nullStr(c.AuthorID), c.Source, c.Machines, Ms(c.CreatedAt))
	if err != nil {
		return Change{}, err
	}
	return c, nil
}

// ChangeByID is one change, or ErrNotFound.
func (s Store) ChangeByID(ctx context.Context, id string) (Change, error) {
	return scanChange(s.q.QueryRow(ctx, `SELECT `+changeCols+` FROM org_changes WHERE id = ?`, id))
}

// ChangeScope is a scope whose changes are asked for.
type ChangeScope struct{ Kind, ID string }

// ChangesOn are the changes of the organization on any of scopes with at in [from, to) (0 for no bound), by at.
func (s Store) ChangesOn(ctx context.Context, orgKey string, scopes []ChangeScope, from, to int64) ([]Change, error) {
	if len(scopes) == 0 {
		return nil, nil
	}
	q := `SELECT ` + changeCols + ` FROM org_changes WHERE org_key = ? AND (`
	args := []any{orgKey}
	var or []string
	for _, sc := range scopes {
		or = append(or, `(scope_kind = ? AND scope_id = ?)`)
		args = append(args, sc.Kind, sc.ID)
	}
	q += strings.Join(or, " OR ") + `)`
	if from > 0 {
		q += ` AND at >= ?`
		args = append(args, from)
	}
	if to > 0 {
		q += ` AND at < ?`
		args = append(args, to)
	}
	rows, err := s.q.Query(ctx, q+` ORDER BY at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

package store

import (
	"context"
	"database/sql"
	"time"
)

// Signal is one line of «Требует внимания» as it is kept (docs/hub/analytics.md, «Signals»).
type Signal struct {
	ID           string
	OrgKey       string
	Rule         string
	DedupeKey    string // what the signal is about inside its rule
	SubjectKind  string
	SubjectID    string
	SubjectLabel string
	ScopeKind    string
	ScopeID      string
	AddresseeID  string // "" when it is addressed to the managers of the scope
	Severity     string
	Weight       float64 // how bad inside a severity, for the order
	Title        string
	Detail       string
	Action       string // JSON of the signal's button
	FirstAt      time.Time
	LastAt       time.Time
	ActedAt      *time.Time // the last time somebody acted (snoozed)
	SnoozedUntil *time.Time
	ResolvedAt   *time.Time
	CreatedAt    time.Time
}

const signalCols = `id, org_key, rule, dedupe_key, subject_kind, subject_id, subject_label, scope_kind, scope_id, addressee_id, severity,
	weight, title, detail, action, first_at, last_at, acted_at, snoozed_until, resolved_at, created_at`

func scanSignal(row interface{ Scan(...any) error }) (Signal, error) {
	var g Signal
	var addressee sql.NullString
	var first, last, created int64
	var acted, snoozed, resolved sql.NullInt64
	if err := row.Scan(&g.ID, &g.OrgKey, &g.Rule, &g.DedupeKey, &g.SubjectKind, &g.SubjectID, &g.SubjectLabel, &g.ScopeKind, &g.ScopeID,
		&addressee, &g.Severity, &g.Weight, &g.Title, &g.Detail, &g.Action, &first, &last, &acted, &snoozed, &resolved, &created); err != nil {
		return Signal{}, err
	}
	g.AddresseeID, g.FirstAt, g.LastAt, g.CreatedAt = addressee.String, fromMs(first), fromMs(last), fromMs(created)
	g.ActedAt, g.SnoozedUntil, g.ResolvedAt = fromNullMs(acted), fromNullMs(snoozed), fromNullMs(resolved)
	return g, nil
}

// SignalByID is one signal, or ErrNotFound.
func (s Store) SignalByID(ctx context.Context, id string) (Signal, error) {
	return scanSignal(s.q.QueryRow(ctx, `SELECT `+signalCols+` FROM org_signals WHERE id = ?`, id))
}

// SignalByKey is the signal of a rule about a subject, open or resolved, or ErrNotFound.
func (s Store) SignalByKey(ctx context.Context, orgKey, rule, dedupeKey string) (Signal, error) {
	return scanSignal(s.q.QueryRow(ctx, `SELECT `+signalCols+` FROM org_signals WHERE org_key = ? AND rule = ? AND dedupe_key = ?`,
		orgKey, rule, dedupeKey))
}

// OpenSignals are the organization's signals not resolved, by creation.
func (s Store) OpenSignals(ctx context.Context, orgKey string) ([]Signal, error) {
	rows, err := s.q.Query(ctx, `SELECT `+signalCols+` FROM org_signals WHERE org_key = ? AND resolved_at IS NULL ORDER BY created_at, id`, orgKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Signal
	for rows.Next() {
		g, err := scanSignal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// AddSignal keeps a new signal; false when one with its (org, rule, key) is there already (a concurrent read made it).
// g.ID is made when empty.
func (s Store) AddSignal(ctx context.Context, g Signal) (bool, error) {
	if g.ID == "" {
		g.ID = NewID()
	}
	res, err := s.q.Exec(ctx, `INSERT INTO org_signals (`+signalCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (org_key, rule, dedupe_key) DO NOTHING`,
		g.ID, g.OrgKey, g.Rule, g.DedupeKey, g.SubjectKind, g.SubjectID, g.SubjectLabel, g.ScopeKind, g.ScopeID, nullStr(g.AddresseeID),
		g.Severity, g.Weight, g.Title, g.Detail, g.Action, Ms(g.FirstAt), Ms(g.LastAt), nullMs(g.ActedAt), nullMs(g.SnoozedUntil),
		nullMs(g.ResolvedAt), Ms(g.CreatedAt))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateSignal writes everything but the identity (id, organization, rule, key) of g.
func (s Store) UpdateSignal(ctx context.Context, g Signal) error {
	_, err := s.q.Exec(ctx, `UPDATE org_signals SET subject_kind = ?, subject_id = ?, subject_label = ?, scope_kind = ?, scope_id = ?,
		addressee_id = ?, severity = ?, weight = ?, title = ?, detail = ?, action = ?, first_at = ?, last_at = ?, acted_at = ?,
		snoozed_until = ?, resolved_at = ? WHERE id = ?`,
		g.SubjectKind, g.SubjectID, g.SubjectLabel, g.ScopeKind, g.ScopeID, nullStr(g.AddresseeID), g.Severity, g.Weight, g.Title,
		g.Detail, g.Action, Ms(g.FirstAt), Ms(g.LastAt), nullMs(g.ActedAt), nullMs(g.SnoozedUntil), nullMs(g.ResolvedAt), g.ID)
	return err
}

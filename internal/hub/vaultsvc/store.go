package vaultsvc

import (
	"context"
	"database/sql"
	stdbase64 "encoding/base64"
	"errors"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// DB is what the safe needs of the hub's database (store.DB, store.Q); queries use ? placeholders.
type DB interface {
	store.Q
}

type row struct {
	ID         string
	OwnerID    string
	Shared     bool
	Origin     string
	Login      string
	Label      *string
	CreatedAt  int64
	UpdatedAt  int64
	LastUsedAt *int64
	sealed     sealed
}

const cols = `id, owner_id, shared, origin, login, label, key_id, nonce, secret, created_at, updated_at, last_used_at`

func scan(sc interface{ Scan(...any) error }) (row, error) {
	var r row
	var shared int
	var label sql.NullString
	var used sql.NullInt64
	var nonce, secret string
	if err := sc.Scan(&r.ID, &r.OwnerID, &shared, &r.Origin, &r.Login, &label, &r.sealed.KeyID, &nonce, &secret, &r.CreatedAt, &r.UpdatedAt, &used); err != nil {
		return row{}, err
	}
	r.Shared = shared == 1
	if label.Valid {
		r.Label = &label.String
	}
	if used.Valid {
		r.LastUsedAt = &used.Int64
	}
	var err error
	if r.sealed.Nonce, err = stdbase64.StdEncoding.DecodeString(nonce); err != nil {
		return row{}, err
	}
	if r.sealed.Secret, err = stdbase64.StdEncoding.DecodeString(secret); err != nil {
		return row{}, err
	}
	return r, nil
}

func scope(r row) string {
	if r.Shared {
		return "shared"
	}
	return "person:" + r.OwnerID
}

func (r row) ad() []byte { return aad(r.ID, scope(r), r.Origin, r.Login) }

func b(v bool) int {
	if v {
		return 1
	}
	return 0
}

// visible: whose entries a person may touch — their own, and shared ones only when seesShared. Always in SQL.
const visible = `((shared = 0 AND owner_id = ?) OR (shared = 1 AND 1 = ?))`

var errDuplicate = errors.New("duplicate entry")

func listVisible(ctx context.Context, q DB, person string, seesShared bool) ([]row, error) {
	rs, err := q.Query(ctx, `SELECT `+cols+` FROM vault_entries WHERE `+visible+` ORDER BY origin, login`, person, b(seesShared))
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		r, err := scan(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func countOwn(ctx context.Context, q DB, person string) (n int, err error) {
	err = q.QueryRow(ctx, `SELECT COUNT(*) FROM vault_entries WHERE owner_id = ?`, person).Scan(&n)
	return
}

// getVisible is an entry the person may see; nil for one that is not there and one that is someone else's alike.
func getVisible(ctx context.Context, q DB, id, person string, seesShared bool) (*row, error) {
	r, err := scan(q.QueryRow(ctx, `SELECT `+cols+` FROM vault_entries WHERE id = ? AND `+visible, id, person, b(seesShared)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func insert(ctx context.Context, q DB, r row, s sealed) error {
	_, err := q.Exec(ctx, `INSERT INTO vault_entries (`+cols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		r.ID, r.OwnerID, b(r.Shared), r.Origin, r.Login, r.Label, s.KeyID,
		stdbase64.StdEncoding.EncodeToString(s.Nonce), stdbase64.StdEncoding.EncodeToString(s.Secret), r.CreatedAt, r.UpdatedAt)
	if store.IsUnique(err) {
		return errDuplicate
	}
	return err
}

// update rewrites login, label and the sealed secret of an entry the editor may change; false: nothing matched.
func update(ctx context.Context, q DB, r row, s sealed, editor string, editsShared bool, at int64) (bool, error) {
	res, err := q.Exec(ctx, `UPDATE vault_entries SET login = ?, label = ?, key_id = ?, nonce = ?, secret = ?, updated_at = ? WHERE id = ? AND `+visible,
		r.Login, r.Label, s.KeyID, stdbase64.StdEncoding.EncodeToString(s.Nonce), stdbase64.StdEncoding.EncodeToString(s.Secret), at, r.ID, editor, b(editsShared))
	if store.IsUnique(err) {
		return false, errDuplicate
	}
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func remove(ctx context.Context, q DB, id, editor string, editsShared bool) (bool, error) {
	res, err := q.Exec(ctx, `DELETE FROM vault_entries WHERE id = ? AND `+visible, id, editor, b(editsShared))
	if err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `DELETE FROM vault_grants WHERE entry_id = ?`, id); err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func markUsed(ctx context.Context, q DB, id string, at int64) error {
	_, err := q.Exec(ctx, `UPDATE vault_entries SET last_used_at = ? WHERE id = ?`, at, id)
	return err
}

func grant(ctx context.Context, q DB, entry, person, agent string, at, until int64) error {
	_, err := q.Exec(ctx, `INSERT INTO vault_grants (entry_id, person_id, agent_key, granted_at, expires_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (entry_id, person_id, agent_key) DO UPDATE SET granted_at = excluded.granted_at, expires_at = excluded.expires_at`,
		entry, person, agent, at, until)
	return err
}

func hasGrant(ctx context.Context, q DB, entry, person, agent string, at int64) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM vault_grants WHERE entry_id = ? AND person_id = ? AND agent_key = ? AND expires_at > ?`, entry, person, agent, at).Scan(&n)
	return n > 0, err
}

func revokeGrants(ctx context.Context, q DB, entry, person string) error {
	_, err := q.Exec(ctx, `DELETE FROM vault_grants WHERE entry_id = ? AND person_id = ?`, entry, person)
	return err
}

type auditEvent struct {
	At       int64   `json:"at"`
	EntryID  *string `json:"entryId"`
	Origin   *string `json:"origin"`
	Action   string  `json:"action"`
	Actor    string  `json:"actor"`
	AgentKey *string `json:"agentKey"`
	Outcome  string  `json:"outcome"`
}

func writeAudit(ctx context.Context, q DB, id, person string, entry, origin *string, action, actor string, agent *string, outcome string, at int64) error {
	_, err := q.Exec(ctx, `INSERT INTO vault_audit (id, at, person_id, entry_id, origin, action, actor, agent_key, outcome) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, at, person, entry, origin, action, actor, agent, outcome)
	return err
}

func listAudit(ctx context.Context, q DB, person string, limit int) ([]auditEvent, error) {
	rs, err := q.Query(ctx, `SELECT at, entry_id, origin, action, actor, agent_key, outcome FROM vault_audit WHERE person_id = ? ORDER BY at DESC, id DESC LIMIT ?`, person, limit)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := []auditEvent{}
	for rs.Next() {
		var e auditEvent
		var entry, origin, agent sql.NullString
		if err := rs.Scan(&e.At, &entry, &origin, &e.Action, &e.Actor, &agent, &e.Outcome); err != nil {
			return nil, err
		}
		if entry.Valid {
			e.EntryID = &entry.String
		}
		if origin.Valid {
			e.Origin = &origin.String
		}
		if agent.Valid {
			e.AgentKey = &agent.String
		}
		out = append(out, e)
	}
	return out, rs.Err()
}

// getByID reads an entry without a person's filter; the caller checks who may have it.
func getByID(ctx context.Context, q DB, id string) (*row, error) {
	r, err := scan(q.QueryRow(ctx, `SELECT `+cols+` FROM vault_entries WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

package store

import (
	"context"
	"database/sql"
	"time"
)

// The states of a TbotJoin (hubproto.TbotJoin… are the same words).
const (
	TbotJoinPending  = "pending"
	TbotJoinApproved = "approved"
	TbotJoinDeclined = "declined"
	TbotJoinRevoked  = "revoked"
)

// TbotJoin is a machine's request for the join token of a Teleport machine identity (docs/hub/tbot-join.md). OwnerEmail
// is the person the bot belongs to, as the cluster names them; only they decide it.
type TbotJoin struct {
	ID          string
	MachineID   string
	MachineName string
	// Host is the host name the machine's setup gave in the request.
	Host        string
	Bot         string
	Token       string
	PublicKey   string
	Fingerprint string
	OwnerEmail  string
	State       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DecidedAt   *time.Time
	DecidedBy   string
}

const tbotJoinCols = `id, machine_id, machine_name, host, bot, token, public_key, fingerprint, owner_email, state, created_at, updated_at, decided_at, decided_by`

func scanTbotJoin(row interface{ Scan(...any) error }) (TbotJoin, error) {
	var j TbotJoin
	var created, updated int64
	var decided sql.NullInt64
	var by sql.NullString
	if err := row.Scan(&j.ID, &j.MachineID, &j.MachineName, &j.Host, &j.Bot, &j.Token, &j.PublicKey, &j.Fingerprint, &j.OwnerEmail, &j.State,
		&created, &updated, &decided, &by); err != nil {
		return TbotJoin{}, err
	}
	j.DecidedBy = by.String
	j.CreatedAt, j.UpdatedAt = fromMs(created), fromMs(updated)
	if decided.Valid {
		at := fromMs(decided.Int64)
		j.DecidedAt = &at
	}
	return j, nil
}

func (s Store) tbotJoins(ctx context.Context, where string, args ...any) ([]TbotJoin, error) {
	rows, err := s.q.Query(ctx, `SELECT `+tbotJoinCols+` FROM tbot_joins WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TbotJoin{}
	for rows.Next() {
		j, err := scanTbotJoin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// TbotJoinByKey is the request of a machine for a token with a key, or ErrNotFound.
func (s Store) TbotJoinByKey(ctx context.Context, machineID, token, fingerprint string) (TbotJoin, error) {
	return scanTbotJoin(s.q.QueryRow(ctx, `SELECT `+tbotJoinCols+` FROM tbot_joins WHERE machine_id = ? AND token = ? AND fingerprint = ?`,
		machineID, token, fingerprint))
}

// TbotJoinByID is one request, or ErrNotFound.
func (s Store) TbotJoinByID(ctx context.Context, id string) (TbotJoin, error) {
	return scanTbotJoin(s.q.QueryRow(ctx, `SELECT `+tbotJoinCols+` FROM tbot_joins WHERE id = ?`, id))
}

// FileTbotJoin keeps a new pending request; j.ID is made when empty.
func (s Store) FileTbotJoin(ctx context.Context, j TbotJoin, now time.Time) (TbotJoin, error) {
	if j.ID == "" {
		j.ID = NewID()
	}
	j.State, j.CreatedAt, j.UpdatedAt = TbotJoinPending, now, now
	_, err := s.q.Exec(ctx, `INSERT INTO tbot_joins (`+tbotJoinCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		j.ID, j.MachineID, j.MachineName, j.Host, j.Bot, j.Token, j.PublicKey, j.Fingerprint, j.OwnerEmail, j.State, Ms(now), Ms(now))
	return j, err
}

// TouchTbotJoin notes that the machine asked again; the owner is the one the cluster names now.
func (s Store) TouchTbotJoin(ctx context.Context, id, ownerEmail, machineName string, now time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE tbot_joins SET updated_at = ?, owner_email = ?, machine_name = ? WHERE id = ?`, Ms(now), ownerEmail, machineName, id)
	return err
}

// TbotJoinsOf are the pending and approved requests a person decides, newest first.
func (s Store) TbotJoinsOf(ctx context.Context, ownerEmail string) ([]TbotJoin, error) {
	return s.tbotJoins(ctx, `owner_email = ? AND state IN (?, ?) ORDER BY created_at DESC, id`, ownerEmail, TbotJoinPending, TbotJoinApproved)
}

// ApprovedTbotJoins are the approved requests for a token, whatever machine or key.
func (s Store) ApprovedTbotJoins(ctx context.Context, token string) ([]TbotJoin, error) {
	return s.tbotJoins(ctx, `token = ? AND state = ? ORDER BY created_at, id`, token, TbotJoinApproved)
}

// MoveTbotJoin moves a request from state from to state to. False when it is no longer in from.
func (s Store) MoveTbotJoin(ctx context.Context, id, from, to, by string, now time.Time) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE tbot_joins SET state = ?, updated_at = ?, decided_at = ?, decided_by = ? WHERE id = ? AND state = ?`,
		to, Ms(now), Ms(now), nullStr(by), id, from)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

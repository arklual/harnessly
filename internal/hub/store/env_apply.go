package store

import (
	"context"
	"time"
)

// AddEnvApplyPending marks a machine for an update run at its next connection (a mark already there is renewed).
func (s Store) AddEnvApplyPending(ctx context.Context, machineID, orgID, by string, at time.Time) error {
	_, err := s.q.Exec(ctx, `INSERT INTO env_apply_pending (machine_id, org_id, by_name, at) VALUES (?, ?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET org_id = excluded.org_id, by_name = excluded.by_name, at = excluded.at`,
		machineID, orgID, by, Ms(at))
	return err
}

// TakeEnvApplyPending removes a machine's mark and says who put it; ok is false when there was none.
func (s Store) TakeEnvApplyPending(ctx context.Context, machineID string) (orgID, by string, ok bool, err error) {
	row := s.q.QueryRow(ctx, `SELECT org_id, by_name FROM env_apply_pending WHERE machine_id = ?`, machineID)
	if err := row.Scan(&orgID, &by); err != nil {
		if IsNotFound(err) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	res, err := s.q.Exec(ctx, `DELETE FROM env_apply_pending WHERE machine_id = ?`, machineID)
	if err != nil {
		return "", "", false, err
	}
	// Two connections at once: only the one that removed the row runs.
	n, _ := res.RowsAffected()
	return orgID, by, n > 0, nil
}

// EnvApplyPendingCount is how many machines of an organization wait for their next connection.
func (s Store) EnvApplyPendingCount(ctx context.Context, orgID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM env_apply_pending WHERE org_id = ?`, orgID).Scan(&n)
	return n, err
}

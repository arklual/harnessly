package store

import (
	"context"
	"time"
)

// TakeLease gives holder the background job `name` until now+ttl and says whether it has it: true for a job nobody
// holds, a lease that ran out, or the holder's own lease (which is extended). A job done by passes takes the lease
// before each pass, so of the replicas of a hub only one does the pass.
func (s Store) TakeLease(ctx context.Context, name, holder string, now time.Time, ttl time.Duration) (bool, error) {
	res, err := s.q.Exec(ctx, `INSERT INTO job_leases (name, holder, lease_until, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET holder = excluded.holder, lease_until = excluded.lease_until, updated_at = excluded.updated_at
WHERE job_leases.holder = excluded.holder OR job_leases.lease_until <= excluded.updated_at`,
		name, holder, Ms(now.Add(ttl)), Ms(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ReleaseLease gives up holder's lease of the job `name`, so another replica takes it at its next try instead of
// waiting for the lease to run out. A lease held by somebody else is left alone.
func (s Store) ReleaseLease(ctx context.Context, name, holder string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM job_leases WHERE name = ? AND holder = ?`, name, holder)
	return err
}

// LiveLeases is the names of the leases that start with prefix (no % or _ in it) and have not run out at now:
// a lease is also how a replica or a pipeline says for how long something lasts (internal/hub/rollout).
func (s Store) LiveLeases(ctx context.Context, prefix string, now time.Time) ([]string, error) {
	rows, err := s.q.Query(ctx, `SELECT name FROM job_leases WHERE name LIKE ? AND lease_until > ? ORDER BY name`,
		prefix+"%", Ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

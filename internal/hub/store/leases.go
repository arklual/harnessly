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

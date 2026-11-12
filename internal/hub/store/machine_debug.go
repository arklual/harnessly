package store

import (
	"context"
	"time"
)

// DebugOpening is one debug opening of a machine: a person whose role manages the machine reads its owner's work for a
// while, with a reason the owner sees (docs/hub/own-work.md, «Debug mode»).
type DebugOpening struct {
	ID        string
	MachineID string
	UserID    string
	Reason    string
	Created   time.Time
	Expires   time.Time
}

// AddDebugOpening keeps an opening; an empty ID is given one.
func (s Store) AddDebugOpening(ctx context.Context, o *DebugOpening) error {
	if o.ID == "" {
		o.ID = NewID()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO machine_debug_openings (id, machine_id, user_id, reason, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		o.ID, o.MachineID, o.UserID, o.Reason, Ms(o.Created), Ms(o.Expires))
	return err
}

// DebugOpen says whether the person has an opening of the machine that has not run out at now.
func (s Store) DebugOpen(ctx context.Context, machineID, userID string, now time.Time) (bool, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM machine_debug_openings WHERE machine_id = ? AND user_id = ? AND expires_at > ? LIMIT 1) t`,
		machineID, userID, Ms(now)).Scan(&n)
	return n > 0, err
}

// DebugOpenings are the machine's openings made at or after since, newest first, at most limit.
func (s Store) DebugOpenings(ctx context.Context, machineID string, since time.Time, limit int) ([]DebugOpening, error) {
	rows, err := s.q.Query(ctx, `SELECT id, machine_id, user_id, reason, created_at, expires_at FROM machine_debug_openings
		WHERE machine_id = ? AND created_at >= ? ORDER BY created_at DESC, id DESC LIMIT ?`, machineID, Ms(since), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DebugOpening
	for rows.Next() {
		var o DebugOpening
		var created, expires int64
		if err := rows.Scan(&o.ID, &o.MachineID, &o.UserID, &o.Reason, &created, &expires); err != nil {
			return nil, err
		}
		o.Created, o.Expires = fromMs(created), fromMs(expires)
		out = append(out, o)
	}
	return out, rows.Err()
}

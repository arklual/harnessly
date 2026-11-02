package store

import (
	"context"
	"database/sql"
	"time"
)

// What the organization's machine screens read and write besides the machine row (docs/hub/analytics.md, «Machines»).

// Machine access kinds.
const (
	AccessUser = "user"
	AccessTeam = "team"
)

// MachineAccessEntry is one person or team a machine is shared with.
type MachineAccessEntry struct {
	MachineID string
	Kind      string
	RefID     string
}

// MachineAccess lists who the given machines are shared with; a machine without entries is open to its whole tenant.
func (s Store) MachineAccess(ctx context.Context, machineIDs []string) ([]MachineAccessEntry, error) {
	var out []MachineAccessEntry
	for i := 0; i < len(machineIDs); i += chunk {
		part := machineIDs[i:min(i+chunk, len(machineIDs))]
		args := make([]any, len(part))
		for j, id := range part {
			args[j] = id
		}
		rows, err := s.q.Query(ctx, `SELECT machine_id, kind, ref_id FROM machine_access WHERE machine_id IN (`+placeholders(len(part))+`)
			ORDER BY machine_id, created_at, kind, ref_id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e MachineAccessEntry
			if err := rows.Scan(&e.MachineID, &e.Kind, &e.RefID); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, e)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// SetMachineAccess replaces who a machine is shared with.
func (s Store) SetMachineAccess(ctx context.Context, machineID string, entries []MachineAccessEntry, at time.Time) error {
	if _, err := s.q.Exec(ctx, `DELETE FROM machine_access WHERE machine_id = ?`, machineID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := s.q.Exec(ctx, `INSERT INTO machine_access (machine_id, kind, ref_id, created_at) VALUES (?, ?, ?, ?)`,
			machineID, e.Kind, e.RefID, Ms(at)); err != nil {
			return err
		}
	}
	return nil
}

// MachineUse is what one person did on a machine, from the sessions the hub keeps.
type MachineUse struct {
	MachineID string
	UserID    string
	Origin    string
	// Last is the latest update of one of their top-level sessions there (epoch ms).
	Last int64
	// Running counts the sessions not ended and updated at or after the cutoff the read was given.
	Running int
}

// MachineUses reads the top-level sessions of the machines by person and origin: the latest update and how many are
// still running (not ended, updated at or after runningSince, epoch ms).
func (s Store) MachineUses(ctx context.Context, machineIDs []string, runningSince int64) ([]MachineUse, error) {
	var out []MachineUse
	for i := 0; i < len(machineIDs); i += chunk {
		part := machineIDs[i:min(i+chunk, len(machineIDs))]
		args := []any{runningSince}
		for _, id := range part {
			args = append(args, id)
		}
		rows, err := s.q.Query(ctx, `SELECT machine_id, user_id, origin, MAX(updated_at),
			SUM(CASE WHEN ended_at IS NULL AND updated_at >= ? THEN 1 ELSE 0 END)
			FROM agent_sessions WHERE parent_key = '' AND machine_id IN (`+placeholders(len(part))+`)
			GROUP BY machine_id, user_id, origin`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var u MachineUse
			var user sql.NullString
			var running sql.NullInt64
			if err := rows.Scan(&u.MachineID, &user, &u.Origin, &u.Last, &running); err != nil {
				rows.Close()
				return nil, err
			}
			u.UserID, u.Running = user.String, int(running.Int64)
			out = append(out, u)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

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

// Machine access levels (docs/hub/own-work.md, «Levels»): how far an entry opens the machine's work.
const (
	// AccessTasks lets the person write in the chats and start the agent, besides reading; the level of an entry kept
	// without one.
	AccessTasks = "tasks"
	// AccessView lets the person read the chats and their results, and nothing else.
	AccessView = "view"
)

// MachineAccessEntry is one person or team a machine is shared with.
type MachineAccessEntry struct {
	MachineID string
	Kind      string
	RefID     string
	// Level is AccessTasks or AccessView; empty is AccessTasks.
	Level string
}

// AccessLevel is the entry's level, AccessTasks when it has none.
func (e MachineAccessEntry) AccessLevel() string {
	if e.Level == AccessView {
		return AccessView
	}
	return AccessTasks
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
		rows, err := s.q.Query(ctx, `SELECT machine_id, kind, ref_id, level FROM machine_access WHERE machine_id IN (`+placeholders(len(part))+`)
			ORDER BY machine_id, created_at, kind, ref_id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e MachineAccessEntry
			var level sql.NullString
			if err := rows.Scan(&e.MachineID, &e.Kind, &e.RefID, &level); err != nil {
				rows.Close()
				return nil, err
			}
			e.Level = MachineAccessEntry{Level: level.String}.AccessLevel()
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
		// Tasks is kept as NULL, the level every row had before there were levels.
		var level any
		if e.AccessLevel() == AccessView {
			level = AccessView
		}
		if _, err := s.q.Exec(ctx, `INSERT INTO machine_access (machine_id, kind, ref_id, level, created_at) VALUES (?, ?, ?, ?, ?)`,
			machineID, e.Kind, e.RefID, level, Ms(at)); err != nil {
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

// MachineAccountUse is the account a machine's agent of one provider worked on at its latest session that had one.
type MachineAccountUse struct {
	Provider  string
	AccountID string
	Last      int64
}

// MachineAccountUses reads, for each provider, the account of the machine's newest top-level session that names one.
func (s Store) MachineAccountUses(ctx context.Context, machineID string) ([]MachineAccountUse, error) {
	rows, err := s.q.Query(ctx, `SELECT provider, account_id, MAX(updated_at) FROM agent_sessions
		WHERE machine_id = ? AND account_id IS NOT NULL AND account_id <> '' GROUP BY provider, account_id
		ORDER BY MAX(updated_at) DESC`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MachineAccountUse
	seen := map[string]bool{}
	for rows.Next() {
		var u MachineAccountUse
		if err := rows.Scan(&u.Provider, &u.AccountID, &u.Last); err != nil {
			return nil, err
		}
		if !seen[u.Provider] {
			seen[u.Provider] = true
			out = append(out, u)
		}
	}
	return out, rows.Err()
}

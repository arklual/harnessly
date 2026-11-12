package store

import (
	"context"
	"database/sql"
	"errors"
)

// ProviderWindow is one subscription window of a provider on a machine (migration 0060).
type ProviderWindow struct {
	MachineID string
	Provider  string // "claude", "codex"
	Window    string // "five_hour", "seven_day"
	UsedPct   float64
	ResetsAt  int64 // Unix seconds, 0 when unknown
	SampledAt int64 // Unix milliseconds, the hub's clock
}

// PutProviderWindow replaces the machine's reading of one window.
func (s Store) PutProviderWindow(ctx context.Context, w ProviderWindow) error {
	_, err := s.q.Exec(ctx, `INSERT INTO machine_provider_limits (machine_id, provider, win, used_pct, resets_at, sampled_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, provider, win) DO UPDATE SET used_pct = excluded.used_pct, resets_at = excluded.resets_at, sampled_at = excluded.sampled_at`,
		w.MachineID, w.Provider, w.Window, w.UsedPct, w.ResetsAt, w.SampledAt)
	return err
}

// ProviderWindowsOfMachine reads all windows a machine reported.
func (s Store) ProviderWindowsOfMachine(ctx context.Context, machineID string) ([]ProviderWindow, error) {
	rows, err := s.q.Query(ctx, `SELECT machine_id, provider, win, used_pct, resets_at, sampled_at FROM machine_provider_limits
		WHERE machine_id = ? ORDER BY provider, win`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderWindow
	for rows.Next() {
		var w ProviderWindow
		if err := rows.Scan(&w.MachineID, &w.Provider, &w.Window, &w.UsedPct, &w.ResetsAt, &w.SampledAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// FullestProviderWindow is the most used reading of a window among the not revoked machines a person made in an organization's spaces, read not
// before sinceMs; ErrNotFound when there is none.
func (s Store) FullestProviderWindow(ctx context.Context, orgID, userID, provider, window string, sinceMs int64) (ProviderWindow, error) {
	var w ProviderWindow
	err := s.q.QueryRow(ctx, `SELECT l.machine_id, l.provider, l.win, l.used_pct, l.resets_at, l.sampled_at
		FROM machine_provider_limits l JOIN machines m ON m.id = l.machine_id JOIN spaces sp ON sp.id = m.space_id
		WHERE sp.org_id = ? AND m.created_by = ? AND m.revoked_at IS NULL AND l.provider = ? AND l.win = ? AND l.sampled_at >= ?
		ORDER BY l.used_pct DESC, l.machine_id LIMIT 1`, orgID, userID, provider, window, sinceMs).
		Scan(&w.MachineID, &w.Provider, &w.Window, &w.UsedPct, &w.ResetsAt, &w.SampledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderWindow{}, ErrNotFound
	}
	return w, err
}

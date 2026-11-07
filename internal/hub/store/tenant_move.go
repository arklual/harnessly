package store

import "context"

// MoveMachine takes a live machine to another space, keeping its row (so its id, key and links): it lands in teamID
// ("" for none). Its access list is the caller's to clear.
func (s Store) MoveMachine(ctx context.Context, machineID, spaceID, teamID string) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET space_id = ?, team_id = ? WHERE id = ? AND revoked_at IS NULL`, spaceID, nullStr(teamID), machineID)
	return err
}

// CountRevokedMachines is how many revoked machines a space still holds on its row.
func (s Store) CountRevokedMachines(ctx context.Context, spaceID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM machines WHERE space_id = ? AND revoked_at IS NOT NULL`, spaceID).Scan(&n)
	return n, err
}

// DetachRevokedMachines takes the revoked machines off a space, so that deleting it does not delete their rows (their
// history is keyed by the machine).
func (s Store) DetachRevokedMachines(ctx context.Context, spaceID string) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET space_id = NULL, team_id = NULL WHERE space_id = ? AND revoked_at IS NOT NULL`, spaceID)
	return err
}

// CountSitesOfSpace is how many sites carry the space's slug in their address.
func (s Store) CountSitesOfSpace(ctx context.Context, slug string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM sites WHERE space = ?`, slug).Scan(&n)
	return n, err
}

// CountEnvEntriesOfTenant is how many environment entries are set at the tenant's layer.
func (s Store) CountEnvEntriesOfTenant(ctx context.Context, spaceID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM env_entries WHERE layer = ? AND scope_id = ?`, EnvLayerTenant, spaceID).Scan(&n)
	return n, err
}

// CountLimitRulesOfTenant is how many limit rules the tenant and its teams carry.
func (s Store) CountLimitRulesOfTenant(ctx context.Context, spaceID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM limit_rules WHERE (scope_kind = ? AND scope_id = ?) OR org_key = ?
		OR (scope_kind = ? AND scope_id IN (SELECT id FROM teams WHERE space_id = ?))`,
		ScopeTenant, spaceID, "space:"+spaceID, ScopeTeam, spaceID).Scan(&n)
	return n, err
}

// CountActiveInvitesOfSpace counts the invites of a space that are neither revoked nor expired at nowMs.
func (s Store) CountActiveInvitesOfSpace(ctx context.Context, spaceID string, nowMs int64) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM invites WHERE space_id = ? AND revoked_at IS NULL AND expires_at > ?`, spaceID, nowMs).Scan(&n)
	return n, err
}

// DeleteSpace removes a space row. What hangs on it by a foreign key (members, invites, codes, the ssh key) goes with
// it; the caller has checked the rest is empty.
func (s Store) DeleteSpace(ctx context.Context, id string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM spaces WHERE id = ?`, id)
	return err
}

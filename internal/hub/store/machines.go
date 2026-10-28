package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

// Machine is a registered machine. SpaceID is "" for the local machine before an admin claims it.
type Machine struct {
	ID           string
	SpaceID      string
	Name         string
	Hostname     string
	OS           string
	Arch         string
	UnixUser     string
	Profile      string
	Via          string
	KeyPub       string // Ed25519, base64 std
	EncPub       string // X25519, base64 std
	AgentVersion string
	CreatedBy    string
	CreatedAt    time.Time
	LastSeenAt   *time.Time
	RevokedAt    *time.Time
	Info         string // JSON: the last preflight report, agent statuses, policy and proxy, for offline display
}

const machineCols = `id, space_id, name, hostname, os, arch, unix_user, profile, via, key_pub, enc_pub, agent_version, created_by, created_at, last_seen_at, revoked_at, info`

func scanMachine(row interface{ Scan(...any) error }) (Machine, error) {
	var m Machine
	var space, createdBy sql.NullString
	var created int64
	var seen, revoked sql.NullInt64
	if err := row.Scan(&m.ID, &space, &m.Name, &m.Hostname, &m.OS, &m.Arch, &m.UnixUser, &m.Profile, &m.Via, &m.KeyPub, &m.EncPub,
		&m.AgentVersion, &createdBy, &created, &seen, &revoked, &m.Info); err != nil {
		return Machine{}, err
	}
	m.SpaceID, m.CreatedBy = space.String, createdBy.String
	m.CreatedAt, m.LastSeenAt, m.RevokedAt = fromMs(created), fromNullMs(seen), fromNullMs(revoked)
	return m, nil
}

func (s Store) CreateMachine(ctx context.Context, m *Machine) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if m.Info == "" {
		m.Info = "{}"
	}
	_, err := s.q.Exec(ctx, `INSERT INTO machines (`+machineCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, nullStr(m.SpaceID), m.Name, m.Hostname, m.OS, m.Arch, m.UnixUser, m.Profile, m.Via, m.KeyPub, m.EncPub,
		m.AgentVersion, nullStr(m.CreatedBy), Ms(m.CreatedAt), nullMs(m.LastSeenAt), nullMs(m.RevokedAt), m.Info)
	return err
}

func (s Store) MachineByID(ctx context.Context, id string) (Machine, error) {
	return scanMachine(s.q.QueryRow(ctx, `SELECT `+machineCols+` FROM machines WHERE id = ?`, id))
}

// MachinesBySpace lists the space's machines that are not revoked, oldest first.
func (s Store) MachinesBySpace(ctx context.Context, spaceID string) ([]Machine, error) {
	rows, err := s.q.Query(ctx, `SELECT `+machineCols+` FROM machines WHERE space_id = ? AND revoked_at IS NULL ORDER BY created_at, id`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UnclaimedLocalMachine is the local machine that registered into no space yet, if any.
func (s Store) UnclaimedLocalMachine(ctx context.Context) (Machine, error) {
	return scanMachine(s.q.QueryRow(ctx, `SELECT `+machineCols+` FROM machines WHERE space_id IS NULL AND via = 'local' AND revoked_at IS NULL ORDER BY created_at DESC, id LIMIT 1`))
}

// RevokeUnclaimedLocal revokes the local machines nobody claimed and returns their ids: a local machine that registers
// again (its volume was wiped) replaces the one that was waiting.
func (s Store) RevokeUnclaimedLocal(ctx context.Context, at time.Time) ([]string, error) {
	rows, err := s.q.Query(ctx, `SELECT id FROM machines WHERE space_id IS NULL AND via = 'local' AND revoked_at IS NULL`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := s.RevokeMachine(ctx, id, at); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// ClaimMachine puts an unclaimed machine into a space; false when it was claimed already.
func (s Store) ClaimMachine(ctx context.Context, id, spaceID, userID string) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE machines SET space_id = ?, created_by = ? WHERE id = ? AND space_id IS NULL AND revoked_at IS NULL`, spaceID, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s Store) RenameMachine(ctx context.Context, id, name string) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET name = ? WHERE id = ?`, name, id)
	return err
}

// SetMachineVia records how a machine was connected, for the ways that learn it after the machine registered.
func (s Store) SetMachineVia(ctx context.Context, id, via string) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET via = ? WHERE id = ?`, via, id)
	return err
}

// UpdateMachineHello records what the agent said about itself when it connected.
func (s Store) UpdateMachineHello(ctx context.Context, m Machine) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET hostname = ?, os = ?, arch = ?, unix_user = ?, agent_version = ? WHERE id = ?`,
		m.Hostname, m.OS, m.Arch, m.UnixUser, m.AgentVersion, m.ID)
	return err
}

func (s Store) UpdateMachineInfo(ctx context.Context, id, info string) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET info = ? WHERE id = ?`, info, id)
	return err
}

func (s Store) TouchMachine(ctx context.Context, id string, at time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET last_seen_at = ? WHERE id = ?`, Ms(at), id)
	return err
}

// RevokeMachine marks a machine revoked and ends its runtime-origin sessions; false when it already was.
func (s Store) RevokeMachine(ctx context.Context, id string, at time.Time) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE machines SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, Ms(at), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	return true, s.DeleteMachineSessions(ctx, id)
}

// RegCode is a one-time machine registration code (only its hash is kept).
type RegCode struct {
	CodeHash  string
	SpaceID   string
	CreatedBy string
	Profile   string
	Local     bool
	ExpiresAt time.Time
	UsedAt    *time.Time
	MachineID string
}

const regCodeCols = `code_hash, space_id, created_by, profile, local, expires_at, used_at, machine_id`

func scanRegCode(row interface{ Scan(...any) error }) (RegCode, error) {
	var c RegCode
	var space, by, machine sql.NullString
	var local int
	var expires int64
	var used sql.NullInt64
	if err := row.Scan(&c.CodeHash, &space, &by, &c.Profile, &local, &expires, &used, &machine); err != nil {
		return RegCode{}, err
	}
	c.SpaceID, c.CreatedBy, c.MachineID = space.String, by.String, machine.String
	c.Local, c.ExpiresAt, c.UsedAt = local != 0, fromMs(expires), fromNullMs(used)
	return c, nil
}

func (s Store) CreateRegCode(ctx context.Context, c RegCode) error {
	_, err := s.q.Exec(ctx, `INSERT INTO registration_codes (`+regCodeCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.CodeHash, nullStr(c.SpaceID), nullStr(c.CreatedBy), c.Profile, boolInt(c.Local), Ms(c.ExpiresAt), nullMs(c.UsedAt), nullStr(c.MachineID))
	return err
}

func (s Store) RegCodeByHash(ctx context.Context, hash string) (RegCode, error) {
	return scanRegCode(s.q.QueryRow(ctx, `SELECT `+regCodeCols+` FROM registration_codes WHERE code_hash = ?`, hash))
}

// ConsumeRegCode marks the code used by a machine, only while it is unused and unexpired; false otherwise.
func (s Store) ConsumeRegCode(ctx context.Context, hash, machineID string, now time.Time) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE registration_codes SET used_at = ?, machine_id = ? WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`,
		Ms(now), machineID, hash, Ms(now))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CountActiveRegCodes counts the unused, unexpired codes of a space.
func (s Store) CountActiveRegCodes(ctx context.Context, spaceID string, now time.Time) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM registration_codes WHERE space_id = ? AND used_at IS NULL AND expires_at > ?`, spaceID, Ms(now)).Scan(&n)
	return n, err
}

// DeleteLocalRegCodes drops the local codes: the hub is about to make a fresh one.
func (s Store) DeleteLocalRegCodes(ctx context.Context) error {
	_, err := s.q.Exec(ctx, `DELETE FROM registration_codes WHERE local = 1`)
	return err
}

// ActiveLocalRegCode is the local code that can still be used, if any.
func (s Store) ActiveLocalRegCode(ctx context.Context, now time.Time) (RegCode, error) {
	return scanRegCode(s.q.QueryRow(ctx, `SELECT `+regCodeCols+` FROM registration_codes WHERE local = 1 AND used_at IS NULL AND expires_at > ? ORDER BY expires_at DESC LIMIT 1`, Ms(now)))
}

// AgentConnection is the queryable summary of an agent on a machine. Never a secret.
type AgentConnection struct {
	MachineID       string
	Provider        string
	Installed       bool
	Version         string
	LoginState      string
	AuthMode        string
	ProviderVerdict string
	CheckedAt       *time.Time
}

func (s Store) UpsertAgentConnection(ctx context.Context, c AgentConnection) error {
	_, err := s.q.Exec(ctx, `INSERT INTO agent_connections (machine_id, provider, installed, version, login_state, auth_mode, provider_verdict, checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, provider) DO UPDATE SET installed = excluded.installed, version = excluded.version,
		login_state = excluded.login_state, auth_mode = excluded.auth_mode, provider_verdict = excluded.provider_verdict, checked_at = excluded.checked_at`,
		c.MachineID, c.Provider, boolInt(c.Installed), c.Version, c.LoginState, c.AuthMode, c.ProviderVerdict, nullMs(c.CheckedAt))
	return err
}

func (s Store) AgentConnections(ctx context.Context, machineID string) ([]AgentConnection, error) {
	rows, err := s.q.Query(ctx, `SELECT machine_id, provider, installed, version, login_state, auth_mode, provider_verdict, checked_at FROM agent_connections WHERE machine_id = ? ORDER BY provider`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentConnection
	for rows.Next() {
		var c AgentConnection
		var installed int
		var checked sql.NullInt64
		if err := rows.Scan(&c.MachineID, &c.Provider, &installed, &c.Version, &c.LoginState, &c.AuthMode, &c.ProviderVerdict, &checked); err != nil {
			return nil, err
		}
		c.Installed, c.CheckedAt = installed != 0, fromNullMs(checked)
		out = append(out, c)
	}
	return out, rows.Err()
}

// EnvNames lists the names of the secrets delivered to a machine, sorted. Never the values.
func (s Store) EnvNames(ctx context.Context, machineID string) ([]string, error) {
	rows, err := s.q.Query(ctx, `SELECT name FROM machine_env WHERE machine_id = ?`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out, rows.Err()
}

func (s Store) RecordEnv(ctx context.Context, machineID, name, userID string, at time.Time) error {
	_, err := s.q.Exec(ctx, `INSERT INTO machine_env (machine_id, name, delivered_at, delivered_by) VALUES (?, ?, ?, ?)
		ON CONFLICT (machine_id, name) DO UPDATE SET delivered_at = excluded.delivered_at, delivered_by = excluded.delivered_by`,
		machineID, name, Ms(at), nullStr(userID))
	return err
}

func (s Store) ForgetEnv(ctx context.Context, machineID, name string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM machine_env WHERE machine_id = ? AND name = ?`, machineID, name)
	return err
}

// SetEnvNames makes the recorded names of a machine exactly names (what the agent says it has).
func (s Store) SetEnvNames(ctx context.Context, machineID string, names []string, userID string, at time.Time) error {
	if _, err := s.q.Exec(ctx, `DELETE FROM machine_env WHERE machine_id = ?`, machineID); err != nil {
		return err
	}
	for _, n := range names {
		if err := s.RecordEnv(ctx, machineID, n, userID, at); err != nil {
			return err
		}
	}
	return nil
}

// IsNotFound says whether err is the "no such row" error.
func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

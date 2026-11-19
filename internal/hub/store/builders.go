package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// The roles of a machine. A machine with no row is an agents' machine.
const (
	RoleAgents  = "agents"
	RoleBuilder = "builder"
)

// MachineBuilder is the role of a machine in the pool: agents (chats are placed on it) or builder (heavy jobs are, and
// chats never are). Caps are its labels, Slots how many heavy jobs it runs at once.
type MachineBuilder struct {
	MachineID string
	Role      string
	Caps      []string
	Slots     int
	UpdatedBy string
	UpdatedAt time.Time
}

// Builder says the machine is a builder.
func (b MachineBuilder) Builder() bool { return b.Role == RoleBuilder }

// SplitCaps reads the comma separated labels of a column.
func SplitCaps(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// JoinCaps writes labels for a column.
func JoinCaps(caps []string) string { return strings.Join(caps, ",") }

const builderCols = `machine_id, role, caps, slots, updated_by, updated_at`

func scanBuilder(row interface{ Scan(...any) error }) (MachineBuilder, error) {
	var b MachineBuilder
	var caps string
	var by sql.NullString
	var at int64
	if err := row.Scan(&b.MachineID, &b.Role, &caps, &b.Slots, &by, &at); err != nil {
		return MachineBuilder{}, err
	}
	b.Caps, b.UpdatedBy, b.UpdatedAt = SplitCaps(caps), by.String, fromMs(at)
	return b, nil
}

// SetMachineBuilder gives a machine its role in the pool.
func (s Store) SetMachineBuilder(ctx context.Context, b MachineBuilder) error {
	if b.UpdatedAt.IsZero() {
		b.UpdatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO machine_builders (`+builderCols+`) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET role = excluded.role, caps = excluded.caps, slots = excluded.slots, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		b.MachineID, b.Role, JoinCaps(b.Caps), b.Slots, nullStr(b.UpdatedBy), Ms(b.UpdatedAt))
	return err
}

// MachineBuilderOf is a machine's role; a machine with none is an agents' machine (ok false).
func (s Store) MachineBuilderOf(ctx context.Context, machineID string) (MachineBuilder, bool, error) {
	b, err := scanBuilder(s.q.QueryRow(ctx, `SELECT `+builderCols+` FROM machine_builders WHERE machine_id = ?`, machineID))
	if IsNotFound(err) {
		return MachineBuilder{MachineID: machineID, Role: RoleAgents}, false, nil
	}
	return b, err == nil, err
}

// BuildersBySpace lists the roles of the space's machines that are builders and not revoked, by machine id.
func (s Store) BuildersBySpace(ctx context.Context, spaceID string) (map[string]MachineBuilder, error) {
	rows, err := s.q.Query(ctx, `SELECT b.machine_id, b.role, b.caps, b.slots, b.updated_by, b.updated_at FROM machine_builders b
		JOIN machines m ON m.id = b.machine_id WHERE m.space_id = ? AND m.revoked_at IS NULL`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]MachineBuilder{}
	for rows.Next() {
		b, err := scanBuilder(rows)
		if err != nil {
			return nil, err
		}
		out[b.MachineID] = b
	}
	return out, rows.Err()
}

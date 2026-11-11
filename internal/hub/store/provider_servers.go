package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ProviderServer is a server at the provider that the hub asked for or took over, with the space it is of.
type ProviderServer struct {
	ID         string
	SpaceID    string
	Provider   string
	ExternalID string
	OpID       string
	Name       string
	CreatedBy  string
	CreatedAt  time.Time
}

// AddProviderServer records a server for its space. A server another row names already is a unique violation.
func (s Store) AddProviderServer(ctx context.Context, p *ProviderServer) error {
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO provider_servers (id, space_id, provider, external_id, op_id, name, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, p.SpaceID, p.Provider, p.ExternalID, p.OpID, p.Name, p.CreatedBy, Ms(p.CreatedAt))
	return err
}

// ProviderServerOfSpace reports whether the space has the provider's server: one it made or took over.
func (s Store) ProviderServerOfSpace(ctx context.Context, spaceID, provider, externalID string) (bool, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM provider_servers WHERE space_id = ? AND provider = ? AND external_id = ?`,
		spaceID, provider, externalID).Scan(&n)
	return n > 0, err
}

// ProviderServerKnown reports whether any space has the provider's server.
func (s Store) ProviderServerKnown(ctx context.Context, provider, externalID string) (bool, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM provider_servers WHERE provider = ? AND external_id = ?`, provider, externalID).Scan(&n)
	return n > 0, err
}

// NameProviderServer writes the provider's id of the server an operation of the space made, once the operation says it.
func (s Store) NameProviderServer(ctx context.Context, spaceID, provider, opID, externalID string) error {
	_, err := s.q.Exec(ctx, `UPDATE provider_servers SET external_id = ? WHERE space_id = ? AND provider = ? AND op_id = ? AND external_id = ''`,
		externalID, spaceID, provider, opID)
	return err
}

// ProviderOp is an operation the hub started at the provider.
type ProviderOp struct {
	Provider  string
	OpID      string
	SpaceID   string
	MachineID string
	Kind      string
	CreatedBy string
	CreatedAt time.Time
}

// AddProviderOp records who started an operation; one recorded already stays as it was.
func (s Store) AddProviderOp(ctx context.Context, o ProviderOp) error {
	if o.CreatedAt.IsZero() {
		o.CreatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO provider_ops (provider, op_id, space_id, machine_id, kind, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider, op_id) DO NOTHING`, o.Provider, o.OpID, o.SpaceID, o.MachineID, o.Kind, o.CreatedBy, Ms(o.CreatedAt))
	return err
}

// ProviderOpOfSpace reads an operation the space started; ErrNotFound for one it did not.
func (s Store) ProviderOpOfSpace(ctx context.Context, spaceID, provider, opID string) (ProviderOp, error) {
	o := ProviderOp{Provider: provider, OpID: opID, SpaceID: spaceID}
	var at int64
	err := s.q.QueryRow(ctx, `SELECT machine_id, kind, created_by, created_at FROM provider_ops WHERE space_id = ? AND provider = ? AND op_id = ?`,
		spaceID, provider, opID).Scan(&o.MachineID, &o.Kind, &o.CreatedBy, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	o.CreatedAt = fromMs(at)
	return o, err
}

// ProviderOpOfMachine reports whether the operation was started on this machine: through the provider calls, or as
// the provider's side of one of the machine's own operations.
func (s Store) ProviderOpOfMachine(ctx context.Context, m Machine, provider, opID string) (bool, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM provider_ops WHERE space_id = ? AND provider = ? AND op_id = ? AND machine_id = ?)
		+ (SELECT COUNT(*) FROM machine_ops WHERE machine_id = ? AND provider_op_id = ?)`, m.SpaceID, provider, opID, m.ID, m.ID, opID).Scan(&n)
	return n > 0, err
}

// ResourceConflict is a machine that lost its tie to a server another machine named first.
type ResourceConflict struct {
	MachineID, Provider, ExternalID, KeptMachineID string
}

// ResourceConflicts lists the machines whose tie to a provider's server was dropped because another machine had it.
func (s Store) ResourceConflicts(ctx context.Context) ([]ResourceConflict, error) {
	rows, err := s.q.Query(ctx, `SELECT machine_id, provider, external_id, kept_machine_id FROM machine_resource_conflicts ORDER BY noted_at, machine_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResourceConflict
	for rows.Next() {
		var c ResourceConflict
		if err := rows.Scan(&c.MachineID, &c.Provider, &c.ExternalID, &c.KeptMachineID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountProviderServers is how many servers the space asked a provider for or tied to itself.
func (s Store) CountProviderServers(ctx context.Context, spaceID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM provider_servers WHERE space_id = ?`, spaceID).Scan(&n)
	return n, err
}

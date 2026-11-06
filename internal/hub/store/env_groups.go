package store

import (
	"context"
	"database/sql"
	"time"
)

// EnvGroup is a named group of a layer's entries (docs/hub/environment.md, «Groups»).
type EnvGroup struct {
	ID      string
	OrgID   string
	Layer   string
	ScopeID string
	Name    string
	Hint    string
	// Required: the group's items are pinned for everybody the layer reaches. Otherwise each person chooses.
	Required bool
	// DefaultOn: an optional group is on for a person who has not chosen.
	DefaultOn bool
	Position  int
	UpdatedAt time.Time
	UpdatedBy string
}

const envGroupCols = `id, org_id, layer, scope_id, name, hint, required, default_on, position, updated_at, updated_by`

func scanEnvGroup(row interface{ Scan(...any) error }) (EnvGroup, error) {
	var g EnvGroup
	var required, defaultOn int
	var at int64
	var by sql.NullString
	if err := row.Scan(&g.ID, &g.OrgID, &g.Layer, &g.ScopeID, &g.Name, &g.Hint, &required, &defaultOn, &g.Position, &at, &by); err != nil {
		return EnvGroup{}, err
	}
	g.Required, g.DefaultOn, g.UpdatedAt, g.UpdatedBy = required != 0, defaultOn != 0, fromMs(at), by.String
	return g, nil
}

// AddEnvGroup keeps a new group; g.ID is made when empty. False when the layer has a group of that name.
func (s Store) AddEnvGroup(ctx context.Context, g EnvGroup) (EnvGroup, bool, error) {
	if g.ID == "" {
		g.ID = NewID()
	}
	res, err := s.q.Exec(ctx, `INSERT INTO env_groups (`+envGroupCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (layer, scope_id, name) DO NOTHING`,
		g.ID, g.OrgID, g.Layer, g.ScopeID, g.Name, g.Hint, envInt(g.Required), envInt(g.DefaultOn), g.Position, Ms(g.UpdatedAt), nullStr(g.UpdatedBy))
	if err != nil {
		return EnvGroup{}, false, err
	}
	n, _ := res.RowsAffected()
	return g, n > 0, nil
}

// EnvGroupByID is one group, or ErrNotFound.
func (s Store) EnvGroupByID(ctx context.Context, id string) (EnvGroup, error) {
	return scanEnvGroup(s.q.QueryRow(ctx, `SELECT `+envGroupCols+` FROM env_groups WHERE id = ?`, id))
}

// EnvGroupsOfOrg are the groups of all the layers of an organization, by layer, scope, position and name.
func (s Store) EnvGroupsOfOrg(ctx context.Context, orgID string) ([]EnvGroup, error) {
	rows, err := s.q.Query(ctx, `SELECT `+envGroupCols+` FROM env_groups WHERE org_id = ? ORDER BY layer, scope_id, position, name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnvGroup
	for rows.Next() {
		g, err := scanEnvGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UpdateEnvGroup writes name, hint, required, default_on, position, updated_at and updated_by of g.
func (s Store) UpdateEnvGroup(ctx context.Context, g EnvGroup) error {
	_, err := s.q.Exec(ctx, `UPDATE env_groups SET name = ?, hint = ?, required = ?, default_on = ?, position = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
		g.Name, g.Hint, envInt(g.Required), envInt(g.DefaultOn), g.Position, Ms(g.UpdatedAt), nullStr(g.UpdatedBy), g.ID)
	return err
}

// DeleteEnvGroup removes a group; its entries stay, without a group, and the people's choices of it go. Call it in a
// transaction (DB.Tx) when the three steps must go together.
func (s Store) DeleteEnvGroup(ctx context.Context, id string) error {
	if _, err := s.q.Exec(ctx, `UPDATE env_entries SET group_id = NULL WHERE group_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.q.Exec(ctx, `DELETE FROM env_person_group WHERE group_id = ?`, id); err != nil {
		return err
	}
	_, err := s.q.Exec(ctx, `DELETE FROM env_groups WHERE id = ?`, id)
	return err
}

// EnvPersonGroups are a person's choices of optional groups in a tenant: group id → on.
func (s Store) EnvPersonGroups(ctx context.Context, userID, scope string) (map[string]bool, error) {
	rows, err := s.q.Query(ctx, `SELECT group_id, turned_on FROM env_person_group WHERE user_id = ? AND scope = ?`, userID, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		var on int
		if err := rows.Scan(&id, &on); err != nil {
			return nil, err
		}
		out[id] = on != 0
	}
	return out, rows.Err()
}

// SetEnvPersonGroup keeps a person's choice of a group in a tenant.
func (s Store) SetEnvPersonGroup(ctx context.Context, userID, scope, groupID string, on bool, at time.Time) error {
	_, err := s.q.Exec(ctx, `INSERT INTO env_person_group (user_id, scope, group_id, turned_on, at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, scope, group_id) DO UPDATE SET turned_on = excluded.turned_on, at = excluded.at`,
		userID, scope, groupID, envInt(on), Ms(at))
	return err
}

// EnvPersonGroupsInScope are the choices of every person of a tenant: user id → group id → on.
func (s Store) EnvPersonGroupsInScope(ctx context.Context, scope string) (map[string]map[string]bool, error) {
	rows, err := s.q.Query(ctx, `SELECT user_id, group_id, turned_on FROM env_person_group WHERE scope = ?`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var u, g string
		var on int
		if err := rows.Scan(&u, &g, &on); err != nil {
			return nil, err
		}
		if out[u] == nil {
			out[u] = map[string]bool{}
		}
		out[u][g] = on != 0
	}
	return out, rows.Err()
}

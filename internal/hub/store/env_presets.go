package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"
)

// EnvPreset is a named set of the groups of one layer (docs/hub/environment.md, «Presets»).
type EnvPreset struct {
	ID        string
	OrgID     string
	Layer     string
	ScopeID   string
	Name      string
	Hint      string
	GroupIDs  []string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
	UpdatedBy string
}

const envPresetCols = `id, org_id, layer, scope_id, name, hint, group_ids, position, created_at, updated_at, updated_by`

func scanEnvPreset(row interface{ Scan(...any) error }) (EnvPreset, error) {
	var p EnvPreset
	var ids string
	var created, updated int64
	var by sql.NullString
	if err := row.Scan(&p.ID, &p.OrgID, &p.Layer, &p.ScopeID, &p.Name, &p.Hint, &ids, &p.Position, &created, &updated, &by); err != nil {
		return EnvPreset{}, err
	}
	if err := json.Unmarshal([]byte(ids), &p.GroupIDs); err != nil {
		return EnvPreset{}, err
	}
	p.CreatedAt, p.UpdatedAt, p.UpdatedBy = fromMs(created), fromMs(updated), by.String
	return p, nil
}

func presetIDs(ids []string) (string, error) {
	if ids == nil {
		ids = []string{}
	}
	b, err := json.Marshal(ids)
	return string(b), err
}

// AddEnvPreset keeps a new preset; p.ID is made when empty. False when the layer has a preset of that name.
func (s Store) AddEnvPreset(ctx context.Context, p EnvPreset) (EnvPreset, bool, error) {
	if p.ID == "" {
		p.ID = NewID()
	}
	ids, err := presetIDs(p.GroupIDs)
	if err != nil {
		return EnvPreset{}, false, err
	}
	res, err := s.q.Exec(ctx, `INSERT INTO env_presets (`+envPresetCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (layer, scope_id, name) DO NOTHING`,
		p.ID, p.OrgID, p.Layer, p.ScopeID, p.Name, p.Hint, ids, p.Position, Ms(p.CreatedAt), Ms(p.UpdatedAt), nullStr(p.UpdatedBy))
	if err != nil {
		return EnvPreset{}, false, err
	}
	n, _ := res.RowsAffected()
	return p, n > 0, nil
}

// EnvPresetByID is one preset, or ErrNotFound.
func (s Store) EnvPresetByID(ctx context.Context, id string) (EnvPreset, error) {
	return scanEnvPreset(s.q.QueryRow(ctx, `SELECT `+envPresetCols+` FROM env_presets WHERE id = ?`, id))
}

// EnvPresetsOfOrg are the presets of all the layers of an organization, by layer, scope, position and name.
func (s Store) EnvPresetsOfOrg(ctx context.Context, orgID string) ([]EnvPreset, error) {
	rows, err := s.q.Query(ctx, `SELECT `+envPresetCols+` FROM env_presets WHERE org_id = ? ORDER BY layer, scope_id, position, name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnvPreset
	for rows.Next() {
		p, err := scanEnvPreset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateEnvPreset writes name, hint, group_ids, position, updated_at and updated_by of p.
func (s Store) UpdateEnvPreset(ctx context.Context, p EnvPreset) error {
	ids, err := presetIDs(p.GroupIDs)
	if err != nil {
		return err
	}
	_, err = s.q.Exec(ctx, `UPDATE env_presets SET name = ?, hint = ?, group_ids = ?, position = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
		p.Name, p.Hint, ids, p.Position, Ms(p.UpdatedAt), nullStr(p.UpdatedBy), p.ID)
	return err
}

// DeleteEnvPreset removes a preset.
func (s Store) DeleteEnvPreset(ctx context.Context, id string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM env_presets WHERE id = ?`, id)
	return err
}

// dropGroupFromPresets takes a group's id out of the presets of its layer.
func (s Store) dropGroupFromPresets(ctx context.Context, orgID, groupID string) error {
	all, err := s.EnvPresetsOfOrg(ctx, orgID)
	if err != nil {
		return err
	}
	for _, p := range all {
		i := slices.Index(p.GroupIDs, groupID)
		if i < 0 {
			continue
		}
		p.GroupIDs = slices.Delete(slices.Clone(p.GroupIDs), i, i+1)
		if err := s.UpdateEnvPreset(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

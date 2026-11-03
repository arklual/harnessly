package store

import (
	"context"
	"time"
)

// OrgFeature is an organization's choice for one flag.
type OrgFeature struct {
	OrgKey    string
	Feature   string
	Enabled   bool
	UpdatedAt time.Time
	UpdatedBy string // "" when nobody is known (a seed)
}

// OrgFeatures are the organization's choices, by flag name.
func (s Store) OrgFeatures(ctx context.Context, orgKey string) ([]OrgFeature, error) {
	rows, err := s.q.Query(ctx, `SELECT feature, enabled, updated_at, COALESCE(updated_by, '') FROM org_features WHERE org_key = ? ORDER BY feature`, orgKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgFeature
	for rows.Next() {
		f := OrgFeature{OrgKey: orgKey}
		var on int
		var at int64
		if err := rows.Scan(&f.Feature, &on, &at, &f.UpdatedBy); err != nil {
			return nil, err
		}
		f.Enabled, f.UpdatedAt = on != 0, fromMs(at)
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetOrgFeature keeps the organization's choice for a flag.
func (s Store) SetOrgFeature(ctx context.Context, orgKey, feature string, enabled bool, by string, at time.Time) error {
	_, err := s.q.Exec(ctx, `INSERT INTO org_features (org_key, feature, enabled, updated_at, updated_by) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (org_key, feature) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		orgKey, feature, boolInt(enabled), Ms(at), nullStr(by))
	return err
}

package store

import (
	"context"
	"database/sql"
	"time"
)

// Limit periods and units (docs/hub/limits.md, «Rules»).
const (
	PeriodMonth    = "month"
	PeriodFiveHour = "five_hour"
	PeriodSevenDay = "seven_day"

	UnitMoney     = "money"
	UnitTokens    = "tokens"
	UnitWindowPct = "window_pct"
)

// LimitRule is one rule. UserID, AccountID and ModelPattern are "" for «none»; Amount is dollars for money, tokens for
// tokens, percent of the window for window_pct.
type LimitRule struct {
	ID           string
	OrgKey       string
	ScopeKind    string
	ScopeID      string
	UserID       string
	AccountID    string
	ModelPattern string
	Forbid       bool
	Period       string
	Unit         string
	Amount       float64
	WarnPct      float64
	SetBy        string
	SetAt        time.Time
}

const limitCols = `id, org_key, scope_kind, scope_id, user_id, account_id, model_pattern, forbid, period, unit, amount, warn_pct, set_by, set_at`

func scanLimitRule(row interface{ Scan(...any) error }) (LimitRule, error) {
	var r LimitRule
	var forbid int
	var by sql.NullString
	var at int64
	if err := row.Scan(&r.ID, &r.OrgKey, &r.ScopeKind, &r.ScopeID, &r.UserID, &r.AccountID, &r.ModelPattern, &forbid, &r.Period,
		&r.Unit, &r.Amount, &r.WarnPct, &by, &at); err != nil {
		return LimitRule{}, err
	}
	r.Forbid, r.SetBy, r.SetAt = forbid != 0, by.String, fromMs(at)
	return r, nil
}

// LimitRuleByKey is the rule on the natural key of r, or ErrNotFound.
func (s Store) LimitRuleByKey(ctx context.Context, r LimitRule) (LimitRule, error) {
	return scanLimitRule(s.q.QueryRow(ctx, `SELECT `+limitCols+` FROM limit_rules WHERE scope_kind = ? AND scope_id = ? AND user_id = ?
		AND account_id = ? AND model_pattern = ? AND period = ? AND unit = ?`,
		r.ScopeKind, r.ScopeID, r.UserID, r.AccountID, r.ModelPattern, r.Period, r.Unit))
}

func (s Store) LimitRuleByID(ctx context.Context, id string) (LimitRule, error) {
	return scanLimitRule(s.q.QueryRow(ctx, `SELECT `+limitCols+` FROM limit_rules WHERE id = ?`, id))
}

// PutLimitRule writes the rule on its natural key: a new one gets r.ID (made when empty), an existing one keeps its id.
// It returns the rule as stored.
func (s Store) PutLimitRule(ctx context.Context, r LimitRule) (LimitRule, error) {
	old, err := s.LimitRuleByKey(ctx, r)
	switch {
	case err == nil:
		r.ID = old.ID
		_, err = s.q.Exec(ctx, `UPDATE limit_rules SET org_key = ?, forbid = ?, amount = ?, warn_pct = ?, set_by = ?, set_at = ? WHERE id = ?`,
			r.OrgKey, boolInt(r.Forbid), r.Amount, r.WarnPct, nullStr(r.SetBy), Ms(r.SetAt), r.ID)
	case IsNotFound(err):
		if r.ID == "" {
			r.ID = NewID()
		}
		_, err = s.q.Exec(ctx, `INSERT INTO limit_rules (`+limitCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.OrgKey, r.ScopeKind, r.ScopeID, r.UserID, r.AccountID, r.ModelPattern, boolInt(r.Forbid), r.Period, r.Unit,
			r.Amount, r.WarnPct, nullStr(r.SetBy), Ms(r.SetAt))
	}
	if err != nil {
		return LimitRule{}, err
	}
	return r, nil
}

func (s Store) DeleteLimitRule(ctx context.Context, id string) (bool, error) {
	res, err := s.q.Exec(ctx, `DELETE FROM limit_rules WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// LimitRulesByOrg is every rule of an organization (org key: OrgKey), oldest first.
func (s Store) LimitRulesByOrg(ctx context.Context, orgKey string) ([]LimitRule, error) {
	rows, err := s.q.Query(ctx, `SELECT `+limitCols+` FROM limit_rules WHERE org_key = ? ORDER BY set_at, id`, orgKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LimitRule
	for rows.Next() {
		r, err := scanLimitRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteLimitRulesOn drops the rules of a scope (a team deleted, a tenant moved to another organization).
func (s Store) DeleteLimitRulesOn(ctx context.Context, scopeKind, scopeID string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM limit_rules WHERE scope_kind = ? AND scope_id = ?`, scopeKind, scopeID)
	return err
}

// MachineBudget is the last budget.set a machine took.
type MachineBudget struct {
	MachineID string
	Version   int64
	Hash      string
	SyncedAt  time.Time
}

func (s Store) MachineBudgetOf(ctx context.Context, machineID string) (MachineBudget, error) {
	b := MachineBudget{MachineID: machineID}
	var at int64
	err := s.q.QueryRow(ctx, `SELECT version, hash, synced_at FROM machine_budgets WHERE machine_id = ?`, machineID).Scan(&b.Version, &b.Hash, &at)
	if err != nil {
		return MachineBudget{}, err
	}
	b.SyncedAt = fromMs(at)
	return b, nil
}

func (s Store) SetMachineBudget(ctx context.Context, b MachineBudget) error {
	_, err := s.q.Exec(ctx, `INSERT INTO machine_budgets (machine_id, version, hash, synced_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET version = excluded.version, hash = excluded.hash, synced_at = excluded.synced_at`,
		b.MachineID, b.Version, b.Hash, Ms(b.SyncedAt))
	return err
}

// DeleteLimitRulesOfTenant drops the rules a tenant carried from its organization: the tenant's, its teams' and those of
// the implicit organization it was. A tenant that moves starts clean in the new organization.
func (s Store) DeleteLimitRulesOfTenant(ctx context.Context, spaceID string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM limit_rules WHERE (scope_kind = ? AND scope_id = ?) OR org_key = ?
		OR (scope_kind = ? AND scope_id IN (SELECT id FROM teams WHERE space_id = ?))`,
		ScopeTenant, spaceID, "space:"+spaceID, ScopeTeam, spaceID)
	return err
}

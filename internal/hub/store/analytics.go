package store

import (
	"context"
	"database/sql"
	"strings"
)

// The reads of the analytics screens (docs/hub/analytics.md): a person's hourly sums, their sessions with the money
// of each, and what the share of a subscription is computed from. Times are epoch ms.

// chunk is the most keys one IN (…) carries.
const chunk = 200

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// UserRollupRow is a row of usage_rollup_hour with its organization.
type UserRollupRow struct {
	OrgKey string
	RollupRow
}

// UserRollup reads a person's hourly sums in [from, to); orgKey "" for every organization.
func (s Store) UserRollup(ctx context.Context, userID, orgKey string, from, to int64) ([]UserRollupRow, error) {
	q := `SELECT org_key, hour, user_id, team_id, space_id, account_id, funding, model, calls, unpriced_calls, input, output,
		cache_read, cache_write, list_cost_micros, paid_cost_micros FROM usage_rollup_hour WHERE user_id = ? AND hour >= ? AND hour < ?`
	args := []any{userID, from, to}
	if orgKey != "" {
		q += ` AND org_key = ?`
		args = append(args, orgKey)
	}
	rows, err := s.q.Query(ctx, q+` ORDER BY hour, org_key, account_id, model`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRollupRow
	for rows.Next() {
		var r UserRollupRow
		if err := rows.Scan(&r.OrgKey, &r.Hour, &r.UserID, &r.TeamID, &r.SpaceID, &r.AccountID, &r.Funding, &r.Model, &r.Calls, &r.Unpriced,
			&r.Input, &r.Output, &r.CacheRead, &r.CacheWrite, &r.ListCostMicros, &r.PaidCostMicros); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AccountsByIDs reads the accounts of these ids; a missing id is left out.
func (s Store) AccountsByIDs(ctx context.Context, ids []string) (map[string]Account, error) {
	out := map[string]Account{}
	for len(ids) > 0 {
		n := min(len(ids), chunk)
		part := ids[:n]
		ids = ids[n:]
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		rows, err := s.q.Query(ctx, `SELECT `+accountCols+` FROM accounts WHERE id IN (`+placeholders(len(part))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			a, err := scanAccount(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out[a.ID] = a
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AccountListTotals sums the list price of everyone's calls on each of these accounts in [from, to): what a
// subscription's fee is split by.
func (s Store) AccountListTotals(ctx context.Context, accountIDs []string, from, to int64) (map[string]int64, error) {
	out := map[string]int64{}
	for len(accountIDs) > 0 {
		n := min(len(accountIDs), chunk)
		part := accountIDs[:n]
		accountIDs = accountIDs[n:]
		args := []any{from, to}
		for _, id := range part {
			args = append(args, id)
		}
		rows, err := s.q.Query(ctx, `SELECT account_id, COALESCE(SUM(list_cost_micros), 0) FROM usage_rollup_hour
			WHERE hour >= ? AND hour < ? AND account_id IN (`+placeholders(len(part))+`) GROUP BY account_id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var sum int64
			if err := rows.Scan(&id, &sum); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = sum
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanAgentSession(row interface{ Scan(...any) error }) (AgentSession, error) {
	var x AgentSession
	var machine, user, space, team, account sql.NullString
	var ended sql.NullInt64
	if err := row.Scan(&x.OrgKey, &x.Provider, &x.SessionKey, &x.ParentKey, &machine, &user, &space, &team, &account, &x.Funding, &x.Origin,
		&x.Attribution, &x.StartedAt, &ended, &x.Metrics, &x.Seq, &x.UpdatedAt); err != nil {
		return AgentSession{}, err
	}
	x.MachineID, x.UserID, x.SpaceID, x.TeamID, x.AccountID = machine.String, user.String, space.String, team.String, account.String
	if ended.Valid {
		x.EndedAt = &ended.Int64
	}
	return x, nil
}

func (s Store) sessions(ctx context.Context, q string, args ...any) ([]AgentSession, error) {
	rows, err := s.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentSession
	for rows.Next() {
		x, err := scanAgentSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SessionFilter picks a person's top-level sessions (subagents are inside their parent).
type SessionFilter struct {
	UserID string
	OrgKey string // "" for every organization
	From   int64  // started_at >= From
	To     int64  // started_at < To
	// LiveOnly leaves out eval and automation sessions (KPI count only a person's own chats).
	LiveOnly bool
	// Limit > 0 returns the newest first, at most Limit; 0 returns all, oldest first.
	Limit int
}

// UserSessions reads a person's top-level sessions.
func (s Store) UserSessions(ctx context.Context, f SessionFilter) ([]AgentSession, error) {
	q := `SELECT ` + agentSessionCols + ` FROM agent_sessions WHERE user_id = ? AND parent_key = '' AND started_at >= ? AND started_at < ?`
	args := []any{f.UserID, f.From, f.To}
	if f.OrgKey != "" {
		q += ` AND org_key = ?`
		args = append(args, f.OrgKey)
	}
	if f.LiveOnly {
		q += ` AND origin = 'live'`
	}
	if f.Limit > 0 {
		q += ` ORDER BY started_at DESC, session_key DESC LIMIT ?`
		args = append(args, f.Limit)
	} else {
		q += ` ORDER BY started_at, session_key`
	}
	return s.sessions(ctx, q, args...)
}

// SessionRef names a session.
type SessionRef struct{ OrgKey, Provider, SessionKey string }

// SessionCallSum is the calls of a top-level session and its subagents on one model.
type SessionCallSum struct {
	SessionRef
	Model          string
	Calls          int64
	ListCostMicros int64
}

// SessionSums sums the calls of these top-level sessions, their subagents' calls included, by model, and counts the
// subagents of each.
func (s Store) SessionSums(ctx context.Context, refs []SessionRef) ([]SessionCallSum, map[SessionRef]int, error) {
	byOrg := map[[2]string][]string{}
	for _, r := range refs {
		k := [2]string{r.OrgKey, r.Provider}
		byOrg[k] = append(byOrg[k], r.SessionKey)
	}
	var sums []SessionCallSum
	subagents := map[SessionRef]int{}
	for k, keys := range byOrg {
		orgKey, provider := k[0], k[1]
		for len(keys) > 0 {
			n := min(len(keys), chunk)
			part := keys[:n]
			keys = keys[n:]
			args := []any{orgKey, provider}
			for _, key := range part {
				args = append(args, key)
			}
			// The parent of each session: itself for a top-level one, its parent for a subagent.
			owner := map[string]string{}
			for _, key := range part {
				owner[key] = key
			}
			rows, err := s.q.Query(ctx, `SELECT session_key, parent_key FROM agent_sessions WHERE org_key = ? AND provider = ? AND parent_key IN (`+
				placeholders(len(part))+`)`, args...)
			if err != nil {
				return nil, nil, err
			}
			for rows.Next() {
				var key, parent string
				if err := rows.Scan(&key, &parent); err != nil {
					rows.Close()
					return nil, nil, err
				}
				owner[key] = parent
				subagents[SessionRef{orgKey, provider, parent}]++
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, nil, err
			}
			all := make([]string, 0, len(owner))
			for key := range owner {
				all = append(all, key)
			}
			acc := map[[2]string]*SessionCallSum{}
			for len(all) > 0 {
				m := min(len(all), chunk)
				sub := all[:m]
				all = all[m:]
				cargs := []any{orgKey, provider}
				for _, key := range sub {
					cargs = append(cargs, key)
				}
				rows, err := s.q.Query(ctx, `SELECT session_key, model, COUNT(*), COALESCE(SUM(list_cost_micros), 0) FROM usage_calls
					WHERE org_key = ? AND provider = ? AND session_key IN (`+placeholders(len(sub))+`) GROUP BY session_key, model`, cargs...)
				if err != nil {
					return nil, nil, err
				}
				for rows.Next() {
					var key, model string
					var calls, list int64
					if err := rows.Scan(&key, &model, &calls, &list); err != nil {
						rows.Close()
						return nil, nil, err
					}
					p := owner[key]
					a := acc[[2]string{p, model}]
					if a == nil {
						a = &SessionCallSum{SessionRef: SessionRef{orgKey, provider, p}, Model: model}
						acc[[2]string{p, model}] = a
					}
					a.Calls += calls
					a.ListCostMicros += list
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return nil, nil, err
				}
			}
			for _, a := range acc {
				sums = append(sums, *a)
			}
		}
	}
	return sums, subagents, nil
}

// MachineNames reads the names of these machines.
func (s Store) MachineNames(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for len(ids) > 0 {
		n := min(len(ids), chunk)
		part := ids[:n]
		ids = ids[n:]
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		rows, err := s.q.Query(ctx, `SELECT id, name FROM machines WHERE id IN (`+placeholders(len(part))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = name
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UserAccountedUntil is the last time the hub took a batch from a machine the person's sessions ran on: «на сервере
// учтено до …». Nil when none has been read.
func (s Store) UserAccountedUntil(ctx context.Context, userID, orgKey, stream string) (*int64, error) {
	q := `SELECT MAX(c.acked_at) FROM ingest_cursors c WHERE c.stream = ? AND c.machine_id IN
		(SELECT DISTINCT machine_id FROM agent_sessions WHERE user_id = ? AND machine_id IS NOT NULL`
	args := []any{stream, userID}
	if orgKey != "" {
		q += ` AND org_key = ?`
		args = append(args, orgKey)
	}
	var v sql.NullInt64
	if err := s.q.QueryRow(ctx, q+`)`, args...).Scan(&v); err != nil {
		return nil, err
	}
	if !v.Valid {
		return nil, nil
	}
	return &v.Int64, nil
}

// ScopeFilter picks the top-level sessions of an organization, or of one of its tenants or teams.
type ScopeFilter struct {
	OrgKey  string
	SpaceID string // "" for every tenant
	TeamID  string // "" for every team
	From    int64  // started_at >= From
	To      int64  // started_at < To
	// LiveOnly leaves out eval and automation sessions.
	LiveOnly bool
}

func (f ScopeFilter) where() (string, []any) {
	q := ` WHERE org_key = ? AND parent_key = ''`
	args := []any{f.OrgKey}
	if f.SpaceID != "" {
		q += ` AND space_id = ?`
		args = append(args, f.SpaceID)
	}
	if f.TeamID != "" {
		q += ` AND team_id = ?`
		args = append(args, f.TeamID)
	}
	return q, args
}

// ScopeSessions reads the top-level sessions of a scope, oldest first.
func (s Store) ScopeSessions(ctx context.Context, f ScopeFilter) ([]AgentSession, error) {
	w, args := f.where()
	q := `SELECT ` + agentSessionCols + ` FROM agent_sessions` + w + ` AND started_at >= ? AND started_at < ?`
	args = append(args, f.From, f.To)
	if f.LiveOnly {
		q += ` AND origin = 'live'`
	}
	return s.sessions(ctx, q+` ORDER BY started_at, session_key`, args...)
}

// ScopeAccountedUntil is the last time the hub took a batch from a machine the scope's sessions ran on. Nil when none
// has been read.
func (s Store) ScopeAccountedUntil(ctx context.Context, f ScopeFilter, stream string) (*int64, error) {
	w, args := f.where()
	var v sql.NullInt64
	if err := s.q.QueryRow(ctx, `SELECT MAX(c.acked_at) FROM ingest_cursors c WHERE c.stream = ? AND c.machine_id IN
		(SELECT DISTINCT machine_id FROM agent_sessions`+w+` AND machine_id IS NOT NULL)`, append([]any{stream}, args...)...).Scan(&v); err != nil {
		return nil, err
	}
	if !v.Valid {
		return nil, nil
	}
	return &v.Int64, nil
}

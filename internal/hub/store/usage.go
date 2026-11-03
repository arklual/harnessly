package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Funding says who pays for an account.
const (
	FundingOrg      = "org"
	FundingPersonal = "personal"
)

// OrgKey is the key spend is kept under: the organization's id, or space:<id> for a space of the implicit organization.
func OrgKey(sp Space) string {
	if sp.OrgID != "" {
		return sp.OrgID
	}
	return "space:" + sp.ID
}

// Price is a row of the price list: USD (or credits) per million tokens, in effect from EffectiveFrom (epoch ms).
type Price struct {
	Provider         string
	ModelPattern     string
	EffectiveFrom    int64
	Input            float64
	CacheWrite5m     float64
	CacheWrite1h     float64
	CacheRead        float64
	Output           float64
	LongCtxThreshold *int64
	LongCtxFactor    *float64
	Unit             string
	Source           string
}

const priceCols = `provider, model_pattern, effective_from, input, cache_write_5m, cache_write_1h, cache_read, output, long_ctx_threshold, long_ctx_factor, unit, source`

// AddPrice inserts a price row unless the same (provider, pattern, effective_from) is there: a seed never overwrites
// what an administrator corrected. True when it was added.
func (s Store) AddPrice(ctx context.Context, p Price) (bool, error) {
	if p.Unit == "" {
		p.Unit = "usd"
	}
	var thr, fac any
	if p.LongCtxThreshold != nil {
		thr = *p.LongCtxThreshold
	}
	if p.LongCtxFactor != nil {
		fac = *p.LongCtxFactor
	}
	res, err := s.q.Exec(ctx, `INSERT INTO prices (`+priceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider, model_pattern, effective_from) DO NOTHING`,
		p.Provider, p.ModelPattern, p.EffectiveFrom, p.Input, p.CacheWrite5m, p.CacheWrite1h, p.CacheRead, p.Output, thr, fac, p.Unit, p.Source)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Prices lists the whole price list, by provider, pattern and time.
func (s Store) Prices(ctx context.Context) ([]Price, error) {
	rows, err := s.q.Query(ctx, `SELECT `+priceCols+` FROM prices ORDER BY provider, model_pattern, effective_from`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Price
	for rows.Next() {
		var p Price
		var thr sql.NullInt64
		var fac sql.NullFloat64
		if err := rows.Scan(&p.Provider, &p.ModelPattern, &p.EffectiveFrom, &p.Input, &p.CacheWrite5m, &p.CacheWrite1h, &p.CacheRead, &p.Output, &thr, &fac, &p.Unit, &p.Source); err != nil {
			return nil, err
		}
		if thr.Valid {
			p.LongCtxThreshold = &thr.Int64
		}
		if fac.Valid {
			p.LongCtxFactor = &fac.Float64
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Account is an account a model is paid through.
type Account struct {
	ID               string
	OrgKey           string
	ScopeKind        string
	ScopeID          string
	Provider         string
	Kind             string // subscription, api_key, prepaid
	Plan             string
	AccountRef       string
	BaseURLHost      string
	Label            string
	Funding          string
	OwnerUserID      string
	MonthlyFeeMicros int64
	// Discovered: the hub made it on seeing an unknown account_ref on a machine (personal until someone registers it).
	Discovered bool
	CreatedBy  string
	CreatedAt  time.Time
}

const accountCols = `id, org_key, scope_kind, scope_id, provider, kind, plan, account_ref, base_url_host, label, funding, owner_user_id, monthly_fee_micros, discovered, created_by, created_at`

func scanAccount(row interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var owner, by sql.NullString
	var disc int
	var created int64
	if err := row.Scan(&a.ID, &a.OrgKey, &a.ScopeKind, &a.ScopeID, &a.Provider, &a.Kind, &a.Plan, &a.AccountRef, &a.BaseURLHost, &a.Label,
		&a.Funding, &owner, &a.MonthlyFeeMicros, &disc, &by, &created); err != nil {
		return Account{}, err
	}
	a.OwnerUserID, a.CreatedBy, a.Discovered, a.CreatedAt = owner.String, by.String, disc != 0, fromMs(created)
	return a, nil
}

// CreateAccount inserts an account; the same (org, provider, ref) twice is a unique violation.
func (s Store) CreateAccount(ctx context.Context, a *Account) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Funding == "" {
		a.Funding = FundingPersonal
	}
	_, err := s.q.Exec(ctx, `INSERT INTO accounts (`+accountCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.OrgKey, a.ScopeKind, a.ScopeID, a.Provider, a.Kind, a.Plan, a.AccountRef, a.BaseURLHost, a.Label, a.Funding,
		nullStr(a.OwnerUserID), a.MonthlyFeeMicros, boolInt(a.Discovered), nullStr(a.CreatedBy), Ms(a.CreatedAt))
	return err
}

func (s Store) AccountByID(ctx context.Context, id string) (Account, error) {
	return scanAccount(s.q.QueryRow(ctx, `SELECT `+accountCols+` FROM accounts WHERE id = ?`, id))
}

func (s Store) AccountByRef(ctx context.Context, orgKey, provider, ref string) (Account, error) {
	return scanAccount(s.q.QueryRow(ctx, `SELECT `+accountCols+` FROM accounts WHERE org_key = ? AND provider = ? AND account_ref = ?`, orgKey, provider, ref))
}

// AccountsByOrg lists an organization's accounts, oldest first.
func (s Store) AccountsByOrg(ctx context.Context, orgKey string) ([]Account, error) {
	rows, err := s.q.Query(ctx, `SELECT `+accountCols+` FROM accounts WHERE org_key = ? ORDER BY created_at, id`, orgKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateAccount changes what a person may change of an account: its scope, label, plan, who pays, the monthly fee.
func (s Store) UpdateAccount(ctx context.Context, a Account) error {
	_, err := s.q.Exec(ctx, `UPDATE accounts SET scope_kind = ?, scope_id = ?, label = ?, plan = ?, kind = ?, funding = ?, owner_user_id = ?,
		monthly_fee_micros = ?, discovered = ? WHERE id = ?`,
		a.ScopeKind, a.ScopeID, a.Label, a.Plan, a.Kind, a.Funding, nullStr(a.OwnerUserID), a.MonthlyFeeMicros, boolInt(a.Discovered), a.ID)
	return err
}

// AccountGrant gives an account to a scope below its own.
type AccountGrant struct {
	AccountID string
	ScopeKind string
	ScopeID   string
	GrantedBy string
	GrantedAt time.Time
}

func (s Store) GrantAccount(ctx context.Context, g AccountGrant) (bool, error) {
	if g.GrantedAt.IsZero() {
		g.GrantedAt = time.Now().UTC()
	}
	res, err := s.q.Exec(ctx, `INSERT INTO account_grants (account_id, scope_kind, scope_id, granted_by, granted_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (account_id, scope_kind, scope_id) DO NOTHING`, g.AccountID, g.ScopeKind, g.ScopeID, nullStr(g.GrantedBy), Ms(g.GrantedAt))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s Store) RevokeAccount(ctx context.Context, accountID, scopeKind, scopeID string) (bool, error) {
	res, err := s.q.Exec(ctx, `DELETE FROM account_grants WHERE account_id = ? AND scope_kind = ? AND scope_id = ?`, accountID, scopeKind, scopeID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s Store) AccountGrants(ctx context.Context, accountID string) ([]AccountGrant, error) {
	rows, err := s.q.Query(ctx, `SELECT account_id, scope_kind, scope_id, granted_by, granted_at FROM account_grants WHERE account_id = ? ORDER BY granted_at, scope_kind, scope_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountGrant
	for rows.Next() {
		var g AccountGrant
		var by sql.NullString
		var at int64
		if err := rows.Scan(&g.AccountID, &g.ScopeKind, &g.ScopeID, &by, &at); err != nil {
			return nil, err
		}
		g.GrantedBy, g.GrantedAt = by.String, fromMs(at)
		out = append(out, g)
	}
	return out, rows.Err()
}

// AccountWindow is the freshest measurement of one window of an account.
type AccountWindow struct {
	AccountID  string
	Scope      string
	Name       string
	LengthSec  int64
	Unit       string
	UsedPct    *float64
	Used       *float64
	Limit      *float64
	ResetsAt   *int64
	MeasuredAt int64
	Source     string
	Confidence string
	MachineID  string
}

func nullF(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullI(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// PutWindow records a measurement. Only a newer measurement with a value replaces the row: a measurement with nothing
// in it (after a reset, before the provider reports again) leaves the window as it was; the UI then sees
// resets_at in the past and says «ждём замер». True when the row changed.
func (s Store) PutWindow(ctx context.Context, w AccountWindow) (bool, error) {
	if w.UsedPct == nil && w.Used == nil {
		return false, nil
	}
	res, err := s.q.Exec(ctx, `INSERT INTO account_windows (account_id, scope, name, length_sec, unit, used_pct, used, lim, resets_at, measured_at, source, confidence, machine_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, scope, name) DO UPDATE SET length_sec = excluded.length_sec, unit = excluded.unit, used_pct = excluded.used_pct,
		used = excluded.used, lim = excluded.lim, resets_at = excluded.resets_at, measured_at = excluded.measured_at, source = excluded.source,
		confidence = excluded.confidence, machine_id = excluded.machine_id
		WHERE excluded.measured_at > account_windows.measured_at`,
		w.AccountID, w.Scope, w.Name, w.LengthSec, w.Unit, nullF(w.UsedPct), nullF(w.Used), nullF(w.Limit), nullI(w.ResetsAt), w.MeasuredAt, w.Source, w.Confidence, nullStr(w.MachineID))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s Store) Windows(ctx context.Context, accountID string) ([]AccountWindow, error) {
	rows, err := s.q.Query(ctx, `SELECT account_id, scope, name, length_sec, unit, used_pct, used, lim, resets_at, measured_at, source, confidence, machine_id
		FROM account_windows WHERE account_id = ? ORDER BY scope, length_sec, name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountWindow
	for rows.Next() {
		var w AccountWindow
		var pct, used, lim sql.NullFloat64
		var resets sql.NullInt64
		var machine sql.NullString
		if err := rows.Scan(&w.AccountID, &w.Scope, &w.Name, &w.LengthSec, &w.Unit, &pct, &used, &lim, &resets, &w.MeasuredAt, &w.Source, &w.Confidence, &machine); err != nil {
			return nil, err
		}
		if pct.Valid {
			w.UsedPct = &pct.Float64
		}
		if used.Valid {
			w.Used = &used.Float64
		}
		if lim.Valid {
			w.Limit = &lim.Float64
		}
		if resets.Valid {
			w.ResetsAt = &resets.Int64
		}
		w.MachineID = machine.String
		out = append(out, w)
	}
	return out, rows.Err()
}

// AccountBalance is the freshest balance of an account in one currency; Error is the last failed poll's, kept beside
// the last good value instead of replacing it.
type AccountBalance struct {
	AccountID     string
	Currency      string
	Available     *float64
	SpendCap      *float64
	SpentInPeriod *float64
	MeasuredAt    int64
	Source        string
	Error         string
}

// PutBalance records a measurement; a newer one replaces the row. A failed poll (Error set) only writes its error.
func (s Store) PutBalance(ctx context.Context, b AccountBalance) error {
	if b.Error != "" {
		_, err := s.q.Exec(ctx, `UPDATE account_balances SET error = ? WHERE account_id = ? AND currency = ?`, b.Error, b.AccountID, b.Currency)
		return err
	}
	_, err := s.q.Exec(ctx, `INSERT INTO account_balances (account_id, currency, available, spend_cap, spent_in_period, measured_at, source, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, '')
		ON CONFLICT (account_id, currency) DO UPDATE SET available = excluded.available, spend_cap = excluded.spend_cap,
		spent_in_period = excluded.spent_in_period, measured_at = excluded.measured_at, source = excluded.source, error = ''
		WHERE excluded.measured_at > account_balances.measured_at`,
		b.AccountID, b.Currency, nullF(b.Available), nullF(b.SpendCap), nullF(b.SpentInPeriod), b.MeasuredAt, b.Source)
	return err
}

func (s Store) Balances(ctx context.Context, accountID string) ([]AccountBalance, error) {
	rows, err := s.q.Query(ctx, `SELECT account_id, currency, available, spend_cap, spent_in_period, measured_at, source, error
		FROM account_balances WHERE account_id = ? ORDER BY currency`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountBalance
	for rows.Next() {
		var b AccountBalance
		var av, cp, sp sql.NullFloat64
		if err := rows.Scan(&b.AccountID, &b.Currency, &av, &cp, &sp, &b.MeasuredAt, &b.Source, &b.Error); err != nil {
			return nil, err
		}
		if av.Valid {
			b.Available = &av.Float64
		}
		if cp.Valid {
			b.SpendCap = &cp.Float64
		}
		if sp.Valid {
			b.SpentInPeriod = &sp.Float64
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AgentSession is a session as the hub keeps it.
type AgentSession struct {
	OrgKey      string
	Provider    string
	SessionKey  string
	ParentKey   string
	MachineID   string
	UserID      string
	SpaceID     string
	TeamID      string
	AccountID   string
	Funding     string
	Origin      string
	Attribution string
	StartedAt   int64
	EndedAt     *int64
	Metrics     string // JSON object
	Seq         int64
	UpdatedAt   int64
}

const agentSessionCols = `org_key, provider, session_key, parent_key, machine_id, user_id, space_id, team_id, account_id, funding, origin, attribution, started_at, ended_at, metrics, seq, updated_at`

// PutSession inserts a session or replaces it with a record of a later seq from the same machine, or of any machine when
// the stored one is older by update time. A record that names no account keeps the account the session has (an admin
// gave it one, or an earlier record named it). True when the row changed.
func (s Store) PutSession(ctx context.Context, x AgentSession) (bool, error) {
	if x.Metrics == "" {
		x.Metrics = "{}"
	}
	res, err := s.q.Exec(ctx, `INSERT INTO agent_sessions (`+agentSessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (org_key, provider, session_key) DO UPDATE SET parent_key = excluded.parent_key, machine_id = excluded.machine_id,
		user_id = excluded.user_id, space_id = excluded.space_id, team_id = excluded.team_id,
		account_id = COALESCE(excluded.account_id, agent_sessions.account_id),
		funding = CASE WHEN excluded.account_id IS NULL AND agent_sessions.account_id IS NOT NULL THEN agent_sessions.funding ELSE excluded.funding END,
		origin = excluded.origin, attribution = excluded.attribution, started_at = excluded.started_at,
		ended_at = excluded.ended_at, metrics = excluded.metrics, seq = excluded.seq, updated_at = excluded.updated_at
		WHERE (COALESCE(excluded.machine_id, '') = COALESCE(agent_sessions.machine_id, '') AND excluded.seq > agent_sessions.seq)
		   OR (COALESCE(excluded.machine_id, '') <> COALESCE(agent_sessions.machine_id, '') AND excluded.updated_at > agent_sessions.updated_at)`,
		x.OrgKey, x.Provider, x.SessionKey, x.ParentKey, nullStr(x.MachineID), nullStr(x.UserID), nullStr(x.SpaceID), nullStr(x.TeamID),
		nullStr(x.AccountID), x.Funding, x.Origin, x.Attribution, x.StartedAt, nullI(x.EndedAt), x.Metrics, x.Seq, x.UpdatedAt)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s Store) SessionByKey(ctx context.Context, orgKey, provider, key string) (AgentSession, error) {
	var x AgentSession
	var machine, user, space, team, account sql.NullString
	var ended sql.NullInt64
	err := s.q.QueryRow(ctx, `SELECT `+agentSessionCols+` FROM agent_sessions WHERE org_key = ? AND provider = ? AND session_key = ?`, orgKey, provider, key).
		Scan(&x.OrgKey, &x.Provider, &x.SessionKey, &x.ParentKey, &machine, &user, &space, &team, &account, &x.Funding, &x.Origin, &x.Attribution,
			&x.StartedAt, &ended, &x.Metrics, &x.Seq, &x.UpdatedAt)
	if err != nil {
		return AgentSession{}, err
	}
	x.MachineID, x.UserID, x.SpaceID, x.TeamID, x.AccountID = machine.String, user.String, space.String, team.String, account.String
	if ended.Valid {
		x.EndedAt = &ended.Int64
	}
	return x, nil
}

// UsageCall is one model call. ListCostMicros is nil when the model has no price.
type UsageCall struct {
	OrgKey         string
	Provider       string
	MsgKey         string
	SessionKey     string
	MachineID      string
	Ts             int64
	Model          string
	PriceProvider  string
	BaseURLHost    string
	Input          int64
	Output         int64
	CacheRead      int64
	CacheWrite5m   int64
	CacheWrite1h   int64
	ListCostMicros *int64
	PriceVer       *int64
	TsShifted      bool
	ReceivedAt     int64
}

const callCols = `org_key, provider, msg_key, session_key, machine_id, ts, model, price_provider, base_url_host, input, output, cache_read, cache_write_5m, cache_write_1h, list_cost_micros, price_ver, ts_shifted, received_at`

// AddCall inserts a call unless the organization has it already (the same message seen again: a resume, a fork, a
// moved chat, a repeated batch). True when it was new.
func (s Store) AddCall(ctx context.Context, c UsageCall) (bool, error) {
	res, err := s.q.Exec(ctx, `INSERT INTO usage_calls (`+callCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (org_key, provider, msg_key) DO NOTHING`,
		c.OrgKey, c.Provider, c.MsgKey, c.SessionKey, nullStr(c.MachineID), c.Ts, c.Model, c.PriceProvider, c.BaseURLHost,
		c.Input, c.Output, c.CacheRead, c.CacheWrite5m, c.CacheWrite1h, nullI(c.ListCostMicros), nullI(c.PriceVer), boolInt(c.TsShifted), c.ReceivedAt)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UsageTotals is a sum of calls.
type UsageTotals struct {
	Calls, Unpriced                      int64
	Input, Output, CacheRead, CacheWrite int64
	ListCostMicros                       int64
}

// SumCalls sums an organization's calls in [from, to) (epoch ms); sessionKey "" for all.
func (s Store) SumCalls(ctx context.Context, orgKey string, from, to int64, sessionKey string) (UsageTotals, error) {
	var t UsageTotals
	q := `SELECT COUNT(*), COALESCE(SUM(CASE WHEN list_cost_micros IS NULL THEN 1 ELSE 0 END), 0), COALESCE(SUM(input), 0), COALESCE(SUM(output), 0),
		COALESCE(SUM(cache_read), 0), COALESCE(SUM(cache_write_5m + cache_write_1h), 0), COALESCE(SUM(list_cost_micros), 0)
		FROM usage_calls WHERE org_key = ? AND ts >= ? AND ts < ?`
	args := []any{orgKey, from, to}
	if sessionKey != "" {
		q += ` AND session_key = ?`
		args = append(args, sessionKey)
	}
	err := s.q.QueryRow(ctx, q, args...).Scan(&t.Calls, &t.Unpriced, &t.Input, &t.Output, &t.CacheRead, &t.CacheWrite, &t.ListCostMicros)
	return t, err
}

// Cursor is where the hub stands in a machine's outbox.
type Cursor struct {
	MachineID string
	Stream    string
	Epoch     string
	Seq       int64
	AckedAt   *int64
}

// CursorOf reads a cursor; a machine never read has the zero cursor.
func (s Store) CursorOf(ctx context.Context, machineID, stream string) (Cursor, error) {
	c := Cursor{MachineID: machineID, Stream: stream}
	var acked sql.NullInt64
	err := s.q.QueryRow(ctx, `SELECT epoch, seq, acked_at FROM ingest_cursors WHERE machine_id = ? AND stream = ?`, machineID, stream).Scan(&c.Epoch, &c.Seq, &acked)
	if IsNotFound(err) {
		return c, nil
	}
	if acked.Valid {
		c.AckedAt = &acked.Int64
	}
	return c, err
}

func (s Store) SetCursor(ctx context.Context, c Cursor) error {
	_, err := s.q.Exec(ctx, `INSERT INTO ingest_cursors (machine_id, stream, epoch, seq, acked_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, stream) DO UPDATE SET epoch = excluded.epoch, seq = excluded.seq, acked_at = excluded.acked_at`,
		c.MachineID, c.Stream, c.Epoch, c.Seq, nullI(c.AckedAt))
	return err
}

// EnsureAccount returns the account of (org, provider, ref), inserting a when there is none. It never fails on a
// concurrent insert of the same account (two machines on one subscription), so it is safe inside a transaction.
func (s Store) EnsureAccount(ctx context.Context, a Account) (Account, error) {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Funding == "" {
		a.Funding = FundingPersonal
	}
	if _, err := s.q.Exec(ctx, `INSERT INTO accounts (`+accountCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (org_key, provider, account_ref) DO NOTHING`,
		a.ID, a.OrgKey, a.ScopeKind, a.ScopeID, a.Provider, a.Kind, a.Plan, a.AccountRef, a.BaseURLHost, a.Label, a.Funding,
		nullStr(a.OwnerUserID), a.MonthlyFeeMicros, boolInt(a.Discovered), nullStr(a.CreatedBy), Ms(a.CreatedAt)); err != nil {
		return Account{}, err
	}
	return s.AccountByRef(ctx, a.OrgKey, a.Provider, a.AccountRef)
}

// ResetSessionSeqs forgets the outbox seqs of a machine's sessions: its outbox was made anew and counts from 1 again,
// so the next record of each session must win over the one kept from the old epoch.
func (s Store) ResetSessionSeqs(ctx context.Context, machineID string) error {
	_, err := s.q.Exec(ctx, `UPDATE agent_sessions SET seq = 0 WHERE machine_id = ?`, machineID)
	return err
}

// HourMs is the start of the UTC hour ts falls in, the key of usage_rollup_hour.
func HourMs(ts int64) int64 { return ts - ((ts%3600000)+3600000)%3600000 }

// CallHours are the hours (HourMs) in which the calls of these sessions, or of their subagents, fell.
func (s Store) CallHours(ctx context.Context, orgKey string, sessionKeys []string) ([]int64, error) {
	var out []int64
	for len(sessionKeys) > 0 {
		n := min(len(sessionKeys), 200)
		chunk := sessionKeys[:n]
		sessionKeys = sessionKeys[n:]
		in := strings.Repeat("?, ", n-1) + "?"
		args := []any{orgKey}
		for _, k := range chunk {
			args = append(args, k)
		}
		args = append(args, orgKey)
		for _, k := range chunk {
			args = append(args, k)
		}
		rows, err := s.q.Query(ctx, `SELECT DISTINCT ts - ts % 3600000 FROM usage_calls WHERE org_key = ? AND (session_key IN (`+in+`)
			OR session_key IN (SELECT session_key FROM agent_sessions WHERE org_key = ? AND parent_key IN (`+in+`)))`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h int64
			if err := rows.Scan(&h); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, h)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// rollupSelect sums an organization's calls of one hour by every cut. A subagent's call is money of its parent session:
// the parent's person, team, space and account. paid is the list price on a key; on a subscription it is the monthly
// fee, split when the screens read (analytics.md), so 0 here.
const rollupSelect = `SELECT org_key, CAST(? AS BIGINT), user_id, team_id, space_id, account_id, funding, model, COUNT(*),
	SUM(CASE WHEN list_cost_micros IS NULL THEN 1 ELSE 0 END), SUM(input), SUM(output), SUM(cache_read), SUM(cache_write_5m + cache_write_1h),
	COALESCE(SUM(list_cost_micros), 0), COALESCE(SUM(CASE WHEN kind IN ('api_key', 'prepaid') THEN list_cost_micros ELSE 0 END), 0)
	FROM (SELECT c.org_key, c.model, c.input, c.output, c.cache_read, c.cache_write_5m, c.cache_write_1h, c.list_cost_micros,
		COALESCE(CASE WHEN p.session_key IS NULL THEN s.user_id ELSE p.user_id END, '') AS user_id,
		COALESCE(CASE WHEN p.session_key IS NULL THEN s.team_id ELSE p.team_id END, '') AS team_id,
		COALESCE(CASE WHEN p.session_key IS NULL THEN s.space_id ELSE p.space_id END, '') AS space_id,
		COALESCE(CASE WHEN p.session_key IS NULL THEN s.account_id ELSE p.account_id END, '') AS account_id,
		COALESCE(CASE WHEN p.session_key IS NULL THEN s.funding ELSE p.funding END, '') AS funding,
		COALESCE(a.kind, '') AS kind
		FROM usage_calls c
		LEFT JOIN agent_sessions s ON s.org_key = c.org_key AND s.provider = c.provider AND s.session_key = c.session_key
		LEFT JOIN agent_sessions p ON s.parent_key <> '' AND p.org_key = s.org_key AND p.provider = s.provider AND p.session_key = s.parent_key
		LEFT JOIN accounts a ON a.id = CASE WHEN p.session_key IS NULL THEN s.account_id ELSE p.account_id END
		WHERE c.org_key = ? AND c.ts >= ? AND c.ts < ?) x
	GROUP BY org_key, user_id, team_id, space_id, account_id, funding, model`

// Rollup sums the organization's calls of these hours (HourMs) anew. It is a pure function of usage_calls and
// agent_sessions, so it is right whatever came first: a call before its session, a session that changed person.
func (s Store) Rollup(ctx context.Context, orgKey string, hours []int64) error {
	seen := map[int64]bool{}
	for _, h := range hours {
		if seen[h] {
			continue
		}
		seen[h] = true
		if _, err := s.q.Exec(ctx, `DELETE FROM usage_rollup_hour WHERE org_key = ? AND hour = ?`, orgKey, h); err != nil {
			return err
		}
		if _, err := s.q.Exec(ctx, `INSERT INTO usage_rollup_hour (org_key, hour, user_id, team_id, space_id, account_id, funding, model, calls,
			unpriced_calls, input, output, cache_read, cache_write, list_cost_micros, paid_cost_micros) `+rollupSelect, h, orgKey, h, h+3600000); err != nil {
			return err
		}
	}
	return nil
}

// RollupRow is one row of usage_rollup_hour.
type RollupRow struct {
	Hour                                                  int64
	UserID, TeamID, SpaceID, AccountID, Funding, Model    string
	Calls, Unpriced, Input, Output, CacheRead, CacheWrite int64
	ListCostMicros, PaidCostMicros                        int64
}

// RollupRows reads an organization's hourly sums in [from, to) (epoch ms).
func (s Store) RollupRows(ctx context.Context, orgKey string, from, to int64) ([]RollupRow, error) {
	rows, err := s.q.Query(ctx, `SELECT hour, user_id, team_id, space_id, account_id, funding, model, calls, unpriced_calls, input, output,
		cache_read, cache_write, list_cost_micros, paid_cost_micros FROM usage_rollup_hour WHERE org_key = ? AND hour >= ? AND hour < ?
		ORDER BY hour, user_id, team_id, space_id, account_id, funding, model`, orgKey, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RollupRow
	for rows.Next() {
		var r RollupRow
		if err := rows.Scan(&r.Hour, &r.UserID, &r.TeamID, &r.SpaceID, &r.AccountID, &r.Funding, &r.Model, &r.Calls, &r.Unpriced, &r.Input,
			&r.Output, &r.CacheRead, &r.CacheWrite, &r.ListCostMicros, &r.PaidCostMicros); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SessionsWithoutAccount are the keys of the person's sessions of a CLI in the organization that have no account.
func (s Store) SessionsWithoutAccount(ctx context.Context, orgKey, userID, provider string) ([]string, error) {
	return s.sessionKeys(ctx, `SELECT session_key FROM agent_sessions WHERE org_key = ? AND user_id = ? AND provider = ? AND account_id IS NULL`,
		orgKey, userID, provider)
}

// SetAccountFunding gives every session of the account its funding and returns the keys of those that changed.
func (s Store) SetAccountFunding(ctx context.Context, orgKey, accountID, funding string) ([]string, error) {
	keys, err := s.sessionKeys(ctx, `SELECT session_key FROM agent_sessions WHERE org_key = ? AND account_id = ? AND funding <> ?`, orgKey, accountID, funding)
	if err != nil || len(keys) == 0 {
		return nil, err
	}
	_, err = s.q.Exec(ctx, `UPDATE agent_sessions SET funding = ? WHERE org_key = ? AND account_id = ?`, funding, orgKey, accountID)
	return keys, err
}

func (s Store) sessionKeys(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SetSessionsAccount gives the sessions (keys of one CLI in the organization) the account and its funding.
func (s Store) SetSessionsAccount(ctx context.Context, orgKey, provider string, keys []string, accountID, funding string) error {
	for len(keys) > 0 {
		n := min(len(keys), 200)
		args := []any{accountID, funding, orgKey, provider}
		for _, k := range keys[:n] {
			args = append(args, k)
		}
		keys = keys[n:]
		if _, err := s.q.Exec(ctx, `UPDATE agent_sessions SET account_id = ?, funding = ? WHERE org_key = ? AND provider = ? AND session_key IN (`+
			strings.Repeat("?, ", n-1)+`?)`, args...); err != nil {
			return err
		}
	}
	return nil
}

// UnpricedCalls are up to limit calls without a price, after the key (org, provider, msg) given, in key order; the
// token columns and what prices them are filled.
func (s Store) UnpricedCalls(ctx context.Context, after [3]string, limit int) ([]UsageCall, error) {
	rows, err := s.q.Query(ctx, `SELECT org_key, provider, msg_key, ts, model, price_provider, input, output, cache_read, cache_write_5m, cache_write_1h
		FROM usage_calls WHERE list_cost_micros IS NULL AND (org_key, provider, msg_key) > (?, ?, ?)
		ORDER BY org_key, provider, msg_key LIMIT ?`, after[0], after[1], after[2], limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageCall
	for rows.Next() {
		var c UsageCall
		if err := rows.Scan(&c.OrgKey, &c.Provider, &c.MsgKey, &c.Ts, &c.Model, &c.PriceProvider, &c.Input, &c.Output, &c.CacheRead,
			&c.CacheWrite5m, &c.CacheWrite1h); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PriceCall gives a call without a price its price; false when it had one already.
func (s Store) PriceCall(ctx context.Context, orgKey, provider, msgKey string, micros, ver int64) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE usage_calls SET list_cost_micros = ?, price_ver = ?
		WHERE org_key = ? AND provider = ? AND msg_key = ? AND list_cost_micros IS NULL`, micros, ver, orgKey, provider, msgKey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

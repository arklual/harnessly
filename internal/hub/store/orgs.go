package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Scope kinds and roles of the organization tree (docs/hub/orgs.md): an organization holds tenants (spaces), a tenant
// holds teams.
const (
	ScopeOrg    = "org"
	ScopeTenant = "tenant"
	ScopeTeam   = "team"

	RoleAdmin = "admin"
	RoleLead  = "lead"
)

// Org is an organization.
type Org struct {
	ID        string
	Slug      string
	Name      string
	Timezone  string
	Currency  string
	CreatedBy string
	CreatedAt time.Time
	// AppsConsent is the organization's rule for apps: AppsConsentAsk, the default, or AppsConsentAuto.
	AppsConsent string
}

// The organization's rules for apps: what the machines of the organization do with the rights an app asks for.
const (
	AppsConsentAsk  = "ask"
	AppsConsentAuto = "auto"
)

const orgCols = `id, slug, name, timezone, currency, created_by, created_at, apps_consent`

func scanOrg(row interface{ Scan(...any) error }) (Org, error) {
	var o Org
	var by, consent sql.NullString
	var created int64
	if err := row.Scan(&o.ID, &o.Slug, &o.Name, &o.Timezone, &o.Currency, &by, &created, &consent); err != nil {
		return Org{}, err
	}
	o.CreatedBy, o.CreatedAt = by.String, fromMs(created)
	o.AppsConsent = AppsConsentAsk
	if consent.String == AppsConsentAuto {
		o.AppsConsent = AppsConsentAuto
	}
	return o, nil
}

// CreateOrg inserts an organization. A taken slug is a unique violation (see IsUnique).
func (s Store) CreateOrg(ctx context.Context, o *Org) error {
	if o.ID == "" {
		o.ID = NewID()
	}
	if o.CreatedAt.IsZero() {
		o.CreatedAt = time.Now().UTC()
	}
	if o.Timezone == "" {
		o.Timezone = "Europe/Moscow"
	}
	if o.Currency == "" {
		o.Currency = "RUB"
	}
	if o.AppsConsent != AppsConsentAuto {
		o.AppsConsent = AppsConsentAsk
	}
	_, err := s.q.Exec(ctx, `INSERT INTO orgs (`+orgCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.Slug, o.Name, o.Timezone, o.Currency, nullStr(o.CreatedBy), Ms(o.CreatedAt), o.AppsConsent)
	return err
}

func (s Store) OrgByID(ctx context.Context, id string) (Org, error) {
	return scanOrg(s.q.QueryRow(ctx, `SELECT `+orgCols+` FROM orgs WHERE id = ?`, id))
}

func (s Store) OrgBySlug(ctx context.Context, slug string) (Org, error) {
	return scanOrg(s.q.QueryRow(ctx, `SELECT `+orgCols+` FROM orgs WHERE slug = ?`, slug))
}

func (s Store) UpdateOrg(ctx context.Context, o Org) error {
	consent := AppsConsentAsk
	if o.AppsConsent == AppsConsentAuto {
		consent = AppsConsentAuto
	}
	_, err := s.q.Exec(ctx, `UPDATE orgs SET name = ?, timezone = ?, currency = ?, apps_consent = ? WHERE id = ?`, o.Name, o.Timezone, o.Currency, consent, o.ID)
	return err
}

func (s Store) queryOrgs(ctx context.Context, q string, args ...any) ([]Org, error) {
	rows, err := s.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Org
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// OrgsForUser lists the organizations a person has anything in: a grant on the organization, or a membership of one of
// its tenants. Oldest first.
func (s Store) OrgsForUser(ctx context.Context, userID string) ([]Org, error) {
	return s.queryOrgs(ctx, `SELECT `+orgCols+` FROM orgs WHERE id IN (
			SELECT scope_id FROM role_grants WHERE user_id = ? AND scope_kind = 'org'
			UNION SELECT s.org_id FROM spaces s JOIN memberships m ON m.space_id = s.id WHERE m.user_id = ? AND s.org_id IS NOT NULL
		) ORDER BY created_at, id`, userID, userID)
}

// AllOrgs lists every organization (an instance administrator's view).
func (s Store) AllOrgs(ctx context.Context) ([]Org, error) {
	return s.queryOrgs(ctx, `SELECT `+orgCols+` FROM orgs ORDER BY created_at, id`)
}

// SetSpaceOrg moves a space into an organization ("" takes it out, back to the implicit one).
func (s Store) SetSpaceOrg(ctx context.Context, spaceID, orgID string) error {
	_, err := s.q.Exec(ctx, `UPDATE spaces SET org_id = ? WHERE id = ?`, nullStr(orgID), spaceID)
	return err
}

// SpacesByOrg lists the tenants of an organization, oldest first.
func (s Store) SpacesByOrg(ctx context.Context, orgID string) ([]Space, error) {
	rows, err := s.q.Query(ctx, `SELECT `+spaceCols+` FROM spaces WHERE org_id = ? ORDER BY created_at, id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Space
	for rows.Next() {
		sp, err := scanSpace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// Team is a team inside a tenant.
type Team struct {
	ID        string
	SpaceID   string
	Slug      string
	Name      string
	CreatedBy string
	CreatedAt time.Time
}

const teamCols = `id, space_id, slug, name, created_by, created_at`

func scanTeam(row interface{ Scan(...any) error }) (Team, error) {
	var t Team
	var by sql.NullString
	var created int64
	if err := row.Scan(&t.ID, &t.SpaceID, &t.Slug, &t.Name, &by, &created); err != nil {
		return Team{}, err
	}
	t.CreatedBy, t.CreatedAt = by.String, fromMs(created)
	return t, nil
}

// CreateTeam inserts a team. A slug taken in the space is a unique violation.
func (s Store) CreateTeam(ctx context.Context, t *Team) error {
	if t.ID == "" {
		t.ID = NewID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO teams (`+teamCols+`) VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.SpaceID, t.Slug, t.Name, nullStr(t.CreatedBy), Ms(t.CreatedAt))
	return err
}

func (s Store) TeamByID(ctx context.Context, id string) (Team, error) {
	return scanTeam(s.q.QueryRow(ctx, `SELECT `+teamCols+` FROM teams WHERE id = ?`, id))
}

func (s Store) TeamBySlug(ctx context.Context, spaceID, slug string) (Team, error) {
	return scanTeam(s.q.QueryRow(ctx, `SELECT `+teamCols+` FROM teams WHERE space_id = ? AND slug = ?`, spaceID, slug))
}

func (s Store) RenameTeam(ctx context.Context, id, name string) error {
	_, err := s.q.Exec(ctx, `UPDATE teams SET name = ? WHERE id = ?`, name, id)
	return err
}

func (s Store) DeleteTeam(ctx context.Context, id string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM teams WHERE id = ?`, id)
	return err
}

// TeamsBySpace lists a tenant's teams, oldest first.
func (s Store) TeamsBySpace(ctx context.Context, spaceID string) ([]Team, error) {
	rows, err := s.q.Query(ctx, `SELECT `+teamCols+` FROM teams WHERE space_id = ? ORDER BY created_at, id`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TeamMember is a person in a team.
type TeamMember struct {
	TeamID    string
	UserID    string
	IsPrimary bool
	CreatedAt time.Time
}

// AddTeamMember puts a person in a team; already there is no error, and is_primary is left as it was.
func (s Store) AddTeamMember(ctx context.Context, teamID, userID string, primary bool, at time.Time) error {
	_, err := s.q.Exec(ctx, `INSERT INTO team_members (team_id, user_id, is_primary, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (team_id, user_id) DO NOTHING`, teamID, userID, boolInt(primary), Ms(at))
	return err
}

func (s Store) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID)
	return err
}

// SetPrimaryTeam makes teamID the person's primary team in its tenant, and no other team of that tenant.
func (s Store) SetPrimaryTeam(ctx context.Context, userID, teamID string) error {
	if _, err := s.q.Exec(ctx, `UPDATE team_members SET is_primary = 0 WHERE user_id = ? AND team_id IN (
			SELECT id FROM teams WHERE space_id = (SELECT space_id FROM teams WHERE id = ?))`, userID, teamID); err != nil {
		return err
	}
	_, err := s.q.Exec(ctx, `UPDATE team_members SET is_primary = 1 WHERE user_id = ? AND team_id = ?`, userID, teamID)
	return err
}

func (s Store) queryTeamMembers(ctx context.Context, q string, args ...any) ([]TeamMember, error) {
	rows, err := s.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeamMember
	for rows.Next() {
		var m TeamMember
		var primary int
		var created int64
		if err := rows.Scan(&m.TeamID, &m.UserID, &primary, &created); err != nil {
			return nil, err
		}
		m.IsPrimary, m.CreatedAt = primary != 0, fromMs(created)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s Store) TeamMembers(ctx context.Context, teamID string) ([]TeamMember, error) {
	return s.queryTeamMembers(ctx, `SELECT team_id, user_id, is_primary, created_at FROM team_members WHERE team_id = ? ORDER BY created_at, user_id`, teamID)
}

// TeamsOfUser lists the teams a person is in, across tenants.
func (s Store) TeamsOfUser(ctx context.Context, userID string) ([]TeamMember, error) {
	return s.queryTeamMembers(ctx, `SELECT team_id, user_id, is_primary, created_at FROM team_members WHERE user_id = ? ORDER BY created_at, team_id`, userID)
}

// RoleGrant is a role on a scope.
type RoleGrant struct {
	UserID    string
	ScopeKind string
	ScopeID   string
	Role      string
	GrantedBy string
	GrantedAt time.Time
}

const grantCols = `user_id, scope_kind, scope_id, role, granted_by, granted_at`

func (s Store) queryGrants(ctx context.Context, q string, args ...any) ([]RoleGrant, error) {
	rows, err := s.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoleGrant
	for rows.Next() {
		var g RoleGrant
		var by sql.NullString
		var at int64
		if err := rows.Scan(&g.UserID, &g.ScopeKind, &g.ScopeID, &g.Role, &by, &at); err != nil {
			return nil, err
		}
		g.GrantedBy, g.GrantedAt = by.String, fromMs(at)
		out = append(out, g)
	}
	return out, rows.Err()
}

// Grant gives a role; false when the person had it already.
func (s Store) Grant(ctx context.Context, g RoleGrant) (bool, error) {
	if g.GrantedAt.IsZero() {
		g.GrantedAt = time.Now().UTC()
	}
	res, err := s.q.Exec(ctx, `INSERT INTO role_grants (`+grantCols+`) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (user_id, scope_kind, scope_id, role) DO NOTHING`,
		g.UserID, g.ScopeKind, g.ScopeID, g.Role, nullStr(g.GrantedBy), Ms(g.GrantedAt))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Revoke takes a role away; false when the person did not have it.
func (s Store) Revoke(ctx context.Context, userID, scopeKind, scopeID, role string) (bool, error) {
	res, err := s.q.Exec(ctx, `DELETE FROM role_grants WHERE user_id = ? AND scope_kind = ? AND scope_id = ? AND role = ?`, userID, scopeKind, scopeID, role)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GrantsOfUser lists every role a person holds.
func (s Store) GrantsOfUser(ctx context.Context, userID string) ([]RoleGrant, error) {
	return s.queryGrants(ctx, `SELECT `+grantCols+` FROM role_grants WHERE user_id = ? ORDER BY granted_at, scope_kind, scope_id`, userID)
}

// GrantsOn lists the roles held on one scope.
func (s Store) GrantsOn(ctx context.Context, scopeKind, scopeID string) ([]RoleGrant, error) {
	return s.queryGrants(ctx, `SELECT `+grantCols+` FROM role_grants WHERE scope_kind = ? AND scope_id = ? ORDER BY granted_at, user_id`, scopeKind, scopeID)
}

// DeleteGrantsOn takes every role on a scope away (a deleted team's leads).
func (s Store) DeleteGrantsOn(ctx context.Context, scopeKind, scopeID string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM role_grants WHERE scope_kind = ? AND scope_id = ?`, scopeKind, scopeID)
	return err
}

// CountGrants counts the holders of a role on a scope (for the last administrator).
func (s Store) CountGrants(ctx context.Context, scopeKind, scopeID, role string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM role_grants WHERE scope_kind = ? AND scope_id = ? AND role = ?`, scopeKind, scopeID, role).Scan(&n)
	return n, err
}

// AuditEvent is one change in the journal. Before and After are JSON ("" for none).
type AuditEvent struct {
	ID         string
	OrgID      string
	SpaceID    string
	TeamID     string
	ActorID    string
	ScopeKind  string
	ScopeID    string
	Action     string
	TargetKind string
	TargetID   string
	Before     string
	After      string
	Self       bool
	At         time.Time
}

const auditCols = `id, org_id, space_id, team_id, actor_id, scope_kind, scope_id, action, target_kind, target_id, before_json, after_json, self, at`

// AddAudit writes a journal entry; call it in the change's transaction.
func (s Store) AddAudit(ctx context.Context, e *AuditEvent) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO audit_events (`+auditCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, nullStr(e.OrgID), nullStr(e.SpaceID), nullStr(e.TeamID), nullStr(e.ActorID), e.ScopeKind, e.ScopeID, e.Action,
		e.TargetKind, e.TargetID, nullStr(e.Before), nullStr(e.After), boolInt(e.Self), Ms(e.At))
	return err
}

// AuditFilter picks the journal of a scope: exactly one of OrgID, SpaceID, TeamID is set, and the scope's own events
// and its descendants' come back, newest first.
type AuditFilter struct {
	OrgID, SpaceID, TeamID string
	Before                 time.Time // only events older than this; zero: from the newest
	// BeforeID with Before is a page's cursor: the events of the same instant with a smaller id come too, so a page
	// boundary never falls between events of one transaction.
	BeforeID string
	Limit    int
	// TargetUserID keeps only the events about this person (what a plain member of the scope may read).
	TargetUserID string
}

func (s Store) Audit(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	var col, val string
	switch {
	case f.TeamID != "":
		col, val = "team_id", f.TeamID
	case f.SpaceID != "":
		col, val = "space_id", f.SpaceID
	case f.OrgID != "":
		col, val = "org_id", f.OrgID
	default:
		return nil, errors.New("audit: no scope")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	before := int64(1<<62 - 1)
	if !f.Before.IsZero() {
		before = Ms(f.Before)
	}
	where, args := col+` = ? AND at < ?`, []any{val, before}
	if f.BeforeID != "" {
		where, args = col+` = ? AND (at < ? OR (at = ? AND id < ?))`, []any{val, before, before, f.BeforeID}
	}
	if f.TargetUserID != "" {
		where += ` AND target_kind = 'user' AND target_id = ?`
		args = append(args, f.TargetUserID)
	}
	rows, err := s.q.Query(ctx, `SELECT `+auditCols+` FROM audit_events WHERE `+where+` ORDER BY at DESC, id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var org, space, team, actor, was, became sql.NullString
		var self int
		var at int64
		if err := rows.Scan(&e.ID, &org, &space, &team, &actor, &e.ScopeKind, &e.ScopeID, &e.Action, &e.TargetKind, &e.TargetID, &was, &became, &self, &at); err != nil {
			return nil, err
		}
		e.OrgID, e.SpaceID, e.TeamID, e.ActorID, e.Before, e.After = org.String, space.String, team.String, actor.String, was.String, became.String
		e.Self, e.At = self != 0, fromMs(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetMachineTeam puts a machine in a team of its space ("" for none).
func (s Store) SetMachineTeam(ctx context.Context, machineID, teamID string) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET team_id = ? WHERE id = ?`, nullStr(teamID), machineID)
	return err
}

// SetMachineDisabled switches a machine off (at set) or on again (nil).
func (s Store) SetMachineDisabled(ctx context.Context, machineID string, at *time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE machines SET disabled_at = ? WHERE id = ?`, nullMs(at), machineID)
	return err
}

// CountOrgsBy is how many organizations the person created.
func (s Store) CountOrgsBy(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM orgs WHERE created_by = ?`, userID).Scan(&n)
	return n, err
}

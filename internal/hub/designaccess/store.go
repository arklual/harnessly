package designaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const (
	denyCooldown       = 24 * time.Hour
	maxInviteLife      = 7 * 24 * time.Hour
	maxMessage         = 1000
	maxTitle           = 200
	logLimit           = 100
	maxInviteUses      = 10000
	maxProjectsByOwner = 1000
)

var (
	teamLevels    = map[string]bool{levelNone: true, roleView: true, roleComment: true, roleEdit: true}
	grantRoles    = map[string]bool{roleManage: true, roleEdit: true, roleComment: true, roleView: true}
	inviteRoles   = map[string]bool{roleEdit: true, roleComment: true, roleView: true}
	requestLevels = map[string]bool{roleComment: true, roleEdit: true}
)

// txn is one request's reading or writing: a transaction (or the database), the machine, the clock, its team answers.
type txn struct {
	ctx  context.Context
	st   store.Store
	q    store.Q
	pg   bool
	m    store.Machine
	w    *world
	now  time.Time
	seal *sealer // nil: link addresses are not kept
	// shared: a project's rows may be under another machine of the space (Service.shared).
	shared bool
	homes  map[string]string
}

// key is the machine whose rows hold a project: this one, or with a shared library the machine of the space that
// recorded it (the most recently changed when several did). A project nobody recorded yet is this machine's.
func (t *txn) key(id string) string {
	if !t.shared || t.m.SpaceID == "" {
		return t.m.ID
	}
	if h, ok := t.homes[id]; ok {
		return h
	}
	home := t.m.ID
	var mine int
	if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM design_projects WHERE machine_id = ? AND project_id = ?`, t.m.ID, id).Scan(&mine); err == nil && mine == 0 {
		var other string
		err := t.q.QueryRow(t.ctx, `SELECT p.machine_id FROM design_projects p JOIN machines m ON m.id = p.machine_id
			WHERE p.project_id = ? AND m.space_id = ? AND m.revoked_at IS NULL ORDER BY p.updated_at DESC, p.machine_id LIMIT 1`, id, t.m.SpaceID).Scan(&other)
		if err == nil && other != "" {
			home = other
		}
	}
	if t.homes == nil {
		t.homes = map[string]string{}
	}
	t.homes[id] = home
	return home
}

func (s *Service) read(ctx context.Context, m store.Machine) (*txn, error) {
	st := s.db.S()
	w, err := newWorld(ctx, st, m)
	if err != nil {
		return nil, err
	}
	return &txn{ctx: ctx, st: st, q: st.Q(), pg: s.db.Driver() == store.Postgres, m: m, w: w, now: s.now(), seal: s.seal, shared: s.shared}, nil
}

func (s *Service) write(ctx context.Context, m store.Machine, fn func(t *txn) error) error {
	err := s.db.Tx(ctx, func(st store.Store) error {
		w, err := newWorld(ctx, st, m)
		if err != nil {
			return err
		}
		return fn(&txn{ctx: ctx, st: st, q: st.Q(), pg: s.db.Driver() == store.Postgres, m: m, w: w, now: s.now(), seal: s.seal, shared: s.shared})
	})
	if err == nil {
		s.kickSpace(ctx, m)
	}
	return err
}

func (t *txn) forUpdate() string {
	if t.pg {
		return " FOR UPDATE"
	}
	return ""
}

const projectCols = `project_id, owner_email, owner_name, home_team, team_level, editors_can_share, legacy`

func scanProject(row interface{ Scan(...any) error }) (project, error) {
	var p project
	var owner, home sql.NullString
	var share, legacy int
	if err := row.Scan(&p.ID, &owner, &p.OwnerName, &home, &p.TeamLevel, &share, &legacy); err != nil {
		return project{}, err
	}
	p.Owner, p.HomeTeam, p.EditorsCanShare, p.Legacy = owner.String, home.String, share != 0, legacy != 0
	return p, nil
}

// getProject is the stored row, or nil.
func (t *txn) getProject(id string, lock bool) (*project, error) {
	q := `SELECT ` + projectCols + ` FROM design_projects WHERE machine_id = ? AND project_id = ?`
	if lock {
		q += t.forUpdate()
	}
	p, err := scanProject(t.q.QueryRow(t.ctx, q, t.key(id), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (t *txn) lockProject(id string) (project, error) {
	p, err := t.getProject(id, true)
	if err != nil {
		return project{}, err
	}
	if p == nil {
		return project{}, fail("not_found", "Проект не найден")
	}
	return *p, nil
}

// effective is the row as it counts now: the stored one, or the open one of a project the hub has no row for (everyone
// who may open the machine edits it, as before access existed).
func (t *txn) effective(id string) (project, bool, error) {
	p, err := t.getProject(id, false)
	if err != nil {
		return project{}, false, err
	}
	if p != nil {
		return *p, true, nil
	}
	return project{ID: id, TeamLevel: roleEdit}, false, nil
}

const memberCols = `person_email, role, granted_by, granted_at, expires_at, via_invite`

func scanMember(row interface{ Scan(...any) error }) (member, error) {
	var m member
	var at int64
	var exp sql.NullInt64
	var via sql.NullString
	if err := row.Scan(&m.Email, &m.Role, &m.GrantedBy, &at, &exp, &via); err != nil {
		return member{}, err
	}
	m.GrantedAt, m.Expires, m.ViaInvite = fromMs(at), fromNull(exp), via.String
	return m, nil
}

func (t *txn) memberOf(id, email string) (*member, error) {
	m, err := scanMember(t.q.QueryRow(t.ctx, `SELECT `+memberCols+` FROM design_members WHERE machine_id = ? AND project_id = ? AND person_email = ?`, t.key(id), id, email))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (t *txn) members(id string) ([]member, error) {
	rows, err := t.q.Query(t.ctx, `SELECT `+memberCols+` FROM design_members WHERE machine_id = ? AND project_id = ? ORDER BY granted_at, person_email`, t.key(id), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (t *txn) teamGrants(id string) ([]teamGrant, error) {
	rows, err := t.q.Query(t.ctx, `SELECT team_id, role, can_share, granted_by, granted_at, expires_at FROM design_team_grants
		WHERE machine_id = ? AND project_id = ? ORDER BY granted_at, team_id`, t.key(id), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []teamGrant
	for rows.Next() {
		var g teamGrant
		var share int
		var at int64
		var exp sql.NullInt64
		if err := rows.Scan(&g.Team, &g.Role, &share, &g.GrantedBy, &at, &exp); err != nil {
			return nil, err
		}
		g.CanShare, g.GrantedAt, g.Expires = share != 0, fromMs(at), fromNull(exp)
		out = append(out, g)
	}
	return out, rows.Err()
}

// fold is a person's access to a row now.
func (t *txn) fold(p project, email string) (folded, error) {
	email = normEmail(email)
	m, err := t.memberOf(p.ID, email)
	if err != nil {
		return folded{}, err
	}
	grants, err := t.teamGrants(p.ID)
	if err != nil {
		return folded{}, err
	}
	person, err := t.w.person(email)
	if err != nil {
		return folded{}, err
	}
	return foldAccess(p, m, grants, person, t.w.orgOf(p.HomeTeam), t.now), nil
}

func (t *txn) writeLog(projectID, actor, action, subject string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = t.insertRow(`INSERT INTO design_access_log (id, machine_id, project_id, actor, action, subject, details, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.key(projectID), projectID, actor, action, subject, string(raw), ms(t.now))
	return err
}

type logEntry struct {
	ID      int64          `json:"id"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Subject string         `json:"subject"`
	Details map[string]any `json:"details"`
	At      string         `json:"at"`
}

func (t *txn) accessLog(id string) ([]logEntry, error) {
	rows, err := t.q.Query(t.ctx, `SELECT id, actor, action, subject, details, at FROM design_access_log WHERE machine_id = ? AND project_id = ?
		ORDER BY at DESC, id DESC LIMIT ?`, t.key(id), id, logLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []logEntry{}
	for rows.Next() {
		var e logEntry
		var details string
		var at int64
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Subject, &details, &at); err != nil {
			return nil, err
		}
		e.Details = map[string]any{}
		_ = json.Unmarshal([]byte(details), &e.Details)
		e.At = isoOf(fromMs(at))
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- the runtime's events -------------------------------------------------------------------------------------------

type registered int

const (
	regDone registered = iota
	regExists
	regCap
)

// insertProject stores a project made on the machine: it lives in its maker's team, editors of a team project may share,
// and a maker with no team (an address that may not open the machine) gets a personal project — unless it is a legacy
// one, which stays as open as it was.
func (t *txn) insertProject(id, owner, ownerName, level string, legacy bool) (registered, error) {
	if t.pg {
		if _, err := t.q.Exec(t.ctx, `SELECT pg_advisory_xact_lock(hashtext(?))`, "design-owner:"+t.m.ID+":"+owner); err != nil {
			return 0, err
		}
	}
	if p, err := t.getProject(id, false); err != nil {
		return 0, err
	} else if p != nil {
		return regExists, nil
	}
	var count int
	if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM design_projects WHERE machine_id = ? AND owner_email = ?`, t.m.ID, owner).Scan(&count); err != nil {
		return 0, err
	}
	if count >= maxProjectsByOwner {
		return regCap, t.writeLog(id, owner, "owner_cap", owner, map[string]any{"limit": maxProjectsByOwner})
	}
	home, err := t.w.homeTeamOf(owner)
	if err != nil {
		return 0, err
	}
	if home == "" && !legacy {
		level = levelNone
	}
	_, err = t.q.Exec(t.ctx, `INSERT INTO design_projects (machine_id, project_id, owner_email, owner_name, home_team, team_level, editors_can_share, legacy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.key(id), id, owner, ownerName, nullStr(home), level, boolInt(home != ""), boolInt(legacy), ms(t.now), ms(t.now))
	if store.IsUnique(err) {
		return regExists, nil
	}
	return regDone, err
}

func (s *Service) registerCreated(ctx context.Context, m store.Machine, id, email string) (bool, error) {
	owner := normEmail(email)
	done := false
	err := s.write(ctx, m, func(t *txn) error {
		out, err := t.insertProject(id, owner, t.w.displayName(owner), roleEdit, false)
		if err != nil {
			return err
		}
		if out == regExists {
			p, err := t.getProject(id, false)
			if err != nil {
				return err
			}
			if p != nil && p.Owner != owner {
				return t.writeLog(id, owner, "created_conflict", owner, map[string]any{"owner": nullStr(p.Owner)})
			}
			return nil
		}
		if out != regDone {
			return nil
		}
		done = true
		return t.writeLog(id, owner, "create", owner, nil)
	})
	return done, err
}

func (s *Service) registerClaim(ctx context.Context, m store.Machine, id, email string) (bool, error) {
	owner := normEmail(email)
	done := false
	err := s.write(ctx, m, func(t *txn) error {
		out, err := t.insertProject(id, owner, t.w.displayName(owner), roleEdit, true)
		if err != nil || out != regDone {
			return err
		}
		done = true
		return t.writeLog(id, owner, "claim", owner, map[string]any{"team": roleEdit})
	})
	return done, err
}

// registerDeleted: the project went to the machine's trash. Its row goes with its people, links and requests, so a guest
// link stops answering and the id can be made again; the journal stays. Only the owner is believed.
func (s *Service) registerDeleted(ctx context.Context, m store.Machine, id, email string) (bool, error) {
	who := normEmail(email)
	done := false
	err := s.write(ctx, m, func(t *txn) error {
		p, err := t.getProject(id, true)
		if err != nil || p == nil {
			return err
		}
		f, err := t.fold(*p, who)
		if err != nil {
			return err
		}
		if f.Role != roleOwner {
			return t.writeLog(id, who, "delete_refused", who, map[string]any{"role": nullStr(f.Role)})
		}
		for _, table := range []string{"design_members", "design_team_grants", "design_requests", "design_invites", "design_share_links", "design_projects"} {
			if _, err := t.q.Exec(t.ctx, `DELETE FROM `+table+` WHERE machine_id = ? AND project_id = ?`, t.key(id), id); err != nil {
				return err
			}
		}
		done = true
		return t.writeLog(id, who, "delete", who, map[string]any{"owner": nullStr(p.Owner), "role": f.Role})
	})
	return done, err
}

// ---- changes ------------------------------------------------------------------------------------------------------

func (t *txn) setTeam(id, actor, level string) error {
	if !teamLevels[level] {
		return fail("bad_request", "Неизвестный уровень команды")
	}
	before, err := t.lockProject(id)
	if err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE design_projects SET team_level = ?, updated_at = ? WHERE machine_id = ? AND project_id = ?`, level, ms(t.now), t.key(id), id); err != nil {
		return err
	}
	return t.writeLog(id, actor, "team", level, map[string]any{"from": before.TeamLevel})
}

func (t *txn) setEditorsCanShare(id, actor string, value bool) error {
	if _, err := t.lockProject(id); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE design_projects SET editors_can_share = ?, updated_at = ? WHERE machine_id = ? AND project_id = ?`, boolInt(value), ms(t.now), t.key(id), id); err != nil {
		return err
	}
	return t.writeLog(id, actor, "settings", "editorsCanShare", map[string]any{"value": value})
}

// upsertMember gives or changes a person's own role; true when they had a row.
func (t *txn) upsertMember(id, actor, email, role string, expires *time.Time, via string) (bool, error) {
	old, err := t.memberOf(id, email)
	if err != nil {
		return false, err
	}
	_, err = t.q.Exec(t.ctx, `INSERT INTO design_members (machine_id, project_id, person_email, role, granted_by, granted_at, expires_at, via_invite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, project_id, person_email) DO UPDATE SET
		role = excluded.role, granted_by = excluded.granted_by, granted_at = excluded.granted_at, expires_at = excluded.expires_at, via_invite = excluded.via_invite`,
		t.key(id), id, email, role, actor, ms(t.now), nullTime(expires), nullStr(via))
	return old != nil, err
}

// memberRole is the person's own unexpired role, without the team or ownership.
func (t *txn) memberRole(id, email string) (string, error) {
	m, err := t.memberOf(id, normEmail(email))
	if err != nil || m == nil || !live(m.Expires, t.now) {
		return "", err
	}
	return m.Role, nil
}

func (t *txn) refuseManagerTarget(id, who string, protect bool) error {
	if !protect {
		return nil
	}
	r, err := t.memberRole(id, who)
	if err != nil {
		return err
	}
	if r == roleManage {
		return fail("forbidden", "Роль управляющего меняет владелец или управляющий")
	}
	return nil
}

func (t *txn) grant(id, actor, email, role string, term ttl, protect bool) error {
	who := normEmail(email)
	if !strings.Contains(who, "@") {
		return fail("bad_request", "Нужен адрес почты")
	}
	if !grantRoles[role] {
		return fail("bad_request", "Неизвестная роль")
	}
	expires, err := expiry(term, t.now)
	if err != nil {
		return err
	}
	p, err := t.lockProject(id)
	if err != nil {
		return err
	}
	if p.Owner == who {
		return fail("conflict", "Это владелец проекта")
	}
	if err := t.refuseManagerTarget(id, who, protect); err != nil {
		return err
	}
	changed, err := t.upsertMember(id, actor, who, role, expires, "")
	if err != nil {
		return err
	}
	action := "grant"
	if changed {
		action = "change"
	}
	return t.writeLog(id, actor, action, who, map[string]any{"role": role, "until": iso(expires)})
}

func (t *txn) revoke(id, actor, email string, protect bool) error {
	who := normEmail(email)
	p, err := t.lockProject(id)
	if err != nil {
		return err
	}
	if p.Owner == who {
		return fail("forbidden", "Доступ владельца отозвать нельзя")
	}
	if err := t.refuseManagerTarget(id, who, protect); err != nil {
		return err
	}
	res, err := t.q.Exec(t.ctx, `DELETE FROM design_members WHERE machine_id = ? AND project_id = ? AND person_email = ?`, t.key(id), id, who)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fail("not_found", "У человека нет личной роли в проекте")
	}
	return t.writeLog(id, actor, "revoke", who, nil)
}

// machinesOfProject is the condition on machine_id of what every machine made of a project (its invites and links):
// with a shared library, every machine of the space; otherwise this one.
func (t *txn) machinesOfProject() (string, []any) {
	if t.shared && t.m.SpaceID != "" {
		return `machine_id IN (SELECT id FROM machines WHERE space_id = ?)`, []any{t.m.SpaceID}
	}
	return `machine_id = ?`, []any{t.m.ID}
}

// revokeAll takes back the people, the teams' grants, the team level and every link; requests stay to be decided.
func (t *txn) revokeAll(id, actor string) error {
	if _, err := t.lockProject(id); err != nil {
		return err
	}
	people, err := t.q.Exec(t.ctx, `DELETE FROM design_members WHERE machine_id = ? AND project_id = ?`, t.key(id), id)
	if err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `DELETE FROM design_team_grants WHERE machine_id = ? AND project_id = ?`, t.key(id), id); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE design_projects SET team_level = 'none', updated_at = ? WHERE machine_id = ? AND project_id = ?`, ms(t.now), t.key(id), id); err != nil {
		return err
	}
	in, args := t.machinesOfProject()
	invites, err := t.q.Exec(t.ctx, `UPDATE design_invites SET revoked_at = ? WHERE `+in+` AND project_id = ? AND revoked_at IS NULL`, append(append([]any{ms(t.now)}, args...), id)...)
	if err != nil {
		return err
	}
	shares, err := t.q.Exec(t.ctx, `UPDATE design_share_links SET revoked_at = ? WHERE `+in+` AND project_id = ? AND revoked_at IS NULL`, append(append([]any{ms(t.now)}, args...), id)...)
	if err != nil {
		return err
	}
	np, _ := people.RowsAffected()
	ni, _ := invites.RowsAffected()
	ns, _ := shares.RowsAffected()
	return t.writeLog(id, actor, "revoke-all", "", map[string]any{"people": np, "invites": ni, "shares": ns})
}

// transfer hands the project over; the previous owner stays as a manager. strict (the shared API) wants somebody who
// signed in to the hub at least once, so a typo cannot orphan the project.
func (t *txn) transfer(id, actor, email string, strict bool) error {
	next := normEmail(email)
	if !strings.Contains(next, "@") {
		return fail("bad_request", "Нужен адрес почты")
	}
	p, err := t.lockProject(id)
	if err != nil {
		return err
	}
	if p.Owner == next {
		return fail("conflict", "Этот человек уже владелец")
	}
	person, err := t.w.person(next)
	if err != nil {
		return err
	}
	if strict && person.user == "" {
		return fail("not_found", "Этот человек ещё ни разу не входил: передать можно тому, кто уже входил")
	}
	name := t.w.displayName(next)
	if _, err := t.q.Exec(t.ctx, `DELETE FROM design_members WHERE machine_id = ? AND project_id = ? AND person_email = ?`, t.key(id), id, next); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE design_projects SET owner_email = ?, owner_name = ?, updated_at = ? WHERE machine_id = ? AND project_id = ?`, next, name, ms(t.now), t.key(id), id); err != nil {
		return err
	}
	var fromRole any
	if p.Owner != "" {
		if _, err := t.upsertMember(id, actor, p.Owner, roleManage, nil, ""); err != nil {
			return err
		}
		fromRole = roleManage
	}
	return t.writeLog(id, actor, "transfer", next, map[string]any{"from": nullStr(p.Owner), "fromRole": fromRole})
}

// ---- requests -----------------------------------------------------------------------------------------------------

type requestRow struct {
	ID, Email, Level, Message, Status string
	DecidedBy                         string
	DecidedAt                         *time.Time
	CreatedAt                         time.Time
}

func (r requestRow) old() map[string]any {
	return map[string]any{"id": r.ID, "email": r.Email, "level": r.Level, "message": r.Message, "status": r.Status,
		"decidedBy": nullStr(r.DecidedBy), "decidedAt": iso(r.DecidedAt), "createdAt": isoOf(r.CreatedAt)}
}

const requestCols = `id, person_email, level, message, status, decided_by, decided_at, created_at`

func (t *txn) requests(id, who, status string) ([]requestRow, error) {
	q := `SELECT ` + requestCols + ` FROM design_requests WHERE machine_id = ? AND project_id = ? AND status = ?`
	args := []any{t.key(id), id, status}
	if who != "" {
		q += ` AND person_email = ?`
		args = append(args, who)
	}
	q += ` ORDER BY created_at`
	rows, err := t.q.Query(t.ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []requestRow
	for rows.Next() {
		var r requestRow
		var by sql.NullString
		var at sql.NullInt64
		var created int64
		if err := rows.Scan(&r.ID, &r.Email, &r.Level, &r.Message, &r.Status, &by, &at, &created); err != nil {
			return nil, err
		}
		r.DecidedBy, r.DecidedAt, r.CreatedAt = by.String, fromNull(at), fromMs(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// request: one open request per person; a denial closes the door for a day.
func (t *txn) request(id, email, level, message string) (requestRow, error) {
	who := normEmail(email)
	if !requestLevels[level] {
		return requestRow{}, fail("bad_request", "Можно попросить комментирование или правку")
	}
	p, err := t.lockProject(id)
	if err != nil {
		return requestRow{}, err
	}
	f, err := t.fold(p, who)
	if err != nil {
		return requestRow{}, err
	}
	if f.Role != "" && rank(f.Role) >= rank(level) {
		return requestRow{}, fail("conflict", "У вас уже есть такой доступ")
	}
	if open, err := t.requests(id, who, "open"); err != nil {
		return requestRow{}, err
	} else if len(open) > 0 {
		return requestRow{}, fail("conflict", "Запрос уже отправлен и ждёт ответа")
	}
	var deniedAt sql.NullInt64
	err = t.q.QueryRow(t.ctx, `SELECT MAX(decided_at) FROM design_requests WHERE machine_id = ? AND project_id = ? AND person_email = ? AND status = 'denied'`, t.key(id), id, who).Scan(&deniedAt)
	if err != nil {
		return requestRow{}, err
	}
	if deniedAt.Valid && t.now.Sub(fromMs(deniedAt.Int64)) < denyCooldown {
		return requestRow{}, fail("rate_limited", "Запрос отклонён, повторить можно через сутки")
	}
	r := requestRow{ID: newUUID(), Email: who, Level: level, Message: clip(message, maxMessage), Status: "open", CreatedAt: t.now}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO design_requests (id, machine_id, project_id, person_email, level, message, status, created_at) VALUES (?, ?, ?, ?, ?, ?, 'open', ?)`,
		r.ID, t.key(id), id, who, level, r.Message, ms(t.now)); err != nil {
		return requestRow{}, err
	}
	return r, t.writeLog(id, who, "request", who, map[string]any{"level": level})
}

// decide: the first decision wins; a decision never lowers what the person has since got.
func (t *txn) decide(id, requestID, actor, decision, role string, term ttl) (requestRow, error) {
	if decision != "allow" && decision != "deny" {
		return requestRow{}, fail("bad_request", "Неизвестное решение")
	}
	if role != "" && !grantRoles[role] {
		return requestRow{}, fail("bad_request", "Неизвестная роль")
	}
	var expires *time.Time
	if decision == "allow" {
		var err error
		if expires, err = expiry(term, t.now); err != nil {
			return requestRow{}, err
		}
	}
	p, err := t.lockProject(id)
	if err != nil {
		return requestRow{}, err
	}
	status := "denied"
	if decision == "allow" {
		status = "allowed"
	}
	open, err := t.requests(id, "", "open")
	if err != nil {
		return requestRow{}, err
	}
	var asked *requestRow
	for i := range open {
		if open[i].ID == requestID {
			asked = &open[i]
		}
	}
	if asked == nil {
		return requestRow{}, fail("conflict", "Запрос уже обработан или не найден")
	}
	res, err := t.q.Exec(t.ctx, `UPDATE design_requests SET status = ?, decided_by = ?, decided_at = ? WHERE id = ? AND machine_id = ? AND project_id = ? AND status = 'open'`,
		status, actor, ms(t.now), requestID, t.key(id), id)
	if err != nil {
		return requestRow{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return requestRow{}, fail("conflict", "Запрос уже обработан или не найден")
	}
	asked.Status, asked.DecidedBy, asked.DecidedAt = status, actor, &t.now
	if decision == "deny" {
		return *asked, t.writeLog(id, actor, "deny", asked.Email, map[string]any{"request": asked.ID})
	}
	given := role
	if given == "" {
		given = asked.Level
	}
	f, err := t.fold(p, asked.Email)
	if err != nil {
		return requestRow{}, err
	}
	if f.Role != "" && rank(f.Role) >= rank(given) {
		return *asked, t.writeLog(id, actor, "allow", asked.Email, map[string]any{"role": f.Role, "kept": true, "request": asked.ID})
	}
	if _, err := t.upsertMember(id, actor, asked.Email, given, expires, ""); err != nil {
		return requestRow{}, err
	}
	return *asked, t.writeLog(id, actor, "allow", asked.Email, map[string]any{"role": given, "until": iso(expires), "request": asked.ID})
}

// ---- team grants --------------------------------------------------------------------------------------------------

func (t *txn) grantTeam(id, actor, team, role string, canShare bool, term ttl) error {
	if !teamIDRe.MatchString(team) {
		return fail("bad_request", "Некорректная команда")
	}
	if role != roleView && role != roleComment && role != roleEdit {
		return fail("bad_request", "Роль: view, comment или edit")
	}
	expires, err := expiry(term, t.now)
	if err != nil {
		return err
	}
	sharing := role == roleEdit && canShare
	p, err := t.lockProject(id)
	if err != nil {
		return err
	}
	if p.HomeTeam == team {
		return fail("conflict", "Это своя команда: её доступ меняется в первой строке")
	}
	if _, ok := t.w.team(team); !ok {
		return fail("not_found", "Команда не найдена")
	}
	var existed int
	if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM design_team_grants WHERE machine_id = ? AND project_id = ? AND team_id = ?`, t.key(id), id, team).Scan(&existed); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO design_team_grants (machine_id, project_id, team_id, role, can_share, granted_by, granted_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, project_id, team_id) DO UPDATE SET
		role = excluded.role, can_share = excluded.can_share, granted_by = excluded.granted_by, granted_at = excluded.granted_at, expires_at = excluded.expires_at`,
		t.key(id), id, team, role, boolInt(sharing), actor, ms(t.now), nullTime(expires)); err != nil {
		return err
	}
	action := "team_grant"
	if existed > 0 {
		action = "team_change"
	}
	return t.writeLog(id, actor, action, "team:"+team, map[string]any{"role": role, "canShare": sharing, "until": iso(expires)})
}

func (t *txn) teamGrantShares(id, team string) (bool, error) {
	grants, err := t.teamGrants(id)
	if err != nil {
		return false, err
	}
	for _, g := range grants {
		if g.Team == team && live(g.Expires, t.now) {
			return g.CanShare, nil
		}
	}
	return false, nil
}

func (t *txn) revokeTeam(id, actor, team string) error {
	if _, err := t.lockProject(id); err != nil {
		return err
	}
	res, err := t.q.Exec(t.ctx, `DELETE FROM design_team_grants WHERE machine_id = ? AND project_id = ? AND team_id = ?`, t.key(id), id, team)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fail("not_found", "У команды нет доступа")
	}
	return t.writeLog(id, actor, "team_revoke", "team:"+team, nil)
}

// ---- invites (old API) --------------------------------------------------------------------------------------------

type inviteRow struct {
	ID, Title, Role string
	RoleTTL         *int64
	Expires         time.Time
	MaxUses         *int64
	Uses            int64
	Revoked         *time.Time
	CreatedBy       string
	CreatedAt       time.Time
}

func stepOf(msv *int64) any {
	if msv == nil {
		return nil
	}
	for k, d := range ttlSteps {
		if d.Milliseconds() == *msv {
			return k
		}
	}
	return nil
}

func (r inviteRow) json() map[string]any {
	var maxUses any
	if r.MaxUses != nil {
		maxUses = *r.MaxUses
	}
	return map[string]any{"id": r.ID, "title": r.Title, "role": r.Role, "roleTtl": stepOf(r.RoleTTL), "expiresAt": isoOf(r.Expires),
		"maxUses": maxUses, "uses": r.Uses, "revokedAt": iso(r.Revoked), "createdBy": r.CreatedBy, "createdAt": isoOf(r.CreatedAt)}
}

const inviteCols = `id, title, role, role_ttl_ms, expires_at, max_uses, uses, revoked_at, created_by, created_at`

func scanInvite(row interface{ Scan(...any) error }) (inviteRow, error) {
	var r inviteRow
	var roleTTL, maxUses, revoked sql.NullInt64
	var exp, created int64
	if err := row.Scan(&r.ID, &r.Title, &r.Role, &roleTTL, &exp, &maxUses, &r.Uses, &revoked, &r.CreatedBy, &created); err != nil {
		return inviteRow{}, err
	}
	if roleTTL.Valid {
		r.RoleTTL = &roleTTL.Int64
	}
	if maxUses.Valid {
		r.MaxUses = &maxUses.Int64
	}
	r.Expires, r.Revoked, r.CreatedAt = fromMs(exp), fromNull(revoked), fromMs(created)
	return r, nil
}

func (t *txn) liveInvites(id string) ([]inviteRow, error) {
	rows, err := t.q.Query(t.ctx, `SELECT `+inviteCols+` FROM design_invites WHERE machine_id = ? AND project_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, t.m.ID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []inviteRow
	for rows.Next() {
		r, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type inviteInput struct {
	Role    string
	RoleTTL ttl // none or a step
	TTL     ttl
	MaxUses *int64
	Title   string
}

func (t *txn) createInvite(id, actor string, in inviteInput) (string, string, time.Time, error) {
	if !inviteRoles[in.Role] {
		return "", "", time.Time{}, fail("bad_request", "По ссылке можно дать просмотр, комментирование или правку")
	}
	expires, err := expiry(in.TTL, t.now)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if expires == nil || expires.Sub(t.now) > maxInviteLife {
		return "", "", time.Time{}, fail("bad_request", "Ссылка живёт не дольше 7 дней")
	}
	var roleTTL any
	if !in.RoleTTL.none {
		if in.RoleTTL.step == 0 {
			return "", "", time.Time{}, fail("bad_request", "Неизвестный срок роли")
		}
		roleTTL = in.RoleTTL.step.Milliseconds()
	}
	var maxUses any
	if in.MaxUses != nil {
		if *in.MaxUses < 1 || *in.MaxUses > maxInviteUses {
			return "", "", time.Time{}, fail("bad_request", fmt.Sprintf("Число использований — целое от 1 до %d", maxInviteUses))
		}
		maxUses = *in.MaxUses
	}
	if _, err := t.lockProject(id); err != nil {
		return "", "", time.Time{}, err
	}
	token := randomToken(32)
	inviteID := newUUID()
	if _, err := t.q.Exec(t.ctx, `INSERT INTO design_invites (id, machine_id, project_id, token_hash, title, role, role_ttl_ms, expires_at, max_uses, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, inviteID, t.key(id), id, store.HashToken(token), clip(in.Title, maxTitle), in.Role, roleTTL, ms(*expires), maxUses, normEmail(actor), ms(t.now)); err != nil {
		return "", "", time.Time{}, err
	}
	return inviteID, token, *expires, t.writeLog(id, actor, "invite", inviteID, map[string]any{"role": in.Role, "until": iso(expires), "maxUses": maxUses})
}

func (t *txn) revokeInvite(id, actor, inviteID string) error {
	if _, err := t.lockProject(id); err != nil {
		return err
	}
	res, err := t.q.Exec(t.ctx, `UPDATE design_invites SET revoked_at = ? WHERE id = ? AND machine_id = ? AND project_id = ? AND revoked_at IS NULL`, ms(t.now), inviteID, t.m.ID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fail("not_found", "Ссылка не найдена или уже отозвана")
	}
	return t.writeLog(id, actor, "invite-revoke", inviteID, nil)
}

var inviteTokenRe = regexpToken(43)

// acceptInvite: whoever opens a live invite signed in gets its role. A use is taken by a conditional update, so two
// people racing for the last place cannot both get it; a link never lowers a role the person already has, and it speaks
// for its maker only while they may still share.
func (t *txn) acceptInvite(token, email string) (string, string, *time.Time, error) {
	if !inviteTokenRe.MatchString(token) {
		return "", "", nil, fail("not_found", "Ссылка не найдена")
	}
	who := normEmail(email)
	hash := store.HashToken(token)
	var projectID string
	row := t.q.QueryRow(t.ctx, `SELECT project_id, `+inviteCols+` FROM design_invites WHERE machine_id = ? AND token_hash = ?`, t.m.ID, hash)
	var inv inviteRow
	{
		var roleTTL, maxUses, revoked sql.NullInt64
		var exp, created int64
		err := row.Scan(&projectID, &inv.ID, &inv.Title, &inv.Role, &roleTTL, &exp, &maxUses, &inv.Uses, &revoked, &inv.CreatedBy, &created)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", nil, fail("not_found", "Ссылка не найдена")
		}
		if err != nil {
			return "", "", nil, err
		}
		if roleTTL.Valid {
			inv.RoleTTL = &roleTTL.Int64
		}
		inv.Expires, inv.Revoked = fromMs(exp), fromNull(revoked)
	}
	p, err := t.lockProject(projectID)
	if err != nil {
		return "", "", nil, err
	}
	if p.Owner == who {
		return projectID, roleOwner, nil, nil
	}
	if who == normEmail(inv.CreatedBy) {
		f, err := t.fold(p, who)
		if err != nil {
			return "", "", nil, err
		}
		if f.Role == "" {
			return "", "", nil, fail("forbidden", "Нет доступа")
		}
		return projectID, f.Role, f.Until, nil
	}
	gone := fail("gone", "Ссылка устарела, отозвана или использована")
	if inv.Revoked != nil || !inv.Expires.After(t.now) {
		return "", "", nil, gone
	}
	maker, err := t.fold(p, inv.CreatedBy)
	if err != nil {
		return "", "", nil, err
	}
	if !maker.CanManage {
		return "", "", nil, gone
	}
	var memberExpires *time.Time
	if inv.RoleTTL != nil {
		e := t.now.Add(time.Duration(*inv.RoleTTL) * time.Millisecond)
		memberExpires = &e
	}
	if own, err := t.memberOf(projectID, who); err != nil {
		return "", "", nil, err
	} else if own != nil && live(own.Expires, t.now) {
		lasts := own.Expires != nil && (memberExpires == nil || memberExpires.After(*own.Expires))
		if rank(own.Role) > rank(inv.Role) || (own.Role == inv.Role && !lasts) {
			return projectID, own.Role, own.Expires, nil
		}
	}
	res, err := t.q.Exec(t.ctx, `UPDATE design_invites SET uses = uses + 1 WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ? AND (max_uses IS NULL OR uses < max_uses)`, hash, ms(t.now))
	if err != nil {
		return "", "", nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", "", nil, gone
	}
	if _, err := t.upsertMember(projectID, inv.CreatedBy, who, inv.Role, memberExpires, inv.ID); err != nil {
		return "", "", nil, err
	}
	return projectID, inv.Role, memberExpires, t.writeLog(projectID, who, "invite-accept", inv.ID, map[string]any{"role": inv.Role, "until": iso(memberExpires)})
}

// expireDue removes the roles that ran out (the fold already stopped honouring them) and says which machines changed.
func (s *Service) expireDue(ctx context.Context) ([]string, error) {
	now := s.now()
	rows, err := s.db.Query(ctx, `SELECT machine_id, project_id, person_email, role, expires_at FROM design_members WHERE expires_at IS NOT NULL AND expires_at <= ?`, ms(now))
	if err != nil {
		return nil, err
	}
	type gone struct {
		machine, project, email, role string
		at                            int64
	}
	var all []gone
	for rows.Next() {
		var g gone
		if err := rows.Scan(&g.machine, &g.project, &g.email, &g.role, &g.at); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, g)
	}
	rows.Close()
	machines := map[string]bool{}
	for _, g := range all {
		err := s.db.Tx(ctx, func(st store.Store) error {
			res, err := st.Q().Exec(ctx, `DELETE FROM design_members WHERE machine_id = ? AND project_id = ? AND person_email = ? AND expires_at = ?`, g.machine, g.project, g.email, g.at)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil
			}
			machines[g.machine] = true
			t := &txn{ctx: ctx, st: st, q: st.Q(), m: store.Machine{ID: g.machine}, now: now}
			return t.writeLog(g.project, "system", "expire", g.email, map[string]any{"role": g.role, "at": isoOf(fromMs(g.at))})
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(machines))
	for id := range machines {
		out = append(out, id)
	}
	return out, nil
}

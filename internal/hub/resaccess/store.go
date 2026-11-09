package resaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Service is the access model over the hub's database.
type Service struct {
	db        *store.DB
	now       func() time.Time
	publicURL string
	// Notify tells a person that someone asked them for access or that a request was decided; nil keeps the hub silent.
	Notify func(ctx context.Context, to, subject, body string)
}

// New returns the service; publicURL is the address guest links are built on (the hub's own).
func New(db *store.DB, now func() time.Time, publicURL string) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now, publicURL: strings.TrimRight(publicURL, "/")}
}

type txn struct {
	ctx context.Context
	st  store.Store
	q   store.Q
	pg  bool
	now time.Time
	w   *world
}

func (s *Service) read(ctx context.Context) *txn {
	st := s.db.S()
	return &txn{ctx: ctx, st: st, q: st.Q(), pg: s.db.Driver() == store.Postgres, now: s.now(), w: newWorld(ctx, st)}
}

func (s *Service) write(ctx context.Context, fn func(t *txn) error) error {
	return s.db.Tx(ctx, func(st store.Store) error {
		return fn(&txn{ctx: ctx, st: st, q: st.Q(), pg: s.db.Driver() == store.Postgres, now: s.now(), w: newWorld(ctx, st)})
	})
}

func (t *txn) forUpdate() string {
	if t.pg {
		return " FOR UPDATE"
	}
	return ""
}

// ---- reading a resource ----------------------------------------------------------------------------------------------

func (t *txn) stored(ref Ref, lock bool) (*resource, error) {
	q := `SELECT owner_email, owner_name, home_team, team_level, editors_can_share, title FROM resource_access WHERE kind = ? AND id = ?`
	if lock {
		q += t.forUpdate()
	}
	r := resource{Ref: ref, Stored: true}
	var share int
	err := t.q.QueryRow(t.ctx, q, ref.Kind, ref.ID).Scan(&r.Owner, &r.OwnerName, &r.HomeTeam, &r.TeamLevel, &share, &r.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.EditorsCanShare = share != 0
	return &r, nil
}

// legacy is a resource as the kind's older records have it: the page's picture before the model had a row for it.
func (t *txn) legacy(ref Ref) (resource, []member, []teamGrant, error) {
	r := resource{Ref: ref, TeamLevel: roleView}
	switch ref.Kind {
	case KindDoc:
		var level string
		err := t.q.QueryRow(t.ctx, `SELECT owner_email, owner_name, team_level, title FROM doc_meta WHERE slug = ?`, ref.ID).Scan(&r.Owner, &r.OwnerName, &level, &r.Title)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return r, nil, nil, err
		}
		if err == nil && teamLevels[level] {
			r.TeamLevel = level
		}
		r.Owner = normEmail(r.Owner)
	case KindDesign:
		machine, project, ok := strings.Cut(ref.ID, "/")
		if !ok {
			return r, nil, nil, nil
		}
		var owner, home sql.NullString
		var share int
		err := t.q.QueryRow(t.ctx, `SELECT owner_email, owner_name, home_team, team_level, editors_can_share, title FROM design_projects
			WHERE machine_id = ? AND project_id = ?`, machine, project).Scan(&owner, &r.OwnerName, &home, &r.TeamLevel, &share, &r.Title)
		if errors.Is(err, sql.ErrNoRows) {
			return r, nil, nil, nil
		}
		if err != nil {
			return r, nil, nil, err
		}
		r.Owner, r.HomeTeam, r.EditorsCanShare = normEmail(owner.String), home.String, share != 0
		var people []member
		rows, err := t.q.Query(t.ctx, `SELECT person_email, role, granted_by, granted_at, expires_at FROM design_members WHERE machine_id = ? AND project_id = ?`, machine, project)
		if err != nil {
			return r, nil, nil, err
		}
		for rows.Next() {
			var m member
			var at int64
			var exp sql.NullInt64
			if err := rows.Scan(&m.Email, &m.Role, &m.GrantedBy, &at, &exp); err != nil {
				rows.Close()
				return r, nil, nil, err
			}
			m.GrantedAt = fromMs(at)
			if exp.Valid {
				e := fromMs(exp.Int64)
				m.Expires = &e
			}
			people = append(people, m)
		}
		rows.Close()
		var grants []teamGrant
		rows, err = t.q.Query(t.ctx, `SELECT team_id, role, can_share, granted_by, granted_at, expires_at FROM design_team_grants WHERE machine_id = ? AND project_id = ?`, machine, project)
		if err != nil {
			return r, nil, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var g teamGrant
			var share, at int64
			var exp sql.NullInt64
			if err := rows.Scan(&g.Team, &g.Role, &share, &g.GrantedBy, &at, &exp); err != nil {
				return r, nil, nil, err
			}
			g.CanShare, g.GrantedAt = share != 0, fromMs(at)
			if exp.Valid {
				e := fromMs(exp.Int64)
				g.Expires = &e
			}
			grants = append(grants, g)
		}
		return r, people, grants, rows.Err()
	}
	return r, nil, nil, nil
}

// load is the resource with its people and team grants: the model's rows, or the older records of the kind.
func (t *txn) load(ref Ref, lock bool) (resource, []member, []teamGrant, error) {
	r, err := t.stored(ref, lock)
	if err != nil {
		return resource{}, nil, nil, err
	}
	if r == nil {
		return t.legacy(ref)
	}
	people, err := t.members(ref)
	if err != nil {
		return resource{}, nil, nil, err
	}
	grants, err := t.teamGrants(ref)
	if err != nil {
		return resource{}, nil, nil, err
	}
	return *r, people, grants, nil
}

func (t *txn) members(ref Ref) ([]member, error) {
	rows, err := t.q.Query(t.ctx, `SELECT email, role, granted_by, granted_at, expires_at FROM resource_access_people
		WHERE kind = ? AND id = ? ORDER BY granted_at, email`, ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []member
	for rows.Next() {
		var m member
		var at int64
		var exp sql.NullInt64
		if err := rows.Scan(&m.Email, &m.Role, &m.GrantedBy, &at, &exp); err != nil {
			return nil, err
		}
		m.GrantedAt = fromMs(at)
		if exp.Valid {
			e := fromMs(exp.Int64)
			m.Expires = &e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (t *txn) teamGrants(ref Ref) ([]teamGrant, error) {
	rows, err := t.q.Query(t.ctx, `SELECT team_id, role, can_share, granted_by, granted_at, expires_at FROM resource_access_teams
		WHERE kind = ? AND id = ? ORDER BY granted_at, team_id`, ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []teamGrant
	for rows.Next() {
		var g teamGrant
		var share, at int64
		var exp sql.NullInt64
		if err := rows.Scan(&g.Team, &g.Role, &share, &g.GrantedBy, &at, &exp); err != nil {
			return nil, err
		}
		g.CanShare, g.GrantedAt = share != 0, fromMs(at)
		if exp.Valid {
			e := fromMs(exp.Int64)
			g.Expires = &e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// foldFor is a person's access to a resource now.
func (t *txn) foldFor(r resource, people []member, grants []teamGrant, email string) (folded, error) {
	email = normEmail(email)
	person, err := t.w.person(email)
	if err != nil {
		return folded{}, err
	}
	var mine *member
	for i := range people {
		if people[i].Email == email {
			mine = &people[i]
		}
	}
	return fold(r, mine, grants, person, t.w.homeOrg(r.HomeTeam), t.now), nil
}

// ---- writing --------------------------------------------------------------------------------------------------------

func (t *txn) log(ref Ref, actor, action, subject string, details map[string]any) error {
	d := ""
	if len(details) > 0 {
		b, _ := json.Marshal(details)
		d = string(b)
	}
	_, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_log (log_id, kind, id, actor, action, subject, details, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		newID(), ref.Kind, ref.ID, actor, action, subject, d, ms(t.now))
	return err
}

// ensure writes the resource's row (and copies the older records of its kind) the first time a manager changes it.
// owner and ownerName are used only when the kind's older records know none.
func (t *txn) ensure(ref Ref, owner, ownerName string) (resource, error) {
	if r, err := t.stored(ref, true); err != nil || r != nil {
		if r != nil {
			return *r, err
		}
		return resource{}, err
	}
	r, people, grants, err := t.legacy(ref)
	if err != nil {
		return r, err
	}
	if r.Owner == "" {
		r.Owner, r.OwnerName = normEmail(owner), ownerName
	}
	now := ms(t.now)
	share := 0
	if r.EditorsCanShare {
		share = 1
	}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access (kind, id, owner_email, owner_name, home_team, team_level, editors_can_share, title, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (kind, id) DO NOTHING`,
		ref.Kind, ref.ID, r.Owner, r.OwnerName, r.HomeTeam, r.TeamLevel, share, r.Title, now, now); err != nil {
		return r, err
	}
	for _, m := range people {
		if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_people (kind, id, email, role, granted_by, granted_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (kind, id, email) DO NOTHING`, ref.Kind, ref.ID, normEmail(m.Email), m.Role, m.GrantedBy, ms(m.GrantedAt), nullTime(m.Expires)); err != nil {
			return r, err
		}
	}
	for _, g := range grants {
		c := 0
		if g.CanShare {
			c = 1
		}
		if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_teams (kind, id, team_id, role, can_share, granted_by, granted_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (kind, id, team_id) DO NOTHING`, ref.Kind, ref.ID, g.Team, g.Role, c, g.GrantedBy, ms(g.GrantedAt), nullTime(g.Expires)); err != nil {
			return r, err
		}
	}
	r.Stored = true
	return r, nil
}

func (t *txn) touch(ref Ref) error {
	_, err := t.q.Exec(t.ctx, `UPDATE resource_access SET updated_at = ? WHERE kind = ? AND id = ?`, ms(t.now), ref.Kind, ref.ID)
	return err
}

func (t *txn) setTeam(ref Ref, actor, level string) error {
	if !teamLevels[level] {
		return fail("bad_request", "Уровень команды: none, view, comment или edit")
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access SET team_level = ?, updated_at = ? WHERE kind = ? AND id = ?`, level, ms(t.now), ref.Kind, ref.ID); err != nil {
		return err
	}
	return t.log(ref, actor, "team_level", level, nil)
}

func (t *txn) setEditorsCanShare(ref Ref, actor string, v bool) error {
	n := 0
	if v {
		n = 1
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access SET editors_can_share = ?, updated_at = ? WHERE kind = ? AND id = ?`, n, ms(t.now), ref.Kind, ref.ID); err != nil {
		return err
	}
	return t.log(ref, actor, "editors_can_share", map[bool]string{true: "on", false: "off"}[v], nil)
}

func (t *txn) memberRole(ref Ref, email string) (string, error) {
	var role string
	err := t.q.QueryRow(t.ctx, `SELECT role FROM resource_access_people WHERE kind = ? AND id = ? AND email = ?`, ref.Kind, ref.ID, email).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// guard stops a sharer (not a manager) from touching a manager's grant.
func (t *txn) guard(ref Ref, who string, protect bool) error {
	if !protect {
		return nil
	}
	role, err := t.memberRole(ref, who)
	if err != nil {
		return err
	}
	if role == roleManage {
		return fail("forbidden", "Управляющего меняет только владелец или другой управляющий")
	}
	return nil
}

func (t *txn) grant(ref Ref, actor, email, role string, term ttl, protect bool) error {
	email = normEmail(email)
	if !emailRe.MatchString(email) {
		return fail("bad_request", "Некорректный адрес почты")
	}
	if !grantRoles[role] {
		return fail("bad_request", "Роль: manage, edit, comment или view")
	}
	var owner string
	_ = t.q.QueryRow(t.ctx, `SELECT owner_email FROM resource_access WHERE kind = ? AND id = ?`, ref.Kind, ref.ID).Scan(&owner)
	if owner != "" && owner == email {
		return fail("conflict", "Это владелец: ему не нужна отдельная роль")
	}
	if err := t.guard(ref, email, protect); err != nil {
		return err
	}
	exp, err := expiry(term, t.now)
	if err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_people (kind, id, email, role, granted_by, granted_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (kind, id, email) DO UPDATE SET role = excluded.role, granted_by = excluded.granted_by, granted_at = excluded.granted_at, expires_at = excluded.expires_at`,
		ref.Kind, ref.ID, email, role, actor, ms(t.now), nullTime(exp)); err != nil {
		return err
	}
	if err := t.touch(ref); err != nil {
		return err
	}
	return t.log(ref, actor, "grant", email, map[string]any{"role": role, "expires": iso(exp)})
}

func (t *txn) revoke(ref Ref, actor, email string, protect bool) error {
	email = normEmail(email)
	if err := t.guard(ref, email, protect); err != nil {
		return err
	}
	res, err := t.q.Exec(t.ctx, `DELETE FROM resource_access_people WHERE kind = ? AND id = ? AND email = ?`, ref.Kind, ref.ID, email)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fail("not_found", "У этого человека нет отдельной роли")
	}
	if err := t.touch(ref); err != nil {
		return err
	}
	return t.log(ref, actor, "revoke", email, nil)
}

func (t *txn) grantTeam(ref Ref, actor, team, role string, canShare bool, term ttl) error {
	if _, ok := t.w.team(team); !ok {
		return fail("not_found", "Команда не найдена")
	}
	if role != roleEdit && role != roleComment && role != roleView {
		return fail("bad_request", "Роль команды: view, comment или edit")
	}
	if canShare && role != roleEdit {
		return fail("bad_request", "Делиться дальше может только команда с ролью edit")
	}
	exp, err := expiry(term, t.now)
	if err != nil {
		return err
	}
	c := 0
	if canShare {
		c = 1
	}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_teams (kind, id, team_id, role, can_share, granted_by, granted_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (kind, id, team_id) DO UPDATE SET role = excluded.role, can_share = excluded.can_share, granted_by = excluded.granted_by,
		granted_at = excluded.granted_at, expires_at = excluded.expires_at`, ref.Kind, ref.ID, team, role, c, actor, ms(t.now), nullTime(exp)); err != nil {
		return err
	}
	if err := t.touch(ref); err != nil {
		return err
	}
	return t.log(ref, actor, "grant_team", team, map[string]any{"role": role, "canShare": canShare, "expires": iso(exp)})
}

func (t *txn) revokeTeam(ref Ref, actor, team string) error {
	res, err := t.q.Exec(t.ctx, `DELETE FROM resource_access_teams WHERE kind = ? AND id = ? AND team_id = ?`, ref.Kind, ref.ID, team)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fail("not_found", "У этой команды нет отдельной роли")
	}
	if err := t.touch(ref); err != nil {
		return err
	}
	return t.log(ref, actor, "revoke_team", team, nil)
}

func (t *txn) transfer(ref Ref, actor, email string) error {
	email = normEmail(email)
	if !emailRe.MatchString(email) {
		return fail("bad_request", "Некорректный адрес почты")
	}
	u, err := t.st.UserByEmail(t.ctx, email)
	if err != nil {
		return fail("bad_request", "Передать можно человеку, который уже входил в хаб")
	}
	if _, err := t.q.Exec(t.ctx, `DELETE FROM resource_access_people WHERE kind = ? AND id = ? AND email = ?`, ref.Kind, ref.ID, email); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access SET owner_email = ?, owner_name = ?, updated_at = ? WHERE kind = ? AND id = ?`, email, u.Name, ms(t.now), ref.Kind, ref.ID); err != nil {
		return err
	}
	if ref.Kind == KindDoc {
		if _, err := t.q.Exec(t.ctx, `UPDATE doc_meta SET owner_email = ?, owner_name = ?, updated_at = ? WHERE slug = ?`, email, u.Name, ms(t.now), ref.ID); err != nil {
			return err
		}
	}
	return t.log(ref, actor, "transfer", email, nil)
}

func (t *txn) revokeAll(ref Ref, actor string) error {
	for _, tbl := range []string{"resource_access_people", "resource_access_teams"} {
		if _, err := t.q.Exec(t.ctx, `DELETE FROM `+tbl+` WHERE kind = ? AND id = ?`, ref.Kind, ref.ID); err != nil {
			return err
		}
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access SET team_level = ?, updated_at = ? WHERE kind = ? AND id = ?`, levelNone, ms(t.now), ref.Kind, ref.ID); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_guest_links SET revoked_at = ? WHERE kind = ? AND id = ? AND revoked_at IS NULL`, ms(t.now), ref.Kind, ref.ID); err != nil {
		return err
	}
	return t.log(ref, actor, "revoke_all", "", nil)
}

// ---- requests ---------------------------------------------------------------------------------------------------------

type requestRow struct {
	ID, Email, Level, Message, Status, DecidedBy string
	DecidedAt                                    *time.Time
	CreatedAt                                    time.Time
}

func (r requestRow) json() map[string]any {
	return map[string]any{"id": r.ID, "email": r.Email, "level": r.Level, "message": r.Message, "status": r.Status,
		"decidedBy": nullStr(r.DecidedBy), "decidedAt": iso(r.DecidedAt), "createdAt": isoOf(r.CreatedAt)}
}

func (t *txn) requests(ref Ref, who, status string) ([]requestRow, error) {
	q := `SELECT request_id, email, level, message, status, decided_by, decided_at, created_at FROM resource_access_requests WHERE kind = ? AND id = ? AND status = ?`
	args := []any{ref.Kind, ref.ID, status}
	if who != "" {
		q += ` AND email = ?`
		args = append(args, who)
	}
	rows, err := t.q.Query(t.ctx, q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []requestRow
	for rows.Next() {
		var r requestRow
		var at sql.NullInt64
		var created int64
		if err := rows.Scan(&r.ID, &r.Email, &r.Level, &r.Message, &r.Status, &r.DecidedBy, &at, &created); err != nil {
			return nil, err
		}
		if at.Valid {
			d := fromMs(at.Int64)
			r.DecidedAt = &d
		}
		r.CreatedAt = fromMs(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// request: one open request per person; a denial closes the door for a day.
func (t *txn) request(ref Ref, email, level, message string) (requestRow, error) {
	email = normEmail(email)
	if !requestLevels[level] {
		return requestRow{}, fail("bad_request", "Уровень запроса: comment или edit")
	}
	open, err := t.requests(ref, email, "open")
	if err != nil {
		return requestRow{}, err
	}
	if len(open) > 0 {
		return open[0], nil
	}
	var denied sql.NullInt64
	err = t.q.QueryRow(t.ctx, `SELECT MAX(decided_at) FROM resource_access_requests WHERE kind = ? AND id = ? AND email = ? AND status = 'denied'`,
		ref.Kind, ref.ID, email).Scan(&denied)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return requestRow{}, err
	}
	if denied.Valid && fromMs(denied.Int64).Add(denyCooldown).After(t.now) {
		return requestRow{}, fail("rate_limited", "Запрос уже отклонили: новый можно отправить через сутки")
	}
	r := requestRow{ID: newID(), Email: email, Level: level, Message: clip(message, maxMessage), Status: "open", CreatedAt: t.now}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_requests (request_id, kind, id, email, level, message, status, created_at) VALUES (?, ?, ?, ?, ?, ?, 'open', ?)`,
		r.ID, ref.Kind, ref.ID, email, level, r.Message, ms(t.now)); err != nil {
		return requestRow{}, err
	}
	return r, t.log(ref, email, "request", level, nil)
}

func (t *txn) decide(ref Ref, id, actor, decision, role string, term ttl, protect bool) (requestRow, error) {
	var r requestRow
	var created int64
	err := t.q.QueryRow(t.ctx, `SELECT request_id, email, level, message, status, created_at FROM resource_access_requests WHERE request_id = ? AND kind = ? AND id = ?`+t.forUpdate(),
		id, ref.Kind, ref.ID).Scan(&r.ID, &r.Email, &r.Level, &r.Message, &r.Status, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return r, fail("not_found", "Запрос не найден")
	}
	if err != nil {
		return r, err
	}
	if r.Status != "open" {
		return r, fail("conflict", "Запрос уже решён")
	}
	status := "denied"
	if decision == "allow" {
		status = "allowed"
		if role == "" {
			role = r.Level
		}
		if err := t.grant(ref, actor, r.Email, role, term, protect); err != nil {
			return r, err
		}
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access_requests SET status = ?, decided_by = ?, decided_at = ? WHERE request_id = ?`, status, actor, ms(t.now), id); err != nil {
		return r, err
	}
	r.Status, r.DecidedBy = status, actor
	d := t.now
	r.DecidedAt, r.CreatedAt = &d, fromMs(created)
	return r, t.log(ref, actor, "decide", r.Email, map[string]any{"decision": decision})
}

// ---- log ----------------------------------------------------------------------------------------------------------------

func (t *txn) accessLog(ref Ref) ([]map[string]any, error) {
	rows, err := t.q.Query(t.ctx, `SELECT actor, action, subject, details, created_at FROM resource_access_log WHERE kind = ? AND id = ? ORDER BY created_at DESC, log_id DESC LIMIT ?`,
		ref.Kind, ref.ID, logLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for i := 1; rows.Next(); i++ {
		var actor, action, subject, details string
		var at int64
		if err := rows.Scan(&actor, &action, &subject, &details, &at); err != nil {
			return nil, err
		}
		d := map[string]any{}
		if details != "" {
			_ = json.Unmarshal([]byte(details), &d)
		}
		out = append(out, map[string]any{"id": i, "actor": actor, "action": action, "subject": subject, "details": d, "at": isoOf(fromMs(at))})
	}
	return out, rows.Err()
}

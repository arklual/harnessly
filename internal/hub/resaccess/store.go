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
	q := `SELECT owner_email, owner_name, home_team, team_level, editors_can_share, title FROM resource_access WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`
	if lock {
		q += t.forUpdate()
	}
	r := resource{Ref: ref, Stored: true}
	var share int
	err := t.q.QueryRow(t.ctx, q, ref.Kind, ref.ID, ref.Space).Scan(&r.Owner, &r.OwnerName, &r.HomeTeam, &r.TeamLevel, &share, &r.Title)
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
	case KindKit:
		r.TeamLevel = levelNone // a kit nobody recorded is closed to everyone: the first machine to put it in the room claims it
	case KindDoc:
		var level string
		err := t.q.QueryRow(t.ctx, `SELECT owner_email, owner_name, team_level, title FROM doc_meta WHERE slug = ? AND COALESCE(space_id, '') = ?`, ref.ID, ref.Space).Scan(&r.Owner, &r.OwnerName, &level, &r.Title)
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
	rows, err := t.q.Query(t.ctx, `SELECT email, COALESCE(user_id, ''), role, granted_by, granted_at, expires_at FROM resource_access_people
		WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? ORDER BY granted_at, email`, ref.Kind, ref.ID, ref.Space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []member
	for rows.Next() {
		var m member
		var at int64
		var exp sql.NullInt64
		if err := rows.Scan(&m.Email, &m.UserID, &m.Role, &m.GrantedBy, &at, &exp); err != nil {
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
		WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? ORDER BY granted_at, team_id`, ref.Kind, ref.ID, ref.Space)
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
func (t *txn) foldFor(r resource, people []member, grants []teamGrant, a Actor) (folded, error) {
	email := normEmail(a.Email)
	if a.Outside && r.HomeTeam == "" {
		r.TeamLevel = levelNone
	}
	person, err := t.w.person(email)
	if err != nil {
		return folded{}, err
	}
	return fold(r, own(people, a, t.now), grants, person, t.w.homeOrg(r.HomeTeam), t.now), nil
}

// reaches reports whether a row of a resource's people is the person's: the role was given to their account, or it is
// an invitation to an address they have proved. An address that merely matches the one an account signed in under
// gives nothing.
func reaches(m member, a Actor) bool {
	if m.UserID != "" {
		return m.UserID == a.UserID
	}
	return a.proved(m.Email)
}

// own is the strongest live role the person holds by name.
func own(people []member, a Actor, now time.Time) *member {
	var mine *member
	for i := range people {
		m := &people[i]
		if !reaches(*m, a) || !live(m.Expires, now) {
			continue
		}
		if mine == nil || roleRank[m.Role] > roleRank[mine.Role] {
			mine = m
		}
	}
	return mine
}

// accept makes the invitations that reach the person theirs: from then on the role is held by the account, whatever
// address it signs in under. It never fails the request.
func (s *Service) accept(ctx context.Context, ref Ref, people []member, a Actor) {
	if a.UserID == "" {
		return
	}
	for _, m := range people {
		if m.UserID != "" || !a.proved(m.Email) {
			continue
		}
		_ = s.write(ctx, func(t *txn) error {
			res, err := t.q.Exec(t.ctx, `UPDATE resource_access_people SET user_id = ? WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND email = ? AND user_id IS NULL`,
				a.UserID, ref.Kind, ref.ID, ref.Space, m.Email)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil
			}
			return t.log(ref, m.Email, "accept", m.Email, nil)
		})
	}
}

// heldBy is the account that holds the role given to the address in this resource; "" for none or an invitation.
func (t *txn) heldBy(ref Ref, email string) (string, error) {
	id, _, err := t.rowOf(ref, email)
	return id, err
}

// rowOf is heldBy that also says whether the address has a row at all: one without an account is an invitation.
func (t *txn) rowOf(ref Ref, email string) (id string, exists bool, err error) {
	err = t.q.QueryRow(t.ctx, `SELECT COALESCE(user_id, '') FROM resource_access_people WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND email = ?`,
		ref.Kind, ref.ID, ref.Space, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

// holder is the one person who has proved the address, or "" when a role given to it waits as an invitation.
func (t *txn) holder(email string) (string, error) {
	return t.st.UserIDByVerifiedEmail(t.ctx, email)
}

// ---- writing --------------------------------------------------------------------------------------------------------

func (t *txn) log(ref Ref, actor, action, subject string, details map[string]any) error {
	d := ""
	if len(details) > 0 {
		b, _ := json.Marshal(details)
		d = string(b)
	}
	_, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_log (log_id, kind, id, space_id, actor, action, subject, details, created_at) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)`,
		newID(), ref.Kind, ref.ID, ref.Space, actor, action, subject, d, ms(t.now))
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
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access (kind, id, space_id, owner_email, owner_name, home_team, team_level, editors_can_share, title, created_at, updated_at)
		VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (kind, id) DO NOTHING`,
		ref.Kind, ref.ID, ref.Space, r.Owner, r.OwnerName, r.HomeTeam, r.TeamLevel, share, r.Title, now, now); err != nil {
		return r, err
	}
	for _, m := range people {
		uid, err := t.holder(normEmail(m.Email))
		if err != nil {
			return r, err
		}
		if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_people (kind, id, space_id, email, user_id, role, granted_by, granted_at, expires_at) VALUES (?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?, ?)
			ON CONFLICT (kind, id, email) DO NOTHING`, ref.Kind, ref.ID, ref.Space, normEmail(m.Email), uid, m.Role, m.GrantedBy, ms(m.GrantedAt), nullTime(m.Expires)); err != nil {
			return r, err
		}
	}
	for _, g := range grants {
		c := 0
		if g.CanShare {
			c = 1
		}
		if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_teams (kind, id, space_id, team_id, role, can_share, granted_by, granted_at, expires_at) VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)
			ON CONFLICT (kind, id, team_id) DO NOTHING`, ref.Kind, ref.ID, ref.Space, g.Team, g.Role, c, g.GrantedBy, ms(g.GrantedAt), nullTime(g.Expires)); err != nil {
			return r, err
		}
	}
	r.Stored = true
	return r, nil
}

func (t *txn) touch(ref Ref) error {
	_, err := t.q.Exec(t.ctx, `UPDATE resource_access SET updated_at = ? WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, ms(t.now), ref.Kind, ref.ID, ref.Space)
	return err
}

func (t *txn) setTeam(ref Ref, actor, level string) error {
	if !teamLevels[level] {
		return fail("bad_request", "Уровень команды: none, view, comment или edit")
	}
	if err := kitTeamLevel(ref, level); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access SET team_level = ?, updated_at = ? WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, level, ms(t.now), ref.Kind, ref.ID, ref.Space); err != nil {
		return err
	}
	return t.log(ref, actor, "team_level", level, nil)
}

func (t *txn) setEditorsCanShare(ref Ref, actor string, v bool) error {
	n := 0
	if v {
		n = 1
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access SET editors_can_share = ?, updated_at = ? WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, n, ms(t.now), ref.Kind, ref.ID, ref.Space); err != nil {
		return err
	}
	return t.log(ref, actor, "editors_can_share", map[bool]string{true: "on", false: "off"}[v], nil)
}

func (t *txn) memberRole(ref Ref, email string) (string, error) {
	var role string
	err := t.q.QueryRow(t.ctx, `SELECT role FROM resource_access_people WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND email = ?`, ref.Kind, ref.ID, ref.Space, email).Scan(&role)
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

// grant gives a role to an address: to the person who has proved it, or, while nobody has, as an invitation that
// waits for the person who will.
func (t *txn) grant(ref Ref, actor, email, role string, term ttl, protect bool) error {
	email = normEmail(email)
	if !emailRe.MatchString(email) {
		return fail("bad_request", "Некорректный адрес почты")
	}
	uid, err := t.holder(email)
	if err != nil {
		return err
	}
	return t.grantTo(ref, actor, email, uid, role, term, protect)
}

// grantTo gives a role to the person userID, known by the address email; an empty userID leaves an invitation.
func (t *txn) grantTo(ref Ref, actor, email, userID, role string, term ttl, protect bool) error {
	if !grantRoles[role] {
		return fail("bad_request", "Роль: manage, edit, comment или view")
	}
	if err := kitPersonRole(ref, role); err != nil {
		return err
	}
	var owner string
	_ = t.q.QueryRow(t.ctx, `SELECT owner_email FROM resource_access WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, ref.Kind, ref.ID, ref.Space).Scan(&owner)
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
	// A role one person holds under the address is never handed to another account.
	held, exists, err := t.rowOf(ref, email)
	if err != nil {
		return err
	}
	if held != "" && userID != "" && held != userID {
		return fail("conflict", "Роль на этот адрес уже держит другой человек")
	}
	if userID != "" {
		proven, err := t.holder(email)
		if err != nil {
			return err
		}
		// An address somebody has proved stands for that person only: another account that signs in under it is not
		// given a role in its name.
		if proven != "" && proven != userID {
			return fail("conflict", "Этот адрес подтверждён другим человеком")
		}
		// An invitation waits for the person who proves the address: an account that merely signs in under it does
		// not take it.
		if exists && held == "" && proven != userID {
			return fail("conflict", "На этот адрес уже есть приглашение: оно сработает, когда адрес будет подтверждён")
		}
	}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_people (kind, id, space_id, email, user_id, role, granted_by, granted_at, expires_at) VALUES (?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?, ?)
		ON CONFLICT (kind, id, email) DO UPDATE SET role = excluded.role, granted_by = excluded.granted_by, granted_at = excluded.granted_at, expires_at = excluded.expires_at,
			user_id = COALESCE(resource_access_people.user_id, excluded.user_id)
		WHERE COALESCE(resource_access_people.space_id, '') = COALESCE(excluded.space_id, '')`,
		ref.Kind, ref.ID, ref.Space, email, userID, role, actor, ms(t.now), nullTime(exp)); err != nil {
		return err
	}
	if err := t.touch(ref); err != nil {
		return err
	}
	return t.log(ref, actor, "grant", email, map[string]any{"role": role, "expires": iso(exp), "pending": userID == ""})
}

func (t *txn) revoke(ref Ref, actor, email string, protect bool) error {
	email = normEmail(email)
	if err := t.guard(ref, email, protect); err != nil {
		return err
	}
	res, err := t.q.Exec(t.ctx, `DELETE FROM resource_access_people WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND email = ?`, ref.Kind, ref.ID, ref.Space, email)
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
	if err := kitGroupRole(ref, role, canShare); err != nil {
		return err
	}
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
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_teams (kind, id, space_id, team_id, role, can_share, granted_by, granted_at, expires_at) VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)
		ON CONFLICT (kind, id, team_id) DO UPDATE SET role = excluded.role, can_share = excluded.can_share, granted_by = excluded.granted_by,
		granted_at = excluded.granted_at, expires_at = excluded.expires_at
		WHERE COALESCE(resource_access_teams.space_id, '') = COALESCE(excluded.space_id, '')`, ref.Kind, ref.ID, ref.Space, team, role, c, actor, ms(t.now), nullTime(exp)); err != nil {
		return err
	}
	if err := t.touch(ref); err != nil {
		return err
	}
	return t.log(ref, actor, "grant_team", team, map[string]any{"role": role, "canShare": canShare, "expires": iso(exp)})
}

func (t *txn) revokeTeam(ref Ref, actor, team string) error {
	res, err := t.q.Exec(t.ctx, `DELETE FROM resource_access_teams WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND team_id = ?`, ref.Kind, ref.ID, ref.Space, team)
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
	if _, err := t.q.Exec(t.ctx, `DELETE FROM resource_access_people WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND email = ?`, ref.Kind, ref.ID, ref.Space, email); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access SET owner_email = ?, owner_name = ?, updated_at = ? WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, email, u.Name, ms(t.now), ref.Kind, ref.ID, ref.Space); err != nil {
		return err
	}
	if ref.Kind == KindDoc {
		if _, err := t.q.Exec(t.ctx, `UPDATE doc_meta SET owner_email = ?, owner_name = ?, updated_at = ? WHERE slug = ? AND COALESCE(space_id, '') = ?`, email, u.Name, ms(t.now), ref.ID, ref.Space); err != nil {
			return err
		}
	}
	return t.log(ref, actor, "transfer", email, nil)
}

func (t *txn) revokeAll(ref Ref, actor string) error {
	for _, tbl := range []string{"resource_access_people", "resource_access_teams"} {
		if _, err := t.q.Exec(t.ctx, `DELETE FROM `+tbl+` WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, ref.Kind, ref.ID, ref.Space); err != nil {
			return err
		}
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access SET team_level = ?, updated_at = ? WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, levelNone, ms(t.now), ref.Kind, ref.ID, ref.Space); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_guest_links SET revoked_at = ? WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND revoked_at IS NULL`, ms(t.now), ref.Kind, ref.ID, ref.Space); err != nil {
		return err
	}
	return t.log(ref, actor, "revoke_all", "", nil)
}

// ---- requests ---------------------------------------------------------------------------------------------------------

type requestRow struct {
	ID, Email, UserID, Level, Message, Status, DecidedBy string
	DecidedAt                                            *time.Time
	CreatedAt                                            time.Time
}

func (r requestRow) json() map[string]any {
	return map[string]any{"id": r.ID, "email": r.Email, "level": r.Level, "message": r.Message, "status": r.Status,
		"decidedBy": nullStr(r.DecidedBy), "decidedAt": iso(r.DecidedAt), "createdAt": isoOf(r.CreatedAt)}
}

func (t *txn) requests(ref Ref, who, status string) ([]requestRow, error) {
	q := `SELECT request_id, email, COALESCE(user_id, ''), level, message, status, decided_by, decided_at, created_at FROM resource_access_requests WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND status = ?`
	args := []any{ref.Kind, ref.ID, ref.Space, status}
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
		if err := rows.Scan(&r.ID, &r.Email, &r.UserID, &r.Level, &r.Message, &r.Status, &r.DecidedBy, &at, &created); err != nil {
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
func (t *txn) request(ref Ref, email, userID, level, message string) (requestRow, error) {
	email = normEmail(email)
	if !requestLevels[level] && !(ref.Kind == KindKit && level == roleView) {
		return requestRow{}, fail("bad_request", "Уровень запроса: comment или edit")
	}
	if err := kitPersonRole(ref, level); err != nil {
		return requestRow{}, err
	}
	open, err := t.requests(ref, email, "open")
	if err != nil {
		return requestRow{}, err
	}
	if len(open) > 0 {
		return open[0], nil
	}
	var denied sql.NullInt64
	err = t.q.QueryRow(t.ctx, `SELECT MAX(decided_at) FROM resource_access_requests WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND email = ? AND status = 'denied'`,
		ref.Kind, ref.ID, ref.Space, email).Scan(&denied)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return requestRow{}, err
	}
	if denied.Valid && fromMs(denied.Int64).Add(denyCooldown).After(t.now) {
		return requestRow{}, fail("rate_limited", "Запрос уже отклонили: новый можно отправить через сутки")
	}
	r := requestRow{ID: newID(), Email: email, UserID: userID, Level: level, Message: clip(message, maxMessage), Status: "open", CreatedAt: t.now}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access_requests (request_id, kind, id, space_id, email, user_id, level, message, status, created_at) VALUES (?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, 'open', ?)`,
		r.ID, ref.Kind, ref.ID, ref.Space, email, userID, level, r.Message, ms(t.now)); err != nil {
		return requestRow{}, err
	}
	return r, t.log(ref, email, "request", level, nil)
}

func (t *txn) decide(ref Ref, id, actor, decision, role string, term ttl, protect bool) (requestRow, error) {
	var r requestRow
	var created int64
	err := t.q.QueryRow(t.ctx, `SELECT request_id, email, COALESCE(user_id, ''), level, message, status, created_at FROM resource_access_requests WHERE request_id = ? AND kind = ? AND id = ? AND COALESCE(space_id, '') = ?`+t.forUpdate(),
		id, ref.Kind, ref.ID, ref.Space).Scan(&r.ID, &r.Email, &r.UserID, &r.Level, &r.Message, &r.Status, &created)
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
		// The role goes to the account that asked, whatever address it signed in under.
		if r.UserID != "" {
			err = t.grantTo(ref, actor, r.Email, r.UserID, role, term, protect)
		} else {
			err = t.grant(ref, actor, r.Email, role, term, protect)
		}
		if err != nil {
			return r, err
		}
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_access_requests SET status = ?, decided_by = ?, decided_at = ? WHERE request_id = ? AND COALESCE(space_id, '') = ?`, status, actor, ms(t.now), id, ref.Space); err != nil {
		return r, err
	}
	r.Status, r.DecidedBy = status, actor
	d := t.now
	r.DecidedAt, r.CreatedAt = &d, fromMs(created)
	return r, t.log(ref, actor, "decide", r.Email, map[string]any{"decision": decision})
}

// ---- log ----------------------------------------------------------------------------------------------------------------

func (t *txn) accessLog(ref Ref) ([]map[string]any, error) {
	rows, err := t.q.Query(t.ctx, `SELECT actor, action, subject, details, created_at FROM resource_access_log WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? ORDER BY created_at DESC, log_id DESC LIMIT ?`,
		ref.Kind, ref.ID, ref.Space, logLimit)
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

// AdoptSpace gives the rows of a resource that name no space the space the resource is now known to be in. It runs in
// the caller's transaction.
func AdoptSpace(ctx context.Context, q store.Q, ref Ref) error {
	for _, tbl := range []string{"resource_access", "resource_access_people", "resource_access_teams", "resource_access_requests",
		"resource_access_log", "resource_guest_links"} {
		if _, err := q.Exec(ctx, `UPDATE `+tbl+` SET space_id = ? WHERE kind = ? AND id = ? AND space_id IS NULL`, ref.Space, ref.Kind, ref.ID); err != nil {
			return err
		}
	}
	return nil
}

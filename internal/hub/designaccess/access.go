package designaccess

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The one access API of a resource, <platform api>/access/{kind}/{id} (and /access/me, /access/directory): what the
// newer «Поделиться» dialog speaks. The same records and rules as the older routes; the hub answers the kind design.

const (
	deniedManageAny = "Доступом управляют владелец и те, кому он это разрешил"
	deniedOwnerAny  = "Это может сделать только владелец"
	deniedShare     = "Право делиться дальше даёт владелец или тот, кто сам им обладает"
	deniedLink      = "Это чужая ссылка: её меняет тот, кто её сделал, или владелец"
	linkOff         = "Ссылка отключена: сначала включите её"
	ttlMessage      = "Срок: 1h, 1d, 7d, 30d, дата со смещением или null"
	roleMessage     = "Роль: view, comment или edit"
	badEmail        = "Некорректный адрес почты"
	maxForWhom      = 200
	directoryLimit  = 8
	maxQuery        = 60
	noticeTimeout   = 30 * time.Second
)

// Notice mails one person about their access; nil leaves the hub silent.
type Notice func(ctx context.Context, to, subject, body, link string) error

func shareRole(v any) (string, error) {
	r, _ := v.(string)
	if r != roleView && r != roleComment && r != roleEdit {
		return "", fail("bad_request", roleMessage)
	}
	return r, nil
}

// storedRole: «edit and may share» is kept as manage.
func storedRole(role string, canShare bool) string {
	if role == roleEdit && canShare {
		return roleManage
	}
	return role
}

func shownRole(role string) any {
	switch role {
	case "":
		return nil
	case roleManage:
		return roleEdit
	}
	return role
}

func boolField(b map[string]any, field string) (bool, error) {
	v, ok := b[field]
	if !ok || v == nil {
		return false, nil
	}
	x, ok := v.(bool)
	if !ok {
		return false, fail("bad_request", field+" — true или false")
	}
	return x, nil
}

// termField: no term given is forever, as the dialog sends it.
func termField(b map[string]any) (ttl, error) {
	v, ok := b["ttl"]
	if !ok {
		return ttl{none: true}, nil
	}
	return ttlOf(v, ttlMessage)
}

func (s *Service) serveAccess(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, rest string) {
	var parts []string
	for _, raw := range strings.Split(strings.Trim(rest, "/"), "/") {
		if raw == "" {
			continue
		}
		p, err := url.PathUnescape(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Некорректный адрес", "code": "bad_request"})
			return
		}
		parts = append(parts, p)
	}
	res, status, err := s.accessRoute(r, u, m, parts)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, status, res)
}

func (s *Service) accessRoute(r *http.Request, u store.User, m store.Machine, parts []string) (any, int, error) {
	ctx := r.Context()
	email := normEmail(u.Email)
	n := len(parts)
	if r.Method == http.MethodGet && n == 1 && parts[0] == "me" {
		v, err := s.myTeams(ctx, m, email)
		return v, 200, err
	}
	if r.Method == http.MethodGet && n == 1 && parts[0] == "directory" {
		v, err := s.directory(ctx, m, email, r.URL.Query().Get("q"))
		return v, 200, err
	}
	if n < 2 || parts[0] != "design" {
		return nil, 0, fail("not_found", "Маршрут не найден")
	}
	project := parts[1]
	if !projectIDRe.MatchString(project) {
		return nil, 0, fail("not_found", "Проект не найден")
	}
	return s.resourceRoute(r, m, email, project, parts[2:])
}

// ---- the resource -------------------------------------------------------------------------------------------------

func (s *Service) viewOf(ctx context.Context, m store.Machine, email, project string) (map[string]any, error) {
	t, err := s.read(ctx, m)
	if err != nil {
		return nil, err
	}
	p, stored, err := t.effective(project)
	if err != nil {
		return nil, err
	}
	return s.viewIn(t, p, stored, email)
}

func (s *Service) viewIn(t *txn, p project, stored bool, email string) (map[string]any, error) {
	f, err := t.fold(p, email)
	if err != nil {
		return nil, err
	}
	manager := f.Role == roleOwner || f.Role == roleManage || f.Steward
	out := map[string]any{
		"resource": map[string]any{"kind": "design", "id": p.ID, "subkind": "design", "title": p.ID},
		"stored":   stored, "role": shownRole(f.Role), "until": iso(f.Until), "canManage": f.CanManage, "manager": manager,
		"steward": f.Steward, "owner": nil, "homeTeam": nil, "editorsCanShare": p.EditorsCanShare, "myRequest": nil,
	}
	if p.Owner != "" {
		var known any
		if f.Role != "" || f.CanManage {
			known = p.Owner
		}
		name := p.OwnerName
		if name == "" {
			name = t.w.displayName(p.Owner)
		}
		out["owner"] = map[string]any{"email": known, "name": name}
	}
	if p.HomeTeam != "" {
		name := p.HomeTeam
		if ref, ok := t.w.team(p.HomeTeam); ok {
			name = ref.Name
		}
		var level any
		if p.TeamLevel != levelNone {
			level = p.TeamLevel
		}
		out["homeTeam"] = map[string]any{"teamId": p.HomeTeam, "name": name, "role": level}
	}
	if !stored {
		return out, nil
	}
	mine, err := t.requests(p.ID, normEmail(email), "open")
	if err != nil {
		return nil, err
	}
	if len(mine) > 0 {
		out["myRequest"] = map[string]any{"id": mine[0].ID, "level": mine[0].Level, "status": mine[0].Status}
	}
	if !f.CanManage {
		return out, nil
	}
	grants := []map[string]any{}
	teams, err := t.teamGrants(p.ID)
	if err != nil {
		return nil, err
	}
	for _, g := range teams {
		if !live(g.Expires, t.now) {
			continue
		}
		name, sub := g.Team, ""
		if ref, ok := t.w.team(g.Team); ok {
			name, sub = ref.Name, ref.OrgName+" · "+strconv.Itoa(ref.Members)
		}
		grants = append(grants, map[string]any{"type": "team", "id": g.Team, "name": name, "sub": sub, "role": g.Role,
			"canShare": g.CanShare, "expiresAt": iso(g.Expires)})
	}
	people, err := t.members(p.ID)
	if err != nil {
		return nil, err
	}
	for _, mb := range people {
		if !live(mb.Expires, t.now) {
			continue
		}
		grants = append(grants, map[string]any{"type": "user", "id": mb.Email, "name": t.w.displayName(mb.Email), "sub": mb.Email,
			"role": shownRole(mb.Role), "canShare": mb.Role == roleManage, "expiresAt": iso(mb.Expires)})
	}
	open, err := t.requests(p.ID, "", "open")
	if err != nil {
		return nil, err
	}
	reqs := []map[string]any{}
	for _, rq := range open {
		reqs = append(reqs, map[string]any{"id": rq.ID, "email": rq.Email, "name": t.w.displayName(rq.Email), "level": rq.Level,
			"message": rq.Message, "createdAt": isoOf(rq.CreatedAt)})
	}
	out["grants"], out["requests"] = grants, reqs
	if s.shareURL != "" {
		rows, err := t.shareRows(p.ID)
		if err != nil {
			return nil, err
		}
		links := []map[string]any{}
		for _, row := range rows {
			links = append(links, s.linkJSON(row))
		}
		out["links"] = links
	}
	return out, nil
}

func (s *Service) linkJSON(r shareRow) map[string]any {
	options := map[string]any{}
	if r.View != nil {
		options = r.View
	}
	var pages any
	if r.Pages != nil {
		pages = r.Pages
	}
	return map[string]any{"id": r.ID, "forWhom": r.Title, "rights": r.Rights, "expiresAt": iso(r.Expires), "disabledAt": iso(r.Revoked),
		"opens": r.Opens, "lastOpenedAt": iso(r.LastOpened), "createdAt": isoOf(r.CreatedAt), "createdBy": r.CreatedBy,
		"comments": r.Comments, "options": options, "version": r.Version, "pages": pages, "copyable": r.Sealed && s.seal != nil}
}

func (s *Service) resourceRoute(r *http.Request, m store.Machine, email, project string, rest []string) (any, int, error) {
	ctx := r.Context()
	method, n := r.Method, len(rest)
	section, third := part(rest, 0), part(rest, 1)

	t, err := s.read(ctx, m)
	if err != nil {
		return nil, 0, err
	}
	p, stored, err := t.effective(project)
	if err != nil {
		return nil, 0, err
	}
	if method == http.MethodGet && n == 0 {
		v, err := s.viewIn(t, p, stored, email)
		return v, 200, err
	}
	if !stored {
		return nil, 0, fail("not_found", "Проект не найден")
	}
	f, err := t.fold(p, email)
	if err != nil {
		return nil, 0, err
	}
	role := f.Role
	manager := role == roleOwner || role == roleManage || f.Steward
	fresh := func() (any, int, error) {
		v, err := s.viewOf(ctx, m, email, project)
		return v, 200, err
	}
	change := func(fn func(t *txn) error) (any, int, error) {
		if err := s.write(ctx, m, fn); err != nil {
			return nil, 0, err
		}
		return fresh()
	}

	switch {
	case method == http.MethodPost && section == "requests" && n == 1:
		if p.Owner == "" {
			return nil, 0, fail("conflict", "У объекта нет владельца, просить доступ не у кого")
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		level := roleView
		if l, _ := b["level"].(string); l == roleEdit || l == roleComment {
			level = l
		}
		var made requestRow
		if err := s.write(ctx, m, func(t *txn) error {
			var err error
			made, err = t.request(project, email, level, stringOr(b["message"]))
			return err
		}); err != nil {
			return nil, 0, err
		}
		s.noticeRequest(m, project, email, made)
		return fresh()

	case method == http.MethodGet && section == "log" && n == 1:
		if !f.CanManage {
			return nil, 0, fail("forbidden", deniedManageAny)
		}
		log, err := t.accessLog(project)
		return map[string]any{"log": log}, 200, err

	case method == http.MethodPut && section == "settings" && n == 1:
		if role != roleOwner {
			return nil, 0, fail("forbidden", deniedOwnerAny)
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		v, ok := b["editorsCanShare"].(bool)
		if !ok {
			return nil, 0, fail("bad_request", "editorsCanShare — true или false")
		}
		return change(func(t *txn) error { return t.setEditorsCanShare(project, email, v) })

	case method == http.MethodPost && section == "owner" && n == 1:
		if role != roleOwner && !f.Steward && (!f.CanManage || p.Owner != "") {
			return nil, 0, fail("forbidden", deniedOwnerAny)
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		next, _ := b["email"].(string)
		next = normEmail(next)
		if next == "" || len(next) > maxEmail || !emailRe.MatchString(next) {
			return nil, 0, fail("bad_request", badEmail)
		}
		return change(func(t *txn) error { return t.transfer(project, email, next, true) })
	}

	if !f.CanManage {
		if role == "" {
			return nil, 0, fail("forbidden", deniedText)
		}
		return nil, 0, fail("forbidden", deniedManageAny)
	}

	switch {
	case method == http.MethodPut && section == "team" && n == 1:
		if !manager {
			return nil, 0, fail("forbidden", deniedManageAny)
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		level := levelNone
		if v := b["role"]; v != nil {
			if level, err = shareRole(v); err != nil {
				return nil, 0, err
			}
		}
		return change(func(t *txn) error { return t.setTeam(project, email, level) })

	case section == "links":
		return s.linksRoute(r, m, email, project, manager, rest)

	case section == "grants" && n == 3 && (method == http.MethodPut || method == http.MethodDelete):
		subject := part(rest, 2)
		switch third {
		case "user":
			target := normEmail(subject)
			if target == "" || len(target) > maxEmail || !emailRe.MatchString(target) {
				return nil, 0, fail("bad_request", badEmail)
			}
			if target == email && role != roleOwner {
				return nil, 0, fail("forbidden", deniedSelf)
			}
			protect := !manager
			if method == http.MethodDelete {
				return change(func(t *txn) error { return t.revoke(project, email, target, protect) })
			}
			b, err := readBody(r)
			if err != nil {
				return nil, 0, err
			}
			given, err := shareRole(b["role"])
			if err != nil {
				return nil, 0, err
			}
			canShare, err := boolField(b, "canShare")
			if err != nil {
				return nil, 0, err
			}
			kept := storedRole(given, canShare)
			if kept == roleManage && !manager {
				return nil, 0, fail("forbidden", deniedShare)
			}
			term, err := termField(b)
			if err != nil {
				return nil, 0, err
			}
			return change(func(t *txn) error { return t.grant(project, email, target, kept, term, protect) })
		case "team":
			if !teamIDRe.MatchString(subject) {
				return nil, 0, fail("bad_request", "Некорректная команда")
			}
			if !manager {
				shares, err := t.teamGrantShares(project, subject)
				if err != nil {
					return nil, 0, err
				}
				if shares {
					return nil, 0, fail("forbidden", deniedShare)
				}
			}
			me, err := t.w.person(email)
			if err != nil {
				return nil, 0, err
			}
			if _, mine := me.teams[subject]; mine && role != roleOwner && !f.Steward {
				return nil, 0, fail("forbidden", deniedSelf)
			}
			if method == http.MethodDelete {
				return change(func(t *txn) error { return t.revokeTeam(project, email, subject) })
			}
			b, err := readBody(r)
			if err != nil {
				return nil, 0, err
			}
			given, err := shareRole(b["role"])
			if err != nil {
				return nil, 0, err
			}
			canShare, err := boolField(b, "canShare")
			if err != nil {
				return nil, 0, err
			}
			if canShare && !manager {
				return nil, 0, fail("forbidden", deniedShare)
			}
			term, err := termField(b)
			if err != nil {
				return nil, 0, err
			}
			return change(func(t *txn) error { return t.grantTeam(project, email, subject, given, canShare, term) })
		}

	case method == http.MethodPost && section == "requests" && n == 2:
		if !uuidRe.MatchString(third) {
			return nil, 0, fail("not_found", "Запрос не найден")
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		decision, _ := b["decision"].(string)
		if decision != "allow" && decision != "deny" {
			return nil, 0, fail("bad_request", "decision — allow или deny")
		}
		given := ""
		term := ttl{none: true}
		if decision == "allow" {
			if v, ok := b["role"]; ok && v != nil {
				role, err := shareRole(v)
				if err != nil {
					return nil, 0, err
				}
				canShare, err := boolField(b, "canShare")
				if err != nil {
					return nil, 0, err
				}
				given = storedRole(role, canShare)
			}
			if given == roleManage && !manager {
				return nil, 0, fail("forbidden", deniedShare)
			}
			if term, err = termField(b); err != nil {
				return nil, 0, err
			}
		}
		var decided requestRow
		if err := s.write(ctx, m, func(t *txn) error {
			var err error
			decided, err = t.decide(project, third, email, decision, given, term)
			return err
		}); err != nil {
			return nil, 0, err
		}
		s.noticeDecision(m, project, email, decided)
		return fresh()
	}
	return nil, 0, fail("not_found", "Маршрут не найден")
}

// ---- links without sign-in ----------------------------------------------------------------------------------------

func (t *txn) shareByID(id, linkID string) (shareRow, error) {
	r, err := scanShare(t.q.QueryRow(t.ctx, `SELECT `+shareCols+` FROM design_share_links l WHERE l.id = ? AND l.machine_id = ? AND l.project_id = ?`, linkID, t.m.ID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return shareRow{}, fail("not_found", "Ссылка не найдена")
	}
	return r, err
}

func (t *txn) enableShare(id, actor, linkID string) error {
	if _, err := t.lockProject(id); err != nil {
		return err
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE design_share_links SET revoked_at = NULL WHERE id = ? AND machine_id = ? AND project_id = ? AND revoked_at IS NOT NULL`,
		linkID, t.m.ID, id); err != nil {
		return err
	}
	return t.writeLog(id, actor, "share.enable", linkID, nil)
}

// reissueShare gives a link a new address; the old one stops opening at once.
func (t *txn) reissueShare(id, actor, linkID string) (string, error) {
	if _, err := t.lockProject(id); err != nil {
		return "", err
	}
	token := randomToken(16)
	res, err := t.q.Exec(t.ctx, `UPDATE design_share_links SET token_hash = ?, token_enc = ? WHERE id = ? AND machine_id = ? AND project_id = ? AND revoked_at IS NULL`,
		store.HashToken(token), nullStr(t.seal.seal(linkID, token)), linkID, t.m.ID, id)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fail("conflict", linkOff)
	}
	return token, t.writeLog(id, actor, "share.reissue", linkID, nil)
}

// shareAddress is the kept token of a link; false when it was never kept or the hub's key changed.
func (t *txn) shareAddress(id, linkID string) (string, bool, error) {
	var enc sql.NullString
	err := t.q.QueryRow(t.ctx, `SELECT token_enc FROM design_share_links WHERE id = ? AND machine_id = ? AND project_id = ?`, linkID, t.m.ID, id).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, fail("not_found", "Ссылка не найдена")
	}
	if err != nil {
		return "", false, err
	}
	token, ok := t.seal.open(linkID, enc.String)
	return token, ok, nil
}

func (s *Service) linksRoute(r *http.Request, m store.Machine, email, project string, manager bool, rest []string) (any, int, error) {
	ctx := r.Context()
	if s.shareURL == "" {
		return nil, 0, fail("unavailable", "Ссылки без входа не настроены")
	}
	method, n, linkID := r.Method, len(rest), part(rest, 1)
	if n == 1 {
		switch method {
		case http.MethodGet:
			t, err := s.read(ctx, m)
			if err != nil {
				return nil, 0, err
			}
			rows, err := t.shareRows(project)
			links := []map[string]any{}
			for _, row := range rows {
				links = append(links, s.linkJSON(row))
			}
			return map[string]any{"links": links}, 200, err
		case http.MethodPost:
			return s.createLink(r, m, email, project)
		}
		return nil, 0, fail("not_found", "Маршрут не найден")
	}
	if !uuidRe.MatchString(linkID) {
		return nil, 0, fail("not_found", "Ссылка не найдена")
	}
	t, err := s.read(ctx, m)
	if err != nil {
		return nil, 0, err
	}
	link, err := t.shareByID(project, linkID)
	if err != nil {
		return nil, 0, err
	}
	if !manager && normEmail(link.CreatedBy) != email {
		return nil, 0, fail("forbidden", deniedLink)
	}
	switch {
	case n == 2 && method == http.MethodPatch:
		return s.changeLink(r, m, email, project, link)
	case n == 3 && part(rest, 2) == "address" && method == http.MethodGet:
		token, ok, err := t.shareAddress(project, linkID)
		if err != nil || !ok {
			return map[string]any{"url": nil}, 200, err
		}
		return map[string]any{"url": s.shareURLOf(token)}, 200, nil
	case n == 3 && part(rest, 2) == "address" && method == http.MethodPost:
		var token string
		if err := s.write(ctx, m, func(t *txn) error {
			var err error
			token, err = t.reissueShare(project, email, linkID)
			return err
		}); err != nil {
			return nil, 0, err
		}
		s.dropTokens()
		return map[string]any{"url": s.shareURLOf(token)}, 200, nil
	}
	return nil, 0, fail("not_found", "Маршрут не найден")
}

func forWhomField(v any) (string, error) {
	s, _ := v.(string)
	s = clip(strings.TrimSpace(s), maxForWhom)
	if s == "" || control(s) {
		return "", fail("bad_request", "Напишите, для кого ссылка")
	}
	return s, nil
}

func linkRights(v any) (string, error) {
	r, _ := v.(string)
	if r != roleView && r != roleComment {
		return "", fail("bad_request", "Доступ по ссылке: смотреть (view) или комментировать (comment)")
	}
	return r, nil
}

func (s *Service) createLink(r *http.Request, m store.Machine, email, project string) (any, int, error) {
	ctx := r.Context()
	b, err := readBody(r)
	if err != nil {
		return nil, 0, err
	}
	in := shareInput{Version: "latest"}
	if in.Title, err = forWhomField(b["forWhom"]); err != nil {
		return nil, 0, err
	}
	if in.Rights, err = linkRights(b["rights"]); err != nil {
		return nil, 0, err
	}
	if in.TTL, err = termField(b); err != nil {
		return nil, 0, err
	}
	if in.View, err = checkView(b["options"]); err != nil {
		return nil, 0, err
	}
	if v, ok := b["version"]; ok && v != nil {
		if in.Version, err = checkVersion(v); err != nil {
			return nil, 0, err
		}
	}
	if in.Pages, err = checkPages(b["pages"]); err != nil {
		return nil, 0, err
	}
	if in.Version == "pinned" {
		if in.Rev, err = s.pin(ctx, m, project); err != nil {
			return nil, 0, err
		}
	}
	var row shareRow
	var token string
	if err := s.write(ctx, m, func(t *txn) error {
		var err error
		row, token, err = t.createShare(project, email, in)
		return err
	}); err != nil {
		return nil, 0, err
	}
	return map[string]any{"link": s.linkJSON(row), "url": s.shareURLOf(token)}, http.StatusCreated, nil
}

func (s *Service) changeLink(r *http.Request, m store.Machine, email, project string, link shareRow) (any, int, error) {
	ctx := r.Context()
	b, err := readBody(r)
	if err != nil {
		return nil, 0, err
	}
	var c shareChange
	changes := false
	if v, ok := b["forWhom"]; ok {
		title, err := forWhomField(v)
		if err != nil {
			return nil, 0, err
		}
		c.Title, changes = &title, true
	}
	if v, ok := b["rights"]; ok {
		rights, err := linkRights(v)
		if err != nil {
			return nil, 0, err
		}
		c.Rights, changes = &rights, true
	}
	if _, ok := b["ttl"]; ok {
		term, err := termField(b)
		if err != nil {
			return nil, 0, err
		}
		c.TTL, changes = &term, true
	}
	if v, ok := b["pages"]; ok {
		pages, err := checkPages(v)
		if err != nil {
			return nil, 0, err
		}
		c.Pages, changes = &pages, true
	}
	if v, ok := b["options"]; ok {
		view, err := checkView(v)
		if err != nil {
			return nil, 0, err
		}
		c.View, changes = &view, true
	}
	if v, ok := b["version"]; ok && v != nil {
		version, err := checkVersion(v)
		if err != nil {
			return nil, 0, err
		}
		c.Version, changes = &version, true
		if version == "latest" && link.Rev != "" {
			none := ""
			c.Rev = &none
		}
		if version == "pinned" && link.Rev == "" {
			pinned, err := s.pin(ctx, m, project)
			if err != nil {
				return nil, 0, err
			}
			c.Rev = &pinned
		}
	}
	var disabled *bool
	if v, ok := b["disabled"]; ok && v != nil {
		x, ok := v.(bool)
		if !ok {
			return nil, 0, fail("bad_request", "disabled — true или false")
		}
		disabled = &x
	}
	off := link.Revoked != nil
	if off && changes && (disabled == nil || *disabled) {
		return nil, 0, fail("conflict", linkOff)
	}
	var out shareRow
	if err := s.write(ctx, m, func(t *txn) error {
		if disabled != nil && !*disabled && off {
			if err := t.enableShare(project, email, link.ID); err != nil {
				return err
			}
		}
		if changes {
			if _, err := t.updateShare(project, email, link.ID, c); err != nil {
				return err
			}
		}
		if disabled != nil && *disabled && !off {
			if err := t.revokeShare(project, email, link.ID); err != nil {
				return err
			}
		}
		var err error
		out, err = t.shareByID(project, link.ID)
		return err
	}); err != nil {
		return nil, 0, err
	}
	s.dropTokens()
	// A link whose maker may no longer share counts as off, the way a guest is told.
	t, err := s.read(ctx, m)
	if err != nil {
		return nil, 0, err
	}
	if out.Revoked == nil {
		if ok, err := t.makerMayShare(project, normEmail(out.CreatedBy)); err != nil {
			return nil, 0, err
		} else if !ok {
			now := t.now
			out.Revoked = &now
		}
	}
	return map[string]any{"link": s.linkJSON(out)}, 200, nil
}

// ---- who the person is, whom they may add ---------------------------------------------------------------------------

func (s *Service) myTeams(ctx context.Context, m store.Machine, email string) (map[string]any, error) {
	w, err := newWorld(ctx, s.db.S(), m)
	if err != nil {
		return nil, err
	}
	p, err := w.person(email)
	if err != nil {
		return nil, err
	}
	orgSeen := map[string]bool{}
	orgs := []map[string]any{}
	addOrg := func(id string) {
		if id == "" || orgSeen[id] {
			return
		}
		orgSeen[id] = true
		orgs = append(orgs, map[string]any{"orgId": id, "name": w.orgName(id), "admin": p.admin[id]})
	}
	ids := make([]string, 0, len(p.teams))
	for id := range p.teams {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	teams := []map[string]any{}
	for _, id := range ids {
		place := p.teams[id]
		addOrg(place.org)
		name := id
		if ref, ok := w.team(id); ok {
			name = ref.Name
		}
		teams = append(teams, map[string]any{"teamId": id, "orgId": place.org, "name": name, "lead": place.lead, "primary": place.primary})
	}
	admins := make([]string, 0, len(p.admin))
	for id := range p.admin {
		admins = append(admins, id)
	}
	sort.Strings(admins)
	for _, id := range admins {
		addOrg(id)
	}
	return map[string]any{"orgs": orgs, "teams": teams}, nil
}

// directory: the people and the teams of the machine's space a person may add; nothing for someone outside every team.
func (s *Service) directory(ctx context.Context, m store.Machine, email, query string) (map[string]any, error) {
	people, teams := []map[string]any{}, []map[string]any{}
	empty := map[string]any{"people": people, "teams": teams}
	w, err := newWorld(ctx, s.db.S(), m)
	if err != nil {
		return nil, err
	}
	me, err := w.person(email)
	if err != nil {
		return nil, err
	}
	if len(me.teams) == 0 && len(me.admin) == 0 {
		return empty, nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if r := []rune(q); len(r) > maxQuery {
		q = string(r[:maxQuery])
	}
	members, err := s.db.S().Members(ctx, m.SpaceID)
	if err != nil {
		return nil, err
	}
	for _, mb := range members {
		addr := normEmail(mb.Email)
		if addr == email || addr == "" {
			continue
		}
		name := strings.TrimSpace(mb.Name)
		if name == "" {
			name = localPart(addr)
		}
		if q != "" && !strings.Contains(addr, q) && !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		var names []string
		if p, err := w.person(addr); err == nil {
			for id, place := range p.teams {
				if place.space != w.space.ID {
					continue
				}
				if ref, ok := w.team(id); ok {
					names = append(names, ref.Name)
				}
			}
		}
		sort.Strings(names)
		if names == nil {
			names = []string{}
		}
		people = append(people, map[string]any{"email": addr, "name": name, "teams": names})
		if len(people) >= directoryLimit {
			break
		}
	}
	ids := []string{w.implicitTeam()}
	if list, err := s.db.S().TeamsBySpace(ctx, m.SpaceID); err == nil {
		for _, tm := range list {
			ids = append(ids, tm.ID)
		}
	}
	for _, id := range ids {
		ref, ok := w.team(id)
		if !ok || (q != "" && !strings.Contains(strings.ToLower(ref.Name), q)) {
			continue
		}
		teams = append(teams, map[string]any{"teamId": ref.ID, "name": ref.Name, "orgName": ref.OrgName, "members": ref.Members})
		if len(teams) >= directoryLimit {
			break
		}
	}
	return map[string]any{"people": people, "teams": teams}, nil
}

// ---- notices ------------------------------------------------------------------------------------------------------

func (s *Service) pageURL(m store.Machine, project, access string) string {
	u := s.hubURL + "/m/" + m.ID + "/designly/" + url.PathEscape(project)
	if access != "" {
		u += "?access=" + access
	}
	return u
}

func (s *Service) send(to, subject, body, link string) {
	if s.notice == nil || to == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), noticeTimeout)
		defer cancel()
		if err := s.notice(ctx, to, subject, body, link); err != nil {
			s.log.Warn("design access notice not sent", "error", err)
		}
	}()
}

// noticeRequest tells those who run the access that somebody asks for it; never the one who asked.
func (s *Service) noticeRequest(m store.Machine, project, asker string, rq requestRow) {
	if s.notice == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	t, err := s.read(ctx, m)
	if err != nil {
		return
	}
	p, stored, err := t.effective(project)
	if err != nil || !stored {
		return
	}
	to := map[string]bool{}
	if p.Owner != "" {
		to[p.Owner] = true
	}
	if people, err := t.members(project); err == nil {
		for _, mb := range people {
			if live(mb.Expires, t.now) && (mb.Role == roleManage || (mb.Role == roleEdit && p.EditorsCanShare)) {
				to[mb.Email] = true
			}
		}
	}
	delete(to, normEmail(asker))
	what := "комментирование"
	if rq.Level == roleEdit {
		what = "правку"
	}
	subject := clip(t.w.displayName(asker), 60) + " просит " + what + " «" + clip(project, 80) + "»"
	link := s.pageURL(m, project, "requests")
	for addr := range to {
		s.send(addr, subject, clip(rq.Message, 180), link)
	}
}

// noticeDecision tells the person who asked what was decided.
func (s *Service) noticeDecision(m store.Machine, project, actor string, rq requestRow) {
	if rq.Email == "" || rq.Email == normEmail(actor) {
		return
	}
	title := "«" + clip(project, 80) + "»"
	if rq.Status == "allowed" {
		s.send(rq.Email, "Доступ выдан", title, s.pageURL(m, project, ""))
		return
	}
	s.send(rq.Email, "Отклонён", "Запрос доступа к "+title+" отклонён", "")
}

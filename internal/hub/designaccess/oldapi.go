package designaccess

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The page's access API, <platform api>/design/access/*: the same routes, bodies, answers and refusals as the
// cluster's gateway the page was written against.

const (
	deniedText   = "Нет доступа"
	deniedManage = "Доступом проекта управляют владелец и те, кому он это разрешил"
	deniedSelf   = "Свой доступ меняет владелец"
	deniedOwner  = "Это может сделать только владелец проекта"
)

func textField(v any, field string) (string, error) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fail("bad_request", "Не указано поле "+field)
	}
	return s, nil
}

func emailField(v any, field string) (string, error) {
	s, _ := v.(string)
	s = normEmail(s)
	if s == "" || len(s) > maxEmail || !emailRe.MatchString(s) {
		return "", fail("bad_request", "Некорректный адрес почты ("+field+")")
	}
	return s, nil
}

// ttlField: a missing field is an error (forever is never the default) unless a fallback is given.
func ttlField(body map[string]any, field string, fallback *ttl) (ttl, error) {
	v, ok := body[field]
	if !ok {
		if fallback != nil {
			return *fallback, nil
		}
		return ttl{}, fail("bad_request", "Укажите срок ("+field+")")
	}
	return ttlOf(v, "Срок: 1h, 1d, 7d, 30d, дата или null")
}

func stringOr(v any) string {
	s, _ := v.(string)
	return s
}

// view is the person's access to a stored project as the page reads it; the lists only for who runs the access.
func (t *txn) view(p project, email string) (map[string]any, folded, error) {
	f, err := t.fold(p, email)
	if err != nil {
		return nil, f, err
	}
	out := map[string]any{
		"role": nullStr(f.Role), "owner": nullStr(p.Owner), "ownerName": p.OwnerName, "team": p.TeamLevel,
		"canManage": f.CanManage, "editorsCanShare": p.EditorsCanShare, "request": nil,
	}
	own, err := t.requests(p.ID, normEmail(email), "open")
	if err != nil {
		return nil, f, err
	}
	if len(own) > 0 {
		out["request"] = own[0].old()
	}
	if !f.CanManage {
		return out, f, nil
	}
	members, err := t.members(p.ID)
	if err != nil {
		return nil, f, err
	}
	people := []map[string]any{}
	for _, m := range members {
		if !live(m.Expires, t.now) {
			continue
		}
		people = append(people, map[string]any{"email": m.Email, "role": m.Role, "grantedBy": m.GrantedBy,
			"grantedAt": isoOf(m.GrantedAt), "expiresAt": iso(m.Expires), "viaInvite": nullStr(m.ViaInvite)})
	}
	invites, err := t.liveInvites(p.ID)
	if err != nil {
		return nil, f, err
	}
	inv := []map[string]any{}
	for _, i := range invites {
		inv = append(inv, i.json())
	}
	open, err := t.requests(p.ID, "", "open")
	if err != nil {
		return nil, f, err
	}
	reqs := []map[string]any{}
	for _, r := range open {
		reqs = append(reqs, r.old())
	}
	out["people"], out["invites"], out["requests"] = people, inv, reqs
	return out, f, nil
}

func (s *Service) serveOld(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, rest string) {
	var parts []string
	if rest != "" {
		for _, raw := range strings.Split(rest, "/") {
			part, err := url.PathUnescape(raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Некорректный адрес", "code": "bad_request"})
				return
			}
			parts = append(parts, part)
		}
	}
	res, status, err := s.oldRoute(r, u, m, parts)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, status, res)
}

func part(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return ""
}

func (s *Service) oldRoute(r *http.Request, u store.User, m store.Machine, parts []string) (any, int, error) {
	ctx := r.Context()
	email := normEmail(u.Email)
	method := r.Method
	first, section, third := part(parts, 0), part(parts, 1), part(parts, 2)
	n := len(parts)

	if method == http.MethodPost && first == "invites" && third == "accept" && n == 3 {
		var project, role string
		var until any
		err := s.write(ctx, m, func(t *txn) error {
			p, ro, un, err := t.acceptInvite(section, email)
			project, role, until = p, ro, iso(un)
			return err
		})
		if err != nil {
			return nil, 0, err
		}
		return map[string]any{"project": project, "role": role, "until": until}, 200, nil
	}
	project := first
	if project == "" {
		return nil, 0, fail("not_found", "Маршрут не найден")
	}
	if !projectIDRe.MatchString(project) {
		return nil, 0, fail("not_found", "Проект не найден")
	}

	t, err := s.read(ctx, m)
	if err != nil {
		return nil, 0, err
	}
	p, stored, err := t.effective(project)
	if err != nil {
		return nil, 0, err
	}
	if method == http.MethodGet && n == 1 {
		if !stored {
			return map[string]any{"role": roleEdit, "owner": nil, "ownerName": "", "team": roleEdit, "canManage": false, "editorsCanShare": false, "request": nil}, 200, nil
		}
		v, f, err := t.view(p, email)
		if err != nil {
			return nil, 0, err
		}
		if f.Role == "" {
			v["owner"] = nil
		}
		return v, 200, nil
	}
	if !stored {
		return nil, 0, fail("not_found", "Проект не найден")
	}
	f, err := t.fold(p, email)
	if err != nil {
		return nil, 0, err
	}
	role := f.Role
	respond := func() (any, int, error) {
		t, err := s.read(ctx, m)
		if err != nil {
			return nil, 0, err
		}
		p, _, err := t.effective(project)
		if err != nil {
			return nil, 0, err
		}
		v, _, err := t.view(p, email)
		return v, 200, err
	}
	change := func(fn func(t *txn) error) (any, int, error) {
		if err := s.write(ctx, m, fn); err != nil {
			return nil, 0, err
		}
		return respond()
	}
	body := func() (map[string]any, error) { return readBody(r) }

	switch {
	case method == http.MethodGet && section == "log" && n == 2:
		if !f.CanManage {
			return nil, 0, fail("forbidden", deniedManage)
		}
		log, err := t.accessLog(project)
		return map[string]any{"log": log}, 200, err

	case method == http.MethodGet && section == "guest-comments" && n == 2:
		if role == "" {
			return nil, 0, fail("forbidden", deniedText)
		}
		list, err := t.listGuestComments(project)
		if list == nil {
			list = []guestComment{}
		}
		return list, 200, err

	case method == http.MethodPost && section == "requests" && n == 2:
		b, err := body()
		if err != nil {
			return nil, 0, err
		}
		level, err := textField(b["level"], "level")
		if err != nil {
			return nil, 0, err
		}
		var made requestRow
		if err := s.write(ctx, m, func(t *txn) error {
			var err error
			made, err = t.request(project, email, level, stringOr(b["message"]))
			return err
		}); err != nil {
			return nil, 0, err
		}
		v, status, err := respond()
		if err != nil {
			return nil, 0, err
		}
		v.(map[string]any)["request"] = made.old()
		return v, status, nil

	case method == http.MethodPost && section == "revoke-all" && n == 2:
		if role != roleOwner {
			return nil, 0, fail("forbidden", deniedOwner)
		}
		return change(func(t *txn) error { return t.revokeAll(project, email) })

	case method == http.MethodPut && section == "settings" && n == 2:
		if role != roleOwner {
			return nil, 0, fail("forbidden", deniedOwner)
		}
		b, err := body()
		if err != nil {
			return nil, 0, err
		}
		v, ok := b["editorsCanShare"].(bool)
		if !ok {
			return nil, 0, fail("bad_request", "editorsCanShare — true или false")
		}
		return change(func(t *txn) error { return t.setEditorsCanShare(project, email, v) })

	case method == http.MethodPost && section == "owner" && n == 2:
		takes := p.Owner == "" && role != "" && rank(role) >= rank(roleEdit)
		if role != roleOwner && !takes {
			return nil, 0, fail("forbidden", deniedOwner)
		}
		b, err := body()
		if err != nil {
			return nil, 0, err
		}
		next, err := emailField(b["email"], "email")
		if err != nil {
			return nil, 0, err
		}
		return change(func(t *txn) error { return t.transfer(project, email, next, role != roleOwner) })
	}

	if !f.CanManage {
		if section != "" {
			return nil, 0, fail("forbidden", deniedManage)
		}
		return nil, 0, fail("forbidden", deniedText)
	}
	isManager := role == roleOwner || role == roleManage || f.Steward

	switch {
	case method == http.MethodPut && section == "team" && n == 2:
		if !isManager {
			return nil, 0, fail("forbidden", deniedManage)
		}
		b, err := body()
		if err != nil {
			return nil, 0, err
		}
		level, err := textField(b["level"], "level")
		if err != nil {
			return nil, 0, err
		}
		return change(func(t *txn) error { return t.setTeam(project, email, level) })

	case (method == http.MethodPut || method == http.MethodDelete) && section == "people" && n == 3:
		target, err := emailField(third, "email")
		if err != nil {
			return nil, 0, err
		}
		if target == email && role != roleOwner {
			return nil, 0, fail("forbidden", deniedSelf)
		}
		protect := !isManager
		if method == http.MethodDelete {
			return change(func(t *txn) error { return t.revoke(project, email, target, protect) })
		}
		b, err := body()
		if err != nil {
			return nil, 0, err
		}
		given, err := textField(b["role"], "role")
		if err != nil {
			return nil, 0, err
		}
		if given == roleManage && !isManager {
			return nil, 0, fail("forbidden", deniedManage)
		}
		term, err := ttlField(b, "ttl", nil)
		if err != nil {
			return nil, 0, err
		}
		return change(func(t *txn) error { return t.grant(project, email, target, given, term, protect) })

	case method == http.MethodPost && section == "requests" && n == 3:
		if !uuidRe.MatchString(third) {
			return nil, 0, fail("not_found", "Запрос не найден")
		}
		b, err := body()
		if err != nil {
			return nil, 0, err
		}
		decision, _ := b["decision"].(string)
		if decision != "allow" && decision != "deny" {
			return nil, 0, fail("bad_request", "decision — allow или deny")
		}
		given := ""
		if v, ok := b["role"]; ok {
			if given, err = textField(v, "role"); err != nil {
				return nil, 0, err
			}
		}
		if given == roleManage && !isManager {
			return nil, 0, fail("forbidden", deniedManage)
		}
		term := ttl{none: true}
		if decision == "allow" {
			if term, err = ttlField(b, "ttl", nil); err != nil {
				return nil, 0, err
			}
		}
		return change(func(t *txn) error {
			_, err := t.decide(project, third, email, decision, given, term)
			return err
		})

	case section == "shares":
		return s.sharesRoute(r, m, email, project, parts)

	case method == http.MethodPost && section == "invites" && n == 2:
		b, err := body()
		if err != nil {
			return nil, 0, err
		}
		in := inviteInput{Title: stringOr(b["title"])}
		if in.Role, err = textField(b["role"], "role"); err != nil {
			return nil, 0, err
		}
		if in.RoleTTL, err = ttlField(b, "roleTtl", nil); err != nil {
			return nil, 0, err
		}
		if in.TTL, err = ttlField(b, "ttl", nil); err != nil {
			return nil, 0, err
		}
		if v, ok := b["maxUses"]; ok && v != nil {
			x, ok := v.(float64)
			if !ok || x != float64(int64(x)) || x < 1 || x > maxInviteUses {
				return nil, 0, fail("bad_request", "maxUses — целое от 1 или null")
			}
			n := int64(x)
			in.MaxUses = &n
		}
		var id, token string
		var expires any
		if err := s.write(ctx, m, func(t *txn) error {
			i, tok, e, err := t.createInvite(project, email, in)
			id, token, expires = i, tok, isoOf(e)
			return err
		}); err != nil {
			return nil, 0, err
		}
		return map[string]any{"id": id, "token": token, "expiresAt": expires, "url": s.hubURL + "/m/" + m.ID + "/designly/invite/" + token}, 200, nil

	case method == http.MethodDelete && section == "invites" && n == 3:
		if !uuidRe.MatchString(third) {
			return nil, 0, fail("not_found", "Ссылка не найдена")
		}
		return change(func(t *txn) error { return t.revokeInvite(project, email, third) })
	}
	return nil, 0, fail("not_found", "Маршрут не найден")
}

func (s *Service) shareURLOf(token string) string { return s.shareURL + "/s/" + token }

func (s *Service) sharesRoute(r *http.Request, m store.Machine, email, project string, parts []string) (any, int, error) {
	ctx := r.Context()
	if s.shareURL == "" {
		return nil, 0, fail("unavailable", "Ссылки без входа не настроены")
	}
	method, n, third := r.Method, len(parts), part(parts, 2)
	switch {
	case method == http.MethodGet && n == 2:
		t, err := s.read(ctx, m)
		if err != nil {
			return nil, 0, err
		}
		list, err := t.listShares(project)
		if list == nil {
			list = []map[string]any{}
		}
		return list, 200, err

	case method == http.MethodPost && n == 2:
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		in := shareInput{Title: stringOr(b["title"]), Version: "latest"}
		if v, ok := b["version"]; ok {
			if in.Version, err = checkVersion(v); err != nil {
				return nil, 0, err
			}
		}
		rights, err := textField(b["rights"], "rights")
		if err != nil {
			return nil, 0, err
		}
		if in.Rights, err = checkRights(rights); err != nil {
			return nil, 0, err
		}
		week := ttl{step: ttlSteps["7d"]}
		if in.TTL, err = ttlField(b, "ttl", &week); err != nil {
			return nil, 0, err
		}
		if in.View, err = checkView(b["view"]); err != nil {
			return nil, 0, err
		}
		if in.Pages, err = checkPages(b["pages"]); err != nil {
			return nil, 0, err
		}
		if in.Version == "pinned" {
			if in.Rev, err = s.pin(ctx, m, project); err != nil {
				return nil, 0, err
			}
		} else if err := s.shareGate(ctx, m, project); err != nil {
			return nil, 0, err
		}
		var made map[string]any
		if err := s.write(ctx, m, func(t *txn) error {
			row, token, err := t.createShare(project, email, in)
			made = row.json(s.shareURLOf(token))
			return err
		}); err != nil {
			return nil, 0, err
		}
		return made, http.StatusCreated, nil

	case method == http.MethodGet && n == 4 && part(parts, 3) == "views":
		if !uuidRe.MatchString(third) {
			return nil, 0, fail("not_found", "Ссылка не найдена")
		}
		t, err := s.read(ctx, m)
		if err != nil {
			return nil, 0, err
		}
		v, err := t.shareViews(project, third)
		return v, 200, err

	case n == 3 && (method == http.MethodPatch || method == http.MethodDelete):
		if !uuidRe.MatchString(third) {
			return nil, 0, fail("not_found", "Ссылка не найдена")
		}
		if method == http.MethodDelete {
			if err := s.write(ctx, m, func(t *txn) error { return t.revokeShare(project, email, third) }); err != nil {
				return nil, 0, err
			}
			s.dropTokens()
			return map[string]any{"ok": true}, 200, nil
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		var c shareChange
		if v, ok := b["title"]; ok {
			title := stringOr(v)
			c.Title = &title
		}
		if v, ok := b["rights"]; ok {
			rights, err := textField(v, "rights")
			if err != nil {
				return nil, 0, err
			}
			if rights, err = checkRights(rights); err != nil {
				return nil, 0, err
			}
			c.Rights = &rights
		}
		if _, ok := b["ttl"]; ok {
			term, err := ttlField(b, "ttl", nil)
			if err != nil {
				return nil, 0, err
			}
			c.TTL = &term
		}
		if v, ok := b["pages"]; ok {
			pages, err := checkPages(v)
			if err != nil {
				return nil, 0, err
			}
			c.Pages = &pages
		}
		if v, ok := b["view"]; ok {
			view, err := checkView(v)
			if err != nil {
				return nil, 0, err
			}
			c.View = &view
		}
		refresh := false
		if v, ok := b["refresh"]; ok {
			if refresh, ok = v.(bool); !ok {
				return nil, 0, fail("bad_request", "refresh — true или false")
			}
		}
		requested := ""
		if v, ok := b["version"]; ok {
			if requested, err = checkVersion(v); err != nil {
				return nil, 0, err
			}
			c.Version = &requested
		}
		if requested != "" || refresh {
			t, err := s.read(ctx, m)
			if err != nil {
				return nil, 0, err
			}
			_, version, rev, err := t.shareState(project, third)
			if err != nil {
				return nil, 0, err
			}
			next := requested
			if next == "" {
				next = version
			}
			if refresh && next != "pinned" {
				return nil, 0, fail("bad_request", "Обновить можно только ссылку с закреплённой версией")
			}
			if next == "latest" {
				if rev != "" {
					none := ""
					c.Rev = &none
				}
			} else if refresh || rev == "" {
				pinned, err := s.pin(ctx, m, project)
				if err != nil {
					return nil, 0, err
				}
				c.Rev = &pinned
			}
		}
		var out map[string]any
		if err := s.write(ctx, m, func(t *txn) error {
			row, err := t.updateShare(project, email, third, c)
			out = row.json(nil)
			return err
		}); err != nil {
			return nil, 0, err
		}
		s.dropTokens()
		return out, 200, nil
	}
	return nil, 0, fail("not_found", "Маршрут не найден")
}

package resaccess

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Actor is the signed-in person a request is made by. Admin is an administrator of the installation: they run every
// resource's access.
type Actor struct {
	// UserID is the person's account: a role given to a person is held by it.
	UserID string
	Email  string
	Name   string
	// Verified is the addresses the person has proved, in lower case. An invitation to an address reaches only a
	// person who has proved it.
	Verified []string
	Admin    bool
	// Outside says the person is of none of the resource's organizations, as the caller knows them: the level a
	// resource without a home team gives "the team" does not reach them. Shares by name and team grants still do.
	Outside bool
}

// Info is what the caller knows about a resource the model has no row for yet: its owner, as the kind's own records have it.
type Info struct{ Owner, OwnerName string }

// proved reports whether the person has proved the address.
func (a Actor) proved(email string) bool {
	for _, v := range a.Verified {
		if v == email {
			return true
		}
	}
	return false
}

const (
	deniedManage = "Доступом управляют владелец и те, кому он это разрешил"
	deniedOwner  = "Это может сделать только владелец"
	deniedSelf   = "Свою роль меняет только владелец"
)

func readBody(r *http.Request) (map[string]any, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return nil, fail("bad_request", "Тело запроса не прочитано")
	}
	out := map[string]any{}
	if len(strings.TrimSpace(string(b))) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fail("bad_request", "Некорректное тело запроса")
	}
	return out, nil
}

func str(b map[string]any, k string) string { s, _ := b[k].(string); return s }

func termOf(b map[string]any, k, fallback string) (ttl, error) {
	v, ok := b[k]
	if !ok {
		if fallback == "" {
			return ttl{none: true}, nil
		}
		v = fallback
	}
	return ttlOf(v)
}

// Role is a person's role in a resource now ("" when closed to them); admins count as managers.
func (s *Service) Role(ctx context.Context, ref Ref, a Actor) (string, error) {
	t := s.read(ctx)
	r, people, grants, err := t.load(ref, false)
	if err != nil {
		return "", err
	}
	f, err := t.foldFor(r, people, grants, a)
	if err != nil {
		return "", err
	}
	s.accept(ctx, ref, people, a)
	if a.Admin && Rank(f.Role) < Rank(roleManage) {
		return roleManage, nil
	}
	return f.Role, nil
}

// View is the access state of a resource as the person sees it.
func (s *Service) View(ctx context.Context, ref Ref, a Actor) (map[string]any, error) {
	return s.read(ctx).view(ref, a, Info{})
}

func (t *txn) view(ref Ref, a Actor, info Info) (map[string]any, error) {
	r, people, grants, err := t.load(ref, false)
	if err != nil {
		return nil, err
	}
	if r.Owner == "" && info.Owner != "" {
		r.Owner, r.OwnerName = normEmail(info.Owner), info.OwnerName
	}
	f, err := t.foldFor(r, people, grants, a)
	if err != nil {
		return nil, err
	}
	if a.Admin && Rank(f.Role) < Rank(roleManage) {
		f = folded{Role: roleManage, CanManage: true, Steward: true}
	}
	out := map[string]any{"role": nullStr(f.Role), "owner": nil, "ownerName": r.OwnerName, "team": r.TeamLevel, "canManage": f.CanManage,
		"steward": f.Steward, "editorsCanShare": r.EditorsCanShare, "stored": r.Stored, "request": nil, "until": iso(f.Until)}
	if f.Role != "" || f.CanManage {
		out["owner"] = nullStr(r.Owner)
	}
	if r.HomeTeam != "" {
		name := r.HomeTeam
		if tr, ok := t.w.team(r.HomeTeam); ok {
			name = tr.Name
		}
		out["homeTeam"] = map[string]any{"teamId": r.HomeTeam, "name": name}
	}
	if r.Stored {
		mine, err := t.requests(ref, normEmail(a.Email), "open")
		if err != nil {
			return nil, err
		}
		if len(mine) > 0 {
			out["request"] = mine[0].json()
		}
	}
	if !f.CanManage {
		return out, nil
	}
	ppl := []map[string]any{}
	for _, m := range people {
		if live(m.Expires, t.now) {
			ppl = append(ppl, map[string]any{"email": m.Email, "name": t.w.displayName(m.Email), "role": m.Role, "grantedBy": m.GrantedBy,
				"grantedAt": isoOf(m.GrantedAt), "expiresAt": iso(m.Expires), "pending": m.UserID == ""})
		}
	}
	tms := []map[string]any{}
	for _, g := range grants {
		if !live(g.Expires, t.now) {
			continue
		}
		name := g.Team
		if tr, ok := t.w.team(g.Team); ok {
			name = tr.Name
		}
		tms = append(tms, map[string]any{"teamId": g.Team, "name": name, "role": g.Role, "canShare": g.CanShare, "grantedBy": g.GrantedBy,
			"grantedAt": isoOf(g.GrantedAt), "expiresAt": iso(g.Expires)})
	}
	reqs := []map[string]any{}
	if r.Stored {
		open, err := t.requests(ref, "", "open")
		if err != nil {
			return nil, err
		}
		for _, q := range open {
			j := q.json()
			j["name"] = t.w.displayName(q.Email)
			// Whether the account that asks has proved the address it is shown under.
			proven, err := t.holder(q.Email)
			if err != nil {
				return nil, err
			}
			j["verified"] = q.UserID != "" && proven == q.UserID
			reqs = append(reqs, j)
		}
	}
	shares := []map[string]any{}
	ls, err := t.links(ref)
	if err != nil {
		return nil, err
	}
	for _, l := range ls {
		shares = append(shares, l.json(nil))
	}
	out["people"], out["teams"], out["requests"], out["shares"] = ppl, tms, reqs, shares
	return out, nil
}

func (s *Service) shareURL(token string) string { return s.publicURL + "/d/" + token }

// Serve answers one request of the resource's access API (the part of the path after /acl). The caller has checked that
// the resource exists; info tells who owns it where the model has no row yet. Pinned turns a link's version into the
// one it should keep: "" for the live resource.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, a Actor, ref Ref, rest []string, info Info, pinned func(requested any) (string, error)) {
	out, status, err := s.route(r, a, ref, rest, info, pinned)
	if err != nil {
		var e *Error
		code, msg := "internal", "Внутренняя ошибка"
		if asError(err, &e) {
			code, msg = e.Code, e.Message
		}
		writeJSON(w, Status(err), map[string]any{"error": msg, "code": code})
		return
	}
	writeJSON(w, status, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func part(p []string, i int) string {
	if i < len(p) {
		return p[i]
	}
	return ""
}

func (s *Service) route(r *http.Request, a Actor, ref Ref, rest []string, info Info, pinned func(any) (string, error)) (any, int, error) {
	if !kinds[ref.Kind] || ref.ID == "" {
		return nil, 0, fail("not_found", "Ресурс не найден")
	}
	ctx := r.Context()
	email := normEmail(a.Email)
	method, n := r.Method, len(rest)
	section, second := part(rest, 0), part(rest, 1)
	if ref.Kind == KindKit && (section == "links" || section == "shares" || section == "guest-comments") {
		return nil, 0, fail("forbidden", "Кит не раздают по ссылке без входа: только людям и командам")
	}

	t := s.read(ctx)
	v, err := t.view(ref, a, info)
	if err != nil {
		return nil, 0, err
	}
	role, _ := v["role"].(string)
	canManage, _ := v["canManage"].(bool)
	steward, _ := v["steward"].(bool)
	stored, _ := v["stored"].(bool)
	fresh := func() (any, int, error) {
		x, err := s.read(ctx).view(ref, a, info)
		return x, 200, err
	}
	if method == http.MethodGet && n == 0 {
		return v, 200, nil
	}
	// A request is the one thing a person the resource is closed to can do.
	if method == http.MethodPost && section == "requests" && n == 1 {
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		if !stored && info.Owner == "" {
			return nil, 0, fail("conflict", "У ресурса нет владельца, просить доступ не у кого")
		}
		var made requestRow
		err = s.write(ctx, func(t *txn) error {
			if _, err := t.ensure(ref, info.Owner, info.OwnerName); err != nil {
				return err
			}
			made, err = t.request(ref, email, a.UserID, str(b, "level"), str(b, "message"))
			return err
		})
		if err != nil {
			return nil, 0, err
		}
		s.noticeRequest(ctx, ref, email, made)
		return fresh()
	}
	if role == "" && !canManage {
		return nil, 0, fail("not_found", "Ресурс не найден")
	}
	if method == http.MethodGet && section == "guest-comments" && n == 1 {
		out, err := t.guestComments(ref)
		return out, 200, err
	}
	if method == http.MethodGet && section == "log" && n == 1 {
		if !canManage {
			return nil, 0, fail("forbidden", deniedManage)
		}
		out, err := t.accessLog(ref)
		return map[string]any{"log": out}, 200, err
	}
	if !canManage {
		return nil, 0, fail("forbidden", deniedManage)
	}
	owner := role == roleOwner
	manager := owner || role == roleManage || steward || a.Admin
	change := func(fn func(t *txn) error) (any, int, error) {
		err := s.write(ctx, func(t *txn) error {
			if _, err := t.ensure(ref, info.Owner, info.OwnerName); err != nil {
				return err
			}
			return fn(t)
		})
		if err != nil {
			return nil, 0, err
		}
		return fresh()
	}
	switch {
	case method == http.MethodPut && section == "settings" && n == 1:
		if !owner && !a.Admin {
			return nil, 0, fail("forbidden", deniedOwner)
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		on, ok := b["editorsCanShare"].(bool)
		if !ok {
			return nil, 0, fail("bad_request", "editorsCanShare — true или false")
		}
		return change(func(t *txn) error { return t.setEditorsCanShare(ref, email, on) })

	case method == http.MethodPut && section == "team" && n == 1:
		if !manager {
			return nil, 0, fail("forbidden", deniedManage)
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		return change(func(t *txn) error { return t.setTeam(ref, email, str(b, "level")) })

	case (method == http.MethodPut || method == http.MethodDelete) && section == "people" && n == 2:
		target := normEmail(second)
		// The person's own role is whatever reaches them: the address they sign in under, one they have proved, or
		// a role their account holds under another address.
		self := target == email || a.proved(target)
		if !self && a.UserID != "" {
			held, err := t.heldBy(ref, target)
			if err != nil {
				return nil, 0, err
			}
			self = held == a.UserID
		}
		if self && !owner {
			return nil, 0, fail("forbidden", deniedSelf)
		}
		if method == http.MethodDelete {
			return change(func(t *txn) error { return t.revoke(ref, email, target, !manager) })
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		given := str(b, "role")
		if given == roleManage && !manager {
			return nil, 0, fail("forbidden", deniedManage)
		}
		term, err := termOf(b, "ttl", "")
		if err != nil {
			return nil, 0, err
		}
		return change(func(t *txn) error { return t.grant(ref, email, target, given, term, !manager) })

	case (method == http.MethodPut || method == http.MethodDelete) && section == "teams" && n == 2:
		if !manager {
			return nil, 0, fail("forbidden", deniedManage)
		}
		if method == http.MethodDelete {
			return change(func(t *txn) error { return t.revokeTeam(ref, email, second) })
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		term, err := termOf(b, "ttl", "")
		if err != nil {
			return nil, 0, err
		}
		share, _ := b["canShare"].(bool)
		return change(func(t *txn) error { return t.grantTeam(ref, email, second, str(b, "role"), share, term) })

	case method == http.MethodPost && section == "requests" && n == 2:
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		decision := str(b, "decision")
		if decision != "allow" && decision != "deny" {
			return nil, 0, fail("bad_request", "decision — allow или deny")
		}
		given := str(b, "role")
		if given == roleManage && !manager {
			return nil, 0, fail("forbidden", deniedManage)
		}
		term, err := termOf(b, "ttl", "")
		if err != nil {
			return nil, 0, err
		}
		var done requestRow
		out, status, err := change(func(t *txn) error {
			var err error
			done, err = t.decide(ref, second, email, decision, given, term, !manager)
			return err
		})
		if err == nil {
			s.noticeDecision(ctx, ref, done)
		}
		return out, status, err

	case method == http.MethodPost && section == "owner" && n == 1:
		if !owner && !a.Admin {
			return nil, 0, fail("forbidden", deniedOwner)
		}
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		return change(func(t *txn) error { return t.transfer(ref, email, str(b, "email")) })

	case method == http.MethodPost && section == "revoke-all" && n == 1:
		if !manager {
			return nil, 0, fail("forbidden", deniedManage)
		}
		return change(func(t *txn) error { return t.revokeAll(ref, email) })

	case section == "shares":
		return s.sharesRoute(r, a, ref, rest, info, pinned, change)
	}
	return nil, 0, fail("not_found", "Маршрут не найден")
}

func (s *Service) sharesRoute(r *http.Request, a Actor, ref Ref, rest []string, info Info, pinned func(any) (string, error), change func(func(*txn) error) (any, int, error)) (any, int, error) {
	ctx := r.Context()
	email := normEmail(a.Email)
	n, id := len(rest), part(rest, 1)
	switch {
	case r.Method == http.MethodGet && n == 1:
		x, err := s.read(ctx).view(ref, a, info)
		if err != nil {
			return nil, 0, err
		}
		shares, _ := x["shares"].([]map[string]any)
		if shares == nil {
			shares = []map[string]any{}
		}
		return map[string]any{"shares": shares}, 200, nil

	case r.Method == http.MethodPost && n == 1:
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		in := linkInput{Rights: roleView, Title: str(b, "title")}
		if v := str(b, "rights"); v != "" {
			in.Rights = v
		}
		if in.Term, err = termOf(b, "ttl", "7d"); err != nil {
			return nil, 0, err
		}
		if pinned != nil {
			if in.Pinned, err = pinned(b["version"]); err != nil {
				return nil, 0, err
			}
		}
		var made link
		var token string
		_, _, err = change(func(t *txn) error {
			var err error
			made, token, err = t.createLink(ref, email, in)
			return err
		})
		if err != nil {
			return nil, 0, err
		}
		return made.json(s.shareURL(token)), 201, nil

	case r.Method == http.MethodPatch && n == 2:
		b, err := readBody(r)
		if err != nil {
			return nil, 0, err
		}
		var title *string
		if v, ok := b["title"].(string); ok {
			title = &v
		}
		var term *ttl
		if _, ok := b["ttl"]; ok {
			x, err := termOf(b, "ttl", "")
			if err != nil {
				return nil, 0, err
			}
			term = &x
		}
		var out link
		_, _, err = change(func(t *txn) error {
			var err error
			out, err = t.updateLink(ref, email, id, title, str(b, "rights"), term)
			return err
		})
		if err != nil {
			return nil, 0, err
		}
		return out.json(nil), 200, nil

	case r.Method == http.MethodDelete && n == 1:
		var count int64
		_, _, err := change(func(t *txn) error {
			var err error
			count, err = t.revokeAllLinks(ref, email)
			return err
		})
		return map[string]any{"revoked": count}, 200, err

	case r.Method == http.MethodDelete && n == 2:
		_, _, err := change(func(t *txn) error { return t.revokeLink(ref, email, id) })
		return map[string]any{"ok": true}, 200, err
	}
	return nil, 0, fail("not_found", "Маршрут не найден")
}

// noticeRequest and noticeDecision tell people by mail where the hub can; they never fail the request.
func (s *Service) noticeRequest(ctx context.Context, ref Ref, from string, q requestRow) {
	if s.Notify == nil {
		return
	}
	t := s.read(ctx)
	r, _, _, err := t.load(ref, false)
	if err != nil || r.Owner == "" {
		return
	}
	s.Notify(ctx, r.Owner, "Просят доступ", from+" просит доступ ("+q.Level+") к «"+r.Title+"»\n"+q.Message)
}

func (s *Service) noticeDecision(ctx context.Context, ref Ref, q requestRow) {
	if s.Notify == nil || q.Email == "" {
		return
	}
	word := map[string]string{"allowed": "Доступ открыт", "denied": "В доступе отказано"}[q.Status]
	s.Notify(ctx, q.Email, word, word+": "+ref.Kind+" "+ref.ID)
}

func asError(err error, out **Error) bool { return errors.As(err, out) }

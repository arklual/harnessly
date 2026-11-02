package orgs

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Teams, the people in them, roles and the journal of an organization (docs/hub/orgs.md, «Endpoints»).

func (s *Service) mountTeams(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/teams", s.Gate(s.listTeams))
	mux.Handle("POST /api/orgs/{org}/teams", s.Gate(s.createTeam))
	mux.Handle("PATCH /api/orgs/{org}/teams/{teamId}", s.Gate(s.renameTeam))
	mux.Handle("DELETE /api/orgs/{org}/teams/{teamId}", s.Gate(s.deleteTeam))
	mux.Handle("GET /api/orgs/{org}/scopes/{kind}/{id}/members", s.Gate(s.listMembers))
	mux.Handle("PUT /api/orgs/{org}/teams/{teamId}/members/{userId}", s.Gate(s.putTeamMember))
	mux.Handle("DELETE /api/orgs/{org}/teams/{teamId}/members/{userId}", s.Gate(s.deleteTeamMember))
	mux.Handle("PUT /api/orgs/{org}/grants", s.Gate(s.putGrant))
	mux.Handle("GET /api/orgs/{org}/audit", s.Gate(s.audit))
}

// chainIn is the scope's chain [org, tenant?, team?], or not found when the scope is not in the organization.
func chainIn(ctx context.Context, st store.Store, o store.Org, sc authz.Scope) ([]authz.Scope, error) {
	if sc.ID == "" {
		return nil, httpx.ErrNotFound
	}
	chain, err := authz.New(st).Chain(ctx, sc)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 || chain[0].Kind != store.ScopeOrg || chain[0].ID != o.ID {
		return nil, httpx.ErrNotFound
	}
	return chain, nil
}

// teamIn is a team of the organization, with its chain.
func teamIn(ctx context.Context, st store.Store, o store.Org, teamID string) (store.Team, []authz.Scope, error) {
	chain, err := chainIn(ctx, st, o, authz.Scope{Kind: store.ScopeTeam, ID: teamID})
	if err != nil {
		return store.Team{}, nil, err
	}
	t, err := st.TeamByID(ctx, teamID)
	if err != nil {
		return store.Team{}, nil, notFound(err)
	}
	return t, chain, nil
}

func notFound(err error) error {
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	}
	return err
}

// event is a journal entry at the bottom of chain: the chain's ids go with it, so every scope above reads it.
func event(chain []authz.Scope, actor, action, targetKind, targetID string, before, after any, at time.Time) *store.AuditEvent {
	e := &store.AuditEvent{ActorID: actor, Action: action, TargetKind: targetKind, TargetID: targetID,
		Before: jsonOf(before), After: jsonOf(after), At: at}
	for _, c := range chain {
		switch c.Kind {
		case store.ScopeOrg:
			e.OrgID = c.ID
		case store.ScopeTenant:
			e.SpaceID = c.ID
		case store.ScopeTeam:
			e.TeamID = c.ID
		}
	}
	last := chain[len(chain)-1]
	e.ScopeKind, e.ScopeID = last.Kind, last.ID
	return e
}

func (s *Service) listTeams(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	o, err := Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	tenantID := r.URL.Query().Get("tenant")
	if tenantID == "" {
		return httpx.Invalid("tenant", "Укажите тенант")
	}
	sc := authz.Scope{Kind: store.ScopeTenant, ID: tenantID}
	if _, err := chainIn(ctx, st, o, sc); err != nil {
		return err
	}
	// The tenant's viewers get every team; a plain member of the tenant gets their own (as in the organization's view).
	only := map[string]bool(nil)
	if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.View, Scope: sc}); err != nil {
		role, rerr := st.Role(ctx, tenantID, p.User.ID)
		if rerr != nil {
			return rerr
		}
		if role == "" {
			return err
		}
		mine, err := st.TeamsOfUser(ctx, p.User.ID)
		if err != nil {
			return err
		}
		only = map[string]bool{}
		for _, m := range mine {
			only[m.TeamID] = true
		}
	}
	teams, err := st.TeamsBySpace(ctx, tenantID)
	if err != nil {
		return err
	}
	out := []TeamView{}
	for _, t := range teams {
		if only != nil && !only[t.ID] {
			continue
		}
		tv, err := teamView(ctx, st, t)
		if err != nil {
			return err
		}
		out = append(out, tv)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"teams": out})
	return nil
}

func (s *Service) createTeam(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		TenantID string
		Name     string
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := CleanName(req.Name, "Назовите команду")
	if err != nil {
		return err
	}
	if req.TenantID == "" {
		return httpx.Invalid("tenantId", "Выберите тенант")
	}
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	if ok, retry := s.lim.Allow("team:"+p.User.ID, 60, time.Hour); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	sc := authz.Scope{Kind: store.ScopeTenant, ID: req.TenantID}
	now := s.now().UTC()
	var t store.Team
	// A slug taken in the tenant spoils the transaction; the whole of it is tried again with the next one.
	for n := 1; ; n++ {
		t = store.Team{SpaceID: req.TenantID, Slug: spaces.Slug(name, n), Name: name, CreatedBy: p.User.ID, CreatedAt: now}
		err = s.db.Tx(ctx, func(st store.Store) error {
			chain, err := chainIn(ctx, st, o, sc)
			if err != nil {
				return err
			}
			if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.Manage, Scope: sc}); err != nil {
				return err
			}
			if err := st.CreateTeam(ctx, &t); err != nil {
				return err
			}
			return st.AddAudit(ctx, event(append(chain, authz.Scope{Kind: store.ScopeTeam, ID: t.ID}), p.User.ID, "team.create",
				"team", t.ID, nil, map[string]any{"name": t.Name, "slug": t.Slug}, now))
		})
		if err == nil {
			break
		}
		if !store.IsUnique(err) || n > 24 {
			return err
		}
	}
	tv, err := teamView(ctx, s.db.S(), t)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"team": tv})
	return nil
}

func (s *Service) renameTeam(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ Name string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := CleanName(req.Name, "Назовите команду")
	if err != nil {
		return err
	}
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	var t store.Team
	err = s.db.Tx(ctx, func(st store.Store) error {
		var chain []authz.Scope
		var err error
		if t, chain, err = teamIn(ctx, st, o, r.PathValue("teamId")); err != nil {
			return err
		}
		if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.Manage, Scope: chain[len(chain)-1]}); err != nil {
			return err
		}
		if t.Name == name {
			return nil
		}
		was := t.Name
		t.Name = name
		if err := st.RenameTeam(ctx, t.ID, name); err != nil {
			return err
		}
		return st.AddAudit(ctx, event(chain, p.User.ID, "team.update", "team", t.ID,
			map[string]any{"name": was}, map[string]any{"name": name}, s.now().UTC()))
	})
	if err != nil {
		return err
	}
	tv, err := teamView(ctx, s.db.S(), t)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"team": tv})
	return nil
}

// deleteTeam removes a team: its machines keep their tenant and lose the team, its leads lose the role. The journal
// entry is written at the tenant, since the team's own scope is gone.
func (s *Service) deleteTeam(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	err = s.db.Tx(ctx, func(st store.Store) error {
		t, chain, err := teamIn(ctx, st, o, r.PathValue("teamId"))
		if err != nil {
			return err
		}
		tenant := chain[:len(chain)-1]
		if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.Manage, Scope: tenant[len(tenant)-1]}); err != nil {
			return err
		}
		members, err := st.TeamMembers(ctx, t.ID)
		if err != nil {
			return err
		}
		if err := st.DeleteGrantsOn(ctx, store.ScopeTeam, t.ID); err != nil {
			return err
		}
		if err := st.DeleteLimitRulesOn(ctx, store.ScopeTeam, t.ID); err != nil {
			return err
		}
		if err := st.DeleteTeam(ctx, t.ID); err != nil {
			return err
		}
		return st.AddAudit(ctx, event(tenant, p.User.ID, "team.delete", "team", t.ID,
			map[string]any{"name": t.Name, "slug": t.Slug, "memberCount": len(members)}, nil, s.now().UTC()))
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// MemberView is Member of the contract.
type MemberView struct {
	UserID       string      `json:"userId"`
	Email        string      `json:"email"`
	Name         string      `json:"name"`
	JoinedAt     string      `json:"joinedAt"`
	LastActiveAt *string     `json:"lastActiveAt"`
	Invited      bool        `json:"invited"`
	Grants       []GrantView `json:"grants"`
	Teams        []TeamRef   `json:"teams"`
	TenantRole   *string     `json:"tenantRole"`
}

// subtree is the scope and everything under it: the tenants and the teams, by key.
type subtree struct {
	keys    map[string]bool
	tenants []store.Space
	teams   []store.Team
}

func subtreeOf(ctx context.Context, st store.Store, sc authz.Scope) (subtree, error) {
	t := subtree{keys: map[string]bool{sc.Key(): true}}
	switch sc.Kind {
	case store.ScopeOrg:
		sps, err := st.SpacesByOrg(ctx, sc.ID)
		if err != nil {
			return t, err
		}
		t.tenants = sps
	case store.ScopeTenant:
		sp, err := st.SpaceByID(ctx, sc.ID)
		if err != nil {
			return t, notFound(err)
		}
		t.tenants = []store.Space{sp}
	case store.ScopeTeam:
		tm, err := st.TeamByID(ctx, sc.ID)
		if err != nil {
			return t, notFound(err)
		}
		t.teams = []store.Team{tm}
		return t, nil
	}
	for _, sp := range t.tenants {
		t.keys[store.ScopeTenant+":"+sp.ID] = true
		teams, err := st.TeamsBySpace(ctx, sp.ID)
		if err != nil {
			return t, err
		}
		for _, tm := range teams {
			t.keys[store.ScopeTeam+":"+tm.ID] = true
		}
		t.teams = append(t.teams, teams...)
	}
	return t, nil
}

// members is the people of a scope: an organization's are everyone in its tenants and its admins, a tenant's are its
// members, a team's are its people.
func members(ctx context.Context, st store.Store, sc authz.Scope) ([]MemberView, error) {
	tree, err := subtreeOf(ctx, st, sc)
	if err != nil {
		return nil, err
	}
	type row struct {
		joined time.Time
		role   string
	}
	rows := map[string]*row{}
	var order []string
	add := func(userID string, joined time.Time, role string) {
		r, ok := rows[userID]
		if !ok {
			r = &row{joined: joined}
			rows[userID] = r
			order = append(order, userID)
		}
		if !joined.IsZero() && (r.joined.IsZero() || joined.Before(r.joined)) {
			r.joined = joined
		}
		if role == store.RoleOwner || r.role == "" {
			r.role = role
		}
	}
	switch sc.Kind {
	case store.ScopeTeam:
		tm := tree.teams[0]
		people, err := st.TeamMembers(ctx, tm.ID)
		if err != nil {
			return nil, err
		}
		for _, m := range people {
			ms, err := st.MemberByUser(ctx, tm.SpaceID, m.UserID)
			if err != nil && !store.IsNotFound(err) {
				return nil, err
			}
			joined := m.CreatedAt
			if err == nil {
				joined = ms.JoinedAt
			}
			add(m.UserID, joined, ms.Role)
		}
	default:
		for _, sp := range tree.tenants {
			ms, err := st.Members(ctx, sp.ID)
			if err != nil {
				return nil, err
			}
			for _, m := range ms {
				add(m.UserID, m.JoinedAt, m.Role)
			}
		}
		if sc.Kind == store.ScopeOrg {
			admins, err := st.GrantsOn(ctx, store.ScopeOrg, sc.ID)
			if err != nil {
				return nil, err
			}
			for _, g := range admins {
				add(g.UserID, g.GrantedAt, "")
			}
		}
	}
	out := make([]MemberView, 0, len(order))
	for _, id := range order {
		u, err := st.UserByID(ctx, id)
		if err != nil {
			return nil, err
		}
		r := rows[id]
		mv := MemberView{UserID: u.ID, Email: u.Email, Name: u.Name, JoinedAt: httpx.Time(r.joined),
			LastActiveAt: httpx.TimePtr(u.LastLoginAt), Invited: u.LastLoginAt == nil, Grants: []GrantView{}, Teams: []TeamRef{}}
		if r.role != "" {
			role := r.role
			mv.TenantRole = &role
		}
		grants, err := st.GrantsOfUser(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, g := range grants {
			if tree.keys[g.ScopeKind+":"+g.ScopeID] {
				mv.Grants = append(mv.Grants, grantView(g))
			}
		}
		teams, err := st.TeamsOfUser(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, t := range teams {
			if tree.keys[store.ScopeTeam+":"+t.TeamID] {
				mv.Teams = append(mv.Teams, TeamRef{TeamID: t.TeamID, IsPrimary: t.IsPrimary})
			}
		}
		out = append(out, mv)
	}
	return out, nil
}

// inScope: the person is in the scope without managing it (a member of the tenant, a person of the team).
func inScope(ctx context.Context, st store.Store, userID string, chain []authz.Scope) (bool, error) {
	sc := chain[len(chain)-1]
	switch sc.Kind {
	case store.ScopeTeam:
		mine, err := st.TeamsOfUser(ctx, userID)
		if err != nil {
			return false, err
		}
		for _, m := range mine {
			if m.TeamID == sc.ID {
				return true, nil
			}
		}
		return false, nil
	case store.ScopeTenant:
		role, err := st.Role(ctx, sc.ID, userID)
		return role != "", err
	}
	return false, nil
}

func (s *Service) listMembers(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	o, err := Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	sc := authz.Scope{Kind: r.PathValue("kind"), ID: r.PathValue("id")}
	switch sc.Kind {
	case store.ScopeOrg, store.ScopeTenant, store.ScopeTeam:
	default:
		return httpx.ErrNotFound
	}
	chain, err := chainIn(ctx, st, o, sc)
	if err != nil {
		return err
	}
	// The scope's viewers; and a person of a team sees their teammates. Anything above that is forbidden_scope.
	if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.View, Scope: sc}); err != nil {
		in, ierr := inScope(ctx, st, p.User.ID, chain)
		if ierr != nil {
			return ierr
		}
		if !in || sc.Kind != store.ScopeTeam {
			return err
		}
	}
	ms, err := members(ctx, st, sc)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"members": ms})
	return nil
}

func memberOf(ctx context.Context, st store.Store, sc authz.Scope, userID string) (MemberView, error) {
	ms, err := members(ctx, st, sc)
	if err != nil {
		return MemberView{}, err
	}
	for _, m := range ms {
		if m.UserID == userID {
			return m, nil
		}
	}
	return MemberView{}, httpx.ErrNotFound
}

// putTeamMember adds a member of the tenant to a team. The first team of a person in a tenant becomes their primary
// one; choosing another primary team is the tenant admin's (the plan's «основную выбирает админ тенанта»).
func (s *Service) putTeamMember(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ IsPrimary *bool }
	if r.ContentLength != 0 {
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
	}
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	userID := r.PathValue("userId")
	var sc authz.Scope
	err = s.db.Tx(ctx, func(st store.Store) error {
		t, chain, err := teamIn(ctx, st, o, r.PathValue("teamId"))
		if err != nil {
			return err
		}
		sc = chain[len(chain)-1]
		az := authz.New(st)
		pr := Principal(p)
		if _, err := az.Can(ctx, pr, authz.Check{Action: authz.Manage, Scope: sc}); err != nil {
			return err
		}
		if req.IsPrimary != nil && *req.IsPrimary {
			if _, err := az.Can(ctx, pr, authz.Check{Action: authz.Manage, Scope: chain[len(chain)-2]}); err != nil {
				return err
			}
		}
		role, err := st.Role(ctx, t.SpaceID, userID)
		if err != nil {
			return err
		}
		if role == "" {
			return httpx.Invalid("userId", "Человек не состоит в тенанте")
		}
		mine, err := st.TeamsOfUser(ctx, userID)
		if err != nil {
			return err
		}
		tenantTeams, err := st.TeamsBySpace(ctx, t.SpaceID)
		if err != nil {
			return err
		}
		ofTenant := map[string]bool{}
		for _, tt := range tenantTeams {
			ofTenant[tt.ID] = true
		}
		already, hasPrimary, primaryHere := false, false, false
		for _, m := range mine {
			if m.TeamID == t.ID {
				already, primaryHere = true, m.IsPrimary
			}
			if ofTenant[m.TeamID] && m.IsPrimary {
				hasPrimary = true
			}
		}
		now := s.now().UTC()
		if !already {
			if err := st.AddTeamMember(ctx, t.ID, userID, !hasPrimary, now); err != nil {
				return err
			}
			if err := st.AddAudit(ctx, event(chain, p.User.ID, "member.add", "user", userID, nil,
				map[string]any{"teamId": t.ID, "isPrimary": !hasPrimary}, now)); err != nil {
				return err
			}
			primaryHere = !hasPrimary
		}
		if req.IsPrimary != nil && *req.IsPrimary && !primaryHere {
			if err := st.SetPrimaryTeam(ctx, userID, t.ID); err != nil {
				return err
			}
			return st.AddAudit(ctx, event(chain, p.User.ID, "member.primary", "user", userID,
				map[string]any{"isPrimary": false}, map[string]any{"isPrimary": true}, now))
		}
		return nil
	})
	if err != nil {
		return err
	}
	m, err := memberOf(ctx, s.db.S(), sc, userID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"member": m})
	return nil
}

func (s *Service) deleteTeamMember(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	userID := r.PathValue("userId")
	err = s.db.Tx(ctx, func(st store.Store) error {
		t, chain, err := teamIn(ctx, st, o, r.PathValue("teamId"))
		if err != nil {
			return err
		}
		if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.Manage, Scope: chain[len(chain)-1]}); err != nil {
			return err
		}
		mine, err := st.TeamsOfUser(ctx, userID)
		if err != nil {
			return err
		}
		for _, m := range mine {
			if m.TeamID != t.ID {
				continue
			}
			if err := st.RemoveTeamMember(ctx, t.ID, userID); err != nil {
				return err
			}
			return st.AddAudit(ctx, event(chain, p.User.ID, "member.remove", "user", userID,
				map[string]any{"teamId": t.ID, "isPrimary": m.IsPrimary}, nil, s.now().UTC()))
		}
		return nil
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// putGrant gives or takes a role (orgs.md, the rights table). Giving a tenant's admin role to someone outside the
// tenant makes them its member too; a team's lead must already be a member of the tenant.
func (s *Service) putGrant(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		UserID  string
		Scope   ScopeView
		Role    string
		Granted *bool
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.UserID == "" {
		return httpx.Invalid("userId", "Выберите человека")
	}
	if req.Granted == nil {
		return httpx.Invalid("granted", "Укажите, выдать роль или снять")
	}
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	sc := authz.Scope{Kind: req.Scope.Kind, ID: req.Scope.ID}
	switch sc.Kind {
	case store.ScopeOrg, store.ScopeTenant, store.ScopeTeam:
	default:
		return httpx.Invalid("scope", "Неизвестная область")
	}
	var out *GrantView
	err = s.db.Tx(ctx, func(st store.Store) error {
		chain, err := chainIn(ctx, st, o, sc)
		if err != nil {
			return err
		}
		target, err := st.UserByID(ctx, req.UserID)
		if store.IsNotFound(err) {
			return httpx.Invalid("userId", "Такого человека нет")
		}
		if err != nil {
			return err
		}
		action := authz.Grant
		if !*req.Granted {
			action = authz.Revoke
		}
		if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: action, Scope: sc, Target: target.ID, Role: req.Role}); err != nil {
			return err
		}
		now := s.now().UTC()
		if !*req.Granted {
			removed, err := st.Revoke(ctx, target.ID, sc.Kind, sc.ID, req.Role)
			if err != nil || !removed {
				return err
			}
			return st.AddAudit(ctx, event(chain, p.User.ID, "grant.remove", "user", target.ID,
				map[string]any{"role": req.Role}, nil, now))
		}
		switch sc.Kind {
		case store.ScopeTenant:
			role, err := st.Role(ctx, sc.ID, target.ID)
			if err != nil {
				return err
			}
			if role == "" {
				if err := st.AddMember(ctx, sc.ID, target.ID, store.RoleMember); err != nil {
					return err
				}
				if err := st.AddAudit(ctx, event(chain, p.User.ID, "member.add", "user", target.ID, nil,
					map[string]any{"tenantRole": store.RoleMember}, now)); err != nil {
					return err
				}
			}
		case store.ScopeTeam:
			tenant := chain[len(chain)-2]
			role, err := st.Role(ctx, tenant.ID, target.ID)
			if err != nil {
				return err
			}
			if role == "" {
				return httpx.Invalid("userId", "Лидом команды может быть только участник тенанта")
			}
		}
		g := store.RoleGrant{UserID: target.ID, ScopeKind: sc.Kind, ScopeID: sc.ID, Role: req.Role, GrantedBy: p.User.ID, GrantedAt: now}
		added, err := st.Grant(ctx, g)
		if err != nil {
			return err
		}
		if added {
			if err := st.AddAudit(ctx, event(chain, p.User.ID, "grant.add", "user", target.ID, nil,
				map[string]any{"role": req.Role}, now)); err != nil {
				return err
			}
		}
		held, err := st.GrantsOn(ctx, sc.Kind, sc.ID)
		if err != nil {
			return err
		}
		for _, h := range held {
			if h.UserID == target.ID && h.Role == req.Role {
				gv := grantView(h)
				out = &gv
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"grant": out})
	return nil
}

// AuditEventView is AuditEvent of the contract.
type AuditEventView struct {
	ID     string         `json:"id"`
	At     string         `json:"at"`
	Actor  *ActorView     `json:"actor"`
	Scope  ScopeView      `json:"scope"`
	Action string         `json:"action"`
	Target *TargetView    `json:"target"`
	Before map[string]any `json:"before"`
	After  map[string]any `json:"after"`
	Self   bool           `json:"self"`
}

type ActorView struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
}

type TargetView struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

const (
	auditPage    = 50
	auditMaxPage = 200
)

func (s *Service) audit(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	o, err := Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	q := r.URL.Query()
	sc := authz.Scope{Kind: store.ScopeOrg, ID: o.ID}
	if raw := q.Get("scope"); raw != "" {
		kind, id, ok := strings.Cut(raw, ":")
		if !ok {
			return httpx.Invalid("scope", "Область пишется как kind:id")
		}
		sc = authz.Scope{Kind: kind, ID: id}
	}
	switch sc.Kind {
	case store.ScopeOrg, store.ScopeTenant, store.ScopeTeam:
	default:
		return httpx.Invalid("scope", "Неизвестная область")
	}
	chain, err := chainIn(ctx, st, o, sc)
	if err != nil {
		return err
	}
	f := store.AuditFilter{Limit: auditPage}
	switch sc.Kind {
	case store.ScopeOrg:
		f.OrgID = sc.ID
	case store.ScopeTenant:
		f.SpaceID = sc.ID
	case store.ScopeTeam:
		f.TeamID = sc.ID
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > auditMaxPage {
			return httpx.Invalid("limit", "Число от 1 до 200")
		}
		f.Limit = n
	}
	// before is a time in RFC 3339, or the `next` of the previous page: that time and the last event's id.
	if raw := q.Get("before"); raw != "" {
		at, id, _ := strings.Cut(raw, "~")
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return httpx.Invalid("before", "Время в RFC 3339")
		}
		f.Before, f.BeforeID = t, id
	}
	// The scope's viewers read all of it; anyone else in the organization reads only what is about themselves.
	if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.View, Scope: sc}); err != nil {
		in, ierr := inScope(ctx, st, p.User.ID, chain)
		if ierr != nil {
			return ierr
		}
		if !in && sc.Kind != store.ScopeOrg {
			return err
		}
		f.TargetUserID = p.User.ID
	}
	evs, err := st.Audit(ctx, f)
	if err != nil {
		return err
	}
	labels := labeler{st: st, cache: map[string]string{}}
	out := struct {
		Events []AuditEventView `json:"events"`
		Next   *string          `json:"next"`
	}{Events: []AuditEventView{}}
	for _, e := range evs {
		v := AuditEventView{ID: e.ID, At: e.At.UTC().Format(time.RFC3339Nano), Scope: ScopeView{e.ScopeKind, e.ScopeID},
			Action: e.Action, Before: objOf(e.Before), After: objOf(e.After), Self: e.Self}
		if e.ActorID != "" {
			v.Actor = &ActorView{UserID: e.ActorID, Name: labels.get(ctx, "user", e.ActorID)}
		}
		if e.TargetKind != "" {
			v.Target = &TargetView{Kind: e.TargetKind, ID: e.TargetID, Label: labels.get(ctx, e.TargetKind, e.TargetID)}
			if e.TargetKind == "limit" {
				v.Target.Label = labels.limit(ctx, v.After, v.Before, e.TargetID)
			}
		}
		out.Events = append(out.Events, v)
	}
	if len(evs) == f.Limit {
		last := evs[len(evs)-1]
		next := last.At.UTC().Format(time.RFC3339Nano) + "~" + last.ID
		out.Next = &next
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// labeler names the journal's actors and targets; a target that is gone keeps its id as the label.
type labeler struct {
	st    store.Store
	cache map[string]string
}

func (l labeler) get(ctx context.Context, kind, id string) string {
	key := kind + ":" + id
	if v, ok := l.cache[key]; ok {
		return v
	}
	label := id
	switch kind {
	case "user":
		if u, err := l.st.UserByID(ctx, id); err == nil {
			label = u.Name
			if label == "" {
				label = u.Email
			}
		}
	case "team":
		if t, err := l.st.TeamByID(ctx, id); err == nil {
			label = t.Name
		}
	case "tenant":
		if sp, err := l.st.SpaceByID(ctx, id); err == nil {
			label = sp.Name
		}
	case "org":
		if o, err := l.st.OrgByID(ctx, id); err == nil {
			label = o.Name
		}
	case "machine":
		if m, err := l.st.MachineByID(ctx, id); err == nil {
			label = m.Name
		}
	case "change":
		if c, err := l.st.ChangeByID(ctx, id); err == nil {
			label = c.Title
		}
	case "signal":
		if g, err := l.st.SignalByID(ctx, id); err == nil {
			label = g.Title
		}
	}
	l.cache[key] = label
	return label
}

// limit names a limit rule of the journal by whom it holds: the person of a personal limit, otherwise the scope's name.
func (l labeler) limit(ctx context.Context, after, before map[string]any, id string) string {
	rule := after
	if rule == nil {
		rule = before
	}
	if rule == nil {
		return id
	}
	if uid, _ := rule["userId"].(string); uid != "" {
		return l.get(ctx, "user", uid)
	}
	if sc, _ := rule["scope"].(string); sc != "" {
		if kind, sid, ok := strings.Cut(sc, ":"); ok {
			return l.get(ctx, kind, sid)
		}
	}
	return id
}

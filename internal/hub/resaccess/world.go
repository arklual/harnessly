package resaccess

import (
	"context"
	"errors"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// personTeams is what the hub knows of a person's teams: the teams they are in (and lead) and the organizations they
// administer. A person who never signed in has none.
type personTeams struct {
	email string
	user  string
	name  string
	teams map[string]teamPlace
	admin map[string]bool
}

type teamPlace struct {
	lead bool
	org  string
}

type teamRef struct{ ID, Name, OrgID string }

// world answers team questions during one request and caches what it read.
type world struct {
	ctx    context.Context
	s      store.Store
	people map[string]personTeams
	teams  map[string]*teamRef
	orgs   map[string]string // space id -> org key
}

func newWorld(ctx context.Context, s store.Store) *world {
	return &world{ctx: ctx, s: s, people: map[string]personTeams{}, teams: map[string]*teamRef{}, orgs: map[string]string{}}
}

func (w *world) orgOf(spaceID string) string {
	if v, ok := w.orgs[spaceID]; ok {
		return v
	}
	key := "space:" + spaceID
	if sp, err := w.s.SpaceByID(w.ctx, spaceID); err == nil && sp.OrgID != "" {
		key = sp.OrgID
	}
	w.orgs[spaceID] = key
	return key
}

// team is a hub team by id.
func (w *world) team(id string) (*teamRef, bool) {
	if t, ok := w.teams[id]; ok {
		return t, t != nil
	}
	t, err := w.s.TeamByID(w.ctx, id)
	if err != nil {
		w.teams[id] = nil
		return nil, false
	}
	ref := &teamRef{ID: t.ID, Name: t.Name, OrgID: w.orgOf(t.SpaceID)}
	w.teams[id] = ref
	return ref, true
}

func (w *world) person(email string) (personTeams, error) {
	email = normEmail(email)
	if p, ok := w.people[email]; ok {
		return p, nil
	}
	p := personTeams{email: email, teams: map[string]teamPlace{}, admin: map[string]bool{}}
	u, err := w.s.UserByEmail(w.ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		w.people[email] = p
		return p, nil
	}
	if err != nil {
		return p, err
	}
	p.user, p.name = u.ID, strings.TrimSpace(u.Name)
	grants, err := w.s.GrantsOfUser(w.ctx, u.ID)
	if err != nil {
		return p, err
	}
	leads := map[string]bool{}
	for _, g := range grants {
		switch {
		case g.ScopeKind == store.ScopeTeam && g.Role == store.RoleLead:
			leads[g.ScopeID] = true
		case g.ScopeKind == store.ScopeOrg && g.Role == store.RoleAdmin:
			p.admin[g.ScopeID] = true
		}
	}
	mine, err := w.s.TeamsOfUser(w.ctx, u.ID)
	if err != nil {
		return p, err
	}
	for _, tm := range mine {
		t, ok := w.team(tm.TeamID)
		if !ok {
			continue
		}
		p.teams[t.ID] = teamPlace{lead: leads[t.ID], org: t.OrgID}
	}
	w.people[email] = p
	return p, nil
}

// displayName is the name a person goes by, or the part of the address before the @.
func (w *world) displayName(email string) string {
	if p, err := w.person(email); err == nil && p.name != "" {
		return p.name
	}
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return email
}

// homeOrg is the organization of a resource's home team ("" when it has none).
func (w *world) homeOrg(team string) string {
	if team == "" {
		return ""
	}
	if t, ok := w.team(team); ok {
		return t.OrgID
	}
	return ""
}

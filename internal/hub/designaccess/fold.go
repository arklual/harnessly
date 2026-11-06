package designaccess

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The one rule of access: what a person may do with a project right now.
//
// Role = the strongest of: the owner; the person's own grant (until it runs out); the home team's level when the person is
// in the home team; a role given to another team the person is in (until it runs out). A grant «edit + may share» is the
// role manage.
//
// Who runs the access (canManage): the owner; manage; an editor when the project lets editors share; and for a team
// project (its home team has a level) the home team's lead and the admin of its organization — the steward — without a
// role of their own in it.

// project is a stored project row.
type project struct {
	ID              string
	Owner           string // "" when nobody owns it
	OwnerName       string
	HomeTeam        string // "" for a row whose level counts for everyone who may open the machine
	TeamLevel       string
	EditorsCanShare bool
	Legacy          bool
}

type member struct {
	Email     string
	Role      string
	GrantedBy string
	GrantedAt time.Time
	Expires   *time.Time
	ViaInvite string
}

type teamGrant struct {
	Team      string
	Role      string
	CanShare  bool
	GrantedBy string
	GrantedAt time.Time
	Expires   *time.Time
}

type folded struct {
	Role      string // "" when the project is closed to the person
	Until     *time.Time
	CanManage bool
	Steward   bool
}

func foldAccess(p project, m *member, grants []teamGrant, person personTeams, homeOrg string, now time.Time) folded {
	if p.Owner != "" && p.Owner == person.email {
		return folded{Role: roleOwner, CanManage: true}
	}
	own := ""
	if m != nil && live(m.Expires, now) {
		own = m.Role
	}
	teamOn := p.TeamLevel != levelNone
	_, inHome := person.teams[p.HomeTeam]
	if p.HomeTeam == "" {
		inHome = true
	}
	shared := ""
	if teamOn && inHome {
		shared = p.TeamLevel
	}
	for _, g := range grants {
		if _, in := person.teams[g.Team]; !in || !live(g.Expires, now) {
			continue
		}
		r := g.Role
		if r == roleEdit && g.CanShare {
			r = roleManage
		}
		shared = stronger(shared, r)
	}
	role := stronger(own, shared)
	ownWins := own != "" && (shared == "" || rank(own) > rank(shared))
	byRole := role == roleManage || (role == roleEdit && p.EditorsCanShare)
	steward := false
	if teamOn && p.HomeTeam != "" {
		steward = person.teams[p.HomeTeam].lead || (homeOrg != "" && person.admin[homeOrg])
	}
	f := folded{Role: role, CanManage: byRole || steward, Steward: steward}
	if ownWins && m != nil {
		f.Until = m.Expires
	}
	return f
}

// ---- who is in which team -------------------------------------------------------------------------------------------

// A machine of a hub is shared by its space. Besides the hub's own teams (orgs.go of the store), every member of the
// machine's space is in the space's implicit team "space:<id>", led by the space's owner: so a project lives in its
// maker's team even on a hub with no teams, and is open to the space as on the platform the page came from.

const implicitPrefix = "space:"

type teamPlace struct {
	lead, primary bool
	org, space    string
}

type personTeams struct {
	email string
	user  string // hub user id, "" when the address never signed in
	name  string
	teams map[string]teamPlace
	admin map[string]bool
}

// world answers team questions for one machine during one request; it caches what it read.
type world struct {
	ctx     context.Context
	s       store.Store
	machine store.Machine
	space   store.Space
	people  map[string]personTeams
	spaces  map[string]store.Space
	orgs    map[string]string
}

func newWorld(ctx context.Context, s store.Store, m store.Machine) (*world, error) {
	sp, err := s.SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return nil, err
	}
	return &world{ctx: ctx, s: s, machine: m, space: sp, people: map[string]personTeams{}, spaces: map[string]store.Space{sp.ID: sp}, orgs: map[string]string{}}, nil
}

func orgKey(sp store.Space) string {
	if sp.OrgID != "" {
		return sp.OrgID
	}
	return implicitPrefix + sp.ID
}

func (w *world) spaceOf(id string) (store.Space, error) {
	if sp, ok := w.spaces[id]; ok {
		return sp, nil
	}
	sp, err := w.s.SpaceByID(w.ctx, id)
	if err != nil {
		return store.Space{}, err
	}
	w.spaces[id] = sp
	return sp, nil
}

func (w *world) implicitTeam() string { return implicitPrefix + w.space.ID }

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
		t, err := w.s.TeamByID(w.ctx, tm.TeamID)
		if err != nil {
			continue
		}
		sp, err := w.spaceOf(t.SpaceID)
		if err != nil {
			continue
		}
		p.teams[t.ID] = teamPlace{lead: leads[t.ID], primary: tm.IsPrimary, org: orgKey(sp), space: sp.ID}
	}
	if role, err := w.s.Role(w.ctx, w.space.ID, u.ID); err == nil {
		p.teams[w.implicitTeam()] = teamPlace{lead: role == store.RoleOwner, org: orgKey(w.space), space: w.space.ID}
		if role == store.RoleOwner && w.space.OrgID == "" {
			p.admin[orgKey(w.space)] = true
		}
	}
	w.people[email] = p
	return p, nil
}

// homeTeamOf is the team a person's new project lives in: their primary team in the machine's space, else their first
// team there, else the space itself; "" for an address that may not open the machine.
func (w *world) homeTeamOf(email string) (string, error) {
	p, err := w.person(email)
	if err != nil {
		return "", err
	}
	var ids []string
	for id, place := range p.teams {
		if place.space == w.space.ID && !strings.HasPrefix(id, implicitPrefix) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := p.teams[ids[i]], p.teams[ids[j]]
		if a.primary != b.primary {
			return a.primary
		}
		return ids[i] < ids[j]
	})
	if len(ids) > 0 {
		return ids[0], nil
	}
	if _, ok := p.teams[w.implicitTeam()]; ok {
		return w.implicitTeam(), nil
	}
	return "", nil
}

// orgOf is the organization of a team, "" when it is unknown.
func (w *world) orgOf(team string) string {
	if team == "" {
		return ""
	}
	if o, ok := w.orgs[team]; ok {
		return o
	}
	org := ""
	if strings.HasPrefix(team, implicitPrefix) {
		if sp, err := w.spaceOf(strings.TrimPrefix(team, implicitPrefix)); err == nil {
			org = orgKey(sp)
		}
	} else if t, err := w.s.TeamByID(w.ctx, team); err == nil {
		if sp, err := w.spaceOf(t.SpaceID); err == nil {
			org = orgKey(sp)
		}
	}
	w.orgs[team] = org
	return org
}

// displayName is the person's name as the hub knows it, else the local part of the address.
func (w *world) displayName(email string) string {
	if p, err := w.person(email); err == nil && p.name != "" {
		return p.name
	}
	return localPart(email)
}

// mayOpen says whether an address may open the machine at all: a member of its space.
func (w *world) mayOpen(email string) bool {
	p, err := w.person(email)
	if err != nil {
		return false
	}
	_, ok := p.teams[w.implicitTeam()]
	return ok
}

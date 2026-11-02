// Package seed makes organizations, tenants, teams, people and roles from a file (`hub seed`, docs/hub/orgs.md,
// «Seeding»). It only adds and updates, by natural keys, in one transaction: a second run changes nothing, and a dry run
// is the same run rolled back.
package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// File is the seed file.
type File struct {
	Orgs []Org `yaml:"orgs"`
}

type Org struct {
	Slug     string   `yaml:"slug"`
	Name     string   `yaml:"name"`
	Timezone string   `yaml:"timezone"`
	Currency string   `yaml:"currency"`
	Admins   []string `yaml:"admins"`
	Tenants  []Tenant `yaml:"tenants"`
}

type Tenant struct {
	Slug    string   `yaml:"slug"`
	Name    string   `yaml:"name"`
	Owners  []string `yaml:"owners"`
	Members []string `yaml:"members"`
	Teams   []Team   `yaml:"teams"`
}

type Team struct {
	Slug    string   `yaml:"slug"`
	Name    string   `yaml:"name"`
	Leads   []string `yaml:"leads"`
	Members []string `yaml:"members"`
}

var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])$`)

// Parse reads and checks a seed file: unknown keys, bad slugs and addresses, a tenant without an owner, a team member
// who is not in the tenant are errors.
func Parse(r io.Reader) (File, error) {
	var f File
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("seed file: %w", err)
	}
	return f, f.check()
}

func (f *File) check() error {
	orgSlugs, spaceSlugs := map[string]bool{}, map[string]bool{}
	for i := range f.Orgs {
		o := &f.Orgs[i]
		where := fmt.Sprintf("orgs[%d]", i)
		if err := checkNamed(where, o.Slug, o.Name); err != nil {
			return err
		}
		if orgSlugs[o.Slug] {
			return fmt.Errorf("%s: slug %q twice", where, o.Slug)
		}
		orgSlugs[o.Slug] = true
		if o.Timezone != "" {
			if _, err := time.LoadLocation(o.Timezone); err != nil || o.Timezone == "Local" {
				return fmt.Errorf("%s: unknown timezone %q", where, o.Timezone)
			}
		}
		if o.Currency != "" && o.Currency != "RUB" && o.Currency != "USD" {
			return fmt.Errorf("%s: currency must be RUB or USD", where)
		}
		var err error
		if o.Admins, err = emails(where+".admins", o.Admins); err != nil {
			return err
		}
		for j := range o.Tenants {
			t := &o.Tenants[j]
			tw := fmt.Sprintf("%s.tenants[%d]", where, j)
			if err := checkNamed(tw, t.Slug, t.Name); err != nil {
				return err
			}
			if spaceSlugs[t.Slug] {
				return fmt.Errorf("%s: slug %q twice", tw, t.Slug)
			}
			spaceSlugs[t.Slug] = true
			if t.Owners, err = emails(tw+".owners", t.Owners); err != nil {
				return err
			}
			if len(t.Owners) == 0 {
				return fmt.Errorf("%s: a tenant needs an owner", tw)
			}
			if t.Members, err = emails(tw+".members", t.Members); err != nil {
				return err
			}
			in := map[string]bool{}
			for _, e := range append(append([]string{}, t.Owners...), t.Members...) {
				in[e] = true
			}
			teamSlugs := map[string]bool{}
			for k := range t.Teams {
				m := &t.Teams[k]
				mw := fmt.Sprintf("%s.teams[%d]", tw, k)
				if err := checkNamed(mw, m.Slug, m.Name); err != nil {
					return err
				}
				if teamSlugs[m.Slug] {
					return fmt.Errorf("%s: slug %q twice", mw, m.Slug)
				}
				teamSlugs[m.Slug] = true
				if m.Leads, err = emails(mw+".leads", m.Leads); err != nil {
					return err
				}
				if m.Members, err = emails(mw+".members", m.Members); err != nil {
					return err
				}
				for _, e := range append(append([]string{}, m.Leads...), m.Members...) {
					if !in[e] {
						return fmt.Errorf("%s: %s is not an owner or member of the tenant", mw, e)
					}
				}
			}
		}
	}
	return nil
}

func checkNamed(where, slug, name string) error {
	if !slugRe.MatchString(slug) {
		return fmt.Errorf("%s: slug %q must be 3–40 of [a-z0-9-]", where, slug)
	}
	if n := len([]rune(strings.TrimSpace(name))); n == 0 || n > 60 {
		return fmt.Errorf("%s: name must be 1–60 characters", where)
	}
	return nil
}

// emails lowers and checks the addresses of a list, dropping repeats.
func emails(where string, in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range in {
		e := strings.ToLower(strings.TrimSpace(raw))
		if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
			return nil, fmt.Errorf("%s: %q is not an email address", where, raw)
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out, nil
}

var errDryRun = errors.New("dry run")

// Apply makes the file true in the database and returns what it changed, one line each (none: nothing to do). With
// dry it changes nothing and returns what it would change.
func Apply(ctx context.Context, db *store.DB, f File, dry bool, now time.Time) ([]string, error) {
	var changes []string
	err := db.Tx(ctx, func(st store.Store) error {
		a := &applier{ctx: ctx, st: st, now: now.UTC(), users: map[string]store.User{}}
		for _, o := range f.Orgs {
			if err := a.org(o); err != nil {
				return err
			}
		}
		changes = a.changes
		if dry {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	return changes, err
}

type applier struct {
	ctx     context.Context
	st      store.Store
	now     time.Time
	users   map[string]store.User
	changes []string
}

func (a *applier) note(format string, args ...any) {
	a.changes = append(a.changes, fmt.Sprintf(format, args...))
}

func (a *applier) audit(orgID, spaceID, teamID, scopeKind, scopeID, action, targetKind, targetID string, after any) error {
	return a.st.AddAudit(a.ctx, &store.AuditEvent{OrgID: orgID, SpaceID: spaceID, TeamID: teamID, ScopeKind: scopeKind, ScopeID: scopeID,
		Action: action, TargetKind: targetKind, TargetID: targetID, After: jsonOf(after), At: a.now})
}

// user is the person with the address, made when they never signed in (signing in by the address later joins them).
func (a *applier) user(email string) (store.User, error) {
	if u, ok := a.users[email]; ok {
		return u, nil
	}
	u, err := a.st.UserByEmail(a.ctx, email)
	if store.IsNotFound(err) {
		u = store.User{Email: email, CreatedAt: a.now}
		if err = a.st.CreateUser(a.ctx, &u); err == nil {
			a.note("+ user %s", email)
		}
	}
	if err != nil {
		return store.User{}, err
	}
	a.users[email] = u
	return u, nil
}

func (a *applier) grant(email, kind, id, role, orgID, spaceID, teamID, label string) error {
	u, err := a.user(email)
	if err != nil {
		return err
	}
	added, err := a.st.Grant(a.ctx, store.RoleGrant{UserID: u.ID, ScopeKind: kind, ScopeID: id, Role: role, GrantedAt: a.now})
	if err != nil || !added {
		return err
	}
	a.note("+ %s %s of %s", role, email, label)
	return a.audit(orgID, spaceID, teamID, kind, id, "grant.add", "user", u.ID, map[string]any{"role": role})
}

func (a *applier) org(in Org) error {
	o, err := a.st.OrgBySlug(a.ctx, in.Slug)
	switch {
	case store.IsNotFound(err):
		o = store.Org{Slug: in.Slug, Name: strings.TrimSpace(in.Name), Timezone: in.Timezone, Currency: in.Currency, CreatedAt: a.now}
		if err := a.st.CreateOrg(a.ctx, &o); err != nil {
			return err
		}
		a.note("+ org %s (%s)", o.Slug, o.Name)
		if err := a.audit(o.ID, "", "", store.ScopeOrg, o.ID, "org.create", "org", o.ID, map[string]any{"name": o.Name, "slug": o.Slug}); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		next := o
		next.Name = strings.TrimSpace(in.Name)
		if in.Timezone != "" {
			next.Timezone = in.Timezone
		}
		if in.Currency != "" {
			next.Currency = in.Currency
		}
		if next != o {
			if err := a.st.UpdateOrg(a.ctx, next); err != nil {
				return err
			}
			a.note("~ org %s", o.Slug)
			if err := a.audit(o.ID, "", "", store.ScopeOrg, o.ID, "org.update", "org", o.ID,
				map[string]any{"name": next.Name, "timezone": next.Timezone, "currency": next.Currency}); err != nil {
				return err
			}
			o = next
		}
	}
	for _, e := range in.Admins {
		if err := a.grant(e, store.ScopeOrg, o.ID, store.RoleAdmin, o.ID, "", "", "org "+o.Slug); err != nil {
			return err
		}
	}
	for _, t := range in.Tenants {
		if err := a.tenant(o, t); err != nil {
			return err
		}
	}
	return nil
}

func (a *applier) tenant(o store.Org, in Tenant) error {
	name := strings.TrimSpace(in.Name)
	sp, err := a.st.SpaceBySlug(a.ctx, in.Slug)
	switch {
	case store.IsNotFound(err):
		owner, err := a.user(in.Owners[0])
		if err != nil {
			return err
		}
		sp = store.Space{Slug: in.Slug, Name: name, CreatedBy: owner.ID, CreatedAt: a.now, OrgID: o.ID}
		if err := a.st.CreateSpace(a.ctx, &sp); err != nil {
			return err
		}
		a.note("+ tenant %s (%s) in org %s", sp.Slug, sp.Name, o.Slug)
		if err := a.audit(o.ID, sp.ID, "", store.ScopeOrg, o.ID, "tenant.move", "tenant", sp.ID, map[string]any{"orgId": o.ID}); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if sp.OrgID != o.ID {
			if err := a.st.SetSpaceOrg(a.ctx, sp.ID, o.ID); err != nil {
				return err
			}
			a.note("~ tenant %s moved into org %s", sp.Slug, o.Slug)
			for _, in := range []string{o.ID, sp.OrgID} {
				if in == "" {
					continue
				}
				if err := a.audit(in, sp.ID, "", store.ScopeOrg, in, "tenant.move", "tenant", sp.ID, map[string]any{"orgId": o.ID}); err != nil {
					return err
				}
			}
			sp.OrgID = o.ID
		}
		if sp.Name != name {
			if err := a.st.UpdateSpace(a.ctx, sp.ID, name, sp.OnboardingStep); err != nil {
				return err
			}
			a.note("~ tenant %s renamed", sp.Slug)
		}
	}
	for _, list := range []struct {
		role   string
		emails []string
	}{{store.RoleOwner, in.Owners}, {store.RoleMember, in.Members}} {
		for _, e := range list.emails {
			if err := a.member(o, sp, e, list.role); err != nil {
				return err
			}
		}
	}
	for _, t := range in.Teams {
		if err := a.team(o, sp, t); err != nil {
			return err
		}
	}
	return nil
}

// member makes the person a member of the space with at least the role (an owner is never made a member).
func (a *applier) member(o store.Org, sp store.Space, email, role string) error {
	u, err := a.user(email)
	if err != nil {
		return err
	}
	had, err := a.st.Role(a.ctx, sp.ID, u.ID)
	if err != nil {
		return err
	}
	switch {
	case had == "":
		err = a.st.AddMember(a.ctx, sp.ID, u.ID, role)
	case had == store.RoleMember && role == store.RoleOwner:
		err = a.st.SetRole(a.ctx, sp.ID, u.ID, role)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	a.note("+ %s %s of tenant %s", role, email, sp.Slug)
	return a.audit(o.ID, sp.ID, "", store.ScopeTenant, sp.ID, "member.add", "user", u.ID, map[string]any{"role": role})
}

func (a *applier) team(o store.Org, sp store.Space, in Team) error {
	name := strings.TrimSpace(in.Name)
	t, err := a.st.TeamBySlug(a.ctx, sp.ID, in.Slug)
	switch {
	case store.IsNotFound(err):
		t = store.Team{SpaceID: sp.ID, Slug: in.Slug, Name: name, CreatedAt: a.now}
		if err := a.st.CreateTeam(a.ctx, &t); err != nil {
			return err
		}
		a.note("+ team %s (%s) in tenant %s", t.Slug, t.Name, sp.Slug)
		if err := a.audit(o.ID, sp.ID, t.ID, store.ScopeTenant, sp.ID, "team.create", "team", t.ID, map[string]any{"name": t.Name, "slug": t.Slug}); err != nil {
			return err
		}
	case err != nil:
		return err
	case t.Name != name:
		if err := a.st.RenameTeam(a.ctx, t.ID, name); err != nil {
			return err
		}
		a.note("~ team %s renamed", t.Slug)
	}
	members, err := a.st.TeamMembers(a.ctx, t.ID)
	if err != nil {
		return err
	}
	inTeam := map[string]bool{}
	for _, m := range members {
		inTeam[m.UserID] = true
	}
	all := append(append([]string{}, in.Leads...), in.Members...)
	for _, e := range all {
		u, err := a.user(e)
		if err != nil {
			return err
		}
		if inTeam[u.ID] {
			continue
		}
		inTeam[u.ID] = true
		primary, err := a.firstTeamIn(sp.ID, u.ID)
		if err != nil {
			return err
		}
		if err := a.st.AddTeamMember(a.ctx, t.ID, u.ID, primary, a.now); err != nil {
			return err
		}
		a.note("+ %s in team %s of tenant %s", e, t.Slug, sp.Slug)
		if err := a.audit(o.ID, sp.ID, t.ID, store.ScopeTeam, t.ID, "member.add", "user", u.ID, map[string]any{"isPrimary": primary}); err != nil {
			return err
		}
	}
	for _, e := range in.Leads {
		if err := a.grant(e, store.ScopeTeam, t.ID, store.RoleLead, o.ID, sp.ID, t.ID, "team "+t.Slug); err != nil {
			return err
		}
	}
	return nil
}

// firstTeamIn says whether the person is in no team of the tenant yet: their first one there is their primary.
func (a *applier) firstTeamIn(spaceID, userID string) (bool, error) {
	teams, err := a.st.TeamsOfUser(a.ctx, userID)
	if err != nil {
		return false, err
	}
	for _, m := range teams {
		t, err := a.st.TeamByID(a.ctx, m.TeamID)
		if err != nil {
			return false, err
		}
		if t.SpaceID == spaceID {
			return false, nil
		}
	}
	return true, nil
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

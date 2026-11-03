// Package seed makes organizations, tenants, teams, people and roles from a file (`hub seed`, docs/hub/orgs.md,
// «Seeding»). It adds and updates by natural keys, in one transaction: a second run changes nothing, and a dry run is the
// same run rolled back. The one thing it takes away is a person listed under offboard: every role, tenant, team and
// session they have.
package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

// File is the seed file.
type File struct {
	Orgs []Org `yaml:"orgs"`
	// Offboard lists people to take out of the installation: their roles, tenants, teams and sessions go, the user row
	// stays (their history and audit point at it). Signing in afterwards finds nothing to open.
	Offboard []string `yaml:"offboard"`
}

type Org struct {
	Slug     string    `yaml:"slug"`
	Name     string    `yaml:"name"`
	Timezone string    `yaml:"timezone"`
	Currency string    `yaml:"currency"`
	Admins   []string  `yaml:"admins"`
	Tenants  []Tenant  `yaml:"tenants"`
	Accounts []Account `yaml:"accounts"`
}

// Account registers a model account the organization's machines report: who pays for it and what it costs a month. A
// machine names an account it has not seen before personal; this is how an admin says the organization pays.
type Account struct {
	// Provider is the CLI the account is used with: claude or codex.
	Provider string `yaml:"provider"`
	// Ref is the account as the machines report it: claude:<account uuid>, codex:<account id>, key:<key hash>.
	Ref   string `yaml:"ref"`
	Label string `yaml:"label"`
	Plan  string `yaml:"plan"`
	// Funding is who pays: org (the default) or personal.
	Funding string `yaml:"funding"`
	// MonthlyFee is a subscription's fee in USD a month, split over its use on the money screens.
	MonthlyFee float64 `yaml:"monthlyFee"`
	Owner      string  `yaml:"owner"`
	// Tenant narrows the account to one tenant of the organization; empty keeps the scope it has (the organization for
	// a new one).
	Tenant string `yaml:"tenant"`
	// History gives the account the owner's sessions of this CLI that came with no account: those the machine had
	// before it first reported which account it signs in with.
	History bool `yaml:"history"`
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
	var err error
	if f.Offboard, err = emails("offboard", f.Offboard); err != nil {
		return err
	}
	leaving := map[string]bool{}
	for _, e := range f.Offboard {
		leaving[e] = true
	}
	if err := f.checkOrgs(); err != nil {
		return err
	}
	for _, o := range f.Orgs {
		for _, e := range o.people() {
			if leaving[e] {
				return fmt.Errorf("offboard: %s is also given a role in org %s", e, o.Slug)
			}
		}
	}
	return nil
}

// people is every address the org's part of the file gives a role.
func (o Org) people() []string {
	out := append([]string{}, o.Admins...)
	for _, x := range o.Accounts {
		if x.Owner != "" {
			out = append(out, x.Owner)
		}
	}
	for _, t := range o.Tenants {
		out = append(append(out, t.Owners...), t.Members...)
		for _, m := range t.Teams {
			out = append(append(out, m.Leads...), m.Members...)
		}
	}
	return out
}

func (f *File) checkOrgs() error {
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
		if err := o.checkAccounts(where); err != nil {
			return err
		}
	}
	return nil
}

var refRe = regexp.MustCompile(`^(claude|codex|key):[A-Za-z0-9._-]{1,100}$`)

func (o *Org) checkAccounts(where string) error {
	refs := map[string]bool{}
	for i := range o.Accounts {
		x := &o.Accounts[i]
		aw := fmt.Sprintf("%s.accounts[%d]", where, i)
		if x.Provider != "claude" && x.Provider != "codex" {
			return fmt.Errorf("%s: provider must be claude or codex", aw)
		}
		if !refRe.MatchString(x.Ref) {
			return fmt.Errorf("%s: ref %q must be claude:<id>, codex:<id> or key:<hash>", aw, x.Ref)
		}
		if refs[x.Provider+" "+x.Ref] {
			return fmt.Errorf("%s: ref %q twice", aw, x.Ref)
		}
		refs[x.Provider+" "+x.Ref] = true
		x.Label, x.Plan = strings.TrimSpace(x.Label), strings.TrimSpace(x.Plan)
		if len([]rune(x.Label)) > 60 || len([]rune(x.Plan)) > 60 {
			return fmt.Errorf("%s: label and plan are at most 60 characters", aw)
		}
		switch x.Funding {
		case "":
			x.Funding = store.FundingOrg
		case store.FundingOrg, store.FundingPersonal:
		default:
			return fmt.Errorf("%s: funding must be org or personal", aw)
		}
		if x.MonthlyFee < 0 || x.MonthlyFee > 1e6 || math.IsNaN(x.MonthlyFee) {
			return fmt.Errorf("%s: monthlyFee must be 0–1000000 USD", aw)
		}
		if x.Owner != "" {
			e, err := emails(aw+".owner", []string{x.Owner})
			if err != nil {
				return err
			}
			x.Owner = e[0]
		}
		if x.History && x.Owner == "" {
			return fmt.Errorf("%s: history needs an owner", aw)
		}
		if x.Tenant != "" && !slugRe.MatchString(x.Tenant) {
			return fmt.Errorf("%s: tenant %q is not a slug", aw, x.Tenant)
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
		for _, e := range f.Offboard {
			if err := a.offboard(e); err != nil {
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
	for _, x := range in.Accounts {
		if err := a.account(o, x); err != nil {
			return err
		}
	}
	return nil
}

// account registers the account in the organization, gives its funding to its sessions, gives it the owner's sessions
// without an account when asked, and sums anew the hours whose money that moved.
func (a *applier) account(o store.Org, in Account) error {
	orgKey := o.ID
	provider := usage.PriceProvider(in.Provider, "")
	kind := "subscription"
	if strings.HasPrefix(in.Ref, "key:") {
		kind = "api_key"
	}
	ownerID := ""
	if in.Owner != "" {
		u, err := a.user(in.Owner)
		if err != nil {
			return err
		}
		ownerID = u.ID
	}
	scopeKind, scopeID := "", ""
	if in.Tenant != "" {
		sp, err := a.st.SpaceBySlug(a.ctx, in.Tenant)
		if store.IsNotFound(err) || (err == nil && sp.OrgID != o.ID) {
			return fmt.Errorf("account %s: tenant %s is not in org %s", in.Ref, in.Tenant, o.Slug)
		}
		if err != nil {
			return err
		}
		scopeKind, scopeID = store.ScopeTenant, sp.ID
	}
	fee := int64(math.Round(in.MonthlyFee * 1e6))
	cur, err := a.st.AccountByRef(a.ctx, orgKey, provider, in.Ref)
	switch {
	case store.IsNotFound(err):
		cur = store.Account{OrgKey: orgKey, ScopeKind: def(scopeKind, store.ScopeOrg), ScopeID: def(scopeID, o.ID), Provider: provider, Kind: kind,
			Plan: in.Plan, AccountRef: in.Ref, Label: in.Label, Funding: in.Funding, OwnerUserID: ownerID, MonthlyFeeMicros: fee, CreatedAt: a.now}
		if err := a.st.CreateAccount(a.ctx, &cur); err != nil {
			return err
		}
		a.note("+ account %s %s (%s) in org %s", in.Provider, in.Ref, cur.Funding, o.Slug)
		if err := a.audit(o.ID, "", "", store.ScopeOrg, o.ID, "account.create", "account", cur.ID, accountAfter(cur)); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		next := cur
		next.Label, next.Plan, next.Kind, next.Funding, next.MonthlyFeeMicros, next.Discovered = in.Label, in.Plan, kind, in.Funding, fee, false
		if ownerID != "" {
			next.OwnerUserID = ownerID
		}
		if scopeKind != "" {
			next.ScopeKind, next.ScopeID = scopeKind, scopeID
		}
		if next != cur {
			if err := a.st.UpdateAccount(a.ctx, next); err != nil {
				return err
			}
			a.note("~ account %s %s (%s) in org %s", in.Provider, in.Ref, next.Funding, o.Slug)
			if err := a.audit(o.ID, "", "", store.ScopeOrg, o.ID, "account.update", "account", cur.ID, accountAfter(next)); err != nil {
				return err
			}
			cur = next
		}
	}
	moved, err := a.st.SetAccountFunding(a.ctx, orgKey, cur.ID, cur.Funding)
	if err != nil {
		return err
	}
	if len(moved) > 0 {
		a.note("~ %d sessions of account %s %s now %s", len(moved), in.Provider, in.Ref, cur.Funding)
	}
	if in.History {
		keys, err := a.st.SessionsWithoutAccount(a.ctx, orgKey, ownerID, in.Provider)
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := a.st.SetSessionsAccount(a.ctx, orgKey, in.Provider, keys, cur.ID, cur.Funding); err != nil {
				return err
			}
			a.note("~ %d sessions of %s without an account given account %s %s", len(keys), in.Owner, in.Provider, in.Ref)
			moved = append(moved, keys...)
		}
	}
	if len(moved) == 0 {
		return nil
	}
	hours, err := a.st.CallHours(a.ctx, orgKey, moved)
	if err != nil {
		return err
	}
	return a.st.Rollup(a.ctx, orgKey, hours)
}

func accountAfter(x store.Account) map[string]any {
	return map[string]any{"provider": x.Provider, "label": x.Label, "plan": x.Plan, "kind": x.Kind, "funding": x.Funding,
		"monthlyFeeMicros": x.MonthlyFeeMicros, "scopeKind": x.ScopeKind, "scopeId": x.ScopeID}
}

func def(v, d string) string {
	if v == "" {
		return d
	}
	return v
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

// offboard takes the person out of every tenant, team and role and ends their sessions. Someone who never signed in
// and was never seeded is nothing to do. The last owner of a tenant is refused: the tenant would have no one to run it.
func (a *applier) offboard(email string) error {
	u, err := a.st.UserByEmail(a.ctx, email)
	if store.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	teams, err := a.st.TeamsOfUser(a.ctx, u.ID)
	if err != nil {
		return err
	}
	for _, m := range teams {
		t, err := a.st.TeamByID(a.ctx, m.TeamID)
		if err != nil {
			return err
		}
		orgID, err := a.orgOfSpace(t.SpaceID)
		if err != nil {
			return err
		}
		if err := a.st.RemoveTeamMember(a.ctx, t.ID, u.ID); err != nil {
			return err
		}
		a.note("- %s from team %s", email, t.Slug)
		if err := a.audit(orgID, t.SpaceID, t.ID, store.ScopeTeam, t.ID, "member.remove", "user", u.ID, nil); err != nil {
			return err
		}
	}
	spaces, err := a.st.SpacesByUser(a.ctx, u.ID)
	if err != nil {
		return err
	}
	for _, sp := range spaces {
		if sp.Role == store.RoleOwner {
			n, err := a.st.CountOwners(a.ctx, sp.ID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return fmt.Errorf("offboard: %s is the last owner of tenant %s, name another owner first", email, sp.Slug)
			}
		}
		if err := a.st.RemoveMember(a.ctx, sp.ID, u.ID); err != nil {
			return err
		}
		a.note("- %s %s of tenant %s", sp.Role, email, sp.Slug)
		if err := a.audit(sp.OrgID, sp.ID, "", store.ScopeTenant, sp.ID, "member.remove", "user", u.ID, map[string]any{"role": sp.Role}); err != nil {
			return err
		}
	}
	grants, err := a.st.GrantsOfUser(a.ctx, u.ID)
	if err != nil {
		return err
	}
	for _, g := range grants {
		removed, err := a.st.Revoke(a.ctx, u.ID, g.ScopeKind, g.ScopeID, g.Role)
		if err != nil {
			return err
		}
		if !removed {
			continue
		}
		orgID, spaceID, teamID, err := a.chainOf(g.ScopeKind, g.ScopeID)
		if err != nil {
			return err
		}
		a.note("- %s %s of %s %s", g.Role, email, g.ScopeKind, g.ScopeID)
		if err := a.audit(orgID, spaceID, teamID, g.ScopeKind, g.ScopeID, "grant.remove", "user", u.ID, map[string]any{"role": g.Role}); err != nil {
			return err
		}
	}
	if u.IsAdmin {
		if err := a.st.SetUserAdmin(a.ctx, u.ID, false); err != nil {
			return err
		}
		a.note("- instance admin %s", email)
	}
	return a.st.DeleteUserSessions(a.ctx, u.ID, "")
}

func (a *applier) orgOfSpace(spaceID string) (string, error) {
	sp, err := a.st.SpaceByID(a.ctx, spaceID)
	return sp.OrgID, err
}

// chainOf names the organization, tenant and team a scope sits in, for the audit line.
func (a *applier) chainOf(kind, id string) (orgID, spaceID, teamID string, err error) {
	switch kind {
	case store.ScopeOrg:
		return id, "", "", nil
	case store.ScopeTenant:
		orgID, err = a.orgOfSpace(id)
		return orgID, id, "", err
	case store.ScopeTeam:
		t, err := a.st.TeamByID(a.ctx, id)
		if err != nil {
			return "", "", "", err
		}
		orgID, err = a.orgOfSpace(t.SpaceID)
		return orgID, t.SpaceID, id, err
	}
	return "", "", "", nil
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

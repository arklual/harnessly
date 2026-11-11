// Package authz is the one place the hub decides who may do what in the organization tree (docs/hub/orgs.md, «Rights»):
// organization ⊃ tenant (a space) ⊃ team ⊃ person.
//
// The rule: a person manages scope S when they are admin of S or of an ancestor of S, or lead of S when S is their team.
// A space's owner is its tenant's admin. A space with no organization is a tenant of an implicit organization of its own:
// its chain is just the tenant (and its teams), so it behaves exactly as before organizations existed.
//
// On top of that: nobody changes their own role or limit, nor the limit of a scope they manage themselves (a limit of S is
// set from above S; the organization's own limit is the one exception, flagged Self); a person who manages a scope is
// limited only by someone who manages it from higher up; the last admin of an organization is never removed.
package authz

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Action is what is checked.
type Action string

const (
	// View: see the scope's people and analytics.
	View Action = "view"
	// Money: see the scope's spend and subscriptions.
	Money Action = "money"
	// Manage: membership, roles below, machines and access, accounts in the scope.
	Manage Action = "manage"
	// Grant and Revoke: give or take Role at Scope to/from Target.
	Grant  Action = "grant"
	Revoke Action = "revoke"
	// SetLimit: the limit OF Scope itself (a budget of the team, of the tenant, of the organization).
	SetLimit Action = "set_limit"
	// SetPersonalLimit: Target's own limit inside Scope (a team, or a tenant for the personal ceiling).
	SetPersonalLimit Action = "set_personal_limit"
	// SetAccountLimit: a rule on an account inside Scope. On the account's own scope it is the account's shared limit;
	// on a tenant the account is granted to it is «account X in my tenant», a rule of its own that only narrows.
	SetAccountLimit Action = "set_account_limit"
)

// Scope is a node of the tree.
type Scope struct {
	Kind string // store.ScopeOrg, store.ScopeTenant, store.ScopeTeam
	ID   string
}

func (s Scope) Key() string { return s.Kind + ":" + s.ID }

// Principal is who asks.
type Principal struct {
	UserID        string
	InstanceAdmin bool
}

// Check is one question.
type Check struct {
	Action Action
	Scope  Scope
	Target string // the person a grant or a personal limit is about
	Role   string // Grant, Revoke: store.RoleAdmin or store.RoleLead
}

// Decision is a yes, with what the journal must know about it.
type Decision struct {
	// Self: the change is of a scope the actor manages themselves (only an organization's admin on its own limit gets
	// here); the journal marks it so that every admin of the organization sees it.
	Self bool
}

// Who could do what was refused: the UI's «Может поменять: …» hint (details.needs).
const (
	NeedsOrgAdmin      = "org_admin"
	NeedsTenantAdmin   = "tenant_admin"
	NeedsTeamLead      = "team_lead"
	NeedsInstanceAdmin = "instance_admin"
)

// Errors. Each carries details.scope and details.needs.
var (
	ErrForbiddenScope = httpx.New(http.StatusForbidden, "forbidden_scope", "Это выше вашего уровня")
	ErrSelfChange     = httpx.New(http.StatusForbidden, "self_change", "Это меняет тот, кто выше вас")
	ErrLastAdmin      = httpx.New(http.StatusConflict, "last_admin", "Нельзя снять последнего администратора организации")
)

// Authz answers over a store (or a transaction's store, so that a check and its change see the same rows).
type Authz struct {
	S store.Store
	// MembershipOnly ignores role grants and organizations: what the hub did before them (HUB_ORGS=off). A space's
	// owner still manages it.
	MembershipOnly bool
}

func New(s store.Store) Authz { return Authz{S: s} }

// Chain is the scope and its ancestors, top first: [org?, tenant, team?]. The org is absent for an implicit organization.
func (a Authz) Chain(ctx context.Context, s Scope) ([]Scope, error) {
	var tenantID string
	var out []Scope
	switch s.Kind {
	case store.ScopeTeam:
		t, err := a.S.TeamByID(ctx, s.ID)
		if err != nil {
			return nil, notFound(err)
		}
		tenantID = t.SpaceID
		out = append(out, s)
	case store.ScopeTenant:
		tenantID = s.ID
	case store.ScopeOrg:
		if _, err := a.S.OrgByID(ctx, s.ID); err != nil {
			return nil, notFound(err)
		}
		return []Scope{s}, nil
	default:
		return nil, httpx.Invalid("scope", "Неизвестная область")
	}
	sp, err := a.S.SpaceByID(ctx, tenantID)
	if err != nil {
		return nil, notFound(err)
	}
	out = append([]Scope{{store.ScopeTenant, sp.ID}}, out...)
	if sp.OrgID != "" && !a.MembershipOnly {
		out = append([]Scope{{store.ScopeOrg, sp.OrgID}}, out...)
	}
	return out, nil
}

func notFound(err error) error {
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	}
	return err
}

// level is the index in chain of the highest scope the person manages it from, or -1 when they do not manage it. An
// admin grant on an org or tenant, a tenant's owner membership, and a lead grant on the team at the bottom count. A
// grant on a tenant or on its team counts only while the person is a member of that tenant: a grant left behind by
// somebody who is no longer in it gives nothing.
func (a Authz) level(ctx context.Context, userID string, chain []Scope) (int, error) {
	if userID == "" {
		return -1, nil
	}
	var grants []store.RoleGrant
	if !a.MembershipOnly {
		var err error
		if grants, err = a.S.GrantsOfUser(ctx, userID); err != nil {
			return -1, err
		}
	}
	has := map[string]bool{}
	for _, g := range grants {
		has[g.ScopeKind+":"+g.ScopeID+":"+g.Role] = true
	}
	member := false
	for i, s := range chain {
		switch s.Kind {
		case store.ScopeOrg:
			if has[s.Key()+":"+store.RoleAdmin] {
				return i, nil
			}
		case store.ScopeTenant:
			role, err := a.S.Role(ctx, s.ID, userID)
			if err != nil {
				return -1, err
			}
			member = role != ""
			if role == store.RoleOwner || (member && has[s.Key()+":"+store.RoleAdmin]) {
				return i, nil
			}
		case store.ScopeTeam:
			if member && has[s.Key()+":"+store.RoleLead] {
				return i, nil
			}
		}
	}
	return -1, nil
}

// needsFor is the role that manages the scope at chain[i].
func needsFor(chain []Scope, i int) string {
	if i < 0 {
		return NeedsInstanceAdmin
	}
	switch chain[i].Kind {
	case store.ScopeOrg:
		return NeedsOrgAdmin
	case store.ScopeTenant:
		return NeedsTenantAdmin
	default:
		return NeedsTeamLead
	}
}

func deny(base *httpx.Error, s Scope, needs string) error {
	return base.With("scope", s.Key()).With("needs", needs)
}

// Can answers one check: a Decision, or ErrForbiddenScope, ErrSelfChange, ErrLastAdmin, httpx.ErrNotFound.
func (a Authz) Can(ctx context.Context, p Principal, c Check) (Decision, error) {
	chain, err := a.Chain(ctx, c.Scope)
	if err != nil {
		return Decision{}, err
	}
	at := len(chain) - 1 // the scope itself
	lvl, err := a.level(ctx, p.UserID, chain)
	if err != nil {
		return Decision{}, err
	}
	if p.InstanceAdmin && lvl < 0 {
		lvl = 0 // an instance administrator manages everything from the top; the self rules still hold
		if err := a.noteOperator(ctx, p.UserID, chain, "operator."+string(c.Action), c.Scope.Kind, c.Scope.ID); err != nil {
			return Decision{}, err
		}
	}
	self := c.Target != "" && c.Target == p.UserID

	switch c.Action {
	case View, Money:
		if lvl >= 0 {
			return Decision{}, nil
		}
		return Decision{}, deny(ErrForbiddenScope, c.Scope, needsFor(chain, at))

	case Manage:
		if lvl >= 0 {
			return Decision{}, nil
		}
		return Decision{}, deny(ErrForbiddenScope, c.Scope, needsFor(chain, at))

	case Grant, Revoke:
		if err := roleFits(c.Scope, c.Role); err != nil {
			return Decision{}, err
		}
		if self {
			return Decision{}, deny(ErrSelfChange, c.Scope, needsFor(chain, at))
		}
		// An admin role of S is given by whoever manages S as admin (a lead manages only the team at the bottom); a lead
		// role by whoever manages the team. Removing is the same right: one's level and below, never above.
		if lvl < 0 {
			return Decision{}, deny(ErrForbiddenScope, c.Scope, needsFor(chain, at))
		}
		if c.Action == Revoke && c.Scope.Kind == store.ScopeOrg && c.Role == store.RoleAdmin {
			n, err := a.S.CountGrants(ctx, store.ScopeOrg, c.Scope.ID, store.RoleAdmin)
			if err != nil {
				return Decision{}, err
			}
			if n <= 1 {
				return Decision{}, ErrLastAdmin.With("scope", c.Scope.Key())
			}
		}
		return Decision{}, nil

	case SetLimit:
		// The limit of S is set from above S. The organization has nothing above: its admin sets it, marked Self.
		if at == 0 && c.Scope.Kind == store.ScopeOrg {
			if lvl == 0 {
				return Decision{Self: a.isAdmin(ctx, p.UserID, c.Scope)}, nil
			}
			return Decision{}, deny(ErrForbiddenScope, c.Scope, NeedsOrgAdmin)
		}
		if at == 0 {
			// The tenant of an implicit organization: nobody is above its owner but the instance administrator.
			if p.InstanceAdmin {
				return Decision{}, nil
			}
			if lvl == 0 {
				return Decision{}, deny(ErrSelfChange, c.Scope, NeedsInstanceAdmin)
			}
			return Decision{}, deny(ErrForbiddenScope, c.Scope, NeedsInstanceAdmin)
		}
		if lvl >= 0 && lvl < at {
			return Decision{}, nil
		}
		if lvl == at {
			return Decision{}, deny(ErrSelfChange, c.Scope, needsFor(chain, at-1))
		}
		return Decision{}, deny(ErrForbiddenScope, c.Scope, needsFor(chain, at-1))

	case SetPersonalLimit:
		if self {
			// One's own limit is always set from above one's level in this scope.
			up := at
			if lvl >= 0 {
				up = lvl - 1
			}
			return Decision{}, deny(ErrSelfChange, c.Scope, needsFor(chain, up))
		}
		if lvl < 0 {
			return Decision{}, deny(ErrForbiddenScope, c.Scope, needsFor(chain, at))
		}
		// A person who manages the scope (the team's lead, the tenant's admin) is limited only from higher up.
		tl, err := a.level(ctx, c.Target, chain)
		if err != nil {
			return Decision{}, err
		}
		if tl >= 0 && lvl >= tl {
			return Decision{}, deny(ErrForbiddenScope, c.Scope, needsFor(chain, tl-1))
		}
		return Decision{}, nil

	case SetAccountLimit:
		// On the account's own scope this is the shared limit (the organization's account: its admin); on a tenant it is
		// the tenant's own narrowing rule, which the tenant's admin may set (the plan's «аккаунт X в моём тенанте»).
		if c.Scope.Kind == store.ScopeTeam {
			return Decision{}, httpx.Invalid("scope", "Лимит аккаунта ставится на организацию или тенант")
		}
		if lvl >= 0 {
			return Decision{Self: c.Scope.Kind == store.ScopeOrg}, nil
		}
		return Decision{}, deny(ErrForbiddenScope, c.Scope, needsFor(chain, at))
	}
	return Decision{}, errors.New("authz: unknown action " + string(c.Action))
}

func (a Authz) isAdmin(ctx context.Context, userID string, s Scope) bool {
	gs, err := a.S.GrantsOfUser(ctx, userID)
	if err != nil {
		return false
	}
	for _, g := range gs {
		if g.ScopeKind == s.Kind && g.ScopeID == s.ID && g.Role == store.RoleAdmin {
			return true
		}
	}
	return false
}

func roleFits(s Scope, role string) error {
	switch {
	case role == store.RoleAdmin && (s.Kind == store.ScopeOrg || s.Kind == store.ScopeTenant):
	case role == store.RoleLead && s.Kind == store.ScopeTeam:
	default:
		return httpx.Invalid("role", "Такой роли у этой области нет")
	}
	return nil
}

// Allowed is Can as a yes or no; a refusal is false, any other error is returned.
func (a Authz) Allowed(ctx context.Context, p Principal, c Check) (bool, error) {
	_, err := a.Can(ctx, p, c)
	var he *httpx.Error
	if errors.As(err, &he) && (he.Code == ErrForbiddenScope.Code || he.Code == ErrSelfChange.Code) {
		return false, nil
	}
	return err == nil, err
}

// Capabilities is what the person may do at one scope, for the UI (orgs.md, Capabilities).
type Capabilities struct {
	View           bool     `json:"view"`
	Money          bool     `json:"money"`
	Manage         bool     `json:"manage"`
	Grant          []string `json:"grant"`
	SetLimit       bool     `json:"setLimit"`
	SetChildLimits bool     `json:"setChildLimits"`
	Needs          struct {
		Manage   *string `json:"manage"`
		SetLimit *string `json:"setLimit"`
	} `json:"needs"`
}

// CapabilitiesAt computes Capabilities for the person at s.
func (a Authz) CapabilitiesAt(ctx context.Context, p Principal, s Scope) (Capabilities, error) {
	var c Capabilities
	c.Grant = []string{}
	chain, err := a.Chain(ctx, s)
	if err != nil {
		return c, err
	}
	at := len(chain) - 1
	lvl, err := a.level(ctx, p.UserID, chain)
	if err != nil {
		return c, err
	}
	if p.InstanceAdmin && lvl < 0 {
		lvl = 0
	}
	c.View, c.Money, c.Manage, c.SetChildLimits = lvl >= 0, lvl >= 0, lvl >= 0, lvl >= 0
	if c.Manage {
		if s.Kind == store.ScopeTeam {
			c.Grant = []string{store.RoleLead}
		} else {
			c.Grant = []string{store.RoleAdmin}
		}
	} else {
		n := needsFor(chain, at)
		c.Needs.Manage = &n
	}
	if _, err := a.Can(ctx, p, Check{Action: SetLimit, Scope: s}); err == nil {
		c.SetLimit = true
	} else {
		var he *httpx.Error
		if !errors.As(err, &he) {
			return c, err
		}
		n, _ := he.Details["needs"].(string)
		c.Needs.SetLimit = &n
	}
	return c, nil
}

// OperatorWindow is how long the same use of the installation administrator's role stays one line of the journal.
const OperatorWindow = time.Hour

// NoteOperator writes into the journal of the scope that an administrator of the installation, who holds no place in
// it, reached it or something in it (target): who, where and what, never the content. Repeats within OperatorWindow
// are one line.
func (a Authz) NoteOperator(ctx context.Context, userID string, s Scope, action, targetKind, targetID string) error {
	chain, err := a.Chain(ctx, s)
	if err != nil {
		return err
	}
	return a.noteOperator(ctx, userID, chain, action, targetKind, targetID)
}

func (a Authz) noteOperator(ctx context.Context, userID string, chain []Scope, action, targetKind, targetID string) error {
	at := chain[len(chain)-1]
	e := &store.AuditEvent{ActorID: userID, ScopeKind: at.Kind, ScopeID: at.ID, Action: action, TargetKind: targetKind, TargetID: targetID, At: time.Now().UTC()}
	for _, s := range chain {
		switch s.Kind {
		case store.ScopeOrg:
			e.OrgID = s.ID
		case store.ScopeTenant:
			e.SpaceID = s.ID
		case store.ScopeTeam:
			e.TeamID = s.ID
		}
	}
	_, err := a.S.AddOperatorRead(ctx, e, e.At.Add(-OperatorWindow))
	return err
}

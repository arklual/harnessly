package orgs

import (
	"context"
	"encoding/json"

	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The JSON of docs/hub/orgs.md, «Types» (web/app/hub-api/orgs.ts).

type ScopeView struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type OrgView struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Timezone  string `json:"timezone"`
	Currency  string `json:"currency"`
	CreatedAt string `json:"createdAt"`
	// AppsConsent is the rule for apps on the organization's machines: "ask" (the default) or "auto".
	AppsConsent string `json:"appsConsent"`
}

type TenantView struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	OrgID       string `json:"orgId"`
	CreatedAt   string `json:"createdAt"`
	MemberCount int    `json:"memberCount"`
}

type TeamView struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenantId"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	CreatedAt   string   `json:"createdAt"`
	MemberCount int      `json:"memberCount"`
	LeadIDs     []string `json:"leadIds"`
}

type GrantView struct {
	UserID    string    `json:"userId"`
	Scope     ScopeView `json:"scope"`
	Role      string    `json:"role"`
	GrantedBy *string   `json:"grantedBy"`
	GrantedAt string    `json:"grantedAt"`
}

type TeamRef struct {
	TeamID    string `json:"teamId"`
	IsPrimary bool   `json:"isPrimary"`
}

type MeView struct {
	UserID        string                        `json:"userId"`
	InstanceAdmin bool                          `json:"instanceAdmin"`
	Grants        []GrantView                   `json:"grants"`
	Teams         []TeamRef                     `json:"teams"`
	Scopes        map[string]authz.Capabilities `json:"scopes"`
}

// View is OrgView of the contract: the organization as one person sees it.
type View struct {
	Org     OrgView      `json:"org"`
	Tenants []TenantView `json:"tenants"`
	Teams   []TeamView   `json:"teams"`
	Me      MeView       `json:"me"`
	// Features are the flags that are on for the organization (docs/hub/api.md, «Feature flags»).
	Features []string `json:"features"`
}

func orgView(o store.Org) OrgView {
	return OrgView{ID: o.ID, Slug: o.Slug, Name: o.Name, Timezone: o.Timezone, Currency: o.Currency, CreatedAt: httpx.Time(o.CreatedAt), AppsConsent: o.AppsConsent}
}

func grantView(g store.RoleGrant) GrantView {
	v := GrantView{UserID: g.UserID, Scope: ScopeView{g.ScopeKind, g.ScopeID}, Role: g.Role, GrantedAt: httpx.Time(g.GrantedAt)}
	if g.GrantedBy != "" {
		by := g.GrantedBy
		v.GrantedBy = &by
	}
	return v
}

func tenantView(ctx context.Context, st store.Store, sp store.Space) (TenantView, error) {
	members, err := st.Members(ctx, sp.ID)
	if err != nil {
		return TenantView{}, err
	}
	return TenantView{ID: sp.ID, Slug: sp.Slug, Name: sp.Name, OrgID: sp.OrgID, CreatedAt: httpx.Time(sp.CreatedAt), MemberCount: len(members)}, nil
}

func teamView(ctx context.Context, st store.Store, t store.Team) (TeamView, error) {
	members, err := st.TeamMembers(ctx, t.ID)
	if err != nil {
		return TeamView{}, err
	}
	leads, err := st.GrantsOn(ctx, store.ScopeTeam, t.ID)
	if err != nil {
		return TeamView{}, err
	}
	v := TeamView{ID: t.ID, TenantID: t.SpaceID, Slug: t.Slug, Name: t.Name, CreatedAt: httpx.Time(t.CreatedAt), MemberCount: len(members), LeadIDs: []string{}}
	for _, g := range leads {
		if g.Role == store.RoleLead {
			v.LeadIDs = append(v.LeadIDs, g.UserID)
		}
	}
	return v, nil
}

// buildView is the organization as p sees it: every tenant and team they can view, and those they are in.
func buildView(ctx context.Context, st store.Store, p authz.Principal, o store.Org) (View, error) {
	az := authz.New(st)
	v := View{Org: orgView(o), Tenants: []TenantView{}, Teams: []TeamView{},
		Features: []string{}, Me: MeView{UserID: p.UserID, InstanceAdmin: p.InstanceAdmin, Grants: []GrantView{}, Teams: []TeamRef{}, Scopes: map[string]authz.Capabilities{}}}

	myTeams, err := st.TeamsOfUser(ctx, p.UserID)
	if err != nil {
		return v, err
	}
	inTeam := map[string]bool{}
	for _, t := range myTeams {
		inTeam[t.TeamID] = true
	}
	inOrg := map[string]bool{store.ScopeOrg + ":" + o.ID: true}

	orgScope := authz.Scope{Kind: store.ScopeOrg, ID: o.ID}
	if c, err := az.CapabilitiesAt(ctx, p, orgScope); err != nil {
		return v, err
	} else if c.View {
		v.Me.Scopes[orgScope.Key()] = c
	}
	tenants, err := st.SpacesByOrg(ctx, o.ID)
	if err != nil {
		return v, err
	}
	for _, sp := range tenants {
		ts := authz.Scope{Kind: store.ScopeTenant, ID: sp.ID}
		inOrg[ts.Key()] = true
		tc, err := az.CapabilitiesAt(ctx, p, ts)
		if err != nil {
			return v, err
		}
		role, err := st.Role(ctx, sp.ID, p.UserID)
		if err != nil {
			return v, err
		}
		teams, err := st.TeamsBySpace(ctx, sp.ID)
		if err != nil {
			return v, err
		}
		for _, t := range teams {
			inOrg[store.ScopeTeam+":"+t.ID] = true
		}
		if !tc.View && role == "" {
			continue
		}
		tv, err := tenantView(ctx, st, sp)
		if err != nil {
			return v, err
		}
		v.Tenants = append(v.Tenants, tv)
		if tc.View {
			v.Me.Scopes[ts.Key()] = tc
		}
		for _, t := range teams {
			ms := authz.Scope{Kind: store.ScopeTeam, ID: t.ID}
			mc, err := az.CapabilitiesAt(ctx, p, ms)
			if err != nil {
				return v, err
			}
			if !mc.View && !inTeam[t.ID] {
				continue
			}
			mv, err := teamView(ctx, st, t)
			if err != nil {
				return v, err
			}
			v.Teams = append(v.Teams, mv)
			v.Me.Scopes[ms.Key()] = mc
		}
	}
	for _, t := range myTeams {
		if inOrg[store.ScopeTeam+":"+t.TeamID] {
			v.Me.Teams = append(v.Me.Teams, TeamRef{TeamID: t.TeamID, IsPrimary: t.IsPrimary})
		}
	}
	grants, err := st.GrantsOfUser(ctx, p.UserID)
	if err != nil {
		return v, err
	}
	for _, g := range grants {
		if inOrg[g.ScopeKind+":"+g.ScopeID] {
			v.Me.Grants = append(v.Me.Grants, grantView(g))
		}
	}
	return v, nil
}

// roleIn is the person's highest role in the organization for GET /api/orgs: admin of it or of one of its tenants (an
// owner of a tenant counts), lead of one of its teams, or a member.
func roleIn(ctx context.Context, st store.Store, userID string, o store.Org, grants []store.RoleGrant) (string, error) {
	tenants, err := st.SpacesByOrg(ctx, o.ID)
	if err != nil {
		return "", err
	}
	tenant := map[string]bool{}
	for _, sp := range tenants {
		tenant[sp.ID] = true
		role, err := st.Role(ctx, sp.ID, userID)
		if err != nil {
			return "", err
		}
		if role == store.RoleOwner {
			return store.RoleAdmin, nil
		}
	}
	lead := false
	for _, g := range grants {
		switch g.ScopeKind {
		case store.ScopeOrg:
			if g.ScopeID == o.ID && g.Role == store.RoleAdmin {
				return store.RoleAdmin, nil
			}
		case store.ScopeTenant:
			if tenant[g.ScopeID] && g.Role == store.RoleAdmin {
				return store.RoleAdmin, nil
			}
		case store.ScopeTeam:
			t, err := st.TeamByID(ctx, g.ScopeID)
			if err != nil {
				if store.IsNotFound(err) {
					continue
				}
				return "", err
			}
			lead = lead || tenant[t.SpaceID]
		}
	}
	if lead {
		return store.RoleLead, nil
	}
	return "member", nil
}

// jsonOf is the journal's «было»/«стало» of a value.
func jsonOf(v any) string {
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// objOf reads a journal's «было»/«стало» back; an empty one is null.
func objOf(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

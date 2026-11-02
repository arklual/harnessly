package orgs

import (
	"context"

	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Belongs is whether the person is in the scope, as a member: a person of the team, a member of the tenant, anyone with
// something in the organization (docs/hub/analytics.md, «Signals»: a member sees their own signals there).
func Belongs(ctx context.Context, st store.Store, o store.Org, sc authz.Scope, userID string) (bool, error) {
	chain, err := chainIn(ctx, st, o, sc)
	if err != nil {
		return false, err
	}
	if sc.Kind == store.ScopeOrg {
		mine, err := st.OrgsForUser(ctx, userID)
		if err != nil {
			return false, err
		}
		for _, m := range mine {
			if m.ID == o.ID {
				return true, nil
			}
		}
		return false, nil
	}
	return inScope(ctx, st, userID, chain)
}

// LimitRaisers are the people who can raise the personal limit of userID inside the scope of chain, at the lowest level
// that has any: the team's leads for a team, the tenant's admins and owners for a tenant, the organization's admins for
// the organization, going up while a level has nobody who may (nobody sets their own limit, nor the limit of someone at
// or above their level). The rights are authz's SetPersonalLimit.
func LimitRaisers(ctx context.Context, st store.Store, chain []authz.Scope, userID string) ([]string, error) {
	sc := chain[len(chain)-1]
	az := authz.New(st)
	for i := len(chain) - 1; i >= 0; i-- {
		var holders []string
		switch chain[i].Kind {
		case store.ScopeTeam:
			gs, err := st.GrantsOn(ctx, store.ScopeTeam, chain[i].ID)
			if err != nil {
				return nil, err
			}
			for _, g := range gs {
				if g.Role == store.RoleLead {
					holders = append(holders, g.UserID)
				}
			}
		case store.ScopeTenant:
			gs, err := st.GrantsOn(ctx, store.ScopeTenant, chain[i].ID)
			if err != nil {
				return nil, err
			}
			for _, g := range gs {
				if g.Role == store.RoleAdmin {
					holders = append(holders, g.UserID)
				}
			}
			ms, err := st.Members(ctx, chain[i].ID)
			if err != nil {
				return nil, err
			}
			for _, m := range ms {
				if m.Role == store.RoleOwner {
					holders = append(holders, m.UserID)
				}
			}
		case store.ScopeOrg:
			gs, err := st.GrantsOn(ctx, store.ScopeOrg, chain[i].ID)
			if err != nil {
				return nil, err
			}
			for _, g := range gs {
				if g.Role == store.RoleAdmin {
					holders = append(holders, g.UserID)
				}
			}
		}
		var out []string
		seen := map[string]bool{}
		for _, h := range holders {
			if seen[h] {
				continue
			}
			seen[h] = true
			ok, err := az.Allowed(ctx, authz.Principal{UserID: h}, authz.Check{Action: authz.SetPersonalLimit, Scope: sc, Target: userID})
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, h)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, nil
}

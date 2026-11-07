package orgs

import (
	"context"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Deleting an empty tenant (GitHub issue 28). A tenant goes only when nothing of value is in it: no machine, no other
// person, no team, no live invite, no site, no environment entry. Anything of those is a conflict that names it, never
// a cascade. What goes with the tenant is what is only its own settings: the deleter's membership and admin grant, its
// limit rules, its registration codes and ssh key. Revoked machines are taken off it and keep their history.

// Blocker is one kind of thing that keeps a tenant from being deleted.
type Blocker struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// DeletePlan is what deleting a tenant does (and, with dry_run, would do).
type DeletePlan struct {
	Tenant   TenantView `json:"tenant"`
	Blockers []Blocker  `json:"blockers"`
	// Removes are the settings that go with the tenant.
	Removes   []Blocker `json:"removes"`
	CanDelete bool      `json:"canDelete"`
}

func (s *Service) deleteTenant(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	dry := r.URL.Query().Get("dry_run") == "true" || r.URL.Query().Get("dry_run") == "1"
	st := s.db.S()
	o, err := Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	sp, err := st.SpaceByID(ctx, r.PathValue("spaceId"))
	if store.IsNotFound(err) || (err == nil && sp.OrgID != o.ID) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeTenant, ID: sp.ID}}); err != nil {
		return err
	}
	if dry {
		plan, err := s.planDelete(ctx, st, p, sp)
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"dry_run": true, "plan": plan})
		return nil
	}
	var plan DeletePlan
	err = s.db.Tx(ctx, func(st store.Store) error {
		var err error
		if plan, err = s.planDelete(ctx, st, p, sp); err != nil {
			return err
		}
		if !plan.CanDelete {
			return httpx.Conflict("tenant_not_empty", "В тенанте остались данные, удалить его нельзя").With("blockers", plan.Blockers)
		}
		if err := st.DetachRevokedMachines(ctx, sp.ID); err != nil {
			return err
		}
		if err := st.DeleteLimitRulesOfTenant(ctx, sp.ID); err != nil {
			return err
		}
		if err := st.DeleteGrantsOn(ctx, store.ScopeTenant, sp.ID); err != nil {
			return err
		}
		if err := st.AddAudit(ctx, &store.AuditEvent{OrgID: o.ID, SpaceID: sp.ID, ActorID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID,
			Action: "tenant.delete", TargetKind: "tenant", TargetID: sp.ID, Before: jsonOf(map[string]any{"name": sp.Name, "slug": sp.Slug}), At: s.now().UTC()}); err != nil {
			return err
		}
		return st.DeleteSpace(ctx, sp.ID)
	})
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "space_deleted", r, sp.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "plan": plan})
	return nil
}

func (s *Service) planDelete(ctx context.Context, st store.Store, p *auth.Principal, sp store.Space) (DeletePlan, error) {
	tv, err := tenantView(ctx, st, sp)
	if err != nil {
		return DeletePlan{}, err
	}
	plan := DeletePlan{Tenant: tv, Blockers: []Blocker{}, Removes: []Blocker{}}
	add := func(list *[]Blocker, kind string, n int) {
		if n > 0 {
			*list = append(*list, Blocker{Kind: kind, Count: n})
		}
	}
	machines, err := st.MachinesBySpace(ctx, sp.ID)
	if err != nil {
		return plan, err
	}
	add(&plan.Blockers, "machines", len(machines))
	members, err := st.Members(ctx, sp.ID)
	if err != nil {
		return plan, err
	}
	others := 0
	for _, m := range members {
		if m.UserID != p.User.ID {
			others++
		}
	}
	add(&plan.Blockers, "members", others)
	grants, err := st.GrantsOn(ctx, store.ScopeTenant, sp.ID)
	if err != nil {
		return plan, err
	}
	otherGrants := 0
	for _, g := range grants {
		if g.UserID != p.User.ID {
			otherGrants++
		}
	}
	add(&plan.Blockers, "grants", otherGrants)
	teams, err := st.TeamsBySpace(ctx, sp.ID)
	if err != nil {
		return plan, err
	}
	add(&plan.Blockers, "teams", len(teams))
	n, err := st.CountActiveInvitesOfSpace(ctx, sp.ID, store.Ms(s.now()))
	if err != nil {
		return plan, err
	}
	add(&plan.Blockers, "invites", n)
	if n, err = st.CountSitesOfSpace(ctx, sp.Slug); err != nil {
		return plan, err
	}
	add(&plan.Blockers, "sites", n)
	if n, err = st.CountEnvEntriesOfTenant(ctx, sp.ID); err != nil {
		return plan, err
	}
	add(&plan.Blockers, "environment", n)
	if n, err = st.CountLimitRulesOfTenant(ctx, sp.ID); err != nil {
		return plan, err
	}
	add(&plan.Removes, "limit_rules", n)
	if n, err = st.CountRevokedMachines(ctx, sp.ID); err != nil {
		return plan, err
	}
	add(&plan.Removes, "revoked_machines_detached", n)
	plan.CanDelete = len(plan.Blockers) == 0
	return plan, nil
}

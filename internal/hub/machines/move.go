package machines

import (
	"context"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Moving a machine to another tenant (GitHub issue 28). The machine keeps its row: its id, key, runtime links and
// history stay. What belongs to the old tenant does not follow: the team is reset (or set to a team of the target the
// request names) and the per-person and per-team access list is cleared, so the machine is open to the target's members
// as any new machine of it is. Both tenants are managed by the person.
//
// Limits: the hub caps spend, not the number of machines (docs/hub/limits.md), so a move is never refused by count.
// Spend made after the move counts against the target tenant's limit rules; what was spent before stays in the old
// tenant's history and its limits. The plan says how many rules each side has.

// TenantRef is a tenant in a plan.
type TenantRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OrgID string `json:"orgId"`
}

// MoveLimits is what the move means for spend limits: the number of limit rules of the tenant (and its teams) on
// each side. New spend counts against To's rules; past spend stays with From.
type MoveLimits struct {
	FromRules int `json:"fromRules"`
	ToRules   int `json:"toRules"`
}

// MovePlan is what a move does (and, with dry_run, what it would do).
type MovePlan struct {
	Machine struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"machine"`
	From TenantRef `json:"from"`
	To   TenantRef `json:"to"`
	// Noop: the machine is in the target already.
	Noop bool `json:"noop"`
	// TeamCleared is the team the machine leaves; TeamAssigned the team of the target it lands in.
	TeamCleared  *AccessView `json:"teamCleared"`
	TeamAssigned *AccessView `json:"teamAssigned"`
	// AccessRevoked are the entries of the old access list, all of which go.
	AccessRevoked []AccessView `json:"accessRevoked"`
	Limits        MoveLimits   `json:"limits"`
	// Kept lists what stays with the machine.
	Kept []string `json:"kept"`
}

type moveRequest struct {
	TenantID string `json:"tenantId"`
	TeamID   string `json:"teamId"`
	DryRun   bool   `json:"dry_run"`
}

func (s *Service) move(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req moveRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.TenantID == "" {
		return httpx.Invalid("tenantId", "Выберите тенант")
	}
	ctx := r.Context()
	id := r.PathValue("machineId")
	if req.DryRun {
		plan, _, err := s.planMove(ctx, s.db.S(), p, id, req)
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"dry_run": true, "plan": plan})
		return nil
	}
	var plan MovePlan
	var from store.Machine
	err := s.db.Tx(ctx, func(st store.Store) error {
		var err error
		var m store.Machine
		if plan, m, err = s.planMove(ctx, st, p, id, req); err != nil {
			return err
		}
		from = m
		if plan.Noop {
			return nil
		}
		return s.applyMove(ctx, st, p, m, plan, req)
	})
	if err != nil {
		return err
	}
	st := s.db.S()
	h, err := s.hubMachine(ctx, st, id)
	if err != nil {
		return err
	}
	if !plan.Noop {
		s.events.PublishJSON(from.SpaceID, "machineRemoved", map[string]string{"machineId": id}, false)
		if m, err := st.MachineByID(ctx, id); err == nil {
			s.Publish(ctx, m)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machine": h, "moved": !plan.Noop, "plan": plan})
	return nil
}

// planMove checks the rights in both tenants and builds the plan; it changes nothing.
func (s *Service) planMove(ctx context.Context, st store.Store, p *auth.Principal, id string, req moveRequest) (MovePlan, store.Machine, error) {
	var plan MovePlan
	m, err := machineOf(ctx, st, id)
	if err != nil {
		return plan, m, err
	}
	// Someone who sees nothing of the tenant is told nothing of it: 404, not a refusal that confirms the machine.
	if err := seen(ctx, st, p, authz.Scope{Kind: store.ScopeTenant, ID: m.SpaceID}); err != nil {
		return plan, m, err
	}
	if err := manage(ctx, st, p, m); err != nil {
		return plan, m, err
	}
	to, err := st.SpaceByID(ctx, req.TenantID)
	if store.IsNotFound(err) {
		return plan, m, httpx.ErrNotFound
	}
	if err != nil {
		return plan, m, err
	}
	if err := seen(ctx, st, p, authz.Scope{Kind: store.ScopeTenant, ID: to.ID}); err != nil {
		return plan, m, err
	}
	if _, err := authz.New(st).Can(ctx, orgs.Principal(p), authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeTenant, ID: to.ID}}); err != nil {
		return plan, m, err
	}
	from, err := st.SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return plan, m, err
	}
	plan.Machine.ID, plan.Machine.Name = m.ID, m.Name
	plan.From = TenantRef{ID: from.ID, Name: from.Name, OrgID: from.OrgID}
	plan.To = TenantRef{ID: to.ID, Name: to.Name, OrgID: to.OrgID}
	plan.AccessRevoked = []AccessView{}
	plan.Kept = []string{"machine_id", "key", "runtime_links", "history", "environment_values"}
	if from.ID == to.ID {
		plan.Noop = true
		return plan, m, nil
	}
	lb := newLabels(ctx, st)
	if m.TeamID != "" {
		name, _ := lb.team(m.TeamID)
		plan.TeamCleared = &AccessView{Kind: store.AccessTeam, ID: m.TeamID, Label: name}
	}
	if req.TeamID != "" {
		t, err := st.TeamByID(ctx, req.TeamID)
		if store.IsNotFound(err) || (err == nil && t.SpaceID != to.ID) {
			return plan, m, httpx.Invalid("teamId", "Команда должна быть из целевого тенанта")
		}
		if err != nil {
			return plan, m, err
		}
		plan.TeamAssigned = &AccessView{Kind: store.AccessTeam, ID: t.ID, Label: t.Name}
	}
	cur, err := st.MachineAccess(ctx, []string{m.ID})
	if err != nil {
		return plan, m, err
	}
	for _, e := range cur {
		label, _ := lb.access(e.Kind, e.RefID)
		plan.AccessRevoked = append(plan.AccessRevoked, AccessView{Kind: e.Kind, ID: e.RefID, Label: label})
	}
	if plan.Limits.FromRules, err = st.CountLimitRulesOfTenant(ctx, from.ID); err != nil {
		return plan, m, err
	}
	if plan.Limits.ToRules, err = st.CountLimitRulesOfTenant(ctx, to.ID); err != nil {
		return plan, m, err
	}
	return plan, m, nil
}

// applyMove changes the machine and writes the journal of both tenants.
func (s *Service) applyMove(ctx context.Context, st store.Store, p *auth.Principal, m store.Machine, plan MovePlan, req moveRequest) error {
	if err := st.MoveMachine(ctx, m.ID, plan.To.ID, req.TeamID); err != nil {
		return err
	}
	if len(plan.AccessRevoked) > 0 {
		if err := st.SetMachineAccess(ctx, m.ID, nil, s.now().UTC()); err != nil {
			return err
		}
	}
	// The source's journal: at the team the machine left (its chain includes the tenant and the organization).
	before := map[string]any{"tenantId": plan.From.ID, "teamId": nilIfEmpty(m.TeamID), "access": plan.AccessRevoked}
	after := map[string]any{"tenantId": plan.To.ID, "teamId": nilIfEmpty(req.TeamID)}
	if err := s.journalBy(ctx, st, p.User.ID, m, "machine.move", before, after); err != nil {
		return err
	}
	chain, err := authz.New(st).Chain(ctx, authz.Scope{Kind: store.ScopeTenant, ID: plan.To.ID})
	if err != nil {
		return err
	}
	return st.AddAudit(ctx, orgs.Journal(chain, p.User.ID, "machine.move", "machine", m.ID, before, after, s.now().UTC()))
}

// seen says the person is in the tenant or may view it; if not, the answer is 404.
func seen(ctx context.Context, st store.Store, p *auth.Principal, sc authz.Scope) error {
	if role, err := st.Role(ctx, sc.ID, p.User.ID); err != nil {
		return err
	} else if role != "" {
		return nil
	}
	if _, err := authz.New(st).Can(ctx, orgs.Principal(p), authz.Check{Action: authz.View, Scope: sc}); err != nil {
		return httpx.ErrNotFound
	}
	return nil
}

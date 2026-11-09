package analytics

import (
	"net/http"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// MachinesResponse is GET …/machines (docs/hub/analytics.md, «Machines»).
type MachinesResponse struct {
	Scope    orgs.ScopeView        `json:"scope"`
	Currency Currency              `json:"currency"`
	Machines []machines.HubMachine `json:"machines"`
}

// UseMachines gives the service the machines that build the list's rows; without them the list does not exist.
func (s *Service) UseMachines(m *machines.Service) { s.machines = m }

func (s *Service) mountMachines(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/machines", s.gate(s.machinesList))
}

// machinesList is the scope's machines, those with problems first (then by name); ?problems=1 keeps only those.
func (s *Service) machinesList(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if s.machines == nil {
		return httpx.ErrNotFound
	}
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.View)
	if isForbiddenScope(err) {
		// A member without a role sees the machines of the tenants they are in that are open to them: the list of
		// the member's own screen (the access list below still hides the rest).
		x, err = s.memberScope(ctx, st, p, r)
	}
	if err != nil {
		return err
	}
	onlyProblems := false
	switch r.URL.Query().Get("problems") {
	case "", "0":
	case "1":
		onlyProblems = true
	default:
		return httpx.Invalid("problems", "problems — 1 или не указывать")
	}
	var ms []store.Machine
	for id := range x.tenants {
		list, err := st.MachinesBySpace(ctx, id)
		if err != nil {
			return err
		}
		// A lead's screen is their team: its machines and, unless they see the whole tenant, the tenant's other
		// machines open to them (a member without a role sees those too, so a lead must not lose them).
		ownOnly := true
		if x.sc.Kind == store.ScopeTeam {
			ownOnly, err = authz.New(st).Allowed(ctx, orgs.Principal(p),
				authz.Check{Action: authz.View, Scope: authz.Scope{Kind: store.ScopeTenant, ID: id}})
			if err != nil {
				return err
			}
		}
		for _, m := range list {
			if x.sc.Kind == store.ScopeTeam && m.TeamID != x.sc.ID && ownOnly {
				continue
			}
			if !s.machines.Visible(ctx, p.User.ID, m) {
				continue
			}
			ms = append(ms, m)
		}
	}
	hs, err := s.machines.HubMachines(ctx, st, ms)
	if err != nil {
		return err
	}
	cur := s.currency(ctx, x.org.Currency, span{from: x.now, to: x.now, loc: x.loc})
	out := make([]machines.HubMachine, 0, len(hs))
	for _, h := range hs {
		if w := h.Week; w != nil && w.Calls > w.Unpriced {
			spend := cur.Micros(w.SpendMicros)
			w.Spend = &spend
		}
		if onlyProblems && len(h.Problems) == 0 {
			continue
		}
		out = append(out, h)
	}
	slices.SortFunc(out, func(a, b machines.HubMachine) int {
		if (len(a.Problems) > 0) != (len(b.Problems) > 0) {
			if len(a.Problems) > 0 {
				return -1
			}
			return 1
		}
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	httpx.WriteJSON(w, http.StatusOK, MachinesResponse{
		Scope:    orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID},
		Currency: cur,
		Machines: out,
	})
	return nil
}

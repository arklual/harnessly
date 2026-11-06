package machines

import (
	"context"
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Runtime says whether a machine's page may open on this hub (View.Runtime).
const (
	// RuntimeReady: the machine opens.
	RuntimeReady = "ready"
	// RuntimeNeedsTrust: a hub without HUB_RUNTIME_DOMAIN would run the page on its own origin and nobody has trusted the
	// machine yet; an owner of its space can (POST /api/machines/{id}/trust).
	RuntimeNeedsTrust = "needs_trust"
	// RuntimeBlocked: the same, and the hub does not offer to trust (HUB_MACHINE_TRUST is off): only HUB_RUNTIME_DOMAIN helps.
	RuntimeBlocked = "blocked"
)

// runtimeState is View.Runtime: what RequireTrustedMachine would answer for the machine.
func (s *Service) runtimeState(ctx context.Context, m store.Machine) (string, error) {
	err := s.auth.RequireTrustedMachine(ctx, m)
	switch {
	case err == nil:
		return RuntimeReady, nil
	case errors.Is(err, httpx.ErrMachineUntrusted):
		if s.cfg.MachineTrust {
			return RuntimeNeedsTrust, nil
		}
		return RuntimeBlocked, nil
	}
	return "", err
}

// trust is POST /api/machines/{id}/trust: an owner of the machine's space accepts that its page runs on the hub's own
// origin, where its script acts with the session of whoever opens it. DELETE takes it back. Both are the owner's alone
// (a member may open a machine, never decide that it is safe), both are audited, and both answer 409 on a hub that
// has a runtime domain or has not switched HUB_MACHINE_TRUST on, where the question does not arise.
func (s *Service) trust(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	m, err := s.Authorize(ctx, p.User.ID, r.PathValue("machineId"), true)
	if err != nil {
		return err
	}
	if s.cfg.SubdomainMode() || !s.cfg.MachineTrust {
		return httpx.Conflict("trust_unavailable", "На этом хабе доверять машине не нужно или нельзя: у неё свой адрес (HUB_RUNTIME_DOMAIN) либо доверие выключено (HUB_MACHINE_TRUST)")
	}
	if r.Method == http.MethodDelete {
		if err := s.db.S().UntrustMachine(ctx, m.ID); err != nil {
			return err
		}
		s.auth.Audit(ctx, p.User.ID, "machine_untrusted", r, m.ID)
	} else {
		if err := s.db.S().TrustMachine(ctx, m.ID, p.User.ID, s.now()); err != nil {
			return err
		}
		s.auth.Audit(ctx, p.User.ID, "machine_trusted", r, m.ID)
	}
	v, err := s.View(ctx, m)
	if err != nil {
		return err
	}
	s.Publish(ctx, m)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machine": v})
	return nil
}

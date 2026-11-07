package env

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// «Применить на машинах» (docs/hub/environment.md, «Apply on the machines»): a layer's managers ask every machine the
// layer reaches for an update run — the list, and the base package with it — «Сейчас» or «При следующем запуске».

const (
	ApplyNow  = "now"
	ApplyNext = "next"
)

type applyIn struct {
	Layer   string `json:"layer"`
	ScopeID string `json:"scopeId"`
	When    string `json:"when"`
}

// ApplyResponse is the answer of POST /api/orgs/{org}/env/apply.
type ApplyResponse struct {
	// Machines the layer reaches; Started of them run now; Waiting do it when they next connect.
	Machines int `json:"machines"`
	Started  int `json:"started"`
	Waiting  int `json:"waiting"`
}

func (s *Service) apply(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in applyIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.When != ApplyNow && in.When != ApplyNext {
		return httpx.Invalid("when", "now или next")
	}
	if in.Layer != store.EnvLayerOrg && in.Layer != store.EnvLayerTenant && in.Layer != store.EnvLayerTeam {
		return httpx.Invalid("layer", "org, tenant или team")
	}
	q, err := s.open(r, s.db.S(), p, FeatureEnv)
	if err != nil {
		return err
	}
	if !q.w.layerExists(in.Layer, in.ScopeID) {
		return httpx.ErrNotFound
	}
	if err := q.rt.require(scopeOf(in.Layer, in.ScopeID)); err != nil {
		return err
	}
	ms, err := q.w.machinesOf(q.ctx, q.st, in.Layer, in.ScopeID)
	if err != nil {
		return err
	}
	var online []string
	if s.reg != nil {
		online = s.reg.Online()
	}
	by := cmp.Or(p.User.Name, p.User.Email)
	now := s.now()
	out := ApplyResponse{}
	for _, m := range ms {
		if m.DisabledAt != nil {
			continue
		}
		out.Machines++
		if in.When == ApplyNow && slices.Contains(online, m.ID) && s.push != nil {
			if setup, _ := s.push.SetupOf(m.ID); setup {
				if _, err := s.startRun(q.ctx, q.w.org.ID, m, hubproto.RunUpdate, nil, by); err == nil {
					out.Started++
					continue
				} else {
					s.log.Warn("environment apply: run not accepted, waits for the next connection", "machine", m.ID, "error", err)
				}
			}
		}
		if err := q.st.AddEnvApplyPending(q.ctx, m.ID, q.w.org.ID, by, now); err != nil {
			return err
		}
		out.Waiting++
	}
	title := "Применили окружение на машинах"
	if in.When == ApplyNext {
		title = "Окружение применится при следующем запуске машин"
	}
	if err := s.record(q.ctx, q.st, q.w, p, in.Layer, in.ScopeID, title, "env.apply", "env_layer", in.ScopeID, nil, out, now); err != nil {
		s.log.Warn("environment apply not recorded", "error", err)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// applyPending runs the update a machine was marked for, a moment after it connected (its list goes out first).
func (s *Service) applyPending(ctx context.Context, machineID string) {
	wait := time.NewTimer(s.applyDelay)
	defer wait.Stop()
	select {
	case <-ctx.Done():
		return // the hub stops; the mark stays for the next connection
	case <-wait.C:
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout+10*time.Second)
	defer cancel()
	st := s.db.S()
	orgID, by, ok, err := st.TakeEnvApplyPending(ctx, machineID)
	if err != nil || !ok {
		if err != nil {
			s.log.Warn("environment apply: pending mark not read", "machine", machineID, "error", err)
		}
		return
	}
	m, err := st.MachineByID(ctx, machineID)
	if err != nil || m.RevokedAt != nil || m.DisabledAt != nil {
		return
	}
	if _, err := s.startRun(ctx, orgID, m, hubproto.RunUpdate, nil, by); err != nil {
		s.log.Warn("environment apply: run at connection not accepted", "machine", machineID, "error", err)
		// Kept for the next connection, even when the run failed because the hub is stopping.
		kctx, kcancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_ = st.AddEnvApplyPending(kctx, machineID, orgID, by, s.now())
		kcancel()
	}
}

// applyDelay lets the list reach a machine that just connected before its run is asked; tests make it short.
var applyDelay = 5 * time.Second

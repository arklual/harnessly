package efficiency

import (
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/switches"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

const maxSpan = 92 * 24 * time.Hour

// Service serves the figures of the screen. Everything is behind the organization flag features.Efficiency.
type Service struct {
	DB *store.DB
	// Gate is the guard of an organization's route (orgs.Service.OrgGate).
	OrgGate  func(tenant.Handler) httpx.Handler
	Features features.Gate
	// Switches decides per person whether the overview and the personal tab come from the hub (SwitchOverview).
	Switches switches.Gate
	Cost     CostFn
	Now      func() time.Time
}

// Mount registers the endpoints.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/efficiency/summary", s.OrgGate(s.summary))
	mux.Handle("GET /api/orgs/{org}/efficiency/pulse", s.OrgGate(s.pulseGet))
	mux.Handle("POST /api/orgs/{org}/efficiency/pulse", s.OrgGate(s.pulsePut))
	s.mountOverview(mux)
}

func (s *Service) summary(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	ctx := r.Context()
	st := s.DB.S()
	o := *t.Org
	var err error
	if s.Features == nil || !s.Features.On(ctx, o.ID, features.Efficiency) {
		return httpx.ErrNotFound
	}
	loc := time.UTC
	if l, err := time.LoadLocation(o.Timezone); err == nil && o.Timezone != "" {
		loc = l
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	to := now.In(loc).Truncate(24*time.Hour).AddDate(0, 0, 1)
	from := to.AddDate(0, 0, -14)
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return httpx.Invalid("from", "Дата в виде ГГГГ-ММ-ДД")
		}
		from = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return httpx.Invalid("to", "Дата в виде ГГГГ-ММ-ДД")
		}
		to = t.AddDate(0, 0, 1)
	}
	if !to.After(from) || to.Sub(from) > maxSpan {
		return httpx.Invalid("from", "Период от одного до 92 дней")
	}
	cost := s.Cost
	if cost == nil {
		if cost, err = RollupCost(ctx, st, o.ID, from, to, loc); err != nil {
			return err
		}
	}
	sum, err := Compute(ctx, st, from, to, loc, cost)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, sum)
	return nil
}

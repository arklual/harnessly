package orgs

import (
	"context"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

// FeatureView is one flag of an organization (docs/hub/api.md, «Feature flags»).
type FeatureView struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Forced is true when the instance decides the flag (HUB_FEATURES_FORCE): the organization's choice changes nothing.
	Forced bool `json:"forced"`
}

// FeaturesResponse is GET /api/orgs/{org}/features.
type FeaturesResponse struct {
	Features []FeatureView `json:"features"`
}

// view is the organization as p sees it, with the flags that are on for it.
func (s *Service) view(ctx context.Context, st store.Store, p *auth.Principal, o store.Org) (View, error) {
	v, err := buildView(ctx, st, Principal(p), o)
	if err != nil {
		return View{}, err
	}
	if s.features != nil {
		v.Features = s.features.List(ctx, o.ID)
	}
	return v, nil
}

func (s *Service) mountFeatures(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/features", s.OrgGate(s.featuresList))
	mux.Handle("PUT /api/orgs/{org}/features/{name}", s.OrgGate(s.featureSet))
}

func (s *Service) featureView(ctx context.Context, orgID, name string) FeatureView {
	if s.features == nil {
		return FeatureView{Name: name}
	}
	return FeatureView{Name: name, Enabled: s.features.On(ctx, orgID, name), Forced: s.features.Forced(name)}
}

// featuresList is every flag the hub knows with its state for the organization; any member of it may read.
func (s *Service) featuresList(w http.ResponseWriter, r *http.Request, tc *tenant.Ctx) error {
	o := *tc.Org
	out := FeaturesResponse{Features: make([]FeatureView, 0, len(features.Known))}
	for _, n := range features.Known {
		out.Features = append(out.Features, s.featureView(r.Context(), o.ID, n))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// featureSet switches a flag for the organization. Only an instance administrator may: what is shipped dark is theirs
// to open. Journal: org.feature.
func (s *Service) featureSet(w http.ResponseWriter, r *http.Request, tc *tenant.Ctx) error {
	p := tc.Person
	var req struct{ Enabled *bool }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	o := *tc.Org
	if !p.Admin {
		return httpx.ErrForbidden
	}
	name := r.PathValue("name")
	if s.features == nil || !features.IsKnown(name) {
		return httpx.ErrNotFound
	}
	if req.Enabled == nil {
		return httpx.Invalid("enabled", "enabled — true или false")
	}
	was := s.features.On(ctx, o.ID, name)
	if err := s.features.Set(ctx, o.ID, name, *req.Enabled, p.User.ID); err != nil {
		return err
	}
	if was != *req.Enabled {
		if err := s.db.S().AddAudit(ctx, &store.AuditEvent{OrgID: o.ID, ActorID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID, Action: "org.feature",
			TargetKind: "feature", TargetID: name, Before: jsonOf(map[string]any{"enabled": was}),
			After: jsonOf(map[string]any{"enabled": *req.Enabled}), At: s.now().UTC()}); err != nil {
			return err
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"feature": s.featureView(ctx, o.ID, name)})
	return nil
}

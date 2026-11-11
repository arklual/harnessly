package orgs

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

// ErrNotEnabled is an area whose flag is off for the organization: it does not exist for it yet.
var ErrNotEnabled = httpx.New(http.StatusNotFound, "not_enabled", "Эта возможность не включена для организации")

func (s *Service) mountEvents(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/events", s.OrgGate(s.orgEvents))
}

// orgEvents is the organization's live stream (docs/hub/api.md, «Organization events»). ?scope= is `all` (the default:
// the whole organization) or the slug (or `tenant:<id>`) of one tenant. The person sees what their role in that scope
// lets them: leads and admins everything about it, a member only what is addressed to them or to everyone.
func (s *Service) orgEvents(w http.ResponseWriter, r *http.Request, tc *tenant.Ctx) error {
	p := tc.Person
	ctx := r.Context()
	st := s.db.S()
	o := *tc.Org
	if s.bus == nil || s.features == nil || !s.features.On(ctx, o.ID, features.OrgEvents) {
		return ErrNotEnabled
	}
	f, err := s.streamFilter(ctx, st, p, o, r.URL.Query().Get("scope"))
	if err != nil {
		return err
	}
	var last *int64
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("lastEventId")
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
		last = &n
	}
	sub := s.bus.Subscribe(o.ID, f, last)
	if sub == nil {
		return httpx.New(http.StatusServiceUnavailable, "shutting_down", "Хаб останавливается")
	}
	events.ServeOrg(w, r, s.bus, sub)
	return nil
}

// streamFilter is what the person may see on the stream of a scope of the organization: a role in it and its tenant.
func (s *Service) streamFilter(ctx context.Context, st store.Store, p *auth.Principal, o store.Org, scope string) (events.Filter, error) {
	f := events.Filter{UserID: p.User.ID, Role: events.RoleMember}
	sps, err := st.SpacesByOrg(ctx, o.ID)
	if err != nil {
		return f, err
	}
	var tenant *store.Space
	if scope != "" && scope != "all" && scope != store.ScopeOrg+":"+o.ID {
		id, isID := strings.CutPrefix(scope, store.ScopeTenant+":")
		for i := range sps {
			if (isID && sps[i].ID == id) || (!isID && sps[i].Slug == scope) {
				tenant = &sps[i]
			}
		}
		if tenant == nil {
			return f, httpx.Invalid("scope", "Неверная область")
		}
		f.Space = tenant.ID
	}
	if p.Admin {
		f.Role = events.RoleAdmin
		return f, nil
	}
	inScope := func(spaceID string) bool { return tenant == nil || tenant.ID == spaceID }
	owned := map[string]bool{}
	for _, sp := range sps {
		owned[sp.ID] = true
	}
	grants, err := st.GrantsOfUser(ctx, p.User.ID)
	if err != nil {
		return f, err
	}
	for _, g := range grants {
		switch {
		case g.ScopeKind == store.ScopeOrg && g.ScopeID == o.ID && g.Role == store.RoleAdmin:
			f.Role = events.RoleAdmin
			return f, nil
		case g.ScopeKind == store.ScopeTenant && g.Role == store.RoleAdmin && owned[g.ScopeID] && inScope(g.ScopeID):
			f.Role = events.RoleLead
		case g.ScopeKind == store.ScopeTeam && g.Role == store.RoleLead:
			t, err := st.TeamByID(ctx, g.ScopeID)
			if err != nil {
				if store.IsNotFound(err) {
					continue
				}
				return f, err
			}
			if owned[t.SpaceID] && inScope(t.SpaceID) {
				f.Role = events.RoleLead
			}
		}
	}
	// A tenant's owner leads it; membership alone is what a tenant stream asks of a member.
	member := false
	for _, sp := range sps {
		if !inScope(sp.ID) {
			continue
		}
		role, err := st.Role(ctx, sp.ID, p.User.ID)
		if err != nil {
			return f, err
		}
		if role == store.RoleOwner {
			f.Role = events.RoleLead
		}
		member = member || role != ""
	}
	if tenant != nil && !member && f.Role != events.RoleLead {
		return f, authz.ErrForbiddenScope.With("scope", store.ScopeTenant+":"+tenant.ID)
	}
	return f, nil
}

// peopleChanged tells the organization's open pages that the people of a tenant (spaceID, "" for the organization as a
// whole) changed, and the workspace pages of the person it happened to that their machines may have. lost says the
// person's role changed: the organization's streams they hold were opened with the old one and are ended, so the
// reconnect is given what the new one allows.
func (s *Service) peopleChanged(orgID, spaceID, userID string, lost bool) {
	if s.onPeople != nil && userID != "" {
		s.onPeople(userID)
	}
	if s.bus == nil {
		return
	}
	events.PublishPeople(s.bus, orgID, events.KindMembers, spaceID)
	if lost && userID != "" {
		s.bus.Kick(orgID, userID)
	}
}

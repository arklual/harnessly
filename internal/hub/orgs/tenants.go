package orgs

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

// «Новый тенант» of an organization (GitHub issue 23): the tenant is made in the organization in one transaction, so a
// failure leaves no space of its own somewhere else, and a retry of the same press (the same requestId) gets the tenant
// the first one made instead of a second.

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// tenantMade runs inside the transaction after the tenant is made and bound; a test fails it to see nothing is left.
var tenantMade = func(context.Context, store.Store, store.Space) error { return nil }

func (s *Service) createTenant(w http.ResponseWriter, r *http.Request, tc *tenant.Ctx) error {
	p := tc.Person
	var req struct {
		Name      string
		RequestID string
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := CleanName(req.Name, "Назовите тенант")
	if err != nil {
		return err
	}
	if req.RequestID != "" && !requestIDRe.MatchString(req.RequestID) {
		return httpx.Invalid("requestId", "Неверный ключ запроса")
	}
	ctx := r.Context()
	st := s.db.S()
	o := *tc.Org
	if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeOrg, ID: o.ID}}); err != nil {
		return err
	}
	// A retry: the tenant the first press made, whatever happened to the answer.
	if sp, ok, err := s.madeBefore(ctx, p, o, req.RequestID); err != nil || ok {
		if err == nil {
			err = s.writeTenant(ctx, w, http.StatusOK, p, sp)
		}
		return err
	}
	if ok, retry := s.lim.Allow("tenant:"+p.User.ID, 30, time.Hour); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	if n, err := st.CountOwnedSpaces(ctx, p.User.ID); err != nil {
		return err
	} else if n >= s.cfg.MaxSpaces() {
		return httpx.Conflict("space_limit", "У вас уже много пространств")
	}
	now := s.now().UTC()
	var sp store.Space
	// A taken slug is a unique violation that spoils the transaction; it is tried again with the next one. So is a
	// concurrent retry that made the tenant first: then that one is the answer.
	for n := 1; ; n++ {
		sp = store.Space{Slug: spaces.Slug(name, n), Name: name, CreatedBy: p.User.ID, CreatedAt: now, OrgID: o.ID}
		err = s.db.Tx(ctx, func(st store.Store) error { return s.makeTenant(ctx, st, p, o, &sp, req.RequestID, now) })
		if err == nil {
			break
		}
		if !store.IsUnique(err) || n > 24 {
			return err
		}
		if made, ok, lookErr := s.madeBefore(ctx, p, o, req.RequestID); lookErr != nil {
			return lookErr
		} else if ok {
			return s.writeTenant(ctx, w, http.StatusOK, p, made)
		}
	}
	s.auth.Audit(ctx, p.User.ID, "space_created", r, sp.ID)
	return s.writeTenant(ctx, w, http.StatusCreated, p, sp)
}

func (s *Service) makeTenant(ctx context.Context, st store.Store, p *auth.Principal, o store.Org, sp *store.Space, key string, now time.Time) error {
	// CreateSpace binds the organization in the same insert and makes the author the tenant's owner (its admin).
	if err := st.CreateSpace(ctx, sp); err != nil {
		return err
	}
	if key != "" {
		if err := st.SetSpaceCreateKey(ctx, sp.ID, key); err != nil {
			return err
		}
	}
	if err := st.AddAudit(ctx, &store.AuditEvent{OrgID: o.ID, SpaceID: sp.ID, ActorID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID,
		Action: "tenant.create", TargetKind: "tenant", TargetID: sp.ID, After: jsonOf(map[string]any{"name": sp.Name, "slug": sp.Slug}), At: now}); err != nil {
		return err
	}
	return tenantMade(ctx, st, *sp)
}

// madeBefore finds the tenant an earlier request of this key made. A key that made a space of another organization is
// not this request's: 409, and nothing is made.
func (s *Service) madeBefore(ctx context.Context, p *auth.Principal, o store.Org, key string) (store.Space, bool, error) {
	if key == "" {
		return store.Space{}, false, nil
	}
	sp, err := s.db.S().SpaceByCreateKey(ctx, p.User.ID, key)
	if store.IsNotFound(err) {
		return store.Space{}, false, nil
	}
	if err != nil {
		return store.Space{}, false, err
	}
	if sp.OrgID != o.ID {
		return store.Space{}, false, httpx.Conflict("request_reused", "Этот запрос уже создал пространство в другом месте")
	}
	return sp, true, nil
}

// writeTenant answers the tenant as the organization shows it and the space as the spaces endpoints do (the author's
// role in it), so the screen that made it goes on to invites and machines without a second read.
func (s *Service) writeTenant(ctx context.Context, w http.ResponseWriter, code int, p *auth.Principal, sp store.Space) error {
	st := s.db.S()
	tv, err := tenantView(ctx, st, sp)
	if err != nil {
		return err
	}
	role, err := st.Role(ctx, sp.ID, p.User.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, code, map[string]any{"tenant": tv, "space": spaces.View(sp, role)})
	return nil
}

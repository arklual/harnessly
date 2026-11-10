// Package orgs is the hub's organizations: making one from one's spaces, moving tenants between them, and the view of an
// organization a person gets (docs/hub/orgs.md). The endpoints follow HUB_ORGS: absent while it is off, for instance
// administrators only in shadow, for everyone when on.
package orgs

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/rates"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const (
	maxNameRunes = 60
	// maxSpacesPerCreate bounds one request's list of spaces to move.
	maxSpacesPerCreate = 50
)

// Deps are what the service is built from.
type Deps struct {
	Config  *config.Config
	DB      *store.DB
	Auth    *auth.Service
	Limiter *ratelimit.Limiter
	Log     *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// LimitsChanged runs after a limit rule was set or removed (the machines' lists are recomputed); nil: nothing.
	LimitsChanged func()
	// Rates is the exchange rate money is shown in; nil shows dollars.
	Rates *rates.Book
	// Features are the organizations' flags; nil: every flag is off.
	Features *features.Store
	// Events is the bus of the organizations' live stream; nil: there is none.
	Events events.OrgBus
	// EnvChanged runs after a setting the machines' environment lists carry changed (the rule for apps); nil: nothing.
	EnvChanged func()
}

// Service is the organizations side of the hub.
type Service struct {
	cfg  *config.Config
	db   *store.DB
	auth *auth.Service
	lim  *ratelimit.Limiter
	log  *slog.Logger
	now  func() time.Time

	onLimits func()
	onEnv    func()
	rates    *rates.Book
	features *features.Store
	bus      events.OrgBus
}

// New makes the service.
func New(d Deps) *Service {
	s := &Service{cfg: d.Config, db: d.DB, auth: d.Auth, lim: d.Limiter, log: d.Log, now: d.Now, onLimits: d.LimitsChanged, onEnv: d.EnvChanged, rates: d.Rates, features: d.Features, bus: d.Events}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.lim == nil {
		s.lim = ratelimit.New()
	}
	return s
}

// Handler is an endpoint of the organizations: it runs for a signed-in person the flag lets in.
type Handler func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error

// Gate wraps an endpoint in the session check and HUB_ORGS: off or (shadow and not an instance administrator) is 404.
func (s *Service) Gate(h Handler) httpx.Handler {
	return s.auth.Require(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
		switch s.cfg.Orgs {
		case config.OrgsOn:
		case config.OrgsShadow:
			if !p.Admin {
				return httpx.ErrNotFound
			}
		default:
			return httpx.ErrNotFound
		}
		return h(w, r, p)
	})
}

// Mount registers the endpoints.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs", s.Gate(s.list))
	mux.Handle("POST /api/orgs", s.Gate(s.create))
	mux.Handle("GET /api/orgs/{org}", s.Gate(s.get))
	mux.Handle("PATCH /api/orgs/{org}", s.Gate(s.patch))
	mux.Handle("POST /api/orgs/{org}/tenants", s.Gate(s.createTenant))
	mux.Handle("POST /api/orgs/{org}/tenants/move", s.Gate(s.move))
	mux.Handle("DELETE /api/orgs/{org}/tenants/{spaceId}", s.Gate(s.deleteTenant))
	s.mountTeams(mux)
	s.mountLimits(mux)
	s.mountFeatures(mux)
	s.mountEvents(mux)
}

// Principal is the person as the rights module sees them.
func Principal(p *auth.Principal) authz.Principal {
	return authz.Principal{UserID: p.User.ID, InstanceAdmin: p.Admin}
}

// Resolve finds the organization of the path by id or slug, and checks the person has something in it (an instance
// administrator has everything): otherwise it does not exist for them.
func Resolve(ctx context.Context, st store.Store, p *auth.Principal, ref string) (store.Org, error) {
	o, err := st.OrgByID(ctx, ref)
	if store.IsNotFound(err) {
		o, err = st.OrgBySlug(ctx, ref)
	}
	if store.IsNotFound(err) {
		return store.Org{}, httpx.ErrNotFound
	}
	if err != nil {
		return store.Org{}, err
	}
	if p.Admin {
		return o, nil
	}
	mine, err := st.OrgsForUser(ctx, p.User.ID)
	if err != nil {
		return store.Org{}, err
	}
	for _, m := range mine {
		if m.ID == o.ID {
			return o, nil
		}
	}
	return store.Org{}, httpx.ErrNotFound
}

// CleanName trims a name of an organization or a team and checks it is 1–60 characters without control characters.
func CleanName(raw, empty string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	n := len([]rune(name))
	if n == 0 {
		return "", httpx.Invalid("name", empty)
	}
	if n > maxNameRunes {
		return "", httpx.Invalid("name", "Название длиннее 60 символов")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", httpx.Invalid("name", "В названии есть недопустимые символы")
		}
	}
	return name, nil
}

type orgListItem struct {
	OrgView
	Role string `json:"role"`
}

type spaceRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func (s *Service) list(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	var all []store.Org
	var err error
	if p.Admin && s.cfg.Orgs == config.OrgsShadow {
		all, err = st.AllOrgs(ctx)
	} else {
		all, err = st.OrgsForUser(ctx, p.User.ID)
	}
	if err != nil {
		return err
	}
	grants, err := st.GrantsOfUser(ctx, p.User.ID)
	if err != nil {
		return err
	}
	out := struct {
		Orgs        []orgListItem `json:"orgs"`
		LooseSpaces []spaceRef    `json:"looseSpaces"`
	}{Orgs: []orgListItem{}, LooseSpaces: []spaceRef{}}
	for _, o := range all {
		role, err := roleIn(ctx, st, p.User.ID, o, grants)
		if err != nil {
			return err
		}
		out.Orgs = append(out.Orgs, orgListItem{OrgView: orgView(o), Role: role})
	}
	mine, err := st.SpacesByUser(ctx, p.User.ID)
	if err != nil {
		return err
	}
	for _, sp := range mine {
		if sp.OrgID == "" {
			out.LooseSpaces = append(out.LooseSpaces, spaceRef{ID: sp.ID, Slug: sp.Slug, Name: sp.Name, Role: sp.Role})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// defaultTenantName is the first tenant of an organization that was not asked to name it.
const defaultTenantName = "Основное"

func (s *Service) create(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		Name     string
		SpaceIDs []string
		// TenantName asks for a first tenant made together with the organization (HRN-762): an organization without one
		// is a dead end for a person who has no tenant to move in. A blank name means "Основное". Absent: the
		// organization is made as it always was.
		TenantName *string
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := CleanName(req.Name, "Назовите организацию")
	if err != nil {
		return err
	}
	tenant := ""
	if req.TenantName != nil {
		tenant = strings.TrimSpace(*req.TenantName)
		if tenant == "" {
			tenant = defaultTenantName
		}
		if tenant, err = CleanName(tenant, "Назовите тенант"); err != nil {
			return err
		}
	}
	if len(req.SpaceIDs) > maxSpacesPerCreate {
		return httpx.Invalid("spaceIds", "Слишком много пространств за раз")
	}
	ctx := r.Context()
	if ok, retry := s.lim.Allow("org:"+p.User.ID, 10, time.Hour); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	now := s.now().UTC()
	var o store.Org
	// A taken slug is a unique violation that spoils the transaction; the whole of it is tried again with the next one.
	for n := 1; ; n++ {
		o = store.Org{Slug: spaces.Slug(name, n), Name: name, CreatedBy: p.User.ID, CreatedAt: now}
		err = s.db.Tx(ctx, func(st store.Store) error { return s.createIn(ctx, st, p, &o, req.SpaceIDs, tenant, n, now) })
		if err == nil {
			break
		}
		if !store.IsUnique(err) || n > 24 {
			return err
		}
	}
	v, err := s.view(ctx, s.db.S(), p, o)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, v)
	return nil
}

func (s *Service) createIn(ctx context.Context, st store.Store, p *auth.Principal, o *store.Org, spaceIDs []string, tenant string, attempt int, now time.Time) error {
	seen := map[string]bool{}
	var moved []store.Space
	for _, id := range spaceIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		sp, err := st.SpaceByID(ctx, id)
		if err != nil && !store.IsNotFound(err) {
			return err
		}
		role := ""
		if err == nil {
			if role, err = st.Role(ctx, id, p.User.ID); err != nil {
				return err
			}
		}
		if role != store.RoleOwner || sp.OrgID != "" {
			return authz.ErrForbiddenScope.With("scope", store.ScopeTenant+":"+id).With("needs", authz.NeedsTenantAdmin).With("spaceId", id)
		}
		moved = append(moved, sp)
	}
	if err := st.CreateOrg(ctx, o); err != nil {
		return err
	}
	if _, err := st.Grant(ctx, store.RoleGrant{UserID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID, Role: store.RoleAdmin, GrantedBy: p.User.ID, GrantedAt: now}); err != nil {
		return err
	}
	if err := st.AddAudit(ctx, &store.AuditEvent{OrgID: o.ID, ActorID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID, Action: "org.create",
		TargetKind: "org", TargetID: o.ID, After: jsonOf(map[string]any{"name": o.Name, "slug": o.Slug}), At: now}); err != nil {
		return err
	}
	if tenant != "" {
		sp := store.Space{Slug: spaces.Slug(tenant, attempt), Name: tenant, CreatedBy: p.User.ID, CreatedAt: now}
		if err := st.CreateSpace(ctx, &sp); err != nil {
			return err
		}
		if err := st.SetSpaceOrg(ctx, sp.ID, o.ID); err != nil {
			return err
		}
		if err := st.AddAudit(ctx, &store.AuditEvent{OrgID: o.ID, SpaceID: sp.ID, ActorID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID,
			Action: "tenant.create", TargetKind: "tenant", TargetID: sp.ID, After: jsonOf(map[string]any{"name": sp.Name, "slug": sp.Slug}), At: now}); err != nil {
			return err
		}
	}
	for _, sp := range moved {
		if err := st.SetSpaceOrg(ctx, sp.ID, o.ID); err != nil {
			return err
		}
		if err := st.AddAudit(ctx, &store.AuditEvent{OrgID: o.ID, SpaceID: sp.ID, ActorID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID,
			Action: "tenant.move", TargetKind: "tenant", TargetID: sp.ID, Before: jsonOf(map[string]any{"orgId": nil}),
			After: jsonOf(map[string]any{"orgId": o.ID}), At: now}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) get(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	o, err := Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	v, err := s.view(ctx, st, p, o)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) patch(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		Name     *string
		Timezone *string
		Currency *string
		// AppsConsent is the rule for apps (HRN-915): "ask" or "auto" («Не спрашивать разрешений»). It is the
		// organization's admin's, by a person's session: Gate lets no machine's or agent's token in.
		AppsConsent *string
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	if _, err := authz.New(s.db.S()).Can(ctx, Principal(p), authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeOrg, ID: o.ID}}); err != nil {
		return err
	}
	before := map[string]any{}
	after := map[string]any{}
	next := o
	if req.Name != nil {
		name, err := CleanName(*req.Name, "Назовите организацию")
		if err != nil {
			return err
		}
		next.Name = name
	}
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil || *req.Timezone == "" || *req.Timezone == "Local" {
			return httpx.Invalid("timezone", "Неизвестный часовой пояс")
		}
		next.Timezone = *req.Timezone
	}
	if req.Currency != nil {
		if *req.Currency != "RUB" && *req.Currency != "USD" {
			return httpx.Invalid("currency", "Валюта — RUB или USD")
		}
		next.Currency = *req.Currency
	}
	if req.AppsConsent != nil {
		if *req.AppsConsent != store.AppsConsentAsk && *req.AppsConsent != store.AppsConsentAuto {
			return httpx.Invalid("appsConsent", "Правило — ask или auto")
		}
		next.AppsConsent = *req.AppsConsent
	}
	for _, f := range []struct {
		key      string
		was, now string
	}{{"name", o.Name, next.Name}, {"timezone", o.Timezone, next.Timezone}, {"currency", o.Currency, next.Currency}, {"appsConsent", o.AppsConsent, next.AppsConsent}} {
		if f.was != f.now {
			before[f.key], after[f.key] = f.was, f.now
		}
	}
	if len(after) > 0 {
		err = s.db.Tx(ctx, func(st store.Store) error {
			if err := st.UpdateOrg(ctx, next); err != nil {
				return err
			}
			return st.AddAudit(ctx, &store.AuditEvent{OrgID: o.ID, ActorID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: o.ID, Action: "org.update",
				TargetKind: "org", TargetID: o.ID, Before: jsonOf(before), After: jsonOf(after), At: s.now().UTC()})
		})
		if err != nil {
			return err
		}
		if next.AppsConsent != o.AppsConsent && s.onEnv != nil {
			s.onEnv() // the machines' lists carry the rule: they get it now
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"org": orgView(next)})
	return nil
}

// move brings a space into the organization: the mover manages the organization, and owns the space when it has none,
// or is an admin of the organization it is in (a tenant moves only between organizations one administers both of).
func (s *Service) move(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ SpaceID string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.SpaceID == "" {
		return httpx.Invalid("spaceId", "Выберите пространство")
	}
	ctx := r.Context()
	st := s.db.S()
	o, err := Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	az := authz.New(st)
	pr := Principal(p)
	if _, err := az.Can(ctx, pr, authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeOrg, ID: o.ID}}); err != nil {
		return err
	}
	sp, err := st.SpaceByID(ctx, req.SpaceID)
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	forbidden := authz.ErrForbiddenScope.With("scope", store.ScopeTenant+":"+sp.ID).With("spaceId", sp.ID)
	switch sp.OrgID {
	case o.ID:
		tv, err := tenantView(ctx, st, sp)
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"tenant": tv})
		return nil
	case "":
		role, err := st.Role(ctx, sp.ID, p.User.ID)
		if err != nil {
			return err
		}
		if role != store.RoleOwner && !p.Admin {
			return forbidden.With("needs", authz.NeedsTenantAdmin)
		}
	default:
		if _, err := az.Can(ctx, pr, authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeOrg, ID: sp.OrgID}}); err != nil {
			var he *httpx.Error
			if errors.As(err, &he) && he.Code == authz.ErrForbiddenScope.Code {
				return forbidden.With("needs", authz.NeedsOrgAdmin)
			}
			return err
		}
	}
	from := sp.OrgID
	now := s.now().UTC()
	err = s.db.Tx(ctx, func(st store.Store) error {
		if err := st.SetSpaceOrg(ctx, sp.ID, o.ID); err != nil {
			return err
		}
		if err := st.DeleteLimitRulesOfTenant(ctx, sp.ID); err != nil {
			return err
		}
		var was any
		if from != "" {
			was = from
		}
		for _, in := range []string{o.ID, from} {
			if in == "" {
				continue
			}
			if err := st.AddAudit(ctx, &store.AuditEvent{OrgID: in, SpaceID: sp.ID, ActorID: p.User.ID, ScopeKind: store.ScopeOrg, ScopeID: in,
				Action: "tenant.move", TargetKind: "tenant", TargetID: sp.ID, Before: jsonOf(map[string]any{"orgId": was}),
				After: jsonOf(map[string]any{"orgId": o.ID}), At: now}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	sp.OrgID = o.ID
	tv, err := tenantView(ctx, st, sp)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tenant": tv})
	return nil
}

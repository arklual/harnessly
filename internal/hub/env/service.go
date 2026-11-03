package env

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The names an organization's features are asked for by.
const (
	FeatureEnv     = "env"
	FeatureCatalog = "catalog"
)

// ChangeKind is the kind of the org_changes rows this package writes.
const ChangeKind = "env"

// FeatureGate says whether a feature is on for an organization. A nil gate means every feature is off.
type FeatureGate interface {
	On(ctx context.Context, orgID, name string) bool
}

// Publisher tells the people watching an organization that something changed. A nil publisher tells nobody.
type Publisher interface {
	Publish(orgID, kind string, data any)
}

// ErrNotEnabled is the answer of a feature that is off for the organization.
var ErrNotEnabled = httpx.New(http.StatusNotFound, "not_enabled", "Раздел не включён")

// Deps are what the service is built from.
type Deps struct {
	DB *store.DB
	// Gate wraps an endpoint in the session check and the organizations flag (orgs.Service.Gate).
	Gate      func(orgs.Handler) httpx.Handler
	Features  FeatureGate
	Publisher Publisher
	// Changed runs after the environment of an organization changed (the machines' lists are recomputed); nil: nothing.
	Changed func()
	Log     *slog.Logger
	Now     func() time.Time
}

// Service is the environment and the catalog of the organizations.
type Service struct {
	db      *store.DB
	gate    func(orgs.Handler) httpx.Handler
	feat    FeatureGate
	pub     Publisher
	changed func()
	log     *slog.Logger
	now     func() time.Time
}

// New makes the service.
func New(d Deps) *Service {
	s := &Service{db: d.DB, gate: d.Gate, feat: d.Features, pub: d.Publisher, changed: d.Changed, log: d.Log, now: d.Now}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	return s
}

// Mount registers the endpoints.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/env", s.gate(s.getEnv))
	mux.Handle("GET /api/orgs/{org}/env/items/{entryId}", s.gate(s.getCard))
	mux.Handle("POST /api/orgs/{org}/env/entries", s.gate(s.addEntry))
	mux.Handle("PATCH /api/orgs/{org}/env/entries/{entryId}", s.gate(s.patchEntry))
	mux.Handle("DELETE /api/orgs/{org}/env/entries/{entryId}", s.gate(s.deleteEntry))
	mux.Handle("GET /api/orgs/{org}/catalog", s.gate(s.listCatalog))
	mux.Handle("POST /api/orgs/{org}/catalog", s.gate(s.addCatalog))
	mux.Handle("PATCH /api/orgs/{org}/catalog/{itemId}", s.gate(s.patchCatalog))
	mux.Handle("DELETE /api/orgs/{org}/catalog/{itemId}", s.gate(s.deleteCatalog))
	mux.Handle("GET /api/me/env", s.gate(s.meEnv))
	mux.Handle("PUT /api/me/env/{itemId}", s.gate(s.putMeEnv))
}

// feature is nil when the feature is on for the organization, ErrNotEnabled otherwise.
func (s *Service) feature(ctx context.Context, orgID, name string) error {
	if s.feat == nil || !s.feat.On(ctx, orgID, name) {
		return ErrNotEnabled
	}
	return nil
}

// On says whether the environment feature is on for an organization.
func (s *Service) On(ctx context.Context, orgID string) bool {
	return s.feature(ctx, orgID, FeatureEnv) == nil
}

func (s *Service) kick() {
	if s.changed != nil {
		s.changed()
	}
}

func (s *Service) publish(orgID, scope, entryID string, rev int64) {
	if s.pub != nil {
		s.pub.Publish(orgID, "env.changed", map[string]any{"scope": scope, "entryId": entryID, "rev": rev})
	}
}

// world is an organization as the environment sees it, read once for a request.
type world struct {
	org      store.Org
	tenants  []store.Space
	teams    map[string][]store.Team // by tenant id
	teamByID map[string]store.Team
	tenantBy map[string]store.Space
	items    map[string]store.CatalogItem
	entries  []store.EnvEntry
}

func loadWorld(ctx context.Context, st store.Store, o store.Org) (*world, error) {
	w := &world{org: o, teams: map[string][]store.Team{}, teamByID: map[string]store.Team{}, tenantBy: map[string]store.Space{},
		items: map[string]store.CatalogItem{}}
	var err error
	if w.tenants, err = st.SpacesByOrg(ctx, o.ID); err != nil {
		return nil, err
	}
	for _, sp := range w.tenants {
		w.tenantBy[sp.ID] = sp
		ts, err := st.TeamsBySpace(ctx, sp.ID)
		if err != nil {
			return nil, err
		}
		w.teams[sp.ID] = ts
		for _, t := range ts {
			w.teamByID[t.ID] = t
		}
	}
	items, err := st.CatalogItems(ctx, o.ID, true)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		w.items[it.ID] = it
	}
	if w.entries, err = st.EnvEntriesOfOrg(ctx, o.ID); err != nil {
		return nil, err
	}
	return w, nil
}

// tenant finds a tenant of the organization by id or slug.
func (w *world) tenant(ref string) (store.Space, bool) {
	for _, sp := range w.tenants {
		if sp.ID == ref || sp.Slug == ref {
			return sp, true
		}
	}
	return store.Space{}, false
}

// layerOf is the tenant a scope of a layer sits in ("" for the organization).
func (w *world) tenantOf(layer, scopeID string) string {
	switch layer {
	case store.EnvLayerTenant:
		return scopeID
	case store.EnvLayerTeam:
		return w.teamByID[scopeID].SpaceID
	}
	return ""
}

// scopeOf names a layer's scope for the rights module.
func scopeOf(layer, scopeID string) authz.Scope {
	switch layer {
	case store.EnvLayerOrg:
		return authz.Scope{Kind: store.ScopeOrg, ID: scopeID}
	case store.EnvLayerTenant:
		return authz.Scope{Kind: store.ScopeTenant, ID: scopeID}
	}
	return authz.Scope{Kind: store.ScopeTeam, ID: scopeID}
}

// layerExists checks that a layer's scope is the organization's own.
func (w *world) layerExists(layer, scopeID string) bool {
	switch layer {
	case store.EnvLayerOrg:
		return scopeID == w.org.ID
	case store.EnvLayerTenant:
		_, ok := w.tenantBy[scopeID]
		return ok
	case store.EnvLayerTeam:
		_, ok := w.teamByID[scopeID]
		return ok
	}
	return false
}

// resolverItems and resolverEntries put the stored rows in the resolver's shape.
func (w *world) resolverItems() map[string]Item {
	out := make(map[string]Item, len(w.items))
	for id, it := range w.items {
		out[id] = Item{ID: it.ID, Kind: it.Kind, Name: it.Name, Hint: it.Hint, Source: []byte(it.Source), Archived: it.ArchivedAt != nil}
	}
	return out
}

func resolverEntries(es []store.EnvEntry) []Entry {
	out := make([]Entry, len(es))
	for i, e := range es {
		out[i] = Entry{ID: e.ID, Layer: e.Layer, ScopeID: e.ScopeID, ItemID: e.ItemID, Enabled: e.Enabled, Pinned: e.Pinned,
			Audience: ParseAudience(e.Audience)}
	}
	return out
}

// rights answers what a person may do in an organization, remembering the answers of the request.
type rights struct {
	ctx  context.Context
	az   authz.Authz
	p    authz.Principal
	memo map[string]bool
}

func newRights(ctx context.Context, st store.Store, p *auth.Principal) *rights {
	return &rights{ctx: ctx, az: authz.New(st), p: orgs.Principal(p), memo: map[string]bool{}}
}

// manages says whether the person manages a scope; only a failure of the store is an error.
func (r *rights) manages(sc authz.Scope) (bool, error) {
	if v, ok := r.memo[sc.Key()]; ok {
		return v, nil
	}
	_, err := r.az.Can(r.ctx, r.p, authz.Check{Action: authz.Manage, Scope: sc})
	var he *httpx.Error
	switch {
	case err == nil:
		r.memo[sc.Key()] = true
		return true, nil
	case errors.As(err, &he):
		r.memo[sc.Key()] = false
		return false, nil
	}
	return false, err
}

// require is manages as an error: the one the rights module gives for the scope.
func (r *rights) require(sc authz.Scope) error {
	_, err := r.az.Can(r.ctx, r.p, authz.Check{Action: authz.Manage, Scope: sc})
	return err
}

// sees says whether the person may look at a tenant's environment: they manage the tenant or one of its teams.
func (r *rights) sees(w *world, tenantID string) (bool, error) {
	ok, err := r.manages(authz.Scope{Kind: store.ScopeTenant, ID: tenantID})
	if err != nil || ok {
		return ok, err
	}
	for _, t := range w.teams[tenantID] {
		if ok, err = r.manages(authz.Scope{Kind: store.ScopeTeam, ID: t.ID}); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// visibleTenants are the tenants of the organization the person may look at.
func (r *rights) visibleTenants(w *world) ([]store.Space, error) {
	var out []store.Space
	for _, sp := range w.tenants {
		ok, err := r.sees(w, sp.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, sp)
		}
	}
	return out, nil
}

var kindRu = map[string]string{store.CatalogSkill: "скилл", store.CatalogMCP: "MCP", store.CatalogRepo: "репозиторий", store.CatalogDoc: "документ"}

// whereRu says where a layer is, for the title of a change.
func (w *world) whereRu(layer, scopeID string) string {
	switch layer {
	case store.EnvLayerOrg:
		return "организации"
	case store.EnvLayerTenant:
		return fmt.Sprintf("тенанте «%s»", w.tenantBy[scopeID].Name)
	}
	return fmt.Sprintf("команде «%s»", w.teamByID[scopeID].Name)
}

// machinesIn counts the live machines a scope's environment reaches.
func (w *world) machinesIn(ctx context.Context, st store.Store, layer, scopeID string) (int, error) {
	n := 0
	for _, sp := range w.tenants {
		if layer != store.EnvLayerOrg && w.tenantOf(layer, scopeID) != sp.ID {
			continue
		}
		ms, err := st.MachinesBySpace(ctx, sp.ID)
		if err != nil {
			return 0, err
		}
		for _, m := range ms {
			if layer == store.EnvLayerTeam && m.TeamID != scopeID {
				continue
			}
			n++
		}
	}
	return n, nil
}

// record writes the change and the audit entry of one edit, inside the edit's transaction.
func (s *Service) record(ctx context.Context, st store.Store, w *world, p *auth.Principal, layer, scopeID, title, action, targetKind, targetID string,
	before, after any, now time.Time) error {
	sc := scopeOf(layer, scopeID)
	chain, err := orgs.Chain(ctx, st, w.org, sc)
	if err != nil {
		return err
	}
	n, err := w.machinesIn(ctx, st, layer, scopeID)
	if err != nil {
		return err
	}
	if _, err = st.AddChange(ctx, store.Change{OrgKey: w.org.ID, ScopeKind: sc.Kind, ScopeID: sc.ID, Kind: ChangeKind, Title: title, At: now,
		AuthorID: p.User.ID, Source: store.ChangeManual, Machines: n, CreatedAt: now}); err != nil {
		return err
	}
	return st.AddAudit(ctx, orgs.Journal(chain, p.User.ID, action, targetKind, targetID, before, after, now))
}

// StaticFeatures is a gate with the same features on for every organization.
func StaticFeatures(names []string) FeatureGate {
	on := make(map[string]bool, len(names))
	for _, n := range names {
		on[n] = true
	}
	return staticGate(on)
}

type staticGate map[string]bool

func (g staticGate) On(_ context.Context, _, name string) bool { return g[name] }

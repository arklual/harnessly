package env

import (
	"context"
	"net/http"
	"slices"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// MeSpace and MeOrg name where a person's environment is.
type MeSpace struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type MeOrg struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MeItem is one item of a person's environment.
type MeItem struct {
	ItemID     string `json:"itemId"`
	EntryID    string `json:"entryId"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Hint       string `json:"hint"`
	Layer      string `json:"layer"`
	ScopeID    string `json:"scopeId"`
	LayerTitle string `json:"layerTitle"`
	Pinned     bool   `json:"pinned"`
	Off        bool   `json:"off"`
	CanToggle  bool   `json:"canToggle"`
}

// MeResponse is GET /api/me/env.
type MeResponse struct {
	Space MeSpace  `json:"space"`
	Org   *MeOrg   `json:"org"`
	Items []MeItem `json:"items"`
}

// me is a request about the environment of the person themself in one tenant.
type me struct {
	ctx    context.Context
	st     store.Store
	space  store.Space
	org    store.Org
	w      *world
	person Person
}

// layerTitle names a layer the way the screens of the organization do.
func (w *world) layerTitle(layer, scopeID string) string {
	switch layer {
	case store.EnvLayerOrg:
		return "Организация " + w.org.Name
	case store.EnvLayerTenant:
		return "Тенант " + w.tenantBy[scopeID].Name
	}
	return "Команда " + w.teamByID[scopeID].Name
}

// openMe finds the tenant (?space= or the body's space: an id or a slug; the only one the person is in when none is
// named), checks the person is in it and that the environment is on for its organization.
func (s *Service) openMe(r *http.Request, st store.Store, p *auth.Principal, ref string) (*me, error) {
	ctx := r.Context()
	mine, err := st.SpacesByUser(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	var sp store.Space
	switch {
	case ref == "" && len(mine) == 1:
		sp = mine[0].Space
	case ref == "":
		return nil, httpx.Invalid("space", "Укажите пространство")
	default:
		i := slices.IndexFunc(mine, func(m store.SpaceWithRole) bool { return m.ID == ref || m.Slug == ref })
		if i < 0 {
			return nil, httpx.ErrNotFound
		}
		sp = mine[i].Space
	}
	if sp.OrgID == "" {
		return nil, ErrNotEnabled
	}
	if err := s.feature(ctx, sp.OrgID, FeatureEnv); err != nil {
		return nil, err
	}
	o, err := st.OrgByID(ctx, sp.OrgID)
	if err != nil {
		return nil, err
	}
	w, err := loadWorld(ctx, st, o)
	if err != nil {
		return nil, err
	}
	tms, err := st.TeamsOfUser(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	var teams []string
	for _, tm := range tms {
		if w.teamByID[tm.TeamID].SpaceID == sp.ID {
			teams = append(teams, tm.TeamID)
		}
	}
	slices.Sort(teams)
	off, err := st.EnvPersonOffs(ctx, p.User.ID, sp.ID)
	if err != nil {
		return nil, err
	}
	return &me{ctx: ctx, st: st, space: sp, org: o, w: w, person: Person{OrgID: o.ID, SpaceID: sp.ID, Teams: teams, Off: off}}, nil
}

func (m *me) effective() []Effective {
	return Resolve(m.w.resolverItems(), resolverEntries(m.w.entries), m.person)
}

func (s *Service) meEnv(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.openMe(r, s.db.S(), p, r.URL.Query().Get("space"))
	if err != nil {
		return err
	}
	out := MeResponse{Space: MeSpace{ID: m.space.ID, Slug: m.space.Slug, Name: m.space.Name}, Org: &MeOrg{ID: m.org.ID, Name: m.org.Name}, Items: []MeItem{}}
	for _, e := range m.effective() {
		scope := m.org.ID
		switch e.Layer {
		case store.EnvLayerTenant:
			scope = m.space.ID
		case store.EnvLayerTeam:
			scope = m.entryScope(e.EntryID)
		}
		out.Items = append(out.Items, MeItem{ItemID: e.ID, EntryID: e.EntryID, Kind: e.Kind, Name: e.Name, Hint: e.Hint, Layer: e.Layer, ScopeID: scope,
			LayerTitle: m.w.layerTitle(e.Layer, scope), Pinned: e.Pinned, Off: e.Off, CanToggle: !e.Pinned})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (m *me) entryScope(entryID string) string {
	for _, e := range m.w.entries {
		if e.ID == entryID {
			return e.ScopeID
		}
	}
	return ""
}

// offIn is the body of PUT /api/me/env/{itemId}.
type offIn struct {
	Off   *bool  `json:"off"`
	Space string `json:"space"`
}

// putMeEnv switches an item off or back on for the person in a tenant. It is theirs alone: no change of the
// organization, no machine list; only the audit trail remembers it.
func (s *Service) putMeEnv(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in offIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Off == nil {
		return httpx.Invalid("off", "Нужно true или false")
	}
	space := r.URL.Query().Get("space")
	if space == "" {
		space = in.Space
	}
	itemID := r.PathValue("itemId")
	var out MeItem
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		m, err := s.openMe(r, st, p, space)
		if err != nil {
			return err
		}
		var eff *Effective
		for _, e := range m.effective() {
			if e.ID == itemID {
				eff = &e
				break
			}
		}
		if eff == nil {
			return httpx.ErrNotFound
		}
		if eff.Pinned {
			return httpx.Conflict("locked", "Закреплено: выключить нельзя")
		}
		was := m.person.Off[itemID]
		if was != *in.Off {
			if err := st.SetEnvPersonOff(r.Context(), p.User.ID, m.space.ID, itemID, *in.Off, s.now()); err != nil {
				return err
			}
			chain, err := orgs.Chain(r.Context(), st, m.org, authz.Scope{Kind: store.ScopeTenant, ID: m.space.ID})
			if err != nil {
				return err
			}
			if err := st.AddAudit(r.Context(), orgs.Journal(chain, p.User.ID, "env.person.off", "catalog_item", itemID,
				map[string]any{"off": was}, map[string]any{"off": *in.Off, "spaceId": m.space.ID}, s.now())); err != nil {
				return err
			}
		}
		out = MeItem{ItemID: eff.ID, EntryID: eff.EntryID, Kind: eff.Kind, Name: eff.Name, Hint: eff.Hint, Layer: eff.Layer, Off: *in.Off, CanToggle: true}
		scope := m.org.ID
		switch eff.Layer {
		case store.EnvLayerTenant:
			scope = m.space.ID
		case store.EnvLayerTeam:
			scope = m.entryScope(eff.EntryID)
		}
		out.ScopeID, out.LayerTitle = scope, m.w.layerTitle(eff.Layer, scope)
		return nil
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

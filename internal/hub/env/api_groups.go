package env

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Groups of a layer's entries (docs/hub/environment.md, «Groups»). A required group pins its items; an optional one is
// each person's to switch on or off for themselves, and on the machines that are theirs.

// GroupView is a group as a layer lists it.
type GroupView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Hint      string `json:"hint"`
	Required  bool   `json:"required"`
	DefaultOn bool   `json:"defaultOn"`
	Position  int    `json:"position"`
	// BuiltIn: the platform's own group («Рекомендованное»); it is not changed or removed in the hub.
	BuiltIn bool `json:"builtIn"`
}

func groupView(g store.EnvGroup) GroupView {
	return GroupView{ID: g.ID, Name: g.Name, Hint: g.Hint, Required: g.Required, DefaultOn: g.DefaultOn, Position: g.Position,
		BuiltIn: g.ID == RecommendedGroupID}
}

// groupsOf are the groups of one layer scope.
func (w *world) groupsOf(layer, scopeID string) []GroupView {
	out := []GroupView{}
	for _, g := range w.groups {
		if g.Layer == layer && g.ScopeID == scopeID {
			out = append(out, groupView(g))
		}
	}
	return out
}

func (w *world) group(id string) (store.EnvGroup, bool) {
	for _, g := range w.groups {
		if g.ID == id {
			return g, true
		}
	}
	return store.EnvGroup{}, false
}

// groupIn is the body of POST …/env/groups and PATCH …/env/groups/{id}: what is absent stays (defaults on create:
// optional, on).
type groupIn struct {
	Layer     string  `json:"layer"`
	ScopeID   string  `json:"scopeId"`
	Name      *string `json:"name"`
	Hint      *string `json:"hint"`
	Required  *bool   `json:"required"`
	DefaultOn *bool   `json:"defaultOn"`
	Position  *int    `json:"position"`
}

func (in groupIn) apply(g *store.EnvGroup) error {
	if in.Name != nil {
		n, err := CleanText("name", *in.Name, 40, true)
		if err != nil {
			return err
		}
		g.Name = n
	}
	if in.Hint != nil {
		h, err := CleanText("hint", *in.Hint, 200, false)
		if err != nil {
			return err
		}
		g.Hint = h
	}
	if in.Required != nil {
		g.Required = *in.Required
	}
	if in.DefaultOn != nil {
		g.DefaultOn = *in.DefaultOn
	}
	if in.Position != nil {
		g.Position = *in.Position
	}
	return nil
}

func (s *Service) addGroup(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in groupIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out store.EnvGroup
	var q *req
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureEnv); err != nil {
			return err
		}
		if in.Layer != store.EnvLayerOrg && in.Layer != store.EnvLayerTenant && in.Layer != store.EnvLayerTeam {
			return httpx.Invalid("layer", "org, tenant или team")
		}
		if !q.w.layerExists(in.Layer, in.ScopeID) {
			return httpx.ErrNotFound
		}
		if err := q.rt.require(scopeOf(in.Layer, in.ScopeID)); err != nil {
			return err
		}
		if in.Name == nil {
			return httpx.Invalid("name", "Нужно название группы")
		}
		g := store.EnvGroup{OrgID: q.w.org.ID, Layer: in.Layer, ScopeID: in.ScopeID, DefaultOn: true, Position: len(q.w.groupsOf(in.Layer, in.ScopeID)) + 1,
			UpdatedAt: s.now(), UpdatedBy: p.User.ID}
		if err := in.apply(&g); err != nil {
			return err
		}
		var added bool
		if out, added, err = st.AddEnvGroup(r.Context(), g); err != nil {
			return err
		} else if !added {
			return httpx.Conflict("name_taken", "Группа с таким названием в этом слое уже есть")
		}
		title := fmt.Sprintf("Окружение: группа «%s» добавлена в %s", out.Name, q.w.whereRu(out.Layer, out.ScopeID))
		return s.record(r.Context(), st, q.w, p, out.Layer, out.ScopeID, title, "env.group.add", "env_group", out.ID, nil, groupView(out), s.now())
	})
	if err != nil {
		return err
	}
	s.groupChanged(q.w.org.ID, out)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"group": groupView(out)})
	return nil
}

func (s *Service) patchGroup(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in groupIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out store.EnvGroup
	var q *req
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureEnv); err != nil {
			return err
		}
		g, ok := q.w.group(r.PathValue("groupId"))
		if !ok {
			return httpx.ErrNotFound
		}
		if g.ID == RecommendedGroupID {
			return httpx.Conflict("built_in", "Группу платформы в хабе не меняют: каждый включает её у себя")
		}
		if err := q.rt.require(scopeOf(g.Layer, g.ScopeID)); err != nil {
			return err
		}
		before := groupView(g)
		if err := in.apply(&g); err != nil {
			return err
		}
		if groupView(g) == before {
			out = g
			return nil
		}
		for _, o := range q.w.groupsOf(g.Layer, g.ScopeID) {
			if o.ID != g.ID && strings.EqualFold(o.Name, g.Name) {
				return httpx.Conflict("name_taken", "Группа с таким названием в этом слое уже есть")
			}
		}
		g.UpdatedAt, g.UpdatedBy = s.now(), p.User.ID
		if err := st.UpdateEnvGroup(r.Context(), g); err != nil {
			return err
		}
		out = g
		title := fmt.Sprintf("Окружение: группа «%s» изменена в %s", g.Name, q.w.whereRu(g.Layer, g.ScopeID))
		return s.record(r.Context(), st, q.w, p, g.Layer, g.ScopeID, title, "env.group.update", "env_group", g.ID, before, groupView(g), s.now())
	})
	if err != nil {
		return err
	}
	s.groupChanged(q.w.org.ID, out)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"group": groupView(out)})
	return nil
}

func (s *Service) deleteGroup(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var out store.EnvGroup
	var q *req
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureEnv); err != nil {
			return err
		}
		g, ok := q.w.group(r.PathValue("groupId"))
		if !ok {
			return httpx.ErrNotFound
		}
		if g.ID == RecommendedGroupID {
			return httpx.Conflict("built_in", "Группу платформы в хабе не меняют: каждый включает её у себя")
		}
		if err := q.rt.require(scopeOf(g.Layer, g.ScopeID)); err != nil {
			return err
		}
		if err := st.DeleteEnvGroup(r.Context(), g.ID); err != nil {
			return err
		}
		out = g
		title := fmt.Sprintf("Окружение: группа «%s» удалена из %s, её пункты остались без группы", g.Name, q.w.whereRu(g.Layer, g.ScopeID))
		return s.record(r.Context(), st, q.w, p, g.Layer, g.ScopeID, title, "env.group.delete", "env_group", g.ID, groupView(g), nil, s.now())
	})
	if err != nil {
		return err
	}
	s.groupChanged(q.w.org.ID, out)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// groupChanged tells the pages and the pusher: a group's change changes the lists of the machines it reaches.
func (s *Service) groupChanged(orgID string, g store.EnvGroup) {
	s.publish(orgID, scopeOf(g.Layer, g.ScopeID).Key(), "", 0)
	s.kick()
}

// MeGroup is a group of the person's environment in a tenant.
type MeGroup struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Hint      string `json:"hint"`
	Layer     string `json:"layer"`
	Required  bool   `json:"required"`
	On        bool   `json:"on"`
	CanToggle bool   `json:"canToggle"`
	Items     int    `json:"items"` // how many items of the person's environment are in it
	BuiltIn   bool   `json:"builtIn"`
}

// meGroups are the groups the person's items are in, in the order of the layers and positions.
func (m *me) meGroups(eff []Effective) []MeGroup {
	count := map[string]int{}
	for _, e := range eff {
		if e.GroupID != "" {
			count[e.GroupID]++
		}
	}
	out := []MeGroup{}
	for _, g := range m.w.groups {
		if count[g.ID] == 0 {
			continue
		}
		on, chose := m.person.GroupOn[g.ID]
		if !chose {
			on = g.DefaultOn
		}
		out = append(out, MeGroup{ID: g.ID, Name: g.Name, Hint: g.Hint, Layer: g.Layer, Required: g.Required, On: on || g.Required, BuiltIn: g.ID == RecommendedGroupID,
			CanToggle: !g.Required, Items: count[g.ID]})
	}
	return out
}

// groupOnIn is the body of PUT /api/me/env/groups/{groupId}.
type groupOnIn struct {
	On    *bool  `json:"on"`
	Space string `json:"space"`
}

func (s *Service) putMeGroup(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in groupOnIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.On == nil {
		return httpx.Invalid("on", "Нужно true или false")
	}
	space := r.URL.Query().Get("space")
	if space == "" {
		space = in.Space
	}
	var out MeGroup
	var orgID string
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		m, err := s.openMe(r, st, p, space)
		if err != nil {
			return err
		}
		orgID = m.org.ID
		var found *MeGroup
		for _, g := range m.meGroups(m.effective()) {
			if g.ID == r.PathValue("groupId") {
				found = &g
				break
			}
		}
		if found == nil {
			return httpx.ErrNotFound
		}
		if found.Required {
			return httpx.Conflict("locked", "Обязательная группа: выключить нельзя")
		}
		if found.On != *in.On {
			chain, err := orgs.Chain(r.Context(), st, m.org, authz.Scope{Kind: store.ScopeTenant, ID: m.space.ID})
			if err != nil {
				return err
			}
			if err := s.setPersonGroup(r, st, p, m, chain, *found, *in.On, s.now()); err != nil {
				return err
			}
		}
		out = *found
		out.On = *in.On
		return nil
	})
	if err != nil {
		return err
	}
	if orgID != "" {
		s.kick() // the person's machines get the list without (or with) the group
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// setPersonGroup keeps the person's choice of an optional group in the tenant and records it in the audit log.
func (s *Service) setPersonGroup(r *http.Request, st store.Store, p *auth.Principal, m *me, chain []authz.Scope, g MeGroup, on bool, at time.Time) error {
	if g.On == on {
		return nil
	}
	if err := st.SetEnvPersonGroup(r.Context(), p.User.ID, m.space.ID, g.ID, on, at); err != nil {
		return err
	}
	return st.AddAudit(r.Context(), orgs.Journal(chain, p.User.ID, "env.person.group", "env_group", g.ID,
		map[string]any{"on": g.On}, map[string]any{"on": on, "spaceId": m.space.ID}, at))
}

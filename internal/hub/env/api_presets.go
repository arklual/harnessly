package env

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Presets of a layer (docs/hub/environment.md, «Presets»): a named set of the layer's groups. A person who picks one
// turns its optional groups on and the layer's other optional groups off; required groups stay as they are.

// PresetView is a preset as a layer lists it.
type PresetView struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Hint     string   `json:"hint"`
	GroupIDs []string `json:"groupIds"`
	Position int      `json:"position"`
}

func presetView(p store.EnvPreset) PresetView {
	return PresetView{ID: p.ID, Name: p.Name, Hint: p.Hint, GroupIDs: append([]string{}, p.GroupIDs...), Position: p.Position}
}

// presetsOf are the presets of one layer scope.
func (w *world) presetsOf(layer, scopeID string) []PresetView {
	out := []PresetView{}
	for _, p := range w.presets {
		if p.Layer == layer && p.ScopeID == scopeID {
			out = append(out, presetView(p))
		}
	}
	return out
}

func (w *world) preset(id string) (store.EnvPreset, bool) {
	for _, p := range w.presets {
		if p.ID == id {
			return p, true
		}
	}
	return store.EnvPreset{}, false
}

// presetIn is the body of POST …/env/presets and PATCH …/env/presets/{id}: what is absent stays.
type presetIn struct {
	Layer    string    `json:"layer"`
	ScopeID  string    `json:"scopeId"`
	Name     *string   `json:"name"`
	Hint     *string   `json:"hint"`
	GroupIDs *[]string `json:"groupIds"`
	Position *int      `json:"position"`
}

// apply puts the body on p; every group must be one of p's layer.
func (in presetIn) apply(w *world, p *store.EnvPreset) error {
	if in.Name != nil {
		n, err := CleanText("name", *in.Name, 40, true)
		if err != nil {
			return err
		}
		if utf8.RuneCountInString(n) > 40 {
			return httpx.Invalid("name", "Не длиннее 40 знаков")
		}
		p.Name = n
	}
	if in.Hint != nil {
		h, err := CleanText("hint", *in.Hint, 200, false)
		if err != nil {
			return err
		}
		p.Hint = h
	}
	if in.GroupIDs != nil {
		ids := []string{}
		for _, id := range *in.GroupIDs {
			g, ok := w.group(id)
			if !ok || g.Layer != p.Layer || g.ScopeID != p.ScopeID {
				return httpx.Invalid("groupIds", "Группа должна быть из этого же слоя")
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		p.GroupIDs = ids
	}
	if in.Position != nil {
		p.Position = *in.Position
	}
	return nil
}

func (s *Service) addPreset(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in presetIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out store.EnvPreset
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
			return httpx.Invalid("name", "Нужно название пресета")
		}
		now := s.now()
		pr := store.EnvPreset{OrgID: q.w.org.ID, Layer: in.Layer, ScopeID: in.ScopeID, Position: len(q.w.presetsOf(in.Layer, in.ScopeID)) + 1,
			CreatedAt: now, UpdatedAt: now, UpdatedBy: p.User.ID}
		if err := in.apply(q.w, &pr); err != nil {
			return err
		}
		for _, o := range q.w.presetsOf(in.Layer, in.ScopeID) {
			if strings.EqualFold(o.Name, pr.Name) {
				return httpx.Conflict("name_taken", "Пресет с таким названием в этом слое уже есть")
			}
		}
		var added bool
		if out, added, err = st.AddEnvPreset(r.Context(), pr); err != nil {
			return err
		} else if !added {
			return httpx.Conflict("name_taken", "Пресет с таким названием в этом слое уже есть")
		}
		title := fmt.Sprintf("Окружение: пресет «%s» добавлен в %s", out.Name, q.w.whereRu(out.Layer, out.ScopeID))
		return s.record(r.Context(), st, q.w, p, out.Layer, out.ScopeID, title, "env.preset.add", "env_preset", out.ID, nil, presetView(out), now)
	})
	if err != nil {
		return err
	}
	s.publish(q.w.org.ID, scopeOf(out.Layer, out.ScopeID).Key(), "", 0)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"preset": presetView(out)})
	return nil
}

func (s *Service) patchPreset(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in presetIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out store.EnvPreset
	var q *req
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureEnv); err != nil {
			return err
		}
		pr, ok := q.w.preset(r.PathValue("presetId"))
		if !ok {
			return httpx.ErrNotFound
		}
		if err := q.rt.require(scopeOf(pr.Layer, pr.ScopeID)); err != nil {
			return err
		}
		before := presetView(pr)
		if err := in.apply(q.w, &pr); err != nil {
			return err
		}
		out = pr
		if v := presetView(pr); v.Name == before.Name && v.Hint == before.Hint && v.Position == before.Position && slices.Equal(v.GroupIDs, before.GroupIDs) {
			return nil
		}
		for _, o := range q.w.presetsOf(pr.Layer, pr.ScopeID) {
			if o.ID != pr.ID && strings.EqualFold(o.Name, pr.Name) {
				return httpx.Conflict("name_taken", "Пресет с таким названием в этом слое уже есть")
			}
		}
		pr.UpdatedAt, pr.UpdatedBy = s.now(), p.User.ID
		if err := st.UpdateEnvPreset(r.Context(), pr); err != nil {
			return err
		}
		out = pr
		title := fmt.Sprintf("Окружение: пресет «%s» изменён в %s", pr.Name, q.w.whereRu(pr.Layer, pr.ScopeID))
		return s.record(r.Context(), st, q.w, p, pr.Layer, pr.ScopeID, title, "env.preset.update", "env_preset", pr.ID, before, presetView(pr), pr.UpdatedAt)
	})
	if err != nil {
		return err
	}
	s.publish(q.w.org.ID, scopeOf(out.Layer, out.ScopeID).Key(), "", 0)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"preset": presetView(out)})
	return nil
}

func (s *Service) deletePreset(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var out store.EnvPreset
	var q *req
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureEnv); err != nil {
			return err
		}
		pr, ok := q.w.preset(r.PathValue("presetId"))
		if !ok {
			return httpx.ErrNotFound
		}
		if err := q.rt.require(scopeOf(pr.Layer, pr.ScopeID)); err != nil {
			return err
		}
		if err := st.DeleteEnvPreset(r.Context(), pr.ID); err != nil {
			return err
		}
		out = pr
		title := fmt.Sprintf("Окружение: пресет «%s» удалён из %s", pr.Name, q.w.whereRu(pr.Layer, pr.ScopeID))
		return s.record(r.Context(), st, q.w, p, pr.Layer, pr.ScopeID, title, "env.preset.delete", "env_preset", pr.ID, presetView(pr), nil, s.now())
	})
	if err != nil {
		return err
	}
	s.publish(q.w.org.ID, scopeOf(out.Layer, out.ScopeID).Key(), "", 0)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// MePreset is a preset of the person's environment in a tenant.
type MePreset struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Hint     string   `json:"hint"`
	Layer    string   `json:"layer"`
	GroupIDs []string `json:"groupIds"`
	// Active: the person's choice of the layer's optional groups is the preset's.
	Active bool `json:"active"`
}

// presetGroups are the optional groups of the preset's layer the person has items in (from meGroups).
func presetGroups(pr store.EnvPreset, w *world, groups []MeGroup) []MeGroup {
	var out []MeGroup
	for _, g := range groups {
		if sg, ok := w.group(g.ID); ok && !g.Required && g.Layer == pr.Layer && sg.ScopeID == pr.ScopeID {
			out = append(out, g)
		}
	}
	return out
}

// mePresets are the presets with at least one group the person has items in, in the order of the layers and positions.
func (m *me) mePresets(groups []MeGroup) []MePreset {
	have := map[string]bool{}
	for _, g := range groups {
		have[g.ID] = true
	}
	out := []MePreset{}
	for _, pr := range m.w.presets {
		if !slices.ContainsFunc(pr.GroupIDs, func(id string) bool { return have[id] }) {
			continue
		}
		active := true
		for _, g := range presetGroups(pr, m.w, groups) {
			if g.On != slices.Contains(pr.GroupIDs, g.ID) {
				active = false
				break
			}
		}
		out = append(out, MePreset{ID: pr.ID, Name: pr.Name, Hint: pr.Hint, Layer: pr.Layer, GroupIDs: append([]string{}, pr.GroupIDs...), Active: active})
	}
	return out
}

// putMePreset is PUT /api/me/env/presets/{presetId}?space=: the person's choices of the layer's optional groups become
// the preset's. The answer is the refreshed presets and groups of the person.
func (s *Service) putMePreset(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in struct {
		Space string `json:"space"`
	}
	if r.ContentLength != 0 {
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
	}
	space := r.URL.Query().Get("space")
	if space == "" {
		space = in.Space
	}
	var presets []MePreset
	var groups []MeGroup
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		m, err := s.openMe(r, st, p, space)
		if err != nil {
			return err
		}
		groups = m.meGroups(m.effective())
		pr, ok := m.w.preset(r.PathValue("presetId"))
		if !ok || !slices.ContainsFunc(pr.GroupIDs, func(id string) bool { return slices.ContainsFunc(groups, func(g MeGroup) bool { return g.ID == id }) }) {
			return httpx.ErrNotFound
		}
		chain, err := orgs.Chain(r.Context(), st, m.org, authz.Scope{Kind: store.ScopeTenant, ID: m.space.ID})
		if err != nil {
			return err
		}
		now := s.now()
		for _, g := range presetGroups(pr, m.w, groups) {
			if err := s.setPersonGroup(r, st, p, m, chain, g, slices.Contains(pr.GroupIDs, g.ID), now); err != nil {
				return err
			}
		}
		// Read the person's choices again for the answer.
		if m.person.GroupOn, err = st.EnvPersonGroups(r.Context(), p.User.ID, m.space.ID); err != nil {
			return err
		}
		groups = m.meGroups(m.effective())
		presets = m.mePresets(groups)
		return nil
	})
	if err != nil {
		return err
	}
	s.kick() // the person's machines get the list of the new choice
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"presets": presets, "groups": groups})
	return nil
}

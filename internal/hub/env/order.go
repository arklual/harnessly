package env

import (
	"fmt"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// nextPosition is the place a new script entry takes at the end of its layer.
func (w *world) nextPosition(layer, scopeID string) int {
	max := 0
	for _, e := range w.entries {
		if e.Layer == layer && e.ScopeID == scopeID && w.items[e.ItemID].Kind == store.CatalogScript && e.Position > max {
			max = e.Position
		}
	}
	return max + 1
}

// scriptEntries are the entries of a layer whose item is a live script.
func (w *world) scriptEntries(layer, scopeID string) []store.EnvEntry {
	var out []store.EnvEntry
	for _, e := range w.entries {
		it := w.items[e.ItemID]
		if e.Layer == layer && e.ScopeID == scopeID && it.Kind == store.CatalogScript && it.ArchivedAt == nil {
			out = append(out, e)
		}
	}
	return out
}

// orderIn is the body of PUT …/env/order.
type orderIn struct {
	Layer    string   `json:"layer"`
	ScopeID  string   `json:"scopeId"`
	EntryIDs []string `json:"entryIds"`
}

// putOrder sets the order of the scripts of a layer: all of the layer's script entries, each once. The order is part
// of the list a machine holds, so a changed order is a changed list.
func (s *Service) putOrder(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in orderIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var q *req
	var changed bool
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
		have := q.w.scriptEntries(in.Layer, in.ScopeID)
		if len(in.EntryIDs) != len(have) {
			return httpx.Invalid("entryIds", "Нужны все скрипты слоя, каждый один раз")
		}
		seen := map[string]bool{}
		byID := map[string]store.EnvEntry{}
		for _, e := range have {
			byID[e.ID] = e
		}
		for _, id := range in.EntryIDs {
			if _, ok := byID[id]; !ok || seen[id] {
				return httpx.Invalid("entryIds", "Нужны все скрипты слоя, каждый один раз")
			}
			seen[id] = true
		}
		now := s.now()
		for i, id := range in.EntryIDs {
			e := byID[id]
			if e.Position == i+1 {
				continue
			}
			e.Position, e.Rev, e.UpdatedAt, e.UpdatedBy = i+1, e.Rev+1, now, p.User.ID
			if err := st.UpdateEnvEntry(r.Context(), e); err != nil {
				return err
			}
			changed = true
		}
		if !changed {
			return nil
		}
		title := fmt.Sprintf("Окружение: изменён порядок скриптов в %s", q.w.whereRu(in.Layer, in.ScopeID))
		return s.record(r.Context(), st, q.w, p, in.Layer, in.ScopeID, title, "env.entry.order", "env_layer", in.ScopeID,
			nil, map[string]any{"layer": in.Layer, "entryIds": in.EntryIDs}, now)
	})
	if err != nil {
		return err
	}
	if changed {
		s.publish(q.w.org.ID, scopeOf(in.Layer, in.ScopeID).Key(), "", 0)
		s.kick()
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"entryIds": append([]string{}, in.EntryIDs...)})
	return nil
}

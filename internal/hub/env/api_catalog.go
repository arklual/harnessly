package env

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// CatalogItemView is an item of the catalog. Added says the layer the request names has the item.
type CatalogItemView struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Name      string          `json:"name"`
	Hint      string          `json:"hint"`
	Source    json.RawMessage `json:"source"`
	BuiltIn   bool            `json:"builtIn"`
	Archived  bool            `json:"archived"`
	Added     bool            `json:"added"`
	CreatedAt string          `json:"createdAt"`
	// Owner is the layer that owns the item; null: the organization.
	Owner *OwnerRef `json:"owner"`
}

// OwnerRef names the layer that owns a catalog item and may change it.
type OwnerRef struct {
	Layer   string `json:"layer"`
	ScopeID string `json:"scopeId"`
}

// CatalogResponse is GET /api/orgs/{org}/catalog.
type CatalogResponse struct {
	Items []CatalogItemView `json:"items"`
}

func catalogView(c store.CatalogItem, added bool) CatalogItemView {
	src := json.RawMessage(c.Source)
	if len(src) == 0 {
		src = json.RawMessage("{}")
	}
	v := CatalogItemView{ID: c.ID, Kind: c.Kind, Name: c.Name, Hint: c.Hint, Source: src, BuiltIn: c.OrgID == "", Archived: c.ArchivedAt != nil,
		Added: added, CreatedAt: httpx.Time(c.CreatedAt)}
	if c.OwnerLayer != "" {
		v.Owner = &OwnerRef{Layer: c.OwnerLayer, ScopeID: c.OwnerScope}
	}
	return v
}

// canBrowse: the person manages something in the organization, so the catalog is theirs to add from.
func (q *req) canBrowse() error {
	orgScope := authz.Scope{Kind: store.ScopeOrg, ID: q.w.org.ID}
	ok, err := q.rt.manages(orgScope)
	if err != nil || ok {
		return err
	}
	vis, err := q.rt.visibleTenants(q.w)
	if err != nil {
		return err
	}
	if len(vis) == 0 {
		return q.rt.require(orgScope)
	}
	return nil
}

func (s *Service) listCatalog(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	q, err := s.open(r, s.db.S(), p, FeatureCatalog)
	if err != nil {
		return err
	}
	if err := q.canBrowse(); err != nil {
		return err
	}
	qs := r.URL.Query()
	kind := qs.Get("kind")
	if kind != "" && !ValidKind(kind) {
		return httpx.Invalid("kind", kindsRu)
	}
	added := map[string]bool{}
	if layer, scopeID := qs.Get("layer"), qs.Get("scopeId"); layer != "" || scopeID != "" {
		if !q.w.layerExists(layer, scopeID) {
			return httpx.ErrNotFound
		}
		for _, e := range q.w.entries {
			if e.Layer == layer && e.ScopeID == scopeID {
				added[e.ItemID] = true
			}
		}
	}
	out := CatalogResponse{Items: []CatalogItemView{}}
	items, err := q.st.CatalogItems(q.ctx, q.w.org.ID, false)
	if err != nil {
		return err
	}
	for _, c := range items {
		if kind == "" || c.Kind == kind {
			out.Items = append(out.Items, catalogView(c, added[c.ID]))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// catalogIn is the body of POST and PATCH …/catalog.
type catalogIn struct {
	Kind     string          `json:"kind"`
	Name     *string         `json:"name"`
	Hint     *string         `json:"hint"`
	Source   json.RawMessage `json:"source"`
	Archived *bool           `json:"archived"`
	// Owner (POST only) is the layer the item is made for; absent: the organization.
	Owner *OwnerRef `json:"owner"`
}

func (q *req) requireOrgAdmin() error {
	return q.rt.require(authz.Scope{Kind: store.ScopeOrg, ID: q.w.org.ID})
}

// consentMsg: a secret of someone's safe is read by a script, and a forge's login uses a token of the safe; whoever
// writes such an item can read every member's token, so only the organization's admins do.
const consentMsg = "Токены из сейфа и доступы подключает админ организации"

// claudeMsg: a plugin brings hooks that run with every tool call, settings change every chat of the person.
const claudeMsg = "Плагины и настройки Claude подключает админ организации"

// appMsg: an app works in every chat of the people it reaches, with the rights it asks.
const appMsg = "Приложения в окружение ставит админ организации"

// CheckConsent refuses the items only an organization admin may write: an access, and a script that takes secrets from
// the safe. It is asked for every item a team or a tenant owns.
func CheckConsent(kind string, src json.RawMessage) error {
	switch kind {
	case store.CatalogAccess:
		return httpx.Invalid("kind", consentMsg)
	case store.CatalogPlugin, store.CatalogClaudeSettings:
		return httpx.Invalid("kind", claudeMsg)
	case store.CatalogApp:
		return httpx.Invalid("kind", appMsg)
	case store.CatalogScript:
		var s struct {
			Secrets []string `json:"secrets"`
		}
		if json.Unmarshal(src, &s) == nil && len(s.Secrets) > 0 {
			return httpx.Invalid("source.secrets", consentMsg)
		}
	}
	return nil
}

// requireItemRights: an organization admin changes any item of the organization, the managers of the owner layer their own.
func (q *req) requireItemRights(c store.CatalogItem) error {
	orgScope := authz.Scope{Kind: store.ScopeOrg, ID: q.w.org.ID}
	if c.OwnerLayer == "" {
		return q.rt.require(orgScope)
	}
	ok, err := q.rt.manages(orgScope)
	if err != nil || ok {
		return err
	}
	return q.rt.require(scopeOf(c.OwnerLayer, c.OwnerScope))
}

// checkOwner checks the owner a new item is made for and that the person manages it. nil owner: the organization.
func (q *req) checkOwner(o *OwnerRef) (layer, scope string, err error) {
	if o == nil || (o.Layer == store.EnvLayerOrg && (o.ScopeID == q.w.org.ID || o.ScopeID == "")) {
		return "", "", q.requireOrgAdmin()
	}
	if o.Layer != store.EnvLayerTenant && o.Layer != store.EnvLayerTeam {
		return "", "", httpx.Invalid("owner.layer", "org, tenant или team")
	}
	if !q.w.layerExists(o.Layer, o.ScopeID) {
		return "", "", httpx.Invalid("owner.scopeId", "Такого слоя нет")
	}
	if err := q.rt.require(scopeOf(o.Layer, o.ScopeID)); err != nil {
		return "", "", err
	}
	return o.Layer, o.ScopeID, nil
}

// checkSource refuses a script that takes its file from a repository item the catalog does not have.
func (q *req) checkSource(kind string, src json.RawMessage) error {
	if kind != store.CatalogScript {
		return nil
	}
	var s struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal(src, &s); err != nil || s.Repo == "" {
		return nil
	}
	if r, ok := q.w.items[s.Repo]; !ok || r.Kind != store.CatalogRepo || r.ArchivedAt != nil {
		return httpx.Invalid("source.repo", "Такого репозитория нет в каталоге")
	}
	return nil
}

// nameTaken: another live item of the organization (or a built-in one) has the kind and the name.
func (q *req) nameTaken(kind, name, except string) bool {
	for _, c := range q.w.items {
		if c.ID != except && c.ArchivedAt == nil && c.Kind == kind && strings.EqualFold(c.Name, name) {
			return true
		}
	}
	return false
}

// catalogChanged writes the change on the item's owner layer (the organization's by default).
func (s *Service) catalogChanged(q *req, st store.Store, p *auth.Principal, c store.CatalogItem, title, action string, before, after any) error {
	layer, scope := store.EnvLayerOrg, q.w.org.ID
	if c.OwnerLayer != "" {
		layer, scope = c.OwnerLayer, c.OwnerScope
	}
	return s.record(q.ctx, st, q.w, p, layer, scope, title, action, "catalog_item", c.ID, before, after, s.now())
}

func catalogJournal(c store.CatalogItem) map[string]any {
	return map[string]any{"kind": c.Kind, "name": c.Name, "hint": c.Hint, "archived": c.ArchivedAt != nil}
}

func (s *Service) addCatalog(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in catalogIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out store.CatalogItem
	var q *req
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureCatalog); err != nil {
			return err
		}
		ownerLayer, ownerScope, err := q.checkOwner(in.Owner)
		if err != nil {
			return err
		}
		if !ValidKind(in.Kind) {
			return httpx.Invalid("kind", kindsRu)
		}
		name, err := CleanText("name", deref(in.Name), maxNameRunes, true)
		if err != nil {
			return err
		}
		hint, err := CleanText("hint", deref(in.Hint), maxHintRunes, false)
		if err != nil {
			return err
		}
		src, err := CleanSource(in.Kind, in.Source)
		if err != nil {
			return err
		}
		if err := q.checkSource(in.Kind, src); err != nil {
			return err
		}
		if ownerLayer != "" {
			if err := CheckConsent(in.Kind, src); err != nil {
				return err
			}
		}
		if q.nameTaken(in.Kind, name, "") {
			return httpx.Conflict("name_taken", "Такое название уже есть в каталоге")
		}
		c := store.CatalogItem{OrgID: q.w.org.ID, Kind: in.Kind, Name: name, Hint: hint, Source: string(src), CreatedAt: s.now(), CreatedBy: p.User.ID,
			OwnerLayer: ownerLayer, OwnerScope: ownerScope}
		if out, err = st.AddCatalogItem(r.Context(), c); err != nil {
			return err
		}
		return s.catalogChanged(q, st, p, out, fmt.Sprintf("Каталог: добавлен %s «%s»", kindRu[out.Kind], out.Name), "catalog.add", nil, catalogJournal(out))
	})
	if err != nil {
		return err
	}
	s.publish(q.w.org.ID, scopeOf(store.EnvLayerOrg, q.w.org.ID).Key(), "", 0)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"item": catalogView(out, false)})
	return nil
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// itemOf finds an item of the organization's own; a built-in one is not for the organization to change.
func (q *req) ownItem(id string) (store.CatalogItem, error) {
	c, ok := q.w.items[id]
	if !ok || (c.OrgID != "" && c.OrgID != q.w.org.ID) {
		return c, httpx.ErrNotFound
	}
	if c.OrgID == "" {
		return c, httpx.Conflict("built_in", "Встроенный элемент каталога не меняют")
	}
	return c, nil
}

func (s *Service) patchCatalog(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in catalogIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out store.CatalogItem
	var q *req
	var touched bool
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureCatalog); err != nil {
			return err
		}
		c, err := q.ownItem(r.PathValue("itemId"))
		if err != nil {
			return err
		}
		if err := q.requireItemRights(c); err != nil {
			return err
		}
		before := catalogJournal(c)
		var verbs []string
		if in.Name != nil {
			name, err := CleanText("name", *in.Name, maxNameRunes, true)
			if err != nil {
				return err
			}
			if name != c.Name {
				if q.nameTaken(c.Kind, name, c.ID) {
					return httpx.Conflict("name_taken", "Такое название уже есть в каталоге")
				}
				c.Name = name
				verbs = append(verbs, "переименован")
			}
		}
		if in.Hint != nil {
			hint, err := CleanText("hint", *in.Hint, maxHintRunes, false)
			if err != nil {
				return err
			}
			if hint != c.Hint {
				c.Hint = hint
				verbs = append(verbs, "изменено описание")
			}
		}
		if in.Source != nil {
			src, err := CleanSource(c.Kind, in.Source)
			if err != nil {
				return err
			}
			if err := q.checkSource(c.Kind, src); err != nil {
				return err
			}
			if c.OwnerLayer != "" {
				if err := CheckConsent(c.Kind, src); err != nil {
					return err
				}
			}
			if string(src) != string(CanonicalSource(c.Source)) {
				c.Source = string(src)
				verbs = append(verbs, "изменён источник")
			}
		}
		if in.Archived != nil && *in.Archived != (c.ArchivedAt != nil) {
			if *in.Archived {
				at := s.now()
				c.ArchivedAt = &at
				verbs = append(verbs, "убран из каталога")
			} else {
				if q.nameTaken(c.Kind, c.Name, c.ID) {
					return httpx.Conflict("name_taken", "Такое название уже есть в каталоге")
				}
				c.ArchivedAt = nil
				verbs = append(verbs, "возвращён в каталог")
			}
		}
		out = c
		if len(verbs) == 0 {
			return nil
		}
		touched = true
		if err := st.UpdateCatalogItem(r.Context(), c); err != nil {
			return err
		}
		return s.catalogChanged(q, st, p, c, fmt.Sprintf("Каталог: %s «%s» %s", kindRu[c.Kind], c.Name, strings.Join(verbs, ", ")), "catalog.update",
			before, catalogJournal(c))
	})
	if err != nil {
		return err
	}
	if touched {
		s.publish(q.w.org.ID, scopeOf(store.EnvLayerOrg, q.w.org.ID).Key(), "", 0)
		s.kick()
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"item": catalogView(out, false)})
	return nil
}

// CanonicalSource is a kept source in the form CleanSource gives.
func CanonicalSource(raw string) []byte {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return []byte(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(raw)
	}
	return b
}

// deleteCatalog archives the item: the entries that used it stop putting it in, and nothing is lost.
func (s *Service) deleteCatalog(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var q *req
	var touched bool
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureCatalog); err != nil {
			return err
		}
		c, err := q.ownItem(r.PathValue("itemId"))
		if err != nil {
			return err
		}
		if err := q.requireItemRights(c); err != nil {
			return err
		}
		if c.ArchivedAt != nil {
			return nil
		}
		touched = true
		before := catalogJournal(c)
		at := s.now()
		c.ArchivedAt = &at
		if err := st.UpdateCatalogItem(r.Context(), c); err != nil {
			return err
		}
		return s.catalogChanged(q, st, p, c, fmt.Sprintf("Каталог: %s «%s» убран из каталога", kindRu[c.Kind], c.Name), "catalog.archive",
			before, catalogJournal(c))
	})
	if err != nil {
		return err
	}
	if touched {
		s.publish(q.w.org.ID, scopeOf(store.EnvLayerOrg, q.w.org.ID).Key(), "", 0)
		s.kick()
	}
	httpx.NoContent(w)
	return nil
}

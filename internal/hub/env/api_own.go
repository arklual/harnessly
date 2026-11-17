package env

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A person's own layer (docs/hub/environment.md «Own layer»): the skills and MCP servers a person adds for
// themselves on top of what the organization gives. An own item is a catalog row the person owns (owner_layer "user",
// owner_scope their user id) with one entry of the layer "user"; the organization's screens and catalog do not list it,
// and it reaches only the machines that are the person's own (store.Machine.CreatedBy). What the organization gives is
// never replaced: an own item of the same kind and name is refused, and one added before the organization's is left out
// of the lists (withoutShadowed).

// ownKinds are what a person may add for themselves.
var ownKinds = map[string]bool{store.CatalogSkill: true, store.CatalogMCP: true}

// maxOwnItems bounds a person's own items in one organization.
const maxOwnItems = 30

// OwnIn is the body of POST and PATCH /api/me/env/own (and of the machine's env.own).
type OwnIn struct {
	Space  string          `json:"space"`
	Kind   string          `json:"kind"`
	Name   *string         `json:"name"`
	Hint   *string         `json:"hint"`
	Source json.RawMessage `json:"source"`
}

// own is a change of a person's own layer in an organization.
type own struct {
	ctx    context.Context
	st     store.Store
	w      *world
	userID string
	space  string // the tenant the change is made from: its chain gets the audit row
}

// ownItems are the person's own live items of the organization.
func (o *own) items() []store.CatalogItem {
	var out []store.CatalogItem
	for _, c := range o.w.items {
		if c.OwnerLayer == store.EnvLayerUser && c.OwnerScope == o.userID && c.ArchivedAt == nil {
			out = append(out, c)
		}
	}
	return out
}

// item finds one of the person's own live items; anything else is not found.
func (o *own) item(id string) (store.CatalogItem, error) {
	c, ok := o.w.items[id]
	if !ok || c.OwnerLayer != store.EnvLayerUser || c.OwnerScope != o.userID || c.ArchivedAt != nil {
		return c, httpx.ErrNotFound
	}
	return c, nil
}

// clash says why a name cannot be the person's: they have an own item of it, or the organization's catalog (or the
// platform) has one of the same kind and name.
func (o *own) clash(kind, name, except string) error {
	key := OwnKey(kind, name)
	for _, c := range o.w.items {
		if c.ID == except || c.ArchivedAt != nil || OwnKey(c.Kind, c.Name) != key {
			continue
		}
		switch {
		case c.OwnerLayer == store.EnvLayerUser && c.OwnerScope == o.userID:
			return httpx.Conflict("name_taken", "У вас уже есть своё с таким названием")
		case c.OwnerLayer != store.EnvLayerUser:
			return httpx.Conflict("given", "Такое уже есть в окружении организации: своё его не заменяет")
		}
	}
	return nil
}

// journal writes the change to the audit trail of the tenant's chain (the person's own: nobody else is told).
func (s *Service) ownJournal(o *own, action, itemID string, before, after any) error {
	chain, err := orgs.Chain(o.ctx, o.st, o.w.org, authz.Scope{Kind: store.ScopeTenant, ID: o.space})
	if err != nil {
		return err
	}
	return o.st.AddAudit(o.ctx, orgs.Journal(chain, o.userID, action, "catalog_item", itemID, before, after, s.now()))
}

func ownView(c store.CatalogItem) map[string]any {
	return map[string]any{"kind": c.Kind, "name": c.Name, "hint": c.Hint}
}

// addOwn adds an own item and its entry.
func (s *Service) addOwn(o *own, in OwnIn) (store.CatalogItem, error) {
	if !ownKinds[in.Kind] {
		return store.CatalogItem{}, httpx.Invalid("kind", "Своё — это skill или mcp")
	}
	name, err := CleanText("name", deref(in.Name), maxNameRunes, true)
	if err != nil {
		return store.CatalogItem{}, err
	}
	hint, err := CleanText("hint", deref(in.Hint), maxHintRunes, false)
	if err != nil {
		return store.CatalogItem{}, err
	}
	src, err := CleanSource(in.Kind, in.Source)
	if err != nil {
		return store.CatalogItem{}, err
	}
	if err := o.clash(in.Kind, name, ""); err != nil {
		return store.CatalogItem{}, err
	}
	if len(o.items()) >= maxOwnItems {
		return store.CatalogItem{}, httpx.New(http.StatusTooManyRequests, "too_many", fmt.Sprintf("Своего может быть не больше %d", maxOwnItems))
	}
	now := s.now()
	c, err := o.st.AddCatalogItem(o.ctx, store.CatalogItem{OrgID: o.w.org.ID, Kind: in.Kind, Name: name, Hint: hint, Source: string(src),
		CreatedAt: now, CreatedBy: o.userID, OwnerLayer: store.EnvLayerUser, OwnerScope: o.userID})
	if err != nil {
		return c, err
	}
	if _, _, err := o.st.AddEnvEntry(o.ctx, store.EnvEntry{OrgID: o.w.org.ID, Layer: store.EnvLayerUser, ScopeID: o.userID, ItemID: c.ID,
		Enabled: true, UpdatedAt: now, UpdatedBy: o.userID}); err != nil {
		return c, err
	}
	o.w.items[c.ID] = c
	return c, s.ownJournal(o, "env.own.add", c.ID, nil, ownView(c))
}

// patchOwn changes the name, the hint or the source of an own item; the kind stays.
func (s *Service) patchOwn(o *own, id string, in OwnIn) (store.CatalogItem, error) {
	c, err := o.item(id)
	if err != nil {
		return c, err
	}
	if in.Kind != "" && in.Kind != c.Kind {
		return c, httpx.Invalid("kind", "Вид своего не меняется: удалите и добавьте заново")
	}
	before := ownView(c)
	if in.Name != nil {
		if c.Name, err = CleanText("name", *in.Name, maxNameRunes, true); err != nil {
			return c, err
		}
		if err := o.clash(c.Kind, c.Name, c.ID); err != nil {
			return c, err
		}
	}
	if in.Hint != nil {
		if c.Hint, err = CleanText("hint", *in.Hint, maxHintRunes, false); err != nil {
			return c, err
		}
	}
	if len(in.Source) > 0 {
		src, err := CleanSource(c.Kind, in.Source)
		if err != nil {
			return c, err
		}
		c.Source = string(src)
	}
	if err := o.st.UpdateCatalogItem(o.ctx, c); err != nil {
		return c, err
	}
	o.w.items[c.ID] = c
	return c, s.ownJournal(o, "env.own.change", c.ID, before, ownView(c))
}

// removeOwn takes an own item out: its entry goes, the row is archived (its past calls stay).
func (s *Service) removeOwn(o *own, id string) error {
	c, err := o.item(id)
	if err != nil {
		return err
	}
	e, err := o.st.EnvEntryByKey(o.ctx, store.EnvLayerUser, o.userID, c.ID)
	switch {
	case err == nil:
		if err := o.st.DeleteEnvEntry(o.ctx, e.ID); err != nil {
			return err
		}
	case !store.IsNotFound(err):
		return err
	}
	now := s.now()
	c.ArchivedAt = &now
	if err := o.st.UpdateCatalogItem(o.ctx, c); err != nil {
		return err
	}
	return s.ownJournal(o, "env.own.remove", c.ID, ownView(c), nil)
}

// openOwn is the person's own layer for a request of their own session: the tenant as /api/me/env finds it, and the
// layer switched on for its organization.
func (s *Service) openOwn(r *http.Request, st store.Store, p *auth.Principal, space string) (*own, *me, error) {
	return s.openOwnFor(r.Context(), st, p.User.ID, space)
}

func (s *Service) openOwnFor(ctx context.Context, st store.Store, userID, space string) (*own, *me, error) {
	m, err := s.openMeFor(ctx, st, userID, space)
	if err != nil {
		return nil, nil, err
	}
	if !m.w.own {
		return nil, nil, ErrNotEnabled
	}
	return &own{ctx: ctx, st: st, w: m.w, userID: userID, space: m.space.ID}, m, nil
}

// Own makes a change of a person's own layer that their machine's agent sends (hubproto.EventEnvOwn): for the person
// the machine is theirs, in the machine's tenant. A machine without an owner changes nothing.
func (s *Service) Own(ctx context.Context, m store.Machine, raw json.RawMessage) hubproto.EnvOwnResult {
	var in hubproto.EnvOwnRequest
	if err := json.Unmarshal(raw, &in); err != nil {
		return hubproto.EnvOwnResult{Status: http.StatusBadRequest, Code: "invalid_request", Message: "Не удалось разобрать запрос"}
	}
	res := hubproto.EnvOwnResult{RequestID: in.RequestID, Status: http.StatusOK}
	if m.CreatedBy == "" {
		res.Status, res.Code, res.Message = http.StatusForbidden, "no_owner", "У машины нет владельца: своего у неё нет"
		return res
	}
	err := s.db.Tx(ctx, func(st store.Store) error {
		o, me, err := s.openOwnFor(ctx, st, m.CreatedBy, m.SpaceID)
		if err != nil {
			return err
		}
		switch in.Action {
		case hubproto.EnvOwnAdd:
			c, err := s.addOwn(o, OwnIn{Kind: in.Kind, Name: in.Name, Hint: in.Hint, Source: in.Source})
			res.Status, res.ItemID = http.StatusCreated, c.ID
			return err
		case hubproto.EnvOwnUpdate:
			c, err := s.patchOwn(o, in.ItemID, OwnIn{Kind: in.Kind, Name: in.Name, Hint: in.Hint, Source: in.Source})
			res.ItemID = c.ID
			return err
		case hubproto.EnvOwnRemove:
			res.ItemID = in.ItemID
			return s.removeOwn(o, in.ItemID)
		case hubproto.EnvOwnSwitch:
			res.ItemID = in.ItemID
			_, err := s.switchMe(me, m.CreatedBy, in.ItemID, in.Off)
			return err
		}
		return httpx.Invalid("action", "Нужно add, update, remove или switch")
	})
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) {
			res.Status, res.Code, res.Message = he.Status, he.Code, he.Message
		} else {
			res.Status, res.Code, res.Message = http.StatusInternalServerError, "internal", "Не получилось; попробуйте ещё раз"
		}
		return res
	}
	s.kick()
	return res
}

// meOwnItem is an own item as /api/me/env lists it.
func meOwnItem(m *me, c store.CatalogItem) MeItem {
	it := MeItem{ItemID: c.ID, Kind: c.Kind, Name: c.Name, Hint: c.Hint, Layer: store.EnvLayerUser, ScopeID: c.OwnerScope,
		LayerTitle: "Своё", CanToggle: true, Own: true, Off: m.person.Off[c.ID], Source: json.RawMessage(c.Source)}
	for _, e := range m.w.entries {
		if e.Layer == store.EnvLayerUser && e.ItemID == c.ID {
			it.EntryID = e.ID
		}
	}
	return it
}

func (s *Service) postMeOwn(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in OwnIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out MeItem
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		o, m, err := s.openOwn(r, st, p, spaceOf(r, in.Space))
		if err != nil {
			return err
		}
		c, err := s.addOwn(o, in)
		if err != nil {
			return err
		}
		out = meOwnItem(m, c)
		return nil
	})
	if err != nil {
		return err
	}
	s.kick()
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) patchMeOwn(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in OwnIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out MeItem
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		o, m, err := s.openOwn(r, st, p, spaceOf(r, in.Space))
		if err != nil {
			return err
		}
		c, err := s.patchOwn(o, r.PathValue("itemId"), in)
		if err != nil {
			return err
		}
		out = meOwnItem(m, c)
		return nil
	})
	if err != nil {
		return err
	}
	s.kick()
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (s *Service) deleteMeOwn(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		o, _, err := s.openOwn(r, st, p, r.URL.Query().Get("space"))
		if err != nil {
			return err
		}
		return s.removeOwn(o, r.PathValue("itemId"))
	})
	if err != nil {
		return err
	}
	s.kick()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// spaceOf is the tenant a request names: ?space= first, then the body's.
func spaceOf(r *http.Request, body string) string {
	if q := r.URL.Query().Get("space"); q != "" {
		return q
	}
	return body
}

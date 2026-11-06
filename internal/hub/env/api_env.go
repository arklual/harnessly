package env

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// req is one request in an organization: who asks, what the organization holds and what the person may.
type req struct {
	ctx context.Context
	st  store.Store
	p   *auth.Principal
	w   *world
	rt  *rights
	s   *Service
}

// open resolves the organization of the path, checks the feature is on and reads the organization.
func (s *Service) open(r *http.Request, st store.Store, p *auth.Principal, feature string) (*req, error) {
	ctx := r.Context()
	o, err := orgs.Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return nil, err
	}
	if err := s.feature(ctx, o.ID, feature); err != nil {
		return nil, err
	}
	w, err := loadWorld(ctx, st, o, s.recommended)
	if err != nil {
		return nil, err
	}
	return &req{ctx: ctx, st: st, p: p, w: w, rt: newRights(ctx, st, p), s: s}, nil
}

// canEdit says whether the person may change the entries of a layer.
func (q *req) canEdit(layer, scopeID string) (bool, error) {
	return q.rt.manages(scopeOf(layer, scopeID))
}

// selection is the tenants a screen is about: ?scope= a tenant (slug or id), or all of those the person may look at.
func (q *req) selection(scope string) (tenants []store.Space, canOrg bool, err error) {
	vis, err := q.rt.visibleTenants(q.w)
	if err != nil {
		return nil, false, err
	}
	canOrg, err = q.rt.manages(authz.Scope{Kind: store.ScopeOrg, ID: q.w.org.ID})
	if err != nil {
		return nil, false, err
	}
	if scope == "" || scope == "all" {
		if len(vis) == 0 && !canOrg {
			return nil, false, q.rt.require(authz.Scope{Kind: store.ScopeOrg, ID: q.w.org.ID})
		}
		return vis, canOrg, nil
	}
	sp, ok := q.w.tenant(scope)
	if !ok {
		return nil, false, httpx.ErrNotFound
	}
	if !slices.ContainsFunc(vis, func(v store.Space) bool { return v.ID == sp.ID }) {
		return nil, false, q.rt.require(authz.Scope{Kind: store.ScopeTenant, ID: sp.ID})
	}
	return []store.Space{sp}, canOrg, nil
}

func (q *req) leads(teamID string, names map[string]string) (string, error) {
	gs, err := q.st.GrantsOn(q.ctx, store.ScopeTeam, teamID)
	if err != nil {
		return "", err
	}
	var out []string
	for _, g := range gs {
		if g.Role != store.RoleLead {
			continue
		}
		n, ok := names[g.UserID]
		if !ok {
			u, err := q.st.UserByID(q.ctx, g.UserID)
			switch {
			case err == nil:
				n = cmp.Or(u.Name, u.Email)
			case !store.IsNotFound(err):
				return "", err
			}
			names[g.UserID] = n
		}
		if n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, ", "), nil
}

func (s *Service) getEnv(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	q, err := s.open(r, s.db.S(), p, FeatureEnv)
	if err != nil {
		return err
	}
	tenants, _, err := q.selection(r.URL.Query().Get("scope"))
	if err != nil {
		return err
	}
	teamFilter := r.URL.Query().Get("team")
	if teamFilter != "" {
		t, ok := q.w.teamByID[teamFilter]
		if !ok || !slices.ContainsFunc(tenants, func(v store.Space) bool { return v.ID == t.SpaceID }) {
			return httpx.ErrNotFound
		}
	}
	use, err := loadUsage(q.ctx, q.st, q.w.org.ID, s.now())
	if err != nil {
		return err
	}
	spaces := map[string]bool{}
	for _, sp := range tenants {
		spaces[sp.ID] = true
	}
	resp := Response{Layers: []LayerView{}, Teams: []TeamRef{}}
	names := map[string]string{}
	var layers []LayerView
	add := func(layer, scopeID, tenantID, title, subtitle string, c cover) error {
		edit, err := q.canEdit(layer, scopeID)
		if err != nil {
			return err
		}
		layers = append(layers, q.layerView(layer, scopeID, tenantID, title, subtitle, c, !edit, use))
		return nil
	}
	if err := add(store.EnvLayerOrg, q.w.org.ID, "", "Организация "+q.w.org.Name, "есть во всех тенантах", cover{spaces: spaces}); err != nil {
		return err
	}
	for _, sp := range tenants {
		if err := add(store.EnvLayerTenant, sp.ID, sp.ID, "Тенант "+sp.Name, "для всех команд тенанта", cover{spaces: map[string]bool{sp.ID: true}}); err != nil {
			return err
		}
		for _, t := range q.w.teams[sp.ID] {
			resp.Teams = append(resp.Teams, TeamRef{ID: t.ID, Name: t.Name, TenantID: sp.ID})
			if teamFilter != "" && t.ID != teamFilter {
				continue
			}
			sub := "только людям команды"
			lead, err := q.leads(t.ID, names)
			if err != nil {
				return err
			}
			if lead != "" {
				sub += " · лид " + lead
			}
			if err := add(store.EnvLayerTeam, t.ID, sp.ID, "Команда "+t.Name, sub, cover{spaces: map[string]bool{sp.ID: true}, team: t.ID}); err != nil {
				return err
			}
		}
	}
	resp.Layers = layers
	resp.Summary = q.summary(layers, use, spaces, teamFilter)
	inEnv := map[string]bool{}
	for _, l := range layers {
		for _, it := range l.Items {
			if it.Enabled {
				inEnv[it.ItemID] = true
			}
		}
	}
	resp.Observed = use.observed(cover{spaces: spaces, team: teamFilter}, inEnv)
	var byItem map[string]*ItemDelivery
	if resp.Delivery, byItem, err = q.delivery(tenants, teamFilter); err != nil {
		return err
	}
	for i := range resp.Layers {
		for j := range resp.Layers[i].Items {
			if it := &resp.Layers[i].Items[j]; it.Enabled {
				it.Delivery = byItem[it.ItemID]
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

// layerView lists the rows of one layer. Calls count what the layer's cover (the tenants of the screen, the tenant, the
// team) called; without any tool facts the organization has, they are unknown.
func (q *req) layerView(layer, scopeID, tenantID, title, subtitle string, c cover, locked bool, use *usageIndex) LayerView {
	lv := LayerView{Layer: layer, ScopeID: scopeID, TenantID: tenantID, Title: title, Subtitle: subtitle, Locked: locked, Items: []EntryItem{},
		Groups: q.w.groupsOf(layer, scopeID), Presets: q.w.presetsOf(layer, scopeID)}
	rs := q.w.resolverEntries()
	var calls int64
	for _, e := range q.w.entries {
		if e.Layer != layer || e.ScopeID != scopeID {
			continue
		}
		row, ok := q.row(e, !locked, c, use, rs)
		if !ok {
			continue
		}
		if e.Enabled {
			lv.Counts.add(row.Kind)
			if row.CallsWeek != nil {
				calls += *row.CallsWeek
			}
		}
		lv.Items = append(lv.Items, row)
	}
	sortEntryItems(lv.Items)
	if use.has {
		lv.CallsWeek = &calls
	}
	return lv
}

// row is an entry as a layer lists it; false for an entry of an archived item.
func (q *req) row(e store.EnvEntry, edit bool, c cover, use *usageIndex, rs []Entry) (EntryItem, bool) {
	it, ok := q.w.items[e.ItemID]
	if !ok || it.ArchivedAt != nil {
		return EntryItem{}, false
	}
	above := ""
	if e.Layer != store.EnvLayerOrg {
		above = LockedAbove(rs, e.Layer, q.w.org.ID, q.w.tenantOf(e.Layer, e.ScopeID), e.ItemID)
	}
	row := EntryItem{EntryID: e.ID, ItemID: it.ID, Kind: it.Kind, Name: it.Name, Hint: it.Hint, Audience: audienceView(e.Audience),
		Pinned: e.Pinned, Enabled: e.Enabled, GroupID: e.GroupID}
	row.LockedReason = lockedReason(edit, above, e)
	row.Locked = row.LockedReason != nil
	if use.has {
		st := use.stat(it.ID, c)
		row.CallsWeek, row.Spark14, row.LastUsedAt = &st.week, st.spark, msStr(st.last)
		if e.Enabled {
			row.Unused = unusedOf(e, st, use)
		}
	}
	return row, true
}

// unusedOf: nothing called for a month, and the entry is older than that (an entry put in last week has not had the
// time to be called).
func unusedOf(e store.EnvEntry, st stat, use *usageIndex) *bool {
	v := st.month == 0 && e.UpdatedAt.Before(use.now.AddDate(0, 0, -unusedDays))
	return &v
}

func (q *req) summary(layers []LayerView, use *usageIndex, spaces map[string]bool, team string) Summary {
	sum := Summary{Top: []TopItem{}}
	enabled := map[string]bool{}
	for _, l := range layers {
		for _, it := range l.Items {
			if !it.Enabled {
				continue
			}
			sum.Total++
			sum.ByKind.add(it.Kind)
			enabled[it.ItemID] = true
			if l.Layer == store.EnvLayerOrg && it.Pinned {
				sum.PinnedByOrg++
			}
			if it.Unused != nil && *it.Unused {
				if sum.Unused == nil {
					sum.Unused = new(int)
				}
				*sum.Unused++
			}
		}
	}
	if !use.has {
		return sum
	}
	if sum.Unused == nil {
		sum.Unused = new(int)
	}
	c := cover{spaces: spaces, team: team}
	total := use.total(enabled, c)
	sum.CallsWeek = &total
	type ranked struct {
		id, name string
		calls    int64
	}
	var rank []ranked
	for id := range enabled {
		if n := use.total(map[string]bool{id: true}, c); n > 0 {
			rank = append(rank, ranked{id, q.w.items[id].Name, n})
		}
	}
	slices.SortFunc(rank, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(b.calls, a.calls), cmp.Compare(a.name, b.name), cmp.Compare(a.id, b.id))
	})
	for i := 0; i < len(rank) && i < 2; i++ {
		sum.Top = append(sum.Top, TopItem{ID: rank[i].id, Name: rank[i].name})
	}
	return sum
}

// entryOf finds an entry of the organization.
func (q *req) entryOf(id string) (store.EnvEntry, error) {
	e, err := q.st.EnvEntryByID(q.ctx, id)
	if store.IsNotFound(err) || (err == nil && e.OrgID != q.w.org.ID) {
		return e, httpx.ErrNotFound
	}
	return e, err
}

// canSeeEntry: the person looks at the entry's tenant (any tenant for the organization's layer).
func (q *req) canSeeEntry(e store.EnvEntry) error {
	if e.Layer == store.EnvLayerOrg {
		vis, err := q.rt.visibleTenants(q.w)
		if err != nil {
			return err
		}
		ok, err := q.rt.manages(authz.Scope{Kind: store.ScopeOrg, ID: q.w.org.ID})
		if err != nil {
			return err
		}
		if len(vis) == 0 && !ok {
			return q.rt.require(authz.Scope{Kind: store.ScopeOrg, ID: q.w.org.ID})
		}
		return nil
	}
	tenantID := q.w.tenantOf(e.Layer, e.ScopeID)
	ok, err := q.rt.sees(q.w, tenantID)
	if err != nil {
		return err
	}
	if !ok {
		return q.rt.require(authz.Scope{Kind: store.ScopeTenant, ID: tenantID})
	}
	return nil
}

func (s *Service) getCard(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	q, err := s.open(r, s.db.S(), p, FeatureEnv)
	if err != nil {
		return err
	}
	e, err := q.entryOf(r.PathValue("entryId"))
	if err != nil {
		return err
	}
	if err := q.canSeeEntry(e); err != nil {
		return err
	}
	it, ok := q.w.items[e.ItemID]
	if !ok {
		return httpx.ErrNotFound
	}
	spaces := map[string]bool{}
	tenants, _, err := q.selection(r.URL.Query().Get("scope"))
	if err != nil {
		return err
	}
	for _, sp := range tenants {
		spaces[sp.ID] = true
	}
	c := cover{spaces: spaces}
	tenantID := q.w.tenantOf(e.Layer, e.ScopeID)
	switch e.Layer {
	case store.EnvLayerTenant:
		c = cover{spaces: map[string]bool{tenantID: true}}
	case store.EnvLayerTeam:
		c = cover{spaces: map[string]bool{tenantID: true}, team: e.ScopeID}
	}
	edit, err := q.canEdit(e.Layer, e.ScopeID)
	if err != nil {
		return err
	}
	above := ""
	if e.Layer != store.EnvLayerOrg {
		above = LockedAbove(q.w.resolverEntries(), e.Layer, q.w.org.ID, tenantID, e.ItemID)
	}
	card := Card{EntryID: e.ID, ItemID: it.ID, Kind: it.Kind, Name: it.Name, Hint: it.Hint, Layer: e.Layer, ScopeID: e.ScopeID,
		Enabled: e.Enabled, Audience: audienceView(e.Audience), Pinned: e.Pinned, LockedReason: lockedReason(edit, above, e)}
	card.Locked = card.LockedReason != nil
	use, err := loadUsage(q.ctx, q.st, q.w.org.ID, s.now())
	if err != nil {
		return err
	}
	if use.has {
		st := use.stat(it.ID, c)
		people := len(st.people)
		perDay := math.Round(float64(st.week)/weekDays*10) / 10
		card.CallsWeek, card.People, card.PerDay, card.LastUsedAt = &st.week, &people, &perDay, msStr(st.last)
		card.Bars14 = make([]Bar, sparkDays)
		for i, d := range use.days {
			card.Bars14[i] = Bar{Day: d, Calls: st.spark[i]}
		}
		if e.Enabled {
			card.Unused = unusedOf(e, st, use)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, card)
	return nil
}

// entryIn is the body of POST …/env/entries.
type entryIn struct {
	Layer    string      `json:"layer"`
	ScopeID  string      `json:"scopeId"`
	ItemID   string      `json:"itemId"`
	Audience *AudienceIn `json:"audience"`
	Enabled  *bool       `json:"enabled"`
	Pinned   *bool       `json:"pinned"`
	// GroupID puts the entry into a group of its layer; "" takes it out.
	GroupID *string `json:"groupId"`
}

// audienceTeams are the teams a layer's audience may name.
func (w *world) audienceTeams(layer, scopeID string) map[string]bool {
	out := map[string]bool{}
	for tid, ts := range w.teams {
		if layer == store.EnvLayerTenant && tid != scopeID {
			continue
		}
		for _, t := range ts {
			out[t.ID] = true
		}
	}
	return out
}

func entryJournal(e store.EnvEntry) map[string]any {
	return map[string]any{"layer": e.Layer, "scopeId": e.ScopeID, "itemId": e.ItemID, "enabled": e.Enabled, "pinned": e.Pinned,
		"audience": audienceView(e.Audience)}
}

func (s *Service) addEntry(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in entryIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out store.EnvEntry
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
		it, ok := q.w.items[in.ItemID]
		if !ok || it.ArchivedAt != nil || (it.OrgID != "" && it.OrgID != q.w.org.ID) {
			return httpx.Invalid("itemId", "Такого элемента нет в каталоге")
		}
		if !FitsLayer(it.OwnerLayer, it.OwnerScope, in.Layer, in.ScopeID) {
			return httpx.Invalid("itemId", "Этот пункт принадлежит другому слою — скопируйте его")
		}
		aud, err := CleanAudience(in.Layer, in.Audience, q.w.audienceTeams(in.Layer, in.ScopeID))
		if err != nil {
			return err
		}
		e := store.EnvEntry{OrgID: q.w.org.ID, Layer: in.Layer, ScopeID: in.ScopeID, ItemID: it.ID, Enabled: in.Enabled == nil || *in.Enabled,
			Pinned: in.Pinned != nil && *in.Pinned, Audience: AudienceJSON(aud), Rev: 1, UpdatedAt: s.now(), UpdatedBy: p.User.ID}
		if it.Kind == store.CatalogScript {
			e.Position = q.w.nextPosition(in.Layer, in.ScopeID)
		}
		if in.GroupID != nil && *in.GroupID != "" {
			g, ok := q.w.group(*in.GroupID)
			if !ok || g.Layer != in.Layer || g.ScopeID != in.ScopeID {
				return httpx.Invalid("groupId", "Такой группы в этом слое нет")
			}
			e.GroupID = g.ID
		}
		if !e.Enabled && in.Layer != store.EnvLayerOrg {
			if LockedAbove(q.w.resolverEntries(), in.Layer, q.w.org.ID, q.w.tenantOf(in.Layer, in.ScopeID), it.ID) != "" {
				return httpx.Conflict("locked", "Закреплено выше: выключить здесь нельзя")
			}
		}
		var added bool
		if out, added, err = st.AddEnvEntry(r.Context(), e); err != nil {
			return err
		} else if !added {
			return httpx.Conflict("exists", "Этот элемент в слое уже есть")
		}
		verb := "добавлен"
		if !e.Enabled {
			verb = "выключен"
		}
		title := fmt.Sprintf("Окружение: %s «%s» %s в %s", kindRu[it.Kind], it.Name, verb, q.w.whereRu(in.Layer, in.ScopeID))
		return s.record(r.Context(), st, q.w, p, in.Layer, in.ScopeID, title, "env.entry.add", "env_entry", out.ID, nil, entryJournal(out), s.now())
	})
	if err != nil {
		return err
	}
	s.afterChange(q.w.org.ID, out)
	card, err := s.answerRow(r, p, out.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, card)
	return nil
}

func (s *Service) afterChange(orgID string, e store.EnvEntry) {
	s.publish(orgID, scopeOf(e.Layer, e.ScopeID).Key(), e.ID, e.Rev)
	s.kick()
}

// patchIn is the body of PATCH …/env/entries/{id}: what is absent stays.
type patchIn struct {
	Enabled  *bool       `json:"enabled"`
	Audience *AudienceIn `json:"audience"`
	Pinned   *bool       `json:"pinned"`
	// GroupID puts the entry into a group of its layer; "" takes it out.
	GroupID *string `json:"groupId"`
}

func (s *Service) patchEntry(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in patchIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var out store.EnvEntry
	var q *req
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureEnv); err != nil {
			return err
		}
		e, err := q.entryOf(r.PathValue("entryId"))
		if err != nil {
			return err
		}
		if err := q.canSeeEntry(e); err != nil {
			return err
		}
		if err := q.rt.require(scopeOf(e.Layer, e.ScopeID)); err != nil {
			return err
		}
		before := entryJournal(e)
		it := q.w.items[e.ItemID]
		var verbs []string
		if in.Enabled != nil && *in.Enabled != e.Enabled {
			if !*in.Enabled && e.Layer != store.EnvLayerOrg &&
				LockedAbove(q.w.resolverEntries(), e.Layer, q.w.org.ID, q.w.tenantOf(e.Layer, e.ScopeID), e.ItemID) != "" {
				return httpx.Conflict("locked", "Закреплено выше: выключить здесь нельзя")
			}
			e.Enabled = *in.Enabled
			verbs = append(verbs, map[bool]string{true: "включён", false: "выключен"}[e.Enabled])
		}
		if in.Pinned != nil && *in.Pinned != e.Pinned {
			e.Pinned = *in.Pinned
			verbs = append(verbs, map[bool]string{true: "закреплён", false: "откреплён"}[e.Pinned])
		}
		if in.Audience != nil {
			aud, err := CleanAudience(e.Layer, in.Audience, q.w.audienceTeams(e.Layer, e.ScopeID))
			if err != nil {
				return err
			}
			if js := AudienceJSON(aud); js != AudienceJSON(ParseAudience(e.Audience)) {
				e.Audience = js
				verbs = append(verbs, "изменён доступ")
			}
		}
		if in.GroupID != nil && *in.GroupID != e.GroupID {
			if *in.GroupID != "" {
				g, ok := q.w.group(*in.GroupID)
				if !ok || g.Layer != e.Layer || g.ScopeID != e.ScopeID {
					return httpx.Invalid("groupId", "Такой группы в этом слое нет")
				}
				verbs = append(verbs, "перенесён в группу «"+g.Name+"»")
			} else {
				verbs = append(verbs, "убран из группы")
			}
			e.GroupID = *in.GroupID
		}
		if len(verbs) == 0 {
			out = e
			return nil
		}
		e.Rev++
		e.UpdatedAt, e.UpdatedBy = s.now(), p.User.ID
		if err := st.UpdateEnvEntry(r.Context(), e); err != nil {
			return err
		}
		out = e
		title := fmt.Sprintf("Окружение: %s «%s» %s в %s", kindRu[it.Kind], it.Name, strings.Join(verbs, ", "), q.w.whereRu(e.Layer, e.ScopeID))
		return s.record(r.Context(), st, q.w, p, e.Layer, e.ScopeID, title, "env.entry.update", "env_entry", e.ID, before, entryJournal(e), s.now())
	})
	if err != nil {
		return err
	}
	s.afterChange(q.w.org.ID, out)
	row, err := s.answerRow(r, p, out.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, row)
	return nil
}

func (s *Service) deleteEntry(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var out store.EnvEntry
	var q *req
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureEnv); err != nil {
			return err
		}
		e, err := q.entryOf(r.PathValue("entryId"))
		if err != nil {
			return err
		}
		if err := q.canSeeEntry(e); err != nil {
			return err
		}
		if err := q.rt.require(scopeOf(e.Layer, e.ScopeID)); err != nil {
			return err
		}
		if err := st.DeleteEnvEntry(r.Context(), e.ID); err != nil {
			return err
		}
		e.Rev++
		out = e
		it := q.w.items[e.ItemID]
		title := fmt.Sprintf("Окружение: %s «%s» убран из %s", kindRu[it.Kind], it.Name, q.w.whereRu(e.Layer, e.ScopeID))
		return s.record(r.Context(), st, q.w, p, e.Layer, e.ScopeID, title, "env.entry.remove", "env_entry", e.ID, entryJournal(e), nil, s.now())
	})
	if err != nil {
		return err
	}
	s.afterChange(q.w.org.ID, out)
	httpx.NoContent(w)
	return nil
}

// answerRow is the row of an entry as the screen shows it, for the answer of a change.
func (s *Service) answerRow(r *http.Request, p *auth.Principal, entryID string) (EntryItem, error) {
	q, err := s.open(r, s.db.S(), p, FeatureEnv)
	if err != nil {
		return EntryItem{}, err
	}
	for _, e := range q.w.entries {
		if e.ID != entryID {
			continue
		}
		edit, err := q.canEdit(e.Layer, e.ScopeID)
		if err != nil {
			return EntryItem{}, err
		}
		use, err := loadUsage(q.ctx, q.st, q.w.org.ID, s.now())
		if err != nil {
			return EntryItem{}, err
		}
		row, _ := q.row(e, edit, q.coverOf(e), use, q.w.resolverEntries())
		return row, nil
	}
	return EntryItem{}, httpx.ErrNotFound
}

// coverOf is where an entry's calls are counted: all the organization's tenants, the tenant, the team.
func (q *req) coverOf(e store.EnvEntry) cover {
	switch e.Layer {
	case store.EnvLayerTenant:
		return cover{spaces: map[string]bool{e.ScopeID: true}}
	case store.EnvLayerTeam:
		return cover{spaces: map[string]bool{q.w.tenantOf(e.Layer, e.ScopeID): true}, team: e.ScopeID}
	}
	return cover{}
}

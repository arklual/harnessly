package env

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
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
	GroupID    string `json:"groupId"` // the group the item is in ("" for none); a group switched off makes Off true
	// Own: the person's own item (layer "user"): they change and remove it at /api/me/env/own/{itemId}.
	Own bool `json:"own,omitempty"`
	// Shadowed: an own item the organization gives one of the same kind and name of: it is not on the machines.
	Shadowed bool `json:"shadowed,omitempty"`
	// Source is an own item's source (what it runs), for the person to change it; empty for anything else.
	Source json.RawMessage `json:"source,omitempty"`
}

// MeResponse is GET /api/me/env.
type MeResponse struct {
	Space MeSpace  `json:"space"`
	Org   *MeOrg   `json:"org"`
	Items []MeItem `json:"items"`
	// Groups are the groups the person's items are in: required ones (always on) and optional ones (theirs to switch).
	Groups []MeGroup `json:"groups"`
	// Presets are the layers' sets of groups with at least one group the person has items in.
	Presets []MePreset `json:"presets"`
	// Outdated: some machine of the person in the tenant does not hold the current list or has a setup item that failed.
	Outdated bool        `json:"outdated"`
	Machines []MeMachine `json:"machines"`
	// Own: the organization has a person's own layer: the person may add their own skills and MCP servers
	// (/api/me/env/own), and what they switch off leaves their machines.
	Own bool `json:"own"`
}

// MeMachine is a machine of the person with what its setup looks like.
type MeMachine struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Online  bool       `json:"online"`
	Setup   bool       `json:"setup"`   // has environment.setup
	Current bool       `json:"current"` // holds the list the hub computes for it now
	Failed  int        `json:"failed"`  // setup items in error or waiting for the person
	LastRun *EnvRunRow `json:"lastRun"`
	// Team is the team whose list the machine holds; null for a machine of no team.
	Team *NameRef `json:"team"`
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
	case store.EnvLayerUser:
		return "Своё"
	}
	return "Команда " + w.teamByID[scopeID].Name
}

// openMe finds the tenant (?space= or the body's space: an id or a slug; the only one the person is in when none is
// named), checks the person is in it and that the environment is on for its organization.
func (s *Service) openMe(r *http.Request, st store.Store, p *auth.Principal, ref string) (*me, error) {
	return s.openMeFor(r.Context(), st, p.User.ID, ref)
}

// openMeFor is openMe for a person by id (their machine's change of their own layer has no session).
func (s *Service) openMeFor(ctx context.Context, st store.Store, userID, ref string) (*me, error) {
	mine, err := st.SpacesByUser(ctx, userID)
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
	w, err := loadWorld(ctx, st, o, s.builtIn)
	if err != nil {
		return nil, err
	}
	tms, err := st.TeamsOfUser(ctx, userID)
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
	off, err := st.EnvPersonOffs(ctx, userID, sp.ID)
	if err != nil {
		return nil, err
	}
	person := Person{OrgID: o.ID, SpaceID: sp.ID, Teams: teams, Off: off, Groups: w.resolverGroups(), GroupOn: w.choicesOf(sp.ID, userID)}
	if w.own {
		person.UserID = userID
	}
	return &me{ctx: ctx, st: st, space: sp, org: o, w: w, person: person}, nil
}

func (m *me) effective() []Effective {
	return m.w.resolve(m.person)
}

func (s *Service) meEnv(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.openMe(r, s.db.S(), p, r.URL.Query().Get("space"))
	if err != nil {
		return err
	}
	out := MeResponse{Space: MeSpace{ID: m.space.ID, Slug: m.space.Slug, Name: m.space.Name}, Org: &MeOrg{ID: m.org.ID, Name: m.org.Name}, Items: []MeItem{}}
	if out.Machines, err = s.meMachineList(m, p.User.ID); err != nil {
		return err
	}
	for _, mm := range out.Machines {
		// A machine that cannot do the setup is not behind on it: it is left out.
		if mm.Setup && (!mm.Current || mm.Failed > 0) {
			out.Outdated = true
		}
	}
	eff := m.effective()
	out.Own = m.w.own
	out.Groups = m.meGroups(eff)
	out.Presets = m.mePresets(out.Groups)
	for _, e := range eff {
		scope := m.org.ID
		switch e.Layer {
		case store.EnvLayerTenant:
			scope = m.space.ID
		case store.EnvLayerTeam:
			scope = m.entryScope(e.EntryID)
		case store.EnvLayerUser:
			scope = p.User.ID
		}
		out.Items = append(out.Items, MeItem{ItemID: e.ID, EntryID: e.EntryID, Kind: e.Kind, Name: e.Name, Hint: e.Hint, Layer: e.Layer, ScopeID: scope,
			LayerTitle: m.w.layerTitle(e.Layer, scope), Pinned: e.Pinned, Off: e.Off, CanToggle: !e.Pinned && !hubproto.IsBuiltInEnvItem(e.ID), GroupID: e.GroupID,
			Own: e.Layer == store.EnvLayerUser})
		if e.Layer == store.EnvLayerUser {
			out.Items[len(out.Items)-1].Source = e.Source
		}
	}
	if m.w.own {
		// One's own item that what the organization gives of the same name left out: still theirs to change or remove.
		in := map[string]bool{}
		for _, e := range eff {
			in[e.ID] = true
		}
		o := &own{w: m.w, userID: p.User.ID}
		for _, c := range o.items() {
			if !in[c.ID] {
				it := meOwnItem(m, c)
				it.Shadowed = true
				out.Items = append(out.Items, it)
			}
		}
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
// organization; only the audit trail remembers it. With a person's own layer (FeatureOwn) their machines' lists follow.
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
		out, err = s.switchMe(m, p.User.ID, itemID, *in.Off)
		return err
	})
	if err != nil {
		return err
	}
	s.kick() // with the own layer the switch is on the person's machines' lists
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// switchMe switches an item of the person's environment off or back on for them in the tenant m is opened for.
func (s *Service) switchMe(m *me, userID, itemID string, off bool) (MeItem, error) {
	var eff *Effective
	for _, e := range m.effective() {
		if e.ID == itemID {
			eff = &e
			break
		}
	}
	if eff == nil {
		return MeItem{}, httpx.ErrNotFound
	}
	if eff.Pinned {
		return MeItem{}, httpx.Conflict("locked", "Закреплено: выключить нельзя")
	}
	if hubproto.IsBuiltInEnvItem(itemID) {
		// The platform's items are not rows of the catalog, and this switch is kept by catalog item. A person turns
		// a platform's app off on their machine («Приложения»), a platform's group with the group's switch.
		return MeItem{}, httpx.Conflict("built_in", "Встроенное выключают на своей машине, в «Приложениях»")
	}
	was := m.person.Off[itemID]
	if was != off {
		if err := m.st.SetEnvPersonOff(m.ctx, userID, m.space.ID, itemID, off, s.now()); err != nil {
			return MeItem{}, err
		}
		chain, err := orgs.Chain(m.ctx, m.st, m.org, authz.Scope{Kind: store.ScopeTenant, ID: m.space.ID})
		if err != nil {
			return MeItem{}, err
		}
		if err := m.st.AddAudit(m.ctx, orgs.Journal(chain, userID, "env.person.off", "catalog_item", itemID,
			map[string]any{"off": was}, map[string]any{"off": off, "spaceId": m.space.ID}, s.now())); err != nil {
			return MeItem{}, err
		}
	}
	out := MeItem{ItemID: eff.ID, EntryID: eff.EntryID, Kind: eff.Kind, Name: eff.Name, Hint: eff.Hint, Layer: eff.Layer, Off: off, CanToggle: true}
	scope := m.org.ID
	switch eff.Layer {
	case store.EnvLayerTenant:
		scope = m.space.ID
	case store.EnvLayerTeam:
		scope = m.entryScope(eff.EntryID)
	case store.EnvLayerUser:
		scope = userID
	}
	out.ScopeID, out.LayerTitle, out.Own = scope, m.w.layerTitle(eff.Layer, scope), eff.Layer == store.EnvLayerUser
	return out, nil
}

// meMachineList are the person's machines in the tenant: the working ones, with what their setup looks like.
func (s *Service) meMachineList(m *me, userID string) ([]MeMachine, error) {
	out := []MeMachine{}
	ms, err := m.st.MachinesByUser(m.ctx, m.space.ID, userID)
	if err != nil {
		return nil, err
	}
	var online []string
	if s.reg != nil {
		online = s.reg.Online()
	}
	names := newRunNames(m.ctx, m.st)
	for _, mc := range ms {
		if mc.RevokedAt != nil {
			continue
		}
		mm := MeMachine{ID: mc.ID, Name: mc.Name, Online: mc.DisabledAt == nil && slices.Contains(online, mc.ID)}
		if t, ok := m.w.teamByID[mc.TeamID]; ok {
			mm.Team = &NameRef{ID: t.ID, Name: t.Name}
		}
		if s.push != nil {
			mm.Setup, _ = s.push.SetupOf(mc.ID)
		}
		want := s.machineList(m.w, m.space.ID, mc)
		st, err := m.st.EnvStateByMachine(m.ctx, mc.ID)
		reported := err == nil && !st.At.IsZero()
		if err != nil && !store.IsNotFound(err) {
			return nil, err
		}
		mm.Current = want.empty() || (reported && want.holds(st.Hash))
		if reported && mm.Setup {
			mm.Failed = m.failedSetup(st.Applied)
		}
		runs, err := m.st.EnvRunsOf(m.ctx, store.EnvRunQuery{OrgID: m.org.ID, MachineIDs: []string{mc.ID}, Since: s.runsSince(), Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(runs) > 0 {
			row, err := names.row(runs[0], false)
			if err != nil {
				return nil, err
			}
			mm.LastRun = &row
		}
		out = append(out, mm)
	}
	return out, nil
}

// tokenAsk starts the message of a setup item that waits for a token of the person's safe («положите токен <name> в
// сейф»); any other pending is the machine still working.
const tokenAsk = "положите токен"

// failedSetup counts the setup items a machine reported in error, or waiting for a token the person has to put in the safe.
func (m *me) failedSetup(applied string) int {
	var items []hubproto.EnvApplied
	if json.Unmarshal([]byte(applied), &items) != nil {
		return 0
	}
	n := 0
	for _, a := range items {
		waits := a.Status == hubproto.EnvPending && strings.Contains(a.Message, tokenAsk)
		if (a.Status == hubproto.EnvError || waits) && hubproto.IsSetupKind(m.w.items[a.ItemID].Kind) {
			n++
		}
	}
	return n
}

func (s *Service) meMachines(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.openMe(r, s.db.S(), p, r.URL.Query().Get("space"))
	if err != nil {
		return err
	}
	list, err := s.meMachineList(m, p.User.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machines": list})
	return nil
}

type meRunIn struct {
	Mode      string `json:"mode"`
	MachineID string `json:"machineId"`
	Space     string `json:"space"`
}

// meRun asks the person's machines in the tenant to update or check their setup.
func (s *Service) meRun(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in meRunIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Mode != hubproto.RunUpdate && in.Mode != hubproto.RunCheck {
		return httpx.Invalid("mode", "update или check")
	}
	space := r.URL.Query().Get("space")
	if space == "" {
		space = in.Space
	}
	m, err := s.openMe(r, s.db.S(), p, space)
	if err != nil {
		return err
	}
	cands, err := s.ownSetupMachines(m.ctx, m.st, p.User.ID, m.space.ID)
	if err != nil {
		return err
	}
	if in.MachineID != "" {
		cands = slices.DeleteFunc(cands, func(mc store.Machine) bool { return mc.ID != in.MachineID })
	}
	if len(cands) == 0 {
		return errNoMachine
	}
	by := cmp.Or(p.User.Name, p.User.Email)
	type started struct {
		RunID     string `json:"runId"`
		MachineID string `json:"machineId"`
	}
	runs := []started{}
	var firstErr error
	for _, mc := range cands {
		id, err := s.startRun(m.ctx, m.org.ID, mc, in.Mode, nil, by)
		if err != nil {
			s.log.Warn("environment run not accepted", "machine", mc.ID, "error", err)
			firstErr = cmp.Or(firstErr, err)
			continue
		}
		runs = append(runs, started{id, mc.ID})
	}
	if len(runs) == 0 {
		return firstErr
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"runs": runs})
	return nil
}

func (s *Service) meRuns(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.openMe(r, s.db.S(), p, r.URL.Query().Get("space"))
	if err != nil {
		return err
	}
	limit, err := runLimit(r)
	if err != nil {
		return err
	}
	ms, err := m.st.MachinesByUser(m.ctx, m.space.ID, p.User.ID)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, mc := range ms {
		ids = append(ids, mc.ID)
	}
	rows, err := m.st.EnvRunsOf(m.ctx, store.EnvRunQuery{OrgID: m.org.ID, MachineIDs: ids, Since: s.runsSince(), Limit: limit})
	if err != nil {
		return err
	}
	return writeRuns(w, newRunNames(m.ctx, m.st), rows)
}

// Package envseed fills the catalog and the environment lists of organizations from a file (`hub env-seed`): the way
// the lists of another system are moved in. Items are matched by (organization, kind, name), entries by (layer, scope,
// item), so a second run changes nothing, and a dry run is the same run rolled back. Sources go through the same check
// as the API's (env.CleanSource).
package envseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// File is the env-seed file.
type File struct {
	Orgs []Org `yaml:"orgs"`
}

// Org is what goes into one organization, named by its slug.
type Org struct {
	Org     string  `yaml:"org"`
	Items   []Item  `yaml:"items"`
	Entries []Entry `yaml:"entries"`
}

// Item is a catalog item. Owner narrows who may change it and where it may be used; none: the organization.
type Item struct {
	Kind   string         `yaml:"kind"`
	Name   string         `yaml:"name"`
	Hint   string         `yaml:"hint"`
	Source map[string]any `yaml:"source"`
	Owner  *Scope         `yaml:"owner"`
}

// Scope names a layer: org, a tenant by slug, or a team by its name inside a tenant.
type Scope struct {
	Layer  string `yaml:"layer"`
	Tenant string `yaml:"tenant"`
	Team   string `yaml:"team"`
}

// Entry puts an item into a layer.
type Entry struct {
	Scope    `yaml:",inline"`
	Item     string `yaml:"item"`
	Kind     string `yaml:"kind"` // only when two kinds have an item of this name
	Pinned   bool   `yaml:"pinned"`
	Disabled bool   `yaml:"disabled"`
	// Audience is tenant (the default) or off.
	Audience string `yaml:"audience"`
	// Position is the place of a script among the layer's scripts; none: after the others for a new entry, unchanged for an old one.
	Position int `yaml:"position"`
}

// Parse reads the file.
func Parse(r io.Reader) (File, error) {
	var f File
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("env-seed file: %w", err)
	}
	for _, o := range f.Orgs {
		if o.Org == "" {
			return File{}, errors.New("env-seed file: every entry of orgs needs org (the slug)")
		}
	}
	return f, nil
}

var errDryRun = errors.New("dry run")

// Apply makes the changes in one transaction and says what it did; with dry nothing is written.
func Apply(ctx context.Context, db *store.DB, f File, dry bool, now time.Time) ([]string, error) {
	var changes []string
	err := db.Tx(ctx, func(st store.Store) error {
		a := &applier{ctx: ctx, st: st, now: now.UTC()}
		for _, o := range f.Orgs {
			if err := a.org(o); err != nil {
				return fmt.Errorf("org %s: %w", o.Org, err)
			}
		}
		changes = a.changes
		if dry {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	return changes, err
}

type applier struct {
	ctx     context.Context
	st      store.Store
	now     time.Time
	changes []string

	org_    store.Org
	items   []store.CatalogItem
	tenants map[string]store.Space // by slug
	teams   map[string]store.Team  // by tenant slug + "/" + team name
}

func (a *applier) note(format string, args ...any) {
	a.changes = append(a.changes, fmt.Sprintf(format, args...))
}

func (a *applier) org(o Org) error {
	var err error
	if a.org_, err = a.st.OrgBySlug(a.ctx, o.Org); err != nil {
		if store.IsNotFound(err) {
			return errors.New("no such organization")
		}
		return err
	}
	a.tenants, a.teams = map[string]store.Space{}, map[string]store.Team{}
	spaces, err := a.st.SpacesByOrg(a.ctx, a.org_.ID)
	if err != nil {
		return err
	}
	for _, sp := range spaces {
		a.tenants[sp.Slug] = sp
		ts, err := a.st.TeamsBySpace(a.ctx, sp.ID)
		if err != nil {
			return err
		}
		for _, t := range ts {
			a.teams[sp.Slug+"/"+t.Name] = t
		}
	}
	if a.items, err = a.st.CatalogItems(a.ctx, a.org_.ID, true); err != nil {
		return err
	}
	for i, it := range o.Items {
		if err := a.item(it); err != nil {
			return fmt.Errorf("items[%d] %q: %w", i, it.Name, err)
		}
	}
	for i, e := range o.Entries {
		if err := a.entry(e); err != nil {
			return fmt.Errorf("entries[%d] %q: %w", i, e.Item, err)
		}
	}
	return nil
}

// resolve turns a scope of the file into the layer and the id of the scope; nil is the organization.
func (a *applier) resolve(s *Scope) (layer, scopeID string, err error) {
	if s == nil || (s.Layer == store.EnvLayerOrg) {
		return store.EnvLayerOrg, a.org_.ID, nil
	}
	sp, ok := a.tenants[s.Tenant]
	switch s.Layer {
	case store.EnvLayerTenant:
		if !ok {
			return "", "", fmt.Errorf("no tenant %q in the organization", s.Tenant)
		}
		return s.Layer, sp.ID, nil
	case store.EnvLayerTeam:
		t, ok := a.teams[s.Tenant+"/"+s.Team]
		if !ok {
			return "", "", fmt.Errorf("no team %q in tenant %q", s.Team, s.Tenant)
		}
		return s.Layer, t.ID, nil
	}
	return "", "", errors.New("layer is org, tenant or team")
}

func (a *applier) find(kind, name string) (int, bool) {
	for i, c := range a.items {
		if c.OrgID == a.org_.ID && c.Kind == kind && strings.EqualFold(c.Name, name) {
			return i, true
		}
	}
	return 0, false
}

func (a *applier) item(in Item) error {
	if !env.ValidKind(in.Kind) {
		return errors.New("kind is skill, mcp, repo, doc, script or access")
	}
	name, err := env.CleanText("name", in.Name, 80, true)
	if err != nil {
		return err
	}
	hint, err := env.CleanText("hint", in.Hint, 200, false)
	if err != nil {
		return err
	}
	if in.Kind == store.CatalogScript {
		// A script names its repository by the item's name here; the catalog keeps the id.
		if r, ok := in.Source["repo"].(string); ok && r != "" {
			if i, found := a.find(store.CatalogRepo, r); found {
				in.Source["repo"] = a.items[i].ID
			}
		}
	}
	rawSrc, err := json.Marshal(in.Source)
	if err != nil {
		return err
	}
	src, err := env.CleanSource(in.Kind, rawSrc)
	if err != nil {
		return err
	}
	if in.Kind == store.CatalogScript {
		var s struct{ Repo string }
		_ = json.Unmarshal(src, &s)
		if s.Repo != "" {
			if i := slices.IndexFunc(a.items, func(c store.CatalogItem) bool { return c.ID == s.Repo }); i < 0 || a.items[i].Kind != store.CatalogRepo {
				return fmt.Errorf("source.repo %q is not a repository of the catalog", s.Repo)
			}
		}
	}
	ownerLayer, ownerScope, err := a.resolve(in.Owner)
	if err != nil {
		return err
	}
	if ownerLayer == store.EnvLayerOrg {
		ownerLayer, ownerScope = "", ""
	}
	// The same rule as the API's: secrets of the safe and accesses are the organization's admins' to connect.
	if ownerLayer != "" {
		if err := env.CheckConsent(in.Kind, src); err != nil {
			return err
		}
	}
	jlayer, jscope := store.EnvLayerOrg, a.org_.ID
	if ownerLayer != "" {
		jlayer, jscope = ownerLayer, ownerScope
	}
	i, found := a.find(in.Kind, name)
	if !found {
		c, err := a.st.AddCatalogItem(a.ctx, store.CatalogItem{OrgID: a.org_.ID, Kind: in.Kind, Name: name, Hint: hint, Source: string(src),
			CreatedAt: a.now, OwnerLayer: ownerLayer, OwnerScope: ownerScope})
		if err != nil {
			return err
		}
		a.items = append(a.items, c)
		a.note("+ %s «%s»", in.Kind, name)
		return a.journal(jlayer, jscope, "catalog.add", "catalog_item", c.ID, fmt.Sprintf("Каталог (из файла): добавлен %s «%s»", in.Kind, name),
			map[string]any{"kind": in.Kind, "name": name, "hint": hint})
	}
	c := a.items[i]
	var what []string
	if c.Name != name {
		what, c.Name = append(what, "name"), name
	}
	if c.Hint != hint {
		what, c.Hint = append(what, "hint"), hint
	}
	if string(env.CanonicalSource(c.Source)) != string(src) {
		what, c.Source = append(what, "source"), string(src)
	}
	if c.OwnerLayer != ownerLayer || c.OwnerScope != ownerScope {
		what, c.OwnerLayer, c.OwnerScope = append(what, "owner"), ownerLayer, ownerScope
	}
	if c.ArchivedAt != nil {
		what, c.ArchivedAt = append(what, "restored"), nil
	}
	if len(what) == 0 {
		return nil
	}
	if err := a.st.UpdateCatalogItem(a.ctx, c); err != nil {
		return err
	}
	a.items[i] = c
	a.note("~ %s «%s» (%s)", in.Kind, name, strings.Join(what, ", "))
	return a.journal(jlayer, jscope, "catalog.update", "catalog_item", c.ID, fmt.Sprintf("Каталог (из файла): %s «%s» изменён (%s)", in.Kind, name, strings.Join(what, ", ")),
		map[string]any{"kind": in.Kind, "name": name, "hint": hint, "changed": what})
}

func (a *applier) entry(in Entry) error {
	layer, scopeID, err := a.resolve(&in.Scope)
	if err != nil {
		return err
	}
	var item *store.CatalogItem
	for i := range a.items {
		c := a.items[i]
		if c.OrgID != a.org_.ID || c.ArchivedAt != nil || !strings.EqualFold(c.Name, in.Item) || (in.Kind != "" && c.Kind != in.Kind) {
			continue
		}
		if item != nil {
			return errors.New("two items have this name: add kind")
		}
		item = &a.items[i]
	}
	if item == nil {
		return errors.New("no such item in the catalog")
	}
	if !env.FitsLayer(item.OwnerLayer, item.OwnerScope, layer, scopeID) {
		return errors.New("this item belongs to another layer: copy it")
	}
	aud := env.Audience{Mode: env.AudienceTenant, Teams: []string{}}
	switch in.Audience {
	case "", env.AudienceTenant:
	case env.AudienceOff:
		aud.Mode = env.AudienceOff
	default:
		return errors.New("audience is tenant or off")
	}
	audience := env.AudienceJSON(aud)
	enabled := !in.Disabled
	cur, err := a.st.EnvEntryByKey(a.ctx, layer, scopeID, item.ID)
	if store.IsNotFound(err) {
		position := 0
		if item.Kind == store.CatalogScript {
			position = in.Position
			if position == 0 {
				position = a.nextPosition(layer, scopeID) // after the layer's other scripts
			}
		}
		added, ok, err := a.st.AddEnvEntry(a.ctx, store.EnvEntry{OrgID: a.org_.ID, Layer: layer, ScopeID: scopeID, ItemID: item.ID, Enabled: enabled,
			Audience: audience, Pinned: in.Pinned, Rev: 1, UpdatedAt: a.now, Position: position})
		if err != nil || !ok {
			return errors.Join(err, errors.New("entry not added"))
		}
		a.note("+ entry %s «%s» in %s", item.Kind, item.Name, a.where(layer, scopeID))
		return a.journal(layer, scopeID, "env.entry.add", "env_entry", added.ID, fmt.Sprintf("Окружение (из файла): %s «%s» добавлен в %s", item.Kind, item.Name, a.where(layer, scopeID)),
			map[string]any{"layer": layer, "scopeId": scopeID, "itemId": item.ID, "enabled": enabled, "pinned": in.Pinned})
	}
	if err != nil {
		return err
	}
	var what []string
	if cur.Enabled != enabled {
		what, cur.Enabled = append(what, "enabled"), enabled
	}
	if cur.Pinned != in.Pinned {
		what, cur.Pinned = append(what, "pinned"), in.Pinned
	}
	if env.AudienceJSON(env.ParseAudience(cur.Audience)) != audience {
		what, cur.Audience = append(what, "audience"), audience
	}
	if item.Kind == store.CatalogScript && in.Position != 0 && cur.Position != in.Position {
		what, cur.Position = append(what, "position"), in.Position
	}
	if len(what) == 0 {
		return nil
	}
	cur.Rev, cur.UpdatedAt = cur.Rev+1, a.now
	if err := a.st.UpdateEnvEntry(a.ctx, cur); err != nil {
		return err
	}
	a.note("~ entry %s «%s» in %s (%s)", item.Kind, item.Name, a.where(layer, scopeID), strings.Join(what, ", "))
	return a.journal(layer, scopeID, "env.entry.update", "env_entry", cur.ID, fmt.Sprintf("Окружение (из файла): %s «%s» в %s изменён (%s)", item.Kind, item.Name, a.where(layer, scopeID), strings.Join(what, ", ")),
		map[string]any{"layer": layer, "scopeId": scopeID, "itemId": item.ID, "changed": what})
}

// journal writes what the file changed where the API writes its own edits: the organization's list of changes and the
// audit trail, with no actor (nobody signed in; the file is the author).
func (a *applier) journal(layer, scopeID, action, targetKind, targetID, title string, after any) error {
	sc := authz.Scope{Kind: store.ScopeTeam, ID: scopeID}
	switch layer {
	case store.EnvLayerOrg:
		sc.Kind = store.ScopeOrg
	case store.EnvLayerTenant:
		sc.Kind = store.ScopeTenant
	}
	chain, err := orgs.Chain(a.ctx, a.st, a.org_, sc)
	if err != nil {
		return err
	}
	if _, err := a.st.AddChange(a.ctx, store.Change{OrgKey: a.org_.ID, ScopeKind: sc.Kind, ScopeID: sc.ID, Kind: env.ChangeKind, Title: title, At: a.now,
		Source: store.ChangeManual, CreatedAt: a.now}); err != nil {
		return err
	}
	return a.st.AddAudit(a.ctx, orgs.Journal(chain, "", action, targetKind, targetID, nil, after, a.now))
}

// nextPosition is the place after the layer's scripts, the ones just added included.
func (a *applier) nextPosition(layer, scopeID string) int {
	es, err := a.st.EnvEntriesOfOrg(a.ctx, a.org_.ID)
	if err != nil {
		return 1
	}
	max := 0
	for _, e := range es {
		if e.Layer != layer || e.ScopeID != scopeID {
			continue
		}
		if i := slices.IndexFunc(a.items, func(c store.CatalogItem) bool { return c.ID == e.ItemID }); i >= 0 && a.items[i].Kind == store.CatalogScript && e.Position > max {
			max = e.Position
		}
	}
	return max + 1
}

func (a *applier) where(layer, scopeID string) string {
	switch layer {
	case store.EnvLayerOrg:
		return "org " + a.org_.Name
	case store.EnvLayerTenant:
		for slug, sp := range a.tenants {
			if sp.ID == scopeID {
				return "tenant " + slug
			}
		}
	}
	for key, t := range a.teams {
		if t.ID == scopeID {
			return "team " + key
		}
	}
	return layer + " " + scopeID
}

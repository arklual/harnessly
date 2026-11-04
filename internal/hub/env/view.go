package env

import (
	"cmp"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The shapes of the screens: web/app/hub-api/env.ts, docs/hub/environment.md.

// KindCounts is a number per kind of item.
type KindCounts struct {
	Skill int `json:"skill"`
	MCP   int `json:"mcp"`
	Repo  int `json:"repo"`
	Doc   int `json:"doc"`
}

func (c *KindCounts) add(kind string) {
	switch kind {
	case store.CatalogSkill:
		c.Skill++
	case store.CatalogMCP:
		c.MCP++
	case store.CatalogRepo:
		c.Repo++
	case store.CatalogDoc:
		c.Doc++
	}
}

// TopItem is one of the most called items.
type TopItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Summary is the strip of numbers over the layers.
type Summary struct {
	Total       int        `json:"total"`
	ByKind      KindCounts `json:"byKind"`
	CallsWeek   *int64     `json:"callsWeek"`
	Top         []TopItem  `json:"top"`
	Unused      *int       `json:"unused"`
	PinnedByOrg int        `json:"pinnedByOrg"`
}

// AudienceView is who an entry reaches; teams are team ids (the names are in Response.Teams).
type AudienceView struct {
	Mode  string   `json:"mode"`
	Teams []string `json:"teams"`
}

// EntryItem is one row of a layer.
type EntryItem struct {
	EntryID      string       `json:"entryId"`
	ItemID       string       `json:"itemId"`
	Kind         string       `json:"kind"`
	Name         string       `json:"name"`
	Hint         string       `json:"hint"`
	Audience     AudienceView `json:"audience"`
	Pinned       bool         `json:"pinned"`
	Locked       bool         `json:"locked"`
	LockedReason *string      `json:"lockedReason"`
	Enabled      bool         `json:"enabled"`
	CallsWeek    *int64       `json:"callsWeek"`
	Spark14      []int64      `json:"spark14"`
	LastUsedAt   *string      `json:"lastUsedAt"`
	Unused       *bool        `json:"unused"`
	// Delivery is what the machines holding the current list said about the item; nil: none of them reported it.
	Delivery *ItemDelivery `json:"delivery"`
}

// LayerView is one layer with its rows.
type LayerView struct {
	Layer     string      `json:"layer"`
	ScopeID   string      `json:"scopeId"`
	TenantID  string      `json:"tenantId"`
	Title     string      `json:"title"`
	Subtitle  string      `json:"subtitle"`
	Locked    bool        `json:"locked"`
	CallsWeek *int64      `json:"callsWeek"`
	Counts    KindCounts  `json:"counts"`
	Items     []EntryItem `json:"items"`
}

// TeamRef names a team an audience may list.
type TeamRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenantId"`
}

// Response is GET /api/orgs/{org}/env.
type Response struct {
	Summary Summary     `json:"summary"`
	Layers  []LayerView `json:"layers"`
	Teams   []TeamRef   `json:"teams"`
	// Observed is what the machines of the screen's tenants (and team) called in the last month, in the environment or
	// not: the list the environment is filled from.
	Observed []ObservedTool `json:"observed"`
	// Delivery is how many of the screen's machines hold the list.
	Delivery Delivery `json:"delivery"`
}

// ObservedTool is a skill or an MCP server the machines called: Name is as the chats name it (the skill, the MCP
// server), ItemID the catalog item it is, InEnv whether an enabled entry of the screen has the item.
type ObservedTool struct {
	Kind       string  `json:"kind"`
	Name       string  `json:"name"`
	ItemID     *string `json:"itemId"`
	InEnv      bool    `json:"inEnv"`
	Calls      int64   `json:"calls"`
	CallsWeek  int64   `json:"callsWeek"`
	Errors     int64   `json:"errors"`
	People     int     `json:"people"`
	LastUsedAt *string `json:"lastUsedAt"`
	last       int64
}

// Bar is one day of the card's chart.
type Bar struct {
	Day   string `json:"day"`
	Calls int64  `json:"calls"`
}

// Card is GET /api/orgs/{org}/env/items/{entryId}.
type Card struct {
	EntryID      string       `json:"entryId"`
	ItemID       string       `json:"itemId"`
	Kind         string       `json:"kind"`
	Name         string       `json:"name"`
	Hint         string       `json:"hint"`
	Layer        string       `json:"layer"`
	ScopeID      string       `json:"scopeId"`
	Enabled      bool         `json:"enabled"`
	CallsWeek    *int64       `json:"callsWeek"`
	People       *int         `json:"people"`
	PerDay       *float64     `json:"perDay"`
	Bars14       []Bar        `json:"bars14"`
	LastUsedAt   *string      `json:"lastUsedAt"`
	Unused       *bool        `json:"unused"`
	Audience     AudienceView `json:"audience"`
	Pinned       bool         `json:"pinned"`
	Locked       bool         `json:"locked"`
	LockedReason *string      `json:"lockedReason"`
}

func audienceView(raw string) AudienceView {
	a := ParseAudience(raw)
	teams := slices.Clone(a.Teams)
	slices.Sort(teams)
	return AudienceView{Mode: a.Mode, Teams: teams}
}

func msStr(ms int64) *string {
	if ms <= 0 {
		return nil
	}
	v := time.UnixMilli(ms).UTC().Format(time.RFC3339)
	return &v
}

func strp(v string) *string { return &v }

// The reasons a row cannot be changed by the person looking at it.
const (
	reasonPinnedOrg    = "pinned_by_org"
	reasonPinnedTenant = "pinned_by_tenant"
	reasonNoRights     = "no_rights"
)

// lockedReason is why a person cannot change an entry: a layer above pinned the item, or they do not manage its layer.
func lockedReason(canEdit bool, above string, e store.EnvEntry) *string {
	switch {
	case above == store.EnvLayerOrg || (!canEdit && e.Layer == store.EnvLayerOrg && e.Pinned):
		return strp(reasonPinnedOrg)
	case above == store.EnvLayerTenant || (!canEdit && e.Layer == store.EnvLayerTenant && e.Pinned):
		return strp(reasonPinnedTenant)
	case !canEdit:
		return strp(reasonNoRights)
	}
	return nil
}

func sortEntryItems(items []EntryItem) {
	slices.SortFunc(items, func(a, b EntryItem) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name), cmp.Compare(a.EntryID, b.EntryID))
	})
}

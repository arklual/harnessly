// Package env keeps the environment of the chats: a catalog of skills, MCP servers, repositories and documents, what
// each layer of the organization (organization, tenant, team) puts into the environment of its people, what a person
// switched off for themselves, and what each machine holds of it (docs/hub/environment.md).
package env

import (
	"cmp"
	"encoding/json"
	"slices"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Audience modes of an entry.
const (
	AudienceTenant = "tenant" // everybody the layer reaches
	AudienceTeams  = "teams"  // only the people of the listed teams
	AudienceOff    = "off"    // nobody: the entry is kept but takes no part
)

// Audience is who an entry reaches inside its layer.
type Audience struct {
	Mode  string   `json:"mode"`
	Teams []string `json:"teams"`
}

// ParseAudience reads the JSON kept with an entry; whatever is not understood reaches everybody the layer reaches.
func ParseAudience(raw string) Audience {
	var a Audience
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		a = Audience{}
	}
	switch a.Mode {
	case AudienceTenant, AudienceTeams, AudienceOff:
	default:
		a.Mode = AudienceTenant
	}
	if a.Teams == nil {
		a.Teams = []string{}
	}
	return a
}

// Item is a catalog item as the resolver needs it.
type Item struct {
	ID       string
	Kind     string
	Name     string
	Hint     string
	Source   json.RawMessage
	Archived bool
}

// Entry is one entry of a layer as the resolver needs it.
type Entry struct {
	ID       string
	Layer    string
	ScopeID  string
	ItemID   string
	Enabled  bool
	Pinned   bool
	Audience Audience
}

// Person is whom the environment is computed for: where they are and what they switched off.
type Person struct {
	OrgID   string          // "" in a tenant without an organization: there is no organization layer then
	SpaceID string          // the tenant
	Teams   []string        // the teams the person is in, in this tenant
	Off     map[string]bool // item ids the person switched off in this tenant
}

// Effective is one item in the environment of a person.
type Effective struct {
	Item
	Layer    string // the highest layer that puts the item in: org, tenant or team
	EntryID  string // that layer's entry
	Pinned   bool   // some layer pinned it: the person cannot switch it off
	LockedBy string // the highest layer that pinned it, "" when none
	Off      bool   // the person switched it off (and could)
}

// LayerRank orders the layers from the widest.
func LayerRank(layer string) int {
	switch layer {
	case store.EnvLayerOrg:
		return 0
	case store.EnvLayerTenant:
		return 1
	}
	return 2
}

// reaches says whether an entry's scope and audience include the person.
func reaches(e Entry, p Person) bool {
	switch e.Layer {
	case store.EnvLayerOrg:
		if p.OrgID == "" || e.ScopeID != p.OrgID {
			return false
		}
	case store.EnvLayerTenant:
		if e.ScopeID != p.SpaceID {
			return false
		}
	case store.EnvLayerTeam:
		if !slices.Contains(p.Teams, e.ScopeID) {
			return false
		}
		return e.Audience.Mode != AudienceOff
	default:
		return false
	}
	switch e.Audience.Mode {
	case AudienceOff:
		return false
	case AudienceTeams:
		for _, t := range e.Audience.Teams {
			if slices.Contains(p.Teams, t) {
				return true
			}
		}
		return false
	}
	return true
}

// Resolve puts the layers together for one person. An item is in when an entry of some layer puts it in and no lower
// entry switches it off (a disabled entry of a lower layer switches off what the layers above put in, and a still
// lower enabled entry may put it in again); a pinned entry of a layer stops the entries below it, and the person, from
// switching the item off. An archived or unknown item is never in. The result has the items the person switched off
// marked Off, and is ordered by kind, name, id.
func Resolve(items map[string]Item, entries []Entry, p Person) []Effective {
	byItem := map[string][]Entry{}
	for _, e := range entries {
		it, ok := items[e.ItemID]
		if !ok || it.Archived || !reaches(e, p) {
			continue
		}
		byItem[e.ItemID] = append(byItem[e.ItemID], e)
	}
	var out []Effective
	for id, es := range byItem {
		slices.SortStableFunc(es, func(a, b Entry) int { return cmp.Compare(LayerRank(a.Layer), LayerRank(b.Layer)) })
		var eff Effective
		present := false
		for _, e := range es {
			switch {
			case e.Enabled:
				present = true
				if eff.EntryID == "" {
					eff.Layer, eff.EntryID = e.Layer, e.ID
				}
				if e.Pinned && eff.LockedBy == "" {
					eff.Pinned, eff.LockedBy = true, e.Layer
				}
			case eff.LockedBy == "":
				present = false
			}
		}
		if !present {
			continue
		}
		eff.Item = items[id]
		eff.Off = p.Off[id] && !eff.Pinned
		out = append(out, eff)
	}
	slices.SortFunc(out, func(a, b Effective) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// Active leaves out what the person switched off.
func Active(all []Effective) []Effective {
	out := make([]Effective, 0, len(all))
	for _, e := range all {
		if !e.Off {
			out = append(out, e)
		}
	}
	return out
}

// LockedAbove says which layer above (layer, scopeID) pins an item for everybody the layer reaches: "" when none. orgID
// and spaceID place the layer: the organization layer has no layer above it, a tenant has the organization, a team the
// organization and its tenant. Only entries for the whole layer count (audience tenant); one for some teams does not
// lock the others.
func LockedAbove(entries []Entry, layer, orgID, spaceID, itemID string) string {
	best := ""
	bestRank := 3
	for _, e := range entries {
		if e.ItemID != itemID || !e.Enabled || !e.Pinned || e.Audience.Mode != AudienceTenant {
			continue
		}
		r := LayerRank(e.Layer)
		if r >= LayerRank(layer) || r >= bestRank {
			continue
		}
		if (e.Layer == store.EnvLayerOrg && e.ScopeID == orgID && orgID != "") || (e.Layer == store.EnvLayerTenant && e.ScopeID == spaceID) {
			best, bestRank = e.Layer, r
		}
	}
	return best
}

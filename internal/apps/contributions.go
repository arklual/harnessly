package apps

import (
	"sort"
	"strings"
)

// The registry of contributions: everything an installed app puts into the host is one record {owner, kind,
// id, target, spec}. A kind of contribution is described once, in contributionTable: its name, what it is, the targets
// it shows in and how it is read from a manifest. The list of the machine is made from the installed apps, so removing an
// app takes all its records away in one operation (Contributions.Without), and nobody keeps a copy that outlives it.

// Contribution is one thing an app puts into the host.
type Contribution struct {
	// App is the owner: the app's id.
	App string `json:"app"`
	// Kind is the kind of contribution (view, chat.command, sinopia.panel…).
	Kind string `json:"kind"`
	// ID names the contribution inside its app and kind.
	ID string `json:"id"`
	// Target is where it shows: a target of the surface (chat, pane, settings, sinopia.<kind>…).
	Target string `json:"target"`
	// Spec is the part of the manifest it was read from.
	Spec any `json:"spec,omitempty"`
}

// ContributionKind is a kind of contribution as the catalog gives it.
type ContributionKind struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	// Field is where the manifest declares it.
	Field string `json:"field"`
	// Targets are the targets it can show in.
	Targets []string `json:"targets"`
}

type contributionReader struct {
	ContributionKind
	read func(m *Manifest) []Contribution
}

// contributionTable is the one place a kind of contribution is added; the reader and the implementor on the page are
// the rest of it.
var contributionTable = []contributionReader{
	{ContributionKind{Name: "view", Field: "views[]", Doc: "A page of the app in a frame.",
		Targets: []string{TargetPane, TargetApp, TargetChat, TargetSettings, TargetOnboarding, TargetObjectAction, TargetHubSettings, "sinopia.<kind>"}},
		func(m *Manifest) []Contribution {
			var out []Contribution
			for _, v := range m.Views {
				for _, t := range v.Targets {
					out = append(out, Contribution{Kind: "view", ID: v.ID, Target: t, Spec: v})
				}
			}
			return out
		}},
	{ContributionKind{Name: "chat.command", Field: "chat.commands[]", Doc: "A /-command of the chat's palette.", Targets: []string{TargetChat}},
		func(m *Manifest) []Contribution {
			if m.Chat == nil {
				return nil
			}
			var out []Contribution
			for _, c := range m.Chat.Commands {
				out = append(out, Contribution{Kind: "chat.command", ID: c, Target: TargetChat, Spec: c})
			}
			return out
		}},
	{ContributionKind{Name: "chat.card", Field: "chat.cards[]", Doc: "A view of the app the chat's feed shows as a card.", Targets: []string{TargetChat}},
		func(m *Manifest) []Contribution {
			if m.Chat == nil {
				return nil
			}
			var out []Contribution
			for _, c := range m.Chat.Cards {
				out = append(out, Contribution{Kind: "chat.card", ID: c, Target: TargetChat, Spec: c})
			}
			return out
		}},
	{ContributionKind{Name: "agent", Field: "agent", Doc: "The agent profile: instructions, skills, MCP servers, tool rules, validators.",
		Targets: []string{TargetChat, TargetPane, "sinopia.<kind>"}},
		func(m *Manifest) []Contribution {
			if m.Agent == nil {
				return nil
			}
			var out []Contribution
			for _, t := range m.Targets {
				if t == TargetChat || t == TargetPane || strings.HasPrefix(t, "sinopia.") {
					out = append(out, Contribution{Kind: "agent", ID: "agent", Target: t, Spec: m.Agent})
				}
			}
			return out
		}},
	{ContributionKind{Name: "branding", Field: "branding", Doc: "The product's name and accent.", Targets: []string{TargetBranding}},
		func(m *Manifest) []Contribution {
			if m.Branding == nil {
				return nil
			}
			return []Contribution{{Kind: "branding", ID: "branding", Target: TargetBranding, Spec: m.Branding}}
		}},
	{ContributionKind{Name: "sinopia.kind", Field: "sinopia.kinds[]", Doc: "A Sinopia document type of the app's own.", Targets: []string{"sinopia.<kind>"}},
		func(m *Manifest) []Contribution {
			s, err := m.SinopiaOf()
			if err != nil {
				return nil
			}
			var out []Contribution
			for _, k := range s.Kinds {
				out = append(out, Contribution{Kind: "sinopia.kind", ID: k.ID, Target: "sinopia." + k.ID, Spec: k})
			}
			return out
		}},
	{ContributionKind{Name: "sinopia.panel", Field: "sinopia.panels[]", Doc: "A view to the right of the editor.", Targets: []string{"sinopia.<kind>"}},
		sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
			for _, p := range s.Panels {
				add(p.ID, p.Kinds, p)
			}
		}, "sinopia.panel")},
	{ContributionKind{Name: "sinopia.selection", Field: "sinopia.selection[]", Doc: "An item of the selection menu.", Targets: []string{"sinopia.<kind>"}},
		sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
			for _, p := range s.Selection {
				add(p.ID, p.Kinds, p)
			}
		}, "sinopia.selection")},
	{ContributionKind{Name: "sinopia.check", Field: "sinopia.checks[]", Doc: "A check of the document.", Targets: []string{"sinopia.<kind>"}},
		sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
			for _, p := range s.Checks {
				add(p.ID, p.Kinds, p)
			}
		}, "sinopia.check")},
	{ContributionKind{Name: "sinopia.export", Field: "sinopia.export[]", Doc: "An export entry of the document.", Targets: []string{"sinopia.<kind>"}},
		sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
			for _, p := range s.Export {
				add(p.ID, p.Kinds, p)
			}
		}, "sinopia.export")},
	{ContributionKind{Name: "sinopia.import", Field: "sinopia.import[]", Doc: "An import entry of the document.", Targets: []string{"sinopia.<kind>"}},
		sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
			for _, p := range s.Import {
				add(p.ID, nil, p)
			}
		}, "sinopia.import")},
}

// sinopiaPart reads one list of the manifest's `sinopia`: an item for listed kinds shows in each, one for all of the
// app's kinds shows in the app's sinopia targets.
func sinopiaPart(each func(s Sinopia, add func(id string, kinds []string, spec any)), kind string) func(m *Manifest) []Contribution {
	return func(m *Manifest) []Contribution {
		s, err := m.SinopiaOf()
		if err != nil {
			return nil
		}
		var out []Contribution
		each(s, func(id string, kinds []string, spec any) {
			targets := []string{}
			for _, k := range kinds {
				targets = append(targets, "sinopia."+k)
			}
			if len(kinds) == 0 {
				for _, t := range m.Targets {
					if strings.HasPrefix(t, "sinopia.") {
						targets = append(targets, t)
					}
				}
			}
			for _, t := range targets {
				out = append(out, Contribution{Kind: kind, ID: id, Target: t, Spec: spec})
			}
		})
		return out
	}
}

func contributionKinds() []ContributionKind {
	out := make([]ContributionKind, len(contributionTable))
	for i, k := range contributionTable {
		out[i] = k.ContributionKind
	}
	return out
}

// ContributionsOf are the records of one manifest.
func ContributionsOf(m *Manifest) []Contribution {
	if m == nil {
		return nil
	}
	var out []Contribution
	for _, k := range contributionTable {
		for _, c := range k.read(m) {
			c.App = m.ID
			out = append(out, c)
		}
	}
	return out
}

// Contributions is the registry of a machine: the records of every app that is on (installed and not muted).
type Contributions struct {
	list []Contribution
}

// NewContributions makes the registry of a list of installs.
func NewContributions(installed []Installed) *Contributions {
	c := &Contributions{}
	for _, in := range installed {
		if in.Muted {
			continue
		}
		c.list = append(c.list, ContributionsOf(in.Manifest)...)
	}
	sort.SliceStable(c.list, func(i, j int) bool {
		a, b := c.list[i], c.list[j]
		if a.App != b.App {
			return a.App < b.App
		}
		return a.Kind < b.Kind
	})
	return c
}

// All are the records.
func (c *Contributions) All() []Contribution { return append([]Contribution(nil), c.list...) }

// In are the records that show in a target; a record of sinopia.* shows in every sinopia.<kind>.
func (c *Contributions) In(target string) []Contribution {
	var out []Contribution
	for _, r := range c.list {
		if r.Target == target || (r.Target == "sinopia.*" && wildcardCovers(target)) {
			out = append(out, r)
		}
	}
	return out
}

// Of are the records of a kind.
func (c *Contributions) Of(kind string) []Contribution {
	var out []Contribution
	for _, r := range c.list {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// Without is the registry with every record of an app taken away at once.
func (c *Contributions) Without(app string) *Contributions {
	out := &Contributions{}
	for _, r := range c.list {
		if r.App != app {
			out.list = append(out.list, r)
		}
	}
	return out
}

// Contributions is the registry of the apps installed on this machine.
func (r *Registry) Contributions() (*Contributions, error) {
	list, err := r.List()
	if err != nil {
		return nil, err
	}
	return NewContributions(list), nil
}

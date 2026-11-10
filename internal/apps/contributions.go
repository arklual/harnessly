package apps

import (
	"sort"
	"strconv"
	"strings"
)

// The registry of contributions (HRN-691): everything an installed app puts into the host is one record {owner, kind,
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
	// Owner is the app whose point the record goes into (harnessly.sinopia for sinopia.panel); "" for the host's own.
	Owner string `json:"owner,omitempty"`
}

// ContributionKind is a kind of contribution as the catalog gives it.
type ContributionKind struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	// Field is where the manifest declares it.
	Field string `json:"field"`
	// Targets are the targets it can show in.
	Targets []string `json:"targets"`
	// Owner is the app that declares the point (builtin.go); "" for a kind of the host's own.
	Owner string `json:"owner,omitempty"`
}

type contributionReader struct {
	ContributionKind
	read func(m *Manifest) []Contribution
}

// contributionTable is the one place a kind of contribution of the host's own is added; the reader and the implementor
// on the page are the rest of it. The points of a builtin app are in its manifest, and their readers are its code
// (pointReaders, builtin.go).
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
	{ContributionKind{Name: "chat.starter", Field: "chat.starters[]", Doc: "A line an empty chat offers under its field; a click puts it into the field.", Targets: []string{TargetChat}},
		func(m *Manifest) []Contribution {
			if m.Chat == nil {
				return nil
			}
			var out []Contribution
			for i, st := range m.Chat.Starters {
				out = append(out, Contribution{Kind: "chat.starter", ID: strconv.Itoa(i), Target: TargetChat, Spec: st})
			}
			return out
		}},
	{ContributionKind{Name: "chat.button", Field: "chat.buttons[]", Doc: "A button at the chat's field: runs one of the app's commands or opens its view in the pane.", Targets: []string{TargetChat}},
		func(m *Manifest) []Contribution {
			if m.Chat == nil {
				return nil
			}
			var out []Contribution
			for _, b := range m.Chat.Buttons {
				out = append(out, Contribution{Kind: "chat.button", ID: b.ID, Target: TargetChat, Spec: b})
			}
			return out
		}},
	{ContributionKind{Name: "chat.link", Field: "chat.links[]", Doc: "A kind of address the app shows a card for under a message that has such a link.", Targets: []string{TargetChat}},
		func(m *Manifest) []Contribution {
			if m.Chat == nil {
				return nil
			}
			var out []Contribution
			for _, l := range m.Chat.Links {
				out = append(out, Contribution{Kind: "chat.link", ID: l.Match, Target: TargetChat, Spec: l})
			}
			return out
		}},
	{ContributionKind{Name: "chat.mention", Field: "chat.mentions", Doc: "What «@» in the chat's field finds in the app: a tool of its MCP server, asked with what is typed.", Targets: []string{TargetChat}},
		func(m *Manifest) []Contribution {
			if m.Chat == nil || m.Chat.Mentions == nil {
				return nil
			}
			return []Contribution{{Kind: "chat.mention", ID: m.Chat.Mentions.Tool, Target: TargetChat, Spec: *m.Chat.Mentions}}
		}},
	{ContributionKind{Name: "agent", Field: "agent", Doc: "The agent profile: instructions, skills, MCP servers, tool rules, validators.",
		Targets: []string{TargetChat, TargetPane, "sinopia.<kind>"}},
		func(m *Manifest) []Contribution {
			if m.Agent == nil {
				return nil
			}
			var out []Contribution
			for _, t := range m.Targets {
				if t == TargetChat || t == TargetPane || IsSinopiaTarget(t) {
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
}

// contributionKinds are the host's kinds and then the points the builtin apps declare (builtin.go), each shown in the
// families of its owner's aliases.
func contributionKinds() []ContributionKind {
	out := make([]ContributionKind, 0, len(contributionTable))
	for _, k := range contributionTable {
		out = append(out, k.ContributionKind)
	}
	for _, p := range AppPoints() {
		out = append(out, ContributionKind{Name: p.Name, Doc: p.Doc, Field: p.Field, Targets: familiesOf(p.Owner), Owner: p.Owner})
	}
	return out
}

// familiesOf are the aliases of an app's screen that name one scope (sinopia.<kind>), not its wildcard.
func familiesOf(app string) []string {
	m := builtinOf(app)
	if m == nil {
		return nil
	}
	var out []string
	for _, a := range m.Aliases {
		if !strings.HasSuffix(a.Name, ".*") {
			out = append(out, a.Name)
		}
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
	// The contributions to the builtin apps' points, read by their owners' code.
	for _, p := range AppPoints() {
		read := pointReaders[p.Name]
		if read == nil {
			continue
		}
		for _, c := range read(m) {
			c.App, c.Kind, c.Owner = m.ID, p.Name, p.Owner
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
		if r.Target == target || (r.Target != "" && r.Target == wildcardOf(target)) {
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

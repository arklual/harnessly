package hlcli

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// The canvas of a kit for an agent: `design kit search` looks through the index the kit screen's search
// uses, `design kit show` gives one node with what an agent needs to use it. An agent picks a layout from the catalogue
// (purpose, slots) instead of opening a hundred boards.

func (c *cli) kitCanvas(o kitOptions, id string) (*designkit.Canvas, error) {
	version := o.version
	if version == "" {
		for _, k := range designkit.List(o.dir) {
			if k.ID == id && len(k.Versions) > 0 {
				version = k.Versions[0].Version
			}
		}
	}
	if version == "" {
		return nil, usageErr("kit %q is not installed in %s", id, o.dir)
	}
	cv, err := designkit.ReadCanvas(o.dir, id, version, o.theme)
	if err != nil {
		return nil, usageErr("kit %s %s: %v", id, version, err)
	}
	return cv, nil
}

func (c *cli) kitSearch(o kitOptions, pos []string) error {
	if len(pos) < 2 {
		return usageErr("design kit search takes a kit and words")
	}
	cv, err := c.kitCanvas(o, pos[0])
	if err != nil {
		return err
	}
	hits := designkit.Search(cv.Index, strings.Join(pos[1:], " "), 40)
	if c.json {
		if hits == nil {
			hits = []designkit.IndexEntry{}
		}
		raw, _ := json.Marshal(struct {
			Kit     string                 `json:"kit"`
			Version string                 `json:"version"`
			Theme   string                 `json:"theme"`
			Results []designkit.IndexEntry `json:"results"`
		}{cv.Kit, cv.Version, cv.Theme, hits})
		c.out("%s\n", raw)
		return nil
	}
	if len(hits) == 0 {
		c.out("nothing found in %s %s (%s)\n", cv.Kit, cv.Version, cv.Theme)
		return nil
	}
	for _, h := range hits {
		c.out("%s\t%s\t%s\t%s\n", h.Node, h.Kind, h.Title, h.Path)
	}
	return nil
}

// KitNode is one node of a canvas as `design kit show` prints it.
type kitNode struct {
	Node  string `json:"node"`
	Kind  string `json:"kind"`
	Theme string `json:"theme"`
	Value any    `json:"value"`
	// Call is how a board uses it, when it is a component, a layout or an asset with a component.
	Call string `json:"call,omitempty"`
	// Address is how a deck takes a layout (`harnessly design page add --layout`).
	Address string `json:"address,omitempty"`
}

func xImport(ns, comp string, props map[string]any) string {
	var b strings.Builder
	b.WriteString(`<x-import component-from-global-scope="` + ns + "." + comp + `"`)
	for _, k := range sortedKeys(props) {
		if s, ok := props[k].(string); ok {
			b.WriteString(" " + k + `="` + s + `"`)
		}
	}
	b.WriteString("></x-import>")
	return b.String()
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func findNode(cv *designkit.Canvas, node string) *kitNode {
	n := &kitNode{Node: node, Theme: cv.Theme}
	for _, g := range cv.Groups {
		for _, k := range g.Components {
			if k.NodeID == node {
				n.Kind, n.Value, n.Call = designkit.KindComponent, k, xImport(cv.Namespace, k.ID, k.Sample)
				return n
			}
		}
	}
	for _, t := range cv.Templates {
		if t.NodeID == node {
			n.Kind, n.Value = designkit.KindTemplate, t
			return n
		}
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				if l.NodeID == node {
					n.Kind, n.Value, n.Address = designkit.KindLayout, l, l.Address
					if t.Component != "" {
						n.Call = xImport(cv.Namespace, t.Component, l.Props)
					}
					return n
				}
			}
		}
	}
	for _, g := range cv.Assets {
		for _, a := range g.Items {
			if a.NodeID == node {
				n.Kind, n.Value = designkit.KindAsset, a
				if g.Component != "" && g.Prop != "" {
					n.Call = xImport(cv.Namespace, g.Component, map[string]any{g.Prop: a.Name})
				}
				return n
			}
		}
	}
	for _, col := range cv.Colors {
		if col.ID == node {
			n.Kind, n.Value = designkit.KindColor, col
			return n
		}
	}
	for _, s := range cv.Type {
		if s.ID == node {
			n.Kind, n.Value = designkit.KindType, s
			return n
		}
	}
	for _, t := range cv.Tokens {
		if t.ID == node {
			n.Kind, n.Value = designkit.KindToken, t
			return n
		}
	}
	return nil
}

func (c *cli) kitShow(o kitOptions, pos []string) error {
	if len(pos) != 2 {
		return usageErr("design kit show takes a kit and a node (from design kit search)")
	}
	cv, err := c.kitCanvas(o, pos[0])
	if err != nil {
		return err
	}
	n := findNode(cv, pos[1])
	if n == nil {
		return usageErr("no node %q in %s %s (%s)", pos[1], cv.Kit, cv.Version, cv.Theme)
	}
	raw, _ := json.MarshalIndent(n, "", "  ")
	c.out("%s\n", raw)
	return nil
}

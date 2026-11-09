package designkit

// The canvas of a kit: what Sinopia's kit screen draws (the tree, the canvas, the search) and what an agent searches. A
// theme of a kit describes itself in its design-system.json (Claude Design's format, v3) with four fields the kit's own
// build fills: sections (the tree's sections), groups (components with props and variants), assetGroups (icons, art,
// logos by name) and templates (a template's sections → its layouts with slots and a picture). The foundation (colours,
// type, radii) is the theme's tokens.json. A kit without these fields has no canvas and is shown by its boards as before.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The canvas's sections, in the tree's order.
const (
	SectionFoundation = "foundation"
	SectionComponents = "components"
	SectionTemplates  = "templates"
	SectionAssets     = "assets"
	// SectionReference holds a template's own guide (how to use it, palettes, icon libraries): shown apart from the
	// templates so they stay clean.
	SectionReference = "reference"
)

// hexColor is the only form of colour a kit may give an asset's background.
var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{3,8}$`)

var sectionOrder = []string{SectionFoundation, SectionComponents, SectionTemplates, SectionAssets, SectionReference}

// ErrNoCanvas: the theme does not describe a canvas (an older kit), or the kit, version or theme is not there.
var ErrNoCanvas = errors.New("the kit has no canvas")

// CanvasTheme is one theme of a kit.
type CanvasTheme struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// CanvasSection is a section of the tree; Title is the kit's own name for it, "" for the screen's default.
type CanvasSection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ColorToken is a colour of the foundation; Values holds a value per mode («light», «dark») or one under "".
type ColorToken struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
	Usage  string            `json:"usage,omitempty"`
}

// TypeStyle is a text style of the foundation.
type TypeStyle struct {
	ID     string `json:"id"`
	Group  string `json:"group"`
	Name   string `json:"name"`
	Family string `json:"family"`
	Size   string `json:"size"`
	Line   string `json:"line,omitempty"`
	Weight string `json:"weight,omitempty"`
}

// FontFace is a font file of the theme (a path relative to its folder).
type FontFace struct {
	Family string `json:"family"`
	File   string `json:"file"`
	Weight string `json:"weight,omitempty"`
}

// PlainToken is any other token of the foundation (a radius, a spacing, a shadow).
type PlainToken struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Usage string `json:"usage,omitempty"`
}

// Prop is a property of a component.
type Prop struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Values      []string `json:"values,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Variant is a named set of a component's props.
type Variant struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Props map[string]any `json:"props,omitempty"`
}

// Component is a component of the kit.
type Component struct {
	ID          string         `json:"id"`
	NodeID      string         `json:"node"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Props       []Prop         `json:"props"`
	Variants    []Variant      `json:"variants"`
	Sample      map[string]any `json:"sample,omitempty"`
	Preview     string         `json:"preview,omitempty"`
	// Thumb is the component's picture (a path in the theme), drawn on the canvas when it is not live; Cell is its place
	// in the group's atlas, -1 without one.
	Thumb string `json:"thumb,omitempty"`
	Cell  int    `json:"cell"`
	// Part is what the component is a part of: another component's id (TableRow of Table), or PartProvider for a
	// provider that draws nothing itself. A part is not a tile of the canvas; it shows in its parent's panel.
	Part string `json:"part,omitempty"`
	// Keywords are the words people look for it by besides its name and description (synonyms, tasks).
	Keywords []string `json:"keywords,omitempty"`
}

// PartProvider marks a component that only provides context (a theme, a store) and draws nothing.
const PartProvider = "provider"

// ComponentGroup is a group of components.
type ComponentGroup struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Components []Component `json:"components"`
	Atlas      *Atlas      `json:"atlas,omitempty"`
}

// Asset is a named file of an asset group, relative to the theme's folder; Dark is its variant for the dark mode.
type Asset struct {
	NodeID string `json:"node"`
	Name   string `json:"name"`
	File   string `json:"file"`
	Dark   string `json:"dark,omitempty"`
	// Bg is the colour the asset is meant to sit on (a white logo on a dark one), "" for any.
	Bg string `json:"bg,omitempty"`
	// Cell is the asset's place in its group's atlas, -1 when the atlas does not hold it (a vector file).
	Cell int `json:"cell"`
}

// Atlas is one picture holding many small ones in a grid of cells, row by row: the far view loads it instead of
// hundreds of files. File is a path under the version's prepared folder.
type Atlas struct {
	File  string `json:"file"`
	CellW int    `json:"cellW"`
	CellH int    `json:"cellH"`
	Cols  int    `json:"cols"`
	Count int    `json:"count"`
}

// AssetGroup is a group of assets (icons, art, logos).
type AssetGroup struct {
	ID        string  `json:"id"`
	NodeID    string  `json:"node"`
	Title     string  `json:"title"`
	Kind      string  `json:"kind"`
	Component string  `json:"component,omitempty"`
	Prop      string  `json:"prop,omitempty"`
	Items     []Asset `json:"items"`
	Atlas     *Atlas  `json:"atlas,omitempty"`
}

// Slot is what an agent fills in a layout: a text (with the sample the template shows), an image, a chart.
type Slot struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// Label is what the field is called to a person («Заголовок», «Картинка 2»); "" falls back to the key.
	Label  string `json:"label,omitempty"`
	Sample string `json:"sample,omitempty"`
}

// TemplateLayout is a layout of a template: its props for the template's component, the scripts it needs, its board
// and its picture (paths relative to the theme's folder).
type TemplateLayout struct {
	ID      string         `json:"id"`
	NodeID  string         `json:"node"`
	Name    string         `json:"name"`
	Purpose string         `json:"purpose,omitempty"`
	Props   map[string]any `json:"props,omitempty"`
	Scripts []string       `json:"scripts,omitempty"`
	Board   string         `json:"board,omitempty"`
	Thumb   string         `json:"thumb,omitempty"`
	Slots   []Slot         `json:"slots"`
	// Cell is the layout's place in its template's atlas, -1 without a picture.
	Cell int `json:"cell"`
	// Address is how a deck takes the layout (`<kit>/<theme>/<layout>`), "" when the theme does not ship its board.
	Address string `json:"address,omitempty"`
}

// TemplateSection is a section of a template.
type TemplateSection struct {
	ID      string           `json:"id"`
	NodeID  string           `json:"node"`
	Title   string           `json:"title"`
	Layouts []TemplateLayout `json:"layouts"`
}

// Template is a template (a deck's master): its sections of layouts.
type Template struct {
	ID     string `json:"id"`
	NodeID string `json:"node"`
	// Section is the tree section the template lives in: templates, or reference for a template's guide.
	Section string `json:"section"`
	// Blank: its layouts are a master's empty layouts (background and placeholders), not counted as templates.
	Blank       bool              `json:"blank,omitempty"`
	Title       string            `json:"title"`
	Kind        string            `json:"kind"`
	Aspect      [2]int            `json:"aspect"`
	Component   string            `json:"component,omitempty"`
	Cover       string            `json:"cover,omitempty"`
	Description string            `json:"description,omitempty"`
	Sections    []TemplateSection `json:"sections"`
	Atlas       *Atlas            `json:"atlas,omitempty"`
}

// Canvas is a theme of an installed kit version as the kit screen draws it.
type Canvas struct {
	Kit       string        `json:"kit"`
	Version   string        `json:"version"`
	Versions  []string      `json:"versions"`
	Title     string        `json:"title"`
	Theme     string        `json:"theme"`
	Themes    []CanvasTheme `json:"themes"`
	Namespace string        `json:"namespace"`
	// Root is the provider component every board on the kit wraps its content in (the kit record's root), "" for none.
	Root string `json:"root,omitempty"`
	// Base is the theme's folder in the version (slash path, "" for a kit that is one theme).
	Base       string           `json:"base"`
	Sections   []CanvasSection  `json:"sections"`
	Modes      []string         `json:"modes"`
	Colors     []ColorToken     `json:"colors"`
	Type       []TypeStyle      `json:"type"`
	Fonts      []FontFace       `json:"fonts"`
	Tokens     []PlainToken     `json:"tokens"`
	Groups     []ComponentGroup `json:"groups"`
	Assets     []AssetGroup     `json:"assets"`
	Templates  []Template       `json:"templates"`
	Index      []IndexEntry     `json:"index"`
	PreparedAt int64            `json:"preparedAt"`
}

// kitTheme is a theme as kit.json names it.
type kitTheme struct {
	ID        string `json:"id"`
	Folder    string `json:"folder"`
	Namespace string `json:"namespace"`
	Title     string `json:"title"`
}

type kitFile struct {
	Title  string     `json:"title"`
	Themes []kitTheme `json:"themes"`
}

// rawManifest is the part of design-system.json the canvas reads.
type rawManifest struct {
	Title     string                     `json:"title"`
	Namespace string                     `json:"namespace"`
	Sections  map[string]json.RawMessage `json:"sections"`
	Groups    []struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Components []struct {
			ID          string          `json:"id"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Props       []Prop          `json:"props"`
			Variants    []Variant       `json:"variants"`
			Sample      map[string]any  `json:"sample"`
			Preview     string          `json:"preview"`
			Thumb       string          `json:"thumb"`
			Part        json.RawMessage `json:"part"`
			Keywords    []string        `json:"keywords"`
		} `json:"components"`
	} `json:"groups"`
	AssetGroups map[string]struct {
		Title     string  `json:"title"`
		Kind      string  `json:"kind"`
		Component string  `json:"component"`
		Prop      string  `json:"prop"`
		Order     int     `json:"order"`
		Items     []Asset `json:"items"`
	} `json:"assetGroups"`
	Templates []struct {
		ID          string `json:"id"`
		Section     string `json:"section"`
		Blank       bool   `json:"blank"`
		Title       string `json:"title"`
		Kind        string `json:"kind"`
		Aspect      []int  `json:"aspect"`
		Component   string `json:"component"`
		Cover       string `json:"cover"`
		Description string `json:"description"`
		Sections    []struct {
			ID      string           `json:"id"`
			Title   string           `json:"title"`
			Layouts []TemplateLayout `json:"layouts"`
		} `json:"sections"`
	} `json:"templates"`
}

// Themes lists the themes of an installed version: kit.json's, or the version itself as one theme ("") when it keeps
// its design-system.json at its root.
func Themes(dir, id, version string) []kitTheme {
	root := Resolve(dir, id, version, nil)
	if root == "" {
		return nil
	}
	var kf kitFile
	if raw, err := readLimited(filepath.Join(root, "kit.json"), 1<<20); err == nil {
		_ = json.Unmarshal(raw, &kf)
	}
	var out []kitTheme
	for _, t := range kf.Themes {
		folder := path.Clean(strings.TrimPrefix(t.Folder, "/"))
		if !ValidID(t.ID) || folder == "." || strings.HasPrefix(folder, "..") {
			continue
		}
		t.Folder = folder
		if Resolve(dir, id, version, append(strings.Split(folder, "/"), ManifestFile)) != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 && Resolve(dir, id, version, []string{ManifestFile}) != "" {
		out = append(out, kitTheme{ID: "default"})
	}
	return out
}

func kitTitle(dir, id, version string) string {
	root := Resolve(dir, id, version, nil)
	var kf kitFile
	if raw, err := readLimited(filepath.Join(root, "kit.json"), 1<<20); err == nil {
		_ = json.Unmarshal(raw, &kf)
	}
	return kf.Title
}

func readThemeFile(dir, id, version, folder, name string) ([]byte, error) {
	segs := []string{name}
	if folder != "" {
		segs = append(strings.Split(folder, "/"), name)
	}
	p := Resolve(dir, id, version, segs)
	if p == "" {
		return nil, os.ErrNotExist
	}
	return readLimited(p, 32<<20)
}

// HasCanvas is whether a theme's manifest describes a canvas: any of its groups, asset groups or templates.
func (m *rawManifest) hasCanvas() bool {
	return len(m.Groups) > 0 || len(m.AssetGroups) > 0 || len(m.Templates) > 0
}

// ReadCanvas reads a theme of an installed version ("" is its first theme) as the kit screen draws it, without the
// prepared atlases (Prepare adds them).
func ReadCanvas(dir, id, version, theme string) (*Canvas, error) {
	themes := Themes(dir, id, version)
	if len(themes) == 0 {
		return nil, ErrNoCanvas
	}
	th := themes[0]
	if theme != "" {
		found := false
		for _, t := range themes {
			if t.ID == theme {
				th, found = t, true
			}
		}
		if !found {
			return nil, ErrNoCanvas
		}
	}
	raw, err := readThemeFile(dir, id, version, th.Folder, ManifestFile)
	if err != nil {
		return nil, ErrNoCanvas
	}
	var m rawManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if !m.hasCanvas() {
		return nil, ErrNoCanvas
	}
	c := &Canvas{Kit: id, Version: version, Theme: th.ID, Namespace: firstNonEmpty(m.Namespace, th.Namespace), Base: th.Folder,
		Title: firstNonEmpty(kitTitle(dir, id, version), m.Title, id)}
	for _, v := range listVersions(dir, id) {
		c.Versions = append(c.Versions, v.Version)
	}
	for _, t := range themes {
		title := t.Title
		if title == "" {
			if raw, err := readThemeFile(dir, id, version, t.Folder, ManifestFile); err == nil {
				var tm rawManifest
				if json.Unmarshal(raw, &tm) == nil {
					title = tm.Title
				}
			}
		}
		c.Themes = append(c.Themes, CanvasTheme{ID: t.ID, Title: firstNonEmpty(title, t.ID)})
	}
	c.Root = kitRoot(raw)
	if tokens, err := readThemeFile(dir, id, version, th.Folder, "tokens.json"); err == nil {
		readFoundation(c, tokens)
	}
	readComponents(c, &m)
	readAssets(c, &m)
	readTemplates(c, &m, dir, id, version)
	readSections(c, &m)
	c.Index = buildIndex(c)
	return c, nil
}

func readSections(c *Canvas, m *rawManifest) {
	has := map[string]bool{
		SectionFoundation: len(c.Colors)+len(c.Type)+len(c.Tokens) > 0,
		SectionComponents: len(c.Groups) > 0,
		SectionAssets:     len(c.Assets) > 0,
	}
	for _, t := range c.Templates {
		has[t.Section] = true
	}
	for _, id := range sectionOrder {
		if !has[id] {
			continue
		}
		title := ""
		if raw, ok := m.Sections[id]; ok {
			var s struct {
				Title string `json:"title"`
			}
			if json.Unmarshal(raw, &s) == nil && s.Title != "" {
				title = s.Title
			}
		}
		c.Sections = append(c.Sections, CanvasSection{ID: id, Title: title})
	}
}

// tokensFile is the part of tokens.json (Claude Design's lists) the foundation reads.
type tokensFile struct {
	Color struct {
		Themes []struct {
			ID string `json:"id"`
		} `json:"themes"`
		Tokens []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
			Usage string          `json:"usage"`
		} `json:"tokens"`
	} `json:"color"`
	Type struct {
		Fonts []struct {
			Family string          `json:"family"`
			File   string          `json:"file"`
			Weight json.RawMessage `json:"weight"`
		} `json:"fonts"`
		Families map[string]string `json:"families"`
		Groups   []struct {
			Name   string `json:"name"`
			Family string `json:"family"`
			Styles []struct {
				Name       string          `json:"name"`
				FontSize   string          `json:"fontSize"`
				LineHeight string          `json:"lineHeight"`
				FontWeight json.RawMessage `json:"fontWeight"`
				Family     string          `json:"family"`
			} `json:"styles"`
		} `json:"groups"`
	} `json:"type"`
}

func rawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func readFoundation(c *Canvas, raw []byte) {
	var t tokensFile
	if json.Unmarshal(raw, &t) != nil {
		return
	}
	for _, th := range t.Color.Themes {
		c.Modes = append(c.Modes, th.ID)
	}
	for _, tok := range t.Color.Tokens {
		values := map[string]string{}
		var byMode map[string]string
		if json.Unmarshal(tok.Value, &byMode) == nil {
			for k, v := range byMode {
				values[k] = v
			}
		} else if s := rawString(tok.Value); s != "" {
			values[""] = s
		}
		if tok.Name == "" || len(values) == 0 {
			continue
		}
		c.Colors = append(c.Colors, ColorToken{ID: "color:" + tok.Name, Name: tok.Name, Values: values, Usage: tok.Usage})
	}
	for _, f := range t.Type.Fonts {
		if file := cleanRel(f.File); f.Family != "" && file != "" {
			c.Fonts = append(c.Fonts, FontFace{Family: f.Family, File: file, Weight: rawString(f.Weight)})
		}
	}
	for _, g := range t.Type.Groups {
		for _, s := range g.Styles {
			fam := firstNonEmpty(s.Family, g.Family)
			if f, ok := t.Type.Families[fam]; ok {
				fam = f
			}
			c.Type = append(c.Type, TypeStyle{ID: "type:" + g.Name + "/" + s.Name, Group: g.Name, Name: s.Name, Family: fam,
				Size: s.FontSize, Line: s.LineHeight, Weight: rawString(s.FontWeight)})
		}
	}
	// Any other list of tokens (radius, spacing, shadow…): {tokens: [{name, value, usage}]} with a plain value.
	var all map[string]json.RawMessage
	if json.Unmarshal(raw, &all) != nil {
		return
	}
	kinds := make([]string, 0, len(all))
	for k := range all {
		if k != "color" && k != "type" {
			kinds = append(kinds, k)
		}
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		var list struct {
			Tokens []struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
				Usage string          `json:"usage"`
			} `json:"tokens"`
		}
		if json.Unmarshal(all[kind], &list) != nil {
			continue
		}
		for _, tok := range list.Tokens {
			if v := rawString(tok.Value); tok.Name != "" && v != "" {
				c.Tokens = append(c.Tokens, PlainToken{ID: "token:" + tok.Name, Kind: kind, Name: tok.Name, Value: v, Usage: tok.Usage})
			}
		}
	}
}

func cleanRel(p string) string {
	p = path.Clean(strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "/"))
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func readComponents(c *Canvas, m *rawManifest) {
	for _, g := range m.Groups {
		if g.ID == "" {
			continue
		}
		out := ComponentGroup{ID: g.ID, Title: firstNonEmpty(g.Title, g.ID)}
		for _, k := range g.Components {
			id := firstNonEmpty(k.ID, k.Name)
			if id == "" {
				continue
			}
			comp := Component{ID: id, NodeID: "component:" + id, Name: firstNonEmpty(k.Name, id), Description: k.Description,
				Props: k.Props, Variants: k.Variants, Sample: k.Sample, Preview: cleanRel(k.Preview), Thumb: cleanRel(k.Thumb),
				Cell: -1, Part: partOf(k.Part), Keywords: k.Keywords}
			if comp.Props == nil {
				comp.Props = []Prop{}
			}
			if comp.Variants == nil {
				comp.Variants = []Variant{}
			}
			out.Components = append(out.Components, comp)
		}
		if len(out.Components) > 0 {
			c.Groups = append(c.Groups, out)
		}
	}
}

func readAssets(c *Canvas, m *rawManifest) {
	ids := make([]string, 0, len(m.AssetGroups))
	for id := range m.AssetGroups {
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := m.AssetGroups[ids[i]], m.AssetGroups[ids[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return assetRank(a.Kind) < assetRank(b.Kind) || (assetRank(a.Kind) == assetRank(b.Kind) && ids[i] < ids[j])
	})
	for _, id := range ids {
		g := m.AssetGroups[id]
		out := AssetGroup{ID: id, NodeID: "assets:" + id, Title: firstNonEmpty(g.Title, id), Kind: firstNonEmpty(g.Kind, "image"),
			Component: g.Component, Prop: g.Prop}
		for _, a := range g.Items {
			a.File, a.Dark = cleanRel(a.File), cleanRel(a.Dark)
			if !hexColor.MatchString(a.Bg) {
				a.Bg = ""
			}
			if a.Name == "" || a.File == "" {
				continue
			}
			a.NodeID = "asset:" + id + "/" + a.Name
			a.Cell = -1
			out.Items = append(out.Items, a)
		}
		if len(out.Items) > 0 {
			c.Assets = append(c.Assets, out)
		}
	}
}

func assetRank(kind string) int {
	switch kind {
	case "icon":
		return 0
	case "image":
		return 1
	case "logo":
		return 2
	}
	return 3
}

func readTemplates(c *Canvas, m *rawManifest, dir, id, version string) {
	for _, t := range m.Templates {
		if t.ID == "" {
			continue
		}
		out := Template{ID: t.ID, NodeID: "template:" + t.ID, Section: SectionTemplates, Blank: t.Blank, Title: firstNonEmpty(t.Title, t.ID),
			Kind: firstNonEmpty(t.Kind, "slides"), Aspect: [2]int{16, 9}, Component: t.Component, Cover: t.Cover, Description: t.Description}
		if t.Section == SectionReference {
			out.Section = SectionReference
		}
		if len(t.Aspect) == 2 && t.Aspect[0] > 0 && t.Aspect[1] > 0 {
			out.Aspect = [2]int{t.Aspect[0], t.Aspect[1]}
		}
		for _, s := range t.Sections {
			if s.ID == "" {
				continue
			}
			sec := TemplateSection{ID: s.ID, NodeID: "tsection:" + t.ID + "/" + s.ID, Title: firstNonEmpty(s.Title, s.ID)}
			for _, l := range s.Layouts {
				if l.ID == "" {
					continue
				}
				l.NodeID = "layout:" + t.ID + "/" + l.ID
				l.Board, l.Thumb = cleanRel(l.Board), cleanRel(l.Thumb)
				var scripts []string
				for _, s := range l.Scripts {
					if s = cleanRel(s); s != "" {
						scripts = append(scripts, s)
					}
				}
				l.Scripts = scripts
				if l.Slots == nil {
					l.Slots = []Slot{}
				}
				l.Cell = -1
				// A layout whose board the theme ships under _harnessly/layouts/ is one a deck can take (layouts.go).
				if c.Theme != "default" && l.Board == "_harnessly/layouts/"+l.ID+".dc.html" && ValidID(l.ID) {
					l.Address = id + "/" + c.Theme + "/" + l.ID
				}
				sec.Layouts = append(sec.Layouts, l)
			}
			if len(sec.Layouts) > 0 {
				out.Sections = append(out.Sections, sec)
			}
		}
		if len(out.Sections) == 0 {
			continue
		}
		if out.Cover == "" {
			out.Cover = out.Sections[0].Layouts[0].ID
		}
		c.Templates = append(c.Templates, out)
	}
}

// kitRoot is the root component the kit's own record names: the top-level member with an id and a version (the one
// install checks), its "root". A namespaced value (Acme.AcmeRoot) is taken by its last part.
func kitRoot(manifest []byte) string {
	var all map[string]json.RawMessage
	if json.Unmarshal(manifest, &all) != nil {
		return ""
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var own struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			Root    any    `json:"root"`
		}
		if json.Unmarshal(all[name], &own) != nil || own.ID == "" || own.Version == "" {
			continue
		}
		root := ""
		switch v := own.Root.(type) {
		case string:
			root = v
		case map[string]any:
			root, _ = v["component"].(string)
		}
		if i := strings.LastIndex(root, "."); i >= 0 {
			root = root[i+1:]
		}
		return root
	}
	return ""
}

// partOf reads a component's part: "provider", a parent's id, or {"of": "<id>"}; "" when it is a component of its own.
func partOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var o struct {
		Of string `json:"of"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return strings.TrimSpace(o.Of)
	}
	return ""
}

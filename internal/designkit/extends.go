package designkit

// A kit on a kit: the kit record's `extends` ("acme-base@^1") names a base kit. The child keeps only its
// differences: its own tokens, the components and presets it adds or replaces, its templates. Everything else is read
// from the base version the range resolves to. The assembly happens once, in ReadCanvas, so every reader (kit show and
// search, the kit_* tools, the kit screen, the live preview, the check) sees the same assembled kit.
//
//   - A token or a component of the child replaces the base's of the same name; the base's others stay, each marked with
//     the kit it comes from (From).
//   - The base's bundle and styles load before the child's (Canvas.Bases, outermost first), so the child's tokens.css and
//     styles overrule the base's by the cascade. A component the child replaces is assigned by its bundle into the
//     base's namespace (window.Acme.Button = …), so boards written on the base draw it too; a component inherited or
//     replaced is called by the base's namespace (Component.Ns), a new one by the child's own.
//   - The base version is fixed when the child's version is published (or first read): the resolved version is kept in
//     <kits>/<id>/.<version>.base.json, so a child shows the same after the base gets a newer release.
//   - Assets (icons, pictures) and the pictures of inherited components and layouts are not inherited yet: the screen
//     draws inherited components live.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxExtendsDepth is how many bases a chain may have (brand → base → foundation is 2).
const MaxExtendsDepth = 3

// Extends is the kit record's link to a base kit: its id and the version range.
type Extends struct {
	Kit   string `json:"kit"`
	Range string `json:"range"`
}

func (e Extends) String() string { return e.Kit + "@" + e.Range }

// ExtendsInfo says how a kit's link to its base came out: the version it resolved to, or the rule it broke and why.
type ExtendsInfo struct {
	Spec     string `json:"spec"`
	Resolved string `json:"resolved,omitempty"`
	Rule     string `json:"rule,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Base is a kit a canvas is built on: where its files are (its theme folder in its version) and its namespace.
type Base struct {
	Kit       string `json:"kit"`
	Version   string `json:"version"`
	Theme     string `json:"theme"`
	Folder    string `json:"folder"`
	Namespace string `json:"namespace"`
}

// fileSrc is where an inherited component's card or layout's board is read from.
type fileSrc struct{ Kit, Version, Folder string }

// ExtendsOf is the extends of a theme's manifest (design-system.json), ok when it names a base.
func ExtendsOf(manifest []byte) (Extends, bool) {
	var all map[string]json.RawMessage
	if json.Unmarshal(manifest, &all) != nil {
		return Extends{}, false
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var own struct {
			ID      string          `json:"id"`
			Version string          `json:"version"`
			Extends json.RawMessage `json:"extends"`
		}
		if json.Unmarshal(all[name], &own) != nil || own.ID == "" || own.Version == "" {
			continue
		}
		s, ok := jsonString(own.Extends)
		if sub := extendsRe.FindStringSubmatch(s); ok && sub != nil {
			return Extends{Kit: sub[1], Range: strings.TrimSpace(sub[2])}, true
		}
		return Extends{}, false
	}
	return Extends{}, false
}

func pinFile(dir, id, version string) string {
	return filepath.Join(dir, id, "."+version+".base.json")
}

func readPins(dir, id, version string) map[string]string {
	pins := map[string]string{}
	if raw, err := os.ReadFile(pinFile(dir, id, version)); err == nil {
		_ = json.Unmarshal(raw, &pins)
	}
	return pins
}

func writePin(dir, id, version, spec, resolved string) {
	if dir == "" || version == "" || version == DraftVersion || Resolve(dir, id, version, nil) == "" {
		return
	}
	pins := readPins(dir, id, version)
	if pins[spec] == resolved {
		return
	}
	pins[spec] = resolved
	raw, _ := json.MarshalIndent(pins, "", "  ")
	tmp := pinFile(dir, id, version) + ".tmp"
	if os.WriteFile(tmp, append(raw, '\n'), 0o644) == nil {
		_ = os.Rename(tmp, pinFile(dir, id, version))
	}
}

// ResolveBase picks the version of the base kit ex names for the kit childID at childVersion: the one pinned when the
// child was published, else the newest installed that satisfies the range (and then pinned). chain holds the ids of the
// kits above, the child last: it detects a loop and a chain that is too long. The returned info has Rule and Error set
// when no base can be used.
func ResolveBase(dir, childID, childVersion string, ex Extends, chain []string) (string, *ExtendsInfo) {
	info := &ExtendsInfo{Spec: ex.String()}
	fail := func(rule, format string, a ...any) (string, *ExtendsInfo) {
		info.Rule, info.Error = rule, fmt.Sprintf(format, a...)
		return "", info
	}
	for _, id := range chain {
		if id == ex.Kit {
			return fail("extends.cycle", "%s extends %s, which leads back to it (%s)", childID, ex.Kit, strings.Join(append(append([]string(nil), chain...), ex.Kit), " → "))
		}
	}
	if len(chain) > MaxExtendsDepth {
		return fail("extends.depth", "the chain of bases is longer than %d (%s)", MaxExtendsDepth, strings.Join(append(append([]string(nil), chain...), ex.Kit), " → "))
	}
	if childVersion != "" && childVersion != DraftVersion {
		if pinned := readPins(dir, childID, childVersion)[info.Spec]; pinned != "" {
			if Resolve(dir, ex.Kit, pinned, nil) == "" {
				return fail("extends.missing", "%s %s was the base this version was published with and is not installed", ex.Kit, pinned)
			}
			info.Resolved = pinned
			return pinned, info
		}
	}
	versions := listVersions(dir, ex.Kit)
	if len(versions) == 0 {
		return fail("extends.missing", "the base kit %s is not installed", ex.Kit)
	}
	for _, v := range versions {
		if ok, err := RangeSatisfies(ex.Range, v.Version); err == nil && ok {
			info.Resolved = v.Version
			writePin(dir, childID, childVersion, info.Spec, v.Version)
			return v.Version, info
		}
	}
	have := make([]string, len(versions))
	for i, v := range versions {
		have[i] = v.Version
	}
	return fail("extends.version", "no installed version of %s satisfies %s (installed: %s)", ex.Kit, ex.Range, strings.Join(have, ", "))
}

// PinBase resolves and keeps the base of a freshly installed or published version; nothing when it has no base or the
// base cannot be resolved yet (it is pinned the first time the version is read).
func PinBase(dir, id, version string) {
	for _, t := range Themes(dir, id, version) {
		raw, err := readThemeFile(dir, id, version, t.Folder, ManifestFile)
		if err != nil {
			continue
		}
		if ex, ok := ExtendsOf(raw); ok {
			ResolveBase(dir, id, version, ex, []string{id})
		}
	}
}

// inherit assembles the canvas c (read from its own manifest raw) with its base, when it has one.
func inherit(dir string, c *Canvas, raw []byte, stack []string) {
	ex, ok := ExtendsOf(raw)
	if !ok {
		return
	}
	chain := append(append([]string(nil), stack...), c.Kit)
	version, info := ResolveBase(dir, c.Kit, c.Version, ex, chain)
	c.Extends = info
	if info.Error != "" {
		return
	}
	theme := ""
	for _, t := range Themes(dir, ex.Kit, version) {
		if t.ID == c.Theme {
			theme = t.ID
		}
	}
	b, err := readCanvas(dir, ex.Kit, version, theme, chain)
	if err != nil {
		info.Rule, info.Error = "extends.missing", fmt.Sprintf("the base kit %s %s cannot be read: %v", ex.Kit, version, err)
		return
	}
	if b.Extends != nil && b.Extends.Error != "" {
		info.Rule, info.Error = b.Extends.Rule, "the base "+ex.Kit+": "+b.Extends.Error
	}
	mergeBase(c, b, ex.Kit+"@"+version)
}

func mergeBase(c, b *Canvas, ref string) {
	fs := &fileSrc{Kit: b.Kit, Version: b.Version, Folder: b.Base}
	c.Bases = append(append([]Base(nil), b.Bases...), Base{Kit: b.Kit, Version: b.Version, Theme: b.Theme, Folder: b.Base, Namespace: b.Namespace})
	if c.Root == "" {
		c.Root = b.Root
		c.RootNs = firstNonEmpty(b.RootNs, b.Namespace)
		if c.RootNs == c.Namespace {
			c.RootNs = ""
		}
	}
	c.TokenPrefix = firstNonEmpty(c.TokenPrefix, b.TokenPrefix)

	seen := map[string]bool{}
	for _, m := range b.Modes {
		seen[m] = true
	}
	modes := append([]string(nil), b.Modes...)
	for _, m := range c.Modes {
		if !seen[m] {
			modes = append(modes, m)
		}
	}
	c.Modes = modes

	// Tokens: the base's, each replaced by the child's of the same name, then the child's new ones.
	colors := make([]ColorToken, 0, len(b.Colors)+len(c.Colors))
	at := map[string]int{}
	for _, t := range b.Colors {
		t.From = firstNonEmpty(t.From, ref)
		at[t.Name] = len(colors)
		colors = append(colors, t)
	}
	for _, t := range c.Colors {
		if i, ok := at[t.Name]; ok {
			colors[i] = t
		} else {
			colors = append(colors, t)
		}
	}
	c.Colors = colors
	styles := make([]TypeStyle, 0, len(b.Type)+len(c.Type))
	at = map[string]int{}
	for _, t := range b.Type {
		at[t.ID] = len(styles)
		styles = append(styles, t)
	}
	for _, t := range c.Type {
		if i, ok := at[t.ID]; ok {
			styles[i] = t
		} else {
			styles = append(styles, t)
		}
	}
	c.Type = styles
	fonts := append([]FontFace(nil), c.Fonts...)
	known := map[string]bool{}
	for _, f := range c.Fonts {
		known[f.Family+"/"+f.Weight] = true
	}
	for _, f := range b.Fonts {
		if !known[f.Family+"/"+f.Weight] {
			fonts = append(fonts, f)
		}
	}
	c.Fonts = fonts
	plain := make([]PlainToken, 0, len(b.Tokens)+len(c.Tokens))
	at = map[string]int{}
	for _, t := range b.Tokens {
		t.From = firstNonEmpty(t.From, ref)
		at[t.ID] = len(plain)
		plain = append(plain, t)
	}
	for _, t := range c.Tokens {
		if i, ok := at[t.ID]; ok {
			plain[i] = t
		} else {
			plain = append(plain, t)
		}
	}
	c.Tokens = plain

	// Components: the base's groups, a component of the child replacing the base's of its id in place; new ones join the
	// group of the same id, else a group of their own.
	own := map[string]Component{}
	for _, g := range c.Groups {
		for _, k := range g.Components {
			own[k.ID] = k
		}
	}
	used := map[string]bool{}
	groups := make([]ComponentGroup, 0, len(b.Groups)+len(c.Groups))
	for _, g := range b.Groups {
		out := ComponentGroup{ID: g.ID, Title: g.Title}
		for _, k := range g.Components {
			if mine, ok := own[k.ID]; ok {
				// A replacement takes the place of the base's in the base's namespace (the child's bundle assigns it
				// there, after the base's), so the boards and samples written on the base draw it too.
				mine.Overrides = ref
				if mine.Ns == "" {
					mine.Ns = firstNonEmpty(k.Ns, b.Namespace)
					if mine.Ns == c.Namespace {
						mine.Ns = ""
					}
				}
				out.Components = append(out.Components, mine)
				used[k.ID] = true
				continue
			}
			k.From = firstNonEmpty(k.From, ref)
			if k.src == nil {
				k.src = fs
			}
			if k.Ns == "" {
				k.Ns = b.Namespace
			}
			if k.Ns == c.Namespace {
				k.Ns = ""
			}
			k.Thumb, k.Preview, k.Cell = "", "", -1
			out.Components = append(out.Components, k)
		}
		for _, cg := range c.Groups {
			if cg.ID != g.ID {
				continue
			}
			for _, k := range cg.Components {
				if !used[k.ID] {
					used[k.ID] = true
					out.Components = append(out.Components, k)
				}
			}
		}
		groups = append(groups, out)
	}
	for _, cg := range c.Groups {
		rest := ComponentGroup{ID: cg.ID, Title: cg.Title, Atlas: cg.Atlas}
		for _, k := range cg.Components {
			if !used[k.ID] {
				rest.Components = append(rest.Components, k)
			}
		}
		if len(rest.Components) > 0 {
			groups = append(groups, rest)
		}
	}
	c.Groups = groups

	// Templates: the base's, replaced by the child's of the same id.
	mine := map[string]bool{}
	for _, t := range c.Templates {
		mine[t.ID] = true
	}
	var inherited []Template
	for _, t := range b.Templates {
		if mine[t.ID] {
			continue
		}
		t.From = firstNonEmpty(t.From, ref)
		t.Atlas = nil
		secs := make([]TemplateSection, len(t.Sections))
		for i, s := range t.Sections {
			s.Layouts = append([]TemplateLayout(nil), s.Layouts...)
			for j := range s.Layouts {
				s.Layouts[j].Thumb, s.Layouts[j].Cell = "", -1
				if s.Layouts[j].src == nil {
					s.Layouts[j].src = fs
				}
			}
			secs[i] = s
		}
		t.Sections = secs
		inherited = append(inherited, t)
	}
	c.Templates = append(inherited, c.Templates...)
}

// NamespaceOf is the namespace a board calls a component by: the kit's, or the base's for an inherited one.
func (c *Canvas) NamespaceOf(k *Component) string {
	if k != nil && k.Ns != "" {
		return k.Ns
	}
	return c.Namespace
}

// Namespaces are the namespaces a page of the kit loads, the kit's own first.
func (c *Canvas) Namespaces() []string {
	out := []string{c.Namespace}
	seen := map[string]bool{c.Namespace: true}
	for i := len(c.Bases) - 1; i >= 0; i-- {
		if n := c.Bases[i].Namespace; n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// readFrom reads a file of the theme a component or layout comes from: the kit's own, or its base's.
func (c *Canvas) readFrom(dir string, src *fileSrc, rel string) ([]byte, error) {
	if src != nil {
		return readThemeFile(dir, src.Kit, src.Version, src.Folder, rel)
	}
	return readThemeFile(dir, c.Kit, c.Version, c.Base, rel)
}

// PropsCompatible says whether a component that replaces a base's keeps its contract: every prop the base declares
// stays, with its type (an enum keeps all its values), and what the replacement adds is optional, so a board written
// on the base still draws on the child. "" when compatible, else what breaks.
func PropsCompatible(base, mine []Prop) string {
	have := map[string]Prop{}
	for _, p := range mine {
		have[p.Name] = p
	}
	var bad []string
	for _, bp := range base {
		p, ok := have[bp.Name]
		switch {
		case !ok:
			bad = append(bad, "drops the prop "+bp.Name)
		case p.Type != bp.Type:
			bad = append(bad, fmt.Sprintf("changes the type of %s from %s to %s", bp.Name, bp.Type, p.Type))
		case bp.Type == "enum":
			for _, v := range bp.Values {
				if !contains(p.Values, v) {
					bad = append(bad, fmt.Sprintf("drops the value %q of %s", v, bp.Name))
				}
			}
		}
		if ok && p.Required && !bp.Required {
			bad = append(bad, "makes "+bp.Name+" required")
		}
	}
	known := map[string]bool{}
	for _, bp := range base {
		known[bp.Name] = true
	}
	for _, p := range mine {
		if !known[p.Name] && p.Required {
			bad = append(bad, "adds the required prop "+p.Name)
		}
	}
	return strings.Join(bad, "; ")
}

package designkit

// The views of a kit for an agent: the same data `harnessly design kit show/search` prints and the MCP tools
// kit_* answer, built once here so the two never differ. An agent reads in three steps: Index (what is there, small),
// ComponentOf (everything about one component, from the manifest and its card), then writes the board. The shapes are
// described in docs/agents/design-kit-mcp.md; ViewSchema names their version.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// ViewSchema is the version of the shapes in this file; it is in every answer of the kit tools and of `kit show`.
const ViewSchema = "kit-view/1"

// IndexBudget is the size the kit index aims at for a kit of fifty components: bytes of its JSON.
const IndexBudget = 2048

// MarshalCompact is v as JSON on one line, with <, > and & as they are (a board's markup stays readable).
func MarshalCompact(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte("null")
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}

// ---- the index ----

// IndexView is the compact map of a kit: its groups with a component per line, how many tokens, its templates.
type IndexView struct {
	Schema    string `json:"schema"`
	Kit       string `json:"kit"`
	Version   string `json:"version"`
	Theme     string `json:"theme"`
	Title     string `json:"title"`
	Namespace string `json:"namespace"`
	Root      string `json:"root,omitempty"`
	// Strict: projects on the kit are checked in strict mode (kit.strict): only its components, props and tokens.
	Strict bool `json:"strict,omitempty"`
	// Components are the groups; an item is «Name — what it is for», with «[new]», «[deprecated → X]» or «+N» (parts)
	// after the name when it applies. A part is not an item of its own: it is counted under its parent.
	Components []IndexGroup `json:"components"`
	// Tokens counts the tokens by family; Total is all of them.
	Tokens    map[string]int  `json:"tokens"`
	Templates []IndexTemplate `json:"templates,omitempty"`
	Next      string          `json:"next"`
}

// IndexGroup is a group of components in the index.
type IndexGroup struct {
	ID    string   `json:"id"`
	Items []string `json:"items"`
}

// IndexTemplate is a template with the number of its layouts.
type IndexTemplate struct {
	ID      string `json:"id"`
	Layouts int    `json:"layouts"`
}

// IndexOf builds the index of a canvas. The descriptions are cut so that the JSON stays near IndexBudget for fifty
// components: a short kit gets whole sentences, a big one names and a few words, and the agent asks for the rest.
func IndexOf(c *Canvas) IndexView {
	descMax := 90
	for {
		v := indexWith(c, descMax)
		if descMax <= 0 || len(MarshalCompact(v)) <= IndexBudget*max(1, (componentCount(c)+49)/50) {
			return v
		}
		descMax -= 4
	}
}

func componentCount(c *Canvas) int {
	n := 0
	for _, g := range c.Groups {
		n += len(g.Components)
	}
	return n
}

func indexWith(c *Canvas, descMax int) IndexView {
	v := IndexView{Schema: ViewSchema, Kit: c.Kit, Version: c.Version, Theme: c.Theme, Title: c.Title, Namespace: c.Namespace, Root: c.Root, Strict: c.Strict,
		Components: []IndexGroup{}, Tokens: map[string]int{}, Next: "kit_component {kit, name}"}
	parts := map[string]int{}
	for _, g := range c.Groups {
		for _, k := range g.Components {
			if k.Part != "" && k.Part != PartProvider {
				parts[k.Part]++
			}
		}
	}
	for _, g := range c.Groups {
		out := IndexGroup{ID: g.ID, Items: []string{}}
		for _, k := range g.Components {
			if k.Part != "" && k.Part != PartProvider {
				continue
			}
			item := k.ID
			switch {
			case k.Part == PartProvider:
				item += " [provider]"
			case k.Status == "new":
				item += " [new]"
			case k.Status == "deprecated" && k.ReplacedBy != "":
				item += " [deprecated → " + k.ReplacedBy + "]"
			case k.Status == "deprecated":
				item += " [deprecated]"
			}
			if n := parts[k.ID]; n > 0 {
				item += fmt.Sprintf(" +%d", n)
			}
			if k.From != "" {
				item += " ←" + strings.SplitN(k.From, "@", 2)[0]
			}
			if d := clip(oneLine(k.Description), descMax); d != "" {
				item += " — " + d
			}
			out.Items = append(out.Items, item)
		}
		if len(out.Items) > 0 {
			v.Components = append(v.Components, out)
		}
	}
	if n := len(c.Colors); n > 0 {
		v.Tokens["color"] = n
	}
	if n := len(c.Type); n > 0 {
		v.Tokens["type"] = n
	}
	for _, t := range c.Tokens {
		v.Tokens[t.Kind]++
	}
	total := 0
	for _, n := range v.Tokens {
		total += n
	}
	v.Tokens["total"] = total
	for _, t := range c.Templates {
		if t.Blank || t.Section != SectionTemplates {
			continue
		}
		n := 0
		for _, s := range t.Sections {
			n += len(s.Layouts)
		}
		v.Templates = append(v.Templates, IndexTemplate{ID: t.ID, Layouts: n})
	}
	return v
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip cuts s to at most n runes at a word, with an ellipsis; "" when n <= 0.
func clip(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)[:n]
	if i := strings.LastIndex(string(r), " "); i > n/2 {
		return strings.TrimRight(string(r[:i]), " ,;:.") + "…"
	}
	return strings.TrimRight(string(r), " ,;:.") + "…"
}

// ---- a component ----

// ComponentView is everything an agent needs to use one component.
type ComponentView struct {
	Schema      string   `json:"schema"`
	Kit         string   `json:"kit"`
	Version     string   `json:"version"`
	Theme       string   `json:"theme"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Group       string   `json:"group"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords,omitempty"`
	Status      string   `json:"status,omitempty"`
	ReplacedBy  string   `json:"replacedBy,omitempty"`
	// From is the base kit the component is inherited from («id@version»); Overrides the base one it replaces.
	From      string `json:"from,omitempty"`
	Overrides string `json:"overrides,omitempty"`
	// Part is the parent's id for a piece of a compound component, «provider» for a provider.
	Part string `json:"part,omitempty"`
	// Props are all of them; Required and Optional are their names, so the minimum call is clear at a glance.
	Props    []Prop    `json:"props"`
	Required []string  `json:"required"`
	Optional []string  `json:"optional"`
	Variants []Variant `json:"variants"`
	// Parts are the pieces of a compound component; InSample: the component's sample draws it.
	Parts []PartView `json:"parts,omitempty"`
	// Sample is the sample's props as the manifest has them; SampleJSX the same as JSX; Call as board markup.
	Sample    map[string]any `json:"sample,omitempty"`
	SampleJSX string         `json:"sampleJsx,omitempty"`
	Call      string         `json:"call"`
	Code      *Code          `json:"code,omitempty"`
	Preset    *Preset        `json:"preset,omitempty"`
	// Presets are the components that are this one with fixed props.
	Presets []PresetView `json:"presets,omitempty"`
	// The card (components/<Comp>/README.md): when to use it and when not, examples, and the optional sections.
	Card CardView `json:"card"`
	// UsedWith is computed from the samples and the layouts' boards, never written by hand.
	UsedWith UsedWith `json:"usedWith"`
}

// PartView is a piece of a compound component.
type PartView struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	InSample    bool   `json:"inSample"`
}

// PresetView is a preset of a component.
type PresetView struct {
	ID    string         `json:"id"`
	Props map[string]any `json:"props,omitempty"`
}

// UsedWith: the components whose samples hold this one or are held by its sample, and the layouts whose boards use it.
type UsedWith struct {
	Components []string `json:"components"`
	Layouts    []string `json:"layouts"`
}

// CardView is a component's card cut into its sections. Empty sections are left out of the JSON.
type CardView struct {
	// Found: the card file exists.
	Found         bool     `json:"found"`
	Summary       string   `json:"summary,omitempty"`
	WhenToUse     []string `json:"whenToUse,omitempty"`
	WhenNotToUse  []string `json:"whenNotToUse,omitempty"`
	Examples      []string `json:"examples,omitempty"`
	InCode        string   `json:"inCode,omitempty"`
	Content       []string `json:"content,omitempty"`
	Accessibility []string `json:"accessibility,omitempty"`
	States        []string `json:"states,omitempty"`
	// Status is the card's own «Status» section (a note on maturity), apart from the manifest's status.
	Status string `json:"status,omitempty"`
}

// CardSections are the headings of a card the view reads, as a card writes them (any case). «Props» is read from the
// manifest, not from the card.
var CardSections = []string{"When to use", "When not to use", "Props", "Example", "Examples", "In code", "Used with", "Content", "Accessibility", "States", "Status"}

// ParseCard cuts a card's markdown into its sections.
func ParseCard(md string) CardView {
	v := CardView{Found: true, Summary: cardSummary(md)}
	sections := map[string][]string{}
	cur := ""
	fenced := false
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		}
		if !fenced && strings.HasPrefix(line, "## ") {
			cur = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			continue
		}
		if cur != "" {
			sections[cur] = append(sections[cur], line)
		}
	}
	v.WhenToUse = bullets(sections["when to use"])
	v.WhenNotToUse = bullets(sections["when not to use"])
	v.Examples = append(blocks(sections["example"]), blocks(sections["examples"])...)
	v.InCode = strings.TrimSpace(stripFences(strings.Join(sections["in code"], "\n")))
	v.Content = bullets(sections["content"])
	v.Accessibility = bullets(sections["accessibility"])
	v.States = bullets(sections["states"])
	v.Status = oneLine(strings.Join(sections["status"], " "))
	return v
}

var bulletRe = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+`)

// bullets are the list items of a section, a paragraph of plain text counting as one item.
func bullets(lines []string) []string {
	var out []string
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, oneLine(strings.Join(para, " ")))
			para = nil
		}
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			flush()
		case bulletRe.MatchString(l):
			flush()
			para = []string{bulletRe.ReplaceAllString(l, "")}
		case strings.HasPrefix(t, "|") || strings.HasPrefix(t, "<!--"):
		default:
			para = append(para, t)
		}
	}
	flush()
	return out
}

// blocks are the paragraphs or fenced blocks of an example section, one example each.
func blocks(lines []string) []string {
	var out []string
	var cur []string
	fenced := false
	flush := func() {
		if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
			out = append(out, s)
		}
		cur = nil
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			fenced = !fenced
			flush()
			continue
		}
		if t == "" && !fenced {
			flush()
			continue
		}
		cur = append(cur, strings.TrimRight(l, " "))
	}
	flush()
	return out
}

func stripFences(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "```") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// CardPath is where a component's card is in a theme's folder.
func CardPath(id string) string { return path.Join("components", id, "README.md") }

// ComponentOf builds the view of a component of a canvas read from dir (the cards and boards are read from there).
// nil when the canvas has no component of this id or name.
func ComponentOf(dir string, c *Canvas, name string) *ComponentView {
	k := c.Component(name)
	if k == nil {
		return nil
	}
	group := ""
	for _, g := range c.Groups {
		for _, x := range g.Components {
			if x.ID == k.ID {
				group = g.ID
			}
		}
	}
	v := &ComponentView{Schema: ViewSchema, Kit: c.Kit, Version: c.Version, Theme: c.Theme, ID: k.ID, Name: k.Name, Group: group,
		Description: k.Description, Keywords: k.Keywords, Status: k.Status, ReplacedBy: k.ReplacedBy, From: k.From, Overrides: k.Overrides, Part: k.Part, Props: k.Props,
		Variants: k.Variants, Sample: k.Sample, Code: k.Code, Preset: k.Preset, Required: []string{}, Optional: []string{}}
	for _, p := range k.Props {
		if p.Required {
			v.Required = append(v.Required, p.Name)
		} else {
			v.Optional = append(v.Optional, p.Name)
		}
	}
	// A preset has the card, the sample and the props of its target.
	src := k
	if k.Preset != nil {
		if t := c.Component(k.Preset.Of); t != nil {
			src = t
			v.Props, v.Variants = t.Props, t.Variants
			v.Required, v.Optional = nil, nil
			for _, p := range t.Props {
				if p.Required {
					v.Required = append(v.Required, p.Name)
				} else {
					v.Optional = append(v.Optional, p.Name)
				}
			}
		}
	}
	if src.Sample != nil {
		sample := map[string]any{}
		for key, val := range src.Sample {
			sample[key] = val
		}
		if k.Preset != nil {
			for key, val := range k.Preset.Props {
				sample[key] = val
			}
		}
		v.Sample = sample
		v.SampleJSX = JSX(k.ID, sample)
	}
	v.Call = XImport(c.NamespaceOf(k), k.ID, v.Sample)
	for _, g := range c.Groups {
		for _, x := range g.Components {
			if x.Part == k.ID {
				v.Parts = append(v.Parts, PartView{ID: x.ID, Description: x.Description, InSample: mentions(src.Sample, x.ID) || variantsMention(src.Variants, x.ID)})
			}
			if x.Preset != nil && x.Preset.Of == k.ID {
				v.Presets = append(v.Presets, PresetView{ID: x.ID, Props: x.Preset.Props})
			}
		}
	}
	if raw, err := c.readFrom(dir, src.src, CardPath(src.ID)); err == nil {
		v.Card = ParseCard(string(raw))
	}
	v.UsedWith = usedWith(dir, c, k)
	return v
}

// usedWith finds where a component is drawn next to others: samples that hold it (and the ones its own sample holds),
// and the layouts whose board names it.
func usedWith(dir string, c *Canvas, k *Component) UsedWith {
	u := UsedWith{Components: []string{}, Layouts: []string{}}
	seen := map[string]bool{}
	for _, g := range c.Groups {
		for _, x := range g.Components {
			if x.ID == k.ID {
				continue
			}
			if mentions(x.Sample, k.ID) || variantsMention(x.Variants, k.ID) || mentions(k.Sample, x.ID) || variantsMention(k.Variants, x.ID) {
				if !seen[x.ID] {
					seen[x.ID] = true
					u.Components = append(u.Components, x.ID)
				}
			}
		}
	}
	sort.Strings(u.Components)
	needle := c.NamespaceOf(k) + "." + k.ID
	for _, t := range c.Templates {
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				if l.Board == "" {
					continue
				}
				raw, err := c.readFrom(dir, l.src, l.Board)
				if err != nil {
					continue
				}
				if boardUses(string(raw), needle) {
					u.Layouts = append(u.Layouts, t.ID+"/"+l.ID)
				}
			}
		}
	}
	return u
}

// boardUses tells whether a board's markup names the component (Acme.Button) as a whole word.
func boardUses(board, qualified string) bool {
	for i := 0; ; {
		j := strings.Index(board[i:], qualified)
		if j < 0 {
			return false
		}
		end := i + j + len(qualified)
		if end >= len(board) || !isIdentByte(board[end]) {
			return true
		}
		i = end
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '$' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// XImport is how a board calls a component: the <x-import> markup with the string props of a sample.
func XImport(ns, comp string, props map[string]any) string {
	var b strings.Builder
	b.WriteString(`<x-import component-from-global-scope="` + ns + "." + comp + `"`)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := props[k].(string); ok {
			b.WriteString(" " + k + `="` + s + `"`)
		}
	}
	b.WriteString("></x-import>")
	return b.String()
}

// JSX writes a component with its props as JSX: strings as attributes, other values in braces, elements ({"$c": …})
// as nested JSX, «children» as the body.
func JSX(comp string, props map[string]any) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		if k != "children" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<" + comp)
	for _, k := range keys {
		switch x := props[k].(type) {
		case string:
			b.WriteString(" " + k + "=" + quoteAttr(x))
		default:
			b.WriteString(" " + k + "={" + jsExpr(x) + "}")
		}
	}
	children, has := props["children"]
	if !has || children == nil {
		b.WriteString(" />")
		return b.String()
	}
	b.WriteString(">" + jsxChildren(children) + "</" + comp + ">")
	return b.String()
}

func quoteAttr(s string) string {
	if !strings.Contains(s, `"`) {
		return `"` + s + `"`
	}
	return "{" + string(MarshalCompact(s)) + "}"
}

func jsxChildren(v any) string {
	switch x := v.(type) {
	case string:
		if strings.ContainsAny(x, "{}<>") {
			return "{" + string(MarshalCompact(x)) + "}"
		}
		return x
	case []any:
		var b strings.Builder
		for _, e := range x {
			b.WriteString(jsxChildren(e))
		}
		return b.String()
	case map[string]any:
		if name, props, children, ok := Element(x); ok {
			p := map[string]any{}
			for k, val := range props {
				p[k] = val
			}
			if children != nil {
				p["children"] = children
			}
			return JSX(name, p)
		}
	}
	return "{" + jsExpr(v) + "}"
}

// jsExpr is a value as a JavaScript expression: an element as JSX, a function value as an arrow function, the rest as JSON.
func jsExpr(v any) string {
	switch x := v.(type) {
	case map[string]any:
		if _, _, _, ok := Element(x); ok {
			return jsxChildren(x)
		}
		if field, ok := FunctionValue(x); ok {
			if field == "" {
				return "() => {}"
			}
			return "(row) => row." + field
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = string(MarshalCompact(k)) + ": " + jsExpr(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = jsExpr(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return string(MarshalCompact(v))
}

// ---- search ----

// SearchHit is a result of kit search: what the node is and what to ask for next.
type SearchHit struct {
	Node  string `json:"node"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Path  string `json:"path,omitempty"`
	// Ref is what the next tool takes: a component's id (kit_component), a layout's id (kit_template).
	Ref string `json:"ref,omitempty"`
	// From is the base kit the node comes from, "" for the kit's own.
	From string `json:"from,omitempty"`
}

// SearchView is the answer to a query.
type SearchView struct {
	Schema  string      `json:"schema"`
	Kit     string      `json:"kit"`
	Version string      `json:"version"`
	Theme   string      `json:"theme"`
	Query   string      `json:"query"`
	Results []SearchHit `json:"results"`
}

// SearchOf looks a query through the canvas's index with the screen's own rule.
func SearchOf(c *Canvas, query string, limit int) SearchView {
	v := SearchView{Schema: ViewSchema, Kit: c.Kit, Version: c.Version, Theme: c.Theme, Query: query, Results: []SearchHit{}}
	for _, e := range Search(c.Index, query, limit) {
		h := SearchHit{Node: e.Node, Kind: e.Kind, Title: e.Title, Path: e.Path, From: e.From}
		switch e.Kind {
		case KindComponent:
			h.Ref = strings.TrimPrefix(e.Node, "component:")
		case KindLayout, KindTemplate:
			h.Ref = e.Node[strings.LastIndexAny(e.Node, "/:")+1:]
		}
		v.Results = append(v.Results, h)
	}
	return v
}

// ---- tokens ----

// TokenView is a token as an agent uses it: its name, the CSS variable, the value (per mode for a colour).
type TokenView struct {
	Name   string            `json:"name"`
	Var    string            `json:"var,omitempty"`
	Value  string            `json:"value,omitempty"`
	Values map[string]string `json:"values,omitempty"`
	Usage  string            `json:"usage,omitempty"`
	// From is the base kit the token is inherited from.
	From string `json:"from,omitempty"`
}

// TokensView is the foundation of a theme by family: color, type, then radius, spacing… as the kit names them.
type TokensView struct {
	Schema  string                 `json:"schema"`
	Kit     string                 `json:"kit"`
	Version string                 `json:"version"`
	Theme   string                 `json:"theme"`
	Prefix  string                 `json:"prefix,omitempty"`
	Modes   []string               `json:"modes,omitempty"`
	Groups  map[string][]TokenView `json:"groups"`
}

// TokensOf lists a canvas's tokens; group names one family ("" all). A token with no explicit var gets the kit's
// prefix and its name, which is how tokens.css names them; none without a prefix.
func TokensOf(c *Canvas, group string) TokensView {
	v := TokensView{Schema: ViewSchema, Kit: c.Kit, Version: c.Version, Theme: c.Theme, Prefix: c.TokenPrefix, Modes: c.Modes, Groups: map[string][]TokenView{}}
	cssVar := func(explicit, name string) string {
		if explicit != "" {
			return explicit
		}
		if c.TokenPrefix != "" {
			return c.TokenPrefix + name
		}
		return ""
	}
	want := func(g string) bool { return group == "" || group == g }
	if want("color") {
		for _, t := range c.Colors {
			tv := TokenView{Name: t.Name, Var: cssVar(t.Var, t.Name), Usage: t.Usage, From: t.From}
			if len(t.Values) == 1 && t.Values[""] != "" {
				tv.Value = t.Values[""]
			} else {
				tv.Values = t.Values
			}
			v.Groups["color"] = append(v.Groups["color"], tv)
		}
	}
	if want("type") {
		for _, s := range c.Type {
			val := strings.TrimSpace(s.Size + "/" + s.Line + " " + s.Weight + " " + s.Family)
			v.Groups["type"] = append(v.Groups["type"], TokenView{Name: s.Group + "/" + s.Name, Value: val})
		}
	}
	for _, t := range c.Tokens {
		if want(t.Kind) {
			v.Groups[t.Kind] = append(v.Groups[t.Kind], TokenView{Name: t.Name, Var: cssVar(t.Var, t.Name), Value: t.Value, Usage: t.Usage, From: t.From})
		}
	}
	return v
}

// TokenGroups are the families a canvas has tokens in, sorted.
func TokenGroups(c *Canvas) []string {
	set := map[string]bool{}
	if len(c.Colors) > 0 {
		set["color"] = true
	}
	if len(c.Type) > 0 {
		set["type"] = true
	}
	for _, t := range c.Tokens {
		set[t.Kind] = true
	}
	out := make([]string, 0, len(set))
	for g := range set {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// ---- templates ----

// TemplateListView lists the templates (pages, slides) with their layouts' purposes.
type TemplateListView struct {
	Schema    string         `json:"schema"`
	Kit       string         `json:"kit"`
	Version   string         `json:"version"`
	Theme     string         `json:"theme"`
	Templates []TemplateItem `json:"templates"`
}

// TemplateItem is a template in the list.
type TemplateItem struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Kind        string       `json:"kind"`
	Aspect      [2]int       `json:"aspect"`
	Description string       `json:"description,omitempty"`
	Layouts     []LayoutItem `json:"layouts"`
}

// LayoutItem is a layout in the list: enough to choose one.
type LayoutItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Section string `json:"section,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}

// TemplatesOf lists a canvas's templates (a template's guide and master layouts are left out); only names a template
// id when template is not empty.
func TemplatesOf(c *Canvas, template string) TemplateListView {
	v := TemplateListView{Schema: ViewSchema, Kit: c.Kit, Version: c.Version, Theme: c.Theme, Templates: []TemplateItem{}}
	for _, t := range c.Templates {
		if t.Blank || t.Section != SectionTemplates || template != "" && t.ID != template {
			continue
		}
		it := TemplateItem{ID: t.ID, Title: t.Title, Kind: t.Kind, Aspect: t.Aspect, Description: t.Description, Layouts: []LayoutItem{}}
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				it.Layouts = append(it.Layouts, LayoutItem{ID: l.ID, Name: l.Name, Section: s.Title, Purpose: l.Purpose})
			}
		}
		v.Templates = append(v.Templates, it)
	}
	return v
}

// TemplateView is one layout with what an agent fills and copies.
type TemplateView struct {
	Schema   string         `json:"schema"`
	Kit      string         `json:"kit"`
	Version  string         `json:"version"`
	Theme    string         `json:"theme"`
	Template string         `json:"template"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Aspect   [2]int         `json:"aspect"`
	Purpose  string         `json:"purpose,omitempty"`
	Slots    []Slot         `json:"slots"`
	Props    map[string]any `json:"props,omitempty"`
	// Address is how a project takes the layout, Board the board's file in the theme.
	Address string `json:"address,omitempty"`
	Board   string `json:"board,omitempty"`
	// Source is the board's HTML when it is small enough to read here (MaxTemplateSource bytes), else BoardBytes tells its size.
	Source     string `json:"source,omitempty"`
	BoardBytes int    `json:"boardBytes,omitempty"`
}

// MaxTemplateSource is the largest board kit_template inlines.
const MaxTemplateSource = 64 << 10

// TemplateOf finds a layout by its id (or a template's id: its first layout) and reads its board from dir.
func TemplateOf(dir string, c *Canvas, id string) *TemplateView {
	for _, t := range c.Templates {
		if t.Blank || t.Section != SectionTemplates {
			continue
		}
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				first := t.ID == id && t.Sections[0].ID == s.ID && s.Layouts[0].ID == l.ID
				if l.ID != id && !first {
					continue
				}
				v := &TemplateView{Schema: ViewSchema, Kit: c.Kit, Version: c.Version, Theme: c.Theme, Template: t.ID, ID: l.ID, Name: l.Name,
					Kind: t.Kind, Aspect: t.Aspect, Purpose: l.Purpose, Slots: l.Slots, Props: l.Props, Address: l.Address, Board: l.Board}
				if l.Board != "" {
					if raw, err := c.readFrom(dir, l.src, l.Board); err == nil {
						if len(raw) <= MaxTemplateSource {
							v.Source = string(raw)
						} else {
							v.BoardBytes = len(raw)
						}
					}
				}
				return v
			}
		}
	}
	return nil
}

// ---- the check ----

// CheckView is the summary of `kit check` for an agent: counts, the problems (errors first, at most MaxCheckProblems),
// and where the components stand in the readiness levels.
type CheckView struct {
	Schema     string    `json:"schema"`
	Kit        string    `json:"kit"`
	Version    string    `json:"version"`
	Ok         bool      `json:"ok"`
	Themes     []string  `json:"themes"`
	Components int       `json:"components"`
	Errors     int       `json:"errors"`
	Warnings   int       `json:"warnings"`
	Problems   []Problem `json:"problems"`
	// Truncated: more problems than were listed.
	Truncated bool `json:"truncated,omitempty"`
	// Level is the kit's readiness (the lowest component's, -1 .. TopLevel), Next what that component lacks.
	Level int    `json:"level"`
	Next  string `json:"next,omitempty"`
	// Levels counts components by level; Behind lists the components below the top level.
	Levels map[string]int       `json:"levels"`
	Behind []ComponentReadiness `json:"behind,omitempty"`
}

// MaxCheckProblems bounds the problems a CheckView lists.
const MaxCheckProblems = 40

// CheckOf summarises a report of Check for a kit version.
func CheckOf(kit, version string, rep *CheckReport) CheckView {
	v := CheckView{Schema: ViewSchema, Kit: kit, Version: version, Themes: rep.Themes, Components: rep.Components, Errors: rep.Errors(),
		Level: rep.Level, Next: rep.Next, Problems: []Problem{}, Levels: map[string]int{}}
	v.Warnings = len(rep.Problems) - v.Errors
	v.Ok = v.Errors == 0
	probs := append([]Problem(nil), rep.Problems...)
	sort.SliceStable(probs, func(i, j int) bool { return probs[i].Level == LevelError && probs[j].Level != LevelError })
	if len(probs) > MaxCheckProblems {
		probs, v.Truncated = probs[:MaxCheckProblems], true
	}
	if probs != nil {
		v.Problems = probs
	}
	for _, r := range rep.Readiness {
		v.Levels[fmt.Sprint(r.Level)]++
		if r.Level < TopLevel && len(v.Behind) < MaxCheckProblems {
			v.Behind = append(v.Behind, r)
		}
	}
	return v
}

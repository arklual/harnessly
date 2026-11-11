package designkit

// The strict check: a mockup on a kit is made of the kit only, so that it goes into the product's code one to
// one. It reads a project's sources — page modules (pages/<name>.tsx), the project's own components and the boards
// written by hand (.dc.html) — against the kit's index and tokens and reports four kinds of finding:
//
//	strict.element     an element that is not a component of the kit (plain HTML, another package, a component
//	                   written inside a page); a block of the project's own is a local component in its own file
//	strict.value       a value that is not a token: a raw colour or length, a CSS variable the kit does not have, a
//	                   token prop set to something that is not a token
//	strict.prop        a prop the component does not declare, or an enum prop set to a value it does not list
//	strict.deprecated  a component the kit marks deprecated
//
// Each finding names the file, the place, what is wrong and what to put instead, so an agent can fix it without
// asking. The check is rules over text, without a model: the same sources give the same findings.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// StrictRules are the strict check's rules; docs/agents/design-kits.md lists them (a test keeps the two together).
var StrictRules = []Rule{
	{"strict.element", LevelError, "every element is a component of the kit, or a local component of the project in a file of its own"},
	{"strict.value", LevelError, "colours, lengths and CSS variables are the kit's tokens, and a token prop gets a token"},
	{"strict.prop", LevelError, "a component gets only the props it declares, an enum prop only one of its values"},
	{"strict.deprecated", LevelError, "no component the kit marks deprecated"},
}

// StrictFinding is one finding of the strict check. Line and Col are 1-based.
type StrictFinding struct {
	Rule    string `json:"rule"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Element string `json:"element,omitempty"`
	What    string `json:"what"`
	Fix     string `json:"fix"`
}

// String is the finding as one line: file:line:col rule — what. Fix: …
func (f StrictFinding) String() string {
	return fmt.Sprintf("%s:%d:%d %s — %s. Fix: %s", f.File, f.Line, f.Col, f.Rule, f.What, f.Fix)
}

// StrictKit is what the strict check knows of a kit.
type StrictKit struct {
	c        *Canvas
	comps    map[string]*Component
	byImport map[string]map[string]string // module -> export -> component id
	vars     map[string]string            // CSS variable -> token name
	tokens   map[string]string            // token name -> CSS variable ("" without one)
	colorTok map[string]string            // a colour's normalised value -> token name
	plainTok map[string]string            // a length's value -> token name
	ns       map[string]bool
	ids      []string
	// libVar: a token's semantic variable (prefix + name) -> the library's own variable (token.var), when they differ.
	libVar map[string]string
}

// NewStrictKit prepares a kit (as assembled, with its bases) for the strict check.
func NewStrictKit(c *Canvas) *StrictKit {
	k := &StrictKit{c: c, comps: map[string]*Component{}, byImport: map[string]map[string]string{}, vars: map[string]string{},
		tokens: map[string]string{}, colorTok: map[string]string{}, plainTok: map[string]string{}, ns: map[string]bool{},
		libVar: map[string]string{}}
	for _, n := range c.Namespaces() {
		k.ns[n] = true
	}
	for gi := range c.Groups {
		for ci := range c.Groups[gi].Components {
			comp := &c.Groups[gi].Components[ci]
			k.comps[comp.ID] = comp
			k.ids = append(k.ids, comp.ID)
		}
	}
	sort.Strings(k.ids)
	for _, id := range k.ids {
		comp := k.comps[id]
		if comp.Code == nil || comp.Code.Import == "" || comp.Code.Export == "" {
			continue
		}
		m := k.byImport[comp.Code.Import]
		if m == nil {
			m = map[string]string{}
			k.byImport[comp.Code.Import] = m
		}
		// An export stands for the component itself, not for a preset of it.
		if have, ok := m[comp.Code.Export]; !ok || (k.comps[have].Preset != nil && comp.Preset == nil) {
			m[comp.Code.Export] = id
		}
	}
	cssVar := func(explicit, name string) string {
		if explicit != "" {
			return explicit
		}
		if c.TokenPrefix != "" {
			return c.TokenPrefix + name
		}
		return ""
	}
	semantic := func(explicit, name string) {
		if explicit != "" && c.TokenPrefix != "" && c.TokenPrefix+name != explicit {
			k.vars[c.TokenPrefix+name] = name
			k.libVar[c.TokenPrefix+name] = explicit
		}
	}
	for _, t := range c.Colors {
		v := cssVar(t.Var, t.Name)
		k.tokens[t.Name] = v
		if v != "" {
			k.vars[v] = t.Name
		}
		semantic(t.Var, t.Name)
		modes := make([]string, 0, len(t.Values))
		for m := range t.Values {
			modes = append(modes, m)
		}
		sort.Strings(modes)
		for _, m := range modes {
			if n := normColor(t.Values[m]); n != "" {
				if _, ok := k.colorTok[n]; !ok {
					k.colorTok[n] = t.Name
				}
			}
		}
	}
	for _, t := range c.Tokens {
		v := cssVar(t.Var, t.Name)
		k.tokens[t.Name] = v
		if v != "" {
			k.vars[v] = t.Name
		}
		semantic(t.Var, t.Name)
		if val := strings.TrimSpace(t.Value); val != "" {
			if _, ok := k.plainTok[val]; !ok {
				k.plainTok[val] = t.Name
			}
		}
	}
	return k
}

// Package is the kit's npm package, "" when it does not name one.
func (k *StrictKit) Package() string { return k.c.Package }

// owns is whether a module specifier is the kit's package or a path below it.
func (k *StrictKit) owns(spec string) bool {
	p := k.c.Package
	if p == "" {
		_, ok := k.byImport[spec]
		return ok
	}
	return spec == p || strings.HasPrefix(spec, p+"/")
}

// propsOf are the props a component declares; a preset takes its target's.
func (k *StrictKit) propsOf(comp *Component) []Prop {
	props := comp.Props
	if comp.Preset != nil {
		if t := k.comps[comp.Preset.Of]; t != nil {
			props = append(append([]Prop{}, t.Props...), props...)
		}
	}
	return props
}

// importOf is how the product imports a component: `import { X } from "pkg"`.
func (k *StrictKit) importOf(id string) string {
	comp := k.comps[id]
	if comp == nil {
		return id
	}
	if comp.Preset != nil {
		if t := k.comps[comp.Preset.Of]; t != nil && t.Code != nil {
			return fmt.Sprintf("<%s %s> (import { %s } from %q)", t.Code.Export, presetAttrs(comp.Preset.Props), t.Code.Export, t.Code.Import)
		}
	}
	if comp.Code != nil {
		return fmt.Sprintf("%s (import { %s } from %q)", comp.Code.Export, comp.Code.Export, comp.Code.Import)
	}
	return id
}

func presetAttrs(props map[string]any) string {
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	var parts []string
	for _, n := range names {
		switch v := props[n].(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s=%q", n, v))
		default:
			parts = append(parts, fmt.Sprintf("%s={%v}", n, v))
		}
	}
	return strings.Join(parts, " ")
}

// ---- suggestions ----

// suggestFor are kit components that usually stand in for an HTML element.
var suggestFor = map[string][]string{
	"div": {"Stack", "Box", "Flex", "Grid", "Container", "Card"}, "section": {"Stack", "Box", "Container", "Card"},
	"main": {"Container", "Stack", "Box"}, "header": {"PageHeader", "Header", "Stack"}, "footer": {"Footer", "Stack"},
	"nav": {"Nav", "Navigation", "Tabs"}, "aside": {"Sidebar", "Stack"},
	"span": {"Text", "Typography", "Badge"}, "p": {"Text", "Typography", "Paragraph"}, "label": {"Label", "Text"},
	"h1": {"Heading", "Title", "Typography", "Text"}, "h2": {"Heading", "Title", "Typography", "Text"},
	"h3": {"Heading", "Title", "Typography", "Text"}, "h4": {"Heading", "Title", "Typography", "Text"},
	"button": {"Button", "IconButton"}, "a": {"Link", "Button"}, "img": {"Image", "Avatar", "Icon"},
	"input": {"Input", "TextField", "Checkbox"}, "textarea": {"Textarea", "TextArea"}, "select": {"Select", "Combobox"},
	"ul": {"List"}, "ol": {"List"}, "li": {"ListItem", "List"}, "table": {"Table", "DataTable"}, "hr": {"Separator", "Divider"},
	"svg": {"Icon"}, "form": {"Form", "Stack"},
}

func (k *StrictKit) elementFix(tag string) string {
	var have []string
	for _, n := range suggestFor[strings.ToLower(tag)] {
		if c, ok := k.comps[n]; ok && c.Status != "deprecated" {
			have = append(have, k.importOf(n))
		}
	}
	local := "a block of your own goes into a local component (components/<Name>.tsx) that the page imports"
	if len(have) > 0 {
		return "use " + strings.Join(have, " or ") + "; " + local
	}
	return "find the kit's component (kit_search " + k.c.Kit + " <what it is>); " + local
}

// near are the declared names closest to name, at most three.
func near(name string, names []string) []string {
	type cand struct {
		n string
		d int
	}
	var cs []cand
	low := strings.ToLower(name)
	for _, n := range names {
		d := editDistance(low, strings.ToLower(n))
		if d <= 2 || strings.Contains(strings.ToLower(n), low) || strings.Contains(low, strings.ToLower(n)) {
			cs = append(cs, cand{n, d})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].d < cs[j].d || cs[i].d == cs[j].d && cs[i].n < cs[j].n })
	var out []string
	for i := 0; i < len(cs) && i < 3; i++ {
		out = append(out, cs[i].n)
	}
	return out
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// ---- values ----

var (
	hexRe     = regexp.MustCompile(`(?:^|[\s:(,])(#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4}))\b`)
	colorFnRe = regexp.MustCompile(`\b(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb)\([^()]*\)`)
	lengthRe  = regexp.MustCompile(`(?:^|[\s:(,])(-?(?:\d+\.?\d*|\.\d+)(?:px|rem|em|pt|vh|vw|vmin|vmax|ch))\b`)
	varRe     = regexp.MustCompile(`var\(\s*(--[A-Za-z0-9_-]+)`)
	zeroRe    = regexp.MustCompile(`^-?0*\.?0+[a-z]*$`)
)

// normColor is a colour in a comparable form (#rrggbb[aa] for hex, spaces dropped for the rest), "" when it is not one.
func normColor(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, c := range h {
				b.WriteRune(c)
				b.WriteRune(c)
			}
			h = b.String()
		}
		if len(h) == 8 && strings.HasSuffix(h, "ff") {
			h = h[:6]
		}
		return "#" + h
	}
	if colorFnRe.MatchString(v) {
		return strings.ReplaceAll(v, " ", "")
	}
	return ""
}

type rawValue struct {
	off       int
	text, fix string
	what      string
}

// rawValues finds what is not a token in a CSS-like text: colours, lengths (0 is fine), variables the kit lacks.
func (k *StrictKit) rawValues(s string) []rawValue {
	var out []rawValue
	inVar := map[int]bool{}
	for _, m := range varRe.FindAllStringSubmatchIndex(s, -1) {
		name := s[m[2]:m[3]]
		for i := m[0]; i < m[1]; i++ {
			inVar[i] = true
		}
		if _, ok := k.vars[name]; !ok {
			fix := "use a token's variable (kit_tokens " + k.c.Kit + ")"
			if n := near(name, k.varNames()); len(n) > 0 {
				fix = "use " + strings.Join(n, " or ") + " (kit_tokens " + k.c.Kit + ")"
			}
			out = append(out, rawValue{off: m[2], text: name, what: fmt.Sprintf("the variable %s is not a token of the kit %s", name, k.c.Kit), fix: fix})
		}
	}
	for _, m := range hexRe.FindAllStringSubmatchIndex(s, -1) {
		out = append(out, k.colorValue(m[2], s[m[2]:m[3]]))
	}
	for _, m := range colorFnRe.FindAllStringIndex(s, -1) {
		if !inVar[m[0]] {
			out = append(out, k.colorValue(m[0], s[m[0]:m[1]]))
		}
	}
	for _, m := range lengthRe.FindAllStringSubmatchIndex(s, -1) {
		v := s[m[2]:m[3]]
		if inVar[m[2]] || zeroRe.MatchString(v) {
			continue
		}
		out = append(out, k.lengthValue(m[2], v))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].off < out[j].off })
	return out
}

func (k *StrictKit) varNames() []string {
	out := make([]string, 0, len(k.vars))
	for v := range k.vars {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func (k *StrictKit) tokenRef(name string) string {
	if v := k.tokens[name]; v != "" {
		return fmt.Sprintf("var(%s) (token %s)", v, name)
	}
	return "the token " + name
}

func (k *StrictKit) colorValue(off int, v string) rawValue {
	r := rawValue{off: off, text: v, what: fmt.Sprintf("the colour %s is not a token", v)}
	if t, ok := k.colorTok[normColor(v)]; ok {
		r.fix = "use " + k.tokenRef(t)
	} else {
		r.fix = "pick a colour token (kit_tokens " + k.c.Kit + " color) or a prop of the component that sets it"
	}
	return r
}

func (k *StrictKit) lengthValue(off int, v string) rawValue {
	r := rawValue{off: off, text: v, what: fmt.Sprintf("the length %s is not a token", v)}
	if t, ok := k.plainTok[v]; ok {
		r.fix = "use " + k.tokenRef(t)
	} else {
		r.fix = "use a spacing or radius token (kit_tokens " + k.c.Kit + "), or the component's own prop (gap, size, padding)"
	}
	return r
}

// styleNumberRe: a number given to a size property of a style object (`padding: 12`), which React makes pixels.
var styleNumberRe = regexp.MustCompile(`\b((?:padding|margin|gap|rowGap|columnGap|width|height|minWidth|maxWidth|minHeight|maxHeight|top|left|right|bottom|inset|fontSize|borderRadius|borderWidth|letterSpacing)(?:Top|Bottom|Left|Right|Inline|Block)?)\s*:\s*(-?\d+\.?\d*)\b`)

// ---- the check of a module ----

type strictRun struct {
	k     *StrictKit
	file  string
	src   string
	lines lineTable
	out   []StrictFinding
}

func (r *strictRun) add(rule string, off int, element, what, fix string) {
	if r.lines == nil {
		r.lines = newLineTable(r.src)
	}
	line, col := r.lines.at(off)
	r.out = append(r.out, StrictFinding{Rule: rule, File: r.file, Line: line, Col: col, Element: element, What: what, Fix: fix})
}

func (r *strictRun) values(off int, element, s string) {
	for _, v := range r.k.rawValues(s) {
		r.add("strict.value", off+v.off, element, v.what, v.fix)
	}
}

// exprValues checks the literals of an expression: strings and template parts are CSS-like text; a number given
// to a size property of a style object is a length.
func (r *strictRun) exprValues(off int, element, expr string, style bool) {
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if c == '\'' || c == '"' || c == '`' {
			end := skipString(expr, i)
			if c == '`' {
				end = strings.IndexByte(expr[i+1:], '`')
				if end < 0 {
					end = len(expr)
				} else {
					end += i + 2
				}
			}
			if end-1 > i+1 {
				r.values(off+i+1, element, expr[i+1:end-1])
			}
			i = end - 1
		}
	}
	if style {
		for _, m := range styleNumberRe.FindAllStringSubmatchIndex(expr, -1) {
			v := expr[m[4]:m[5]]
			if zeroRe.MatchString(v) {
				continue
			}
			lv := r.k.lengthValue(0, v+"px")
			r.add("strict.value", off+m[4], element, fmt.Sprintf("%s: %s (pixels) is not a token", expr[m[2]:m[3]], v), lv.fix)
		}
	}
}

// passThrough are attributes every component takes (React's own, and the accessibility and data attributes a library
// passes to its element).
func passThrough(name string) bool {
	return name == "key" || name == "ref" || name == "children" || strings.HasPrefix(name, "aria-") || strings.HasPrefix(name, "data-")
}

// CheckModule checks a page module or a local component. A page (local false) may hold only the kit's components and
// local components imported from the project's files; a local component may hold plain HTML of its own, but its
// values still come from the tokens.
func (k *StrictKit) CheckModule(file, src string, local bool) []StrictFinding {
	r := &strictRun{k: k, file: file, src: src}
	type binding struct {
		kind   string // kit, react, local, other
		module string
		name   string // the imported name
	}
	scope := map[string]binding{}
	for _, imp := range ScanImports(src) {
		kind := "other"
		switch {
		case k.owns(imp.From):
			kind = "kit"
		case imp.From == "react":
			kind = "react"
		case strings.HasPrefix(imp.From, "./") || strings.HasPrefix(imp.From, "../"):
			kind = "local"
		}
		for localName, imported := range imp.Names {
			scope[localName] = binding{kind: kind, module: imp.From, name: imported}
		}
	}
	for _, el := range ScanJSX(src) {
		if el.Name == "" {
			continue
		}
		head, rest, dotted := strings.Cut(el.Name, ".")
		intrinsic := !dotted && strings.ToLower(el.Name[:1]) == el.Name[:1] && el.Name[:1] != "_" && el.Name[:1] != "$"
		var comp *Component
		switch b, ok := scope[head]; {
		case intrinsic:
			if !local {
				r.add("strict.element", el.Off, el.Name, fmt.Sprintf("<%s> is plain HTML, not a component of the kit %s", el.Name, k.c.Kit), k.elementFix(el.Name))
			}
		case !ok:
			if !local {
				r.add("strict.element", el.Off, el.Name, fmt.Sprintf("<%s> is written in this page, not taken from the kit %s", el.Name, k.c.Kit),
					"take the kit's component, or move <"+head+"> into components/"+head+".tsx and import it from there")
			}
		case b.kind == "react" || b.kind == "local":
			// React's own (Fragment, Suspense) and the project's local components.
		case b.kind == "other":
			r.add("strict.element", el.Off, el.Name, fmt.Sprintf("<%s> comes from %s, not from the kit %s", el.Name, b.module, k.c.Kit), k.elementFix(""))
		default:
			id := ""
			if b.name == "*" {
				id = k.byImport[b.module][rest]
				rest, dotted = "", false
			} else {
				id = k.byImport[b.module][b.name]
			}
			if id != "" && dotted {
				if _, ok := k.comps[id+"."+rest]; ok {
					id += "." + rest
				} else if _, ok := k.comps[id+rest]; ok {
					id += rest
				} else {
					id = ""
				}
			}
			comp = k.comps[id]
			if comp == nil {
				r.add("strict.element", el.Off, el.Name, fmt.Sprintf("<%s> is not a component the kit %s exports from %s", el.Name, k.c.Kit, b.module),
					k.exportsHint(b.module, el.Name))
			}
		}
		if comp != nil {
			r.component(comp, el)
			continue
		}
		for _, a := range el.Attrs {
			r.attrValues(el.Name, a, nil)
		}
	}
	return r.out
}

func (k *StrictKit) exportsHint(module, name string) string {
	var names []string
	for n := range k.byImport[module] {
		names = append(names, n)
	}
	sort.Strings(names)
	if n := near(name, names); len(n) > 0 {
		return "did you mean " + strings.Join(n, " or ") + "? " + module + " exports: " + clip(strings.Join(names, ", "), 400)
	}
	if len(names) == 0 {
		return "the kit exports nothing from " + module + "; kit_search " + k.c.Kit + " finds the component"
	}
	return module + " exports: " + clip(strings.Join(names, ", "), 400)
}

// component checks a kit component's use: deprecated, props, values.
func (r *strictRun) component(comp *Component, el JSXElement) {
	k := r.k
	if comp.Status == "deprecated" {
		fix := "the kit names no replacement: kit_component " + k.c.Kit + " " + comp.ID + " says what to take"
		if comp.ReplacedBy != "" {
			fix = "use " + k.importOf(comp.ReplacedBy)
		}
		r.add("strict.deprecated", el.Off, el.Name, fmt.Sprintf("%s is deprecated in the kit %s", comp.ID, k.c.Kit), fix)
	}
	props := k.propsOf(comp)
	byName := map[string]Prop{}
	var names []string
	for _, p := range props {
		byName[p.Name] = p
		names = append(names, p.Name)
	}
	for _, a := range el.Attrs {
		if a.Kind == '.' {
			continue
		}
		p, ok := byName[a.Name]
		if !ok && !passThrough(a.Name) {
			fix := comp.ID + " declares: " + strings.Join(names, ", ")
			if len(names) == 0 {
				fix = comp.ID + " declares no props"
			}
			if n := near(a.Name, names); len(n) > 0 {
				fix = "did you mean " + strings.Join(n, " or ") + "? " + fix
			}
			if a.Name == "style" || a.Name == "className" || a.Name == "class" {
				fix = "styles come from the component's props and the kit's tokens; a look the kit lacks is a local component (components/<Name>.tsx). " + fix
			}
			r.add("strict.prop", a.At, el.Name, fmt.Sprintf("%s has no prop %q", comp.ID, a.Name), fix)
		}
		r.attrValues(el.Name, a, &p)
	}
}

// attrValues checks the values an attribute carries; p is the declared prop, when there is one.
func (r *strictRun) attrValues(element string, a JSXAttr, p *Prop) {
	k := r.k
	switch a.Kind {
	case 's':
		if p != nil && p.Type == "enum" && len(p.Values) > 0 && !contains(p.Values, a.Value) {
			r.add("strict.prop", a.Off, element, fmt.Sprintf("%s=%q is not one of its values", a.Name, a.Value), "one of: "+strings.Join(p.Values, ", "))
			return
		}
		if p != nil && (p.Type == "token" || p.Type == "color") {
			if _, ok := k.tokens[a.Value]; ok {
				return
			}
			if m := varRe.FindStringSubmatch(a.Value); m != nil {
				if _, ok := k.vars[m[1]]; ok {
					return
				}
			}
			fix := "a token name of the kit (kit_tokens " + k.c.Kit + ")"
			if t, ok := k.colorTok[normColor(a.Value)]; ok {
				fix = "the token " + t
			} else if t, ok := k.plainTok[a.Value]; ok {
				fix = "the token " + t
			}
			r.add("strict.value", a.Off, element, fmt.Sprintf("%s=%q is not a token", a.Name, a.Value), fix)
			return
		}
		r.values(a.Off, element, a.Value)
	case 'e':
		r.exprValues(a.Off, element, a.Value, a.Name == "style")
	}
}

// ---- the check of a board (.dc.html) ----

var (
	createElementRe = regexp.MustCompile(`\b(?:React\.createElement|createElement|h)\(\s*(['"])([a-z][a-z0-9-]*)['"]`)
	stringLitRe     = regexp.MustCompile(`'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"`)
)

// boardAllowed are the elements of a board that are its frame, not its content.
var boardAllowed = map[string]bool{"x-dc": true, "helmet": true, "style": true, "script": true, "template": true}

// CheckBoard checks a board written by hand: its <x-import> elements against the kit, every other element inside
// <x-dc> as not from the kit, its styles and style attributes for raw values, and the elements renderVals() makes
// with createElement('div', …). local says whether a component file a board imports (from="…") is the project's own.
func (k *StrictKit) CheckBoard(file, src string, local func(from string) bool) []StrictFinding {
	r := &strictRun{k: k, file: file, src: src}
	z := html.NewTokenizer(strings.NewReader(src))
	off := 0
	inDC := 0
	var stack []string
	for {
		tt := z.Next()
		raw := z.Raw()
		at := off
		off += len(raw)
		switch tt {
		case html.ErrorToken:
			return r.out
		case html.TextToken:
			if len(stack) == 0 {
				continue
			}
			switch stack[len(stack)-1] {
			case "style":
				r.values(at, "<style>", string(raw))
			case "script@x-dc":
				r.script(at, string(raw))
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			n := string(name)
			for i := len(stack) - 1; i >= 0; i-- {
				if strings.TrimSuffix(stack[i], "@x-dc") == n {
					stack = stack[:i]
					break
				}
			}
			if n == "x-dc" && inDC > 0 {
				inDC--
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, more := z.TagName()
			n := string(name)
			attrs := map[string]string{}
			var order []string
			for more {
				var key, val []byte
				key, val, more = z.TagAttr()
				attrs[string(key)] = string(val)
				order = append(order, string(key))
			}
			if s, ok := attrs["style"]; ok && s != "" {
				r.values(at+strings.Index(string(raw), "style"), "<"+n+">", s)
			}
			entry := n
			if n == "script" && attrs["type"] == "text/x-dc" {
				entry = "script@x-dc"
			}
			switch {
			case n == "x-dc":
				inDC++
			case inDC == 0 || boardAllowed[n] || inHelmet(stack):
			case n == "main" && len(stack) > 0 && stack[len(stack)-1] == "x-dc":
			case n == "x-import":
				r.ximport(at, string(raw), attrs, order, local)
			default:
				r.add("strict.element", at, n, fmt.Sprintf("<%s> is plain HTML, not a component of the kit %s", n, k.c.Kit), k.elementFix(n))
			}
			if tt == html.StartTagToken && !voidElement[n] {
				stack = append(stack, entry)
			}
		}
	}
}

var voidElement = map[string]bool{"meta": true, "link": true, "br": true, "img": true, "input": true, "hr": true, "source": true}

func inHelmet(stack []string) bool {
	for _, s := range stack {
		if s == "helmet" || s == "head" {
			return true
		}
	}
	return false
}

func (r *strictRun) ximport(at int, raw string, attrs map[string]string, order []string, local func(string) bool) {
	k := r.k
	if from, ok := attrs["from"]; ok {
		if local == nil || !local(from) {
			r.add("strict.element", at, "x-import", fmt.Sprintf("the component file %q is not a file of this project", from),
				"a local component lives in the project (components/<Name>.tsx); the kit's components come by component-from-global-scope")
		}
		return
	}
	ref := attrs["component-from-global-scope"]
	ns, id, _ := strings.Cut(ref, ".")
	comp := k.comps[id]
	if ref == "" || !k.ns[ns] || comp == nil {
		fix := "write component-from-global-scope=\"" + k.c.Namespace + ".<Component>\" with a component of the kit (kit_index " + k.c.Kit + ")"
		if n := near(id, k.ids); len(n) > 0 && k.ns[ns] {
			fix = "did you mean " + ns + "." + strings.Join(n, " or "+ns+".") + "?"
		}
		r.add("strict.element", at, ref, fmt.Sprintf("%q is not a component of the kit %s", ref, k.c.Kit), fix)
		return
	}
	el := JSXElement{Name: ref, Off: at}
	for _, a := range order {
		if a == "component-from-global-scope" {
			continue
		}
		v := attrs[a]
		where := at
		if i := strings.Index(raw, " "+a); i >= 0 {
			where = at + i + 1
		}
		attr := JSXAttr{Name: camel(a), Kind: 's', Value: v, Off: where, At: where}
		if strings.HasPrefix(v, "{{") && strings.HasSuffix(v, "}}") {
			attr.Kind, attr.Value = 'e', strings.TrimSpace(v[2:len(v)-2])
		}
		if a == "style" || a == "class" {
			attr.Name = a
		}
		if passThrough(a) {
			attr.Name = a
		}
		el.Attrs = append(el.Attrs, attr)
	}
	r.component(comp, el)
}

// camel is a kebab-case attribute as the prop it sets (sub-tone -> subTone).
func camel(s string) string {
	parts := strings.Split(s, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// script checks the board's renderVals(): elements made of plain HTML, and raw values in its strings.
func (r *strictRun) script(at int, code string) {
	for _, m := range createElementRe.FindAllStringSubmatchIndex(code, -1) {
		tag := code[m[4]:m[5]]
		r.add("strict.element", at+m[4], tag, fmt.Sprintf("renderVals() makes <%s>, plain HTML, not a component of the kit %s", tag, r.k.c.Kit), r.k.elementFix(tag))
	}
	for _, m := range stringLitRe.FindAllStringIndex(code, -1) {
		r.values(at+m[0]+1, "renderVals()", code[m[0]+1:m[1]-1])
	}
}

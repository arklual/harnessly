package kitadd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/designkit/formats"
)

// obj is a JSON object that keeps its keys in the order they were written, so a person's file is rewritten without
// its members moving around.
type obj struct {
	keys []string
	vals map[string]json.RawMessage
}

func newObj() *obj { return &obj{vals: map[string]json.RawMessage{}} }

func parseObj(raw []byte) (*obj, bool) {
	o := newObj()
	if json.Unmarshal(raw, &o.vals) != nil {
		return nil, false
	}
	o.keys = orderedKeys(raw)
	return o, true
}

func (o *obj) has(k string) bool { _, ok := o.vals[k]; return ok }

func (o *obj) set(k string, v any) {
	raw, _ := json.Marshal(v)
	if !o.has(k) {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = raw
}

func (o *obj) del(k string) {
	if !o.has(k) {
		return
	}
	delete(o.vals, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *obj) str(k string) string {
	var s string
	_ = json.Unmarshal(o.vals[k], &s)
	return s
}

func (o *obj) sub(k string) *obj {
	if s, ok := parseObj(o.vals[k]); ok {
		return s
	}
	return nil
}

func (o *obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kr, _ := json.Marshal(k)
		b.Write(kr)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func pretty(v any) []byte {
	raw, _ := json.Marshal(v)
	var b bytes.Buffer
	_ = json.Indent(&b, raw, "", "  ")
	b.WriteByte('\n')
	return b.Bytes()
}

// oldKit is the kit folder as an earlier run (and the person after it) left it.
type oldKit struct {
	id, version, namespace, title, root string
	doc                                 *obj
	groups                              []*obj
	comps                               map[string]*obj
}

func readOld(folder string) (*oldKit, error) {
	raw, err := os.ReadFile(filepath.Join(folder, designkit.ManifestFile))
	if err != nil {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(folder, filepath.FromSlash(SourceFile))); err != nil {
		return nil, fmt.Errorf("%s holds a kit that `kit add` did not make (no %s): add into an empty folder", folder, SourceFile)
	}
	doc, ok := parseObj(raw)
	if !ok {
		return nil, fmt.Errorf("%s/%s is not valid JSON", folder, designkit.ManifestFile)
	}
	k := &oldKit{doc: doc, comps: map[string]*obj{}, namespace: doc.str("namespace"), title: doc.str("title")}
	if rec := doc.sub("kit"); rec != nil {
		k.id, k.version, k.root = rec.str("id"), rec.str("version"), rec.str("root")
	}
	var groups []json.RawMessage
	_ = json.Unmarshal(doc.vals["groups"], &groups)
	for _, g := range groups {
		go_, ok := parseObj(g)
		if !ok {
			continue
		}
		k.groups = append(k.groups, go_)
		var cs []json.RawMessage
		_ = json.Unmarshal(go_.vals["components"], &cs)
		for _, c := range cs {
			if co, ok := parseObj(c); ok && co.str("id") != "" {
				k.comps[co.str("id")] = co
			}
		}
	}
	return k, nil
}

// kitProp is a prop as design-system.json writes it.
type kitProp struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Values      []string `json:"values,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
}

func kitProps(ps []TypeProp, old []kitProp) []kitProp {
	had := map[string]kitProp{}
	for _, p := range old {
		had[p.Name] = p
	}
	out := []kitProp{}
	for _, p := range ps {
		kp := kitProp(p)
		if kp.Type == "enum" && len(kp.Values) == 0 {
			kp.Type = "string"
		}
		if kp.Description == "" {
			kp.Description = had[p.Name].Description // written by a person
		}
		out = append(out, kp)
	}
	return out
}

func propSig(p kitProp) string {
	s := p.Type
	if len(p.Values) > 0 {
		s += "(" + strings.Join(p.Values, "|") + ")"
	}
	if p.Required {
		s += " required"
	}
	return s
}

// diffProps is what changed in a component's props, for the report.
func diffProps(id string, old, now []kitProp) *PropChange {
	o, n := map[string]kitProp{}, map[string]kitProp{}
	for _, p := range old {
		o[p.Name] = p
	}
	for _, p := range now {
		n[p.Name] = p
	}
	ch := &PropChange{Component: id}
	for _, p := range now {
		if q, ok := o[p.Name]; !ok {
			ch.Added = append(ch.Added, p.Name)
		} else if propSig(q) != propSig(p) {
			ch.Changed = append(ch.Changed, fmt.Sprintf("%s: %s → %s", p.Name, propSig(q), propSig(p)))
		}
	}
	for _, p := range old {
		if _, ok := n[p.Name]; !ok {
			ch.Removed = append(ch.Removed, p.Name)
		}
	}
	if len(ch.Added)+len(ch.Removed)+len(ch.Changed) == 0 {
		return nil
	}
	return ch
}

// humanize: CardTitle → "Card title".
func humanize(id string) string {
	var b strings.Builder
	for i, r := range id {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// valueFor is a value the live preview can pass for a required prop.
func valueFor(p kitProp) any {
	switch p.Type {
	case "enum":
		return p.Values[0]
	case "boolean":
		return false
	case "number":
		return 0
	case "node":
		return titleCase(humanize(p.Name))
	case "function":
		return map[string]any{"$fn": ""}
	case "array":
		return []any{}
	case "object":
		if textLikeRe.MatchString(p.Name) {
			return titleCase(humanize(p.Name)) // typed as an element alias the analysis could not see through: a text draws
		}
		return map[string]any{}
	}
	return titleCase(humanize(p.Name))
}

var tooltipRe = regexp.MustCompile(`Tooltip$`)

var textLikeRe = regexp.MustCompile(`^(content|title|label|text|description|message|heading|header|caption|tooltip|placeholder|children)$`)

var (
	overlayRe   = regexp.MustCompile(`(?i)modal|drawer|dialog|tour|sheet|popover|popconfirm|tooltip|dropdown|snackbar|backdrop|popup|overlay`)
	listPropRe  = regexp.MustCompile(`^(items|options|dataSource|data|treeData|steps|tabs|list|rows|slides|panels|segments|links|entries)$`)
	bodyItemsRe = regexp.MustCompile(`(?i)tabs|collapse|accordion|descriptions|carousel|timeline|segmented`)
	carouselRe  = regexp.MustCompile(`(?i)^(carousel|slider|swiper)$`)
	masonryRe   = regexp.MustCompile(`(?i)masonry|waterfall`)
	imageRe     = regexp.MustCompile(`(?i)^(image|picture|thumbnail|photo)$`)
)

// placeholderImage is a plain grey picture for a first sample, inline so it needs no network.
const placeholderImage = "data:image/svg+xml;utf8,%3Csvg xmlns='http://www.w3.org/2000/svg' width='160' height='100'%3E%3Crect width='160' height='100' fill='%23d9d9d9'/%3E%3C/svg%3E"

// illustrative is a value for an optional prop whose absence leaves a component empty: a list gets two plain entries,
// an overlay is open. Only a first sample; a person edits it.
func illustrative(id string, p kitProp) (any, bool) {
	switch {
	case p.Type == "boolean" && (p.Name == "open" || p.Name == "visible") && overlayRe.MatchString(id):
		return true, true
	case p.Type == "string" && p.Name == "src" && imageRe.MatchString(id):
		return placeholderImage, true
	case (p.Type == "array" || p.Type == "node" && p.Name == "items") && listPropRe.MatchString(p.Name):
		items := make([]any, 2)
		for i, n := range []string{"One", "Two"} {
			it := map[string]any{"key": strings.ToLower(n), "value": strings.ToLower(n), "label": n, "title": n, "description": n + " description"}
			if bodyItemsRe.MatchString(id) {
				it["children"] = n + " content"
			}
			if p.Name == "items" && masonryRe.MatchString(id) {
				it["data"] = n
				it["height"] = 60 + 40*i
			}
			items[i] = it
		}
		return items, true
	}
	return nil, false
}

func hasProp(ps []kitProp, name string) bool {
	for _, p := range ps {
		if p.Name == name {
			return true
		}
	}
	return false
}

// sampleFor is a first sample of a new component: its label as children, a value for every required prop, and for a
// compound component its parts inside it, so they get a picture. A component the live preview cannot draw alone loses
// it after the render (DropSamples).
func sampleFor(id string, props []kitProp, parts []string, partProps map[string][]kitProp, filler string) map[string]any {
	s := map[string]any{}
	listed := false
	for _, p := range props {
		if p.Required && p.Name != "children" {
			s[p.Name] = valueFor(p)
			if elementTypeProp(p) {
				s[p.Name] = "div" // a tag is a valid component type; an empty object makes React throw
			}
			if v, ok := illustrative(id, p); ok {
				s[p.Name] = v // a required "open" of an overlay draws it open
			}
		} else if v, ok := illustrative(id, p); ok {
			s[p.Name] = v
			if p.Type != "boolean" && p.Name != "src" {
				listed = true
			}
		}
	}
	shown := shownProp(props)
	if shown != "" {
		s[shown] = true // an overlay, a collapsible and a transition draw their content only while shown
	}
	if listed {
		// the entries are the content: a label as children would replace them (a Tabs, a Cascader's trigger)
		return s
	}
	if carouselRe.MatchString(id) && hasProp(props, "children") {
		s["children"] = []any{"One", "Two", "Three"}
		return s
	}
	if !hasProp(props, "children") {
		if tbl := tableParts(id, parts, partProps); tbl != nil {
			s["children"] = tbl // a polymorphic table lists no children, yet it is written as head, body, rows
		}
		return s
	}
	if shown != "" && len(parts) > 0 {
		if nested := composeOpen(parts, partProps, filler); len(nested) > 0 {
			s["children"] = nested
			return s
		}
	}
	if filler != "" && tooltipRe.MatchString(id) {
		s["children"] = map[string]any{"$c": filler, "children": humanize(id)} // a tooltip points at one element
		return s
	}
	if (shown == "in" || shown == "present") && len(parts) == 0 && filler != "" {
		s["children"] = map[string]any{"$c": filler, "children": humanize(id)} // a transition moves an element
		return s
	}
	kids := flatParts(id, parts, partProps, filler)
	if len(kids) > 0 {
		s["children"] = kids
	} else {
		s["children"] = humanize(id)
	}
	return s
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// The keys of a component in the order a new one is written.
var compOrder = []string{"id", "name", "part", "description", "keywords", "code", "props", "variants", "sample", "todo"}

// writeKit writes every file of the kit: what comes from the package anew, what a person wrote kept.
func writeKit(o *Options, b *built, old *oldKit, rep *Report) error {
	dir := o.Folder
	write := func(rel string, data []byte) error {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		rep.Wrote = append(rep.Wrote, rel)
		return os.WriteFile(p, data, 0o644)
	}
	layer := "kit-" + o.ID
	header := fmt.Sprintf("/* %s %s, built by harnessly design kit add: React and ReactDOM come from the page. */\n", b.pkg, b.version)
	if err := write("components/bundle.js", append([]byte(header), b.js...)); err != nil {
		return err
	}
	if err := write("components/bundle.css", []byte(layerCSS(string(b.css), layer))); err != nil {
		return err
	}

	// Tokens: the package's CSS variables; the usage a person wrote stays with its token.
	modes := []formats.Mode{{ID: "light", Name: "Light"}, {ID: "dark", Name: "Dark"}}
	toks, trep, err := formats.ImportCSS(b.tokensCSS, formats.CSSOptions{Modes: modes})
	if err != nil {
		rep.NotCarried = append(rep.NotCarried, "tokens: "+err.Error())
		toks, _ = formats.ParseTokens([]byte(`{}`))
	} else {
		// Hundreds of component selectors say one thing: count them.
		scoped, mixed := 0, 0
		for _, l := range trep.NotCarried {
			switch {
			case strings.Contains(l, "component- or page-specific"):
				scoped++
			case strings.Contains(l, "cannot be a kit colour"):
				mixed++
			default:
				rep.NotCarried = append(rep.NotCarried, "tokens: "+l)
			}
		}
		if scoped > 0 {
			rep.NotCarried = append(rep.NotCarried, fmt.Sprintf("tokens: variables under %d component or page selectors are the components' own, not tokens", scoped))
		}
		if mixed > 0 {
			rep.NotCarried = append(rep.NotCarried, fmt.Sprintf("tokens: %d values made of others (color-mix(), var() in calc()) are not read from the stylesheet: the browser resolves them after the kit is drawn", mixed))
		}
	}
	oldToks, _, _ := formats.LoadTokens(dir)
	for fi := range toks.Families {
		f := &toks.Families[fi]
		kept := f.Tokens[:0]
		for _, t := range f.Tokens {
			if strings.HasPrefix(t.Name, "tw-") { // Tailwind's own working variables, not the design's
				continue
			}
			if oldToks != nil {
				if ot, _ := oldToks.Find(t.Name); ot != nil && ot.Usage != "" && t.Usage == "" {
					t.Usage = ot.Usage
				}
			}
			kept = append(kept, t)
		}
		f.Tokens = kept
	}
	toks.Name = o.Title
	rep.Tokens = toks.Count()
	if rep.Tokens == 0 {
		rep.NotCarried = append(rep.NotCarried, "tokens: the package's CSS defines no variables under :root, [data-theme] or .dark; tokens.json is empty")
	}
	if err := write("tokens.json", toks.Marshal()); err != nil {
		return err
	}
	tcss, _ := formats.ExportCSS(toks, formats.KitInfo{ID: o.ID, Title: o.Title})
	if err := write("tokens.css", tcss); err != nil {
		return err
	}

	// design-system.json.
	doc := newObj()
	if old != nil {
		doc = old.doc
	}
	doc.set("v", 3)
	doc.set("title", o.Title)
	doc.set("namespace", o.Namespace)
	if !doc.has("libraries") {
		doc.set("libraries", []any{})
	}
	rec := newObj()
	if old != nil && old.doc.sub("kit") != nil {
		rec = old.doc.sub("kit")
	}
	rec.set("id", o.ID)
	rec.set("version", b.version)
	rec.set("source", "library")
	rec.set("package", b.kitPackage)
	if b.root != "" {
		rec.set("root", b.root)
	} else {
		rec.del("root")
	}
	rec.set("cssLayer", layer)
	if b.react != "" {
		rec.set("react", b.react)
	} else {
		rec.del("react")
	}
	doc.set("kit", rec)
	if !doc.has("sections") {
		doc.set("sections", map[string]any{"foundation": map[string]string{"title": "Foundation"},
			"components": map[string]string{"title": "Components"}, "templates": map[string]string{"title": "Page templates"},
			"assets": map[string]string{"title": "Assets"}})
	}

	gen := map[string]genComp{}
	var partsOf = map[string][]string{}
	byPos := append([]genComp(nil), b.comps...)
	sort.SliceStable(byPos, func(i, j int) bool { return byPos[i].Pos < byPos[j].Pos })
	for _, c := range byPos {
		gen[c.ID] = c
		// A portal or an overlay has nothing to show of its own in a sample.
		if c.Part != "" && c.Part != designkit.PartProvider && !strings.HasSuffix(c.ID, "Portal") && !strings.HasSuffix(c.ID, "Overlay") {
			partsOf[c.Part] = append(partsOf[c.Part], c.ID)
		}
	}
	newProps := map[string][]kitProp{}
	for _, c := range b.comps {
		var had []kitProp
		if old != nil && old.comps[c.ID] != nil {
			_ = json.Unmarshal(old.comps[c.ID].vals["props"], &had)
		}
		newProps[c.ID] = kitProps(c.Props, had)
	}
	merged := func(c genComp) *obj {
		co := newObj()
		fresh := true
		if old != nil && old.comps[c.ID] != nil {
			co, fresh = old.comps[c.ID], false
		}
		props := newProps[c.ID]
		if fresh {
			props = declareRecipeProps(b.pkg, c.ID, props)
		}
		if !fresh {
			var had []kitProp
			_ = json.Unmarshal(co.vals["props"], &had)
			if ch := diffProps(c.ID, had, props); ch != nil {
				rep.Changed = append(rep.Changed, *ch)
			}
		}
		for _, k := range compOrder {
			switch k {
			case "id":
				co.set("id", c.ID)
			case "name":
				if !co.has("name") {
					co.set("name", c.ID)
				}
			case "part":
				if fresh && c.Part != "" {
					co.set("part", c.Part)
				}
			case "description":
				if co.str("description") == "" {
					d := b.docs[c.ID].Description
					if d == "" {
						d = c.Description
					}
					if d == "" {
						d = fmt.Sprintf("%s from %s; what it is for is not written yet.", c.ID, b.pkg)
					}
					co.set("description", d)
				}
			case "keywords":
				if fresh && len(b.docs[c.ID].Keywords) > 0 && !co.has("keywords") {
					co.set("keywords", b.docs[c.ID].Keywords)
				}
			case "code":
				if c.Code != "" {
					co.set("code", map[string]string{"import": c.Code, "export": c.ID})
				}
			case "props":
				co.set("props", props)
			case "variants", "sample":
				if fresh && c.Part == "" {
					if k == "sample" && len(c.Sample) > 0 {
						co.set("sample", c.Sample)
					} else if k == "sample" {
						smp := sampleFor(c.ID, props, partsOf[c.ID], newProps, fillerFor(func(n string) bool { _, ok := gen[n]; return ok }))
						if r := recipeSample(b.pkg, c.ID, func(n string) bool { _, ok := gen[n]; return ok }, props); r != nil {
							for k, v := range smp { // what the component requires and the recipe leaves out (MUI's `component`)
								if _, has := r[k]; !has && k != "children" {
									r[k] = v
								}
							}
							smp = r
						}
						co.set("sample", smp)
						if _, has := smp["children"]; has && !hasProp(props, "children") {
							props = append(props, kitProp{Name: "children", Type: "node", Description: "The rows of the table: a head and a body."})
							co.set("props", props) // the live preview passes only declared props
						}
					} else if vs := variantsFor(props); len(vs) > 0 {
						co.set("variants", vs)
					}
				}
			case "todo":
				if fresh {
					var todo []string
					if _, ok := b.docs[c.ID]; ok {
						break
					}
					if c.Description == "" {
						todo = append(todo, "description: what it is for and when to take it (the package has no doc comment for it)")
					}
					if c.Part == "" {
						todo = append(todo, "card: «When to use» and «When not to use» (name what to take instead)")
					}
					if len(todo) > 0 {
						co.set("todo", todo)
					}
				}
			}
		}
		if !fresh {
			fixSamples(co, props, gen, rep)
		}
		return co
	}

	// Groups: a component stays in the group it is in; a new one goes to "components".
	placed := map[string]bool{}
	var groups []*obj
	if old != nil {
		for _, g := range old.groups {
			var cs []json.RawMessage
			_ = json.Unmarshal(g.vals["components"], &cs)
			var keep []*obj
			for _, raw := range cs {
				co, ok := parseObj(raw)
				if !ok {
					continue
				}
				id := co.str("id")
				c, ok := gen[id]
				switch {
				case ok:
					keep = append(keep, merged(c))
					placed[id] = true
				case co.has("preset"):
					keep = append(keep, co) // a preset is the kit's own, not the package's
				default:
					rep.Removed = append(rep.Removed, id)
				}
			}
			if len(keep) == 0 {
				continue
			}
			g.set("components", keep)
			groups = append(groups, g)
		}
	}
	var fresh []*obj
	for _, c := range b.comps {
		if !placed[c.ID] {
			fresh = append(fresh, merged(c))
			if old != nil {
				rep.Added = append(rep.Added, c.ID)
			}
		}
	}
	if len(fresh) > 0 {
		var into *obj
		for _, g := range groups {
			if g.str("id") == "components" {
				into = g
			}
		}
		if into == nil {
			into = newObj()
			into.set("id", "components")
			into.set("title", "Components")
			into.set("components", []any{})
			groups = append(groups, into)
		}
		var cs []json.RawMessage
		_ = json.Unmarshal(into.vals["components"], &cs)
		for _, f := range fresh {
			raw, _ := f.MarshalJSON()
			cs = append(cs, raw)
		}
		into.set("components", cs)
	}
	doc.set("groups", groups)
	for _, k := range []string{"assetGroups", "templates"} {
		if !doc.has(k) {
			if k == "templates" {
				doc.set(k, []any{})
			} else {
				doc.set(k, map[string]any{})
			}
		}
	}
	if err := write(designkit.ManifestFile, pretty(doc)); err != nil {
		return err
	}

	// Cards: a new one is a draft; an existing one gets its Props and In code sections anew, the rest is the person's.
	for _, c := range b.comps {
		card := "components/" + c.ID + "/README.md"
		have, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(card)))
		var md string
		if err != nil {
			md = newCard(c, props(newProps, c.ID), o.Namespace, b.pkg, b.docs[c.ID])
		} else {
			md = string(have)
		}
		md = replaceSection(md, "Props", propsTable(newProps[c.ID]))
		if c.Code != "" {
			md = replaceSection(md, "In code", inCode(c))
		}
		if err == nil && md == string(have) {
			continue
		}
		if err := write(card, []byte(md)); err != nil {
			return err
		}
	}
	for _, id := range rep.Removed {
		if _, err := os.Stat(filepath.Join(dir, "components", id, "README.md")); err == nil {
			rep.ToWrite = append(rep.ToWrite, fmt.Sprintf("components/%s/README.md: %s is gone from the package; its card is kept, delete it when nothing needs it", id, id))
		}
	}
	src := sourceRecord{Source: o.Source.String(), CSS: o.CSS, Registry: o.Registry}
	if o.Source.Kind == "npm" {
		src.Source = "npm:" + b.pkg + "@" + b.version
		src.Root = o.Root
		src.React = b.react
	}
	if err := write(SourceFile, pretty(src)); err != nil {
		return err
	}
	if err := writeReadme(o, b, old == nil, rep); err != nil {
		return err
	}
	for _, c := range b.comps {
		switch {
		case c.Part == designkit.PartProvider:
			rep.Providers++
		case c.Part != "":
			rep.Parts++
		default:
			rep.Components++
		}
	}
	sort.Strings(rep.Added)
	sort.Strings(rep.Removed)
	sort.Slice(rep.Changed, func(i, j int) bool { return rep.Changed[i].Component < rep.Changed[j].Component })
	nd := map[string]int{}
	for _, d := range b.docs {
		nd[d.DescSrc]++
	}
	if n := nd[srcDraft] + nd[srcGenerated]; n > 0 {
		rep.ToWrite = append(rep.ToWrite, fmt.Sprintf("%d components have cards written by a model or from names (listed under Status in each card): read them once", n))
	}
	if n := countTodo(dir); n > 0 {
		rep.ToWrite = append(rep.ToWrite, fmt.Sprintf("%d components list in \"todo\" what a person writes: a description, «When to use» and «When not to use» in the card", n))
	}
	return nil
}

func props(m map[string][]kitProp, id string) []kitProp { return m[id] }

// variantsFor is a variant per value of the component's `variant` prop.
func variantsFor(props []kitProp) []map[string]any {
	for _, p := range props {
		if p.Name == "variant" && p.Type == "enum" && len(p.Values) > 1 {
			var vs []map[string]any
			values := append([]string(nil), p.Values...)
			// The first variant is what the picture shows: the library's default look when it has one.
			for i, v := range values {
				if v == "default" && i > 0 {
					values = append(append([]string{v}, values[:i]...), values[i+1:]...)
					break
				}
			}
			for _, v := range values {
				vs = append(vs, map[string]any{"id": v, "name": titleCase(v), "props": map[string]any{"variant": v}})
			}
			return vs
		}
	}
	return nil
}

// fixSamples keeps a person's sample and variants valid against the new props: a prop that is gone is taken out, a
// new required one gets a value, an enum value that is gone is replaced by the first.
func fixSamples(co *obj, props []kitProp, gen map[string]genComp, rep *Report) {
	by := map[string]kitProp{}
	for _, p := range props {
		by[p.Name] = p
	}
	id := co.str("id")
	fix := func(where string, m map[string]any, required bool) {
		for k, v := range m {
			p, ok := by[k]
			if !ok {
				delete(m, k)
				rep.ToWrite = append(rep.ToWrite, fmt.Sprintf("%s: %s took out %s (the prop is gone)", id, where, k))
				continue
			}
			if s, isStr := v.(string); isStr && p.Type == "enum" && !contains(p.Values, s) {
				m[k] = p.Values[0]
				rep.ToWrite = append(rep.ToWrite, fmt.Sprintf("%s: %s %s=%q is not a value any more, set to %q", id, where, k, s, p.Values[0]))
			}
		}
		if required {
			for _, p := range props {
				if _, ok := m[p.Name]; !ok && p.Required && p.Name != "children" {
					m[p.Name] = valueFor(p)
					rep.ToWrite = append(rep.ToWrite, fmt.Sprintf("%s: the sample got a value for the new required prop %s", id, p.Name))
				}
			}
		}
	}
	if co.has("sample") {
		var s map[string]any
		if json.Unmarshal(co.vals["sample"], &s) == nil && s != nil {
			before := string(co.vals["sample"])
			fix("the sample", s, true)
			if raw, _ := json.Marshal(s); string(raw) != before {
				co.set("sample", s)
			}
		}
	}
	if co.has("variants") {
		var vs []map[string]any
		if json.Unmarshal(co.vals["variants"], &vs) == nil {
			changed := false
			for _, v := range vs {
				if ps, ok := v["props"].(map[string]any); ok {
					before, _ := json.Marshal(ps)
					fix("variant "+fmt.Sprint(v["id"]), ps, false)
					if after, _ := json.Marshal(ps); string(after) != string(before) {
						changed = true
					}
				}
			}
			if changed {
				co.set("variants", vs)
			}
		}
	}
	_ = gen
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func countTodo(dir string) int {
	raw, err := os.ReadFile(filepath.Join(dir, designkit.ManifestFile))
	if err != nil {
		return 0
	}
	var d struct {
		Groups []struct {
			Components []struct {
				Todo []string `json:"todo"`
			} `json:"components"`
		} `json:"groups"`
	}
	_ = json.Unmarshal(raw, &d)
	n := 0
	for _, g := range d.Groups {
		for _, c := range g.Components {
			if len(c.Todo) > 0 {
				n++
			}
		}
	}
	return n
}

func newCard(c genComp, props []kitProp, ns, pkg string, doc compDoc) string {
	var b strings.Builder
	desc := doc.Description
	if desc == "" {
		desc = c.Description
	}
	if desc == "" {
		desc = fmt.Sprintf("%s from %s; what it is for is not written yet.", c.ID, pkg)
	}
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", c.ID, desc)
	if len(doc.When) > 0 {
		b.WriteString("## When to use\n\n")
		for _, l := range doc.When {
			b.WriteString("- " + l + "\n")
		}
		b.WriteString("\n")
	}
	if len(doc.WhenNot) > 0 {
		b.WriteString("## When not to use\n\n")
		for _, l := range doc.WhenNot {
			b.WriteString("- " + l + "\n")
		}
		b.WriteString("\n")
	}
	if c.Part != "" && c.Part != designkit.PartProvider {
		fmt.Fprintf(&b, "A part of `%s`: it is drawn inside it, never alone.\n\n", c.Part)
	}
	b.WriteString("## Props\n\n")
	b.WriteString(propsTable(props))
	parent := c.ID
	if c.Part != "" && c.Part != designkit.PartProvider {
		parent = c.Part
	}
	inner := humanize(c.ID)
	if parent != c.ID {
		inner = fmt.Sprintf("<x-import component-from-global-scope=\"%s.%s\">%s</x-import>", ns, c.ID, humanize(c.ID))
	}
	attrs := ""
	for _, p := range props {
		if p.Name == "variant" && p.Type == "enum" && parent == c.ID {
			attrs = fmt.Sprintf(" variant=\"%s\"", p.Values[0])
		}
	}
	fmt.Fprintf(&b, "\n## Example\n\n<x-import component-from-global-scope=\"%s.%s\"%s>%s</x-import>\n", ns, parent, attrs, inner)
	if c.Code != "" {
		b.WriteString("\n## In code\n\n")
		b.WriteString(inCode(c))
	}
	if note := docNote(doc); note != "" {
		b.WriteString("\n## Status\n\n" + note + "\n")
	}
	return b.String()
}

// docNote says where a card's texts came from, when a person should look at them: a model's draft or a plain
// sentence made from the name. The authors' own words (jsdoc, readme, docs) need no note.
func docNote(d compDoc) string {
	var parts []string
	if d.DescSrc == srcDraft {
		parts = append(parts, "the description is a draft by a model from the package's docs")
	} else if d.DescSrc == srcGenerated {
		parts = append(parts, "the description is made from the name")
	}
	switch d.WhenSrc {
	case srcDraft:
		parts = append(parts, "«When to use» and «When not to use» are a draft by a model")
	case srcGenerated:
		parts = append(parts, "«When to use» and «When not to use» are made from the name and the props")
	}
	if len(parts) == 0 {
		return ""
	}
	return "Docs: " + strings.Join(parts, "; ") + "; check them before relying on them."
}

func propsTable(props []kitProp) string {
	if len(props) == 0 {
		return "None of its own: it takes the attributes of the element it draws.\n"
	}
	var b strings.Builder
	b.WriteString("| Prop | Type | Required | What |\n| --- | --- | --- | --- |\n")
	for _, p := range props {
		t := "`" + p.Type + "`"
		if p.Type == "enum" {
			var vs []string
			for _, v := range p.Values {
				vs = append(vs, "`"+v+"`")
			}
			t = strings.Join(vs, " · ")
		}
		req := ""
		if p.Required {
			req = "yes"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", p.Name, t, req, strings.ReplaceAll(p.Description, "|", "\\|"))
	}
	return b.String()
}

func inCode(c genComp) string {
	return fmt.Sprintf("```tsx\nimport { %s } from '%s';\n```\n", c.ID, c.Code)
}

var headingRe = regexp.MustCompile(`(?m)^## +(.+?)[ \t]*$`)

// replaceSection puts body under "## <title>" in place of what was there, or at the end when the card has no such
// section.
func replaceSection(md, title, body string) string {
	locs := headingRe.FindAllStringSubmatchIndex(md, -1)
	for i, l := range locs {
		if !strings.EqualFold(md[l[2]:l[3]], title) {
			continue
		}
		end := len(md)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		tail := md[end:]
		sep := "\n"
		if tail == "" {
			sep = ""
		}
		return md[:l[1]] + "\n\n" + body + sep + tail
	}
	if !strings.HasSuffix(md, "\n") {
		md += "\n"
	}
	return md + "\n## " + title + "\n\n" + body
}

// readmeGenerated is the heading from which the kit's README.md is written by the command on every run.
const readmeGenerated = "## Not in the kit"

// writeReadme writes the kit's README.md: the first run the whole of it, later runs only from «Not in the kit» on (the
// lists and the index follow the package), the text above it is the person's.
func writeReadme(o *Options, b *built, first bool, rep *Report) error {
	p := filepath.Join(o.Folder, "README.md")
	head := ""
	if have, err := os.ReadFile(p); err == nil {
		head = string(have)
		if i := strings.Index(head, readmeGenerated); i >= 0 {
			head = head[:i]
		} else if i := regexp.MustCompile(`(?m)^## +Consuming`).FindStringIndex(head); i != nil {
			head = head[:i[0]]
		}
	}
	if strings.TrimSpace(head) == "" {
		head = fmt.Sprintf("# %s\n\nThe kit of %s: the package's own components built into one browser script that takes React from the page, "+
			"their props read from its types, its CSS variables as tokens. Made with `harnessly design kit add` and brought to a new "+
			"version with `harnessly design kit update`; what a person writes (what a component is for, when not to use it, samples) is "+
			"kept, and this file is rewritten from «Not in the kit» on.\n\n", o.Title, "`"+b.pkg+"`")
	}
	head = strings.TrimRight(head, "\n") + "\n\n"
	body, err := generatedReadme(o.Folder, o.Namespace, b.others, b.root, nil)
	if err != nil {
		return err
	}
	rep.Wrote = append(rep.Wrote, "README.md")
	_ = first
	return os.WriteFile(p, []byte(head+body), 0o644)
}

type readmeComp struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	Part        json.RawMessage `json:"part"`
	Sample      json.RawMessage `json:"sample"`
	Variants    json.RawMessage `json:"variants"`
	Todo        []string        `json:"todo"`
}

func partOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Of string `json:"of"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.Of
}

// generatedReadme is the part of README.md that follows the package: what is not in the kit, Consuming, the index.
func generatedReadme(folder, ns string, others []string, root string, why map[string]string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(folder, designkit.ManifestFile))
	if err != nil {
		return "", err
	}
	var d struct {
		Kit struct {
			React string `json:"react"`
		} `json:"kit"`
		Groups []struct {
			Title      string       `json:"title"`
			Components []readmeComp `json:"components"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(readmeGenerated + "\n\n")
	if len(others) > 0 {
		list := others
		more := ""
		if len(list) > 40 {
			more = fmt.Sprintf(" and %d more", len(list)-40)
			list = list[:40]
		}
		fmt.Fprintf(&b, "- Hooks, functions and constants (not components; a board cannot place them): `%s`%s.\n", strings.Join(list, "`, `"), more)
	}
	var alone []string
	for _, g := range d.Groups {
		for _, c := range g.Components {
			if partOf(c.Part) == "" && isNull(c.Sample) && isNull(c.Variants) {
				reason := "no sample yet"
				if w := why[c.ID]; w != "" {
					reason = "draws only inside an application (" + w + ")"
				}
				alone = append(alone, fmt.Sprintf("  - `%s` — %s", c.ID, reason))
			}
		}
	}
	if len(alone) > 0 {
		b.WriteString("- Not drawn alone (no sample, so the live preview and `kit check --render` skip them; they work on a board inside what they need):\n")
		b.WriteString(strings.Join(alone, "\n") + "\n")
	}
	if len(others) == 0 && len(alone) == 0 {
		b.WriteString("Nothing: every export of the package is a component of the kit.\n")
	}
	reactNote := "React 18.3.1 comes from the page"
	if d.Kit.React == "19" {
		reactNote = "React 19 comes from the page when it has `<meta name=\"dc-react\" content=\"19\">` in its head: `design new --kit` writes it, and the kit's own boards need it"
	}
	fmt.Fprintf(&b, "\n## Consuming\n\nNamespace `%s`: a board writes `<x-import component-from-global-scope=\"%s.<Component>\">`. "+
		"A board loads, in this order: `tokens.css`, `components/bundle.css`, `components/bundle.js` (%s).", ns, ns, reactNote)
	if root != "" {
		fmt.Fprintf(&b, " Everything goes inside `%s.%s`", ns, root)
		if root == "Root" {
			b.WriteString("; `theme=\"dark\"` switches it to dark")
		}
		b.WriteString(".")
	}
	b.WriteString("\n\n### Index\n")
	for _, g := range d.Groups {
		fmt.Fprintf(&b, "\n%s\n", g.Title)
		for _, c := range g.Components {
			if partOf(c.Part) != "" && partOf(c.Part) != designkit.PartProvider {
				continue
			}
			fmt.Fprintf(&b, "- %s — %s\n", c.ID, c.Description)
			for _, p := range g.Components {
				if partOf(p.Part) == c.ID {
					fmt.Fprintf(&b, "  - %s — %s\n", p.ID, p.Description)
				}
			}
		}
	}
	return b.String(), nil
}

func isNull(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null"
}

// DropSamples takes the sample and variants away from components that do not draw alone (the render's verdict, by
// id → why), says why in their todo and in README.md, and leaves everything else as it is. The live preview and
// `kit check --render` then skip them.
func DropSamples(folder, ns, root string, failed map[string]string) error {
	if len(failed) == 0 {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(folder, designkit.ManifestFile))
	if err != nil {
		return err
	}
	doc, ok := parseObj(raw)
	if !ok {
		return fmt.Errorf("%s is not valid JSON", designkit.ManifestFile)
	}
	var groups []json.RawMessage
	_ = json.Unmarshal(doc.vals["groups"], &groups)
	var out []*obj
	for _, g := range groups {
		go_, _ := parseObj(g)
		var cs []json.RawMessage
		_ = json.Unmarshal(go_.vals["components"], &cs)
		var keep []*obj
		for _, c := range cs {
			co, _ := parseObj(c)
			if why, bad := failed[co.str("id")]; bad {
				if notAComponent(why) {
					continue // a class or a factory the package exports: a board cannot place it
				}
				co.del("sample")
				co.del("variants")
				// the reason is in README.md («draws only inside an application»), not a todo: nothing is left for a person to write
			}
			keep = append(keep, co)
		}
		go_.set("components", keep)
		out = append(out, go_)
	}
	doc.set("groups", out)
	if err := os.WriteFile(filepath.Join(folder, designkit.ManifestFile), pretty(doc), 0o644); err != nil {
		return err
	}
	var others []string
	readme, _ := os.ReadFile(filepath.Join(folder, "README.md"))
	if m := regexp.MustCompile("(?m)^- Hooks, functions and constants[^`]*`(.*)`").FindSubmatch(readme); m != nil {
		others = strings.Split(string(m[1]), "`, `")
	}
	head := string(readme)
	if i := strings.Index(head, readmeGenerated); i >= 0 {
		head = head[:i]
	}
	body, err := generatedReadme(folder, ns, others, root, failed)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(folder, "README.md"), []byte(head+body), 0o644)
}

// elementTypeProp is a prop that takes a component or a tag (component, as, ...): an empty object there makes React throw, and
// the component draws by default without it.
func elementTypeProp(p kitProp) bool {
	return p.Type == "object" && (p.Name == "component" || p.Name == "as" || p.Name == "ref" || p.Name == "slots")
}

// notAComponent: the render's verdict says the export is a class or a constructor, not something React draws.
func notAComponent(why string) bool {
	return strings.Contains(why, "cannot be invoked without 'new'") || strings.Contains(why, "is not a constructor")
}

package formats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// A built Storybook as a source of component drafts: `index.json` (Storybook 7+, entries) or `stories.json` (v3, what
// `storybook extract` writes, with args and argTypes), and the component manifest when the build has one. The index
// gives the components and their stories; argTypes give the props; the manifest gives descriptions, props and the
// import. What a Storybook does not know — what a component is for beyond its docs, when not to use it, its code —
// stays in the draft's `todo` list, which `kit check` reports.

// DraftComponent is a component the import will write: the canvas record and its card.
type DraftComponent struct {
	Group      string // group id
	GroupTitle string
	Record     *ordered
	ID         string
	Card       string
	Todo       []string
}

// StorybookResult is what an import of a Storybook found.
type StorybookResult struct {
	Components []DraftComponent
}

type sbStory struct {
	ID, Name, Description string
	Args                  map[string]any
}

type sbComponent struct {
	ID, Title, Name, Group, GroupTitle string
	Description                        string
	Stories                            []sbStory
	ArgTypes                           map[string]any
	ArgOrder                           []string
	Props                              []sbProp
	Import                             string
}

type sbProp struct {
	designkit.Prop
	Default string
}

var (
	sbImportRe   = regexp.MustCompile(`import\s+(?:(\w+)|\{\s*([^}]*)\})\s+from\s+['"]([^'"]+)['"]`)
	compIDValid  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)
	kebabNonWord = regexp.MustCompile(`[^a-z0-9]+`)
)

// ImportStorybook reads a Storybook build: a folder holding index.json or stories.json, or one of those files.
func ImportStorybook(source, namespace string) (*StorybookResult, *Report, error) {
	rep := &Report{}
	dir := source
	var file string
	if st, err := os.Stat(source); err == nil && !st.IsDir() {
		dir, file = filepath.Dir(source), source
	} else if err != nil {
		return nil, rep, err
	}
	find := func(names ...string) string {
		for _, n := range names {
			p := filepath.Join(dir, n)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}
	indexFile := file
	manifestFile := ""
	if file != "" {
		if looksLikeManifest(file) {
			manifestFile = file
			indexFile = find("index.json", "stories.json")
		}
	} else {
		indexFile = find("index.json", "stories.json")
	}
	if manifestFile == "" {
		manifestFile = find("components.json", "component-manifest.json", filepath.Join("manifests", "components.json"))
	}
	if indexFile == "" && manifestFile == "" {
		return nil, rep, fmt.Errorf("no index.json, stories.json or component manifest in %s: build Storybook (storybook build) and point at storybook-static", source)
	}
	comps := map[string]*sbComponent{}
	var order []string
	if indexFile != "" {
		raw, err := os.ReadFile(indexFile)
		if err != nil {
			return nil, rep, err
		}
		if err := readStoryIndex(raw, comps, &order, rep); err != nil {
			return nil, rep, fmt.Errorf("%s: %w", filepath.Base(indexFile), err)
		}
		rep.carried("%s: %d components from %d stories", filepath.Base(indexFile), len(order), countStories(comps))
	}
	if manifestFile != "" {
		raw, err := os.ReadFile(manifestFile)
		if err != nil {
			return nil, rep, err
		}
		n, err := readComponentManifest(raw, comps, &order)
		if err != nil {
			if indexFile == "" {
				return nil, rep, fmt.Errorf("%s: %w", filepath.Base(manifestFile), err)
			}
			rep.notCarried("%s: %v", filepath.Base(manifestFile), err)
		} else {
			rep.carried("%s: descriptions, props and imports for %d components", filepath.Base(manifestFile), n)
		}
	}
	if len(order) == 0 {
		return nil, rep, fmt.Errorf("no components in the Storybook files")
	}
	res := &StorybookResult{}
	seen := map[string]string{}
	for _, key := range order {
		c := comps[key]
		d := draftOf(c, namespace, rep)
		if prev, dup := seen[d.ID]; dup {
			rep.notCarried("%s: its id %s is already taken by %s: skipped", c.Title, d.ID, prev)
			continue
		}
		seen[d.ID] = c.Title
		res.Components = append(res.Components, d)
	}
	rep.notCarried("MDX and autodocs pages: their text is not in index.json (only that they exist)")
	rep.notCarried("play functions, decorators, parameters and the stories' own code: Storybook runs them, the kit's samples are data")
	rep.notCarried("the components' code (the bundle): a Storybook build holds the stories' bundle, not the library — build the kit's bundle from the package")
	return res, rep, nil
}

func looksLikeManifest(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	o, ok := parseOrdered(raw)
	return ok && o.has("components") && !o.has("entries") && !o.has("stories")
}

func countStories(m map[string]*sbComponent) int {
	n := 0
	for _, c := range m {
		n += len(c.Stories)
	}
	return n
}

func readStoryIndex(raw []byte, comps map[string]*sbComponent, order *[]string, rep *Report) error {
	root, ok := parseOrdered(raw)
	if !ok {
		return fmt.Errorf("not a JSON object")
	}
	entries, ok := root.sub("entries")
	if !ok {
		entries, ok = root.sub("stories")
	}
	if !ok {
		return fmt.Errorf("neither \"entries\" (index.json) nor \"stories\" (stories.json) in the file")
	}
	docs := 0
	for _, id := range entries.keys {
		e, ok := entries.sub(id)
		if !ok {
			continue
		}
		var m struct {
			ID, Title, Kind, Name, Story, Type string
			Args                               map[string]any             `json:"args"`
			ArgTypes                           map[string]any             `json:"argTypes"`
			Parameters                         map[string]json.RawMessage `json:"parameters"`
			Tags                               []string                   `json:"tags"`
		}
		if json.Unmarshal(e.MarshalRaw(), &m) != nil {
			continue
		}
		title := firstNonEmpty(m.Title, m.Kind)
		name := firstNonEmpty(m.Name, m.Story)
		if m.Type == "docs" {
			docs++
			continue
		}
		if p, ok := m.Parameters["docsOnly"]; ok && string(p) == "true" {
			docs++
			continue
		}
		if title == "" {
			continue
		}
		c := comps[title]
		if c == nil {
			c = &sbComponent{Title: title, ArgTypes: map[string]any{}}
			c.ID = strings.SplitN(firstNonEmpty(m.ID, id), "--", 2)[0]
			comps[title] = c
			*order = append(*order, title)
			segs := strings.Split(title, "/")
			c.Name = strings.TrimSpace(segs[len(segs)-1])
			if len(segs) > 1 {
				c.GroupTitle = strings.TrimSpace(strings.Join(segs[:len(segs)-1], " / "))
			} else {
				c.GroupTitle = "Components"
			}
		}
		c.Stories = append(c.Stories, sbStory{ID: firstNonEmpty(m.ID, id), Name: name, Args: m.Args})
		for k, v := range m.ArgTypes {
			if _, has := c.ArgTypes[k]; !has {
				c.ArgOrder = append(c.ArgOrder, k)
			}
			c.ArgTypes[k] = v
		}
	}
	if docs > 0 {
		rep.carried("%d docs entries counted, their text is not in the index", docs)
	}
	for _, c := range comps {
		sort.Strings(c.ArgOrder)
		for _, name := range c.ArgOrder {
			if p, ok := propFromArgType(name, c.ArgTypes[name]); ok {
				c.Props = append(c.Props, p)
			}
		}
	}
	return nil
}

// MarshalRaw lets parseOrdered values be re-read by encoding/json.
func (o *ordered) MarshalRaw() []byte { b, _ := o.MarshalJSON(); return b }

func readComponentManifest(raw []byte, comps map[string]*sbComponent, order *[]string) (int, error) {
	root, ok := parseOrdered(raw)
	if !ok {
		return 0, fmt.Errorf("not a JSON object")
	}
	list, ok := root.sub("components")
	if !ok {
		return 0, fmt.Errorf("no \"components\" map: not a component manifest")
	}
	n := 0
	byID := map[string]*sbComponent{}
	byName := map[string]*sbComponent{}
	for _, c := range comps {
		byID[c.ID] = c
		byName[strings.ToLower(c.Name)] = c
	}
	for _, key := range list.keys {
		e, ok := list.sub(key)
		if !ok {
			continue
		}
		var m struct {
			ID, Name, Description, Import, Path string
			Stories                             []struct {
				ID, Name, Description string
			} `json:"stories"`
			ReactDocgen *struct {
				Props map[string]map[string]any `json:"props"`
			} `json:"reactDocgen"`
			Props map[string]map[string]any `json:"props"`
		}
		if json.Unmarshal(e.MarshalRaw(), &m) != nil {
			continue
		}
		id := firstNonEmpty(m.ID, key)
		c := byID[id]
		if c == nil && m.Name != "" {
			c = byName[strings.ToLower(m.Name)]
		}
		if c == nil {
			c = &sbComponent{ID: id, Title: firstNonEmpty(m.Name, key), Name: firstNonEmpty(m.Name, key), GroupTitle: "Components", ArgTypes: map[string]any{}}
			comps[c.Title] = c
			*order = append(*order, c.Title)
		}
		c.Description = oneLine(m.Description)
		c.Import = m.Import
		props := m.Props
		if m.ReactDocgen != nil && len(m.ReactDocgen.Props) > 0 {
			props = m.ReactDocgen.Props
		}
		if len(props) > 0 {
			c.Props = nil
			names := make([]string, 0, len(props))
			for k := range props {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				if p, ok := propFromArgType(k, props[k]); ok {
					c.Props = append(c.Props, p)
				}
			}
		}
		for _, s := range m.Stories {
			found := false
			for i := range c.Stories {
				if c.Stories[i].ID == s.ID {
					c.Stories[i].Description = s.Description
					found = true
				}
			}
			if !found {
				c.Stories = append(c.Stories, sbStory{ID: s.ID, Name: s.Name, Description: s.Description})
			}
		}
		n++
	}
	return n, nil
}

// propFromArgType reads one Storybook argType or react-docgen prop.
func propFromArgType(name string, raw any) (sbProp, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return sbProp{}, false
	}
	if tb, ok := m["table"].(map[string]any); ok {
		if dis, _ := tb["disable"].(bool); dis {
			return sbProp{}, false
		}
	}
	if name == "" || name == "key" || name == "ref" {
		return sbProp{}, false
	}
	p := sbProp{Prop: designkit.Prop{Name: name}}
	p.Description = oneLine(str(m["description"]))
	if r, ok := m["required"].(bool); ok {
		p.Required = r
	}
	typeName, values := typeOf(m)
	if t, ok := m["type"].(map[string]any); ok {
		if r, ok := t["required"].(bool); ok && r {
			p.Required = true
		}
	}
	if ctl, ok := m["control"].(map[string]any); ok && len(values) == 0 {
		switch str(ctl["type"]) {
		case "select", "radio", "inline-radio", "multi-select", "check", "inline-check":
			typeName = "enum"
		case "boolean":
			typeName = "boolean"
		case "text":
			typeName = "string"
		case "number", "range":
			typeName = "number"
		case "color":
			typeName = "color"
		case "object":
			typeName = "object"
		}
	}
	if opts, ok := m["options"].([]any); ok && len(values) == 0 {
		for _, o := range opts {
			values = append(values, fmt.Sprint(o))
		}
		if len(values) > 0 {
			typeName = "enum"
		}
	}
	if ctl, ok := m["control"].(map[string]any); ok && len(values) == 0 {
		if opts, ok := ctl["options"].([]any); ok {
			for _, o := range opts {
				values = append(values, fmt.Sprint(o))
			}
		}
	}
	switch typeName {
	case "enum", "union", "literal":
		if len(values) > 0 {
			p.Type, p.Values = "enum", values
		} else {
			p.Type = "string"
		}
	case "boolean", "bool":
		p.Type = "boolean"
	case "number", "integer":
		p.Type = "number"
	case "string":
		p.Type = "string"
	case "color":
		p.Type = "color"
	case "function", "signature", "action":
		p.Type = "function"
	case "array", "Array":
		p.Type = "array"
	case "object", "shape":
		p.Type = "object"
	case "node", "ReactNode", "ReactElement", "Element", "element", "JSX.Element":
		p.Type = "node"
	default:
		p.Type = "object" // unknown: not guessed to be a string
		p.Description = strings.TrimSpace(p.Description)
	}
	if p.Type == "object" && typeName != "object" && typeName != "shape" {
		p.Default = "?type:" + typeName // remembered for the report, never written
	}
	if tb, ok := m["table"].(map[string]any); ok {
		if d, ok := tb["defaultValue"].(map[string]any); ok {
			p.Default = strings.Trim(str(d["summary"]), `'"`)
		}
		if p.Type == "object" && typeName == "" {
			if t, ok := tb["type"].(map[string]any); ok {
				if tn, vals := typeFromSummary(str(t["summary"])); tn != "" {
					p.Type = tn
					p.Values = vals
				}
			}
		}
	}
	if dv, ok := m["defaultValue"].(map[string]any); ok && p.Default == "" {
		p.Default = strings.Trim(str(dv["value"]), `'"`)
	}
	return p, true
}

var literalRe = regexp.MustCompile(`^['"](.*)['"]$`)

// typeFromSummary reads "'primary' | 'secondary'" (Storybook's table summary) as an enum.
func typeFromSummary(s string) (string, []string) {
	s = strings.TrimSpace(s)
	switch s {
	case "string", "boolean", "number":
		return s, nil
	}
	if strings.Contains(s, "|") {
		var vals []string
		for _, part := range strings.Split(s, "|") {
			part = strings.TrimSpace(part)
			m := literalRe.FindStringSubmatch(part)
			if m == nil {
				return "", nil
			}
			vals = append(vals, m[1])
		}
		return "enum", vals
	}
	return "", nil
}

// typeOf returns the type name of an argType/docgen prop and the literal values of an enum/union.
func typeOf(m map[string]any) (string, []string) {
	var t map[string]any
	switch x := m["type"].(type) {
	case map[string]any:
		t = x
	case string:
		return x, nil
	}
	if t == nil {
		t, _ = m["tsType"].(map[string]any)
	}
	if t == nil {
		return "", nil
	}
	name := str(t["name"])
	var values []string
	collect := func(items []any) bool {
		for _, it := range items {
			im, ok := it.(map[string]any)
			if !ok {
				return false
			}
			if str(im["name"]) == "literal" || str(im["name"]) == "string" && im["value"] != nil {
				values = append(values, strings.Trim(fmt.Sprint(im["value"]), `'"`))
				continue
			}
			return false
		}
		return true
	}
	switch name {
	case "enum":
		for _, v := range asList(t["value"]) {
			values = append(values, strings.Trim(fmt.Sprint(v), `'"`))
		}
	case "union":
		items := asList(t["elements"])
		if len(items) == 0 {
			items = asList(t["value"])
		}
		if !collect(items) {
			values = nil
			name = "other"
		}
	case "other":
		if v, ok := t["value"].(string); ok {
			if tn, vals := typeFromSummary(v); tn == "enum" {
				name, values = "enum", vals
			} else if v != "" {
				name = v // Storybook's own spelling of the type: ReactNode, string…
			}
		}
	}
	return name, values
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// ---------------------------------------------------------------------------------------------------------------------

func pascal(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if up {
				b.WriteRune(unicode.ToUpper(r))
				up = false
			} else {
				b.WriteRune(r)
			}
		} else {
			up = true
		}
	}
	return b.String()
}

func kebab(s string) string {
	return strings.Trim(kebabNonWord.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func draftOf(c *sbComponent, namespace string, rep *Report) DraftComponent {
	id := c.Name
	if !compIDValid.MatchString(id) {
		fixed := pascal(id)
		if fixed == "" || !unicode.IsLetter(rune(fixed[0])) {
			fixed = "C" + fixed
		}
		rep.notCarried("%s: the name is not an export name; written as %s", c.Title, fixed)
		id = fixed
	}
	d := DraftComponent{ID: id, Group: kebab(c.GroupTitle), GroupTitle: c.GroupTitle}
	if d.Group == "" {
		d.Group, d.GroupTitle = "components", "Components"
	}
	declared := map[string]designkit.Prop{}
	var props []designkit.Prop
	for _, p := range c.Props {
		if strings.HasPrefix(p.Default, "?type:") {
			rep.todo("%s.%s: Storybook's type %q is not one of the kit's prop types: written as object, check it", id, p.Name, strings.TrimPrefix(p.Default, "?type:"))
		}
		props = append(props, p.Prop)
		declared[p.Name] = p.Prop
	}
	rec := newOrdered()
	rec.set("id", id)
	rec.set("name", id)
	var todo []string
	if c.Description != "" {
		rec.set("description", c.Description)
	} else {
		rec.set("description", "")
		todo = append(todo, "description: what it is for and when to take it (Storybook has no text for it)")
	}
	if len(props) > 0 {
		rec.set("props", props)
	} else {
		todo = append(todo, "props: Storybook gave no argTypes (build with `storybook extract` for stories.json, or add the component manifest)")
	}
	// Variants and the sample from the stories' args.
	var variants []designkit.Variant
	var sample map[string]any
	usedIDs := map[string]bool{}
	for _, s := range c.Stories {
		args := usableArgs(s.Args, declared)
		if len(args) == 0 {
			continue
		}
		vid := kebab(s.Name)
		if vid == "" || usedIDs[vid] {
			vid = kebab(s.ID)
		}
		usedIDs[vid] = true
		variants = append(variants, designkit.Variant{ID: vid, Name: s.Name, Props: args})
		if sample == nil {
			sample = args
		}
	}
	if len(variants) > 0 {
		rec.set("variants", variants)
		rec.set("sample", sample)
		for _, p := range props {
			if p.Required && designkit.PreviewProp(p.Name) {
				if _, ok := sample[p.Name]; !ok {
					todo = append(todo, fmt.Sprintf("sample: required prop %q has no value in the first story's args", p.Name))
				}
			}
		}
	} else {
		todo = append(todo, "sample: no story carries args for declared props, so there is no sample or variants")
	}
	if exp, mod, ok := parseImport(c.Import, id); ok {
		rec.set("code", map[string]string{"import": mod, "export": exp})
	} else {
		todo = append(todo, "code: {import, export} (the component manifest has the import statement; this build has none)")
	}
	todo = append(todo, "card: «When to use» and «When not to use» (names what to take instead)")
	rec.set("todo", todo)
	d.Record = rec
	d.Todo = todo
	d.Card = cardOf(id, namespace, c, props, sample)
	return d
}

func usableArgs(args map[string]any, declared map[string]designkit.Prop) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		p, ok := declared[k]
		if !ok || !designkit.PreviewProp(k) || v == nil {
			continue
		}
		switch p.Type {
		case "function", "node":
			if _, isStr := v.(string); !isStr || p.Type == "function" {
				continue
			}
		case "enum":
			s, isStr := v.(string)
			if !isStr || !contains(p.Values, s) {
				continue
			}
		case "boolean":
			if _, ok := v.(bool); !ok {
				continue
			}
		case "number":
			if _, ok := v.(float64); !ok {
				continue
			}
		case "string", "color", "token":
			if _, ok := v.(string); !ok {
				continue
			}
		}
		out[k] = v
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// parseImport reads `import { Button } from '@acme/ui'` for the component id.
func parseImport(stmt, id string) (export, module string, ok bool) {
	for _, m := range sbImportRe.FindAllStringSubmatch(stmt, -1) {
		module = m[3]
		if m[1] != "" {
			return "default", module, true
		}
		for _, part := range strings.Split(m[2], ",") {
			part = strings.TrimSpace(part)
			name, alias, has := strings.Cut(part, " as ")
			if strings.TrimSpace(name) == id || (has && strings.TrimSpace(alias) == id) {
				return strings.TrimSpace(name), module, true
			}
		}
		if names := strings.Split(m[2], ","); len(names) == 1 && strings.TrimSpace(names[0]) != "" && !strings.Contains(names[0], " as ") {
			return strings.TrimSpace(names[0]), module, true
		}
	}
	return "", "", false
}

func cardOf(id, namespace string, c *sbComponent, props []designkit.Prop, sample map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", id)
	if c.Description != "" {
		b.WriteString(c.Description + "\n\n")
	}
	if len(props) > 0 {
		b.WriteString("## Props\n\n| Prop | Type | Default | What |\n| --- | --- | --- | --- |\n")
		def := map[string]string{}
		for _, p := range c.Props {
			def[p.Name] = p.Default
		}
		for _, p := range props {
			typ := p.Type
			if len(p.Values) > 0 {
				typ = strings.Join(p.Values, " · ")
			}
			d := def[p.Name]
			if strings.HasPrefix(d, "?type:") || d == "" {
				d = "—"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", p.Name, mdCell(typ), mdCell(d), mdCell(p.Description))
		}
		b.WriteString("\n")
	}
	b.WriteString("## Example\n\n```html\n" + exampleMarkup(namespace, id, sample) + "\n```\n")
	if len(c.Stories) > 0 {
		b.WriteString("\n## Stories\n\nFrom Storybook:")
		for _, s := range c.Stories {
			fmt.Fprintf(&b, " `%s`", s.Name)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func exampleMarkup(ns, id string, sample map[string]any) string {
	var attrs []string
	inner := ""
	keys := make([]string, 0, len(sample))
	for k := range sample {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := sample[k].(type) {
		case string:
			if k == "children" {
				inner = v
			} else {
				attrs = append(attrs, fmt.Sprintf(`%s="%s"`, k, strings.ReplaceAll(v, `"`, "&quot;")))
			}
		case bool:
			attrs = append(attrs, fmt.Sprintf(`%s="{{%v}}"`, k, v))
		case float64:
			attrs = append(attrs, fmt.Sprintf(`%s="{{%s}}"`, k, trimFloat(v)))
		}
	}
	open := fmt.Sprintf(`<x-import component-from-global-scope="%s.%s"`, ns, id)
	if len(attrs) > 0 {
		open += " " + strings.Join(attrs, " ")
	}
	return open + ">" + inner + "</x-import>"
}

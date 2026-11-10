package designkit

// Check reads a kit folder (before it is packed, or a project's ds/<folder>/) the way install, the kit screen and a
// consuming agent read it, and reports what would go wrong: a missing identity, a canvas the screen cannot read,
// tokens in a shape the readers ignore, components an agent cannot tell apart because they have no purpose or no card.
// It looks at files only and runs nothing; the contract it checks is docs/agents/design-kits.md.

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Problem levels: an error makes the kit unfit to pack; a warning is a kit that works and reads worse.
const (
	LevelError   = "error"
	LevelWarning = "warning"
)

// Problem is one finding of Check: the file it is about (slash path from the checked folder) and what is wrong.
type Problem struct {
	Rule    string `json:"rule"`
	Level   string `json:"level"`
	File    string `json:"file"`
	Message string `json:"message"`
}

// CheckReport is what Check found in a folder.
type CheckReport struct {
	Themes     []string  `json:"themes"`
	Components int       `json:"components"`
	Problems   []Problem `json:"problems"`
	// Readiness is how far each component is built out (readiness.go); Level is the kit's, the lowest of its
	// components (-1: a component has no code yet), Next what that component lacks.
	Level     int                  `json:"level"`
	Next      string               `json:"next"`
	Readiness []ComponentReadiness `json:"readiness"`
}

// Errors counts the problems that make the kit unfit to pack.
func (r *CheckReport) Errors() int {
	n := 0
	for _, p := range r.Problems {
		if p.Level == LevelError {
			n++
		}
	}
	return n
}

var (
	namespaceRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]{0,62}$`)
	// A component the live preview can draw: one identifier (kit_view.go's compNameRe).
	playableRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)
	compIDRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)
	tokenNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	// Colour values Claude Design's reader drops (tokens.json): a function inside a function, var(), named colours.
	badColorRe = regexp.MustCompile(`(?i)var\(|color-mix\(|^(transparent|currentcolor|inherit|initial|unset|[a-z]+)$`)
	// React's own internals: a bundle that defines them carries a second React.
	ownReactRe  = regexp.MustCompile(`(__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED|__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE)\s*[=:]`)
	firstSentRe = regexp.MustCompile(`^(.+?[.!?])(\s|$)`)
)

var propTypes = map[string]bool{"enum": true, "boolean": true, "number": true, "string": true, "color": true, "token": true,
	"node": true, "function": true, "object": true, "array": true}

type checker struct {
	root   string
	rep    *CheckReport
	themes int
	cur    map[string]*compFacts // the components of the theme being checked, for the readiness
}

// add reports a problem of rule id (rules.go): the registry gives its level.
func (k *checker) add(id, file, format string, args ...any) {
	r, ok := RuleOf(id)
	if !ok {
		panic("designkit: no rule " + id)
	}
	k.rep.Problems = append(k.rep.Problems, Problem{Rule: id, Level: r.Level, File: file, Message: fmt.Sprintf(format, args...)})
}

func (k *checker) exists(rel string) bool {
	if cleanRel(rel) == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(k.root, filepath.FromSlash(rel)))
	return err == nil && !st.IsDir()
}

func (k *checker) read(rel string) ([]byte, bool) {
	raw, err := readLimited(filepath.Join(k.root, filepath.FromSlash(rel)), 32<<20)
	return raw, err == nil
}

// Check checks the kit folder root: kit.json's themes when it has one, else the folder as one theme.
func Check(root string) (*CheckReport, error) {
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", root)
	}
	k := &checker{root: root, rep: &CheckReport{Themes: []string{}, Problems: []Problem{}, Level: -1, Readiness: []ComponentReadiness{}}}
	folders := map[string]string{}
	if raw, ok := k.read("kit.json"); ok {
		var kf kitFile
		if err := json.Unmarshal(raw, &kf); err != nil {
			k.add("kitjson.json", "kit.json", "not valid JSON: %v", err)
		}
		for i, t := range kf.Themes {
			folder := cleanRel(t.Folder)
			switch {
			case !ValidID(t.ID):
				k.add("kitjson.theme", "kit.json", "themes[%d]: id %q must be lower case letters, digits and -", i, t.ID)
			case folder == "":
				k.add("kitjson.theme", "kit.json", "themes[%d] (%s): folder %q is not a path inside the kit", i, t.ID, t.Folder)
			case !k.exists(path.Join(folder, ManifestFile)):
				k.add("kitjson.theme", "kit.json", "themes[%d] (%s): %s has no %s", i, t.ID, folder, ManifestFile)
			default:
				folders[t.ID] = folder
			}
		}
		if len(kf.Themes) == 0 {
			k.add("kitjson.empty", "kit.json", "lists no themes: drop kit.json for a kit with one theme")
		}
	} else {
		folders["default"] = ""
	}
	k.themes = len(folders)
	ids := make([]string, 0, len(folders))
	for id := range folders {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		k.rep.Themes = append(k.rep.Themes, id)
		k.theme(folders[id])
	}
	return k.rep, nil
}

// manifest is design-system.json as Check reads it: the canvas fields kept raw, so a wrong shape is named, not fatal.
type manifest struct {
	V           json.RawMessage            `json:"v"`
	Title       string                     `json:"title"`
	Namespace   string                     `json:"namespace"`
	Groups      json.RawMessage            `json:"groups"`
	AssetGroups json.RawMessage            `json:"assetGroups"`
	Templates   json.RawMessage            `json:"templates"`
	Sections    map[string]json.RawMessage `json:"sections"`
}

type checkComponent struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Props       []Prop          `json:"props"`
	Variants    []Variant       `json:"variants"`
	Sample      map[string]any  `json:"sample"`
	Preview     string          `json:"preview"`
	Thumb       string          `json:"thumb"`
	Part        json.RawMessage `json:"part"`
	Keywords    json.RawMessage `json:"keywords"`
	Code        json.RawMessage `json:"code"`
	Preset      json.RawMessage `json:"preset"`
	Status      json.RawMessage `json:"status"`
	ReplacedBy  json.RawMessage `json:"replacedBy"`
}

func (k *checker) theme(folder string) {
	at := func(rel string) string { return path.Join(folder, rel) }
	mf := at(ManifestFile)
	raw, ok := k.read(mf)
	if !ok {
		k.add("index.missing", mf, "missing: it is the kit's index")
		return
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		k.add("index.json", mf, "not valid JSON: %v", err)
		return
	}
	var all map[string]json.RawMessage
	_ = json.Unmarshal(raw, &all)
	omitted := k.identity(mf, all)
	if strings.TrimSpace(m.Title) == "" {
		k.add("index.title", mf, "no title: the kit screen shows the id instead")
	}
	if !namespaceRe.MatchString(m.Namespace) {
		k.add("index.namespace", mf, "namespace %q is not a JavaScript name: boards write <Namespace>.<Component>", m.Namespace)
	}
	for id := range m.Sections {
		if id != SectionFoundation && id != SectionComponents && id != SectionTemplates && id != SectionAssets {
			k.add("index.section", mf, "sections.%s: the tree has only foundation, components, templates and assets; this one is not shown", id)
		}
	}

	if !k.exists(at("README.md")) {
		k.add("readme.missing", at("README.md"), "missing: every agent that uses the kit starts there")
	} else if body, _ := k.read(at("README.md")); !regexp.MustCompile(`(?im)^#+\s*consuming`).Match(body) {
		k.add("readme.consuming", at("README.md"), "no Consuming section: the namespace, the files a board loads in order, the root component, the index")
	}
	k.tokens(folder)

	k.cur = map[string]*compFacts{}
	comps := k.groups(folder, mf, m.Groups)
	k.rep.Components += comps
	k.readiness(folder, omitted)
	k.assets(folder, mf, m.AssetGroups)
	k.templates(folder, mf, m.Templates)
	if comps == 0 && len(m.AssetGroups) == 0 && len(m.Templates) == 0 {
		k.add("index.empty", mf, "no groups, assetGroups or templates: the kit screen has no canvas for this theme")
	}
	if comps > 0 {
		k.bundle(folder, m.Namespace)
	}
}

// identity: the member holding the kit's id and version, the one install checks the archive against.
func (k *checker) identity(mf string, all map[string]json.RawMessage) map[string]bool {
	found := false
	omitted := map[string]bool{}
	for name, member := range all {
		var own struct {
			kitFields
			Source string `json:"source"`
		}
		if json.Unmarshal(member, &own) != nil || own.ID == "" || own.Version == "" {
			continue
		}
		found = true
		for o := range k.recordFields(mf, name, own.kitFields) {
			omitted[o] = true
		}
		if !ValidID(own.ID) {
			k.add("identity.id", mf, "%s.id %q: lower case letters, digits and -, 63 at most", name, own.ID)
		}
		if !ValidVersion(own.Version) {
			k.add("identity.version", mf, "%s.version %q: major.minor.patch", name, own.Version)
		}
		if own.Source != "" && own.Source != "library" && own.Source != "draft" {
			k.add("identity.source", mf, "%s.source %q: library or draft", name, own.Source)
		}
	}
	if !found {
		k.add("identity.missing", mf, `no kit record: add "kit": {"id": "<id>", "version": "<x.y.z>", "source": "library|draft"}`)
	}
	return omitted
}

func (k *checker) groups(folder, mf string, raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var names []string
	if json.Unmarshal(raw, &names) == nil && len(names) > 0 {
		k.add("groups.names", mf, "groups is a list of names (a claude.ai design system's asset groups); a kit's groups are component groups: [{id, title, components: […]}]")
		return 0
	}
	var groups []struct {
		ID         string           `json:"id"`
		Title      string           `json:"title"`
		Components []checkComponent `json:"components"`
	}
	if err := json.Unmarshal(raw, &groups); err != nil {
		k.add("groups.shape", mf, "groups: %v (expected [{id, title, components: […]}])", err)
		return 0
	}
	seen := map[string]bool{}
	descs := map[string]string{}
	all := map[string]checkComponent{}
	for _, g := range groups {
		for _, c := range g.Components {
			all[firstNonEmpty(c.ID, c.Name)] = c
		}
	}
	n := 0
	for gi, g := range groups {
		if g.ID == "" {
			k.add("groups.id", mf, "groups[%d] has no id: the screen skips it", gi)
			continue
		}
		for _, c := range g.Components {
			n++
			id := firstNonEmpty(c.ID, c.Name)
			where := fmt.Sprintf("groups.%s.%s", g.ID, id)
			if !compIDRe.MatchString(id) {
				k.add("component.id", mf, "%s: id %q must be the export's name in the namespace (Button, Field.TextInput)", where, id)
				continue
			}
			if seen[id] {
				k.add("component.twice", mf, "%s: listed twice", where)
			}
			seen[id] = true
			k.component(folder, mf, where, id, c, descs, all)
		}
	}
	return n
}

func (k *checker) component(folder, mf, where, id string, c checkComponent, descs map[string]string, all map[string]checkComponent) {
	desc := strings.TrimSpace(c.Description)
	switch {
	case desc == "":
		k.add("component.description", mf, "%s: no description — say what it is for and when to take it; agents choose by it", where)
	case len(strings.Fields(desc)) < 5:
		k.add("component.description-short", mf, "%s: description %q is too short to choose by: what it is for and when to take it", where, desc)
	}
	if desc != "" {
		key := strings.ToLower(desc)
		if other, dup := descs[key]; dup {
			k.add("component.description-same", mf, "%s: same description as %s: an agent cannot tell them apart", where, other)
		}
		descs[key] = id
	}
	for _, p := range c.Props {
		switch {
		case p.Name == "":
			k.add("prop.name", mf, "%s: a prop has no name", where)
		case !propTypes[p.Type]:
			k.add("prop.type", mf, "%s.%s: type %q is not one of enum, boolean, number, string, color, token, node, function, object, array", where, p.Name, p.Type)
		case p.Type == "enum" && len(p.Values) == 0:
			k.add("prop.enum", mf, "%s.%s: an enum needs its values", where, p.Name)
		case strings.TrimSpace(p.Description) == "" && p.Name != "children":
			k.add("prop.description", mf, "%s.%s: no description: what it changes on screen", where, p.Name)
		}
	}
	part := k.part(mf, where, id, c.Part, all)
	k.keywords(mf, where, c.Keywords)
	k.status(mf, where, id, c.Status, c.ReplacedBy, all)
	facts := &compFacts{code: k.code(mf, where, c.Code), isPart: part != ""}
	k.cur[id] = facts
	if target, has := k.presetOf(mf, where, id, c.Preset, all); has {
		// A preset is a name for another component with fixed props: that component carries the picture and the card.
		facts.preset, facts.isPreset = target, true
		return
	}
	if c.Thumb != "" && !k.exists(path.Join(folder, c.Thumb)) {
		k.add("component.thumb", mf, "%s: thumb %s is not in the kit (harnessly design kit render <folder> --shots writes it)", where, c.Thumb)
	}
	k.values(mf, where, "sample", c, c.Sample, all)
	for _, v := range c.Variants {
		if v.ID == "" {
			k.add("variant.id", mf, "%s: a variant has no id: the screen cannot offer it", where)
		}
		k.values(mf, where, "variant "+v.ID, c, v.Props, all)
	}
	// The preview and the picture draw the sample with the first variant over it (kitrender.go): a required prop
	// missing from both is a component drawn without what it cannot do without.
	if part == "" && (len(c.Sample) > 0 || len(c.Variants) > 0) {
		shown := map[string]bool{}
		for name := range c.Sample {
			shown[name] = true
		}
		if len(c.Variants) > 0 {
			for name := range c.Variants[0].Props {
				shown[name] = true
			}
		}
		for _, p := range c.Props {
			if p.Required && !shown[p.Name] && PreviewProp(p.Name) {
				k.add("sample.required", mf, "%s: required prop %q is in neither the sample nor the first variant: the preview and its picture draw it without (give it a value; an element is {\"%s\": \"Comp\", \"props\": {…}})", where, p.Name, ElementKey)
			}
		}
	}
	if c.Preview != "" && !k.exists(path.Join(folder, c.Preview)) {
		k.add("component.preview", mf, "%s: preview %s is not in the kit (the field is optional: the screen draws the sample)", where, c.Preview)
	}
	switch {
	case part != "":
	case !playableRe.MatchString(id) && (len(c.Sample) > 0 || len(c.Variants) > 0):
		k.add("component.dotted", mf, "%s: the live preview draws only one-word components; %s shows no live preview (make it a part of its parent and show it in the parent's sample)", where, id)
	case len(c.Variants) == 0 && len(c.Sample) == 0:
		k.add("component.no-sample", mf, "%s: no sample or variants: the canvas shows it as a name only (or mark it a part: \"part\": \"<Parent>\" or \"provider\")", where)
	}

	facts.sample = len(c.Sample) > 0 || len(c.Variants) > 0
	card := path.Join(folder, "components", strings.ReplaceAll(id, ".", "/"), "README.md")
	body, ok := k.read(card)
	if !ok {
		k.add("card.missing", card, "missing: the component's card (what it is for, when to use, when not, props, example)")
		return
	}
	text := string(body)
	facts.card = true
	facts.whenNot = regexp.MustCompile(`(?im)^#+\s*(when not|когда не)`).MatchString(text)
	facts.example = strings.Contains(text, "component-from-global-scope")
	first := cardSummary(text)
	if first == "" {
		k.add("card.summary", card, "no summary: the first sentence after the title says what the component is for")
	} else if desc != "" && normalize(sentence(first)) != normalize(sentence(desc)) {
		k.add("card.summary-differs", card, "first sentence %q differs from its description in %s %q", sentence(first), ManifestFile, sentence(desc))
	}
	if !regexp.MustCompile(`(?im)^#+\s*(when not|когда не)`).MatchString(text) {
		k.add("card.when-not", card, "no «When not to use»: name what to take instead, so look-alikes are told apart")
	}
	if !strings.Contains(text, "component-from-global-scope") {
		k.add("card.example", card, "no example: the <x-import component-from-global-scope=…> markup a board copies")
	}
}

// cardSummary is the first paragraph of a card after its title.
func cardSummary(md string) string {
	var para []string
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "<!--") {
			if len(para) > 0 {
				break
			}
			continue
		}
		if t == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		para = append(para, t)
	}
	return strings.Join(para, " ")
}

func sentence(s string) string {
	if m := firstSentRe.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		return m[1]
	}
	return strings.TrimSpace(s)
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.Trim(s, " .!?`*_")), " "))
}

func (k *checker) assets(folder, mf string, raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var groups map[string]struct {
		Kind  string  `json:"kind"`
		Items []Asset `json:"items"`
	}
	if err := json.Unmarshal(raw, &groups); err != nil {
		k.add("assets.shape", mf, "assetGroups: %v (expected {id: {title, kind, items: [{name, file}]}})", err)
		return
	}
	for id, g := range groups {
		if len(g.Items) == 0 {
			k.add("assets.items", mf, "assetGroups.%s has no items (a claude.ai design system keeps uploads under files; a kit lists {name, file})", id)
			continue
		}
		if g.Kind != "" && g.Kind != "icon" && g.Kind != "image" && g.Kind != "logo" {
			k.add("assets.kind", mf, "assetGroups.%s.kind %q: icon, image or logo", id, g.Kind)
		}
		missing := 0
		for _, a := range g.Items {
			if a.Name == "" || !k.exists(path.Join(folder, a.File)) || (a.Dark != "" && !k.exists(path.Join(folder, a.Dark))) {
				missing++
			}
		}
		if missing > 0 {
			k.add("assets.files", mf, "assetGroups.%s: %d of %d items have no name or a file that is not in the kit", id, missing, len(g.Items))
		}
	}
}

func (k *checker) templates(folder, mf string, raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var ts []struct {
		ID       string `json:"id"`
		Sections []struct {
			ID      string           `json:"id"`
			Layouts []TemplateLayout `json:"layouts"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(raw, &ts); err != nil {
		k.add("templates.shape", mf, "templates: %v", err)
		return
	}
	for _, t := range ts {
		if t.ID == "" {
			k.add("templates.id", mf, "a template has no id: the screen skips it")
			continue
		}
		layouts := 0
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				layouts++
				where := fmt.Sprintf("templates.%s.%s.%s", t.ID, s.ID, l.ID)
				if l.ID == "" {
					k.add("layout.id", mf, "templates.%s.%s: a layout has no id", t.ID, s.ID)
					continue
				}
				if strings.TrimSpace(l.Purpose) == "" {
					k.add("layout.purpose", mf, "%s: no purpose: the search and an agent pick a layout by it", where)
				}
				for _, f := range []string{l.Board, l.Thumb} {
					if f != "" && !k.exists(path.Join(folder, f)) {
						k.add("layout.file", mf, "%s: %s is not in the kit", where, f)
					}
				}
				if l.Thumb == "" {
					k.add("layout.thumb", mf, "%s: no thumb: the canvas shows an empty frame", where)
				}
			}
		}
		if layouts == 0 {
			k.add("templates.empty", mf, "templates.%s has no layouts: the screen skips it", t.ID)
		}
	}
}

func (k *checker) tokens(folder string) {
	tf := path.Join(folder, "tokens.json")
	raw, ok := k.read(tf)
	if !ok {
		k.add("tokens.missing", tf, "missing: the foundation (colours, type, spacing, radius) is read from it")
		return
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		k.add("tokens.json", tf, "not valid JSON: %v", err)
		return
	}
	if strings.Contains(string(raw), `"$value"`) {
		k.add("tokens.dtcg", tf, "a name→$value map (W3C/DTCG): the readers take only lists, {\"tokens\": [{name, value, usage}]}")
	}
	names := map[string]string{}
	noUsage := 0
	var modes []string
	for family, body := range all {
		if family == "type" || family == "name" || family == "version" || family == "meta" {
			continue
		}
		var list struct {
			Themes []struct {
				ID string `json:"id"`
			} `json:"themes"`
			Tokens []struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
				Usage string          `json:"usage"`
				Var   json.RawMessage `json:"var"`
			} `json:"tokens"`
		}
		if json.Unmarshal(body, &list) != nil || list.Tokens == nil {
			k.add("tokens.family", tf, "%s is not {\"tokens\": [{name, value, usage}]}: the readers show it empty", family)
			continue
		}
		if family == "color" {
			for _, t := range list.Themes {
				modes = append(modes, t.ID)
			}
		}
		for _, t := range list.Tokens {
			if !tokenNameRe.MatchString(t.Name) {
				k.add("tokens.name", tf, "%s: token name %q: letters, digits, _ . -, no spaces or /", family, t.Name)
				continue
			}
			if prev, dup := names[t.Name]; dup {
				k.add("tokens.duplicate", tf, "%s: %s is also in %s; a name is used once across families", family, t.Name, prev)
			}
			names[t.Name] = family
			k.tokenVar(tf, family, t.Name, t.Var)
			if strings.TrimSpace(t.Usage) == "" {
				noUsage++
			}
			if family == "color" {
				k.color(tf, t.Name, t.Value, modes)
			}
		}
	}
	if noUsage > 0 {
		k.add("tokens.usage", tf, "%d tokens have no usage: an agent chooses a token by it", noUsage)
	}
	var typ struct {
		Fonts []struct {
			File string `json:"file"`
		} `json:"fonts"`
	}
	if body, ok := all["type"]; ok && json.Unmarshal(body, &typ) == nil {
		for _, f := range typ.Fonts {
			if f.File != "" && !k.exists(path.Join(folder, f.File)) {
				k.add("tokens.font", tf, "type.fonts: %s is not in the kit", f.File)
			}
		}
	}
}

func (k *checker) color(tf, name string, raw json.RawMessage, modes []string) {
	values := map[string]string{}
	if json.Unmarshal(raw, &values) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			k.add("tokens.color-shape", tf, "color %s: value is neither a string nor {mode: value}", name)
			return
		}
		values[""] = s
	} else {
		for _, m := range modes {
			if _, ok := values[m]; !ok {
				k.add("tokens.mode", tf, "color %s has no %s value: it inherits the first mode's", name, m)
			}
		}
	}
	for mode, v := range values {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "{") || strings.HasPrefix(v, "#") {
			continue
		}
		if badColorRe.MatchString(v) {
			label := name
			if mode != "" {
				label += " (" + mode + ")"
			}
			k.add("tokens.color-value", tf, "color %s: %q is dropped by the readers — resolve it to hex, rgb(), hsl() or oklch() at build time", label, v)
		}
	}
}

func (k *checker) bundle(folder, namespace string) {
	js := path.Join(folder, "components", "bundle.js")
	body, ok := k.read(js)
	if !ok {
		if k.exists(path.Join(folder, "bundle.js")) {
			k.add("bundle.at-root", js, "missing: the bundle is at bundle.js; boards and the live preview load components/bundle.js")
		} else {
			k.add("bundle.missing", js, "missing: the kit lists components, the script that defines them is components/bundle.js")
		}
		return
	}
	text := string(body)
	if strings.Contains(strings.ToLower(text), "</script") {
		k.add("bundle.script-tag", js, "contains </script: it cannot be inlined into a board")
	}
	if namespace != "" && !strings.Contains(text, namespace) {
		k.add("bundle.namespace", js, "never names %s: it should assign window.%s", namespace, namespace)
	}
	if ownReactRe.MatchString(text) {
		k.add("bundle.own-react", js, "carries its own React: map react and react-dom to window.React / window.ReactDOM, the board supplies them")
	}
	if regexp.MustCompile(`(?m)^\s*(import|export)\s`).MatchString(text) {
		k.add("bundle.module", js, "is a module (import/export): it must be one classic script")
	}
	css := path.Join(folder, "components", "bundle.css")
	style, ok := k.read(css)
	if !ok {
		k.add("css.missing", css, "missing: components without their styles")
		return
	}
	if sel := globalRule(string(style)); sel != "" {
		k.add("css.global", css, "%q outside @layer restyles every board that loads the kit: put the kit's rules in @layer kit-<id>", sel)
	}
}

// globalSelRe: a selector whose first part is html, body or *, i.e. not scoped to the kit's own elements.
var globalSelRe = regexp.MustCompile(`^(html|body|\*)([\s:.\[>+~]|$)`)

func globalSelector(prelude string) bool {
	for _, sel := range strings.Split(prelude, ",") {
		if globalSelRe.MatchString(strings.TrimSpace(sel)) {
			return true
		}
	}
	return false
}

// globalRule is the first top-level selector, outside any @layer, that touches html, body or every element.
func globalRule(css string) string {
	css = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	depth := 0
	var stack []bool // per open block: is it (inside) an @layer
	start := 0
	inLayer := func() bool {
		for _, l := range stack {
			if l {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			prelude := strings.TrimSpace(css[start:i])
			layer := strings.HasPrefix(prelude, "@layer")
			if !layer && !inLayer() && !strings.HasPrefix(prelude, "@") && globalSelector(prelude) {
				return prelude
			}
			stack = append(stack, layer)
			depth++
			start = i + 1
		case '}':
			if depth > 0 {
				depth--
				stack = stack[:len(stack)-1]
			}
			start = i + 1
		case ';':
			start = i + 1
		}
	}
	return ""
}

// Identity is the kit's id and version as its folder records them: the kit record of design-system.json at the root,
// or of the first theme kit.json names. Install checks an archive against the same record.
func Identity(root string) (id, version string, err error) {
	manifest := filepath.Join(root, ManifestFile)
	if raw, rerr := readLimited(filepath.Join(root, "kit.json"), 1<<20); rerr == nil {
		var kf kitFile
		if json.Unmarshal(raw, &kf) == nil && len(kf.Themes) > 0 {
			if folder := cleanRel(kf.Themes[0].Folder); folder != "" {
				manifest = filepath.Join(root, filepath.FromSlash(folder), ManifestFile)
			}
		}
	}
	raw, err := readLimited(manifest, 32<<20)
	if err != nil {
		return "", "", fmt.Errorf("no %s: %w", ManifestFile, err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return "", "", fmt.Errorf("%s: %w", ManifestFile, err)
	}
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		var own struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		}
		if json.Unmarshal(all[n], &own) == nil && own.ID != "" && own.Version != "" {
			if !ValidID(own.ID) || !ValidVersion(own.Version) {
				return "", "", fmt.Errorf("%s: the kit record's id %q or version %q is not valid", ManifestFile, own.ID, own.Version)
			}
			return own.ID, own.Version, nil
		}
	}
	return "", "", fmt.Errorf(`%s has no kit record: "kit": {"id": …, "version": …}`, ManifestFile)
}

// part reads a component's "part": "" for a component of its own, PartProvider, or the parent's id. A part is shown in
// its parent's panel and drawn by the parent's sample; a provider sits in the Service group with no picture.
func (k *checker) part(mf, where, id string, raw json.RawMessage, all map[string]checkComponent) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	of := partOf(raw)
	switch {
	case of == "":
		k.add("part.shape", mf, "%s: part must be \"provider\", the parent's id (\"Table\") or {\"of\": \"Table\"}", where)
	case of == PartProvider:
	case of == id:
		k.add("part.self", mf, "%s: a part of itself", where)
	default:
		parent, ok := all[of]
		switch {
		case !ok:
			k.add("part.parent", mf, "%s: part of %q, which is not a component of the kit", where, of)
		case partOf(parent.Part) != "":
			k.add("part.nested", mf, "%s: part of %q, which is itself a part: name the top component", where, of)
		case !mentions(parent.Sample, id) && !variantsMention(parent.Variants, id):
			k.add("part.unused", mf, "%s: part of %s, but %s's sample and variants never use it: the parent's picture is the only one it gets", where, of, of)
		}
	}
	return of
}

func (k *checker) keywords(mf, where string, raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var words []string
	if err := json.Unmarshal(raw, &words); err != nil {
		k.add("keywords.shape", mf, "%s: keywords must be a list of strings — the words a person would search by", where)
	}
}

// values checks the props a sample or a variant sets, element values inside them too: every name is declared by the
// component it is passed to, every element names a component of the kit, and names the live preview never passes
// (PreviewProp) are said out loud instead of vanishing from the picture.
func (k *checker) values(mf, where, from string, c checkComponent, props map[string]any, all map[string]checkComponent) {
	declared, required := map[string]bool{}, map[string]bool{}
	enums := map[string][]string{}
	for _, p := range c.Props {
		declared[p.Name] = true
		required[p.Name] = p.Required
		if p.Type == "enum" {
			enums[p.Name] = p.Values
		}
	}
	for name, v := range props {
		if values, isEnum := enums[name]; isEnum {
			if str, ok := v.(string); ok && !slices.Contains(values, str) {
				k.add("sample.enum", mf, "%s: %s sets %s to %q, which is not one of its values %v: the component draws its default or nothing", where, from, name, str, values)
			}
		}
		switch {
		case !declared[name]:
			k.add("sample.undeclared", mf, "%s: %s sets %q, which is not in props: the live preview passes only declared props, so it is dropped (declare it, children too)", where, from, name)
		case !PreviewProp(name) && !required[name]:
			k.add("sample.not-passed", mf, "%s: %s sets %q, which the live preview never passes (handlers, styles, links, dashed names): the picture draws without it", where, from, name)
		}
		k.elements(mf, where, from+"."+name, v, all, 0)
	}
}

func (k *checker) elements(mf, where, at string, v any, all map[string]checkComponent, depth int) {
	if depth > maxElementDepth {
		k.add("sample.depth", mf, "%s: %s nests elements deeper than %d", where, at, maxElementDepth)
		return
	}
	switch x := v.(type) {
	case []any:
		for i, e := range x {
			k.elements(mf, where, fmt.Sprintf("%s[%d]", at, i), e, all, depth)
		}
	case map[string]any:
		if _, fn := x[FunctionKey]; fn {
			if _, ok := FunctionValue(x); !ok {
				k.add("element.function", mf, "%s: %s: a function is {\"%s\": \"\"} or {\"%s\": \"<field>\"} and nothing else", where, at, FunctionKey, FunctionKey)
			}
			return
		}
		id, props, children, ok := Element(x)
		if !ok {
			if _, bad := x[ElementKey]; bad {
				k.add("element.id", mf, "%s: %s: %q must be a component's id", where, at, ElementKey)
			}
			for key, e := range x {
				k.elements(mf, where, at+"."+key, e, all, depth)
			}
			return
		}
		comp, known := all[id]
		if !known {
			k.add("element.unknown", mf, "%s: %s is an element of %q, which is not a component of the kit", where, at, id)
			return
		}
		k.values(mf, where, at+" ("+id+")", comp, props, all)
		if children != nil {
			k.elements(mf, where, at+".children", children, all, depth+1)
		}
	}
}

// mentions tells whether value v holds an element of component id anywhere inside.
func mentions(v any, id string) bool {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if mentions(e, id) {
				return true
			}
		}
	case map[string]any:
		if name, _, _, ok := Element(x); ok && name == id {
			return true
		}
		for _, e := range x {
			if mentions(e, id) {
				return true
			}
		}
	}
	return false
}

func variantsMention(vs []Variant, id string) bool {
	for _, v := range vs {
		if mentions(v.Props, id) {
			return true
		}
	}
	return false
}

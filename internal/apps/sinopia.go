package apps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
)

// What an app puts into Sinopia (the manifest's `sinopia`): panels beside the editor, items of the selection menu,
// bindings of a board's elements to the app's fields, checks, import and export entries, templates of new boards. It is
// read here and checked with the manifest (Manifest.Check) and with the package's files (Package.checkFiles).

// Sinopia is the manifest's `sinopia`.
type Sinopia struct {
	Panels    []SinopiaPanel     `json:"panels,omitempty"`
	Selection []SinopiaSelection `json:"selection,omitempty"`
	Bindings  *SinopiaBindings   `json:"bindings,omitempty"`
	Checks    []SinopiaCheck     `json:"checks,omitempty"`
	Import    []SinopiaImport    `json:"import,omitempty"`
	Export    []SinopiaExport    `json:"export,omitempty"`
	Templates []string           `json:"templates,omitempty"`
	// Kinds are the document types the app adds (kinds.go).
	Kinds []SinopiaKind `json:"kinds,omitempty"`
}

// SinopiaPanel is a view of the app shown to the right of the editor of the documents of the kinds.
type SinopiaPanel struct {
	ID    string   `json:"id"`
	View  string   `json:"view"`
	Title string   `json:"title,omitempty"`
	Kinds []string `json:"kinds,omitempty"`
}

// SinopiaSelection is an item of the menu of a selection: it calls a tool of the app or sends a task to the chat linked
// to the document, with the selection as an <annotation>.
type SinopiaSelection struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Tool  string   `json:"tool,omitempty"`
	Agent string   `json:"agent,omitempty"`
	Kinds []string `json:"kinds,omitempty"`
}

// SinopiaBindings ties the elements marked data-hl-field to the app's fields.
type SinopiaBindings struct {
	// Schema is the package file that describes the fields: a JSON Schema of an object; a value outside it is refused.
	Schema string `json:"schema,omitempty"`
	// Inspector is the view that opens when a bound element is clicked.
	Inspector string `json:"inspector"`
	// LockUnbound keeps free hand edits to the elements that are bound.
	LockUnbound bool `json:"lockUnbound,omitempty"`
}

// SinopiaCheck looks at a document and reports what is wrong with it. It is either a tool of the app's own MCP server
// (Tool: "<server>.<tool>", given the document's nodes) or a list of rules the host evaluates itself (Rules), which
// needs no server and runs in the document's own process.
type SinopiaCheck struct {
	ID    string   `json:"id"`
	Title string   `json:"title,omitempty"`
	Tool  string   `json:"tool,omitempty"`
	Rules []Rule   `json:"rules,omitempty"`
	On    []string `json:"on,omitempty"`
	// BlockShare is the level of a finding that keeps the document from being shared: "error" or "warning".
	BlockShare string   `json:"blockShare,omitempty"`
	Kinds      []string `json:"kinds,omitempty"`
}

// Rule is one declarative rule of a check: it picks elements by a selector and finds fault with those that break a
// limit. A text limit counts the words that are the element's own; a font limit reads the element's inline font-size.
type Rule struct {
	ID       string `json:"id,omitempty"`
	Select   string `json:"select"`
	MaxWords int    `json:"maxWords,omitempty"`
	MaxChars int    `json:"maxChars,omitempty"`
	// MinFontPx is the smallest inline font-size, in px (pt, rem, em and cqw are read against the board's width).
	MinFontPx float64 `json:"minFontPx,omitempty"`
	Level     string  `json:"level,omitempty"`
	// Message is what the person reads; {n} is the measured value, {limit} the limit.
	Message string `json:"message"`
	// Fix is the style that mends a finding of this rule.
	Fix map[string]string `json:"fix,omitempty"`
}

// SinopiaImport is an entry of the import menu: files of the kinds it accepts go to its tool.
type SinopiaImport struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Accept []string `json:"accept,omitempty"`
	Tool   string   `json:"tool"`
}

// SinopiaExport is an entry of the export menu.
type SinopiaExport struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Tool  string   `json:"tool"`
	Kinds []string `json:"kinds,omitempty"`
}

// Check triggers.
const (
	CheckOnSave  = "save"
	CheckOnShare = "share"
)

// Levels of a finding.
const (
	LevelError   = "error"
	LevelWarning = "warning"
	LevelInfo    = "info"
)

var (
	contribIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	toolRefRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}\.[A-Za-z0-9_.-]{1,64}$`)
)

// SinopiaOf is the app's Sinopia contributions; an app that has none gets an empty value. A manifest that was checked
// has a `sinopia` that reads; one that was not may not, and then the error comes back.
func (m *Manifest) SinopiaOf() (Sinopia, error) {
	var s Sinopia
	if len(m.Sinopia) == 0 {
		return s, nil
	}
	dec := json.NewDecoder(bytes.NewReader(m.Sinopia))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Sinopia{}, err
	}
	return s, nil
}

// OnInDocument says whether the app turns on in an open document of a kind: it works in that kind, and it asks to be on
// when a document is open (activation.on has "document").
func (m *Manifest) OnInDocument(kind string) bool {
	return m.Activation != nil && contains(m.Activation.On, OnDocument) && m.HasTarget("sinopia."+kind)
}

// kindIn says whether a list of kinds ("" for all of the app's) names the kind.
func kindIn(kinds []string, kind string) bool { return len(kinds) == 0 || contains(kinds, kind) }

// ForKind is what the app contributes to a document of a kind: the panels, the selection items, the checks, the
// import and export entries meant for it. The bindings are the app's whole.
func (s Sinopia) ForKind(kind string) Sinopia {
	out := Sinopia{Bindings: s.Bindings, Templates: s.Templates}
	for _, p := range s.Panels {
		if kindIn(p.Kinds, kind) {
			out.Panels = append(out.Panels, p)
		}
	}
	for _, p := range s.Selection {
		if kindIn(p.Kinds, kind) {
			out.Selection = append(out.Selection, p)
		}
	}
	for _, p := range s.Checks {
		if kindIn(p.Kinds, kind) {
			out.Checks = append(out.Checks, p)
		}
	}
	out.Import = s.Import
	for _, p := range s.Export {
		if kindIn(p.Kinds, kind) {
			out.Export = append(out.Export, p)
		}
	}
	return out
}

// checkSinopia is the part of the manifest check that is about `sinopia`.
func (m *Manifest) checkSinopia(targets map[string]bool, add func(path, format string, a ...any), warn func(path, format string, a ...any)) {
	if len(m.Sinopia) == 0 {
		return
	}
	s, err := m.SinopiaOf()
	if err != nil {
		add("sinopia", "%s", decodeMessage(m.Sinopia, err))
		return
	}
	for _, field := range []struct {
		name string
		n    int
	}{{"import", len(s.Import)}, {"export", len(s.Export)}} {
		if field.n > 0 {
			add("sinopia."+field.name, "not supported yet: the host does not run it, remove it from the manifest")
		}
	}
	// A contribution to a kind the app does not work in is a mistake: the app would put something where it is not on.
	kindsOK := func(path string, kinds []string) {
		for i, k := range kinds {
			p := fmt.Sprintf("%s.kinds[%d]", path, i)
			switch {
			case !kindRe.MatchString(k) && !appKindRe.MatchString(k):
				add(p, "%q is not a document type", k)
			case !targets["sinopia."+k] && !targets["sinopia.*"]:
				add(p, "the app has no target sinopia.%s", k)
			}
		}
	}
	ids := func(path string, seen map[string]bool, id string) {
		switch {
		case !contribIDRe.MatchString(id):
			add(path+".id", "%q: an id is lower case latin letters, digits and '-'", id)
		case seen[id]:
			add(path+".id", "%q is used twice", id)
		}
		seen[id] = true
	}
	tool := func(path, ref string) {
		server, _, _ := strings.Cut(ref, ".")
		switch {
		case !toolRefRe.MatchString(ref):
			add(path, "%q is a tool as <server>.<tool> of the app's MCP server", ref)
		case m.Agent == nil || !contains(m.Agent.MCP, server):
			add(path, "the server %q is not in agent.mcp", server)
		}
	}
	view := func(path, id string) {
		if v := m.view(id); v == nil {
			add(path, "there is no view %q", id)
		} else if !hasSinopiaView(v.Targets) {
			add(path, "the view %q has no sinopia target", id)
		}
	}

	m.checkKinds(s, targets, add)

	seen := map[string]bool{}
	for i, p := range s.Panels {
		path := fmt.Sprintf("sinopia.panels[%d]", i)
		ids(path, seen, p.ID)
		view(path+".view", p.View)
		kindsOK(path, p.Kinds)
	}
	seen = map[string]bool{}
	for i, it := range s.Selection {
		path := fmt.Sprintf("sinopia.selection[%d]", i)
		ids(path, seen, it.ID)
		if strings.TrimSpace(it.Title) == "" {
			add(path+".title", "is required: the item's label")
		}
		switch {
		case it.Tool == "" && it.Agent == "":
			add(path, "needs tool or agent")
		case it.Tool != "" && it.Agent != "":
			add(path, "is a tool or an agent task, not both")
		case it.Tool != "":
			tool(path+".tool", it.Tool)
		}
		kindsOK(path, it.Kinds)
	}
	if b := s.Bindings; b != nil {
		view("sinopia.bindings.inspector", b.Inspector)
		if b.Schema == "" && b.LockUnbound {
			warn("sinopia.bindings", "lockUnbound without a schema: the fields are not described, a value is not checked")
		}
	}
	seen = map[string]bool{}
	for i, c := range s.Checks {
		path := fmt.Sprintf("sinopia.checks[%d]", i)
		ids(path, seen, c.ID)
		switch {
		case c.Tool == "" && len(c.Rules) == 0:
			add(path, "needs tool or rules")
		case c.Tool != "" && len(c.Rules) > 0:
			add(path, "is a tool or rules, not both")
		case c.Tool != "":
			tool(path+".tool", c.Tool)
		}
		if len(c.On) == 0 {
			add(path+".on", "is required: save, share or both")
		}
		for j, on := range c.On {
			if on != CheckOnSave && on != CheckOnShare {
				add(fmt.Sprintf("%s.on[%d]", path, j), "%q is not save or share", on)
			}
		}
		switch c.BlockShare {
		case "", LevelError, LevelWarning:
			if c.BlockShare != "" && !contains(c.On, CheckOnShare) {
				warn(path+".blockShare", "blocks sharing but the check does not run on share")
			}
		default:
			add(path+".blockShare", "%q is not error or warning", c.BlockShare)
		}
		ruleIDs := map[string]bool{}
		for j, r := range c.Rules {
			rp := fmt.Sprintf("%s.rules[%d]", path, j)
			if err := design.ValidSelector(r.Select); err != nil {
				add(rp+".select", "%v", err)
			}
			if r.MaxWords < 0 || r.MaxChars < 0 || r.MinFontPx < 0 {
				add(rp, "a limit is not negative")
			}
			if r.MaxWords == 0 && r.MaxChars == 0 && r.MinFontPx == 0 {
				add(rp, "needs a limit: maxWords, maxChars or minFontPx")
			}
			if strings.TrimSpace(r.Message) == "" {
				add(rp+".message", "is required: what the person reads")
			}
			switch r.Level {
			case "", LevelError, LevelWarning, LevelInfo:
			default:
				add(rp+".level", "%q is not error, warning or info", r.Level)
			}
			if r.ID != "" {
				if !contribIDRe.MatchString(r.ID) || ruleIDs[r.ID] {
					add(rp+".id", "%q is not an id, or is used twice", r.ID)
				}
				ruleIDs[r.ID] = true
			}
			for prop := range r.Fix {
				if !cssPropRe.MatchString(prop) {
					add(rp+".fix", "%q is not a style property", prop)
				}
			}
		}
		kindsOK(path, c.Kinds)
	}
	seen = map[string]bool{}
	for i, im := range s.Import {
		path := fmt.Sprintf("sinopia.import[%d]", i)
		ids(path, seen, im.ID)
		if strings.TrimSpace(im.Title) == "" {
			add(path+".title", "is required: the item's label")
		}
		tool(path+".tool", im.Tool)
	}
	seen = map[string]bool{}
	for i, ex := range s.Export {
		path := fmt.Sprintf("sinopia.export[%d]", i)
		ids(path, seen, ex.ID)
		if strings.TrimSpace(ex.Title) == "" {
			add(path+".title", "is required: the item's label")
		}
		tool(path+".tool", ex.Tool)
		kindsOK(path, ex.Kinds)
	}
	for i, t := range s.Templates {
		if strings.TrimSpace(t) == "" {
			add(fmt.Sprintf("sinopia.templates[%d]", i), "is a folder of the package")
		}
	}

	// The permissions a contribution leans on: without them it can only fail when it is used.
	needs := func(path, perm, why string) {
		if !contains(m.Permissions, perm) {
			warn(path, "%s needs the permission %s", why, perm)
		}
	}
	if len(s.Panels) > 0 || s.Bindings != nil {
		needs("sinopia", "sinopia.read", "a panel or a binding reads the document, so it")
	}
	if s.Bindings != nil {
		needs("sinopia.bindings", "sinopia.write", "a binding writes the field into the board, so it")
	}
	if len(s.Export) > 0 {
		needs("sinopia.export", "sinopia.export", "an export entry")
	}
}

var cssPropRe = regexp.MustCompile(`^(--[A-Za-z0-9_-]{1,60}|-?[a-z][a-z-]{0,60})$`)

func hasSinopiaView(targets []string) bool {
	for _, t := range targets {
		if strings.HasPrefix(t, "sinopia.") {
			return true
		}
	}
	return false
}

// SinopiaApp is Sinopia, the builtin app (builtin/sinopia/harnessly/app.json) whose points the manifest's `sinopia`
// contributes to: sinopia.kind, sinopia.panel, sinopia.check and the rest. The targets sinopia.<kind> and sinopia.*
// are its aliases.
const SinopiaApp = "harnessly.sinopia"

// The readers of Sinopia's points: each reads one list of a manifest's `sinopia` (HRN-808).
func init() {
	pointReaders["sinopia.kind"] = func(m *Manifest) []Contribution {
		s, err := m.SinopiaOf()
		if err != nil {
			return nil
		}
		var out []Contribution
		for _, k := range s.Kinds {
			out = append(out, Contribution{Kind: "sinopia.kind", ID: k.ID, Target: "sinopia." + k.ID, Spec: k})
		}
		return out
	}
	pointReaders["sinopia.panel"] = sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
		for _, p := range s.Panels {
			add(p.ID, p.Kinds, p)
		}
	}, "sinopia.panel")
	pointReaders["sinopia.selection"] = sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
		for _, p := range s.Selection {
			add(p.ID, p.Kinds, p)
		}
	}, "sinopia.selection")
	pointReaders["sinopia.check"] = sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
		for _, p := range s.Checks {
			add(p.ID, p.Kinds, p)
		}
	}, "sinopia.check")
	pointReaders["sinopia.export"] = sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
		for _, p := range s.Export {
			add(p.ID, p.Kinds, p)
		}
	}, "sinopia.export")
	pointReaders["sinopia.import"] = sinopiaPart(func(s Sinopia, add func(id string, kinds []string, spec any)) {
		for _, p := range s.Import {
			add(p.ID, nil, p)
		}
	}, "sinopia.import")
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
					if IsSinopiaTarget(t) {
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

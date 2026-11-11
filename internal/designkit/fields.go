package designkit

// The fields the contract gained in HRN-1007 (K1), all optional: a kit without them is checked exactly as before.
//
//	component.code     {import, export}  the real import the component stands for
//	component.preset   {of, props}       a name that expands to another component with fixed props
//	component.status   stable | new | deprecated;  component.replacedBy: the component that takes its place
//	kit.kind           app | site | slides | video
//	kit.extends        "<id>@<range>"    the base kit (extends.go: the child is assembled with it)
//	kit.omitted        sections left out on purpose (tokens, examples, docs, patterns, templates)
//	token.var          the library's CSS variable a token's name stands for
//	kit.strict         true: projects on the kit are checked in strict mode (strict.go)

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Kit kinds and component statuses.
var (
	kitKinds  = map[string]bool{"app": true, "site": true, "slides": true, "video": true}
	statuses  = map[string]bool{"stable": true, "new": true, "deprecated": true}
	omittable = map[string]bool{"tokens": true, "examples": true, "docs": true, "patterns": true, "templates": true}
	cssVarRe  = regexp.MustCompile(`^--[A-Za-z0-9][A-Za-z0-9_-]{0,80}$`)
	extendsRe = regexp.MustCompile(`^([a-z0-9][a-z0-9-]{0,62})@(.+)$`)
)

// kitRecord is the members of the kit record the new checks read.
type kitFields struct {
	ID      string          `json:"id"`
	Version string          `json:"version"`
	Kind    json.RawMessage `json:"kind"`
	Extends json.RawMessage `json:"extends"`
	Omitted json.RawMessage `json:"omitted"`
	Strict  json.RawMessage `json:"strict"`
}

// recordFields checks kind, extends and omitted of a kit record and returns the sections it leaves out.
func (k *checker) recordFields(mf, name string, own kitFields) map[string]bool {
	if s, ok := jsonString(own.Kind); own.Kind != nil && string(own.Kind) != "null" && (!ok || !kitKinds[s]) {
		k.add("identity.kind", mf, "%s.kind %s: one of app, site, slides, video", name, string(own.Kind))
	}
	if own.Extends != nil && string(own.Extends) != "null" {
		s, ok := jsonString(own.Extends)
		sub := extendsRe.FindStringSubmatch(s)
		switch {
		case !ok || sub == nil:
			k.add("identity.extends", mf, "%s.extends %s: expected \"<kit id>@<version range>\", e.g. \"acme-foundation@^2\"", name, string(own.Extends))
		case sub[1] == own.ID:
			k.add("identity.extends", mf, "%s.extends: the kit extends itself", name)
		default:
			if _, err := parseRange(strings.TrimSpace(sub[2])); err != nil {
				k.add("identity.extends", mf, "%s.extends %q: %v", name, s, err)
			}
		}
	}
	if own.Strict != nil && string(own.Strict) != "null" && string(own.Strict) != "true" && string(own.Strict) != "false" {
		k.add("identity.strict", mf, "%s.strict %s: true or false", name, string(own.Strict))
	}
	omitted := map[string]bool{}
	if own.Omitted != nil && string(own.Omitted) != "null" {
		var list []string
		if json.Unmarshal(own.Omitted, &list) != nil {
			k.add("identity.omitted", mf, "%s.omitted must be a list of section names: tokens, examples, docs, patterns, templates", name)
		}
		for _, o := range list {
			if !omittable[o] {
				k.add("identity.omitted", mf, "%s.omitted: %q is not one of tokens, examples, docs, patterns, templates", name, o)
				continue
			}
			omitted[o] = true
		}
	}
	return omitted
}

func jsonString(raw json.RawMessage) (string, bool) {
	var s string
	if raw == nil || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// code checks a component's code and says whether it is a good {import, export}.
func (k *checker) code(mf, where string, raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var c map[string]json.RawMessage
	if json.Unmarshal(raw, &c) != nil {
		k.add("code.shape", mf, `%s: code must be {"import": "<module>", "export": "<name>"}`, where)
		return false
	}
	good := true
	for _, field := range []string{"import", "export"} {
		if s, ok := jsonString(c[field]); !ok || strings.TrimSpace(s) == "" {
			k.add("code.shape", mf, "%s: code.%s must be a non-empty string", where, field)
			good = false
		}
	}
	return good
}

// status checks status and replacedBy.
func (k *checker) status(mf, where, id string, status, replaced json.RawMessage, all map[string]checkComponent) {
	if len(status) > 0 && string(status) != "null" {
		if s, ok := jsonString(status); !ok || !statuses[s] {
			k.add("status.value", mf, "%s: status %s must be stable, new or deprecated", where, string(status))
		}
	}
	if len(replaced) > 0 && string(replaced) != "null" {
		s, ok := jsonString(replaced)
		if _, found := all[s]; !ok || !found || s == id {
			k.add("status.replaced-by", mf, "%s: replacedBy %s must name another component of the kit", where, string(replaced))
		} else if st, _ := jsonString(status); st != "deprecated" {
			k.add("status.replaced-by", mf, "%s: replacedBy is for a deprecated component: set status to deprecated", where)
		}
	}
}

// presetOf checks a component's preset and returns the component it expands to ("" when it has none or it is broken).
func (k *checker) presetOf(mf, where, id string, raw json.RawMessage, all map[string]checkComponent) (target string, has bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var p struct {
		Of    json.RawMessage `json:"of"`
		Props json.RawMessage `json:"props"`
	}
	if json.Unmarshal(raw, &p) != nil {
		k.add("preset.shape", mf, `%s: preset must be {"of": "<component>", "props": {…}}`, where)
		return "", true
	}
	of, ok := jsonString(p.Of)
	if !ok || of == "" {
		k.add("preset.shape", mf, `%s: preset.of must name a component of the kit`, where)
		return "", true
	}
	var props map[string]any
	if len(p.Props) > 0 && string(p.Props) != "null" && json.Unmarshal(p.Props, &props) != nil {
		k.add("preset.shape", mf, `%s: preset.props must be an object`, where)
		return "", true
	}
	t, found := all[of]
	switch {
	case of == id:
		k.add("preset.target", mf, "%s: a preset of itself", where)
		return "", true
	case !found:
		k.add("preset.target", mf, "%s: preset of %q, which is not a component of the kit", where, of)
		return "", true
	case len(t.Preset) > 0 && string(t.Preset) != "null":
		k.add("preset.target", mf, "%s: preset of %q, which is a preset itself: a preset points at a real component (no chains)", where, of)
		return "", true
	}
	declared := map[string]Prop{}
	for _, pr := range t.Props {
		declared[pr.Name] = pr
	}
	for name, v := range props {
		pr, ok := declared[name]
		switch {
		case !ok:
			k.add("preset.props", mf, "%s: preset sets %q, which %s does not declare", where, name, of)
		case !simpleValue(v):
			k.add("preset.props", mf, "%s: preset prop %q is not a plain value (text, number, true/false, null)", where, name)
		case pr.Type == "enum" && !enumHas(pr, v):
			k.add("preset.props", mf, "%s: preset sets %s to %v, which is not one of its values", where, name, v)
		}
	}
	return of, true
}

func simpleValue(v any) bool {
	switch v.(type) {
	case nil, string, float64, bool:
		return true
	}
	return false
}

func enumHas(p Prop, v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, x := range p.Values {
		if x == s {
			return true
		}
	}
	return false
}

// tokenVar checks a token's var.
func (k *checker) tokenVar(tf, family, name string, raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	if s, ok := jsonString(raw); !ok || !cssVarRe.MatchString(s) {
		k.add("token.var", tf, "%s: %s var %s must be a CSS custom property name, --acme-color-accent", family, name, string(raw))
	}
}

// extendsLink resolves the base of the theme being checked and reads what it inherits: k.inherited, for the checks that
// name other components. A base that cannot be used is reported by rule (extends.missing, extends.version,
// extends.cycle, extends.depth); without a kits folder the link is not followed.
func (k *checker) extendsLink(mf string, raw []byte) {
	k.inherited, k.extending = nil, false
	ex, ok := ExtendsOf(raw)
	if !ok {
		return
	}
	k.extending = true
	if k.dir == "" {
		return
	}
	var self struct{ ID, Version string }
	if id, version, err := Identity(k.root); err == nil {
		self.ID, self.Version = id, version
	}
	version, info := ResolveBase(k.dir, self.ID, "", ex, []string{self.ID})
	switch info.Rule {
	case "extends.missing":
		k.add("extends.missing", mf, "%s", info.Error)
	case "extends.version":
		k.add("extends.version", mf, "%s", info.Error)
	case "extends.cycle":
		k.add("extends.cycle", mf, "%s", info.Error)
	case "extends.depth":
		k.add("extends.depth", mf, "%s", info.Error)
	}
	if info.Error != "" {
		return
	}
	b, err := readCanvas(k.dir, ex.Kit, version, "", []string{self.ID})
	if err != nil {
		k.add("extends.missing", mf, "the base kit %s %s cannot be read: %v", ex.Kit, version, err)
		return
	}
	if b.Extends != nil && b.Extends.Error != "" {
		k.add(b.Extends.Rule, mf, "the base %s: %s", ex.Kit, b.Extends.Error)
	}
	k.inherited = map[string]*Component{}
	for _, g := range b.Groups {
		for i := range g.Components {
			c := g.Components[i]
			c.From = firstNonEmpty(c.From, ex.Kit+"@"+version)
			k.inherited[c.ID] = &c
		}
	}
}

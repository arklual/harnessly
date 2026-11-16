package kitadd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// openProps are the boolean props that make an overlay or a collapsible draw its content.
var openProps = []string{"open", "isOpen", "in", "show", "visible", "defaultOpen", "defaultVisible", "expanded", "defaultExpanded", "opened"}

// textProps are the props that carry a visible label when children is missing or does not show.
var textProps = []string{"label", "title", "content", "text", "description", "message", "heading", "header", "name", "placeholder", "value"}

// RetrySamples changes the first sample of the components in ids for one more try at drawing them alone (attempt 1, 2,
// 3): 1 opens what can be opened and gives children a text; 2 also fills the label-like props; 3 puts two plain entries
// into every list prop. It reports the ids it changed; a component the attempt has nothing new for is left out, so the
// caller stops trying it.
func RetrySamples(folder string, ids []string, attempt int) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(folder, designkit.ManifestFile))
	if err != nil {
		return nil, err
	}
	doc, ok := parseObj(raw)
	if !ok {
		return nil, fmt.Errorf("%s is not valid JSON", designkit.ManifestFile)
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var groups []json.RawMessage
	_ = json.Unmarshal(doc.vals["groups"], &groups)
	var out []*obj
	var changed []string
	for _, g := range groups {
		go_, _ := parseObj(g)
		var cs []json.RawMessage
		_ = json.Unmarshal(go_.vals["components"], &cs)
		var keep []*obj
		for _, c := range cs {
			co, _ := parseObj(c)
			id := co.str("id")
			if want[id] {
				if tryMore(co, id, attempt) {
					changed = append(changed, id)
				}
			}
			keep = append(keep, co)
		}
		go_.set("components", keep)
		out = append(out, go_)
	}
	if len(changed) == 0 {
		return nil, nil
	}
	doc.set("groups", out)
	return changed, os.WriteFile(filepath.Join(folder, designkit.ManifestFile), pretty(doc), 0o644)
}

func tryMore(co *obj, id string, attempt int) bool {
	var props []kitProp
	_ = json.Unmarshal(co.vals["props"], &props)
	sample := map[string]any{}
	if len(co.vals["sample"]) > 0 {
		_ = json.Unmarshal(co.vals["sample"], &sample)
	}
	before, _ := json.Marshal(sample)
	if attempt >= 1 && !hasProp(props, "children") && !noChildren.MatchString(id) {
		// Typed as HTML attributes, a component often takes children the analysis did not list: declare it, so the sample
		// (and the preview, which passes only declared props) can hold a text.
		var all []map[string]any
		_ = json.Unmarshal(co.vals["props"], &all)
		all = append(all, map[string]any{"name": "children", "type": "node", "description": "What the component holds."})
		co.set("props", all)
		props = append(props, kitProp{Name: "children", Type: "node"})
		before = append(before, ' ') // the declaration alone is a change
	}
	has := func(n string) (kitProp, bool) {
		for _, p := range props {
			if p.Name == n {
				return p, true
			}
		}
		return kitProp{}, false
	}
	set := func(n string, v any) {
		if _, there := sample[n]; !there {
			sample[n] = v
		}
	}
	if attempt >= 1 {
		for _, n := range openProps {
			if p, ok := has(n); ok && p.Type == "boolean" {
				sample[n] = true
			}
		}
		if _, ok := has("children"); ok {
			set("children", titleCase(humanize(id)))
		}
	}
	if attempt >= 2 {
		if _, isList := sample["children"].([]any); isList {
			// the parts an export name suggested (BadgeRoot, BadgeMark) may be slots, not what the component holds
			sample["children"] = titleCase(humanize(id))
		}
		for _, n := range textProps {
			if p, ok := has(n); ok && (p.Type == "node" || p.Type == "string" || p.Type == "") {
				set(n, titleCase(humanize(n)))
			}
		}
	}
	if attempt >= 3 {
		for _, p := range props {
			if v, ok := illustrative(id, p); ok {
				set(p.Name, v)
			}
			if p.Type == "array" {
				set(p.Name, []any{map[string]any{"key": "one", "value": "one", "label": "One", "title": "One"}, map[string]any{"key": "two", "value": "two", "label": "Two", "title": "Two"}})
			}
		}
	}
	after, _ := json.Marshal(sample)
	if string(before) == string(after) {
		return false
	}
	co.set("sample", sample)
	return true
}

// noChildren are the components that hold nothing (a field, a separator, a picture).
var noChildren = regexp.MustCompile(`(?i)(input|divider|separator|spacer|skeleton|progress|slider|switch|checkbox|radio|image|avatar|icon)$`)

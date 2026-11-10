package designkit

import (
	"regexp"
	"strings"
)

// ElementKey marks a prop value that is another component of the kit rather than data:
//
//	{"$c": "Button", "props": {"appearance": "primary"}, "children": "Save"}
//
// A sample, a variant or a layout's props hold one wherever the component takes a React element: a tooltip's trigger,
// a dialog's body made of its parts, an icon inside a button. The live preview (the kit screen's _play page and
// `harnessly design kit render`) turns it into React.createElement of that component; children is a value, an element
// or a list of them. One spelling for every kit: the check, the preview and the docs read this const.
const ElementKey = "$c"

// maxElementDepth bounds how deep elements nest in one value (elements only: lists and objects between them do not
// count): a preview, not a page.
const maxElementDepth = 12

// FunctionKey marks a prop value that is a function: {"$fn": ""} does nothing, {"$fn": "id"} returns its argument's
// field id (a row's key, an item's label). JSON cannot carry code; this is enough for a required callback or accessor
// to let a component draw (DataTable's rowKey, a list's getLabel).
const FunctionKey = "$fn"

// FunctionValue reads v as a function value: the field it returns, "" for one that does nothing.
func FunctionValue(v any) (field string, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap || len(m) != 1 {
		return "", false
	}
	field, ok = m[FunctionKey].(string)
	if ok && field != "" && !previewPropRe.MatchString(field) {
		return "", false
	}
	return field, ok
}

var previewPropRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]{0,40}$`)

// PreviewProp tells whether the live preview passes a prop of this name: a plain name (letters and digits — the board
// engine reads a dashed attribute under other spellings, HRN-897), never a handler, a style, a link or a loader,
// whatever the kit declares. The check warns about sample props it leaves out, so a picture never silently lacks one.
func PreviewProp(name string) bool {
	if !previewPropRe.MatchString(name) {
		return false
	}
	l := strings.ToLower(name)
	if strings.HasPrefix(l, "on") || strings.HasPrefix(l, "style") || strings.HasPrefix(l, "sc") {
		return false
	}
	switch l {
	case "from", "ref", "key", "class", "classname", "is", "slot", "src", "srcdoc", "href", "action", "formaction",
		"autofocus", "tabindex", "dangerouslysetinnerhtml":
		return false
	}
	return true
}

// Element reads v as an element value: the component's id, its props and its children (nil without).
func Element(v any) (id string, props map[string]any, children any, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap {
		return "", nil, nil, false
	}
	name, isName := m[ElementKey].(string)
	if !isName {
		return "", nil, nil, false
	}
	props, _ = m["props"].(map[string]any)
	return name, props, m["children"], true
}

// ElementID is a component id an element may name: the export's name in the namespace, dotted for a member
// (Button, Dialog.Body).
func ElementID(id string) bool { return compIDRe.MatchString(id) }

// Component finds a component of the canvas by id (or name).
func (c *Canvas) Component(id string) *Component {
	for gi := range c.Groups {
		for ci := range c.Groups[gi].Components {
			k := &c.Groups[gi].Components[ci]
			if k.ID == id || k.Name == id {
				return k
			}
		}
	}
	return nil
}

// CleanValue is v with what the live preview may build: elements of components the canvas has, with props the
// component declares and PreviewProp allows, nested at most maxElementDepth deep. Anything else in an element is
// dropped; plain data (strings, numbers, lists, objects) passes as it is.
func (c *Canvas) CleanValue(v any) any { return c.cleanValue(v, 0) }

func (c *Canvas) cleanValue(v any, depth int) any {
	if depth > maxElementDepth {
		return nil
	}
	switch x := v.(type) {
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, c.cleanValue(e, depth))
		}
		return out
	case map[string]any:
		if field, ok := FunctionValue(x); ok {
			return map[string]any{FunctionKey: field}
		}
		if _, bad := x[FunctionKey]; bad {
			return nil
		}
		if id, props, children, ok := Element(x); ok {
			comp := c.Component(id)
			if !ElementID(id) || comp == nil {
				return nil
			}
			declared := map[string]bool{}
			for _, p := range comp.Props {
				declared[p.Name] = true
			}
			clean := map[string]any{}
			for k, pv := range props {
				if declared[k] && k != "children" && PreviewProp(k) {
					clean[k] = c.cleanValue(pv, depth+1)
				}
			}
			if children == nil {
				children = props["children"]
			}
			el := map[string]any{ElementKey: comp.ID, "props": clean}
			if children != nil {
				el["children"] = c.cleanValue(children, depth+1)
			}
			return el
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = c.cleanValue(e, depth)
		}
		return out
	}
	return v
}

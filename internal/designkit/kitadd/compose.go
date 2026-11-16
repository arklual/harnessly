package kitadd

import (
	"regexp"
	"strings"
)

// General rules for the first sample of a component, by the types and names of its props and parts and never by the
// name of a package. recipes.go holds optional hints on top of them.

// openProps are the boolean props that make an overlay, a collapsible or a transition draw its content.
var shownProps = []string{"open", "isOpen", "visible", "isVisible", "show", "in", "present", "opened", "mounted"}

var (
	triggerRe = regexp.MustCompile(`(?:Trigger|Anchor|Target|Reference|Activator)$`)
	wrapperRe = regexp.MustCompile(`(?:Backdrop|Overlay|Positioner|Container|Portal|Popover|Popup|Wrapper)$`)
	contentRe = regexp.MustCompile(`(?:Content|Dialog|Panel|Menu|Popup|Surface|Drawer|Dropdown)$`)
	// fillerNames are a plain container a transition can move or a trigger can wrap, in order of preference.
	fillerNames = []string{"Box", "Div", "Stack", "Flex", "Paper", "Card", "Container", "Group", "View", "Text", "Paragraph", "Typography"}
)

// fillerFor is a component of the kit that draws a plain block with children, "" when it has none.
func fillerFor(has func(string) bool) string {
	for _, n := range fillerNames {
		if has(n) {
			return n
		}
	}
	return ""
}

// shownProp is the boolean prop of a component that shows its content, "" when it has none.
func shownProp(props []kitProp) string {
	for _, n := range shownProps {
		for _, p := range props {
			if p.Name == n && p.Type == "boolean" {
				return n
			}
		}
	}
	return ""
}

// containerPartRe: parts that hold content even when the package's types do not list children for them (polymorphic props).
var containerPartRe = regexp.MustCompile(`(Section|Body|Content|Header|Footer|Panel|Cell|Td|Th|Caption|Title|Description|Label|Text)$`)

// tableParts nests the parts the way an HTML table is written: a head and a body of rows, a row of cells. Nil when the
// parts do not have a row and a cell.
func tableParts(id string, parts []string, partProps map[string][]kitProp) []any {
	has := map[string]string{}
	for _, p := range parts {
		for _, s := range []string{"Thead", "Tbody", "Tfoot", "Tr", "Th", "Td", "Header", "Body", "Row", "Cell", "HeaderCell"} {
			if p == id+s {
				has[s] = p
			}
		}
	}
	row, head, body := has["Tr"], has["Thead"], has["Tbody"]
	cell, hcell := has["Td"], has["Th"]
	if row == "" {
		row = has["Row"]
	}
	if head == "" {
		head = has["Header"]
	}
	if body == "" {
		body = has["Body"]
	}
	if cell == "" {
		cell = has["Cell"]
	}
	if hcell == "" {
		hcell = has["HeaderCell"]
	}
	if row == "" || (cell == "" && hcell == "") {
		return nil
	}
	cells := func(c string, label string) []any {
		if c == "" {
			return nil
		}
		var out []any
		for _, n := range []string{label + " 1", label + " 2"} {
			el := partNode(c, partProps[c], n)
			out = append(out, el)
		}
		return out
	}
	mk := func(c string, label string) any {
		return partNode(row, partProps[row], cells(c, label))
	}
	var kids []any
	switch {
	case head != "" && body != "":
		hc := hcell
		if hc == "" {
			hc = cell
		}
		kids = append(kids, partNode(head, partProps[head], []any{mk(hc, "Heading")}))
		bc := cell
		if bc == "" {
			bc = hcell
		}
		kids = append(kids, partNode(body, partProps[body], []any{mk(bc, "Cell"), mk(bc, "Cell")}))
	case body != "":
		kids = append(kids, partNode(body, partProps[body], []any{mk(cell, "Cell"), mk(cell, "Cell")}))
	default:
		kids = append(kids, mk(firstNonEmpty(cell, hcell), "Cell"))
	}
	return kids
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// partNode is a part as a child of a sample: required props set, a label when it takes children.
func partNode(part string, pp []kitProp, children any) map[string]any {
	el := map[string]any{"$c": part}
	req := map[string]any{}
	for _, p := range pp {
		if p.Required && p.Name != "children" {
			req[p.Name] = valueFor(p)
		}
	}
	if len(req) > 0 {
		el["props"] = req
	}
	if children != nil {
		el["children"] = children
	} else if hasProp(pp, "children") || containerPartRe.MatchString(part) {
		{
			el["children"] = humanize(part)
		}
	}
	return el
}

// composeOpen nests the parts of an overlay the way they are used: the trigger beside, then the wrappers (backdrop,
// positioner, container), inside them the content, inside that every other part.
func composeOpen(parts []string, partProps map[string][]kitProp, filler string) []any {
	var triggers, wrappers, contents, others []string
	for _, p := range parts {
		switch {
		case triggerRe.MatchString(p):
			triggers = append(triggers, p)
		case contentRe.MatchString(p) && !wrapperRe.MatchString(p):
			contents = append(contents, p)
		case wrapperRe.MatchString(p):
			wrappers = append(wrappers, p)
		default:
			others = append(others, p)
		}
	}
	if len(wrappers)+len(contents) == 0 {
		return nil // nothing to nest: the flat list of parts is as good
	}
	inner := []any{}
	for _, o := range others {
		inner = append(inner, partNode(o, partProps[o], nil))
	}
	var level any
	if len(contents) > 0 {
		for i := len(contents) - 1; i >= 0; i-- {
			var kids any
			if i == 0 && len(inner) > 0 {
				kids = inner
			}
			level = partNode(contents[i], partProps[contents[i]], kids)
		}
	} else if len(inner) > 0 {
		level = inner
	}
	for i := len(wrappers) - 1; i >= 0; i-- {
		w := partNode(wrappers[i], partProps[wrappers[i]], level)
		if level == nil {
			delete(w, "children")
		}
		level = w
	}
	var out []any
	for _, t := range triggers {
		out = append(out, triggerNode(t, partProps[t], filler))
	}
	if level != nil {
		out = append(out, level)
	}
	return out
}

// itemSuffixes name a child of a collection: ListBoxItem in ListBox, SelectOption in Select.
var itemSuffixes = []string{"Item", "Section", "Option", "Step", "Tab", "Tag"}

// collectionParent is the collection a component is an item of: <Parent><Item suffix>, or a singular whose plural or
// group exists (Tab → Tabs, Radio → RadioGroup, Tag → TagGroup).
func collectionParent(id string, have map[string]bool) string {
	for _, suf := range itemSuffixes {
		if base, ok := strings.CutSuffix(id, suf); ok && base != "" && have[base] {
			return base
		}
	}
	for _, suf := range []string{"s", "Group", "List", "Set"} {
		if have[id+suf] {
			return id + suf
		}
	}
	return ""
}

// triggerNode is a trigger, anchor or target part: it hands its behaviour to one element (a text would be refused), so it
// holds a plain element of the kit.
func triggerNode(part string, pp []kitProp, filler string) map[string]any {
	if filler == "" {
		return partNode(part, pp, nil)
	}
	return partNode(part, pp, map[string]any{"$c": filler, "children": humanize(part)})
}

// flatParts lists the parts of a component as children of its sample. Triggers hold an element; when the component has
// one <Name>Item part, the other parts live inside it (an Accordion's Control and Panel are the item's).
func flatParts(id string, parts []string, partProps map[string][]kitProp, filler string) []any {
	if tbl := tableParts(id, parts, partProps); tbl != nil {
		return tbl
	}
	item := ""
	for _, p := range parts {
		if p == id+"Item" {
			item = p
		}
	}
	var kids, inner []any
	for _, part := range parts {
		var node map[string]any
		switch {
		case triggerRe.MatchString(part):
			node = triggerNode(part, partProps[part], filler)
		default:
			node = partNode(part, partProps[part], nil)
		}
		if item != "" && part != item && !triggerRe.MatchString(part) {
			inner = append(inner, node)
			continue
		}
		if part == item {
			continue
		}
		kids = append(kids, node)
		if len(kids) == 8 {
			break
		}
	}
	if item != "" {
		var children any
		if len(inner) > 0 {
			children = inner
		}
		kids = append(kids, partNode(item, partProps[item], children))
	}
	return kids
}

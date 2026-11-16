package kitadd

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// foldRoots drops <Name>Root where <Name> is exported too (HeroUI: Modal and ModalRoot are the same component, the
// first with its parts as properties): the flat name stays importable, the kit lists the component once, and the parts
// that named the Root as their parent belong to <Name>.
func foldRoots(comps []genComp) []genComp {
	have := map[string]bool{}
	for _, c := range comps {
		have[c.ID] = true
	}
	folded := map[string]string{}
	for _, c := range comps {
		if base, ok := strings.CutSuffix(c.ID, "Root"); ok && base != "" && have[base] {
			folded[c.ID] = base
		}
	}
	if len(folded) == 0 {
		return comps
	}
	var out []genComp
	for _, c := range comps {
		if _, drop := folded[c.ID]; drop {
			continue
		}
		if to, ok := folded[c.Part]; ok {
			c.Part = to
		}
		out = append(out, c)
	}
	return out
}

// sampleRecipes are first samples, checked on the real packages, for the components that draw nothing on their own: a
// transition needs an element to move, a tab list its tabs, a menu or a dialog its trigger and content and to be open, a
// tag group the collection it belongs to. They apply only when every component and prop they name is in the kit, so a
// version that renamed one falls back to the generated sample.
var sampleRecipes = map[string]map[string]string{
	"@mui/material": {
		"Accordion":          `{"children":[{"$c":"AccordionSummary","children":"Title"},{"$c":"AccordionDetails","children":"Content"}]}`,
		"AvatarGroup":        `{"children":[{"$c":"Avatar","children":"A"},{"$c":"Avatar","children":"B"}]}`,
		"Breadcrumbs":        `{"children":[{"$c":"Link","children":"Home"},{"$c":"Typography","children":"Page"}]}`,
		"ButtonGroup":        `{"children":[{"$c":"Button","children":"One"},{"$c":"Button","children":"Two"}]}`,
		"Checkbox":           `{"defaultChecked":true}`,
		"Radio":              `{"checked":true}`,
		"Switch":             `{"defaultChecked":true}`,
		"CircularProgress":   `{"variant":"determinate","value":60}`,
		"LinearProgress":     `{"variant":"determinate","value":60}`,
		"ClickAwayListener":  `{"children":{"$c":"Box","children":"Inside"}}`,
		"FormControlLabel":   `{"label":"Label","control":{"$c":"Checkbox"}}`,
		"Fade":               `{"in":true,"children":{"$c":"Box","children":"Fade"}}`,
		"Grow":               `{"in":true,"children":{"$c":"Box","children":"Grow"}}`,
		"Slide":              `{"in":true,"children":{"$c":"Box","children":"Slide"}}`,
		"Zoom":               `{"in":true,"children":{"$c":"Box","children":"Zoom"}}`,
		"Input":              `{"placeholder":"Type"}`,
		"FilledInput":        `{"placeholder":"Type"}`,
		"OutlinedInput":      `{"placeholder":"Type"}`,
		"InputBase":          `{"placeholder":"Type"}`,
		"Modal":              `{"open":true,"children":{"$c":"Box","children":"Modal"}}`,
		"Pagination":         `{"count":5,"page":2}`,
		"PaginationItem":     `{"page":1,"selected":true}`,
		"Portal":             `{"children":{"$c":"Box","children":"Portal"}}`,
		"Snackbar":           `{"open":true,"message":"Saved"}`,
		"SnackbarContent":    `{"message":"Saved"}`,
		"SpeedDialIcon":      `{}`,
		"StepConnector":      `{}`,
		"Stepper":            `{"activeStep":1,"children":[{"$c":"Step","children":{"$c":"StepLabel","children":"One"}},{"$c":"Step","children":{"$c":"StepLabel","children":"Two"}}]}`,
		"Tabs":               `{"value":0,"children":[{"$c":"Tab","label":"One"},{"$c":"Tab","label":"Two"}]}`,
		"TextareaAutosize":   `{"minRows":2,"placeholder":"Type"}`,
		"ToggleButtonGroup":  `{"value":"a","exclusive":true,"children":[{"$c":"ToggleButton","value":"a","children":"A"},{"$c":"ToggleButton","value":"b","children":"B"}]}`,
		"Unstable_TrapFocus": `{"open":true,"children":{"$c":"Box","children":"Trap"}}`,
		"ImageListItem":      `{"children":{"$c":"Box","children":"Item"}}`,
	},
	"@chakra-ui/react": {
		"AspectRatio":       `{"ratio":2,"children":{"$c":"Box","children":"Ratio 2"}}`,
		"ButtonGroup":       `{"children":[{"$c":"Button","children":"One"},{"$c":"Button","children":"Two"}]}`,
		"Group":             `{"children":[{"$c":"Button","children":"One"},{"$c":"Button","children":"Two"}]}`,
		"Highlight":         `{"query":"Chakra","children":"Chakra UI"}`,
		"Input":             `{"placeholder":"Type"}`,
		"Textarea":          `{"placeholder":"Type"}`,
		"InputGroup":        `{"startElement":"$","children":{"$c":"Input","placeholder":"Amount"}}`,
		"ColorSwatch":       `{"value":"#3182ce"}`,
		"Presence":          `{"present":true,"children":{"$c":"Box","children":"Present"}}`,
		"Portal":            `{"children":{"$c":"Box","children":"Portal"}}`,
		"MenuRoot":          `{"open":true,"children":[{"$c":"MenuTrigger","asChild":true,"children":{"$c":"Button","children":"Menu"}},{"$c":"MenuPositioner","children":{"$c":"MenuContent","children":[{"$c":"MenuItem","value":"a","children":"One"},{"$c":"MenuItem","value":"b","children":"Two"}]}}]}`,
		"FloatingPanelRoot": `{"open":true,"children":{"$c":"FloatingPanelPositioner","children":{"$c":"FloatingPanelContent","children":"Panel"}}}`,
		"Icon":              `{"children":{"$c":"Box","children":"i"}}`,
	},
	"@heroui/react": {
		"AlertDialog": `{"isOpen":true,"children":{"$c":"AlertDialogBackdrop","children":{"$c":"AlertDialogContainer","children":{"$c":"AlertDialogDialog","children":[{"$c":"AlertDialogHeader","children":{"$c":"AlertDialogHeading","children":"Delete"}},{"$c":"AlertDialogBody","children":"Are you sure?"}]}}}}`,
		"Modal":       `{"isOpen":true,"children":{"$c":"ModalBackdrop","children":{"$c":"ModalContainer","children":{"$c":"ModalDialog","children":[{"$c":"ModalHeader","children":{"$c":"ModalHeading","children":"Title"}},{"$c":"ModalBody","children":"Body"}]}}}}`,
		"Drawer":      `{"isOpen":true,"children":{"$c":"DrawerBackdrop","children":{"$c":"DrawerContent","children":{"$c":"DrawerDialog","children":[{"$c":"DrawerHeader","children":{"$c":"DrawerHeading","children":"Title"}},{"$c":"DrawerBody","children":"Body"}]}}}}`,
		"Sheet":       `{"isOpen":true,"children":{"$c":"SheetBackdrop","children":{"$c":"SheetContent","children":{"$c":"SheetDialog","children":[{"$c":"SheetHeader","children":{"$c":"SheetHeading","children":"Title"}},{"$c":"SheetBody","children":"Body"}]}}}}`,
		"Popover":     `{"isOpen":true,"children":[{"$c":"PopoverTrigger","children":{"$c":"Button","children":"Open"}},{"$c":"PopoverContent","children":{"$c":"PopoverDialog","children":"Content"}}]}`,
		"Tooltip":     `{"isOpen":true,"children":[{"$c":"TooltipTrigger","children":{"$c":"Button","children":"Hover"}},{"$c":"TooltipContent","children":"Tip"}]}`,
		"Dropdown":    `{"isOpen":true,"children":[{"$c":"DropdownTrigger","children":{"$c":"Button","children":"Menu"}},{"$c":"DropdownPopover","children":{"$c":"DropdownMenu","children":[{"$c":"DropdownItem","id":"a","children":"One"},{"$c":"DropdownItem","id":"b","children":"Two"}]}}]}`,
		"ListBox":     `{"aria-label":"List","children":[{"$c":"ListBoxItem","id":"a","children":"One"},{"$c":"ListBoxItem","id":"b","children":"Two"}]}`,
		"Menu":        `{"aria-label":"Menu","children":[{"$c":"MenuItem","id":"a","children":"One"},{"$c":"MenuItem","id":"b","children":"Two"}]}`,
		"Breadcrumbs": `{"children":[{"$c":"BreadcrumbsItem","href":"#","children":"Home"},{"$c":"BreadcrumbsItem","children":"Page"}]}`,
		"AvatarGroup": `{"children":[{"$c":"Avatar","children":{"$c":"AvatarFallback","children":"A"}},{"$c":"Avatar","children":{"$c":"AvatarFallback","children":"B"}}]}`,
		"Input":       `{"placeholder":"Type"}`,
		"TextArea":    `{"placeholder":"Type"}`,
		"Separator":   `{}`,
		"Spinner":     `{}`,
		"EmptyState":  `{"children":"Nothing here"}`,
		"Fieldset":    `{"children":[{"$c":"FieldsetLegend","children":"Legend"}]}`,
	},
}

// recipeSample is the verified sample for component id of package pkg, when everything it names is in the kit.
func recipeSample(pkg, id string, has func(string) bool, props []kitProp) map[string]any {
	raw, ok := sampleRecipes[pkg][id]
	if !ok {
		return nil
	}
	var s map[string]any
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	declared := map[string]bool{}
	for _, p := range props {
		declared[p.Name] = true
	}
	for k := range s {
		if !declared[k] && k != "children" && !strings.HasPrefix(k, "aria-") && !strings.HasPrefix(k, "data-") {
			return nil
		}
	}
	if !namesExist(s, has) {
		return nil
	}
	return s
}

func namesExist(v any, has func(string) bool) bool {
	switch x := v.(type) {
	case map[string]any:
		if n, ok := x["$c"].(string); ok && !has(n) {
			return false
		}
		for _, e := range x {
			if !namesExist(e, has) {
				return false
			}
		}
	case []any:
		for _, e := range x {
			if !namesExist(e, has) {
				return false
			}
		}
	}
	return true
}

// declareRecipeProps adds to props what a recipe sets and the package's types leave out (children, aria-label): the live
// preview passes only declared props.
func declareRecipeProps(pkg, id string, props []kitProp) []kitProp {
	raw, ok := sampleRecipes[pkg][id]
	if !ok {
		return props
	}
	var s map[string]any
	if json.Unmarshal([]byte(raw), &s) != nil {
		return props
	}
	have := map[string]bool{}
	for _, p := range props {
		have[p.Name] = true
	}
	for _, k := range []string{"children", "aria-label"} {
		if _, in := s[k]; in && !have[k] {
			t := "string"
			if k == "children" {
				t = "node"
			}
			props = append(props, kitProp{Name: k, Type: t})
		}
	}
	return props
}

// itemParents are hints for collection items whose parent the general rule (collectionParent) cannot find.
var itemParents = map[string]map[string]string{
	"@heroui/react": {"Radio": "RadioGroup", "Tab": "Tabs", "Tag": "TagGroup"},
}

// headless are hints: exports that are utilities and not drawn things (collection helpers, press and focus wrappers).
var headless = map[string]map[string]bool{
	"@heroui/react": {"Collection": true, "Focusable": true, "Pressable": true, "Virtualizer": true},
}

// adoptItems makes a collection item a part of its collection (a ListBoxItem draws in a ListBox, a Tab in Tabs): it
// does not draw alone, and the parent's sample shows it. A headless utility of the package is left out of the kit.
func adoptItems(pkg string, comps []genComp) []genComp {
	have := map[string]bool{}
	partOf := map[string]string{}
	for _, c := range comps {
		have[c.ID] = true
		partOf[c.ID] = c.Part
	}
	for _, c := range comps { // the hint says the group is the top: what its own name made a part of the item is not
		if parent := itemParents[pkg][c.ID]; parent != "" && partOf[parent] == c.ID {
			for j := range comps {
				if comps[j].ID == parent {
					comps[j].Part = ""
				}
			}
			partOf[parent] = ""
		}
	}
	var out []genComp
	moved := map[string]string{}
	for _, c := range comps {
		if headless[pkg][c.ID] || nonVisualRe.MatchString(c.ID) {
			continue
		}
		if c.Part == "" {
			parent := itemParents[pkg][c.ID]
			if parent == "" {
				parent = collectionParent(c.ID, have)
				if partOf[parent] == c.ID {
					parent = "" // the group already hangs under it (Avatar and AvatarGroup): no cycle
				}
			}
			if parent != "" && have[parent] {
				c.Part = parent
				moved[c.ID] = parent
			}
		}
		out = append(out, c)
	}
	for n := 0; n < 3; n++ { // the parts of an adopted item belong to the top component
		for i, c := range out {
			for _, p := range out {
				if p.ID == c.Part && p.Part != "" && p.Part != designkit.PartProvider && c.Part != designkit.PartProvider {
					out[i].Part = p.Part
					break
				}
			}
		}
	}
	return out
}

// nonVisualRe are exports that are base classes or helpers a page never draws (an AbstractComponent, a ColorSchemeScript,
// an InlineStyles, a ResizeSensor): they stay out of the kit.
var nonVisualRe = regexp.MustCompile(`^Abstract|Script$|Styles$|Sensor$|Observer$`)

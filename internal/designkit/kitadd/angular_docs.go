package kitadd

// angularAlts says, for the components people confuse, what to take instead: it replaces the generated «When not to
// use» (a neighbour picked by name, which for Angular libraries is often a part of the same component).
var angularAlts = map[string][]string{
	"Button":       {"A link to another page: take a plain link. A choice among a few options: take ButtonToggle or SelectButton."},
	"Chip":         {"A status or count in a table or a list: take Badge or Tag. An action: take Button."},
	"Badge":        {"A word-sized status with its own colour: take Chip or Tag."},
	"Tag":          {"A removable or selectable label: take Chip."},
	"Card":         {"A plain grouping with no frame: take Stack. A list of rows: take List or DataTable."},
	"Checkbox":     {"One choice among several: take RadioButton or Select. An on/off setting that applies at once: take SlideToggle or ToggleSwitch."},
	"SlideToggle":  {"A choice that needs a submit: take Checkbox."},
	"Select":       {"Few options that should all stay visible: take a radio group. Typing to filter a long list: take Autocomplete."},
	"Autocomplete": {"A short fixed list: take Select."},
	"Input":        {"A choice from a list: take Select. A date: take Datepicker."},
	"Datepicker":   {"A free-text date the person types: take Input."},
	"Table":        {"A few labelled values: take List. Data the person edits cell by cell: take a form of TextField."},
	"List":         {"Rows with several columns: take Table or DataTable."},
	"Tabs":         {"Steps in a fixed order: take Stepper. Moving between pages: take Menu or Sidenav."},
	"TabGroup":     {"Steps in a fixed order: take Stepper."},
	"Stepper":      {"Unordered sections: take Tabs."},
	"Menu":         {"A choice that stays on the screen: take Select or Tabs."},
	"Dialog":       {"A short note that needs no answer: take Snackbar or Toast."},
	"Snackbar":     {"A message that needs an answer: take Dialog."},
	"Toolbar":      {"The main navigation of the whole app: take Sidenav."},
	"Sidenav":      {"A top bar with a title and actions: take Toolbar."},
	"Tooltip":      {"Text the person must read: put it on the screen, not in a tooltip."},
	"Progress":     {"A task whose end is unknown and short: take ProgressSpinner."},
	"ProgressBar":  {"A task of unknown length: take ProgressSpinner."},
	"Divider":      {"Spacing alone: take Stack with a gap."},
	"Icon":         {"An icon of the kit's font: take Glyph."},
	"Accordion":    {"Switching between peers on one screen: take Tabs."},
	"Panel":        {"A collapsible group of sections: take Accordion."},
	"Dropdown":     {"Typing to search a long list: take Autocomplete."},
	"InputText":    {"A choice from a list: take Select."},
	"Slider":       {"An exact number: take Input."},
	"Paginator":    {"Endless scroll of a feed: show the list and its end."},
}

// improveAngularDocs puts the curated alternatives in place of the generated ones.
func improveAngularDocs(docs map[string]compDoc, comps []genComp) {
	for _, c := range comps {
		d, ok := docs[c.ID]
		if !ok || d.WhenSrc != srcGenerated || c.Part != "" {
			continue
		}
		if alt, ok := angularAlts[c.ID]; ok {
			d.WhenNot = alt
			docs[c.ID] = d
		}
	}
}

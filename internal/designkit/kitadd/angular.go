package kitadd

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// Angular kits. A ready-made Angular library (@angular/material, primeng, @taiga-ui/*) becomes a kit the same way a
// React one does: the board is React, so the package's components are mounted by one Angular application started in
// the bundle (zoneless, createComponent on a host element) and each gets a React component of the same name in
// window.<Namespace>. Inputs are props, outputs are onXxx props, React children are projected through <ng-content>.

//go:embed angular_analyze.mjs
var angularAnalyzeScript []byte

//go:embed angular_theme_primeng.mjs
var primengThemeScript []byte

//go:embed angular_theme_taiga.mjs
var taigaThemeScript []byte

//go:embed angular_runtime.js
var angularRuntime string

// ngComponent is what the Angular types say about one component or host-like directive.
type ngComponent struct {
	Name        string     `json:"name"`
	Kind        string     `json:"kind"` // component | directive
	Entry       string     `json:"entry"`
	Selector    string     `json:"selector"`
	Standalone  bool       `json:"standalone"`
	CVA         bool       `json:"cva"` // a ControlValueAccessor: ngModel and formControl work on it
	Description string     `json:"description"`
	Inputs      []ngInput  `json:"inputs"`
	Outputs     []ngOutput `json:"outputs"`
	Slots       []string   `json:"slots"`
	_           struct{}   `json:"-"`
}

type ngInput struct {
	Name        string   `json:"name"`
	Prop        string   `json:"prop"`
	Type        string   `json:"type"`
	Values      []string `json:"values"`
	Required    bool     `json:"required"`
	Description string   `json:"description"`
}

type ngOutput struct {
	Name        string `json:"name"`
	Prop        string `json:"prop"`
	Description string `json:"description"`
}

type ngAnalysis struct {
	Entries    []string      `json:"entries"`
	Components []ngComponent `json:"components"`
}

// ngPreset is what a library needs besides its components: a theme stylesheet, providers, the class of the root.
type ngPreset struct {
	CSS          []string // stylesheets of the package (or of its dependencies) that carry the theme tokens
	Install      []string // packages the theme needs
	Imports      string   // JS imports for Providers
	Providers    string   // JS statements that push to `providers`
	RootClass    string
	RootFont     string            // font stack of the Root: the library asks for a web font the board may not have, so the stack ends in a system one
	PartOf       map[string]string // parts the name does not tell: component → the one it is drawn inside
	Prefix       string            // the library's class prefix (Mat, P, Tui): taken off the ids whatever the namespace is
	SkipSuffix   []string          // ids ending so are not carried
	InTextfield  []string          // ids drawn inside a <tui-textfield> (Taiga's kit needs the field of the core package)
	ExtraMods    []string          // packages whose classes the templates may use besides the package's own
	Skip         []string          // ids that only a service creates (dialog and snack bar containers): not carried
	Overlays     map[string]ngOverlay
	AttrDefaults map[string][2]string // id → host attribute with a value the selector does not carry (matButton="filled"): the look a design wants by default
	Samples      map[string]string    // id → sample (JSON) where the generated one does not draw: parts in their parent, an overlay open
	HK           map[string]string    // the library's CSS variables for the kit's own components: text, muted, border, card, page, primary, danger, radius
	Dyn          map[string]string    // id → JS function(p, st) that builds the Angular template of a data component (DataTable, TextField, SelectField)
	IconClass    string               // "pi": icons are the library's own font (primeicons)
	ThemeJS      []byte               // a node script (arg: build folder) that prints the stylesheet the library would build in a browser
}

// ngOverlay is how a component is drawn when it needs more than itself: its own trigger or container around it, an
// open state, or a whole template. The component is then mounted through an Angular template instead of alone.
type ngOverlay struct {
	Before string   `json:"before,omitempty"` // template before the component's element (a trigger, a container)
	After  string   `json:"after,omitempty"`
	Attrs  string   `json:"attrs,omitempty"` // attributes on its element (a template reference the trigger uses)
	Tpl    string   `json:"tpl,omitempty"`   // a whole template instead (data components); `d` is Data, `ca` a children accessor
	Query  *ngQuery `json:"query,omitempty"` // what the open method is called on
	Open   string   `json:"open,omitempty"`  // the method that opens it, when the prop `open` is true
	Data   any      `json:"data,omitempty"`
	Dyn    bool     `json:"dyn,omitempty"` // the template is built from the props by the runtime (DYN[id])
}

type ngQuery struct {
	Name string `json:"name"`
	Cls  string `json:"cls,omitempty"`
}

var ngPresets = map[string]ngPreset{
	"primeng": {
		Prefix:    "P",
		Install:   []string{"@primeuix/themes", "primeicons"},
		CSS:       []string{"primeicons/primeicons.css"},
		IconClass: "pi",
		ExtraMods: []string{"primeng/table", "primeng/inputtext", "primeng/iconfield", "primeng/inputicon", "primeng/select", "primeng/tag", "@angular/forms"},
		HK:        map[string]string{"text": "--p-text-color", "muted": "--p-text-muted-color", "border": "--p-content-border-color", "card": "--p-content-background", "page": "--p-surface-50", "primary": "--p-primary-color", "danger": "--p-red-500", "radius": "--p-content-border-radius"},
		Dyn: map[string]string{
			"DataTable": `function (p, st) {
  var cols = colsOf(p);
  var h = '<p-table [value]="v[' + valIdx(st, arr(p.rows)) + ']"><ng-template #header><tr>';
  cols.forEach(function (c) { h += '<th' + alignStyle(c) + '>' + esc(c.header) + '</th>'; });
  h += '</tr></ng-template><ng-template #body let-r><tr>';
  cols.forEach(function (c) {
    var cell = '{{r["' + c.key + '"]}}';
    if (c.tag) cell = '<p-tag [value]="r[\'' + c.key + '\']" [severity]="v[' + valIdx(st, c.sev) + '][r[\'' + c.key + '\']] || \'secondary\'"/>';
    h += '<td' + alignStyle(c) + '>' + cell + '</td>';
  });
  return h + '</tr></ng-template></p-table>';
}`,
			"TextField": `function (p, st) {
  var ph = valIdx(st, p.placeholder || ''), val = valIdx(st, p.value == null ? '' : p.value), ty = valIdx(st, p.type || 'text'), f = fnIdx(st, p.onChange);
  var inp = '<input pInputText [type]="v[' + ty + ']" [placeholder]="v[' + ph + ']" [value]="v[' + val + ']" [style.width]="\'100%\'" (input)="f[' + f + ']($event.target.value)"/>';
  if (p.icon) inp = '<p-iconfield [style.width]="\'100%\'"><p-inputicon class="pi pi-' + String(p.icon).replace(/[^a-z0-9-]/g, '') + '"/>' + inp + '</p-iconfield>';
  return '<div' + fieldStyle(p) + '>' + (p.label ? '<label style="font-size:0.875rem">' + esc(p.label) + '</label>' : '') + inp + '</div>';
}`,
			"SelectField": `function (p, st) {
  var o = valIdx(st, optsOf(p)), val = valIdx(st, p.value == null ? null : p.value), f = fnIdx(st, p.onChange);
  return '<div' + fieldStyle(p) + '>' + (p.label ? '<label style="font-size:0.875rem">' + esc(p.label) + '</label>' : '') +
    '<p-select [options]="v[' + o + ']" optionLabel="label" optionValue="value" [ngModel]="v[' + val + ']" placeholder="' + attrEsc(p.placeholder || '') + '" (onChange)="f[' + f + ']($event.value)" [style.width]="\'100%\'"/></div>';
}`,
		},
		Imports:    "import { providePrimeNG } from 'primeng/config';\nimport Aura from '@primeuix/themes/aura';\n",
		Providers:  "providers.push(providePrimeNG({ theme: { preset: Aura.default || Aura } }));",
		ThemeJS:    primengThemeScript,
		SkipSuffix: []string{"Icon"}, // svg glyphs the other components draw themselves: nothing a board places alone
		Skip: []string{"Table", "TreeTable", "TTBody", "TTCheckbox", "TTHeaderCheckbox", "TTScrollableView", "CellEditor", "ColumnFilter",
			"TableBody", "TableCheckbox", "TableHeaderCheckbox", "TableRadioButton", "TreeTableCellEditor", "TreeTableToggler", "ColumnFilterFormElement",
			"Image", "Steps", "ConfirmDialog", "ConfirmPopup", "Toast", "ContextMenu", "Popover", "ScrollTop", "Scroller", "Motion", "OrganizationChart", "Terminal", "DynamicDialog", "Overlay", "BlockUI", "Editor", "RadioButton"},
		PartOf: map[string]string{"Tab": "Tabs", "TabList": "Tabs", "TabPanel": "Tabs", "TabPanels": "Tabs"},
		Samples: map[string]string{
			"Accordion":    `{"value":"0","children":[{"$c":"AccordionPanel","props":{"value":"0"},"children":[{"$c":"AccordionHeader","children":"Personal data"},{"$c":"AccordionContent","children":"Details of the panel"}]},{"$c":"AccordionPanel","props":{"value":"1"},"children":[{"$c":"AccordionHeader","children":"Billing"},{"$c":"AccordionContent","children":"Billing details"}]}]}`,
			"Dialog":       `{"visible":true,"header":"Dialog","modal":false,"children":"Dialog content"}`,
			"Drawer":       `{"visible":true,"modal":false,"children":"Drawer content"}`,
			"PanelMenu":    `{"model":[{"label":"Orders","items":[{"label":"New"},{"label":"Archive"}]},{"label":"Customers"}]}`,
			"SelectButton": `{"options":["Day","Week","Month"]}`,
			"Timeline":     `{"value":[{"status":"Ordered"},{"status":"Shipped"},{"status":"Delivered"}]}`,
		},
	},
	"@taiga-ui/core": {
		Prefix:    "Tui",
		Imports:   "import { provideTaiga } from '@taiga-ui/core';\n",
		Providers: "providers.push(provideTaiga());",
		Install:   []string{"less", "@taiga-ui/styles", "@taiga-ui/design-tokens", "@taiga-ui/icons"},
		ThemeJS:   taigaThemeScript,
		Skip: []string{"Icon", "TuiRoot", "Dialog", "DialogComponent", "ModalComponent", "Popup", "Popups", "NotificationComponent", "NotificationTemplate",
			"DropdownComponent", "DropdownContent", "HintComponent", "HintUnstyled", "HintUnstyledComponent", "ErrorField", "ErrorComponent", "ErrorDirective",
			"CarouselComponent", "CarouselDirective", "Expand", "SliderKeySteps", "SliderKeyStepsBase", "ScrollControls"},
		PartOf: map[string]string{"TextfieldContent": "TextfieldComponent", "TextfieldItemComponent": "TextfieldComponent", "InputDirective": "TextfieldComponent", "RadioDirective": "RadioComponent"},
		Samples: map[string]string{
			"TextfieldComponent": `{"children":[{"$c":"Label","children":"Name"},{"$c":"InputDirective"}]}`},
	},
	"@taiga-ui/kit": {
		Prefix:      "Tui",
		Install:     []string{"less", "@taiga-ui/styles", "@taiga-ui/design-tokens", "@taiga-ui/icons"},
		ThemeJS:     taigaThemeScript,
		Skip:        []string{"Pulse", "LineClamp", "LineClampBox", "Confirm", "CopyDirective", "Fullscreen", "NotificationMiddle", "PreviewDialog", "PreviewDialogDirective", "NotificationMiddleComponent", "PushAlert", "PushDirective", "ToastComponent", "ToastTemplate"},
		InTextfield: []string{"ButtonSelect", "ComboBoxDirective", "Counter", "Filter", "InputColorComponent", "InputDateComponent", "InputDateDirective", "InputDateRangeDirective", "InputDateTimeComponent", "InputDateTimeDirective", "InputMonthComponent", "InputMonthDirective", "InputMonthRangeDirective", "InputNumberDirective", "InputNumberStep", "InputPhoneDirective", "InputPhoneInternationalComponent", "InputPinComponent", "InputRange", "InputSliderDirective", "InputTimeComponent", "InputTimeDirective", "InputYearDirective", "PincodeComponent", "InputFilesContent", "InputFilesDirective", "InputNumberStepButtons", "LineClampBox", "TreeItemContent", "RadioList", "Range", "Rating", "SelectDirective", "Switch", "TextareaComponent"},
		Samples: map[string]string{
			"Pager":             `{"count":10,"index":2,"max":7}`,
			"ProgressLabel":     `{"children":"60%"}`,
			"Filter":            `{"items":["All","Active","Archived"]}`,
			"TimelineComponent": `{"children":[{"$c":"TimelineItem","children":"Ordered"},{"$c":"TimelineItem","children":"Shipped"},{"$c":"TimelineItem","children":"Delivered"}]}`,
			"TilesComponent":    `{"children":"Tiles"}`,
		},
		PartOf:    map[string]string{"SelectOption": "SelectDirective", "MultiSelectOption": "SelectDirective", "TimelineItem": "TimelineComponent", "Step": "StepperComponent", "TreeItem": "TreeComponent", "TreeNode": "TreeComponent", "Tile": "TilesComponent", "TabsHorizontal": "TabsWithMore", "TabsVertical": "TabsWithMore", "PincodeContent": "PincodeComponent", "TreeItemContent": "TreeComponent", "MultiSelectGroupComponent": "SelectDirective", "ProgressLabel": "ProgressBar", "InputFiles": "InputFilesDirective"},
		ExtraMods: []string{"@taiga-ui/core", "@angular/forms"},
		Imports:   "import { provideTaiga } from '@taiga-ui/core';\n",
		Providers: "providers.push(provideTaiga());",
	},
	"@angular/material": {
		Prefix:       "Mat",
		CSS:          []string{"@angular/material/prebuilt-themes/azure-blue.css"},
		RootClass:    "mat-app-background mat-typography",
		RootFont:     `Roboto, "Helvetica Neue", Arial, system-ui, sans-serif`,
		AttrDefaults: map[string][2]string{"Button": {"matButton", "filled"}},
		HK:           map[string]string{"text": "--mat-sys-on-surface", "muted": "--mat-sys-on-surface-variant", "border": "--mat-sys-outline-variant", "card": "--mat-sys-surface-container-low", "page": "--mat-sys-surface", "primary": "--mat-sys-primary", "danger": "--mat-sys-error", "radius": "--mat-sys-corner-medium"},
		Dyn: map[string]string{
			"DataTable": `function (p, st) {
  var cols = colsOf(p), names = '[' + cols.map(function (c) { return "'c" + c.i + "'"; }).join(',') + ']';
  var h = '<table mat-table [dataSource]="v[' + valIdx(st, arr(p.rows)) + ']" style="width:100%">';
  cols.forEach(function (c) {
    var cell = '{{r["' + c.key + '"]}}';
    if (c.tag) cell = '<mat-chip [style.--mat-chip-elevated-container-color]="v[' + valIdx(st, c.sev) + '][r[\'' + c.key + '\']]?.bg" [style.--mat-chip-label-text-color]="v[' + valIdx(st, c.sev) + '][r[\'' + c.key + '\']]?.fg">{{r["' + c.key + '"]}}</mat-chip>';
    h += '<ng-container matColumnDef="c' + c.i + '"><th mat-header-cell *matHeaderCellDef' + alignStyle(c) + '>' + esc(c.header) + '</th><td mat-cell *matCellDef="let r"' + alignStyle(c) + '>' + cell + '</td></ng-container>';
  });
  return h + '<tr mat-header-row *matHeaderRowDef="' + names + '"></tr><tr mat-row *matRowDef="let r; columns: ' + names + '"></tr></table>';
}`,
			"TextField": `function (p, st) {
  var ph = valIdx(st, p.placeholder || ''), val = valIdx(st, p.value == null ? '' : p.value), ty = valIdx(st, p.type || 'text'), f = fnIdx(st, p.onChange);
  var ico = p.icon ? '<span matIconPrefix class="hk-ico hk-ico-' + String(p.icon).replace(/[^a-z0-9-]/g, '') + '" style="margin:0 8px 0 12px"></span>' : '';
  return '<mat-form-field appearance="outline" subscriptSizing="dynamic"' + wStyle(p) + '>' + (p.label ? '<mat-label>' + esc(p.label) + '</mat-label>' : '') + ico +
    '<input matInput [type]="v[' + ty + ']" [placeholder]="v[' + ph + ']" [value]="v[' + val + ']" (input)="f[' + f + ']($event.target.value)"></mat-form-field>';
}`,
			"SelectField": `function (p, st) {
  var o = valIdx(st, optsOf(p)), val = valIdx(st, p.value == null ? '' : p.value), ph = valIdx(st, p.placeholder || ''), f = fnIdx(st, p.onChange);
  return '<mat-form-field appearance="outline" subscriptSizing="dynamic"' + wStyle(p) + '>' + (p.label ? '<mat-label>' + esc(p.label) + '</mat-label>' : '') +
    '<mat-select [value]="v[' + val + ']" [placeholder]="v[' + ph + ']" (selectionChange)="f[' + f + ']($event.value)">@for (o of v[' + o + ']; track o.value) {<mat-option [value]="o.value">{{o.label}}</mat-option>}</mat-select></mat-form-field>';
}`,
		},
		PartOf: map[string]string{
			"Cell": "Table", "HeaderCell": "Table", "FooterCell": "Table", "Row": "Table", "HeaderRow": "Table", "FooterRow": "Table",
			"NoDataRow": "Table", "TextColumn": "Table", "NestedTreeNode": "Tree", "Tab": "TabGroup", "Step": "Stepper",
			"Label": "FormField", "Hint": "FormField", "Error": "FormField", "Option": "Select", "Optgroup": "Select",
			"MonthView": "Calendar", "YearView": "Calendar", "MultiYearView": "Calendar", "StartDate": "DateRangeInput",
			"EndDate": "DateRangeInput", "GridTile": "GridList", "GridTileText": "GridList", "GridTileHeaderCssMatStyler": "GridList",
			"GridTileFooterCssMatStyler": "GridList", "StepContent": "Stepper", "StepHeader": "Stepper", "TabBody": "TabGroup",
			"TabHeader": "TabGroup", "TabLink": "TabNav", "TabNavPanel": "TabNav", "SortHeader": "Table", "RecycleRows": "Table",
		},
		Imports:   "import { provideNativeDateAdapter } from '@angular/material/core';\n",
		Providers: "providers.push(provideNativeDateAdapter());",
		Skip:      []string{"DialogContainer", "SnackBarContainer", "SimpleSnackBar", "TooltipComponent", "BottomSheetContainer"},
		Overlays: map[string]ngOverlay{
			"Menu": {Before: `<button matButton="outlined" #trg [matMenuTriggerFor]="hk">Menu</button>`, Attrs: `#hk="matMenu"`,
				Query: &ngQuery{Name: "trg", Cls: "MatMenuTrigger"}, Open: "openMenu"},
			"Autocomplete": {Before: `<mat-form-field><mat-label>Fruit</mat-label><input matInput #trg [matAutocomplete]="hk"></mat-form-field>`, Attrs: `#hk="matAutocomplete"`,
				Query: &ngQuery{Name: "trg", Cls: "MatAutocompleteTrigger"}, Open: "openPanel"},
			"Datepicker": {Before: `<mat-form-field><mat-label>Date</mat-label><input matInput [matDatepicker]="hk"><mat-datepicker-toggle matIconSuffix [for]="hk"></mat-datepicker-toggle></mat-form-field>`,
				Attrs: `#hk`, Query: &ngQuery{Name: "hk"}, Open: "open"},
			"DateRangePicker": {Before: `<mat-form-field><mat-label>Dates</mat-label><mat-date-range-input [rangePicker]="hk"><input matStartDate placeholder="Start"><input matEndDate placeholder="End"></mat-date-range-input><mat-datepicker-toggle matIconSuffix [for]="hk"></mat-datepicker-toggle></mat-form-field>`,
				Attrs: `#hk`, Query: &ngQuery{Name: "hk"}, Open: "open"},
			"DateRangeInput": {Tpl: `<mat-form-field><mat-label>Dates</mat-label><mat-date-range-input [rangePicker]="pk"><input matStartDate placeholder="Start"><input matEndDate placeholder="End"></mat-date-range-input><mat-datepicker-toggle matIconSuffix [for]="pk"></mat-datepicker-toggle><mat-date-range-picker #pk></mat-date-range-picker></mat-form-field>`},
			"Timepicker": {Before: `<mat-form-field><mat-label>Time</mat-label><input matInput [matTimepicker]="hk"><mat-timepicker-toggle matIconSuffix [for]="hk"></mat-timepicker-toggle></mat-form-field>`,
				Attrs: `#hk`, Query: &ngQuery{Name: "hk"}, Open: "open"},
			"Drawer":  {Before: `<mat-drawer-container style="min-height:220px">`, After: `<mat-drawer-content>Content</mat-drawer-content></mat-drawer-container>`},
			"Sidenav": {Before: `<mat-sidenav-container style="min-height:220px">`, After: `<mat-sidenav-content>Content</mat-sidenav-content></mat-sidenav-container>`},
			"TabNav":  {Attrs: `[tabPanel]="tp"`, After: `<mat-tab-nav-panel #tp>Content</mat-tab-nav-panel>`},
			"Table": {Tpl: `<table mat-table [dataSource]="d"><ng-container matColumnDef="name"><th mat-header-cell *matHeaderCellDef>Name</th><td mat-cell *matCellDef="let r">{{r.name}}</td></ng-container><ng-container matColumnDef="role"><th mat-header-cell *matHeaderCellDef>Role</th><td mat-cell *matCellDef="let r">{{r.role}}</td></ng-container><tr mat-header-row *matHeaderRowDef="['name','role']"></tr><tr mat-row *matRowDef="let r; columns: ['name','role']"></tr></table>`,
				Data: []map[string]string{{"name": "Ada", "role": "Admin"}, {"name": "Grace", "role": "Editor"}, {"name": "Linus", "role": "Viewer"}}},
			"Tree": {Tpl: `<mat-tree [dataSource]="d" [childrenAccessor]="ca"><mat-tree-node *matTreeNodeDef="let node">{{node.name}}</mat-tree-node></mat-tree>`,
				Data: []map[string]string{{"name": "Fruit"}, {"name": "Vegetables"}, {"name": "Grains"}}},
		},
		Samples: map[string]string{
			"Menu":            `{"open":true,"children":[{"$c":"MenuItem","children":"Edit"},{"$c":"MenuItem","children":"Duplicate"},{"$c":"MenuItem","children":"Delete"}]}`,
			"Autocomplete":    `{"open":true,"children":[{"$c":"Option","props":{"value":"Apple"},"children":"Apple"},{"$c":"Option","props":{"value":"Banana"},"children":"Banana"}]}`,
			"Datepicker":      `{"open":true}`,
			"DateRangePicker": `{"open":true}`,
			"Timepicker":      `{"open":true}`,
			"Drawer":          `{"opened":true,"mode":"side","children":"Drawer"}`,
			"Sidenav":         `{"opened":true,"mode":"side","children":"Sidenav"}`,
			"TabNav":          `{"children":[{"$c":"TabLink","props":{"active":true},"children":"Overview"},{"$c":"TabLink","children":"Settings"}]}`,
			"TabGroup":        `{"children":[{"$c":"Tab","props":{"label":"Overview"},"children":"Overview content"},{"$c":"Tab","props":{"label":"Settings"},"children":"Settings content"}]}`,
			"Stepper":         `{"children":[{"$c":"Step","props":{"label":"Account"},"children":"Create an account"},{"$c":"Step","props":{"label":"Plan"},"children":"Pick a plan"}]}`,
			"List":            `{"children":[{"$c":"ListItem","children":"Inbox"},{"$c":"ListItem","children":"Sent"},{"$c":"ListItem","children":"Drafts"}]}`,
			"ActionList":      `{"children":[{"$c":"ListItem","children":"Inbox"},{"$c":"ListItem","children":"Sent"}]}`,
			"NavList":         `{"children":[{"$c":"ListItem","children":"Inbox"},{"$c":"ListItem","children":"Sent"}]}`,
			"SelectionList":   `{"children":[{"$c":"ListOption","children":"Apples"},{"$c":"ListOption","children":"Bananas"}]}`,
			"GridList":        `{"cols":2,"rowHeight":"80px","children":[{"$c":"GridTile","children":"One"},{"$c":"GridTile","children":"Two"},{"$c":"GridTile","children":"Three"}]}`,
			"Accordion":       `{"children":[{"$c":"ExpansionPanel","props":{"expanded":true},"children":[{"$c":"ExpansionPanelHeader","children":{"$c":"ExpansionPanelTitle","children":"Personal data"}},"Details of the panel"]},{"$c":"ExpansionPanel","children":[{"$c":"ExpansionPanelHeader","children":{"$c":"ExpansionPanelTitle","children":"Billing"}},"Billing details"]}]}`,
			"FormField":       `{"children":[{"$c":"Label","children":"Name"},{"$c":"Input","props":{"placeholder":"Ada Lovelace"}},{"$c":"Hint","children":"Your full name"}]}`,
			"RadioGroup":      `{"children":[{"$c":"RadioButton","props":{"value":"a"},"children":"Option A"},{"$c":"RadioButton","props":{"value":"b"},"children":"Option B"}]}`,
			"Slider":          `{"min":0,"max":100,"children":{"$c":"SliderThumb","props":{"value":40}}}`,
			"Icon":            `{"children":"home"}`,
			"Chip":            `{"children":"Design"}`,
		},
	},
}

// isAngularPackage: the package declares @angular/core as a peer (or a dependency).
func isAngularPackage(dir, pkg string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return false
	}
	var pj struct {
		Peer map[string]string `json:"peerDependencies"`
		Deps map[string]string `json:"dependencies"`
	}
	if json.Unmarshal(raw, &pj) != nil {
		return false
	}
	_, a := pj.Peer["@angular/core"]
	_, b := pj.Deps["@angular/core"]
	return a || b
}

var majorRe = regexp.MustCompile(`(?:^|[\^~<>=|\s])v?(\d+)\.`)

// angularPeers lists what to install next to an Angular package: its peers at the lowest major their range allows.
func angularPeers(dir, pkg string) []string {
	raw, _ := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
	var pj struct {
		Peer map[string]string `json:"peerDependencies"`
	}
	_ = json.Unmarshal(raw, &pj)
	set := map[string]bool{}
	coreMajor := ""
	for name, rng := range pj.Peer {
		if name == "react" || name == "react-dom" || name == "typescript" {
			continue
		}
		best := 0 // the lowest major the range names: a newer one may not exist yet
		for _, m := range majorRe.FindAllStringSubmatch(rng, -1) {
			if n, _ := strconv.Atoi(m[1]); n > 0 && n < 1000 && (best == 0 || n < best) {
				best = n
			}
		}
		if strings.Contains(rng, ">=") && !strings.ContainsAny(rng, "<^~|") {
			best = 0 // no upper bound (>=19): the package is built against the newest, take the latest
		}
		spec := name
		if best > 0 && (strings.HasPrefix(name, "@angular/") || strings.HasPrefix(name, "@taiga-ui/")) {
			spec = fmt.Sprintf("%s@%d", name, best)
		}
		if best == 0 && strings.HasPrefix(name, "@angular/") {
			spec = name + "@latest" // an installed older one is not kept
		}
		if name == "@angular/core" {
			coreMajor = strconv.Itoa(best)
		}
		set[spec] = true
	}
	if coreMajor == "" || coreMajor == "0" {
		coreMajor = "latest"
	}
	for _, n := range []string{"core", "common", "platform-browser", "compiler"} {
		if !hasPrefixSpec(set, "@angular/"+n) {
			set["@angular/"+n+"@"+coreMajor] = true
		}
	}
	if !hasPrefixSpec(set, "rxjs") {
		set["rxjs"] = true
	}
	return sortedSet(set)
}

func hasPrefixSpec(set map[string]bool, name string) bool {
	for s := range set {
		if s == name || strings.HasPrefix(s, name+"@") {
			return true
		}
	}
	return false
}

func analyzeAngular(o *Options, dir, pkg string) (*ngAnalysis, error) {
	script := filepath.Join(dir, ".kit-angular-analyze.mjs")
	if err := os.WriteFile(script, angularAnalyzeScript, 0o644); err != nil {
		return nil, err
	}
	run := o.Node
	if run == nil {
		run = func(dir string, args ...string) ([]byte, error) { return o.runCmd(dir, "node", args...) }
	}
	out, err := run(dir, script, dir, pkg)
	if err != nil {
		return nil, err
	}
	var a ngAnalysis
	if err := json.Unmarshal(out, &a); err != nil {
		return nil, fmt.Errorf("the Angular types' reader answered something unreadable: %v", err)
	}
	return &a, nil
}

var (
	selTagRe  = regexp.MustCompile(`^[a-z][a-z0-9-]*`)
	selAttrRe = regexp.MustCompile(`\[([A-Za-z][\w-]*)(?:=["']?([^"'\]]*)["']?)?\]`)
)

// hostOf is the element to mount into, from the first alternative of a selector: button[matButton] → button with the
// attribute matButton; mat-checkbox → mat-checkbox; [tuiBadge] → div.
func hostOf(selector string) (tag string, attrs [][2]string) {
	alts := strings.Split(selector, ",")
	first := strings.TrimSpace(alts[0])
	for _, a := range alts { // mat-basic-chip is the chip without the standard look: take the next alternative
		if a = strings.TrimSpace(a); !strings.Contains(a, "-basic-") {
			first = a
			break
		}
	}
	if i := strings.Index(first, ":not("); i >= 0 {
		first = first[:i]
	}
	tag = selTagRe.FindString(first)
	if tag == "" {
		tag = "div"
	}
	for _, m := range selAttrRe.FindAllStringSubmatch(first, -1) {
		attrs = append(attrs, [2]string{m[1], m[2]})
	}
	return tag, attrs
}

type ngRuntimeComp struct {
	ID        string      `json:"id"`
	Cls       string      `json:"cls"`
	Mod       int         `json:"mod"`
	Directive bool        `json:"directive,omitempty"`
	Tag       string      `json:"tag"`
	Attrs     [][2]string `json:"attrs,omitempty"`
	Inputs    []ngInput   `json:"inputs"`
	Outputs   []ngOutput  `json:"outputs"`
	Slots     []string    `json:"slots,omitempty"`
	Ov        *ngOverlay  `json:"ov,omitempty"`
	Tpl       bool        `json:"tpl,omitempty"` // declared in an NgModule: always mounted through a template
}

var identRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// ngID is the id of a component in the kit: the class name without the library's prefix (MatButton → Button in Mat).
func ngID(class, ns string, taken map[string]bool) string {
	id := class
	if ns != "" && strings.HasPrefix(class, ns) && len(class) > len(ns) && class[len(ns)] >= 'A' && class[len(ns)] <= 'Z' {
		id = class[len(ns):]
	}
	if !identRe.MatchString(id) || taken[id] || id == "Root" {
		id = class
	}
	return id
}

func buildAngular(o *Options, rep *Report, dir string, version string) (*built, error) {
	src := o.Source
	pkg := src.Package
	preset := ngPresets[pkg]
	if o.From == "" {
		if err := npmInstall(o, dir, append(angularPeers(dir, pkg), preset.Install...)); err != nil {
			return nil, err
		}
	}
	if im, pv := taigaIcons(dir, pkg); im != "" {
		preset.Imports += im
		preset.Providers += pv
	}
	if pkg == "primeng" {
		preset.Providers = primengLicense(o, dir, preset.Providers)
	}
	a, err := analyzeAngular(o, dir, pkg)
	if err != nil {
		return nil, fmt.Errorf("%s: the Angular components could not be read: %v", pkg, firstLine(err.Error()))
	}
	mods := map[string]int{}
	var modNames []string
	var data []ngRuntimeComp
	var comps []genComp
	taken := map[string]bool{}
	sort.Slice(a.Components, func(i, j int) bool { return a.Components[i].Name < a.Components[j].Name })
	skipped := 0
	var skippedSvc []string
	for _, c := range a.Components {
		if !c.Standalone && c.Kind != "component" {
			skipped++
			continue
		}
		if c.Kind == "directive" && !selTagRe.MatchString(strings.TrimSpace(strings.Split(c.Selector, ",")[0])) {
			continue // a behaviour directive (no element of its own): nothing to draw
		}
		if strings.HasPrefix(c.Name, "ɵ") || strings.HasPrefix(c.Name, "_") {
			continue
		}
		spec := pkg
		if c.Entry != "" {
			spec = pkg + "/" + c.Entry
		}
		mi, ok := mods[spec]
		if !ok {
			mi = len(modNames)
			mods[spec] = mi
			modNames = append(modNames, spec)
		}
		id := ngID(c.Name, cmp.Or(preset.Prefix, o.Namespace), taken)
		if slices.Contains(preset.Skip, id) || slices.ContainsFunc(preset.SkipSuffix, func(x string) bool { return strings.HasSuffix(id, x) && id != x }) {
			skippedSvc = append(skippedSvc, id)
			continue
		}
		taken[id] = true
		tag, attrs := hostOf(c.Selector)
		rc := ngRuntimeComp{ID: id, Cls: c.Name, Mod: mi, Directive: c.Kind == "directive", Tag: tag, Attrs: attrs,
			Inputs: c.Inputs, Outputs: c.Outputs, Slots: c.Slots}
		rc.Tpl = !c.Standalone
		if d, ok := preset.AttrDefaults[id]; ok {
			for i := range rc.Attrs {
				if rc.Attrs[i][0] == d[0] {
					rc.Attrs[i][1] = d[1]
				}
			}
		}
		if ov, ok := preset.Overlays[id]; ok {
			ov := ov
			rc.Ov = &ov
		} else if slices.Contains(preset.InTextfield, id) {
			rc.Ov = &ngOverlay{Before: "<tui-textfield><label tuiLabel>Label</label>", After: "</tui-textfield>", Attrs: "ngModel"}
		}
		data = append(data, rc)
		g := genComp{ID: id, Description: c.Description, File: c.Entry, Code: spec}
		if sm, ok := preset.Samples[id]; ok {
			g.Sample = json.RawMessage(sm)
		}
		for _, in := range c.Inputs {
			g.Props = append(g.Props, TypeProp{Name: in.Name, Type: in.Type, Values: in.Values, Required: in.Required, Description: in.Description})
		}
		for _, out := range c.Outputs {
			g.Props = append(g.Props, TypeProp{Name: "on" + capFirst(out.Name), Type: "function", Description: out.Description})
		}
		if c.CVA {
			if !propNamed(g.Props, "value") {
				g.Props = append(g.Props, TypeProp{Name: "value", Type: "object", Description: "the form value (ngModel / formControl): set it and listen to onChange"})
			}
			g.Props = append(g.Props, TypeProp{Name: "onChange", Type: "function", Description: "the form value changed (the formControl's valueChanges)"},
				TypeProp{Name: "onBlur", Type: "function", Description: "the field was touched (the formControl's touched)"})
		}
		if rc.Ov != nil && rc.Ov.Open != "" {
			g.Props = append(g.Props, TypeProp{Name: "open", Type: "boolean", Description: "draws the overlay open, with its trigger"})
		}
		if len(c.Slots) > 0 || c.Kind == "directive" {
			g.Props = append(g.Props, TypeProp{Name: "children", Type: "node", Description: "projected into <ng-content>"})
		}
		comps = append(comps, g)
	}
	if skipped > 0 {
		rep.NotCarried = append(rep.NotCarried, fmt.Sprintf("directives declared in an NgModule (%d): only standalone directives are mounted", skipped))
	}
	if len(skippedSvc) > 0 {
		rep.NotCarried = append(rep.NotCarried, "created by a service, not drawn in a screen: "+strings.Join(skippedSvc, ", "))
	}
	if len(comps) == 0 {
		return nil, fmt.Errorf("%s: no standalone Angular component found", pkg)
	}
	css := o.CSS
	if len(css) == 0 {
		css = preset.CSS
	}
	if len(css) == 0 {
		css = packageCSS(dir, pkg)
	}
	if len(preset.ThemeJS) > 0 && len(o.CSS) == 0 {
		if themed, err := ngThemeCSS(o, dir, preset.ThemeJS); err != nil {
			rep.NotCarried = append(rep.NotCarried, "tokens: the theme could not be built ("+firstLine(err.Error())+")")
		} else {
			f := filepath.Join(dir, ".kit-theme.css")
			if err := os.WriteFile(f, []byte(themed), 0o644); err != nil {
				return nil, err
			}
			css = append(append([]string{}, css...), f)
		}
	}
	ngMarkParts(comps, preset.PartOf)
	for _, cp := range compositeSet(preset) {
		g := genComp{ID: cp.ID, Description: cp.Description, File: "kit", Code: pkg, Props: cp.Props, Sample: json.RawMessage(cp.Sample)}
		if cp.Dyn {
			var ins []ngInput
			for _, pr := range cp.Props {
				if pr.Type != "function" {
					ins = append(ins, ngInput{Name: pr.Name, Prop: pr.Name, Type: pr.Type, Values: pr.Values, Required: pr.Required, Description: pr.Description})
				}
			}
			data = append(data, ngRuntimeComp{ID: cp.ID, Tag: "div", Inputs: ins, Outputs: []ngOutput{}, Ov: &ngOverlay{Dyn: true}})
		}
		comps = append(comps, g)
	}
	for _, m := range preset.ExtraMods {
		if _, ok := mods[m]; !ok {
			mods[m] = len(modNames)
			modNames = append(modNames, m)
		}
	}
	entry := angularEntry(o.Namespace, modNames, data, preset)
	js, styles, err := bundleAngular(o, dir, entry, css)
	if err != nil {
		return nil, err
	}
	if len(css) == 0 && len(styles) == 0 {
		rep.NotCarried = append(rep.NotCarried, "styles: the package publishes no stylesheet; name one with --css <package>/<file>.css")
	}
	b := &built{pkg: pkg, version: version, kitPackage: pkg, js: js, css: styles, tokensCSS: string(styles), comps: comps,
		codeImport: func(string) string { return pkg }, root: "Root"}
	b.comps = append(b.comps, genComp{ID: "Root", Part: designkit.PartProvider, Description: "Wraps the content of a board in the library's page background and type"})
	b.docs = gatherDocs(o, dir, b, rep)
	improveAngularDocs(b.docs, b.comps)
	return b, nil
}

func capFirst(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		if r == '-' || r == '_' || r == '.' || r == ':' {
			up = true
			continue
		}
		if up {
			b.WriteString(strings.ToUpper(string(r)))
			up = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// angularEntry fills the runtime template with the package's modules, the component list and the library's preset.
func angularEntry(ns string, mods []string, data []ngRuntimeComp, p ngPreset) string {
	var imports, list strings.Builder
	for i, m := range mods {
		fmt.Fprintf(&imports, "import * as M%d from %q;\n", i, m)
		if i > 0 {
			list.WriteString(",")
		}
		fmt.Fprintf(&list, "M%d", i)
	}
	raw, _ := json.Marshal(data)
	nsj, _ := json.Marshal(ns)
	rootFont := "{}"
	if p.RootFont != "" {
		fj, _ := json.Marshal(map[string]string{"fontFamily": p.RootFont})
		rootFont = string(fj)
	}
	root := fmt.Sprintf(`ns.Root = function Root(props) { return React.createElement('div', { className: %q + ' ' + (props.className || ''), style: Object.assign({ minHeight: '100%%', colorScheme: 'light dark' }, %s, props.style) }, props.children); };`, p.RootClass, rootFont)
	s := angularRuntime
	s = strings.Replace(s, "/*IMPORTS*/", imports.String()+p.Imports, 1)
	s = strings.Replace(s, "/*MODS*/", list.String(), 1)
	s = strings.Replace(s, "/*DATA*/", string(raw), 1)
	s = strings.Replace(s, "/*NS*/", string(nsj), 1)
	s = strings.Replace(s, "/*PROVIDERS*/", p.Providers, 1)
	s = strings.Replace(s, "/*COMPOSE*/", composeJS(p), 1)
	s = strings.Replace(s, "/*ROOT*/", root, 1)
	return s
}

// bundleAngular builds the entry and the package's stylesheets into one script and one stylesheet.
func bundleAngular(o *Options, dir, entry string, css []string) (js, styles []byte, err error) {
	return bundleAngularRound(o, dir, entry, css, 0)
}

func bundleAngularRound(o *Options, dir, entry string, css []string, round int) (js, styles []byte, err error) {
	var full strings.Builder
	for _, c := range css {
		fmt.Fprintf(&full, "import %q;\n", c)
	}
	full.WriteString(entry)
	dev := os.Getenv("HARNESSLY_NG_DEV") != "" // Angular's own error texts instead of NG0201 codes, for finding what a component lacks
	res := api.Build(api.BuildOptions{
		Stdin:         &api.StdinOptions{Contents: full.String(), ResolveDir: dir, Sourcefile: "kit-entry.js", Loader: api.LoaderJS},
		AbsWorkingDir: dir, Bundle: true, Write: false, Format: api.FormatIIFE, Platform: api.PlatformBrowser,
		Target: api.ES2022, Charset: api.CharsetUTF8, LegalComments: api.LegalCommentsNone,
		MinifySyntax: !dev, MinifyWhitespace: !dev, MinifyIdentifiers: !dev,
		Define:   map[string]string{"process.env.NODE_ENV": `"production"`, "ngDevMode": fmt.Sprint(dev), "ngJitMode": "true", "ngServerMode": "false", "ngI18nClosureMode": "false", "ngHmrMode": "false"},
		LogLevel: api.LogLevelSilent, Outfile: "bundle.js", Conditions: []string{"style"},
		Loader: map[string]api.Loader{".woff": api.LoaderDataURL, ".woff2": api.LoaderDataURL, ".ttf": api.LoaderDataURL,
			".otf": api.LoaderDataURL, ".eot": api.LoaderDataURL, ".png": api.LoaderDataURL, ".jpg": api.LoaderDataURL,
			".jpeg": api.LoaderDataURL, ".gif": api.LoaderDataURL, ".webp": api.LoaderDataURL, ".svg": api.LoaderDataURL},
	})
	if len(res.Errors) > 0 {
		var missing []string
		for _, m := range res.Errors {
			if mm := unresolvedRe.FindStringSubmatch(m.Text); mm != nil && !strings.HasPrefix(mm[1], ".") && !strings.HasPrefix(mm[1], "/") {
				if p := packageOf(mm[1]); !installed(dir, p) {
					missing = append(missing, p)
				}
			}
		}
		if len(missing) > 0 && o.From == "" && round < 3 {
			if err := npmInstall(o, dir, sortedSet(setOf(missing))); err != nil {
				return nil, nil, err
			}
			return bundleAngularRound(o, dir, entry, css, round+1)
		}
		var msgs []string
		for i, m := range res.Errors {
			if i == 8 {
				break
			}
			at := ""
			if m.Location != nil {
				at = m.Location.File + ": "
			}
			msgs = append(msgs, at+m.Text)
		}
		return nil, nil, errors.New("the Angular bundle does not build:\n  " + strings.Join(msgs, "\n  "))
	}
	for _, f := range res.OutputFiles {
		switch filepath.Ext(f.Path) {
		case ".js":
			js = f.Contents
		case ".css":
			styles = f.Contents
		}
	}
	js = regexp.MustCompile(`(?i)</script`).ReplaceAll(js, []byte(`<\/script`))
	return js, styles, nil
}

func installed(dir, pkg string) bool {
	_, err := os.Stat(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
	return err == nil
}

// ngThemeCSS runs a library's theme script with node: the variables a browser would get from its theme provider.
func ngThemeCSS(o *Options, dir string, script []byte) (string, error) {
	f := filepath.Join(dir, ".kit-theme.mjs")
	if err := os.WriteFile(f, script, 0o644); err != nil {
		return "", err
	}
	run := o.Node
	if run == nil {
		run = func(dir string, args ...string) ([]byte, error) { return o.runCmd(dir, "node", args...) }
	}
	out, err := run(dir, f, dir)
	return string(out), err
}

// ngMarkParts makes a component a part of the longest other component whose name it starts with
// (ExpansionPanelHeader of ExpansionPanel), the shortest such name: such parts need their parent's context and never stand alone.
func ngMarkParts(comps []genComp, known map[string]string) {
	for i := range comps {
		if p, ok := known[comps[i].ID]; ok && comps[i].Part == "" {
			comps[i].Part = p
		}
	}
	for i := range comps {
		if comps[i].Part != "" {
			continue
		}
		best := ""
		for j := range comps {
			p := comps[j].ID
			if i == j || comps[j].Part != "" || comps[j].Code != comps[i].Code || len(comps[i].ID) <= len(p) || !strings.HasPrefix(comps[i].ID, p) {
				continue
			}
			if r := comps[i].ID[len(p)]; r < 'A' || r > 'Z' {
				continue
			}
			if best == "" || len(p) < len(best) { // the top one: a part's parent is never a part
				best = p
			}
		}
		comps[i].Part = best
	}
}

func propNamed(ps []TypeProp, n string) bool {
	for _, p := range ps {
		if p.Name == n {
			return true
		}
	}
	return false
}

// primengLicense: PrimeNG 22 and newer check a PrimeUI license and draw «Invalid PrimeUI License» on every page without
// one; 21.1.10 is the last version without the check. A key (--license-secret) goes into the provider, so the badge
// is not drawn for a licensed team; without one the kit says what the boards will show. The key is part of the kit's
// bundle.js: a kit with a key is not for sharing outside the licensed team.
func primengLicense(o *Options, dir, providers string) string {
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "primeng", "fesm2022", "primeng-license.mjs")); err != nil {
		return providers // a version without the check
	}
	if o.License == "" {
		o.logf("warning: this version of PrimeNG needs a commercial PrimeUI license: every board will show «Invalid PrimeUI License». Give the key with --license-secret NAME (a secret of the safe) or take the last version without the check: npm:primeng@21.1.10\n")
		return providers
	}
	key, _ := json.Marshal(o.License)
	o.logf("PrimeUI license key is set: it is written into the kit's bundle.js, keep the kit inside the licensed team\n")
	return strings.Replace(providers, "providePrimeNG({ theme:", "providePrimeNG({ license: "+string(key)+", theme:", 1)
}

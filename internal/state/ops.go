package state

import (
	"math"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The layout changes that need no terminal, as data. Each answers false when its target is gone.

// LayoutPatch is the body of PATCH /api/layout.
type LayoutPatch struct {
	// ActivePaneID, when set, must name a pane.
	ActivePaneID *string
	// RatioUpdate sets the ratio of the split at Path (child indexes from the root).
	RatioUpdate *RatioUpdate
	Equalize    bool
	// DiffSettings are merged over the stored ones.
	DiffSettings *jsvalue.Object
}

type RatioUpdate struct {
	Path  []int
	Ratio any
}

// ParseLayoutPatch reads the body the way the route does: present fields are applied, truthiness decides.
func ParseLayoutPatch(body *jsvalue.Object) LayoutPatch {
	var p LayoutPatch
	if v, ok := body.Get("activePaneId"); ok {
		s, _ := v.(string)
		p.ActivePaneID = &s
	}
	if ru := body.Object("ratioUpdate"); ru != nil {
		r := &RatioUpdate{}
		r.Ratio, _ = ru.Get("ratio")
		for _, e := range ru.Array("path") {
			n, ok := e.(float64)
			if !ok || n != math.Trunc(n) {
				n = -1
			}
			r.Path = append(r.Path, int(n))
		}
		p.RatioUpdate = r
	}
	if v, ok := body.Get("equalize"); ok {
		p.Equalize = jsvalue.Truthy(v)
	}
	p.DiffSettings = body.Object("diffSettings")
	return p
}

func patchLayoutData(layout *jsvalue.Object, p LayoutPatch) bool {
	if p.ActivePaneID != nil {
		if *p.ActivePaneID == "" || findPane(root(layout), *p.ActivePaneID) == nil {
			return false
		}
		layout.Set("activePaneId", *p.ActivePaneID)
	}
	if p.RatioUpdate != nil {
		layout.Set("root", updateRatioAtPath(root(layout), p.RatioUpdate.Path, p.RatioUpdate.Ratio))
	}
	if p.Equalize {
		layout.Set("root", equalizeNode(root(layout)))
	}
	if p.DiffSettings != nil {
		merged := jsvalue.NewObject()
		if cur := layout.Object("diffSettings"); cur != nil {
			merged.Assign(cur)
		}
		merged.Assign(p.DiffSettings)
		layout.Set("diffSettings", merged)
	}
	return true
}

func patchPaneData(layout *jsvalue.Object, paneID string, activeTabID *string) bool {
	pane := findPane(root(layout), paneID)
	if pane == nil {
		return false
	}
	if activeTabID != nil {
		if t, _ := findTab(pane, *activeTabID); t == nil {
			return false
		}
		pane.Set("activeTabId", *activeTabID)
	}
	return true
}

// reorderTabsData puts a pane's tabs in the given order; the ids must cover every tab of the pane exactly once.
func reorderTabsData(layout *jsvalue.Object, paneID string, tabIDs []string) bool {
	pane := findPane(root(layout), paneID)
	if pane == nil {
		return false
	}
	byID := map[string]*jsvalue.Object{}
	for _, t := range tabsOf(pane) {
		byID[t.String("id")] = t
	}
	seen := map[string]bool{}
	var reordered []*jsvalue.Object
	for _, id := range tabIDs {
		t := byID[id]
		if t == nil {
			continue
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		c := t.Copy()
		c.Set("order", len(reordered))
		reordered = append(reordered, c)
	}
	if len(reordered) != len(pane.Array("tabs")) {
		return false
	}
	setTabs(pane, reordered)
	return true
}

func renumber(pane *jsvalue.Object) {
	for i, t := range tabsOf(pane) {
		t.Set("order", i)
	}
}

func firstTabIDOrNull(pane *jsvalue.Object) any {
	if ts := tabsOf(pane); len(ts) > 0 {
		return ts[0].String("id")
	}
	return nil
}

// spliceIndex is where Array.prototype.splice inserts for a start index.
func spliceIndex(start float64, length int) int {
	if math.IsNaN(start) {
		return 0
	}
	start = math.Trunc(start)
	if start < 0 {
		return int(math.Max(float64(length)+start, 0))
	}
	return int(math.Min(start, float64(length)))
}

func moveTabData(layout *jsvalue.Object, tabID, fromPaneID, toPaneID string, toIndex float64) bool {
	from := findPane(root(layout), fromPaneID)
	to := findPane(root(layout), toPaneID)
	if from == nil || to == nil {
		return false
	}
	tab, idx := findTab(from, tabID)
	if tab == nil {
		return false
	}
	fromTabs := tabsOf(from)
	setTabs(from, append(fromTabs[:idx:idx], fromTabs[idx+1:]...))
	if active, _ := from.Get("activeTabId"); active == tabID {
		from.Set("activeTabId", firstTabIDOrNull(from))
	}
	renumber(from)

	toTabs := tabsOf(to)
	at := spliceIndex(toIndex, len(toTabs))
	next := append(append(append([]*jsvalue.Object{}, toTabs[:at]...), tab), toTabs[at:]...)
	setTabs(to, next)
	renumber(to)
	to.Set("activeTabId", tabID)

	if len(tabsOf(from)) == 0 && len(panes(root(layout))) > 1 {
		removePaneWithFocus(layout, fromPaneID)
	}
	layout.Set("activePaneId", toPaneID)
	return true
}

// TabPatch is the body of PATCH …/tabs/<id>: the fields present, by name.
type TabPatch map[string]any

var tabPatchFields = []string{"name", "panelType", "title", "cwd", "lastCommand", "webUrl", "terminalRatio", "terminalCollapsed"}

// ParseTabPatch checks the body as the route does.
func ParseTabPatch(body *jsvalue.Object) (TabPatch, error) {
	p := TabPatch{}
	for _, k := range tabPatchFields {
		v, ok := body.Get(k)
		if !ok {
			continue
		}
		switch k {
		case "name":
			s, isStr := v.(string)
			if !isStr {
				return nil, badRequest("name must be a string")
			}
			v = jsvalue.TrimJS(s)
		case "terminalRatio":
			n, isNum := v.(float64)
			if !isNum || math.IsInf(n, 0) || math.IsNaN(n) {
				return nil, badRequest("terminalRatio must be a number")
			}
			v = math.Max(0, math.Min(100, n))
		case "terminalCollapsed":
			if _, isBool := v.(bool); !isBool {
				return nil, badRequest("terminalCollapsed must be a boolean")
			}
		}
		p[k] = v
	}
	if len(p) == 0 {
		return nil, badRequest("No fields to update")
	}
	return p, nil
}

func patchTabViewData(layout *jsvalue.Object, paneID, tabID string, p TabPatch) bool {
	pane := findPane(root(layout), paneID)
	if pane == nil {
		return false
	}
	tab, _ := findTab(pane, tabID)
	if tab == nil {
		return false
	}
	if v, ok := p["name"]; ok {
		tab.Set("name", v)
	}
	if v, ok := p["panelType"]; ok {
		if _, named := p["name"]; !named {
			from, _ := tab.Get("panelType")
			tab.Set("name", tabNameForPanelTypeChange(tab.String("name"), from, v))
		}
		tab.Set("panelType", v)
	}
	for _, k := range tabPatchFields[2:] {
		if v, ok := p[k]; ok {
			tab.Set(k, v)
		}
	}
	return true
}

// tabStateDiff is what a tab change set and removed; nil when nothing changed.
func tabStateDiff(before, after *jsvalue.Object) (*jsvalue.Object, []string) {
	set := jsvalue.NewObject()
	var unset []string
	keys := before.Keys()
	for _, k := range after.Keys() {
		if !before.Has(k) {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		bv, inAfter := after.Get(k)
		if !inAfter {
			unset = append(unset, k)
			continue
		}
		av, _ := before.Get(k)
		if !before.Has(k) || !jsvalue.Equal(av, bv) {
			set.Set(k, bv)
		}
	}
	if set.Len() == 0 && len(unset) == 0 {
		return nil, nil
	}
	return set, unset
}

// applyTabState assigns and removes fields of the tab running sessionName; false when there is none.
func applyTabState(layout *jsvalue.Object, sessionName string, set *jsvalue.Object, unset []string) bool {
	tab := tabBySession(layout, sessionName)
	if tab == nil {
		return false
	}
	tab.Assign(set)
	for _, k := range unset {
		tab.Delete(k)
	}
	return true
}

func tabBySession(layout *jsvalue.Object, sessionName string) *jsvalue.Object {
	for _, t := range allTabs(root(layout)) {
		if t.String("sessionName") == sessionName {
			return t
		}
	}
	return nil
}

const webBrowser = "web-browser"

// defaultTabName is a new tab's name for its panel type.
func defaultTabName(panelType any) string {
	if panelType == webBrowser {
		return "Web Browser"
	}
	return ""
}

// tabNameForPanelTypeChange: a tab with no name, or with the old type's default name, takes the new type's default.
func tabNameForPanelTypeChange(name string, from, to any) string {
	fromDefault := defaultTabName(from)
	legacyFromDefault := ""
	if from == "agent-sessions" {
		legacyFromDefault = "Session List"
	}
	toDefault := defaultTabName(to)
	trimmed := jsvalue.TrimJS(name)
	isFromDefault := (fromDefault != "" && trimmed == fromDefault) || (legacyFromDefault != "" && trimmed == legacyFromDefault)
	if trimmed == "" && toDefault != "" {
		return toDefault
	}
	if isFromDefault && trimmed != toDefault {
		return toDefault
	}
	return name
}

var (
	shellNames        = map[string]bool{"zsh": true, "bash": true, "fish": true, "sh": true, "-zsh": true, "-bash": true, "-fish": true, "-sh": true}
	codexProcessNames = map[string]bool{"codex": true, "Codex": true, "node": true}
	hostPathRe        = regexp.MustCompile(`@[^:]+:\s*(.*)`)
	spaceRe           = regexp.MustCompile(`\s+`)
)

func titleBasename(p string) string {
	if p == "~" || p == "/" {
		return p
	}
	parts := strings.Split(strings.TrimSuffix(p, "/"), "/")
	if last := parts[len(parts)-1]; last != "" {
		return last
	}
	return p
}

func processTitle(title string, panelType any) string {
	if panelType == "codex-cli" && codexProcessNames[title] {
		return "Codex"
	}
	return title
}

// formatTabTitle turns tmux's "<command>|<folder>" (or a terminal title) into a tab title: a shell shows its folder's
// name, another program its own name.
func formatTabTitle(raw string, panelType any) string {
	trimmed := jsvalue.TrimJS(raw)
	if trimmed == "" {
		return ""
	}
	if i := strings.IndexByte(trimmed, '|'); i > 0 {
		cmd := trimmed[:i]
		if shellNames[cmd] {
			return titleBasename(trimmed[i+1:])
		}
		return processTitle(cmd, panelType)
	}
	if m := hostPathRe.FindStringSubmatch(trimmed); m != nil {
		return titleBasename(jsvalue.TrimJS(m[1]))
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "~") {
		return titleBasename(trimmed)
	}
	cmd := spaceRe.Split(trimmed, 2)[0]
	if shellNames[cmd] {
		return ""
	}
	return processTitle(cmd, panelType)
}

// FormatTabTitle is a tab's display name from its "<command>|<cwd>" title; panelType "" when the tab has none.
func FormatTabTitle(raw, panelType string) string {
	if panelType == "" {
		return formatTabTitle(raw, nil)
	}
	return formatTabTitle(raw, panelType)
}

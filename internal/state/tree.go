package state

import (
	"math"

	"harnessly/internal/jsvalue"
)

// The layout tree: a pane `{type:'pane', id, tabs, activeTabId}` or a split `{type:'split', orientation, ratio,
// children:[a, b]}`. A node that is not a well-formed split is read as a pane, so a damaged file never panics.

func root(layout *jsvalue.Object) *jsvalue.Object { return layout.Object("root") }

func children(n *jsvalue.Object) (a, b *jsvalue.Object, ok bool) {
	if n == nil || n.String("type") == "pane" {
		return nil, nil, false
	}
	c := n.Array("children")
	if len(c) < 2 {
		return nil, nil, false
	}
	a, _ = c[0].(*jsvalue.Object)
	b, _ = c[1].(*jsvalue.Object)
	return a, b, a != nil && b != nil
}

func isPane(n *jsvalue.Object) bool { _, _, split := children(n); return n != nil && !split }

func setChildren(n, a, b *jsvalue.Object) *jsvalue.Object {
	c := n.Copy()
	c.Set("children", []any{a, b})
	return c
}

func panes(n *jsvalue.Object) []*jsvalue.Object {
	if n == nil {
		return nil
	}
	if a, b, ok := children(n); ok {
		return append(panes(a), panes(b)...)
	}
	return []*jsvalue.Object{n}
}

func tabsOf(pane *jsvalue.Object) []*jsvalue.Object {
	var out []*jsvalue.Object
	for _, t := range pane.Array("tabs") {
		if o, ok := t.(*jsvalue.Object); ok {
			out = append(out, o)
		}
	}
	return out
}

func setTabs(pane *jsvalue.Object, tabs []*jsvalue.Object) {
	arr := make([]any, len(tabs))
	for i, t := range tabs {
		arr[i] = t
	}
	pane.Set("tabs", arr)
}

func allTabs(n *jsvalue.Object) []*jsvalue.Object {
	var out []*jsvalue.Object
	for _, p := range panes(n) {
		out = append(out, tabsOf(p)...)
	}
	return out
}

func findPane(n *jsvalue.Object, id string) *jsvalue.Object {
	for _, p := range panes(n) {
		if p.String("id") == id {
			return p
		}
	}
	return nil
}

func findTab(pane *jsvalue.Object, id string) (*jsvalue.Object, int) {
	for i, t := range tabsOf(pane) {
		if t.String("id") == id {
			return t, i
		}
	}
	return nil, -1
}

func firstPaneID(n *jsvalue.Object) string {
	if a, _, ok := children(n); ok {
		return firstPaneID(a)
	}
	return n.String("id")
}

func lastPaneID(n *jsvalue.Object) string {
	if _, b, ok := children(n); ok {
		return lastPaneID(b)
	}
	return n.String("id")
}

func replacePane(n *jsvalue.Object, id string, with *jsvalue.Object) *jsvalue.Object {
	a, b, ok := children(n)
	if !ok {
		if n.String("id") == id {
			return with
		}
		return n
	}
	return setChildren(n, replacePane(a, id, with), replacePane(b, id, with))
}

func isPaneWithID(n *jsvalue.Object, id string) bool { return isPane(n) && n.String("id") == id }

// removePaneNode is the tree without the pane (its sibling takes the parent's place), nil when it is not there.
func removePaneNode(n *jsvalue.Object, id string) *jsvalue.Object {
	a, b, ok := children(n)
	if !ok {
		return nil
	}
	if isPaneWithID(a, id) {
		return b
	}
	if isPaneWithID(b, id) {
		return a
	}
	if r := removePaneNode(a, id); r != nil {
		return setChildren(n, r, b)
	}
	if r := removePaneNode(b, id); r != nil {
		return setChildren(n, a, r)
	}
	return nil
}

// adjacentPaneID is where focus goes when the pane closes: the first pane of its right sibling when it is a left child,
// the last pane of its left sibling when it is a right child.
func adjacentPaneID(n *jsvalue.Object, id string) string {
	a, b, ok := children(n)
	if !ok {
		return ""
	}
	if isPaneWithID(a, id) {
		return firstPaneID(b)
	}
	if isPaneWithID(b, id) {
		return lastPaneID(a)
	}
	if r := adjacentPaneID(a, id); r != "" {
		return r
	}
	return adjacentPaneID(b, id)
}

func removePaneWithFocus(layout *jsvalue.Object, id string) {
	adjacent := adjacentPaneID(root(layout), id)
	if r := removePaneNode(root(layout), id); r != nil {
		layout.Set("root", r)
	}
	if active, _ := layout.Get("activePaneId"); active == id {
		if adjacent != "" {
			layout.Set("activePaneId", adjacent)
		} else if ps := panes(root(layout)); len(ps) > 0 {
			layout.Set("activePaneId", ps[0].String("id"))
		} else {
			layout.Set("activePaneId", nil)
		}
	}
}

func updateRatioAtPath(n *jsvalue.Object, path []int, ratio any) *jsvalue.Object {
	a, b, ok := children(n)
	if !ok {
		return n
	}
	if len(path) == 0 {
		c := n.Copy()
		c.Set("ratio", ratio)
		return c
	}
	switch path[0] {
	case 0:
		return setChildren(n, updateRatioAtPath(a, path[1:], ratio), b)
	case 1:
		return setChildren(n, a, updateRatioAtPath(b, path[1:], ratio))
	}
	return n
}

func countUnits(n *jsvalue.Object, orientation any) int {
	a, b, ok := children(n)
	if !ok {
		return 1
	}
	if o, _ := n.Get("orientation"); o != orientation {
		return 1
	}
	return countUnits(a, orientation) + countUnits(b, orientation)
}

func ratioOf(n *jsvalue.Object) float64 {
	if r, ok := n.Number("ratio"); ok {
		return r
	}
	return math.NaN()
}

// isEqualized: every split's ratio is within one point of its share of panes along its orientation.
func isEqualized(n *jsvalue.Object) bool {
	a, b, ok := children(n)
	if !ok {
		return true
	}
	o, _ := n.Get("orientation")
	left, right := countUnits(a, o), countUnits(b, o)
	expected := float64(left) / float64(left+right) * 100
	if math.Abs(ratioOf(n)-expected) > 1 {
		return false
	}
	return isEqualized(a) && isEqualized(b)
}

func equalizeNode(n *jsvalue.Object) *jsvalue.Object {
	a, b, ok := children(n)
	if !ok {
		return n
	}
	o, _ := n.Get("orientation")
	left, right := countUnits(a, o), countUnits(b, o)
	c := n.Copy()
	c.Set("ratio", mathRound(float64(left)/float64(left+right)*10000)/100)
	c.Set("children", []any{equalizeNode(a), equalizeNode(b)})
	return c
}

// mathRound is JavaScript's Math.round: halves go up.
func mathRound(x float64) float64 {
	r := math.Floor(x)
	if x-r >= 0.5 {
		r++
	}
	return r
}

// AllTabs are a layout's tabs in tree order.
func AllTabs(layout *jsvalue.Object) []*jsvalue.Object {
	if layout == nil {
		return nil
	}
	root := layout.Object("root")
	if root == nil {
		return nil
	}
	return allTabs(root)
}

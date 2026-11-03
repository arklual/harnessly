package env

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// How far back the calls of a tool are read, and the windows the screens show.
const (
	usageDays  = 90 // read from the store
	weekDays   = 7
	sparkDays  = 14
	unusedDays = 30 // no call for this long and the entry is unused
	dayLayout  = "2006-01-02"
)

// usageIndex is the calls of the tools of an organization by catalog item, read once for a request.
type usageIndex struct {
	has    bool // the organization has any tool facts at all: without them every number is unknown, not zero
	byItem map[string][]store.ToolUsageRow
	tools  []store.ToolUsageRow // the skill and MCP calls, matched to an item or not: what the machines use
	days   [sparkDays]string    // the last 14 UTC days, the oldest first
	week   map[string]bool
	month  map[string]bool
	now    time.Time
}

func loadUsage(ctx context.Context, st store.Store, orgID string, now time.Time) (*usageIndex, error) {
	u := &usageIndex{byItem: map[string][]store.ToolUsageRow{}, week: map[string]bool{}, month: map[string]bool{}, now: now}
	today := now.UTC()
	for i := 0; i < sparkDays; i++ {
		u.days[sparkDays-1-i] = today.AddDate(0, 0, -i).Format(dayLayout)
	}
	for i := 0; i < weekDays; i++ {
		u.week[today.AddDate(0, 0, -i).Format(dayLayout)] = true
	}
	for i := 0; i < unusedDays; i++ {
		u.month[today.AddDate(0, 0, -i).Format(dayLayout)] = true
	}
	rows, err := st.ToolUsageOf(ctx, orgID, today.AddDate(0, 0, -(usageDays-1)).Format(dayLayout))
	if err != nil {
		return nil, err
	}
	u.has = len(rows) > 0
	var items []store.CatalogItem
	loaded := false
	for _, r := range rows {
		id := r.ItemID
		if id == "" && (r.ToolKind == store.CatalogSkill || r.ToolKind == store.CatalogMCP) {
			// The item may have joined the catalog after the calls were written: it is found by the tool's name now.
			if !loaded {
				if items, err = st.CatalogItems(ctx, orgID, true); err != nil {
					return nil, err
				}
				loaded = true
			}
			id = store.ItemForTool(items, r.ToolKind, r.ToolName)
		}
		if id != "" {
			u.byItem[id] = append(u.byItem[id], r)
		}
		if r.ToolKind == store.CatalogSkill || r.ToolKind == store.CatalogMCP {
			r.ItemID = id
			u.tools = append(u.tools, r)
		}
	}
	return u, nil
}

// observedLimit is how many of the tools the machines call the screen lists.
const observedLimit = 40

// observed is the skills and MCP servers the machines of a cover called in the last month, the most called first,
// each with the catalog item it is (nil when the catalog has none yet) and whether the item is in an enabled entry.
func (u *usageIndex) observed(c cover, inEnv map[string]bool) []ObservedTool {
	type key struct{ kind, name string }
	type acc struct {
		ObservedTool
		people map[string]bool
		item   string
	}
	by := map[key]*acc{}
	var order []key
	for _, r := range u.tools {
		if !c.has(r) || r.Calls <= 0 || !u.month[r.Day] {
			continue
		}
		k := key{r.ToolKind, r.ToolName}
		a := by[k]
		if a == nil {
			a = &acc{ObservedTool: ObservedTool{Kind: r.ToolKind, Name: r.ToolName}, people: map[string]bool{}}
			by[k], order = a, append(order, k)
		}
		a.Calls += r.Calls
		a.Errors += r.Errors
		if u.week[r.Day] {
			a.CallsWeek += r.Calls
		}
		if r.UserID != "" {
			a.people[r.UserID] = true
		}
		if r.ItemID != "" {
			a.item = r.ItemID
		}
		at := r.LastAt
		if at == 0 {
			if d, err := time.Parse(dayLayout, r.Day); err == nil {
				at = d.UnixMilli()
			}
		}
		a.last = max(a.last, at)
	}
	out := make([]ObservedTool, 0, len(order))
	for _, k := range order {
		a := by[k]
		a.People = len(a.people)
		a.LastUsedAt = msStr(a.last)
		if a.item != "" {
			a.ItemID = strp(a.item)
			a.InEnv = inEnv[a.item]
		}
		out = append(out, a.ObservedTool)
	}
	slices.SortFunc(out, func(a, b ObservedTool) int {
		return cmp.Or(cmp.Compare(b.Calls, a.Calls), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	if len(out) > observedLimit {
		out = out[:observedLimit]
	}
	return out
}

// cover is the part of the organization a layer's calls are counted in: the tenants (nil for any) and a team ("" for
// any).
type cover struct {
	spaces map[string]bool
	team   string
}

func (c cover) has(r store.ToolUsageRow) bool {
	if c.spaces != nil && !c.spaces[r.SpaceID] {
		return false
	}
	return c.team == "" || r.TeamID == c.team
}

// stat is the calls of one item in a cover.
type stat struct {
	week   int64
	month  int64
	spark  []int64
	last   int64 // epoch ms, 0 when never
	people map[string]bool
}

func (u *usageIndex) stat(itemID string, c cover) stat {
	s := stat{spark: make([]int64, sparkDays), people: map[string]bool{}}
	idx := map[string]int{}
	for i, d := range u.days {
		idx[d] = i
	}
	for _, r := range u.byItem[itemID] {
		if !c.has(r) || r.Calls <= 0 {
			continue
		}
		if i, ok := idx[r.Day]; ok {
			s.spark[i] += r.Calls
		}
		if u.week[r.Day] {
			s.week += r.Calls
			if r.UserID != "" {
				s.people[r.UserID] = true
			}
		}
		if u.month[r.Day] {
			s.month += r.Calls
		}
		at := r.LastAt
		if at == 0 {
			if d, err := time.Parse(dayLayout, r.Day); err == nil {
				at = d.UnixMilli()
			}
		}
		s.last = max(s.last, at)
	}
	return s
}

// total is the calls of the week of a set of items in a cover, each call counted once.
func (u *usageIndex) total(itemIDs map[string]bool, c cover) int64 {
	var n int64
	for id := range itemIDs {
		for _, r := range u.byItem[id] {
			if c.has(r) && u.week[r.Day] {
				n += r.Calls
			}
		}
	}
	return n
}

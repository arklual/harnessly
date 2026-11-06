package analytics

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The tools screen (docs/hub/analytics.md, «Инструменты»): which MCP servers and skills the agents of a scope call, how
// often, how many calls failed and who called. It reads tool_usage_day, the per-day facts the machines report.

// maxToolPeople is how many callers a tool lists; the rest are only in the counts.
const maxToolPeople = 8

// ToolCaller is one person's calls of a tool.
type ToolCaller struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Calls  int64  `json:"calls"`
	Errors int64  `json:"errors"`
}

// ToolItem is one tool (an MCP server, a skill or a built-in tool) over the period.
type ToolItem struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	ItemID string `json:"itemId,omitempty"`
	Calls  int64  `json:"calls"`
	Errors int64  `json:"errors"`
	// Users and Teams count who called it.
	Users  int          `json:"users"`
	Teams  int          `json:"teams"`
	LastAt int64        `json:"lastAt"`
	Days   []int64      `json:"days"`
	People []ToolCaller `json:"people"`
}

// ToolsResponse is GET …/tools.
type ToolsResponse struct {
	Scope  orgs.ScopeView `json:"scope"`
	Period Period         `json:"period"`
	// Days are the period's days, the keys of every item's Days.
	Days  []string   `json:"days"`
	Calls int64      `json:"calls"`
	Items []ToolItem `json:"items"`
}

func (s *Service) mountTools(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/tools", s.gate(s.tools))
}

// inToolScope says that a row of tool_usage_day belongs to the scope.
func (x *orgScope) inToolScope(r store.ToolUsageRow) bool {
	switch x.sc.Kind {
	case store.ScopeTenant:
		return r.SpaceID == x.sc.ID
	case store.ScopeTeam:
		return r.TeamID == x.sc.ID
	}
	_, ok := x.tenants[r.SpaceID]
	return ok
}

// tallyTools sums the rows of days [from, to) into one item per tool, most called first. name resolves a caller.
func tallyTools(rows []store.ToolUsageRow, days []string, name func(string) string) []ToolItem {
	at := make(map[string]int, len(days))
	for i, d := range days {
		at[d] = i
	}
	type key struct{ kind, name string }
	type acc struct {
		item   ToolItem
		people map[string]*ToolCaller
		teams  map[string]bool
	}
	got := map[key]*acc{}
	for _, r := range rows {
		i, ok := at[r.Day]
		if !ok {
			continue
		}
		k := key{r.ToolKind, r.ToolName}
		a := got[k]
		if a == nil {
			a = &acc{item: ToolItem{Kind: r.ToolKind, Name: r.ToolName, Days: make([]int64, len(days)), People: []ToolCaller{}},
				people: map[string]*ToolCaller{}, teams: map[string]bool{}}
			got[k] = a
		}
		a.item.Calls += r.Calls
		a.item.Errors += r.Errors
		a.item.Days[i] += r.Calls
		a.item.LastAt = max(a.item.LastAt, r.LastAt)
		if r.ItemID != "" {
			a.item.ItemID = r.ItemID
		}
		if r.TeamID != "" {
			a.teams[r.TeamID] = true
		}
		c := a.people[r.UserID]
		if c == nil {
			c = &ToolCaller{UserID: r.UserID, Name: name(r.UserID)}
			a.people[r.UserID] = c
		}
		c.Calls += r.Calls
		c.Errors += r.Errors
	}
	out := make([]ToolItem, 0, len(got))
	for _, a := range got {
		a.item.Users, a.item.Teams = len(a.people), len(a.teams)
		for _, c := range a.people {
			a.item.People = append(a.item.People, *c)
		}
		slices.SortFunc(a.item.People, func(x, y ToolCaller) int {
			return cmp.Or(cmp.Compare(y.Calls, x.Calls), strings.Compare(x.Name, y.Name))
		})
		if len(a.item.People) > maxToolPeople {
			a.item.People = a.item.People[:maxToolPeople]
		}
		out = append(out, a.item)
	}
	slices.SortFunc(out, func(x, y ToolItem) int {
		return cmp.Or(cmp.Compare(y.Calls, x.Calls), strings.Compare(x.Kind, y.Kind), strings.Compare(x.Name, y.Name))
	})
	return out
}

// tools is GET /api/orgs/{org}/tools?scope=&from=&to= (or period=day|week|month).
func (s *Service) tools(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.View)
	if err != nil {
		return err
	}
	sp, err := parsePeriod(r.URL.Query(), x.loc, x.now)
	if err != nil {
		return err
	}
	if sp.days() > maxDays {
		return httpx.Invalid("from", "a period of at most 400 days")
	}
	var days []string
	for d := sp.from; d.Before(sp.to); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format(day))
	}
	rows, err := st.ToolUsageOf(ctx, x.org.ID, sp.from.Format(day))
	if err != nil {
		return err
	}
	scoped := rows[:0:0]
	for _, row := range rows {
		if x.inToolScope(row) {
			scoped = append(scoped, row)
		}
	}
	names := map[string]string{}
	name := func(id string) string {
		if n, ok := names[id]; ok {
			return n
		}
		n := "Неизвестно"
		if u, err := st.UserByID(ctx, id); err == nil {
			n = cmp.Or(u.Name, u.Email)
		}
		names[id] = n
		return n
	}
	out := ToolsResponse{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID}, Period: sp.period(), Days: days,
		Items: tallyTools(scoped, days, name)}
	for _, it := range out.Items {
		out.Calls += it.Calls
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

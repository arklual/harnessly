package analytics

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func TestTallyTools(t *testing.T) {
	days := []string{"2026-11-03", "2026-11-04"}
	rows := []store.ToolUsageRow{
		{TeamID: "t1", UserID: "u1", ToolKind: "mcp", ToolName: "tracker", Day: "2026-11-03", Calls: 5, Errors: 1, LastAt: 10},
		{TeamID: "t2", UserID: "u2", ToolKind: "mcp", ToolName: "tracker", Day: "2026-11-04", Calls: 7, LastAt: 20},
		{TeamID: "t1", UserID: "u1", ToolKind: "skill", ToolName: "review", Day: "2026-11-04", Calls: 2},
		{TeamID: "t1", UserID: "u1", ToolKind: "skill", ToolName: "old", Day: "2026-10-01", Calls: 99},
	}
	got := tallyTools(rows, days, func(id string) string { return "name-" + id })
	if len(got) != 2 {
		t.Fatalf("items = %d, want 2 (a day outside the period is not counted): %+v", len(got), got)
	}
	a := got[0]
	if a.Name != "tracker" || a.Calls != 12 || a.Errors != 1 || a.Users != 2 || a.Teams != 2 || a.LastAt != 20 {
		t.Errorf("tracker = %+v", a)
	}
	if a.Days[0] != 5 || a.Days[1] != 7 {
		t.Errorf("days = %v", a.Days)
	}
	if a.People[0].Name != "name-u2" || a.People[0].Calls != 7 {
		t.Errorf("people = %+v", a.People)
	}
	if got[1].Name != "review" || got[1].Calls != 2 {
		t.Errorf("second = %+v", got[1])
	}
}

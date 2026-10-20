package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// designKeeperRunning says whether the tab keeper runs; routes that name a chat by its session match only then.
var designKeeperRunning = func() bool { return false }

func init() { routeGroups = append(routeGroups, designHistoryRoutes) }

// designHistoryRoutes: a board's versions for the panel and for agents, and the import of an export.
func designHistoryRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	cli := designRoute("/api/cli/design/history", a.CLIHistory)
	cli.Match = func(r *http.Request) bool {
		return files.RouteIs(r, "/api/cli/design/history") && (designKeeperRunning() || !design.CLIHistoryNeedsKeeper(r))
	}
	return []gate.JSONRoute{
		designRoute(platformAPI+"/design/library/history", a.History),
		designRoute(platformAPI+"/design/library/import", a.Import),
		cli,
	}
}

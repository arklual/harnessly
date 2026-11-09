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

// designHistoryRoutes: a board's versions for the panel and for agents, the import of an export, the versions of kits
// and of projects.
func designHistoryRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	cli := designRoute("/api/cli/design/history", a.CLIHistory)
	cli.Match = func(r *http.Request) bool {
		return files.RouteIs(r, "/api/cli/design/history") && (designKeeperRunning() || !design.CLIHistoryNeedsKeeper(r))
	}
	return []gate.JSONRoute{
		designRoute(platformAPI+"/design/library/history", a.History),
		designRoute(platformAPI+"/design/library/import", a.Import),
		designRoute(platformAPI+"/design/kits/state", a.KitVersionsState),
		designRoute(platformAPI+"/design/kits/draft", a.KitDraftAction),
		designRoute(platformAPI+"/design/kits/publish", a.KitPublish),
		designRoute(platformAPI+"/design/kits/adopt", a.KitAdopt),
		designRoute(platformAPI+"/design/kits/settings", a.KitSettingsRoute),
		designRoute(platformAPI+"/design/project/kit", a.ProjectKitRoute),
		designRoute(platformAPI+"/design/project/versions", a.ProjectVersionsRoute),
		designRoute(platformAPI+"/design/project/versions/rename", a.ProjectVersionRename),
		designRoute(platformAPI+"/design/project/versions/diff", a.ProjectVersionDiff),
		designRoute(platformAPI+"/design/project/versions/restore", a.ProjectVersionRestore),
		cli,
	}
}

package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// The kit screen's routes (slice "design", HRN-897): the kits with a canvas, a theme's canvas, a template as a project,
// where a kit is used, the kit a project is on, and the kit's files.

func init() {
	routeGroups = append(routeGroups, designKitRoutes)
	bodyRouteGroups = append(bodyRouteGroups, designKitBodyRoutes)
}

func designKitRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	return []gate.JSONRoute{
		designRoute(platformAPI+"/design/kit/canvases", a.KitCanvasList),
		designRoute(platformAPI+"/design/kit/canvas", a.KitCanvas),
		designRoute(platformAPI+"/design/kit/project", a.KitProject),
		designRoute(platformAPI+"/design/kit/usage", a.KitUsage),
		designRoute(platformAPI+"/design/kit/link", a.KitLinkRoute),
	}
}

func designKitBodyRoutes(d deps) []gate.BodyRoute {
	a := designAPI(d)
	return []gate.BodyRoute{
		{Slice: "design", Match: func(r *http.Request) bool { return files.RouteIs(r, platformAPI+"/design/kit/file") }, Serve: a.KitFile},
	}
}

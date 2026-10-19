package main

import (
	"net/http"

	"harnessly/internal/files"
	"harnessly/internal/gate"
)

// The design library's checks and pictures in a headless browser (slice "design"): a design's check report and its
// start, a check's screenshots and the gallery pictures of boards. A picture route's shadow never draws.

func init() {
	routeGroups = append(routeGroups, designCheckRoutes)
	bodyRouteGroups = append(bodyRouteGroups, designImageRoutes)
}

func designCheckRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	return []gate.JSONRoute{designRoute(platformAPI+"/design/library/check", a.Check)}
}

func designImageRoutes(d deps) []gate.BodyRoute {
	a := designAPI(d)
	at := func(p string) func(*http.Request) bool {
		return func(r *http.Request) bool { return files.RouteIs(r, p) }
	}
	return []gate.BodyRoute{
		{Slice: "design", Match: at(platformAPI + "/design/library/shot"), Serve: a.Shot},
		{Slice: "design", Match: at(platformAPI + "/design/library/thumb"), Serve: a.Thumb},
	}
}

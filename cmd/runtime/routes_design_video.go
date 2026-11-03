package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// A video's routes (slice "design"): a project of the kind "video" — a deck folder with meta/video.json — with
// its scenes, comments on moments, frames, check and renders. The deck's routes (settings and style, notes, comments,
// share) keep working on it. The JSON routes answer by method inside their handler; the body routes carry bytes (a
// frame, the published MP4).

func init() {
	routeGroups = append(routeGroups, designVideoRoutes)
	bodyRouteGroups = append(bodyRouteGroups, designVideoBodyRoutes)
}

// videoPath is a video route's path under the platform's API.
func videoPath(p string) string { return platformAPI + "/design/video" + p }

// videoCLIPath is a video route's path under the CLI's API.
func videoCLIPath(p string) string { return "/api/cli/design/video" + p }

func designVideoRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	return []gate.JSONRoute{
		designRoute(videoPath(""), a.Video),
		designRoute(videoPath("/settings"), a.VideoSettings),
		designRoute(videoPath("/view"), a.VideoView),
		designRoute(videoPath("/scene"), a.VideoScene),
		designRoute(videoPath("/overlay"), a.VideoOverlay),
		designRoute(videoPath("/comments"), a.VideoComments),
		designRoute(videoPath("/render"), a.VideoRender),
		designRoute(videoCLIPath(""), a.CLIVideo),
		designRoute(videoCLIPath("/scene"), a.CLIVideoScene),
		designRoute(videoCLIPath("/overlay"), a.CLIVideoOverlay),
		designRoute(videoCLIPath("/frame"), a.CLIVideoFrame),
		designRoute(videoCLIPath("/check"), a.CLIVideoCheck),
		designRoute(videoCLIPath("/render"), a.CLIVideoRender),
	}
}

func designVideoBodyRoutes(d deps) []gate.BodyRoute {
	a := designAPI(d)
	at := func(p string) func(*http.Request) bool {
		return func(r *http.Request) bool { return files.RouteIs(r, videoPath(p)) }
	}
	return []gate.BodyRoute{
		{Slice: "design", Match: at("/frame"), Serve: a.VideoFrame},
		{Slice: "design", Match: at("/file"), Serve: a.VideoFile},
	}
}

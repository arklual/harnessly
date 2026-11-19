package main

import (
	"context"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/builders"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/mediagen"
)

// A 3D scene's routes (slice "design"): a project of the kind "scene3d" — a folder with meta/scene3d.json over
// a blender-lab project — with its model, pins, draft frames and the agent's CLI. The deck's comment routes keep working.

func init() {
	routeGroups = append(routeGroups, designScene3DRoutes)
	bodyRouteGroups = append(bodyRouteGroups, designScene3DBodyRoutes)
}

func scene3dPath(p string) string    { return platformAPI + "/design/scene3d" + p }
func scene3dCLIPath(p string) string { return "/api/cli/design/scene3d" + p }

func designScene3DRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	// Renders go to the hub's pool of builders when there is one, else to this machine's own slots.
	design.SetScene3DBuilders(func() *builders.Client { return builderClient(d) })
	// And the clips of a generation to the machine's video generator, when it has a provider.
	design.SetScene3DGenerator(scene3dGen{d})
	return []gate.JSONRoute{
		designRoute(scene3dPath(""), a.Scene3D),
		designRoute(scene3dPath("/build"), a.Scene3DBuild),
		designRoute(scene3dPath("/frame"), a.Scene3DFrame),
		designRoute(scene3dPath("/comments"), a.Scene3DComments),
		designRoute(scene3dPath("/view"), a.Scene3DView),
		designRoute(scene3dPath("/clip"), a.Scene3DClip),
		designRoute(scene3dPath("/final"), a.Scene3DFinal),
		designRoute(scene3dPath("/to-video"), a.Scene3DToVideo),
		designRoute(scene3dPath("/outputs"), a.Scene3DOutputsRoute),
		designRoute(scene3dPath("/generate"), a.Scene3DGenerate),
		designRoute(scene3dCLIPath(""), a.CLIScene3D),
		designRoute(scene3dCLIPath("/add"), a.CLIScene3DAdd),
		designRoute(scene3dCLIPath("/frame"), a.CLIScene3DFrame),
	}
}

func designScene3DBodyRoutes(d deps) []gate.BodyRoute {
	a := designAPI(d)
	at := func(p string) func(*http.Request) bool {
		return func(r *http.Request) bool { return files.RouteIs(r, scene3dPath(p)) }
	}
	return []gate.BodyRoute{
		{Slice: "design", Match: at("/model"), Serve: a.Scene3DModel},
		{Slice: "design", Match: at("/frame-image"), Serve: a.Scene3DFrameImage},
		{Slice: "design", Match: at("/shot"), Serve: a.Scene3DShot},
		{Slice: "design", Match: at("/fix"), Serve: a.Scene3DFixImage},
		{Slice: "design", Match: at("/clip-file"), Serve: a.Scene3DClipFile},
		{Slice: "design", Match: at("/output-file"), Serve: a.Scene3DOutputFileRoute},
	}
}

// scene3dGen gives the 3D scene the machine's media service (internal/mediagen): the key frames go to a video model, the clip comes back.
type scene3dGen struct{ d deps }

func (g scene3dGen) req(r design.Scene3DGenRequest) mediagen.Request {
	return mediagen.Request{Kind: extproviders.KindVideo, Model: r.Model, Prompt: r.Prompt, Frames: r.Frames, Seconds: r.Seconds, MaxUSD: r.MaxUSD,
		Owner: r.Owner, CopyTo: r.CopyTo, SaveOK: r.SaveOK, FrameOK: r.FrameOK, Origin: r.Origin}
}

func (g scene3dGen) Estimate(ctx context.Context, r design.Scene3DGenRequest) (float64, string, error) {
	p, err := mediaJobs(g.d).Estimate(ctx, g.req(r))
	return p.USD, p.Model, err
}

func (g scene3dGen) job(s mediagen.Snapshot) design.Scene3DGenJob {
	j := design.Scene3DGenJob{ID: s.ID, State: string(s.State), EstimateUSD: s.EstimateUSD, Error: s.Error}
	for _, f := range s.Files {
		j.Videos = append(j.Videos, design.Scene3DGenVideo{Path: f.Path, Copy: f.Copy, OriginFile: f.OriginFile})
	}
	return j
}

func (g scene3dGen) Start(r design.Scene3DGenRequest) (design.Scene3DGenJob, error) {
	s, err := mediaJobs(g.d).Start(g.req(r))
	if err != nil {
		return design.Scene3DGenJob{}, err
	}
	return g.job(s), nil
}

func (g scene3dGen) Get(id string) (design.Scene3DGenJob, bool) {
	s, ok := mediaJobs(g.d).Get(id)
	if !ok {
		return design.Scene3DGenJob{}, false
	}
	return g.job(s), true
}

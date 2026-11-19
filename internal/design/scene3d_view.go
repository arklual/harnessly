package design

import (
	"net/http"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// What the person looks at in a 3D scene and what the agent asks the viewer to do. The page reports its place in
// one short poll and takes the agent's commands in the answer, so no other channel is needed and nothing outlives the runtime.

const (
	scene3dViewLive = 8 * time.Second  // a page counts as open this long after its last report
	scene3dCmdTTL   = 45 * time.Second // a command nobody took is dropped
)

type scene3dViewOne struct {
	report   Scene3DViewReport
	reported time.Time
	cmds     []Scene3DViewCmd
	seq      int64
}

var scene3dViews = struct {
	mu sync.Mutex
	m  map[string]*scene3dViewOne
}{m: map[string]*scene3dViewOne{}}

// Scene3DViewSeen is what the agent reads of the viewer.
type Scene3DViewSeen struct {
	Open bool `json:"open"`
	Scene3DViewReport
}

func scene3dViewSeen(project string, now time.Time) Scene3DViewSeen {
	scene3dViews.mu.Lock()
	defer scene3dViews.mu.Unlock()
	v := scene3dViews.m[project]
	if v == nil || now.Sub(v.reported) > scene3dViewLive {
		return Scene3DViewSeen{}
	}
	return Scene3DViewSeen{Open: true, Scene3DViewReport: v.report}
}

// scene3dViewReport takes a page's report and answers the commands it has not acknowledged.
func scene3dViewReport(project string, r Scene3DViewReport, now time.Time) []Scene3DViewCmd {
	scene3dViews.mu.Lock()
	defer scene3dViews.mu.Unlock()
	v := scene3dViews.m[project]
	if v == nil {
		v = &scene3dViewOne{}
		scene3dViews.m[project] = v
	}
	v.report, v.reported = r, now
	keep := v.cmds[:0]
	for _, c := range v.cmds {
		if c.Seq > r.Acked && now.Sub(c.at) < scene3dCmdTTL {
			keep = append(keep, c)
		}
	}
	v.cmds = keep
	return append([]Scene3DViewCmd{}, keep...)
}

// scene3dViewPush queues a command; false when no page has the scene open.
func scene3dViewPush(project string, c Scene3DViewCmd, now time.Time) bool {
	scene3dViews.mu.Lock()
	defer scene3dViews.mu.Unlock()
	v := scene3dViews.m[project]
	if v == nil || now.Sub(v.reported) > scene3dViewLive {
		return false
	}
	v.seq++
	c.Seq, c.at = v.seq, now
	v.cmds = append(v.cmds, c)
	return true
}

// Scene3DView is POST {artifact, object, pin, cam, frame, playing, acked} (view): {cmds: [Scene3DViewCmd]}.
func (a *API) Scene3DView(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleView, http.MethodPost)
	if no != nil {
		return *no
	}
	var in Scene3DViewReport
	if err := scene3dDecode(body, &in); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The body is not valid", "Тело запроса неверно")), noStore())
	}
	cmds := scene3dViewReport(id, in, time.Now())
	return JSON(r, http.StatusOK, struct {
		Cmds []Scene3DViewCmd `json:"cmds"`
	}{cmds}, noStore())
}

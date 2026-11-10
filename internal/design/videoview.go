package design

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// What the person is looking at in a video's player, and what the agent asks the player to do. The page reports
// its playhead and takes the commands in one short poll, so nothing here outlives the runtime and no new channel is
// needed. The person comes first: a command that arrives right after they moved the player is only offered to them.

const (
	// videoViewLive is how long after its last report a page counts as open.
	videoViewLive = 6 * time.Second
	// videoViewHuman: for this long after the person moved the player the agent's commands are offers.
	videoViewHuman = 8 * time.Second
	// videoViewCmdTTL: a command nobody took is dropped after this long.
	videoViewCmdTTL = 45 * time.Second
)

// VideoViewCmd is one thing the agent asks the player to do.
type VideoViewCmd struct {
	Seq    int64       `json:"seq"`
	Kind   string      `json:"kind"` // seek, play, pause or format
	T      *int64      `json:"t,omitempty"`
	T1     *int64      `json:"t1,omitempty"` // play: stop or loop at this time
	Loop   bool        `json:"loop,omitempty"`
	Format VideoFormat `json:"format,omitempty"`
	// Offer: the person just moved the player; the page shows a button instead of acting.
	Offer bool `json:"offer,omitempty"`
	at    time.Time
}

// VideoViewReport is what the page says of itself.
type VideoViewReport struct {
	T       int64       `json:"t"`
	Playing bool        `json:"playing"`
	Format  VideoFormat `json:"format"`
	// TouchedAgo is ms since the person moved the player; negative when they have not.
	TouchedAgo int64 `json:"touchedAgo"`
	Acked      int64 `json:"acked"`
}

type videoViewOne struct {
	report    VideoViewReport
	reported  time.Time
	touchedAt time.Time
	cmds      []VideoViewCmd
	seq       int64
}

var videoViews = struct {
	mu sync.Mutex
	m  map[string]*videoViewOne
}{m: map[string]*videoViewOne{}}

func videoViewOf(id string) *videoViewOne {
	v := videoViews.m[id]
	if v == nil {
		v = &videoViewOne{}
		videoViews.m[id] = v
	}
	return v
}

// videoViewTake keeps the page's report and answers the commands waiting for it.
func videoViewTake(id string, r VideoViewReport, now time.Time) []VideoViewCmd {
	videoViews.mu.Lock()
	defer videoViews.mu.Unlock()
	v := videoViewOf(id)
	v.report, v.reported = r, now
	if r.TouchedAgo >= 0 {
		v.touchedAt = now.Add(-time.Duration(r.TouchedAgo) * time.Millisecond)
	}
	out := []VideoViewCmd{}
	for _, c := range v.cmds {
		if c.Seq > r.Acked && now.Sub(c.at) < videoViewCmdTTL {
			out = append(out, c)
		}
	}
	v.cmds = out
	return append([]VideoViewCmd{}, out...)
}

// VideoViewSeen is what the agent is told of the player.
type VideoViewSeen struct {
	Open    bool        `json:"open"`
	T       int64       `json:"t"`
	Playing bool        `json:"playing"`
	Format  VideoFormat `json:"format,omitempty"`
	// Moving: the person moved the player a moment ago.
	Moving bool `json:"moving"`
}

func videoViewSeen(id string, now time.Time) VideoViewSeen {
	videoViews.mu.Lock()
	defer videoViews.mu.Unlock()
	v := videoViews.m[id]
	if v == nil || now.Sub(v.reported) > videoViewLive {
		return VideoViewSeen{}
	}
	return VideoViewSeen{Open: true, T: v.report.T, Playing: v.report.Playing, Format: v.report.Format,
		Moving: !v.touchedAt.IsZero() && now.Sub(v.touchedAt) < videoViewHuman}
}

// videoViewPush queues c; offered: the person is busy with the player, so it is only an offer; open: a page is there.
func videoViewPush(id string, c VideoViewCmd, now time.Time) (offered, open bool) {
	videoViews.mu.Lock()
	defer videoViews.mu.Unlock()
	v := videoViews.m[id]
	if v == nil || now.Sub(v.reported) > videoViewLive {
		return false, false
	}
	offered = !v.touchedAt.IsZero() && now.Sub(v.touchedAt) < videoViewHuman
	v.seq++
	c.Seq, c.Offer, c.at = v.seq, offered, now
	v.cmds = append(v.cmds, c)
	return offered, true
}

// VideoView is POST {artifact, t, playing, format, touchedAgo, acked} (view): {cmds}. The page calls it while the player
// is on screen.
func (a *API) VideoView(r *http.Request) Answer {
	body, id, no := a.videoGuarded(r, RoleView, http.MethodPost)
	if no != nil {
		return *no
	}
	var rep VideoViewReport
	if err := json.Unmarshal(jsvalue.Stringify(body, ""), &rep); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody("bad report"), noStore())
	}
	return JSON(r, http.StatusOK, struct {
		Cmds []VideoViewCmd `json:"cmds"`
	}{videoViewTake(id, rep, time.Now())}, noStore())
}

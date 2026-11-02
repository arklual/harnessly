package design

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The video's engine in the machine's headless Chrome, on boards drawn by the real engine (HARNESSLY_TEST_ENGINE) and
// served the way the library serves a video's board: with client.js and video-runtime.js.

// videoBoard is a board of the engine's shape with helmet styles and a template.
func videoBoard(style, tpl string) string {
	return `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><script src="./support.js"></script></head>
<body>
<x-dc>
<helmet><style>
html,body{margin:0;background:var(--deck-bg)}
.slide{position:relative;width:100vw;height:100vh;overflow:hidden;background:var(--deck-bg);color:#111;font-family:sans-serif;container-type:size}
` + style + `
</style></helmet>
<main class="board"` + `>` + tpl + `</main>
</x-dc>
<script type="text/x-dc" data-dc-script>
class Component extends DCLogic { renderVals() { return {}; } }
</script>
</body></html>
`
}

// videoScenesFixture: three scenes. Titles builds in (rise, fade, pop at 0.5, 1.2, 2.0 s); Spin runs its own
// @keyframes for 2 s (1 s delay); Still has one block at 1 s, a fade-in and nothing else.
var videoScenesFixture = []struct{ Title, Board string }{
	{"Titles", videoBoard(`h1{position:absolute;left:10cqw;top:20cqh;font-size:8cqh;margin:0}
p{position:absolute;left:10cqw;top:40cqh;font-size:4cqh;margin:0}
.logo{position:absolute;right:8cqw;top:8cqh;width:10cqh;height:10cqh;background:#e33}`,
		`<section class="slide" data-block="Slide">
<h1 data-block="Title" data-tk-at="0.5" data-build-in="rise">Hello video</h1>
<p data-block="Sub" data-tk-at="1.2" data-build-in="fade" style="opacity: .8">The second line</p>
<div class="logo" data-block="Logo" data-tk-at="2" data-build-in="pop"></div>
</section>`)},
	{"Spin", videoBoard(`@keyframes slide-in{from{transform:translateX(0)}to{transform:translateX(60cqw)}}
.bar{position:absolute;left:5cqw;top:45cqh;width:20cqw;height:10cqh;background:#36c;animation:slide-in 2s linear 1s both}`,
		`<section class="slide" data-block="Slide"><div class="bar" data-block="Bar"></div></section>`)},
	{"Still", strings.Replace(videoBoard(`h2{position:absolute;left:10cqw;top:30cqh;font-size:6cqh;margin:0}`,
		`<section class="slide" data-block="Slide"><h2 data-block="Line" data-tk-at="1">Quiet scene</h2></section>`),
		`<main class="board">`, `<main class="board" data-transition="fade">`, 1)},
}

// videoExtraScenes are scenes a test file adds to the fixture.
var videoExtraScenes []struct{ Title, Board string }

type shootRig struct {
	l      *Library
	id     string
	scenes []VideoScene
}

// newShootRig is a video project of videoScenesFixture behind a stand-in of the library server; skipped without a
// browser or an engine.
func newShootRig(t *testing.T) *shootRig {
	t.Helper()
	if testing.Short() {
		t.Skip("drives a browser")
	}
	root := t.TempDir()
	l := testLibrary(t, root)
	if l.findChrome() == "" {
		t.Skip("no browser on this machine")
	}
	engine := realEngine(t)
	id := "clip"
	writeFile(t, filepath.Join(root, id, ArtifactFile), `{"title":"Clip"}`)
	writeFile(t, filepath.Join(root, id, MetaDirname, MetaProjectFile), `{"v":1,"title":"Clip"}`)
	writeFile(t, filepath.Join(root, id, ProjectDirname, EngineFile), string(engine))
	writeFile(t, filepath.Join(root, id, ProjectDirname, "deck-theme.css"), `:root{--deck-bg:#ffffff}`)
	writeFile(t, filepath.Join(root, id, VideoSettingsFile), `{"v":1,"formats":["16:9","9:16"],"fps":30,"seconds":null,"voice":"none","subs":true}`)
	g := &shootRig{l: l, id: id}
	for i, s := range append(append([]struct{ Title, Board string }{}, videoScenesFixture...), videoExtraScenes...) {
		pid := ULID(1_700_000_000_000+int64(i), [10]byte{byte(i + 1)})
		file := s.Title + ".dc.html"
		writeFile(t, filepath.Join(root, id, MetaDirname, MetaPagesDirname, pid+".json"),
			`{"id":"`+pid+`","title":"`+s.Title+`","file":"`+file+`","w":1920,"h":1080,"order":`+strconv.Itoa(i+1)+`}`)
		writeFile(t, filepath.Join(root, id, ProjectDirname, file), s.Board)
		g.scenes = append(g.scenes, VideoScene{ID: pid, Path: file, Title: s.Title, Dur: 4000,
			Transition: VideoTransition{Kind: VideoTransitionNone}})
	}
	srv := httptest.NewServer(videoStandIn(l))
	t.Cleanup(srv.Close)
	prev := libraryBase
	libraryBase = func(*Library) (string, error) { return srv.URL, nil }
	t.Cleanup(func() { libraryBase = prev })
	return g
}

// videoStandIn serves boards as the library does: the vendor files, client.js, video-runtime.js and a board with the
// tags put in.
func videoStandIn(l *Library) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, vendorPrefix):
			l.serveVendor(w, r, r.URL.Path, false)
			return
		case strings.HasPrefix(r.URL.Path, motionKitPrefix):
			l.serveMotionKit(w, r, r.URL.Path, false)
			return
		case r.URL.Path == scriptPath:
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write([]byte(l.clientScript()))
			return
		case r.URL.Path == videoRuntimePath:
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write([]byte(l.text("video-runtime.js")))
			return
		}
		ref, file, ok := ParsePagePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		real := l.ResolveProjectFile(ref, strings.Split(file, "/"))
		if real == "" {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(real, ".html") {
			src, err := os.ReadFile(real)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(withClientOf(string(src), ref, l.IsVideo(ref))))
			return
		}
		http.ServeFile(w, r, real)
	})
}

// shoot runs videoFrames over times in a fresh tab and gives each frame's hash, Same flag and the time it took.
func (g *shootRig) shoot(t *testing.T, s *deckShooter, sc VideoScene, f VideoFormat, draft bool, times []int64) (hashes []string, same []bool, took time.Duration) {
	t.Helper()
	tab, release, err := s.videoTab(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var start time.Time
	err = g.l.videoFrames(context.Background(), tab, g.id, sc, f, draft, times, func(v videoShot) error {
		if v.I == 0 {
			start = time.Now()
		}
		sum := sha256.Sum256(v.Image)
		hashes = append(hashes, hex.EncodeToString(sum[:8]))
		same = append(same, v.Same)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hashes, same, time.Since(start)
}

func videoTimes(dur int64, fps int) []int64 {
	var out []int64
	for i := int64(0); i*1000/int64(fps) < dur; i++ {
		out = append(out, i*1000/int64(fps))
	}
	return out
}

// The engine on the fixture: build-ins by time, the board's @keyframes seeked, Same over a static stretch, the 9:16
// viewport, and the same frames on a second run.
func TestVideoFramesInChrome(t *testing.T) {
	g := newShootRig(t)
	s, err := g.l.newVideoShooter(g.id)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	// Build-in: hidden before its moment, on its way 300 ms in, whole at the end; its own opacity is kept.
	tab, release, err := s.videoTab(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.videoOpen(g.id, g.scenes[0], VideoLandscape, true); err != nil {
		t.Fatal(err)
	}
	style := func(block, prop string) string {
		v, err := tab.eval(`(() => { const el = document.querySelector('[data-block="` + block + `"]'); return el ? getComputedStyle(el)['` + prop + `'] : 'none'; })()`)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := v.(string)
		return out
	}
	num := func(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
	seek := func(ms int64) bool {
		same, err := tab.videoSeek(ms)
		if err != nil {
			t.Fatal(err)
		}
		return same
	}
	seek(499)
	if o := num(style("Title", "opacity")); o != 0 {
		t.Errorf("title before its moment: opacity %v", o)
	}
	seek(800)
	if o := num(style("Title", "opacity")); o <= 0.3 || o >= 1 {
		t.Errorf("title 300 ms in: opacity %v", o)
	}
	if tr := style("Title", "translate"); tr == "none" || tr == "0px" {
		t.Errorf("a rising title has no lift 300 ms in: %q", tr)
	}
	seek(3999)
	if o := num(style("Title", "opacity")); o != 1 {
		t.Errorf("title at the end: opacity %v", o)
	}
	if o := num(style("Sub", "opacity")); o < 0.79 || o > 0.81 {
		t.Errorf("the sub's own opacity .8 is not kept: %v", o)
	}
	if !seek(3999) {
		t.Error("the same time twice is not Same")
	}
	moments, err := tab.eval(`window.tkVideo.moments()`)
	if err != nil {
		t.Fatal(err)
	}
	if ms, _ := moments.([]any); len(ms) != 3 {
		t.Errorf("moments = %v", moments)
	}
	// The board's @keyframes: seen and seeked.
	if err := tab.videoOpen(g.id, g.scenes[1], VideoLandscape, true); err != nil {
		t.Fatal(err)
	}
	n, err := tab.eval(`document.getAnimations().length`)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("getAnimations in the engine's DOM: %v", n)
	if c, _ := n.(float64); c < 1 {
		t.Error("getAnimations sees no animation in the engine's DOM")
	}
	seek(500)
	before := style("Bar", "transform")
	seek(2000)
	mid := style("Bar", "transform")
	seek(500)
	again := style("Bar", "transform")
	if before == mid || before != again {
		t.Errorf("@keyframes not seeked: 500 → %q, 2000 → %q, 500 again → %q", before, mid, again)
	}
	// 9:16: the viewport is the format's.
	if err := tab.videoOpen(g.id, g.scenes[2], VideoPortrait, true); err != nil {
		t.Fatal(err)
	}
	size, _ := tab.eval(`innerWidth + 'x' + innerHeight`)
	if size != "1080x1920" {
		t.Errorf("9:16 viewport = %v", size)
	}
	release()

	// Same over a static stretch; the same frames twice.
	times := videoTimes(4000, 30)
	h1, same, _ := g.shoot(t, s, g.scenes[2], VideoLandscape, true, times)
	h2, _, _ := g.shoot(t, s, g.scenes[2], VideoLandscape, true, times)
	last := len(times) - 1
	if !same[last] || same[0] {
		t.Errorf("Same over the still end: %v … %v", same[0], same[last])
	}
	for i := range h1 {
		if h1[i] != h2[i] {
			t.Errorf("frame %d (%d ms) differs between runs", i, times[i])
			break
		}
	}
}

// The day-1 gate (VIDEO_SPIKE=1): ms per frame at 1080p and draft, the share of static frames, frame hashes across two
// runs, on the three-scene fixture at 30 fps.
func TestVideoSpike(t *testing.T) {
	if os.Getenv("VIDEO_SPIKE") == "" {
		t.Skip("VIDEO_SPIKE is not set")
	}
	g := newShootRig(t)
	// The capture's own cost: the wait on a busy machine is left out here.
	prevLoad := videoLoad
	videoLoad = func() (float64, bool) { return 0, true }
	defer func() { videoLoad = prevLoad }()
	s, err := g.l.newVideoShooter(g.id)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	times := videoTimes(4000, 30)
	for _, draft := range []bool{true, false} {
		frames, static, differ := 0, 0, 0
		var total time.Duration
		for _, sc := range g.scenes {
			h1, same, took := g.shoot(t, s, sc, VideoLandscape, draft, times)
			h2, _, _ := g.shoot(t, s, sc, VideoLandscape, draft, times)
			total += took
			frames += len(times) - 1
			for i := range h1 {
				if same[i] {
					static++
				}
				if h1[i] != h2[i] {
					differ++
				}
			}
			t.Logf("draft=%v %s: %d frames in %v", draft, sc.Title, len(times), took)
		}
		t.Logf("SPIKE draft=%v: %.1f ms/frame, static %d/%d (%.0f%%), frames differing between runs %d",
			draft, float64(total.Milliseconds())/float64(frames), static, frames, 100*float64(static)/float64(frames), differ)
	}
}

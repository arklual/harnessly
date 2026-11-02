package design

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// vrig is a video of the library's "admin" project with three scenes, the engine and the scene list faked.
type vrig struct {
	*deckRig
	scenes []VideoScene
	mu     sync.Mutex
	drawn  map[string]int // frames asked of the engine, per scene
	gate   chan struct{}  // when set, the engine waits for it to close
	jpeg   []byte
}

func newRenderRig(t *testing.T, subs bool) *vrig {
	t.Helper()
	g := &vrig{deckRig: newDeckRig(t), drawn: map[string]int{}, jpeg: []byte("jpeg")}
	if err := g.l.InitDeck("admin", DeckInit{Minutes: 10, Tone: DeckToneTalk}); err != nil {
		t.Fatal(err)
	}
	if err := g.l.InitVideo("admin", VideoInit{Formats: []VideoFormat{VideoLandscape, VideoPortrait}, Subs: subs}); err != nil {
		t.Fatal(err)
	}
	g.rescene()
	prevScenes, prevFrames, prevBrowser := videoScenesImpl, videoFramesImpl, videoUsesBrowser
	videoScenesImpl = func(*Library, string) ([]VideoScene, error) { return g.scenes, nil }
	videoFramesImpl = g.frames
	videoUsesBrowser = false
	t.Cleanup(func() {
		videoScenesImpl, videoFramesImpl, videoUsesBrowser = prevScenes, prevFrames, prevBrowser
	})
	return g
}

// rescene lists the pages as scenes of 1 s, 1.5 s, 2 s, each with a sentence of text.
func (g *vrig) rescene() {
	g.scenes = nil
	var at int64
	for i, p := range g.l.ReadModel("admin").Pages {
		dur := int64(1000 + 500*i)
		g.scenes = append(g.scenes, VideoScene{ID: p.ID, Path: p.File, Title: p.Title, Start: at, Dur: dur,
			Notes: "Scene " + p.Title + " says a few words here", Transition: VideoTransition{Kind: VideoTransitionFade, Ms: 300}})
		at += dur
	}
}

func (g *vrig) frames(_ *Library, ctx context.Context, _ *deckTab, _ string, sc VideoScene, _ VideoFormat, _ bool, times []int64, each func(videoShot) error) error {
	if g.gate != nil {
		select {
		case <-g.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for i, at := range times {
		g.mu.Lock()
		g.drawn[sc.ID]++
		g.mu.Unlock()
		if err := each(videoShot{I: i, T: at, Image: g.jpeg, Same: i > 0}); err != nil {
			return err
		}
	}
	return nil
}

func (g *vrig) board(t *testing.T, i int) string {
	t.Helper()
	file, _, _ := strings.Cut(g.scenes[i].Path, "?")
	return filepath.Join(g.l.ProjectDir("admin"), file)
}

// fakeFFmpeg is a program that takes what is piped to it and writes the last argument as its output.
func fakeFFmpeg(t *testing.T) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\ncat >/dev/null 2>&1 </dev/stdin\nfor a; do last=$a; done\necho clip > \"$last\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIME_DESIGN_FFMPEG", bin)
}

func waitVideoJob(t *testing.T, l *Library, id, job string) VideoRenderJob {
	t.Helper()
	for end := time.Now().Add(5 * time.Minute); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		j, ok := l.VideoRenderJobOf(id, job)
		if !ok {
			t.Fatalf("job %s is gone", job)
		}
		if j.State == DeckJobDone || j.State == DeckJobFailed {
			return j
		}
	}
	t.Fatalf("job %s did not finish", job)
	return VideoRenderJob{}
}

func bump(t *testing.T, path string) {
	t.Helper()
	at := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// What a clip is drawn from changes its key; what it is not drawn from leaves it, and a neighbour's edit is not an edit.
func TestSceneKeyFollowsItsInputs(t *testing.T) {
	g := newRenderRig(t, false)
	a, b := g.scenes[0], g.scenes[1]
	key := func(sc VideoScene, f VideoFormat, draft bool) string {
		t.Helper()
		k, err := g.l.VideoSceneKey("admin", sc, f, draft)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	base := key(a, VideoLandscape, false)
	if len(base) != 64 || key(a, VideoLandscape, false) != base {
		t.Fatalf("a key is a stable sha256: %q", base)
	}
	if key(a, VideoPortrait, false) == base || key(a, VideoLandscape, true) == base {
		t.Error("the format and the draft flag are in the key")
	}
	keyB := key(b, VideoLandscape, false)
	if keyB == base {
		t.Error("two scenes share a key")
	}
	moved := a
	moved.Start = 99999
	if key(moved, VideoLandscape, false) != base {
		t.Error("the place of a scene on the time line is in its key")
	}
	longer := a
	longer.Dur += 1000
	if key(longer, VideoLandscape, false) == base {
		t.Error("the duration is not in the key")
	}

	// The theme.
	theme := filepath.Join(g.root, "admin", DeckThemeCSS)
	writeFile(t, theme, readFileT(t, theme)+"\n/* edit */")
	themed := key(a, VideoLandscape, false)
	if themed == base {
		t.Error("the theme is not in the key")
	}
	// Scene B's board: B moves, A stays.
	writeFile(t, g.board(t, 1), readFileT(t, g.board(t, 1))+"<!-- edit B -->")
	if key(b, VideoLandscape, false) == keyB {
		t.Error("the board is not in the key")
	}
	if key(a, VideoLandscape, false) != themed {
		t.Error("editing scene B changed scene A's key")
	}
	// The scene file.
	sceneFile := filepath.Join(g.root, "admin", VideoScenesDir, a.ID+".json")
	writeFile(t, sceneFile, `{"v":1,"dur":4000,"transition":null}`)
	withMeta := key(a, VideoLandscape, false)
	if withMeta == themed {
		t.Error("meta/scenes is not in the key")
	}
	// Assets and kits: any other file of the project, by its mtime.
	asset := filepath.Join(g.l.ProjectDir("admin"), "logo.svg")
	writeFile(t, asset, "<svg/>")
	withAsset := key(a, VideoLandscape, false)
	if withAsset == withMeta {
		t.Error("a new asset is not in the key")
	}
	bump(t, asset)
	if key(a, VideoLandscape, false) == withAsset {
		t.Error("the mtime of an asset is not in the key")
	}
	// The engine release, the runtime version and fps.
	k, err := g.l.newVideoKeyer("admin")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := k.scene(a, VideoLandscape, false)
	k.engine = "2026-10-01"
	if after, _ := k.scene(a, VideoLandscape, false); after == before {
		t.Error("the engine release is not in the key")
	}
	k.engine = ""
	prevRuntime, prevFPS := videoRuntimeNow, videoFPSNow
	defer func() { videoRuntimeNow, videoFPSNow = prevRuntime, prevFPS }()
	videoRuntimeNow++
	if after, _ := k.scene(a, VideoLandscape, false); after == before {
		t.Error("the runtime version is not in the key")
	}
	videoRuntimeNow = prevRuntime
	videoFPSNow = 60
	if after, _ := k.scene(a, VideoLandscape, false); after == before {
		t.Error("fps is not in the key")
	}
	videoFPSNow = prevFPS
	// A scene whose board is gone has no key; an unknown format has none.
	gone := a
	gone.Path = "nope.dc.html"
	if _, err := g.l.VideoSceneKey("admin", gone, VideoLandscape, false); err == nil {
		t.Error("a key without a board")
	}
	escape := a
	escape.Path = "../../etc/passwd"
	if _, err := g.l.VideoSceneKey("admin", escape, VideoLandscape, false); err == nil {
		t.Error("a board outside the project")
	}
	if _, err := g.l.VideoSceneKey("admin", a, "4:3", false); err == nil {
		t.Error("an unknown format")
	}
}

func TestFFmpegCommandLines(t *testing.T) {
	clip := videoClipArgs(VideoLandscape, false, "/o/c.mp4.part")
	wantClip := []string{"-hide_banner", "-y", "-loglevel", "error", "-f", "image2pipe", "-c:v", "png", "-framerate", "30", "-i", "pipe:0",
		"-vf", "scale=1920:1080:out_color_matrix=bt709:out_range=tv,setsar=1,format=yuv420p", "-an", "-c:v", "libx264", "-preset", "slow", "-crf", "16", "-r", "30",
		"-pix_fmt", "yuv420p", "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv", "-threads", "2", "-f", "mp4", "/o/c.mp4.part"}
	if !reflect.DeepEqual(clip, wantClip) {
		t.Errorf("clip args\n got %q\nwant %q", clip, wantClip)
	}
	draft := strings.Join(videoClipArgs(VideoPortrait, true, "x"), " ")
	for _, want := range []string{"scale=540:960", "-crf 28", "-c:v mjpeg", "-preset veryfast"} {
		if !strings.Contains(draft, want) {
			t.Errorf("a draft in 9:16 lacks %q: %s", want, draft)
		}
	}
	join := videoJoinArgs("list.txt", "/o/out.mp4")
	wantJoin := []string{"-hide_banner", "-y", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "file",
		"-f", "concat", "-safe", "1", "-i", "list.txt", "-c", "copy", "-an", "-movflags", "+faststart", "-f", "mp4", "/o/out.mp4"}
	if !reflect.DeepEqual(join, wantJoin) {
		t.Errorf("join args\n got %q\nwant %q", join, wantJoin)
	}
	subs := strings.Join(videoSubsArgs("joined.mp4", "out.mp4", false), " ")
	for _, want := range []string{"-vf subtitles=subs.ass", "-an", "-movflags +faststart", "-protocol_whitelist file", "-crf 16", "-i joined.mp4"} {
		if !strings.Contains(subs, want) {
			t.Errorf("subtitles pass lacks %q: %s", want, subs)
		}
	}
}

// Only the hex names of our own clips reach the concat list.
func TestConcatListHasNoOutsideNames(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("0f", 32)
	got, err := videoConcatList([]string{a, b})
	if err != nil || got != "file '"+a+".mp4'\nfile '"+b+".mp4'\n" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"../x", "/etc/passwd", a[:63], a + "x", "x'\nfile '/etc/passwd", strings.ToUpper(a), ""} {
		if _, err := videoConcatList([]string{bad}); err == nil {
			t.Errorf("%q went into the list", bad)
		}
	}
}

func TestFrameTimesAndSubtitleTiming(t *testing.T) {
	times := videoFrameTimes(1000)
	if len(times) != 30 || times[0] != 0 || times[1] != 33 || times[29] != 967 {
		t.Errorf("1 s: %d frames %v", len(times), times[:3])
	}
	if got := videoFrameTimes(1); len(got) != 1 {
		t.Errorf("a scene has at least a frame: %v", got)
	}
	subs := videoSubs([]VideoScene{
		{Start: 0, Dur: 3000, Notes: "One two. Three four!\nFive"}, // three lines
		{Start: 3000, Dur: 3000, Notes: ""},                        // none
		{Start: 6000, Dur: 1000, Notes: "Just one"},                // one line over the whole scene
	})
	want := []mp4Sub{{0, 1000, "One two."}, {1000, 2000, "Three four!"}, {2000, 3000, "Five"}, {6000, 7000, "Just one"}}
	if !reflect.DeepEqual(subs, want) {
		t.Fatalf("subs %+v, want %+v", subs, want)
	}
	ass := mp4ASS(1920, 1080, subs)
	for _, line := range []string{"Dialogue: 0,0:00:00.00,0:00:01.00,Default", "Dialogue: 0,0:00:01.00,0:00:02.00,Default", "Dialogue: 0,0:00:06.00,0:00:07.00,Default"} {
		if !strings.Contains(ass, line) {
			t.Errorf("the ASS lacks %q", line)
		}
	}
}

func TestKeepNewestRendersAndTheCacheLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i, n := range []string{"a", "b", "c", "d", "e"} {
		p := filepath.Join(dir, n+".mp4")
		writeFile(t, p, n)
		at := now.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, at, at)
	}
	writeFile(t, filepath.Join(dir, "note.txt"), "x")
	// "a" is the oldest but just made: it stays, with the 3 newest others.
	dropOldVideoRenders(dir, VideoRendersKept, filepath.Join(dir, "a.mp4"))
	names, _ := readNames(dir)
	if !reflect.DeepEqual(names, []string{"a.mp4", "c.mp4", "d.mp4", "e.mp4", "note.txt"}) {
		t.Fatalf("kept %v", names)
	}

	art := t.TempDir()
	clips := filepath.Join(art, filepath.FromSlash(VideoSceneClipsDir))
	_ = os.MkdirAll(clips, 0o755)
	for i, n := range []string{"old", "mid", "new", "busy"} {
		p := filepath.Join(clips, n+".mp4")
		writeFile(t, p, strings.Repeat("x", 100))
		at := now.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, at, at)
	}
	stale := filepath.Join(clips, "dead.mp4.part")
	writeFile(t, stale, "x")
	_ = os.Chtimes(stale, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	// 400 bytes against a limit of 250: the two least recently used go, the one in use stays whatever its age.
	pruneVideoCache(art, 250, []string{"old"})
	left, _ := readNames(clips)
	if !reflect.DeepEqual(left, []string{"busy.mp4", "old.mp4"}) {
		t.Fatalf("left %v", left)
	}
}

func TestStartRefusesWhatCannotBeRendered(t *testing.T) {
	g := newRenderRig(t, false)
	fakeFFmpeg(t)
	statusOf := func(err error) int {
		t.Helper()
		var de *DeckError
		if err == nil || !asDeckErr(err, &de) {
			t.Fatalf("not a refusal: %v", err)
		}
		return de.Status
	}
	one := "admin"
	if _, err := g.l.StartVideoRender("nope", VideoRenderRequest{Format: VideoLandscape}); statusOf(err) != 404 {
		t.Error("a project that is not a video")
	}
	if _, err := g.l.StartVideoRender(one, VideoRenderRequest{Format: "4:3"}); statusOf(err) != 400 {
		t.Error("an unknown format")
	}
	if err := g.l.WriteVideoSettings(one, VideoSettings{V: 1, Formats: []VideoFormat{VideoLandscape}, FPS: 30, Voice: VideoVoiceNone}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.l.StartVideoRender(one, VideoRenderRequest{Format: VideoPortrait}); statusOf(err) != 400 {
		t.Error("a format the video does not make")
	}
	ghost := "no-such-scene"
	if _, err := g.l.StartVideoRender(one, VideoRenderRequest{Format: VideoLandscape, Page: &ghost}); statusOf(err) != 404 {
		t.Error("an unknown scene")
	}
}

func asDeckErr(err error, target **DeckError) bool {
	de, ok := err.(*DeckError)
	if ok {
		*target = de
	}
	return ok
}

// Asked the same twice, a render is one job; a different draft flag or scene is another; they run one at a time.
func TestRenderJobsAreDedupedAndRunOneAtATime(t *testing.T) {
	g := newRenderRig(t, false)
	fakeFFmpeg(t)
	g.gate = make(chan struct{})
	l := g.l
	first, err := l.StartVideoRender("admin", VideoRenderRequest{Format: VideoLandscape})
	if err != nil || first.ID == "" || first.State != DeckJobQueued && first.State != DeckJobRunning {
		t.Fatalf("%+v %v", first, err)
	}
	again, _ := l.StartVideoRender("admin", VideoRenderRequest{Format: VideoLandscape})
	if again.ID != first.ID {
		t.Errorf("the same ask is another job: %s and %s", first.ID, again.ID)
	}
	draft, _ := l.StartVideoRender("admin", VideoRenderRequest{Format: VideoLandscape, Draft: true})
	page := g.scenes[1].ID
	scene, _ := l.StartVideoRender("admin", VideoRenderRequest{Format: VideoLandscape, Page: &page})
	portrait, _ := l.StartVideoRender("admin", VideoRenderRequest{Format: VideoPortrait})
	ids := map[string]bool{first.ID: true, draft.ID: true, scene.ID: true, portrait.ID: true}
	if len(ids) != 4 {
		t.Errorf("a draft, a scene and a format are jobs of their own: %v", ids)
	}
	if j := l.VideoRunningJob("admin"); j == nil || !ids[j.ID] {
		t.Errorf("the running job: %+v", j)
	}
	if l.VideoRunningJob("other") != nil {
		t.Error("another project's job")
	}
	close(g.gate)
	for id := range ids {
		if j := waitVideoJob(t, l, "admin", id); j.State != DeckJobDone || j.File == nil || j.Progress != 1 {
			t.Errorf("job %s: %+v", id, j)
		}
	}
	if l.VideoRunningJob("admin") != nil {
		t.Error("a job is still running")
	}
	if _, ok := l.VideoRenderJobOf("admin", "nope"); ok {
		t.Error("a job that never was")
	}
	// Once finished, the same ask is a new job.
	next, _ := l.StartVideoRender("admin", VideoRenderRequest{Format: VideoLandscape})
	if next.ID == first.ID {
		t.Error("a finished job was joined")
	}
	waitVideoJob(t, l, "admin", next.ID)
	// The finished render is not drawn again: every scene clip came from the cache.
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, sc := range g.scenes {
		// Each scene was drawn once per variant: landscape, draft, portrait (the scene job reused scene 1's landscape clip).
		if want := 3 * len(videoFrameTimes(sc.Dur)); g.drawn[sc.ID] != want {
			t.Errorf("scene %s drawn %d frames, want %d", sc.ID, g.drawn[sc.ID], want)
		}
	}
}

func TestMissingFFmpegFailsTheJobInWords(t *testing.T) {
	g := newRenderRig(t, false)
	t.Setenv("RUNTIME_DESIGN_FFMPEG", filepath.Join(t.TempDir(), "no-ffmpeg"))
	j, err := g.l.StartVideoRender("admin", VideoRenderRequest{Format: VideoLandscape})
	if err != nil {
		t.Fatal(err)
	}
	if j.State != DeckJobFailed || j.Error == nil || !strings.Contains(strings.ToLower(*j.Error), "ffmpeg") {
		t.Fatalf("%+v", j)
	}
	if g.l.VideoRunningJob("admin") != nil {
		t.Error("a failed job runs")
	}
	if got, ok := g.l.VideoRenderJobOf("admin", j.ID); !ok || got.State != DeckJobFailed {
		t.Errorf("the failed job is not kept: %+v", got)
	}
}

// An engine that fails fails the job with the scene's name, and leaves no half clip.
func TestAnEngineErrorFailsTheJobAndLeavesNoClip(t *testing.T) {
	g := newRenderRig(t, false)
	fakeFFmpeg(t)
	videoFramesImpl = func(*Library, context.Context, *deckTab, string, VideoScene, VideoFormat, bool, []int64, func(videoShot) error) error {
		return context.DeadlineExceeded
	}
	j, _ := g.l.StartVideoRender("admin", VideoRenderRequest{Format: VideoLandscape})
	done := waitVideoJob(t, g.l, "admin", j.ID)
	if done.State != DeckJobFailed || done.Error == nil {
		t.Fatalf("%+v", done)
	}
	clips := filepath.Join(g.root, "admin", filepath.FromSlash(VideoSceneClipsDir))
	left, _ := readNames(clips)
	for _, n := range left {
		if strings.HasSuffix(n, ".mp4") || strings.HasSuffix(n, ".part") {
			t.Errorf("a clip was left: %s", n)
		}
	}
}

func TestRendersStateAgainstTheCurrentKeys(t *testing.T) {
	g := newRenderRig(t, true)
	s, _ := g.l.ReadVideoSettings("admin")
	infos := g.l.VideoRenders("admin", s, g.scenes)
	if len(infos) != 2 || infos[0].Format != VideoLandscape || infos[1].Format != VideoPortrait {
		t.Fatalf("%+v", infos)
	}
	for _, in := range infos {
		if in.Published || in.Key != nil || in.DurMs != nil || in.At != nil || in.Fresh || len(in.CurrentKey) != 64 {
			t.Errorf("nothing published yet: %+v", in)
		}
	}
	key := infos[0].CurrentKey
	pub := g.l.publishedDir("admin")
	writeFile(t, filepath.Join(pub, "video.mp4"), "mp4")
	raw, _ := json.Marshal(VideoPublished{Formats: map[VideoFormat]VideoPublishedFormat{VideoLandscape: {Key: key, DurMs: 4500, At: "2026-10-28T10:00:00Z"}}})
	writeFile(t, filepath.Join(pub, "video.json"), string(raw))
	got := g.l.VideoRenders("admin", s, g.scenes)[0]
	if !got.Published || !got.Fresh || got.Key == nil || *got.Key != key || *got.DurMs != 4500 || *got.At != "2026-10-28T10:00:00Z" {
		t.Fatalf("fresh: %+v", got)
	}
	// An edit makes it stale; the 9:16 file is not there, so it is not published whatever video.json says.
	writeFile(t, g.board(t, 2), readFileT(t, g.board(t, 2))+"<!-- edit -->")
	got = g.l.VideoRenders("admin", s, g.scenes)[0]
	if !got.Published || got.Fresh || *got.Key != key || got.CurrentKey == key {
		t.Errorf("stale: %+v", got)
	}
	// Turning the subtitles off is a different video.
	s.Subs = false
	if g.l.VideoRenders("admin", s, g.scenes)[0].CurrentKey == got.CurrentKey {
		t.Error("the subtitles are not in the video's key")
	}
}

func TestRenderRoutes(t *testing.T) {
	g := newRenderRig(t, false)
	fakeFFmpeg(t)
	g.a.CLIToken = func(*http.Request) bool { return true }
	page := g.scenes[0].ID

	code, out := g.call(t, g.a.VideoRender, "POST", "/x", `{"artifact":"admin","format":"16:9","draft":true}`)
	if code != 202 || out["state"] == nil || out["format"] != "16:9" || out["draft"] != true || out["scene"] != nil {
		t.Fatalf("start: %d %v", code, out)
	}
	id := out["id"].(string)
	waitVideoJob(t, g.l, "admin", id)
	code, out = g.call(t, g.a.VideoRender, "GET", "/x?artifact=admin&id="+id, "")
	if code != 200 || out["state"] != "done" || out["file"] == nil || out["error"] != nil {
		t.Fatalf("job: %d %v", code, out)
	}
	if code, _ = g.call(t, g.a.VideoRender, "GET", "/x?artifact=admin&id=nope", ""); code != 404 {
		t.Errorf("unknown job: %d", code)
	}
	if code, _ = g.call(t, g.a.VideoRender, "POST", "/x", `{"artifact":"admin","format":"4:3"}`); code != 400 {
		t.Errorf("bad format: %d", code)
	}
	if code, _ = g.call(t, g.a.VideoRender, "POST", "/x", `{"artifact":"admin","format":"16:9","page":"zzz"}`); code != 404 {
		t.Errorf("bad scene: %d", code)
	}
	if code, _ = g.call(t, g.a.VideoRender, "PUT", "/x", `{"artifact":"admin"}`); code != 405 {
		t.Errorf("PUT: %d", code)
	}

	// The job's own file (a draft, or one scene) is served by job id.
	file := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		g.a.VideoFile(w, httptest.NewRequest("GET", "/x?artifact=admin&"+query, nil))
		return w
	}
	if w := file("format=16:9&job=" + id); w.Code != 200 || w.Header().Get("Content-Type") != "video/mp4" ||
		!strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Errorf("job file: %d %v", w.Code, w.Header())
	}
	if w := file("format=16:9"); w.Code != 404 {
		t.Errorf("nothing is published yet: %d", w.Code)
	}
	if w := file("format=4:3"); w.Code != 400 {
		t.Errorf("bad format: %d", w.Code)
	}
	if w := file("format=16:9&job=nope"); w.Code != 404 {
		t.Errorf("unknown job: %d", w.Code)
	}
	writeFile(t, filepath.Join(g.l.publishedDir("admin"), "video-9x16.mp4"), "0123456789")
	r := httptest.NewRequest("GET", "/x?artifact=admin&format=9:16", nil)
	r.Header.Set("Range", "bytes=2-5")
	w := httptest.NewRecorder()
	g.a.VideoFile(w, r)
	if w.Code != 206 || w.Body.String() != "2345" || !strings.Contains(w.Header().Get("Content-Disposition"), "9x16") {
		t.Errorf("published, with a range: %d %q %v", w.Code, w.Body.String(), w.Header())
	}

	// The CLI names a scene by id, file or title and answers the job with the project.
	title := g.scenes[0].Title
	code, out = g.call(t, g.a.CLIVideoRender, "POST", "/x", `{"project":"admin","format":"16:9","page":"`+title+`"}`)
	if code != 202 || out["scene"] != page {
		t.Fatalf("cli start: %d %v", code, out)
	}
	waitVideoJob(t, g.l, "admin", out["id"].(string))
	code, out = g.call(t, g.a.CLIVideoRender, "GET", "/x?project=admin&id="+out["id"].(string), "")
	if code != 200 || out["state"] != "done" {
		t.Fatalf("cli job: %d %v", code, out)
	}
	if code, _ = g.call(t, g.a.CLIVideoRender, "POST", "/x", `{"project":"admin","format":"16:9","page":"no such scene"}`); code != 404 {
		t.Errorf("cli unknown scene: %d", code)
	}
	if code, _ = g.call(t, g.a.CLIVideoRender, "GET", "/x?project=admin&id=nope", ""); code != 404 {
		t.Errorf("cli unknown job: %d", code)
	}
}

func TestRenderRoutesAskForTheRightRole(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Video", "")
	l := accessLibrary(t, root)
	l.Migrate(false)
	if err := l.InitDeck("p1", DeckInit{Minutes: 5, Tone: DeckToneTalk}); err != nil {
		t.Fatal(err)
	}
	if err := l.InitVideo("p1", VideoInit{}); err != nil {
		t.Fatal(err)
	}
	fakeFFmpeg(t)
	prevScenes, prevFrames, prevBrowser := videoScenesImpl, videoFramesImpl, videoUsesBrowser
	defer func() {
		videoScenesImpl, videoFramesImpl, videoUsesBrowser = prevScenes, prevFrames, prevBrowser
	}()
	pg := l.ReadModel("p1").Pages[0]
	videoScenesImpl = func(*Library, string) ([]VideoScene, error) {
		return []VideoScene{{ID: pg.ID, Path: pg.File, Title: pg.Title, Dur: 1000}}, nil
	}
	videoFramesImpl = func(_ *Library, _ context.Context, _ *deckTab, _ string, _ VideoScene, _ VideoFormat, _ bool, times []int64, each func(videoShot) error) error {
		for i, at := range times {
			if err := each(videoShot{I: i, T: at, Image: []byte("j")}); err != nil {
				return err
			}
		}
		return nil
	}
	videoUsesBrowser = false
	a := &API{L: l, Session: func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	g := &deckRig{l: l, a: a}
	want := map[string][]int{ // none, view, comment, edit
		"start": {404, 403, 403, 202},
		"read":  {404, 404, 404, 404}, // an unknown job
	}
	for i, level := range []string{"none", "view", "comment", "edit"} {
		if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"p1": level})); err != nil {
			t.Fatal(err)
		}
		code, out := g.call(t, a.VideoRender, "POST", "/x", `{"artifact":"p1","format":"16:9","draft":true}`)
		if code != want["start"][i] {
			t.Errorf("start as %s: %d %v", level, code, out)
		}
		if id, ok := out["id"].(string); ok {
			waitVideoJob(t, l, "p1", id)
		}
		code, _ = g.call(t, a.VideoRender, "GET", "/x?artifact=p1&id=nope", "")
		if level == "none" && code != 404 || level != "none" && code != 404 {
			t.Errorf("read as %s: %d", level, code)
		}
		w := httptest.NewRecorder()
		a.VideoFile(w, httptest.NewRequest("GET", "/x?artifact=p1&format=16:9", nil))
		if w.Code != 404 {
			t.Errorf("file as %s: %d", level, w.Code)
		}
	}
}

// A project that is not a library id, or not a video, is refused before anything is read or drawn.
func TestCLIVideoRoutesRefuseNonVideoProjects(t *testing.T) {
	g := newRenderRig(t, false)
	g.a.CLIToken = func(*http.Request) bool { return true }
	if err := os.MkdirAll(g.l.ProjectDir("plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"../x", "..", "", "plain", "nope"} {
		q := url.QueryEscape(project)
		calls := []struct {
			name           string
			h              func(*http.Request) Answer
			method, target string
			body           string
		}{
			{"frame", g.a.CLIVideoFrame, "GET", "/x?project=" + q + "&page=p&at=0", ""},
			{"check", g.a.CLIVideoCheck, "POST", "/x", `{"project":` + strconv.Quote(project) + `}`},
			{"render post", g.a.CLIVideoRender, "POST", "/x", `{"project":` + strconv.Quote(project) + `,"format":"16:9"}`},
			{"render get", g.a.CLIVideoRender, "GET", "/x?project=" + q + "&id=x", ""},
		}
		for _, c := range calls {
			if code, _ := g.call(t, c.h, c.method, c.target, c.body); code < 400 || code >= 500 {
				t.Errorf("%s with project %q: %d", c.name, project, code)
			}
		}
	}
	if _, err := g.l.ReadVideoSettings("../x"); err == nil {
		t.Error("ReadVideoSettings accepted a path")
	}
	if len(g.drawn) != 0 {
		t.Errorf("the engine drew %v", g.drawn)
	}
	// A real video still works (the frame route draws in a browser, which CI does not have: the check route does not).
	if code, out := g.call(t, g.a.CLIVideo, "GET", "/x?project=admin&what=state", ""); code != 200 || out["project"] != "admin" {
		t.Errorf("video state: %d %v", code, out)
	}
	if code, out := g.call(t, g.a.CLIVideoRender, "GET", "/x?project=admin&id=nope", ""); code != 404 || out["error"] == nil {
		t.Errorf("video render job: %d %v", code, out)
	}
}

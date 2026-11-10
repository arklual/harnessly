package design

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A board of a deck or a site is served as before; a board of a video gets the scene's runtime before its engine, and
// the runtime itself is served.
func TestVideoBoardGetsTheRuntime(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	l.srv = &server{}
	board := "<!doctype html>\n<html><head><meta charset=\"utf-8\"><script src=\"./support.js\"></script></head><body><x-dc></x-dc></body></html>\n"
	for _, id := range []string{"deck", "site", "clip"} {
		writeFile(t, filepath.Join(root, id, ArtifactFile), `{"title":"`+id+`"}`)
		writeFile(t, filepath.Join(root, id, ProjectDirname, "One.dc.html"), board)
	}
	writeFile(t, filepath.Join(root, "deck", DeckSettingsFile), `{}`)
	writeFile(t, filepath.Join(root, "clip", DeckSettingsFile), `{}`)
	writeFile(t, filepath.Join(root, "clip", VideoSettingsFile), `{"v":1,"formats":["16:9"],"fps":30,"seconds":null,"voice":"none","subs":true}`)
	get := func(path string) string {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18470"
		l.serveLibrary(w, r, 18470)
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		return w.Body.String()
	}
	for _, id := range []string{"deck", "site"} {
		want := strings.Replace(board, "<head>", "<head>"+resourcesScript()+`<script data-artifact="`+id+`" src="`+l.clientSrc()+`"></script>`, 1)
		if got := get("/a/" + id + "/p/One.dc.html"); got != want {
			t.Errorf("%s board changed:\n%s", id, got)
		}
	}
	got := get("/a/clip/p/One.dc.html")
	tag := strings.Index(got, `<script src="`+videoRuntimePath+`"></script>`)
	engine := strings.Index(got, `src="./support.js`)
	client := strings.Index(got, `src="`+l.clientSrc()+`"`)
	if tag < 0 || engine < 0 || tag > engine || client > tag {
		t.Errorf("the video board has no runtime before its engine:\n%s", got)
	}
	if js := get(videoRuntimePath); !strings.Contains(js, "window.tkVideo = {") {
		t.Error("the runtime is not served")
	}
	if withClientOf(board, "x", false) != withClient(board, "x") {
		t.Error("withClient is not withClientOf without the video")
	}
}

// The static lint finds the wall clock in a board's own scripts, not in the engine it loads.
func TestVideoLint(t *testing.T) {
	src := `<script src="./support.js">setTimeout(</script>
<script type="text/x-dc" data-dc-script>class Component extends DCLogic {
  mount() { setInterval(() => this.tick(), 100); const n = Math.random(); const d = new Date(); }
}</script>`
	codes := func(issues []VideoCheckIssue) string {
		var out []string
		for _, i := range issues {
			out = append(out, i.Code+":"+string(i.Level))
			if i.Scene != "s1" || i.Format != nil || i.At != nil {
				t.Errorf("issue %+v", i)
			}
		}
		return strings.Join(out, ",")
	}
	if got := codes(videoLintSource("s1", src)); got != "timer:error,random:warn,clock:warn" {
		t.Errorf("lint = %s", got)
	}
	if got := codes(videoLintSource("s1", `<script src="x.js"></script><script>const d = new Date(2026, 1, 1); setTimeoutX()</script>`)); got != "" {
		t.Errorf("clean board = %s", got)
	}
}

// A moment at or past the scene's end is an error tied to its block and time.
func TestVideoPastEnd(t *testing.T) {
	sc := VideoScene{ID: "s1", Dur: 3000, Moments: []VideoMoment{{Block: "A", At: 1000}, {Block: "B", At: 3000}, {Block: "C", At: 4500}}}
	got := videoPastEnd(sc)
	if len(got) != 2 || *got[0].Block != "B" || *got[1].At != 4500 || got[1].Code != VideoCheckPastEnd || got[1].Level != VideoCheckError {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got[1].Msg, "4.5") {
		t.Errorf("msg = %q", got[1].Msg)
	}
}

func TestVideoFrameTimes(t *testing.T) {
	cases := []struct {
		at, strip string
		want      string
	}{
		{"1500", "", "1500"},
		{"", "0:1000:500", "0,500,1000"},
		{"", "0:950:500", "0,500"},
		{"-1", "", "bad"},
		{"", "", "bad"},
		{"10", "0:1000:500", "bad"},
		{"0", "0:1000:500", "0,500,1000"},
		{"", "1000:0:500", "bad"},
		{"", "0:1000:0", "bad"},
		{"", "0:100000:10", "bad"},
		{"", "0:1000", "bad"},
	}
	for _, c := range cases {
		ts, err := videoStripTimes(c.at, c.strip)
		got := "bad"
		if err == nil {
			var parts []string
			for _, t := range ts {
				parts = append(parts, strconv.FormatInt(t, 10))
			}
			got = strings.Join(parts, ",")
		}
		if got != c.want {
			t.Errorf("at=%q strip=%q: %s, want %s", c.at, c.strip, got, c.want)
		}
	}
}

// Frames are cached by the clip's key and t: a second ask draws nothing; a format the video lacks and a time past the
// scene's end are refused.
func TestVideoFrameFilesCache(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	writeFile(t, filepath.Join(root, "clip", ArtifactFile), `{"title":"Clip"}`)
	writeFile(t, filepath.Join(root, "clip", VideoSettingsFile), `{"v":1,"formats":["16:9"],"fps":30,"seconds":null,"voice":"none","subs":true}`)
	sc := VideoScene{ID: "s1", Path: "One.dc.html", Dur: 2000}
	swapVideoEngine(t, []VideoScene{sc})
	drawn := 0
	prevImpl := videoFramesImpl
	videoFramesImpl = func(_ *Library, _ context.Context, _ *deckTab, _ string, _ VideoScene, _ VideoFormat, _ bool, times []int64, each func(videoShot) error) error {
		for i, ts := range times {
			drawn++
			if err := each(videoShot{I: i, T: ts, Image: []byte("jpeg@" + strconv.FormatInt(ts, 10))}); err != nil {
				return err
			}
		}
		return nil
	}
	t.Cleanup(func() { videoFramesImpl = prevImpl })
	prevShooter := videoSharedFor
	videoSharedFor = func(*Library) (*deckShooter, func(), error) { return &deckShooter{}, func() {}, nil }
	t.Cleanup(func() { videoSharedFor = prevShooter })
	prevTab := videoTabFor
	videoTabFor = func(*deckShooter, context.Context) (*deckTab, func(), error) { return &deckTab{}, func() {}, nil }
	t.Cleanup(func() { videoTabFor = prevTab })

	files, err := l.VideoFrameFiles(context.Background(), "clip", "s1", VideoLandscape, true, []int64{0, 1000})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(files[1]); string(b) != "jpeg@1000" || !filepath.IsAbs(files[0]) {
		t.Fatalf("%v: %q", files, b)
	}
	if _, err := l.VideoFrameFiles(context.Background(), "clip", "s1", VideoLandscape, true, []int64{1000, 2000}); err != nil {
		t.Fatal(err)
	}
	if drawn != 3 {
		t.Errorf("drawn %d frames, want 3 (one was cached)", drawn)
	}
	if _, err := l.VideoFrameFiles(context.Background(), "clip", "s1", VideoPortrait, true, []int64{0}); err == nil {
		t.Error("a format the video lacks is drawn")
	}
	if _, err := l.VideoFrameFiles(context.Background(), "clip", "s1", VideoLandscape, true, []int64{2001}); err == nil {
		t.Error("a time past the scene's end is drawn")
	}
	if _, err := l.VideoFrameFiles(context.Background(), "clip", "nope", VideoLandscape, true, []int64{0}); err == nil {
		t.Error("an unknown scene is drawn")
	}
}

// swapVideoEngine gives the video the scenes and a key of the test's own.
func swapVideoEngine(t *testing.T, scenes []VideoScene) {
	t.Helper()
	prevScenes, prevKey := videoScenesFor, videoFrameKeyOf
	videoScenesFor = func(*Library, string) ([]VideoScene, error) { return scenes, nil }
	videoFrameKeyOf = func(_ *Library, _ string, sc VideoScene, f VideoFormat, draft bool) (string, error) {
		return sc.ID + "|" + string(f) + "|" + strconv.FormatBool(draft), nil
	}
	t.Cleanup(func() { videoScenesFor, videoFrameKeyOf = prevScenes, prevKey })
}

// The check in Chrome: setTimeout found by the lint, a text that overflows its box and a block off screen found at the
// scene's end, small text in 9:16 only, and a board driven by the wall clock found unstable.
func TestVideoCheckInChrome(t *testing.T) {
	g := newShootRig(t)
	root := g.l.ArtifactDir(g.id)
	writeFile(t, filepath.Join(root, ProjectDirname, "Bad.dc.html"), videoBoard(`.box{position:absolute;left:5cqw;top:10cqh;width:200px;height:40px;font-size:48px;white-space:nowrap;overflow:hidden}
.away{position:absolute;left:95cqw;top:50cqh;width:30cqw;height:10cqh;background:#c33}
.small{position:absolute;left:5cqw;top:70cqh;font-size:16px}`,
		`<section class="slide" data-block="Slide">
<div class="box" data-block="Box">A text far too long for its box</div>
<div class="away" data-block="Away"></div>
<p class="small" data-block="Small">fine print</p>
</section>`)+`<script>setTimeout(() => {}, 10)</script>`)
	writeFile(t, filepath.Join(root, ProjectDirname, "Wall.dc.html"), videoBoard(`.dot{position:absolute;top:40cqh;width:10cqh;height:10cqh;background:#36c}`,
		`<section class="slide" data-block="Slide"><div class="dot" data-block="Dot"></div></section>`)+
		`<script>let n = 0; (function move() { n++; const d = document.querySelector('[data-block="Dot"]'); if (d) d.style.left = (n % 50) * 2 + 'cqw'; setTimeout(move, 16); })();</script>`)
	scenes := []VideoScene{
		{ID: "bad", Path: "Bad.dc.html", Dur: 3000, Moments: []VideoMoment{{Block: "Late", At: 3500}}},
		{ID: "wall", Path: "Wall.dc.html", Dur: 3000},
	}
	swapVideoEngine(t, scenes)
	issues, err := g.l.VideoCheck(context.Background(), g.id, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, i := range issues {
		f := "*"
		if i.Format != nil {
			f = string(*i.Format)
		}
		b := ""
		if i.Block != nil {
			b = *i.Block
		}
		found[i.Scene+"/"+f+"/"+i.Code+"/"+b] = true
	}
	for _, want := range []string{
		"bad/*/timer/", "bad/*/past-end/Late",
		"bad/16:9/overflow/Box", "bad/16:9/offscreen/Away", "bad/9:16/small-text/Small",
		"wall/16:9/unstable/",
	} {
		if !found[want] {
			t.Errorf("not found: %s", want)
		}
	}
	if found["bad/16:9/small-text/Small"] {
		t.Error("small text is a finding in 16:9")
	}
	t.Logf("issues: %v", found)
	page := "bad"
	fm := VideoPortrait
	one, err := g.l.VideoCheck(context.Background(), g.id, &page, &fm)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range one {
		if i.Scene != "bad" || i.Format != nil && *i.Format != VideoPortrait {
			t.Errorf("a check of one scene in 9:16 found %+v", i)
		}
	}
}

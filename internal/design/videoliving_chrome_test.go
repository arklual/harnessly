package design

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The living art (HRN-652) in Chrome on the real engine: the sample scene (a hero walks in, waves, points at a chart that draws
// itself, cheers under confetti) is a function of time. A frame reached by seeking, in any order, is the frame reached by playing
// the scene through, and the check finds nothing wrong with it.

//go:embed testdata/video-living.dc.html
var videoLivingBoard string

const videoLivingTitle = "Living"

// The same scene with the other two heroes: one script of actions plays on any body.
var videoLivingHeroes = []string{"round", "robot"}

func init() {
	videoExtraScenes = append(videoExtraScenes, struct{ Title, Board string }{videoLivingTitle, videoLivingBoard})
	for _, hero := range videoLivingHeroes {
		videoExtraScenes = append(videoExtraScenes, struct{ Title, Board string }{videoLivingTitle + "-" + hero,
			strings.Replace(videoLivingBoard, "hero-person", "hero-"+hero, 1)})
	}
}

func (g *shootRig) livingScene(t *testing.T) VideoScene {
	t.Helper()
	return g.livingSceneOf(t, videoLivingTitle)
}

func (g *shootRig) livingSceneOf(t *testing.T, title string) VideoScene {
	t.Helper()
	for _, sc := range g.scenes {
		if sc.Title == title {
			sc.Dur = 9000
			return sc
		}
	}
	t.Fatal("no living scene")
	return VideoScene{}
}

func TestVideoLivingSceneIsAFunctionOfTime(t *testing.T) {
	g := newShootRig(t)
	sc := g.livingScene(t)
	s, err := g.l.newVideoShooter(g.id)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	// The runtime sees a healthy scene: nothing failed, no two tweens on one property, the motion ends inside the scene.
	tab, release, err := s.videoTab(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.videoOpen(g.id, sc, VideoLandscape, true); err != nil {
		release()
		t.Fatal(err)
	}
	eval := func(expr string) any {
		v, err := tab.eval(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return v
	}
	if raw, _ := json.Marshal(eval(`window.tkVideo.problems()`)); string(raw) != "[]" {
		t.Errorf("problems %s", raw)
	}
	if raw, _ := json.Marshal(eval(`window.tkVideo.audit({})`)); strings.Contains(string(raw), "motion-overlap") {
		t.Errorf("a hero's actions must not overlap: %s", raw)
	}
	info, _ := json.Marshal(eval(`window.tkVideo.info()`))
	var parsed struct {
		MotionDur int `json:"motionDur"`
	}
	_ = json.Unmarshal(info, &parsed)
	if parsed.MotionDur < 8000 || parsed.MotionDur > 9000 {
		t.Errorf("the motion ends at %d ms, the scene at 9000: %s", parsed.MotionDur, info)
	}
	// The drawings are on the page and the rig has built them.
	for _, sel := range []string{`.bg [data-layer]`, `.hero [data-part="mouth"]`, `.chart [data-part="bar"]`, `.fx [data-tk-particle]`} {
		if n, _ := eval(`document.querySelectorAll('` + sel + `').length`).(float64); n == 0 {
			t.Errorf("nothing at %s", sel)
		}
	}
	if msg, _ := eval(`(() => { try { tk.rig('.chart'); return ''; } catch (e) { return e.message; } })()`).(string); !strings.Contains(msg, "not a hero") {
		t.Errorf("rigging a thing says %q", msg)
	}
	release()

	// What is on the page at t, as numbers: where every element is and how visible. Seeking in any order gives the same.
	tab, release, err = s.videoTab(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := tab.videoOpen(g.id, sc, VideoLandscape, true); err != nil {
		t.Fatal(err)
	}
	type snap = map[string][]float64
	look := func(ms int64) snap {
		if _, err := tab.videoSeek(ms); err != nil {
			t.Fatal(err)
		}
		v, err := tab.eval(`(() => { const o = {}; let i = 0; for (const e of document.querySelectorAll('.slide *')) { const r = e.getBoundingClientRect(); const c = getComputedStyle(e);
  const shown = c.visibility !== 'hidden' && +c.opacity > 0;
  o[i++ + ':' + e.tagName + '.' + (e.getAttribute('data-part') || e.getAttribute('class') || '')] = shown ? [r.left, r.top, r.width, r.height, +c.opacity] : [0, 0, 0, 0, 0]; } return o; })()`)
		if err != nil {
			t.Fatal(err)
		}
		out := snap{}
		for k, row := range v.(map[string]any) {
			for _, n := range row.([]any) {
				out[k] = append(out[k], n.(float64))
			}
		}
		return out
	}
	same := func(a, b snap) string {
		for k, x := range a {
			for i := range x {
				if d := x[i] - b[k][i]; d > 0.06 || d < -0.06 {
					return fmt.Sprintf("%s [%d]: %.3f against %.3f", k, i, x[i], b[k][i])
				}
			}
		}
		return ""
	}
	var all []int64
	for ms := int64(0); ms < 9000; ms += 100 {
		all = append(all, ms)
	}
	all = append(all, 1650, 2150, 3830, 5530, 6020, 6610, 6930, 8990)
	want := map[int64]snap{}
	for _, ms := range all {
		if _, ok := want[ms]; !ok {
			want[ms] = look(ms)
		}
	}
	for i := len(all) - 1; i >= 0; i-- {
		if diff := same(want[all[i]], look(all[i])); diff != "" {
			t.Errorf("backwards: at %d ms %s", all[i], diff)
		}
	}
	for i := range all {
		ms := all[(i*7+3)%len(all)]
		if diff := same(want[ms], look(ms)); diff != "" {
			t.Errorf("scattered: at %d ms %s", ms, diff)
		}
	}
	// Something moves: the scene is not still.
	if diff := same(want[0], want[5530]); diff == "" {
		t.Error("the scene does not move")
	}

	// And the pictures: a frame sought to is the frame played through, as far as Chrome's raster is steady.
	var times []int64
	for ms := int64(0); ms < 9000; ms += 700 {
		times = append(times, ms)
	}
	times = append(times, 2150, 3830, 5530, 6610, 8990)
	shots := func(order []int64) map[int64][]byte {
		out := map[int64][]byte{}
		tb, rel, err := s.videoTab(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer rel()
		err = g.l.videoFrames(context.Background(), tb, g.id, sc, VideoLandscape, true, order, func(v videoShot) error {
			out[order[v.I]] = append([]byte(nil), v.Image...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	forward := shots(times)
	var scattered []int64
	for i := range times {
		scattered = append(scattered, times[(i*5+2)%len(times)])
	}
	for ms, img := range shots(scattered) {
		if !videoFramesClose(forward[ms], img) {
			t.Errorf("the frame at %d ms, sought to, differs from the one played through", ms)
		}
	}
}

// The check, as the person's tool runs it, has nothing to say about the sample scene, with any of the three heroes.
func TestVideoCheckOnTheLivingScene(t *testing.T) {
	g := newShootRig(t)
	titles := []string{videoLivingTitle}
	for _, hero := range videoLivingHeroes {
		titles = append(titles, videoLivingTitle+"-"+hero)
	}
	var scenes []VideoScene
	for _, title := range titles {
		scenes = append(scenes, g.livingSceneOf(t, title))
	}
	prev := videoScenesFor
	videoScenesFor = func(*Library, string) ([]VideoScene, error) { return scenes, nil }
	t.Cleanup(func() { videoScenesFor = prev })
	for _, title := range titles {
		f := VideoLandscape
		issues, err := g.l.VideoCheck(context.Background(), g.id, &title, &f)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range issues {
			if i.Level == VideoCheckError || i.Code == VideoCheckOverlap || i.Code == VideoCheckMotionEnd {
				t.Errorf("%s: the check says %s: %s", title, i.Code, i.Msg)
			}
		}
	}
}

// HRN652_OUT=<dir> writes the sample scene's frames there (one every 1/30 s, full size) for an MP4 and a look.
func TestVideoLivingSceneFrames(t *testing.T) {
	out := os.Getenv("HRN652_OUT")
	if out == "" {
		t.Skip("HRN652_OUT names no folder")
	}
	g := newShootRig(t)
	sc := g.livingScene(t)
	s, err := g.l.newVideoShooter(g.id)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	tab, release, err := s.videoTab(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	err = g.l.videoFrames(context.Background(), tab, g.id, sc, VideoLandscape, false, videoTimes(sc.Dur, 30), func(v videoShot) error {
		return os.WriteFile(filepath.Join(out, fmt.Sprintf("f%04d.png", v.I)), v.Image, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

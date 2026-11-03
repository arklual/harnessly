package design

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The motion contract in Chrome on the real engine: GSAP and a registered engine follow the scene's time in any
// order, and the check's readers see what the board did.

const videoKitTags = `<script src="/__lib/motion/gsap.min.js"></script><script src="/__lib/motion/DrawSVGPlugin.min.js"></script><script src="/__lib/motion/SplitText.min.js"></script><script src="/__lib/motion/tk.js"></script>`

func motionBoard(script, tpl string) string {
	return strings.Replace(videoBoard(`.ball{position:absolute;left:0;top:40vh;width:10vh;height:10vh;background:#36c}h1{margin:0}`, tpl),
		`</head>`, videoKitTags+`<script>`+script+`</script></head>`, 1)
}

func init() {
	videoExtraScenes = append(videoExtraScenes,
		struct{ Title, Board string }{"Gsap", motionBoard(`
window.__built = 0;
tkVideo.motion(function (root) {
  window.__built += 1;
  gsap.set('.ball', { x: 0 });
  gsap.to('.ball', { x: 400, duration: 2, ease: 'none', delay: 1 });
  tk.enter('.title', { at: 0.5, from: 'up' });
  tk.text('.title', { at: 1.5, by: 'words' });
  window.__rootSeen = root.tagName;
});`, `<section class="slide"><h1 class="title" data-block="Title" data-tk-mark="0.5">Два слова</h1><div class="ball" data-block="Ball" data-tk-mark="1"></div><svg><path class="p" d="M0 0L100 100"/></svg></section>`)},
		struct{ Title, Board string }{"Engine", motionBoard(`
window.__seeks = [];
window.__ready = false;
tkVideo.register({ name: 'canvas',
  ready: function () { return new Promise(function (ok) { setTimeout(function () { window.__ready = true; ok(); }, 50); }); },
  seek: function (ms) { window.__seeks.push(ms); document.querySelector('.ball').style.transform = 'translateX(' + ms / 10 + 'px)'; } });
`, `<section class="slide"><div class="ball" data-block="Ball"></div></section>`)},
		struct{ Title, Board string }{"Overlap", motionBoard(`
tkVideo.motion(function () {
  var tl = gsap.timeline();
  tl.to('.ball', { x: 100, duration: 1 }, 0);
  tl.to('.ball', { x: 300, duration: 1 }, 0.5);
});`, `<section class="slide"><div class="ball" data-block="Ball"></div></section>`)},
		struct{ Title, Board string }{"Broken", motionBoard(`
tkVideo.motion(function () { throw new Error('boom in build'); });
tkVideo.register({ name: 'bad', seek: function () { throw new Error('boom in seek'); } });
`, `<section class="slide"><img src="/__lib/motion/none.png" alt=""><div class="ball" data-block="Ball"></div></section>`)},
	)
}

func TestVideoMotionInChrome(t *testing.T) {
	g := newShootRig(t)
	s, err := g.l.newVideoShooter(g.id)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	scene := func(title string) VideoScene {
		for _, sc := range g.scenes {
			if sc.Title == title {
				return sc
			}
		}
		t.Fatalf("no scene %s", title)
		return VideoScene{}
	}
	open := func(title string) (*deckTab, func()) {
		tab, release, err := s.videoTab(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := tab.videoOpen(g.id, scene(title), VideoLandscape, true); err != nil {
			release()
			t.Fatal(err)
		}
		return tab, release
	}
	eval := func(tab *deckTab, expr string) any {
		v, err := tab.eval(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return v
	}
	ballX := func(tab *deckTab) float64 {
		v := eval(tab, `new DOMMatrix(getComputedStyle(document.querySelector('.ball')).transform).m41`)
		f, _ := v.(float64)
		return f
	}

	t.Run("gsap follows the time in any order", func(t *testing.T) {
		tab, release := open("Gsap")
		defer release()
		if built, _ := eval(tab, `window.__built`).(float64); built != 1 {
			t.Errorf("built %v times", built)
		}
		if got, _ := eval(tab, `window.__rootSeen`).(string); got != "MAIN" {
			t.Errorf("root %q", got)
		}
		for _, c := range []struct {
			ms   int64
			want float64
		}{{2000, 200}, {500, 0}, {3000, 400}, {1500, 100}, {3900, 400}, {1000, 0}, {2500, 300}} {
			if _, err := tab.videoSeek(c.ms); err != nil {
				t.Fatal(err)
			}
			if got := ballX(tab); got < c.want-0.5 || got > c.want+0.5 {
				t.Errorf("at %d ms the ball is at %v, want %v", c.ms, got, c.want)
			}
		}
		raw, _ := json.Marshal(eval(tab, `window.tkVideo.info()`))
		if !strings.Contains(string(raw), `"gsap"`) || !strings.Contains(string(raw), `"motionDur":3000`) {
			t.Errorf("info %s", raw)
		}
		if raw, _ := json.Marshal(eval(tab, `window.tkVideo.problems()`)); string(raw) != "[]" {
			t.Errorf("problems %s", raw)
		}
		moments, _ := json.Marshal(eval(tab, `window.tkVideo.moments()`))
		if !strings.Contains(string(moments), `"at":500`) || !strings.Contains(string(moments), `"at":1000`) {
			t.Errorf("moments %s", moments)
		}
		if _, err := tab.videoSeek(3900); err != nil {
			t.Fatal(err)
		}
		same, err := tab.videoSeek(3950)
		if err != nil {
			t.Fatal(err)
		}
		if !same {
			t.Error("past the motion's end nothing changes, but seek says something did")
		}
	})

	t.Run("a registered engine waits and draws", func(t *testing.T) {
		tab, release := open("Engine")
		defer release()
		if ok, _ := eval(tab, `window.__ready`).(bool); !ok {
			t.Error("the scene was ready before its engine")
		}
		if _, err := tab.videoSeek(1000); err != nil {
			t.Fatal(err)
		}
		if got := ballX(tab); got != 100 {
			t.Errorf("x %v", got)
		}
		if _, err := tab.videoSeek(250); err != nil {
			t.Fatal(err)
		}
		if got := ballX(tab); got != 25 {
			t.Errorf("x back %v", got)
		}
		if same, err := tab.videoSeek(250); err != nil || same {
			t.Errorf("a seek without an answer of false counts as a change: same=%v err=%v", same, err)
		}
	})

	t.Run("overlapping tweens are found", func(t *testing.T) {
		tab, release := open("Overlap")
		defer release()
		raw, _ := json.Marshal(eval(tab, `window.tkVideo.audit({})`))
		if !strings.Contains(string(raw), `"motion-overlap"`) || !strings.Contains(string(raw), `x at 0.50s`) {
			t.Errorf("audit %s", raw)
		}
	})

	t.Run("what breaks is reported", func(t *testing.T) {
		tab, release := open("Broken")
		defer release()
		if _, err := tab.videoSeek(100); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(eval(tab, `window.tkVideo.problems()`))
		for _, want := range []string{"boom in build", "boom in seek", `"asset"`} {
			if !strings.Contains(string(raw), want) {
				t.Errorf("problems lack %q: %s", want, raw)
			}
		}
	})
}

// The check's readers on the fixture: errors come out, and a healthy GSAP scene is clean.
func TestVideoCheckOnMotionScenes(t *testing.T) {
	g := newShootRig(t)
	prev := videoScenesFor
	videoScenesFor = func(*Library, string) ([]VideoScene, error) { return g.scenes, nil }
	t.Cleanup(func() { videoScenesFor = prev })
	check := func(title string) []VideoCheckIssue {
		f := VideoLandscape
		issues, err := g.l.VideoCheck(context.Background(), g.id, &title, &f)
		if err != nil {
			t.Fatal(err)
		}
		return issues
	}
	if got := check("Gsap"); len(got) != 0 {
		t.Errorf("a healthy scene: %+v", got)
	}
	has := func(issues []VideoCheckIssue, code string) bool {
		for _, i := range issues {
			if i.Code == code {
				return true
			}
		}
		return false
	}
	if got := check("Overlap"); !has(got, VideoCheckOverlap) {
		t.Errorf("overlap: %+v", got)
	}
	broken := check("Broken")
	for _, code := range []string{VideoCheckEngine, VideoCheckAsset} {
		if !has(broken, code) {
			t.Errorf("broken lacks %s: %+v", code, broken)
		}
	}
}

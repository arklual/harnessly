package design

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// A clip of a 3D scene becomes a scene of a video: the clip's frames and MP4 are copied into the video's project with
// a file that says where they came from (scene, camera, frames, what drew them), and a board plays the frames on the scene's time.

// Scene3DVideoResult is where the clip went.
type Scene3DVideoResult struct {
	Video  string        `json:"video"`
	Scene  string        `json:"scene"`
	Page   string        `json:"page"`
	Folder string        `json:"folder"` // under the video's project
	Origin Scene3DOrigin `json:"origin"`
	New    bool          `json:"new"`
}

// scene3dBoardTpl is the scene's board: a page like every video scene (support.js, the motion kit, the board in <x-dc>), whose
// engine draws the clip's frame for the scene's time on a canvas, from the time alone (a frame is a function of time).
const scene3dBoardTpl = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><script src="./support.js"></script>
<script src="/__lib/motion/gsap.min.js"></script><script src="/__lib/motion/tk.js"></script>
<script>
/* %s */
(function () {
  var N = %d, FPS = %s, BASE = %q, imgs = [], cv = null, g = null;
  function pad(n) { return ('0000' + n).slice(-4); }
  function load(i) {
    return new Promise(function (done) {
      var im = new Image();
      im.onload = im.onerror = function () { done(im); };
      im.src = BASE + 'f' + pad(i + 1) + '.jpg';
      imgs[i] = im;
    });
  }
  function canvas() { if (!cv) { cv = document.querySelector('canvas'); g = cv && cv.getContext('2d'); } return g; }
  tkVideo.register({
    name: 'scene3d',
    ready: function () { var p = []; for (var i = 0; i < N; i++) p.push(load(i)); return Promise.all(p); },
    seek: function (ms) {
      if (!canvas()) return;
      var i = Math.max(0, Math.min(N - 1, Math.floor(ms / 1000 * FPS)));
      if (imgs[i] && imgs[i].naturalWidth) g.drawImage(imgs[i], 0, 0, cv.width, cv.height);
    }
  });
})();
</script></head>
<body>
<x-dc>
<helmet><style>
html,body{margin:0;background:#000}
.slide{position:relative;width:100vw;height:100vh;overflow:hidden;background:#000;container-type:size}
.slide canvas{display:block;width:100%%;height:100%%;object-fit:contain}
</style></helmet>
<main class="board"><section class="slide"><canvas data-block="3D" width="%d" height="%d"></canvas></section></main>
</x-dc>
<script type="text/x-dc" data-dc-script>
class Component extends DCLogic { renderVals() { return {}; } }
</script>
</body></html>
`

// Scene3DClipToVideo puts a clip into a video document as a new scene, with its origin kept beside the frames. videoID empty:
// a new video document of the scene's name is made.
func (l *Library) Scene3DClipToVideo(id, key, videoID string, canEdit func(video string) bool) (Scene3DVideoResult, error) {
	dir, clip, err := l.Scene3DClipDir(id, key)
	if err != nil {
		return Scene3DVideoResult{}, err
	}
	created := false
	if videoID == "" {
		p, err := l.CreateProject(clip.Origin.Title + " · 3D")
		if err != nil {
			return Scene3DVideoResult{}, err
		}
		videoID, created = p.ID, true
		if err := l.InitDeck(videoID, DeckInit{Minutes: 1, Tone: DeckToneTalk, Script: true}); err != nil {
			return Scene3DVideoResult{}, err
		}
		if err := l.InitVideo(videoID, VideoInit{Formats: []VideoFormat{VideoLandscape}}); err != nil {
			return Scene3DVideoResult{}, err
		}
		l.AnnounceNew(videoID)
	} else {
		if canEdit != nil && !canEdit(videoID) {
			return Scene3DVideoResult{}, deckErr(403, "You may not change that video", "Нельзя менять это видео")
		}
		if err := l.requireVideo(videoID); err != nil {
			return Scene3DVideoResult{}, err
		}
	}
	cam := clip.Origin.Cam.N
	if cam == "" {
		cam = "free"
	}
	name := "3d-" + scene3dSafeName(cam) + "-" + key[:6]
	rel := filepath.ToSlash(filepath.Join("clips", name)) + "/"
	dest := filepath.Join(l.ProjectDir(videoID), "clips", name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return Scene3DVideoResult{}, err
	}
	names, _ := readNames(dir)
	for _, n := range names {
		if n == "clip.json" || !isRealFile(filepath.Join(dir, n)) {
			continue
		}
		if err := linkOrCopy(filepath.Join(dir, n), filepath.Join(dest, n)); err != nil {
			return Scene3DVideoResult{}, err
		}
	}
	origin := struct {
		Scene3DOrigin
		Frames  int     `json:"frames"`
		FPS     float64 `json:"fps"`
		Seconds float64 `json:"seconds"`
	}{clip.Origin, clip.Frames, clip.FPS, clip.Seconds}
	if err := os.WriteFile(filepath.Join(dest, "origin.json"), deckJSON(origin), 0o644); err != nil {
		return Scene3DVideoResult{}, err
	}
	title := fmt.Sprintf("3D · %s · %d–%d", cam, clip.Origin.Start, clip.Origin.End)
	page, err := l.CreatePage(videoID, PageSpec{Title: title})
	if err != nil {
		return Scene3DVideoResult{}, err
	}
	note := fmt.Sprintf("3D scene %q, camera %s, frames %d-%d step %d, %s, %s", clip.Origin.Title, cam, clip.Origin.Start, clip.Origin.End, clip.Origin.Step, clip.Origin.Kind, clip.Origin.Engine)
	board := fmt.Sprintf(scene3dBoardTpl, htmlComment(note), clip.Frames, strconv.FormatFloat(clip.FPS, 'f', 3, 64), rel, clip.Origin.Width, clip.Origin.Height)
	if err := os.WriteFile(filepath.Join(l.ProjectDir(videoID), pageFileOf(page.File)), []byte(board), 0o644); err != nil {
		return Scene3DVideoResult{}, err
	}
	l.afterPageChange(videoID)
	dur := json.RawMessage(strconv.FormatInt(videoClampDur(int64(clip.Seconds*1000+0.5)), 10))
	if _, err := l.SetVideoScene(videoID, page.ID, map[string]json.RawMessage{"dur": dur}); err != nil {
		return Scene3DVideoResult{}, err
	}
	return Scene3DVideoResult{Video: videoID, Scene: title, Page: page.ID, Folder: rel, Origin: clip.Origin, New: created}, nil
}

func htmlComment(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '-' || r == '>' || r == '<' || r == '*' || r == '/' {
			r = ' '
		}
		out = append(out, r)
	}
	return string(out)
}

func videoClampDur(ms int64) int64 { return min(max(ms, VideoSceneDurMin), VideoSceneDurMax) }

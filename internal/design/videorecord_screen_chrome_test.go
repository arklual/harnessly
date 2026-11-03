package design

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bitsSite counts the frames it is drawn in: a requestAnimationFrame counter shown as eight black or white squares in the
// corner, which survive the clip's compression.
const bitsSite = `<!doctype html><html><head><meta charset="utf-8"><style>
body{margin:0;background:#fff;font:40px sans-serif}
#bits{position:fixed;left:0;top:0;display:flex}#bits i{width:30px;height:30px;display:block;background:#fff}
</style></head><body><div id="bits"></div><button id="b" style="margin:140px 80px;font:inherit">Press</button>
<script>
var bits=document.getElementById('bits');for(var i=0;i<8;i++)bits.appendChild(document.createElement('i'));
var n=0;(function f(){n++;for(var i=0;i<8;i++)bits.children[i].style.background=(n>>i)&1?'#000':'#fff';requestAnimationFrame(f)})();
</script></body></html>`

// newScreenRig is a video project with no scenes yet behind a stand-in of the library server: the scene of this test is a
// plain board, so it needs no board engine.
func newScreenRig(t *testing.T) *shootRig {
	t.Helper()
	if testing.Short() {
		t.Skip("drives a browser")
	}
	root := t.TempDir()
	l := testLibrary(t, root)
	if l.findChrome() == "" {
		t.Skip("no browser on this machine")
	}
	id := "clip"
	writeFile(t, filepath.Join(root, id, ArtifactFile), `{"title":"Clip"}`)
	writeFile(t, filepath.Join(root, id, MetaDirname, MetaProjectFile), `{"v":1,"title":"Clip"}`)
	writeFile(t, filepath.Join(root, id, ProjectDirname, "deck-theme.css"), `:root{--deck-bg:#ffffff}`)
	writeFile(t, filepath.Join(root, id, VideoSettingsFile), `{"v":1,"formats":["16:9","9:16"],"fps":30,"seconds":null,"voice":"none","subs":true}`)
	srv := httptest.NewServer(videoStandIn(l))
	t.Cleanup(srv.Close)
	prev := libraryBase
	libraryBase = func(*Library) (string, error) { return srv.URL, nil }
	t.Cleanup(func() { libraryBase = prev })
	return &shootRig{l: l, id: id}
}

// The recording and the scene: every frame the scene asks for, in any order, is the clip's frame under that time.
func TestScreenSeeksTheClipFrameByFrame(t *testing.T) {
	recordTestShell(t)
	g := newScreenRig(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(bitsSite))
	}))
	defer site.Close()
	sc, err := ParseRecordScenario([]byte(`{"name": "fx", "url": "` + site.URL + `", "viewport": {"width": 640, "height": 360}, "scale": 2, "fps": 30,
 "steps": [{"do": "click", "on": "#b"}, {"do": "wait", "ms": 800}]}`))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Record(context.Background(), sc, RecordOptions{Dir: g.l.ProjectDir(g.id), CRF: 10, Preset: "ultrafast"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Dropped != 0 {
		t.Fatalf("dropped %d", res.Dropped)
	}
	board := `<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="deck-theme.css"></head>
<body style="margin:0"><main style="width:100vw;height:100vh"><div id="box" style="width:100%;height:100%"></div></main>
<script src="/__lib/motion/screen.js"></script>
<script>tkScreen(document.getElementById('box'), {src: 'records/fx.mp4', log: 'records/fx.json', frame: 'none', camera: false, cursor: false, intro: false, ring: false});</script>
</body></html>`
	pid := ULID(1_700_000_000_900, [10]byte{9})
	writeFile(t, filepath.Join(g.l.ProjectDir(g.id), "Screen.html"), board)
	writeFile(t, filepath.Join(g.l.ArtifactDir(g.id), MetaDirname, MetaPagesDirname, pid+".json"),
		`{"id":"`+pid+`","title":"Screen","file":"Screen.html","w":1920,"h":1080,"order":9}`)
	scene := VideoScene{ID: pid, Path: "Screen.html", Title: "Screen", Dur: 4000, Transition: VideoTransition{Kind: VideoTransitionNone}}

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
	if err := tab.videoOpen(g.id, scene, VideoLandscape, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(res.LogFile)
	var lg RecordLog
	if err := json.Unmarshal(raw, &lg); err != nil {
		t.Fatal(err)
	}
	// the frame's counter: the squares' centres on the canvas, which shows the whole page
	read := `async (ms) => {
  await window.tkVideo.seek(ms);
  const c = document.querySelector('canvas'), x = c.getContext('2d');
  let n = 0;
  for (let i = 0; i < 8; i++) {
    const d = x.getImageData(Math.round((i * 30 + 15) / 640 * c.width), Math.round(15 / 360 * c.height), 1, 1).data;
    if (d[0] < 128) n |= 1 << i;
  }
  return n;
}`
	count := func(frame int, fraction float64) int {
		v, err := tab.eval(fmt.Sprintf("(%s)(%f)", read, (float64(frame)+fraction)*1000/30))
		if err != nil {
			t.Fatal(err)
		}
		return int(v.(float64))
	}
	n0 := count(0, 0)
	order := []int{0, 1, 2, 3, 30, 29, 31, 60, 12, 13, 14, 15, 5, lg.Frames - 2, lg.Frames - 1, 20, lg.Frames - 1, 40, 41}
	for _, k := range order {
		if k >= lg.Frames {
			continue
		}
		want := (n0 + k) & 255
		if got := count(k, 0.3); got != want {
			t.Errorf("frame %d: the page's frame %d, want %d", k, got, want)
		}
	}
	if !strings.Contains(string(raw), `"click"`) {
		t.Fatal("no click in the log")
	}
}

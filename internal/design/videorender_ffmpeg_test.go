package design

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// probe is what ffprobe says of a file.
type probe struct {
	Streams []struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func probeOf(t *testing.T, ffprobe, file string) (w, h int, dur float64) {
	t.Helper()
	out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height:format=duration", "-of", "json", file).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var p probe
	if err := json.Unmarshal(out, &p); err != nil || len(p.Streams) == 0 {
		t.Fatalf("ffprobe said %s (%v)", out, err)
	}
	dur, _ = strconv.ParseFloat(p.Format.Duration, 64)
	return p.Streams[0].Width, p.Streams[0].Height, dur
}

// faststart: the moov box comes before the media data.
func hasFaststart(t *testing.T, file string) bool {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	moov, mdat := strings.Index(string(raw), "moov"), strings.Index(string(raw), "mdat")
	return moov >= 0 && mdat >= 0 && moov < mdat
}

// With the machine's ffmpeg, fake JPEGs become an MP4 of the right size and length in both formats, published whole;
// editing one scene draws that scene again and joins the others as they were.
func TestRenderMakesAnMP4WithFFmpeg(t *testing.T) {
	if testing.Short() {
		t.Skip("runs ffmpeg")
	}
	ffmpeg := DeckFFmpeg()
	ffprobe, err := exec.LookPath("ffprobe")
	if ffmpeg == "" || err != nil {
		t.Skip("no ffmpeg and ffprobe on this machine")
	}
	for _, withSubs := range []bool{false, true} {
		name := "no subtitles"
		if withSubs {
			name = "subtitles"
		}
		t.Run(name, func(t *testing.T) {
			if withSubs && !ffmpegHas(ffmpeg, "subtitles") {
				t.Skip("this ffmpeg cannot draw subtitles")
			}
			g := newRenderRig(t, withSubs)
			// Short scenes keep the encodes small.
			var at int64
			for i := range g.scenes {
				g.scenes[i].Start, g.scenes[i].Dur = at, 1000
				at += 1000
			}
			jpegs := map[[2]int][]byte{}
			for _, size := range [][2]int{{1920, 1080}, {1080, 1920}, {960, 540}, {540, 960}} {
				// A final render takes PNG frames, a draft JPEG ones.
				name := "f.png"
				if size[0] < 1000 && size[1] < 1000 {
					name = "f.jpg"
				}
				file := filepath.Join(t.TempDir(), name)
				out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i",
					"testsrc=size="+strconv.Itoa(size[0])+"x"+strconv.Itoa(size[1])+":rate=1", "-frames:v", "1", file).CombinedOutput()
				if err != nil {
					t.Fatalf("ffmpeg: %v %s", err, out)
				}
				if jpegs[size], err = os.ReadFile(file); err != nil {
					t.Fatal(err)
				}
			}
			videoFramesImpl = func(_ *Library, ctx context.Context, _ *deckTab, _ string, sc VideoScene, f VideoFormat, draft bool, times []int64, each func(videoShot) error) error {
				w, h := f.Size(draft)
				g.mu.Lock()
				g.drawn[sc.ID] += len(times)
				g.mu.Unlock()
				for i, at := range times {
					if err := each(videoShot{I: i, T: at, Image: jpegs[[2]int{w, h}], Same: i > 0}); err != nil {
						return err
					}
				}
				return nil
			}
			var total int64
			for _, sc := range g.scenes {
				total += sc.Dur
			}
			frame := 1.0 / 30
			for _, f := range []VideoFormat{VideoLandscape, VideoPortrait} {
				j, err := g.l.StartVideoRender("admin", VideoRenderRequest{Format: f})
				if err != nil {
					t.Fatal(err)
				}
				if done := waitVideoJob(t, g.l, "admin", j.ID); done.State != DeckJobDone {
					t.Fatalf("%s: %+v", f, done)
				}
				file := filepath.Join(g.l.publishedDir("admin"), VideoPublishedName(f))
				w, h, dur := probeOf(t, ffprobe, file)
				ew, eh := f.Size(false)
				if w != ew || h != eh {
					t.Errorf("%s is %dx%d, want %dx%d", f, w, h, ew, eh)
				}
				if want := float64(total) / 1000; dur < want-frame || dur > want+frame {
					t.Errorf("%s lasts %.3f s, want %.3f ± one frame", f, dur, want)
				}
				if !hasFaststart(t, file) {
					t.Errorf("%s is not faststart", f)
				}
			}
			// The published files.
			var chapters []DeckChapter
			if err := json.Unmarshal([]byte(readFileT(t, filepath.Join(g.l.publishedDir("admin"), "chapters.json"))), &chapters); err != nil || len(chapters) != 3 ||
				chapters[1].Page != g.scenes[1].ID || chapters[1].T != g.scenes[1].Start || chapters[2].Title != g.scenes[2].Title {
				t.Errorf("chapters %+v %v", chapters, err)
			}
			s, _ := g.l.ReadVideoSettings("admin")
			for _, in := range g.l.VideoRenders("admin", s, g.scenes) {
				if !in.Published || !in.Fresh || *in.DurMs != total {
					t.Errorf("after the render: %+v", in)
				}
			}
			// One scene edited: only it is drawn again, and the render is current again after.
			g.mu.Lock()
			g.drawn = map[string]int{}
			g.mu.Unlock()
			writeFile(t, g.board(t, 1), readFileT(t, g.board(t, 1))+"<!-- edit -->")
			if stale := g.l.VideoRenders("admin", s, g.scenes)[0]; stale.Fresh {
				t.Error("an edit left the render fresh")
			}
			j, _ := g.l.StartVideoRender("admin", VideoRenderRequest{Format: VideoLandscape})
			if done := waitVideoJob(t, g.l, "admin", j.ID); done.State != DeckJobDone {
				t.Fatalf("%+v", done)
			}
			g.mu.Lock()
			drawn := len(g.drawn)
			again := g.drawn[g.scenes[1].ID]
			g.mu.Unlock()
			if drawn != 1 || again != len(videoFrameTimes(g.scenes[1].Dur)) {
				t.Errorf("drawn again: %v", g.drawn)
			}
			if !g.l.VideoRenders("admin", s, g.scenes)[0].Fresh {
				t.Error("the render is not current after it")
			}
			// Three renders are kept per format.
			kept, _ := readNames(filepath.Join(g.root, "admin", filepath.FromSlash(VideoRendersDir), "16x9"))
			if len(kept) > VideoRendersKept {
				t.Errorf("%d renders kept: %v", len(kept), kept)
			}
		})
	}
}

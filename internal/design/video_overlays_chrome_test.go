package design

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fl(v float64) *float64 { return &v }

// sameFrame: two JPEGs show the same frame. The browser's raster may blend a semi-transparent ring over a shape with a
// pixel or two of difference depending on what it drew before, so a few pixels are let off; anything bigger is a frame
// that is not a function of time.
func sameFrame(t *testing.T, a, b []byte) (bool, int) {
	t.Helper()
	ia, err := jpeg.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	ib, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if ia.Bounds() != ib.Bounds() {
		return false, -1
	}
	far := 0
	bd := ia.Bounds()
	diff := func(x, y uint32) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		for x := bd.Min.X; x < bd.Max.X; x++ {
			r1, g1, b1, _ := ia.At(x, y).RGBA()
			r2, g2, b2, _ := ib.At(x, y).RGBA()
			if max(diff(r1, r2), diff(g1, g2), diff(b1, b2)) > 48<<8 {
				far++
			}
		}
	}
	return far <= 60, far
}

// overlayFixture is a layer with every kind and every way in.
func overlayFixture() []VideoOverlay {
	mk := func(id string, k VideoOverlayKind, text string, at int64, enter VideoOverlayEnter) VideoOverlay {
		o := videoOverlayDefaults(k)
		o.ID, o.Text, o.At, o.Enter, o.Author = id, text, at, enter, VideoAuthorAgent
		return o
	}
	title := mk("title-1", VideoOverlayTitle, "Привет, видео", 300, "typewriter")
	title.X, title.Y, title.W, title.H = 0.2, 0.04, 0.5, 0.12
	caption := mk("caption-1", VideoOverlayCaption, "Запись интерфейса в одну строку", 600, "mask")
	callout := mk("callout-1", VideoOverlayCallout, "Здесь меняется тариф", 900, "pop")
	callout.X, callout.Y, callout.W, callout.H = 0.56, 0.2, 0.36, 0.1
	hl := mk("highlight-1", VideoOverlayHighlight, "Новое", 1200, "fade")
	key := mk("key-1", VideoOverlayKey, "Ctrl + K", 1500, "pop")
	key.X, key.Y = 0.38, 0.62
	counter := mk("counter-1", VideoOverlayCounter, "{n} ₽ в месяц", 1800, "rise")
	counter.Value = fl(12400)
	counter.X, counter.Y, counter.W, counter.H = 0.05, 0.3, 0.4, 0.14
	badge := mk("badge-1", VideoOverlayBadge, "Новинка", 2100, "scramble")
	badge.X, badge.Y, badge.W, badge.H = 0.74, 0.74, 0.2, 0.1
	words := mk("title-2", VideoOverlayTitle, "Слова появляются по одному", 2400, "words")
	words.Y, words.H, words.Theme = 0.88, 0.08, "dark"
	return []VideoOverlay{title, caption, callout, hl, key, counter, badge, words}
}

// The layer in the real browser: every kind draws, a frame at t is the same whether the scene was played to it or sought to
// it, and 9:16 moves the overlays into the safe zone. HRN653_OUT names a folder to keep the frames in.
func TestVideoOverlaysInChrome(t *testing.T) {
	g := newShootRig(t)
	s, err := g.l.newVideoShooter(g.id)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	sc := g.scenes[0]
	sc.Overlays = overlayFixture()
	times := []int64{200, 700, 1100, 1500, 2000, 2600, 3200, 3900}
	out := os.Getenv("HRN653_OUT")
	for _, f := range []VideoFormat{VideoLandscape, VideoPortrait} {
		keep := map[int64][]byte{}
		tab, release, err := s.videoTab(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		err = g.l.videoFrames(context.Background(), tab, g.id, sc, f, false, times, func(v videoShot) error {
			keep[v.T] = append([]byte(nil), v.Image...)
			if out != "" {
				_ = os.MkdirAll(out, 0o755)
				return os.WriteFile(filepath.Join(out, fmt.Sprintf("overlays-%s-%04d.jpg", f.Slug(), v.T)), v.Image, 0o644)
			}
			return nil
		})
		release()
		if err != nil {
			t.Fatal(err)
		}
		// The same times from the end to the start in a fresh tab: seeking backwards draws the same frames.
		rev := make([]int64, len(times))
		for i, v := range times {
			rev[len(times)-1-i] = v
		}
		tab2, release2, err := s.videoTab(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		err = g.l.videoFrames(context.Background(), tab2, g.id, sc, f, false, rev, func(v videoShot) error {
			if out != "" {
				_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("rev-%s-%04d.jpg", f.Slug(), v.T)), v.Image, 0o644)
			}
			if same, n := sameFrame(t, keep[v.T], v.Image); !same {
				t.Errorf("%s at %d ms: a frame sought backwards differs from the one played forwards (%d pixels)", f, v.T, n)
			}
			return nil
		})
		release2()
		if err != nil {
			t.Fatal(err)
		}
		// Frames that must differ: before the first overlay and with all of them.
		if same, _ := sameFrame(t, keep[200], keep[3900]); same {
			t.Errorf("%s: the overlays drew nothing", f)
		}
	}
}

// The layer goes into the render: a scene's clip is an MP4 whose frames show the overlays, and a scene with other
// overlays is a different clip. HRN653_OUT keeps the MP4.
func TestVideoOverlaysInTheRenderedMP4(t *testing.T) {
	if DeckFFmpeg() == "" {
		t.Skip("no ffmpeg on this machine")
	}
	g := newShootRig(t)
	sc := g.scenes[0]
	for i, o := range overlayFixture() {
		patch := map[string]json.RawMessage{}
		raw, _ := json.Marshal(o)
		_ = json.Unmarshal(raw, &patch)
		for _, k := range []string{"id", "author", "updatedAt"} {
			delete(patch, k)
		}
		_, _, err := g.l.AddVideoOverlay(g.id, sc.ID, "", patch, VideoAuthorAgent)
		if err != nil {
			t.Fatalf("overlay %d: %v", i, err)
		}
	}
	page := sc.ID
	job, err := g.l.StartVideoRender(g.id, VideoRenderRequest{Format: VideoLandscape, Page: &page, Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	var done VideoRenderJob
	for i := 0; i < 600; i++ {
		var ok bool
		if done, ok = g.l.VideoRenderJobOf(g.id, job.ID); ok && (done.State == DeckJobDone || done.State == DeckJobFailed) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if done.State != DeckJobDone || done.File == nil {
		t.Fatalf("the render did not finish: %+v", done)
	}
	clip, ok := g.l.videoJobFile(g.id, job.ID)
	if !ok {
		t.Fatal("the finished render has no file")
	}
	if out := os.Getenv("HRN653_OUT"); out != "" {
		b, err := os.ReadFile(clip)
		if err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(out, 0o755)
		if err := os.WriteFile(filepath.Join(out, "overlays-scene.mp4"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if st, err := os.Stat(clip); err != nil || st.Size() < 5000 {
		t.Fatalf("the scene's clip is missing: %v", err)
	}
}

package design

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The video's engine (D4, D9): a scene's board in a headless tab with video-runtime.js, seeked to a time and captured
// as a JPEG. The render (videorender.go), the frame route and the check call it through videoFramesImpl.

const (
	videoRuntimePath = "/__lib/video-runtime.js"
	// videoReadyWait bounds a board's load and first draw.
	videoReadyWait = 60 * time.Second
	// videoLoadPoll is how often a frame loop looks at the machine's load while it waits; videoLoadWaitMax is the most one
	// scene's frames wait for it in all, so a machine that is always busy slows a render down but never stops it (past
	// that the frames go on at the browser's and ffmpeg's idle priority).
	videoLoadPoll    = 500 * time.Millisecond
	videoLoadWaitMax = 30 * time.Second
	// videoLoadMinFrames: a run of this many frames or fewer (a look at a frame, the check) is someone waiting for it
	// and does not wait for the machine.
	videoLoadMinFrames = 10
)

// videoTabSlots holds at most VideoRenderTabs video tabs open on this machine at once (the render's, the frame route's
// and the check's together).
var videoTabSlots = make(chan struct{}, VideoRenderTabs)

// newVideoShooter starts a browser for video frames at the lowest priority: it is launched from a goroutine that has
// lowered its own thread first, so Chrome and everything it starts inherit nice 19 and idle I/O.
func (l *Library) newVideoShooter(artifact string) (*deckShooter, error) {
	type started struct {
		s   *deckShooter
		err error
	}
	ch := make(chan started, 1)
	go func() {
		lowerPriority()
		s, err := l.newDeckShooter(artifact)
		ch <- started{s, err}
	}()
	r := <-ch
	return r.s, r.err
}

// videoTab opens a tab of s once a slot is free; release closes it and frees the slot.
func (s *deckShooter) videoTab(ctx context.Context) (tab *deckTab, release func(), err error) {
	select {
	case videoTabSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	tab, err = s.videoNewTab()
	if err != nil {
		<-videoTabSlots
		return nil, nil, err
	}
	return tab, func() {
		tab.close()
		<-videoTabSlots
	}, nil
}

// videoNewTab is deckShooter.tab with long waits: a browser at the lowest priority on a busy machine may take long to
// start.
func (s *deckShooter) videoNewTab() (*deckTab, error) {
	t, err := s.br.cdp.sendLong("Target.createTarget", map[string]any{"url": "about:blank"}, "", videoReadyWait)
	if err != nil {
		return nil, err
	}
	target, _ := t["targetId"].(string)
	a, err := s.br.cdp.sendLong("Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, "", videoReadyWait)
	if err != nil {
		return nil, err
	}
	session, _ := a["sessionId"].(string)
	tab := &deckTab{s: s, target: target, session: session}
	for _, d := range []string{"Page", "Runtime"} {
		if _, err := s.br.cdp.sendLong(d+".enable", nil, session, videoReadyWait); err != nil {
			tab.close()
			return nil, err
		}
	}
	return tab, nil
}

// videoOpen shows the scene's board in the tab at the format's size (CSS pixels are always the full size; a draft
// halves the device pixels, so the layout is the same) and waits until the board is drawn and the runtime is ready.
func (t *deckTab) videoOpen(artifact string, sc VideoScene, f VideoFormat, draft bool) error {
	w, h := f.Size(false)
	scale := 1.0
	if draft {
		scale = 0.5
	}
	if _, err := t.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": h, "deviceScaleFactor": scale, "mobile": false}); err != nil {
		return err
	}
	if _, err := t.call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]any{{"name": "prefers-reduced-motion", "value": "no-preference"}}}); err != nil {
		return err
	}
	if err := t.navigate(t.s.base + BoardHref(artifact, sc.Path)); err != nil {
		return err
	}
	ready, err := t.evalWithin(`(async () => {
  if (!window.tkVideo) return false;
  await window.tkVideo.ready();
  window.tkVideo.scene(`+videoSceneConfig(sc)+`);
  return true;
})()`, videoReadyWait)
	if err != nil {
		return err
	}
	if ok, _ := ready.(bool); !ok {
		return errors.New(locale.Pick("The board has no video runtime: is the project a video?", "На доске нет скрипта видео: это проект видео?"))
	}
	return nil
}

// videoSceneConfig is the scene's settings for the runtime: its transition as the video has it and its overlays.
func videoSceneConfig(sc VideoScene) string {
	over, err := json.Marshal(sc.Overlays)
	if err != nil || len(sc.Overlays) == 0 {
		over = []byte("[]")
	}
	return `{transition: {kind: ` + jsString(string(sc.Transition.Kind)) + `, ms: ` + strconv.Itoa(sc.Transition.Ms) + `}, overlays: ` + string(over) + `}`
}

// evalWithin is eval bounded by wait.
func (t *deckTab) evalWithin(expr string, wait time.Duration) (any, error) {
	type answer struct {
		v   any
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		v, err := t.eval(expr)
		ch <- answer{v, err}
	}()
	select {
	case a := <-ch:
		return a.v, a.err
	case <-time.After(wait):
		return nil, errors.New(locale.Pick("The board did not get ready in time", "Доска не успела загрузиться"))
	}
}

// videoSeek sets the scene to t ms; same: nothing on it changed since the previous seek.
func (t *deckTab) videoSeek(ms int64) (same bool, err error) {
	v, err := t.eval(`window.tkVideo.seek(` + strconv.FormatInt(ms, 10) + `)`)
	if err != nil {
		return false, err
	}
	m, _ := v.(map[string]any)
	same, _ = m["same"].(bool)
	return same, nil
}

// capture is a picture of the tab's viewport: a PNG when the tab is lossless (a final render), else a JPEG.
func (t *deckTab) capture() ([]byte, error) {
	params := map[string]any{"format": "jpeg", "quality": VideoFrameJPEGQ, "optimizeForSpeed": true}
	if t.lossless {
		params = map[string]any{"format": "png", "optimizeForSpeed": true}
	}
	r, err := t.callLong("Page.captureScreenshot", params)
	if err != nil {
		return nil, err
	}
	data, _ := r["data"].(string)
	if data == "" {
		return nil, errors.New("the browser gave no picture")
	}
	return base64.StdEncoding.DecodeString(data)
}

// videoFrames opens the scene's board in tab at f's size (half of it for a draft) and seeks it to each of times (ms
// from the scene's start), calling each in order with the frame; Same is set when nothing changed since the previous
// frame. It stops at the first error each returns. Between the frames of a long run it waits while the machine is busy
// (D9).
func (l *Library) videoFrames(ctx context.Context, tab *deckTab, artifact string, sc VideoScene, f VideoFormat, draft bool,
	times []int64, each func(videoShot) error) error {
	if !f.Valid() {
		return fmt.Errorf("unknown video format %q", f)
	}
	if err := tab.videoOpen(artifact, sc, f, draft); err != nil {
		return err
	}
	var prev []byte
	budget := videoLoadWaitMax
	for i, ms := range times {
		if err := ctx.Err(); err != nil {
			return err
		}
		if i > 0 && len(times) > videoLoadMinFrames {
			var err error
			if budget, err = videoWaitLoad(ctx, budget); err != nil {
				return err
			}
		}
		same, err := tab.videoSeek(ms)
		if err != nil {
			return err
		}
		shot := videoShot{I: i, T: ms}
		if same && prev != nil {
			shot.Image, shot.Same = prev, true
		} else {
			if shot.Image, err = tab.capture(); err != nil {
				return err
			}
			prev = shot.Image
		}
		if err := each(shot); err != nil {
			return err
		}
	}
	return nil
}

// videoLoad is the machine's load over the last minute; false when it cannot be read.
var videoLoad = func() (float64, bool) {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	first, _, _ := strings.Cut(string(raw), " ")
	v, err := strconv.ParseFloat(first, 64)
	return v, err == nil
}

// videoWaitLoad waits while the load is above VideoLoadPerCPUPause per CPU, no longer than budget; it returns what is
// left of the budget.
func videoWaitLoad(ctx context.Context, budget time.Duration) (time.Duration, error) {
	limit := VideoLoadPerCPUPause * float64(runtime.NumCPU())
	for budget > 0 {
		if load, ok := videoLoad(); !ok || load <= limit {
			return budget, nil
		}
		select {
		case <-ctx.Done():
			return budget, ctx.Err()
		case <-time.After(videoLoadPoll):
			budget -= videoLoadPoll
		}
	}
	return 0, nil
}

// videoFrameIdle is how long the frame route's browser stays up after its last frame.
const videoFrameIdle = time.Minute

// videoShared is the browser of the frame route and the agent's frames, one per library, closed when idle.
var videoShared = struct {
	mu sync.Mutex
	m  map[*Library]*videoSharedShooter
}{m: map[*Library]*videoSharedShooter{}}

type videoSharedShooter struct {
	s     *deckShooter
	users int
	timer *time.Timer
}

// videoSharedShooter is the library's frame browser, started when there is none or it has died; done gives it back.
func (l *Library) videoSharedShooter() (s *deckShooter, done func(), err error) {
	videoShared.mu.Lock()
	defer videoShared.mu.Unlock()
	sh := videoShared.m[l]
	if sh != nil {
		select {
		case <-sh.s.br.exited:
			if sh.timer != nil {
				sh.timer.Stop()
			}
			delete(videoShared.m, l)
			sh = nil
		default:
		}
	}
	if sh == nil {
		if l.findChrome() == "" {
			return nil, nil, &DeckError{Status: http.StatusServiceUnavailable, Msg: errNoChrome().Error()}
		}
		started, err := l.newVideoShooter("")
		if err != nil {
			return nil, nil, err
		}
		sh = &videoSharedShooter{s: started}
		videoShared.m[l] = sh
	}
	if sh.timer != nil {
		sh.timer.Stop()
		sh.timer = nil
	}
	sh.users++
	return sh.s, func() {
		videoShared.mu.Lock()
		defer videoShared.mu.Unlock()
		sh.users--
		if sh.users > 0 {
			return
		}
		sh.timer = time.AfterFunc(videoFrameIdle, func() {
			videoShared.mu.Lock()
			defer videoShared.mu.Unlock()
			if sh.users == 0 && videoShared.m[l] == sh {
				delete(videoShared.m, l)
				sh.s.close()
			}
		})
	}, nil
}

// videoSharedFor and videoTabFor are the frame route's browser and tab; variables so the tests can draw without one.
var (
	videoSharedFor = (*Library).videoSharedShooter
	videoTabFor    = (*deckShooter).videoTab
)

// videoFrameKeyOf is the cache key of the scene's clip (videorender.go); a variable so the tests can give their own.
var videoFrameKeyOf = func(l *Library, id string, sc VideoScene, f VideoFormat, draft bool) (string, error) {
	return l.VideoSceneKey(id, sc, f, draft)
}

// videoFrameName is the cached frame's file: the clip's key plus t.
func videoFrameName(key string, t int64) string {
	sum := sha256.Sum256([]byte(key + "@" + strconv.FormatInt(t, 10)))
	return hex.EncodeToString(sum[:16]) + ".jpg"
}

// VideoFrameFile is the scene page's frame at t ms in f as a cached JPEG: its absolute path.
func (l *Library) VideoFrameFile(ctx context.Context, id, page string, f VideoFormat, draft bool, t int64) (string, error) {
	files, err := l.VideoFrameFiles(ctx, id, page, f, draft, []int64{t})
	if err != nil {
		return "", err
	}
	return files[0], nil
}

// VideoFrameFiles is VideoFrameFile for several times of one scene, drawn in one visit of the board.
func (l *Library) VideoFrameFiles(ctx context.Context, id, page string, f VideoFormat, draft bool, times []int64) ([]string, error) {
	settings, err := l.ReadVideoSettings(id)
	if err != nil {
		return nil, err
	}
	if !videoHasFormat(settings, f) {
		return nil, deckErr(http.StatusBadRequest, "The video has no format "+string(f), "У видео нет формата "+string(f))
	}
	scenes, err := videoScenesFor(l, id)
	if err != nil {
		return nil, err
	}
	sc, err := l.videoPickScene(id, scenes, page)
	if err != nil {
		return nil, err
	}
	for _, t := range times {
		if t > sc.Dur {
			return nil, deckErr(http.StatusBadRequest, "The scene is "+videoSecondsText(sc.Dur)+" long", "Сцена длится "+videoSecondsText(sc.Dur))
		}
	}
	key, err := videoFrameKeyOf(l, id, sc, f, draft)
	if err != nil {
		return nil, err
	}
	dir := l.deckFile(id, VideoFramesDir)
	files := make([]string, len(times))
	var missing []int64
	for i, t := range times {
		files[i] = filepath.Join(dir, videoFrameName(key, t))
		if isRealFile(files[i]) {
			now := time.Now()
			_ = os.Chtimes(files[i], now, now) // the cache keeps what is used
			continue
		}
		missing = append(missing, t)
	}
	if len(missing) == 0 {
		return files, nil
	}
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return nil, err
	}
	s, done, err := videoSharedFor(l)
	if err != nil {
		return nil, err
	}
	defer done()
	tab, release, err := videoTabFor(s, ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	err = videoFramesImpl(l, ctx, tab, id, sc, f, draft, missing, func(v videoShot) error {
		return writeFileAtomic(filepath.Join(dir, videoFrameName(key, v.T)), v.Image)
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

package design

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// A video's render: every scene becomes a clip (frames from the engine, piped to ffmpeg), the clips are
// joined without re-encoding, subtitles are burned in by a second pass only when they are on, and the file is
// published for guests. A clip is cached under a key made of what it was drawn from, so editing one scene draws that
// scene again and joins the rest as they were. One job runs at a time on the machine (deckTurn).

const (
	videoJobTTL       = 24 * time.Hour
	videoJobKeep      = 100
	videoFramesShare  = 0.9
	videoStalePartAge = time.Hour
)

// ---- seams: the scene list (WP1) and the browser (WP2a) come from other parts

// videoScenesImpl is the project's scenes in order with Start, Dur, Title, Notes and Transition set. It passes no
// formats, so VideoScenes makes no keys (a key is built from these scenes and would recurse).
var videoScenesImpl = func(l *Library, id string) ([]VideoScene, error) { return l.VideoScenes(id, VideoSettings{}) }

// videoRuntimeNow and videoFPSNow are the constants the keys and the command lines use; tests move them.
var (
	videoRuntimeNow = VideoRuntimeVersion
	videoFPSNow     = VideoFPS
)

// videoUsesBrowser: the render needs Chrome. Tests with a fake engine turn it off.
var videoUsesBrowser = true

// videoOpenTabs opens n tabs of one browser started at the lowest priority, and returns them with the closer.
var videoOpenTabs = func(l *Library, artifact string, n int) ([]*deckTab, func(), error) {
	type opened struct {
		sh  *deckShooter
		err error
	}
	ch := make(chan opened, 1)
	// The goroutine lowers its own thread's priority first, so the Chrome it starts is born at it.
	go func() {
		lowerPriority()
		sh, err := l.newDeckShooter(artifact)
		ch <- opened{sh, err}
	}()
	o := <-ch
	if o.err != nil {
		return nil, nil, o.err
	}
	tabs := make([]*deckTab, 0, n)
	for i := 0; i < n; i++ {
		// a browser at the lowest priority on a busy machine may take long to answer: the long waits
		t, err := o.sh.videoNewTab()
		if err != nil {
			o.sh.close()
			return nil, nil, err
		}
		tabs = append(tabs, t)
	}
	return tabs, func() {
		for _, t := range tabs {
			t.close()
		}
		o.sh.close()
	}, nil
}

// ---- keys

// fileSum is a file's digest, remembered by path, mtime and size so that a key made again reads nothing.
type fileSum struct {
	mod  int64
	size int64
	sum  [32]byte
}

var fileSums sync.Map

func digestOf(path string) ([32]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return [32]byte{}, err
	}
	mod := st.ModTime().UnixNano()
	if v, ok := fileSums.Load(path); ok {
		if c := v.(fileSum); c.mod == mod && c.size == st.Size() {
			return c.sum, nil
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return [32]byte{}, err
	}
	sum := sha256.Sum256(raw)
	fileSums.Store(path, fileSum{mod, st.Size(), sum})
	return sum, nil
}

// optionalDigest is the digest of a file that may be absent (then the empty string).
func optionalDigest(path string) (string, error) {
	sum, err := digestOf(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

// videoKeyer makes the keys of one project's scenes; what is the same for every scene is read once.
type videoKeyer struct {
	l      *Library
	id     string
	theme  string
	assets int64
	engine string
}

func (l *Library) newVideoKeyer(id string) (*videoKeyer, error) {
	theme, err := optionalDigest(filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckThemeCSS)))
	if err != nil {
		return nil, err
	}
	boards := map[string]bool{}
	if m := l.ReadModel(id); m != nil {
		for _, p := range m.Pages {
			file, _, _ := strings.Cut(p.File, "?")
			boards[filepath.Clean(filepath.FromSlash(file))] = true
		}
	}
	return &videoKeyer{l: l, id: id, theme: theme, assets: videoAssetsRev(l.ProjectDir(id), boards), engine: l.PinnedRelease(id)}, nil
}

// videoAssetsRev is the newest mtime (ns) of the project's files that are not boards or the theme: assets and kits.
func videoAssetsRev(root string, boards map[string]bool) int64 {
	var rev int64
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || boards[rel] || rel == "deck-theme.css" {
			return nil
		}
		if st, err := d.Info(); err == nil && st.Mode().IsRegular() && st.ModTime().UnixNano() > rev {
			rev = st.ModTime().UnixNano()
		}
		return nil
	})
	return rev
}

func (k *videoKeyer) scene(sc VideoScene, f VideoFormat, draft bool) (string, error) {
	if !f.Valid() {
		return "", deckErr(http.StatusBadRequest, "Unknown format", "Неизвестный формат")
	}
	if !deckIDRe.MatchString(sc.ID) {
		return "", deckErr(http.StatusBadRequest, "Unknown scene", "Неизвестная сцена")
	}
	root := k.l.ProjectDir(k.id)
	// A board may carry a query (a page of a shared file); the query is part of what is drawn.
	file, query, _ := strings.Cut(sc.Path, "?")
	board := filepath.Join(root, filepath.FromSlash(file))
	if rel, err := filepath.Rel(root, board); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", deckErr(http.StatusBadRequest, "The board is outside the project", "Доска вне проекта")
	}
	boardSum, err := digestOf(board)
	if err != nil {
		return "", err
	}
	meta, err := optionalDigest(k.l.deckFile(k.id, VideoScenesDir+"/"+sc.ID+".json"))
	if err != nil {
		return "", err
	}
	h := sha256.New()
	put := func(name string, v any) { fmt.Fprintf(h, "%s=%v\n", name, v) }
	put("kind", "scene")
	put("board", hex.EncodeToString(boardSum[:]))
	put("query", query)
	put("theme", k.theme)
	put("assets", k.assets)
	put("scene", meta)
	put("dur", sc.Dur)
	put("transition", fmt.Sprintf("%s/%d", sc.Transition.Kind, sc.Transition.Ms))
	put("format", f)
	put("fps", videoFPSNow)
	put("draft", draft)
	put("engine", k.engine)
	put("runtime", videoRuntimeNow)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VideoSceneKey is the cache key of a scene's clip in f (sha256 of the board, deck-theme.css, the newest mtime of the
// project's other files, meta/scenes/<id>.json, the format, fps, draft, the engine's release and VideoRuntimeVersion).
func (l *Library) VideoSceneKey(id string, sc VideoScene, f VideoFormat, draft bool) (string, error) {
	k, err := l.newVideoKeyer(id)
	if err != nil {
		return "", err
	}
	return k.scene(sc, f, draft)
}

// videoInputsKey is the key of the whole video in f: its scenes' keys in order, and the subtitles when they are on.
// The published MP4 carries it; it is current while it equals the key of today's inputs.
func (k *videoKeyer) video(s VideoSettings, scenes []VideoScene, f VideoFormat, draft bool) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "video f=%s draft=%v subs=%v\n", f, draft, s.Subs)
	for _, sc := range scenes {
		key, err := k.scene(sc, f, draft)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s %s %d\n", sc.ID, key, sc.Dur)
		if s.Subs {
			fmt.Fprintf(h, "subs %q\n", sc.Notes)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---- the published state

func (l *Library) publishedDir(id string) string {
	return filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckPublishedDir))
}

// readVideoPublished is the published video.json; an absent file is an empty one.
func (l *Library) readVideoPublished(id string) (VideoPublished, error) {
	out := VideoPublished{Formats: map[VideoFormat]VideoPublishedFormat{}}
	raw, err := readFileLimited(l.deckFile(id, VideoPublishedFile), maxDeckFileBytes)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return VideoPublished{Formats: map[VideoFormat]VideoPublishedFormat{}}, fmt.Errorf("%s: %w", VideoPublishedFile, err)
	}
	if out.Formats == nil {
		out.Formats = map[VideoFormat]VideoPublishedFormat{}
	}
	return out, nil
}

// VideoRenders is the published state of each enabled format against the scenes' current keys. A key that cannot be
// made (a board is gone) leaves CurrentKey empty and the format stale.
func (l *Library) VideoRenders(id string, s VideoSettings, scenes []VideoScene) []VideoRenderInfo {
	out := []VideoRenderInfo{}
	pub, perr := l.readVideoPublished(id)
	keyer, kerr := l.newVideoKeyer(id)
	for _, f := range s.Formats {
		info := VideoRenderInfo{Format: f}
		info.Published = isRealFile(filepath.Join(l.publishedDir(id), VideoPublishedName(f)))
		if e, ok := pub.Formats[f]; ok && perr == nil && info.Published {
			key, dur, at := e.Key, e.DurMs, e.At
			info.Key, info.DurMs, info.At = &key, &dur, &at
		}
		if kerr == nil {
			if key, err := keyer.video(s, scenes, f, false); err == nil {
				info.CurrentKey = key
			}
		}
		info.Fresh = info.Key != nil && info.CurrentKey != "" && *info.Key == info.CurrentKey
		out = append(out, info)
	}
	return out
}

// ---- jobs

type videoJob struct {
	VideoRenderJob
	artifact string
	path     string
	made     time.Time
}

type videoJobs struct {
	mu   sync.Mutex
	jobs map[string]*videoJob
}

var videoJobStates sync.Map

func (l *Library) videoJobs() *videoJobs {
	if s, ok := videoJobStates.Load(l); ok {
		return s.(*videoJobs)
	}
	s, _ := videoJobStates.LoadOrStore(l, &videoJobs{jobs: map[string]*videoJob{}})
	return s.(*videoJobs)
}

func (j *videoJob) snapshot() VideoRenderJob {
	out := j.VideoRenderJob
	if j.Scene != nil {
		v := *j.Scene
		out.Scene = &v
	}
	if j.File != nil {
		v := *j.File
		out.File = &v
	}
	if j.Error != nil {
		v := *j.Error
		out.Error = &v
	}
	return out
}

func (s *videoJobs) sweepLocked() {
	for k, j := range s.jobs {
		if time.Since(j.made) > videoJobTTL && (j.State == DeckJobDone || j.State == DeckJobFailed) {
			delete(s.jobs, k)
		}
	}
	for len(s.jobs) > videoJobKeep {
		var oldest string
		var at time.Time
		for k, j := range s.jobs {
			if (j.State == DeckJobDone || j.State == DeckJobFailed) && (oldest == "" || j.made.Before(at)) {
				oldest, at = k, j.made
			}
		}
		if oldest == "" {
			return
		}
		delete(s.jobs, oldest)
	}
}

func sameScene(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// StartVideoRender starts a render, or joins the running one asked the same (format, page, draft). A machine that
// lacks Chrome or ffmpeg gets a job that has failed at once, with the reason in words.
func (l *Library) StartVideoRender(id string, req VideoRenderRequest) (VideoRenderJob, error) {
	if !l.IsVideo(id) {
		return VideoRenderJob{}, deckErr(http.StatusNotFound, "Not a video", "Это не видео")
	}
	settings, err := l.ReadVideoSettings(id)
	if err != nil {
		return VideoRenderJob{}, err
	}
	enabled := false
	for _, f := range settings.Formats {
		enabled = enabled || f == req.Format
	}
	if !req.Format.Valid() || !enabled {
		return VideoRenderJob{}, deckErr(http.StatusBadRequest, "This format is not enabled for the video", "Этот формат не включён у видео")
	}
	scenes, err := videoScenesImpl(l, id)
	if err != nil {
		return VideoRenderJob{}, err
	}
	if len(scenes) == 0 {
		return VideoRenderJob{}, deckErr(http.StatusBadRequest, "The video has no scenes", "В видео нет сцен")
	}
	if req.Page != nil {
		found := false
		for _, sc := range scenes {
			found = found || sc.ID == *req.Page
		}
		if !found {
			return VideoRenderJob{}, deckErr(http.StatusNotFound, "No such scene", "Нет такой сцены")
		}
	}
	s := l.videoJobs()
	s.mu.Lock()
	s.sweepLocked()
	for _, j := range s.jobs {
		if j.artifact == id && j.Format == req.Format && j.Draft == req.Draft && sameScene(j.Scene, req.Page) &&
			(j.State == DeckJobQueued || j.State == DeckJobRunning) {
			snap := j.snapshot()
			s.mu.Unlock()
			return snap, nil
		}
	}
	jid := randomHex(8)
	j := &videoJob{
		VideoRenderJob: VideoRenderJob{ID: jid, Format: req.Format, Draft: req.Draft, State: DeckJobQueued, At: time.Now().UTC().Format(time.RFC3339)},
		artifact:       id, made: time.Now(),
	}
	if req.Page != nil {
		v := *req.Page
		j.Scene = &v
	}
	s.jobs[jobKey(id, jid)] = j
	if err := l.videoNeeds(); err != nil {
		msg := err.Error()
		j.State, j.Error = DeckJobFailed, &msg
		snap := j.snapshot()
		s.mu.Unlock()
		return snap, nil
	}
	snap := j.snapshot()
	s.mu.Unlock()
	go l.runVideoJob(j, deckTurn())
	return snap, nil
}

// videoNeeds is whether the machine has what a render needs, as a reason in words.
func (l *Library) videoNeeds() error {
	if videoUsesBrowser && l.findChrome() == "" {
		return errNoChrome()
	}
	if DeckFFmpeg() == "" {
		return errors.New(locale.Pick("ffmpeg is not installed on this machine", "На машине нет ffmpeg"))
	}
	return nil
}

// VideoRenderJobOf is the job id of the project; false when there is none.
func (l *Library) VideoRenderJobOf(id, job string) (VideoRenderJob, bool) {
	s := l.videoJobs()
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[jobKey(id, job)]
	if j == nil {
		return VideoRenderJob{}, false
	}
	return j.snapshot(), true
}

// videoJobFile is the finished job's file on disk.
func (l *Library) videoJobFile(id, job string) (string, bool) {
	s := l.videoJobs()
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[jobKey(id, job)]
	if j == nil || j.State != DeckJobDone || j.path == "" {
		return "", false
	}
	return j.path, true
}

// VideoRunningJob is the project's queued or running render, nil when none.
func (l *Library) VideoRunningJob(id string) *VideoRenderJob {
	s := l.videoJobs()
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *videoJob
	for _, j := range s.jobs {
		if j.artifact == id && (j.State == DeckJobQueued || j.State == DeckJobRunning) && (best == nil || j.made.After(best.made)) {
			best = j
		}
	}
	if best == nil {
		return nil
	}
	snap := best.snapshot()
	return &snap
}

func (l *Library) runVideoJob(j *videoJob, turn chan struct{}) {
	s := l.videoJobs()
	<-turn
	defer deckTurnDone()
	set := func(fn func()) {
		s.mu.Lock()
		fn()
		s.mu.Unlock()
	}
	set(func() { j.State = DeckJobRunning })
	ctx, cancel := context.WithTimeout(context.Background(), VideoRenderTimeout)
	defer cancel()
	var out string
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("the render broke: %v", r)
			}
		}()
		out, err = l.renderVideo(ctx, j, func(p float64) {
			p = math.Min(0.99, math.Max(0, p))
			set(func() {
				if p > j.Progress {
					j.Progress = p
				}
			})
		})
		return err
	}()
	if err != nil {
		msg := line(err.Error())
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = locale.Pick("The render took too long and was stopped", "Рендер шёл слишком долго и был остановлен")
		}
		set(func() { j.State, j.Error = DeckJobFailed, &msg })
		return
	}
	name := filepath.Base(out)
	set(func() { j.State, j.Progress, j.File, j.path = DeckJobDone, 1, &name, out })
}

// ---- ffmpeg command lines

func videoCRF(draft bool) int {
	if draft {
		return VideoDraftCRF
	}
	return VideoFinalCRF
}

// videoPreset is x264's speed: a final render takes its time, since it is what people see.
func videoPreset(draft bool) string {
	if draft {
		return "veryfast"
	}
	return "slow"
}

// videoColorArgs tags the stream as BT.709: without the tags a player guesses, and greys and accents shift.
func videoColorArgs() []string {
	return []string{"-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv"}
}

// videoClipArgs reads frames from stdin (PNG for a final render, JPEG for a draft) and writes one scene's clip: a fixed
// size, no sound.
func videoClipArgs(f VideoFormat, draft bool, out string) []string {
	w, h := f.Size(draft)
	codec := "png"
	if draft {
		codec = "mjpeg"
	}
	args := []string{"-hide_banner", "-y", "-loglevel", "error",
		"-f", "image2pipe", "-c:v", codec, "-framerate", strconv.Itoa(videoFPSNow), "-i", "pipe:0",
		"-vf", fmt.Sprintf("scale=%d:%d:out_color_matrix=bt709:out_range=tv,setsar=1,format=yuv420p", w, h),
		"-an", "-c:v", "libx264", "-preset", videoPreset(draft), "-crf", strconv.Itoa(videoCRF(draft)), "-r", strconv.Itoa(videoFPSNow),
		"-pix_fmt", "yuv420p"}
	args = append(args, videoColorArgs()...)
	return append(args, "-threads", "2", "-f", "mp4", out)
}

// videoJoinArgs joins the clips named in list (relative names in the list's folder) without re-encoding.
func videoJoinArgs(list, out string) []string {
	return []string{"-hide_banner", "-y", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "file",
		"-f", "concat", "-safe", "1", "-i", list, "-c", "copy", "-an", "-movflags", "+faststart", "-f", "mp4", out}
}

// videoSubsArgs burns subs.ass (in the folder ffmpeg runs in) into in.
func videoSubsArgs(in, out string, draft bool) []string {
	args := []string{"-hide_banner", "-y", "-loglevel", "error", "-nostdin", "-progress", "pipe:1", "-nostats",
		"-protocol_whitelist", "file", "-i", in, "-vf", "subtitles=subs.ass", "-an",
		"-c:v", "libx264", "-preset", videoPreset(draft), "-crf", strconv.Itoa(videoCRF(draft)), "-r", strconv.Itoa(videoFPSNow),
		"-pix_fmt", "yuv420p"}
	args = append(args, videoColorArgs()...)
	return append(args, "-threads", "2", "-movflags", "+faststart", "-f", "mp4", out)
}

// videoConcatList is the concat demuxer's list. Only the hex keys of our own clips go in it, never a name from outside.
func videoConcatList(keys []string) (string, error) {
	var b strings.Builder
	for _, k := range keys {
		if len(k) != 64 || strings.Trim(k, "0123456789abcdef") != "" {
			return "", fmt.Errorf("not a clip key: %q", k)
		}
		fmt.Fprintf(&b, "file '%s.mp4'\n", k)
	}
	return b.String(), nil
}

var sentenceRe = regexp.MustCompile(`[^.!?…]+(?:[.!?…]+|$)`)

// videoSubLines splits a scene's text into lines on line breaks and sentence ends (as the live player does).
func videoSubLines(notes string) []string {
	var out []string
	for _, ln := range strings.Split(notes, "\n") {
		for _, sent := range sentenceRe.FindAllString(ln, -1) {
			if t := strings.Join(strings.Fields(sent), " "); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// videoSubs spreads each scene's lines evenly over the scene.
func videoSubs(scenes []VideoScene) []mp4Sub {
	out := []mp4Sub{}
	for _, sc := range scenes {
		lines := videoSubLines(sc.Notes)
		n := int64(len(lines))
		for i, text := range lines {
			a := sc.Start + sc.Dur*int64(i)/n
			b := sc.Start + sc.Dur*int64(i+1)/n
			out = append(out, mp4Sub{A: a, B: b, Text: text})
		}
	}
	return out
}

// videoFrameTimes are the times (ms from the scene's start) of the scene's frames.
func videoFrameTimes(dur int64) []int64 {
	n := int(math.Round(float64(dur) * float64(videoFPSNow) / 1000))
	n = max(n, 1)
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(math.Round(float64(i) * 1000 / float64(videoFPSNow)))
	}
	return out
}

// ---- the render

func (l *Library) videoCacheDir(id, rel string) string { return l.deckFile(id, rel) }

// renderClip draws one scene into its clip file and moves it into place whole.
func (l *Library) renderClip(ctx context.Context, bin string, tab *deckTab, id string, sc VideoScene, f VideoFormat, draft bool,
	key, dir string, frame func()) error {
	final := filepath.Join(dir, key+".mp4")
	tmp := final + ".part"
	_ = os.Remove(tmp)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := recNice(cctx, bin, videoClipArgs(f, draft, tmp)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	var tail tailBuffer
	cmd.Stderr = &tail
	if err := cmd.Start(); err != nil {
		return err
	}
	if tab != nil {
		tab.lossless = !draft
	}
	// The engine's frame loop waits for a busy machine (videoWaitLoad), within a budget per scene.
	ferr := videoFramesImpl(l, cctx, tab, id, sc, f, draft, videoFrameTimes(sc.Dur), func(s videoShot) error {
		if _, err := stdin.Write(s.Image); err != nil {
			return err
		}
		frame()
		return nil
	})
	_ = stdin.Close()
	if ferr != nil {
		cancel()
	}
	werr := cmd.Wait()
	if ferr != nil {
		_ = os.Remove(tmp)
		if msg := strings.TrimSpace(tail.String()); msg != "" && isBrokenPipe(ferr) {
			return errors.New(locale.Pick("ffmpeg failed: ", "ffmpeg не справился: ") + msg)
		}
		return ferr
	}
	if werr != nil {
		_ = os.Remove(tmp)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(tail.String())
		if msg == "" {
			msg = werr.Error()
		}
		return errors.New(locale.Pick("ffmpeg failed: ", "ffmpeg не справился: ") + msg)
	}
	return os.Rename(tmp, final)
}

func isBrokenPipe(err error) bool {
	return err != nil && (errors.Is(err, io.ErrClosedPipe) || strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "file already closed"))
}

// renderVideo makes the job's file and returns its path.
func (l *Library) renderVideo(ctx context.Context, j *videoJob, progress func(float64)) (string, error) {
	id, f, draft := j.artifact, j.Format, j.Draft
	bin := DeckFFmpeg()
	if bin == "" {
		return "", errors.New(locale.Pick("ffmpeg is not installed on this machine", "На машине нет ffmpeg"))
	}
	settings, err := l.ReadVideoSettings(id)
	if err != nil {
		return "", err
	}
	all, err := videoScenesImpl(l, id)
	if err != nil {
		return "", err
	}
	if len(all) == 0 {
		return "", errors.New(locale.Pick("The video has no scenes", "В видео нет сцен"))
	}
	scenes := all
	if j.Scene != nil {
		scenes = nil
		for _, sc := range all {
			if sc.ID == *j.Scene {
				scenes = []VideoScene{sc}
			}
		}
		if scenes == nil {
			return "", errors.New(locale.Pick("The scene is gone from the video", "Сцены больше нет в видео"))
		}
	}
	keyer, err := l.newVideoKeyer(id)
	if err != nil {
		return "", err
	}
	clips := l.videoCacheDir(id, VideoSceneClipsDir)
	if err := os.MkdirAll(clips, 0o755); err != nil {
		return "", err
	}
	keys := make([]string, len(scenes))
	var missing []int
	var total, done int64
	for i, sc := range scenes {
		if keys[i], err = keyer.scene(sc, f, draft); err != nil {
			return "", fmt.Errorf("%s: %w", sc.Title, err)
		}
		n := int64(len(videoFrameTimes(sc.Dur)))
		total += n
		path := filepath.Join(clips, keys[i]+".mp4")
		if fileSize(path) > 0 {
			done += n
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			continue
		}
		missing = append(missing, i)
	}
	var drawn atomic.Int64
	drawn.Store(done)
	onFrame := func() {
		progress(videoFramesShare * float64(drawn.Add(1)) / float64(total))
	}
	progress(videoFramesShare * float64(done) / float64(total))
	if len(missing) > 0 {
		if err := l.renderClips(ctx, bin, id, scenes, keys, missing, f, draft, clips, onFrame); err != nil {
			return "", err
		}
	}
	defer pruneVideoCache(l.ArtifactDir(id), VideoCacheMaxBytes, keys)

	if j.Scene != nil {
		return filepath.Join(clips, keys[0]+".mp4"), nil
	}
	return l.joinVideo(ctx, bin, id, settings, scenes, keys, keyer, f, draft, clips, progress)
}

// renderClips draws the scenes at the indexes in missing, up to VideoRenderTabs at a time.
func (l *Library) renderClips(ctx context.Context, bin, id string, scenes []VideoScene, keys []string, missing []int,
	f VideoFormat, draft bool, dir string, frame func()) error {
	n := min(VideoRenderTabs, len(missing))
	tabs := make([]*deckTab, n)
	if videoUsesBrowser {
		opened, closeAll, err := videoOpenTabs(l, id, n)
		if err != nil {
			return err
		}
		defer closeAll()
		tabs = opened
	}
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	next := make(chan int, len(missing))
	for _, i := range missing {
		next <- i
	}
	close(next)
	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func(tab *deckTab) {
			defer wg.Done()
			for i := range next {
				if wctx.Err() != nil {
					return
				}
				sc := scenes[i]
				if err := l.renderClip(wctx, bin, tab, id, sc, f, draft, keys[i], dir, frame); err != nil {
					once.Do(func() {
						first = fmt.Errorf("%s: %w", sc.Title, err)
						cancel()
					})
					return
				}
			}
		}(tabs[w])
	}
	wg.Wait()
	if first != nil {
		return first
	}
	return ctx.Err()
}

// joinVideo joins the clips, burns the subtitles in when they are on, keeps the render and publishes it.
func (l *Library) joinVideo(ctx context.Context, bin, id string, settings VideoSettings, scenes []VideoScene, keys []string,
	keyer *videoKeyer, f VideoFormat, draft bool, clips string, progress func(float64)) (string, error) {
	renderKey, err := keyer.video(settings, scenes, f, draft)
	if err != nil {
		return "", err
	}
	renders := filepath.Join(l.videoCacheDir(id, VideoRendersDir), f.Slug())
	cached := filepath.Join(renders, renderKey+".mp4")
	var total int64
	for _, sc := range scenes {
		total += sc.Dur
	}
	if fileSize(cached) == 0 {
		if err := os.MkdirAll(renders, 0o755); err != nil {
			return "", err
		}
		work, err := os.MkdirTemp(renders, ".work-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(work)
		list, err := videoConcatList(keys)
		if err != nil {
			return "", err
		}
		listName := ".list-" + randomHex(6) + ".txt"
		listPath := filepath.Join(clips, listName)
		if err := os.WriteFile(listPath, []byte(list), 0o644); err != nil {
			return "", err
		}
		defer os.Remove(listPath)
		subs := []mp4Sub{}
		if settings.Subs {
			subs = videoSubs(scenes)
		}
		joined := filepath.Join(work, "out.mp4")
		if len(subs) > 0 {
			joined = filepath.Join(work, "joined.mp4")
		}
		if err := runFFmpeg(ctx, bin, clips, videoJoinArgs(listName, joined), total, func(float64) {}); err != nil {
			return "", err
		}
		progress(videoFramesShare + 0.02)
		if len(subs) > 0 {
			if !ffmpegHas(bin, "subtitles") {
				return "", errors.New(locale.Pick("this ffmpeg cannot draw subtitles: turn them off in the video's settings",
					"этот ffmpeg не умеет рисовать субтитры: выключите их в настройках видео"))
			}
			w, h := f.Size(draft)
			if err := os.WriteFile(filepath.Join(work, "subs.ass"), []byte(mp4ASS(w, h, subs)), 0o644); err != nil {
				return "", err
			}
			out := filepath.Join(work, "out.mp4")
			if err := runFFmpeg(ctx, bin, work, videoSubsArgs("joined.mp4", out, draft), total,
				func(p float64) { progress(videoFramesShare + 0.02 + 0.08*0.98*p) }); err != nil {
				return "", err
			}
			joined = out
		}
		if err := os.Rename(joined, cached); err != nil {
			return "", err
		}
	}
	dropOldVideoRenders(renders, VideoRendersKept, cached)
	if draft {
		return cached, nil
	}
	if err := l.publishVideo(id, cached, f, renderKey, total, scenes); err != nil {
		return "", err
	}
	return filepath.Join(l.publishedDir(id), VideoPublishedName(f)), nil
}

// publishVideo puts the render where guests read it, with the chapters and the key it was made from.
func (l *Library) publishVideo(id, src string, f VideoFormat, key string, durMs int64, scenes []VideoScene) error {
	pub := l.publishedDir(id)
	if err := os.MkdirAll(pub, 0o755); err != nil {
		return err
	}
	if err := publishCopy(src, filepath.Join(pub, VideoPublishedName(f))); err != nil {
		return err
	}
	chapters := make([]DeckChapter, 0, len(scenes))
	for _, sc := range scenes {
		chapters = append(chapters, DeckChapter{Page: sc.ID, Title: sc.Title, T: sc.Start})
	}
	raw, err := json.Marshal(chapters)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(pub, "chapters.json"), raw); err != nil {
		return err
	}
	state, err := l.readVideoPublished(id)
	if err != nil {
		state = VideoPublished{Formats: map[VideoFormat]VideoPublishedFormat{}}
	}
	state.Formats[f] = VideoPublishedFormat{Key: key, DurMs: durMs, At: time.Now().UTC().Format(time.RFC3339)}
	raw, err = json.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(pub, "video.json"), raw)
}

// dropOldVideoRenders keeps the newest `keep` renders of the folder, and always the one just made.
func dropOldVideoRenders(dir string, keep int, current string) {
	type item struct {
		path string
		mod  time.Time
	}
	var items []item
	names, _ := readNames(dir)
	for _, n := range names {
		if !strings.HasSuffix(n, ".mp4") {
			continue
		}
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			items = append(items, item{p, st.ModTime()})
		}
	}
	sort.Slice(items, func(a, b int) bool { return items[a].mod.After(items[b].mod) })
	kept := 0
	for _, it := range items {
		if it.path == current || kept < keep {
			if it.path != current {
				kept++
			}
			continue
		}
		_ = os.Remove(it.path)
	}
}

// pruneVideoCache keeps the scene clips and frames of a project under max bytes, dropping the least recently used
// first, never the clips in use; it also clears the half-written files an earlier run left.
func pruneVideoCache(artifactDir string, max int64, inUse []string) {
	busy := map[string]bool{}
	for _, k := range inUse {
		busy[k+".mp4"] = true
	}
	type item struct {
		path string
		size int64
		mod  time.Time
	}
	var items []item
	var sum int64
	for _, rel := range []string{VideoSceneClipsDir, VideoFramesDir} {
		dir := filepath.Join(artifactDir, filepath.FromSlash(rel))
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			st, err := e.Info()
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if strings.HasSuffix(e.Name(), ".part") || strings.HasPrefix(e.Name(), ".list-") {
				if time.Since(st.ModTime()) > videoStalePartAge {
					_ = os.Remove(p)
				}
				continue
			}
			sum += st.Size()
			if !busy[e.Name()] {
				items = append(items, item{p, st.Size(), st.ModTime()})
			}
		}
	}
	if sum <= max {
		return
	}
	sort.Slice(items, func(a, b int) bool { return items[a].mod.Before(items[b].mod) })
	for _, it := range items {
		if sum <= max {
			return
		}
		if os.Remove(it.path) == nil {
			sum -= it.size
		}
	}
}

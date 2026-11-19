package design

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A clip of a 3D scene: the frames of one camera over a stretch of the animation, as an MP4 with the frames beside
// it for a video scene to draw from. A draft is every Nth frame drawn small on this machine (the frames are the draft frames of
// scene3d.go, cached); a final is the frames a rented GPU rendered (scene3d_final.go). Every clip says where it came from.

const (
	Scene3DClipMaxFrames = 24
	scene3dClipsDir      = "clips"
)

// Scene3DClipSpec asks for a clip: a camera of the scene (or a free view), the frames Start..End taking every Step-th.
type Scene3DClipSpec struct {
	Cam   Scene3DCam `json:"cam"`
	Start int        `json:"start"`
	End   int        `json:"end"`
	Step  int        `json:"step"`
}

// Scene3DOrigin says where a clip came from: the scene, its model's hash, the camera, the frames, what drew them and where.
type Scene3DOrigin struct {
	Project   string     `json:"project"`
	Title     string     `json:"title"`
	Hash      string     `json:"hash"`
	Cam       Scene3DCam `json:"cam"`
	Start     int        `json:"start"`
	End       int        `json:"end"`
	Step      int        `json:"step"`
	Kind      string     `json:"kind"` // draft or final
	Engine    string     `json:"engine,omitempty"`
	Where     string     `json:"where,omitempty"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	CreatedAt string     `json:"createdAt"`
	// Outputs is the set of outputs a generated clip came from; Generator is what the generator wrote of it (its origin file).
	Outputs   string          `json:"outputs,omitempty"`
	Generator json.RawMessage `json:"generator,omitempty"`
}

// Scene3DClip is a clip as the page lists it.
type Scene3DClip struct {
	Key     string        `json:"key"`
	Status  string        `json:"status"` // queued, building, ready, failed
	Done    int           `json:"done"`
	Total   int           `json:"total"`
	Frames  int           `json:"frames,omitempty"`
	FPS     float64       `json:"fps,omitempty"`
	Seconds float64       `json:"seconds,omitempty"`
	Error   string        `json:"error,omitempty"`
	Origin  Scene3DOrigin `json:"origin"`
}

type s3Clip struct {
	clip Scene3DClip
}

var s3Clips = map[string]*s3Clip{} // under s3Reg: clip folder -> running or failed clip

func (l *Library) scene3dClipDir(id, hash, key string) string {
	return filepath.Join(l.scene3dCache(id, hash), scene3dClipsDir, key)
}

func scene3dClipKey(hash string, sp Scene3DClipSpec, kind string) string {
	raw, _ := json.Marshal(sp)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%dx%d", hash, kind, raw, Scene3DFrameW, Scene3DFrameH)))
	return hex.EncodeToString(sum[:])[:20]
}

// scene3dClipFrames are the frames a spec takes: Start, Start+Step, … and always End.
func scene3dClipFrames(sp Scene3DClipSpec) []int {
	var out []int
	for f := sp.Start; f <= sp.End; f += sp.Step {
		out = append(out, f)
	}
	if len(out) == 0 || out[len(out)-1] != sp.End {
		out = append(out, sp.End)
	}
	return out
}

// Scene3DClipCheck normalises a spec against the scene's description: a step that keeps the clip within the draft's frame cap
// when none is given.
func Scene3DClipCheck(sp Scene3DClipSpec, meta *Scene3DMeta, maxFrames int) (Scene3DClipSpec, error) {
	cam, err := Scene3DNormCam(sp.Cam, meta)
	if err != nil {
		return sp, err
	}
	sp.Cam = cam
	if sp.Start < meta.Start || sp.End > meta.End || sp.Start > sp.End {
		return sp, deckErr(http400, "start..end: frames within the scene's range, start first", "start..end: кадры в пределах сцены, начало раньше конца")
	}
	if sp.Step <= 0 {
		sp.Step = max(1, (sp.End-sp.Start+maxFrames-1)/max(maxFrames-1, 1))
	}
	if n := len(scene3dClipFrames(sp)); n > maxFrames {
		return sp, deckErr(http400, "Too many frames for a draft: "+strconv.Itoa(n)+" (at most "+strconv.Itoa(maxFrames)+"): raise step", "Слишком много кадров для черновика: "+strconv.Itoa(n)+" (не больше "+strconv.Itoa(maxFrames)+"): увеличьте step")
	}
	return sp, nil
}

// StartScene3DClip draws a draft animation: every Step-th frame of the camera, one after another through the draft queue, then
// assembled. Answers at once: ready from the cache, or the running clip.
func (l *Library) StartScene3DClip(id string, sp Scene3DClipSpec, retry bool) (Scene3DClip, error) {
	if err := l.requireScene3D(id); err != nil {
		return Scene3DClip{}, err
	}
	_, _, hash, err := l.scene3dTarget(id)
	if err != nil {
		return Scene3DClip{}, deckErr(http422, err.Error(), err.Error())
	}
	meta := readScene3DMeta(l.scene3dCache(id, hash))
	if meta == nil {
		return Scene3DClip{}, deckErr(http409, "The model is not built yet", "Модель ещё не собрана")
	}
	if DeckFFmpeg() == "" {
		return Scene3DClip{}, deckErr(http409, "ffmpeg is not installed on this machine", "На машине нет ffmpeg")
	}
	sp, err = Scene3DClipCheck(sp, meta, Scene3DClipMaxFrames)
	if err != nil {
		return Scene3DClip{}, err
	}
	key := scene3dClipKey(hash, sp, "draft")
	dir := l.scene3dClipDir(id, hash, key)
	if c, ok := readScene3DClip(dir); ok {
		return c, nil
	}
	title := id
	if a := l.ReadArtifact(id); a != nil && a.Title != "" {
		title = a.Title
	}
	origin := Scene3DOrigin{Project: id, Title: title, Hash: hash, Cam: sp.Cam, Start: sp.Start, End: sp.End, Step: sp.Step, Kind: "draft",
		Engine: scene3dEngine(), Width: Scene3DFrameW, Height: Scene3DFrameH}
	frames := scene3dClipFrames(sp)
	s3Reg.Lock()
	defer s3Reg.Unlock()
	if j := s3Clips[dir]; j != nil && !(retry && j.clip.Status == Scene3DFailed) {
		return j.clip, nil
	}
	j := &s3Clip{clip: Scene3DClip{Key: key, Status: Scene3DQueued, Total: len(frames), Origin: origin}}
	s3Clips[dir] = j
	s3Touch()
	s3Wg.Add(1)
	go func() {
		defer s3Wg.Done()
		l.scene3dMakeClip(id, dir, sp, frames, meta, j)
	}()
	return j.clip, nil
}

func (l *Library) scene3dMakeClip(id, dir string, sp Scene3DClipSpec, frames []int, meta *Scene3DMeta, j *s3Clip) {
	set := func(f func(c *Scene3DClip)) {
		s3Reg.Lock()
		f(&j.clip)
		s3Touch()
		s3Reg.Unlock()
	}
	fail := func(err error) {
		set(func(c *Scene3DClip) { c.Status, c.Error = Scene3DFailed, err.Error() })
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	set(func(c *Scene3DClip) { c.Status = Scene3DBuilding })
	tmp := dir + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		fail(err)
		return
	}
	defer os.RemoveAll(tmp)
	where := ""
	for i, f := range frames {
		fr, file, err := l.scene3dFrameWait(ctx, id, sp.Cam, f, 8*time.Minute)
		if err != nil {
			fail(fmt.Errorf("frame %d: %w", f, err))
			return
		}
		if fr.Where != "" {
			where = fr.Where
		}
		if err := linkOrCopy(file, filepath.Join(tmp, fmt.Sprintf("f%04d.jpg", i+1))); err != nil {
			fail(err)
			return
		}
		set(func(c *Scene3DClip) { c.Done = i + 1 })
	}
	fps := meta.FPS / float64(max(sp.Step, 1))
	if err := scene3dAssemble(ctx, tmp, fps, meta.FPS); err != nil {
		fail(err)
		return
	}
	origin := j.clip.Origin
	origin.Where, origin.CreatedAt = where, time.Now().UTC().Format(time.RFC3339)
	clip := Scene3DClip{Key: j.clip.Key, Status: Scene3DReady, Done: len(frames), Total: len(frames), Frames: len(frames), FPS: fps,
		Seconds: float64(len(frames)) / fps, Origin: origin}
	if err := os.WriteFile(filepath.Join(tmp, "clip.json"), deckJSON(clip), 0o600); err != nil {
		fail(err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		fail(err)
		return
	}
	if err := os.Rename(tmp, dir); err != nil && !isRealFile(filepath.Join(dir, "clip.json")) {
		fail(err)
		return
	}
	s3Reg.Lock()
	delete(s3Clips, dir)
	s3Touch()
	s3Reg.Unlock()
}

// scene3dAssemble makes clip.mp4 of f0001.jpg… in dir: the frames taken at `fps` and played at `play` fps, so the clip lasts
// as the scene does though only every Nth frame was drawn.
func scene3dAssemble(ctx context.Context, dir string, fps, play float64) error {
	bin := DeckFFmpeg()
	if bin == "" {
		return errors.New("ffmpeg is not installed on this machine")
	}
	_ = play
	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-framerate", strconv.FormatFloat(fps, 'f', 3, 64), "-i", filepath.Join(dir, "f%04d.jpg"),
		"-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "26", "-movflags", "+faststart", filepath.Join(dir, "clip.mp4")}
	argv := scene3dNice(append([]string{bin}, args...))
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, s3Tail(string(out), 4))
	}
	return nil
}

func scene3dNice(argv []string) []string {
	if p, err := exec.LookPath("nice"); err == nil {
		return append([]string{p, "-n", "10"}, argv...)
	}
	return argv
}

func readScene3DClip(dir string) (Scene3DClip, bool) {
	raw, err := readFileLimited(filepath.Join(dir, "clip.json"), 64*1024)
	if err != nil || !isRealFile(filepath.Join(dir, "clip.mp4")) {
		return Scene3DClip{}, false
	}
	var c Scene3DClip
	if json.Unmarshal(raw, &c) != nil || c.Key == "" {
		return Scene3DClip{}, false
	}
	return c, true
}

// scene3dClips lists the clips of the current model: the ready ones and those still going or failed.
func (l *Library) scene3dClips(id, hash string) []Scene3DClip {
	root := filepath.Join(l.scene3dCache(id, hash), scene3dClipsDir)
	out := []Scene3DClip{}
	names, _ := readNames(root)
	for _, n := range names {
		if c, ok := readScene3DClip(filepath.Join(root, n)); ok {
			out = append(out, c)
		}
	}
	s3Reg.Lock()
	for dir, j := range s3Clips {
		if filepath.Dir(dir) == root {
			out = append(out, j.clip)
		}
	}
	s3Reg.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Origin.CreatedAt > out[j].Origin.CreatedAt || out[i].Key < out[j].Key
	})
	return out
}

// Scene3DClipFile is a ready clip's folder (with clip.mp4 and the frames) for a key, in the current model's cache or any older.
func (l *Library) Scene3DClipDir(id, key string) (string, Scene3DClip, error) {
	if err := l.requireScene3D(id); err != nil {
		return "", Scene3DClip{}, err
	}
	if !frameKeyRe.MatchString(key) {
		return "", Scene3DClip{}, deckErr(http400, "Not a clip key", "Это не ключ клипа")
	}
	root := l.deckFile(id, Scene3DCacheDir)
	names, _ := readNames(root)
	for _, n := range names {
		dir := filepath.Join(root, n, scene3dClipsDir, key)
		if c, ok := readScene3DClip(dir); ok {
			return dir, c, nil
		}
	}
	return "", Scene3DClip{}, deckErr(http404, "No such clip", "Нет такого клипа")
}

// scene3dSafeName keeps a name to letters, digits and a few marks, for the files of a board.
func scene3dSafeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

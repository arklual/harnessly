package design

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// «Send to a generator»: the key frames of a set of outputs go to a video generator (a provider the machine has, such
// as Higgsfield, in internal/mediagen) and the clip that comes back is put beside the scene's clips, with where it came from. A
// generation costs money, so it starts only with the person's confirmation after a price; the agent's tools cannot start one.
// The generator itself is the runtime's to give (SetScene3DGenerator); without one the page says there is none.

// Scene3DGenRequest is what the scene gives a generator.
type Scene3DGenRequest struct {
	Model   string
	Prompt  string
	Frames  []string // absolute paths: the first is the first frame, the second (if the model takes it) the last
	Seconds int
	MaxUSD  float64
	Owner   string
	CopyTo  string
	Origin  map[string]any
	// SaveOK and FrameOK say which paths the generator may write to and read from.
	SaveOK  func(string) bool
	FrameOK func(string) bool
}

// Scene3DGenVideo is a clip a generator made: its file, the copy in CopyTo and the origin file beside it.
type Scene3DGenVideo struct {
	Path       string
	Copy       string
	OriginFile string
}

// Scene3DGenJob is a generation as the generator reports it.
type Scene3DGenJob struct {
	ID          string
	State       string // queued, running, done, failed, canceled
	EstimateUSD float64
	Error       string
	Videos      []Scene3DGenVideo
}

// Scene3DGenerator is a video generator the runtime provides.
type Scene3DGenerator interface {
	Estimate(ctx context.Context, r Scene3DGenRequest) (usd float64, note string, err error)
	Start(r Scene3DGenRequest) (Scene3DGenJob, error)
	Get(id string) (Scene3DGenJob, bool)
}

var scene3dGenerator Scene3DGenerator

// SetScene3DGenerator is how the runtime gives the 3D scene its video generator.
func SetScene3DGenerator(g Scene3DGenerator) { scene3dGenerator = g }

// Scene3DGeneration is one generation of a scene, as the page lists it.
type Scene3DGeneration struct {
	ID          string  `json:"id"`
	Outputs     string  `json:"outputs"`
	Model       string  `json:"model"`
	Prompt      string  `json:"prompt"`
	State       string  `json:"state"`
	EstimateUSD float64 `json:"estimateUsd"`
	Error       string  `json:"error,omitempty"`
	Clip        string  `json:"clip,omitempty"` // the clip's key once it was taken in
	StartedAt   string  `json:"startedAt"`
}

// Scene3DGenParams is what the person chooses for a generation.
type Scene3DGenParams struct {
	Outputs string  `json:"outputs"`
	Model   string  `json:"model"`
	Prompt  string  `json:"prompt"`
	Seconds int     `json:"seconds"`
	Source  string  `json:"source"` // beauty or clay: which pictures are the frames
	MaxUSD  float64 `json:"maxUsd"`
}

var s3Gens = struct {
	sync.Mutex
	list   map[string]*Scene3DGeneration // by id
	taking map[string]bool
}{list: map[string]*Scene3DGeneration{}, taking: map[string]bool{}}

func (l *Library) scene3dGenRequest(id string, p Scene3DGenParams) (Scene3DGenRequest, string, error) {
	if scene3dGenerator == nil {
		return Scene3DGenRequest{}, "", deckErr(http409, "This machine has no video generator: connect a provider (Higgsfield) first", "На машине нет генератора видео: сначала подключите провайдера (Higgsfield)")
	}
	dir, m, err := l.Scene3DOutputsFolder(id, p.Outputs)
	if err != nil {
		return Scene3DGenRequest{}, "", err
	}
	if p.Source == "" {
		p.Source = "beauty"
	}
	var frames []string
	for _, k := range m.Keyframes {
		name := k.Beauty
		if p.Source == "clay" {
			name = k.Clay
		}
		if name != "" {
			frames = append(frames, filepath.Join(dir, name))
		}
	}
	if len(frames) == 0 {
		return Scene3DGenRequest{}, "", deckErr(http409, "These outputs have no "+p.Source+" frames", "В этих выходах нет кадров «"+p.Source+"»")
	}
	if len(frames) > 2 {
		frames = []string{frames[0], frames[len(frames)-1]} // the first frame and the last
	}
	prompt, _ := CleanText(p.Prompt, 1500)
	if strings.TrimSpace(prompt) == "" {
		return Scene3DGenRequest{}, "", deckErr(http400, "Say in a sentence what should happen between the frames (prompt)", "Скажите одной фразой, что должно происходить между кадрами (prompt)")
	}
	if p.Seconds <= 0 {
		p.Seconds = 5
	}
	copyTo := filepath.Join(dir, "generated")
	inside := func(root string) func(string) bool {
		return func(real string) bool {
			rel, err := filepath.Rel(root, real)
			return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
		}
	}
	origin := map[string]any{"scene": id, "title": m.Scene.Title, "hash": m.Scene.Hash, "camera": m.Camera, "outputs": p.Outputs,
		"manifest": filepath.Join(dir, Scene3DManifestFile), "source": p.Source, "frames": []int{m.Keyframes[0].Frame, m.Keyframes[len(m.Keyframes)-1].Frame}}
	return Scene3DGenRequest{Model: p.Model, Prompt: prompt, Frames: frames, Seconds: p.Seconds, MaxUSD: p.MaxUSD, Owner: "design:" + id, CopyTo: copyTo,
		Origin: origin, SaveOK: inside(copyTo), FrameOK: inside(dir)}, dir, nil
}

// Scene3DGenEstimate is the generator's price of a generation. Nothing is started.
func (l *Library) Scene3DGenEstimate(ctx context.Context, id string, p Scene3DGenParams) (float64, string, error) {
	req, _, err := l.scene3dGenRequest(id, p)
	if err != nil {
		return 0, "", err
	}
	return scene3dGenerator.Estimate(ctx, req)
}

// StartScene3DGen starts a generation. confirm must be true: it spends money.
func (l *Library) StartScene3DGen(id string, p Scene3DGenParams, confirm bool) (Scene3DGeneration, error) {
	if !confirm {
		return Scene3DGeneration{}, deckErr(http409, "A generation spends money: it starts only when the person confirms the price", "Генерация тратит деньги: она стартует только когда человек подтвердил цену")
	}
	if err := l.requireScene3D(id); err != nil {
		return Scene3DGeneration{}, err
	}
	req, dir, err := l.scene3dGenRequest(id, p)
	if err != nil {
		return Scene3DGeneration{}, err
	}
	if err := os.MkdirAll(req.CopyTo, 0o700); err != nil {
		return Scene3DGeneration{}, err
	}
	_ = dir
	job, err := scene3dGenerator.Start(req)
	if err != nil {
		return Scene3DGeneration{}, deckErr(http422, err.Error(), err.Error())
	}
	g := &Scene3DGeneration{ID: job.ID, Outputs: p.Outputs, Model: p.Model, Prompt: req.Prompt, State: job.State, EstimateUSD: job.EstimateUSD,
		StartedAt: time.Now().UTC().Format(time.RFC3339)}
	s3Gens.Lock()
	s3Gens.list[job.ID] = g
	s3Gens.Unlock()
	s3Reg.Lock()
	s3Touch()
	s3Reg.Unlock()
	return *g, nil
}

// scene3dGens refreshes this scene's generations from the generator, and takes a finished clip in once.
func (l *Library) scene3dGens(id string) []Scene3DGeneration {
	out := []Scene3DGeneration{}
	if scene3dGenerator == nil {
		return out
	}
	s3Gens.Lock()
	var ids []string
	for gid, g := range s3Gens.list {
		if strings.HasPrefix(g.Outputs, "") && l.scene3dOwns(id, g) {
			ids = append(ids, gid)
		}
	}
	s3Gens.Unlock()
	for _, gid := range ids {
		job, ok := scene3dGenerator.Get(gid)
		s3Gens.Lock()
		g := s3Gens.list[gid]
		if ok {
			g.State, g.EstimateUSD, g.Error = job.State, job.EstimateUSD, job.Error
		}
		take := ok && job.State == "done" && g.Clip == "" && !s3Gens.taking[gid] && len(job.Videos) > 0
		if take {
			s3Gens.taking[gid] = true
		}
		out = append(out, *g)
		s3Gens.Unlock()
		if take {
			v := job.Videos[0]
			s3Wg.Add(1)
			go func() {
				defer s3Wg.Done()
				l.scene3dTakeGenerated(id, gid, v)
			}()
		}
	}
	return out
}

// scene3dOwns: the generation belongs to this scene (its outputs are in this scene's folders).
func (l *Library) scene3dOwns(id string, g *Scene3DGeneration) bool {
	_, _, err := l.Scene3DOutputsFolder(id, g.Outputs)
	return err == nil
}

// scene3dTakeGenerated makes a clip of a generator's video: its frames as JPEGs for a video scene, the MP4, and where it came from
// (the scene's side, and the generator's own origin file, kept as it is).
func (l *Library) scene3dTakeGenerated(id, gid string, v Scene3DGenVideo) {
	finish := func(clip, errText string) {
		s3Gens.Lock()
		if g := s3Gens.list[gid]; g != nil {
			g.Clip = clip
			if errText != "" {
				g.Error = errText
			}
		}
		delete(s3Gens.taking, gid)
		s3Gens.Unlock()
		s3Reg.Lock()
		s3Touch()
		s3Reg.Unlock()
	}
	src := v.Copy
	if src == "" {
		src = v.Path
	}
	bin := DeckFFmpeg()
	if bin == "" || !isRealFile(src) {
		finish("", "the generated clip cannot be taken in: no file or no ffmpeg")
		return
	}
	_, _, hash, err := l.scene3dTarget(id)
	if err != nil {
		finish("", err.Error())
		return
	}
	s3Gens.Lock()
	g := s3Gens.list[gid]
	outKey := ""
	if g != nil {
		outKey = g.Outputs
	}
	s3Gens.Unlock()
	_, m, err := l.Scene3DOutputsFolder(id, outKey)
	if err != nil {
		finish("", err.Error())
		return
	}
	key := scene3dClipKey(hash, Scene3DClipSpec{Cam: m.Camera, Start: m.Range[0], End: m.Range[1], Step: 1}, "gen:"+gid)
	dir := l.scene3dClipDir(id, hash, key)
	tmp := dir + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		finish("", err.Error())
		return
	}
	defer os.RemoveAll(tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	const playFPS = 12
	argv := scene3dNice([]string{bin, "-y", "-hide_banner", "-loglevel", "error", "-i", src, "-vf", fmt.Sprintf("fps=%d,scale='min(960,iw)':-2", playFPS), "-frames:v", "150", "-q:v", "4", filepath.Join(tmp, "f%04d.jpg")})
	if o, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput(); err != nil {
		finish("", "ffmpeg: "+err.Error()+": "+s3Tail(string(o), 3))
		return
	}
	if err := linkOrCopy(src, filepath.Join(tmp, "clip.mp4")); err != nil {
		finish("", err.Error())
		return
	}
	names, _ := readNames(tmp)
	n := 0
	for _, f := range names {
		if strings.HasSuffix(f, ".jpg") {
			n++
		}
	}
	if n == 0 {
		finish("", "the generated clip has no frames")
		return
	}
	var extra json.RawMessage
	if v.OriginFile != "" {
		if raw, err := readFileLimited(v.OriginFile, 256*1024); err == nil && json.Valid(raw) {
			extra = raw
		}
	}
	title := m.Scene.Title
	origin := Scene3DOrigin{Project: id, Title: title, Hash: hash, Cam: m.Camera, Start: m.Range[0], End: m.Range[1], Step: 1, Kind: "generated", Engine: "generated",
		Where: "generator", Width: min(960, m.Size[0]), Height: min(960, m.Size[0]) * m.Size[1] / max(m.Size[0], 1), CreatedAt: time.Now().UTC().Format(time.RFC3339), Generator: extra, Outputs: outKey}
	clip := Scene3DClip{Key: key, Status: Scene3DReady, Done: n, Total: n, Frames: n, FPS: playFPS, Seconds: float64(n) / playFPS, Origin: origin}
	if err := os.WriteFile(filepath.Join(tmp, "clip.json"), deckJSON(clip), 0o600); err != nil {
		finish("", err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		finish("", err.Error())
		return
	}
	if err := os.Rename(tmp, dir); err != nil && !isRealFile(filepath.Join(dir, "clip.json")) {
		finish("", err.Error())
		return
	}
	finish(key, "")
}

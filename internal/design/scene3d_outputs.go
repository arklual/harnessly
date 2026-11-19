package design

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The outputs of a scene: what a generator needs of a camera's shot, made from the scene itself — a few key frames, each
// as the beauty draft, clay, depth, normals, and masks of chosen objects, and the camera's path — with one manifest that names
// them all. The contract is in docs/scene3d-outputs.md: a generator (a provider, an app) reads manifest.json and nothing else.
//
// They are small drafts drawn on this machine's shared capacity (one job at a time, like the draft frames).

//go:embed scene3d_outputs.py
var scene3dOutputsPy []byte

const (
	Scene3DOutputsFrames = 4
	Scene3DOutputsMasks  = 4
	// Scene3DOutputsFar is the distance (metres, Blender's units) at which the depth picture is black: near is white.
	Scene3DOutputsFar     = 30.0
	Scene3DManifestFile   = "manifest.json"
	Scene3DCameraPathFile = "camera_path.json"
	scene3dOutputsDir     = "outputs"
)

var scene3dPasses = map[string]bool{"beauty": true, "clay": true, "depth": true, "normals": true}

// Scene3DOutputsSpec asks for the outputs of a camera at some frames.
type Scene3DOutputsSpec struct {
	Cam    Scene3DCam `json:"cam"`
	Frames []int      `json:"frames"`
	Passes []string   `json:"passes"`
	Masks  []string   `json:"masks"`
}

// Scene3DKeyframe is one key frame's pictures, file names in the outputs folder ("" for a pass not asked for).
type Scene3DKeyframe struct {
	Frame   int     `json:"frame"`
	Seconds float64 `json:"seconds"`
	Beauty  string  `json:"beauty,omitempty"`
	Clay    string  `json:"clay,omitempty"`
	Depth   string  `json:"depth,omitempty"`
	Normals string  `json:"normals,omitempty"`
}

// Scene3DMask is the mask of one object at a frame: a PNG whose alpha is the object.
type Scene3DMask struct {
	Object string `json:"object"`
	Frame  int    `json:"frame"`
	File   string `json:"file"`
}

// Scene3DManifest is manifest.json, the contract (version 1): everything is named relative to the manifest's own folder.
type Scene3DManifest struct {
	V     int `json:"v"`
	Scene struct {
		Project string `json:"project"`
		Title   string `json:"title"`
		Hash    string `json:"hash"`
	} `json:"scene"`
	Camera     Scene3DCam        `json:"camera"`
	FPS        float64           `json:"fps"`
	Range      [2]int            `json:"range"`
	Size       [2]int            `json:"size"`
	Engine     string            `json:"engine"`
	DepthFar   float64           `json:"depthFar"`
	Passes     []string          `json:"passes"`
	CameraPath string            `json:"cameraPath"`
	Keyframes  []Scene3DKeyframe `json:"keyframes"`
	Masks      []Scene3DMask     `json:"masks"`
	CreatedAt  string            `json:"createdAt"`
	Where      string            `json:"where,omitempty"`
}

// Scene3DCameraPath is camera_path.json: the camera's pose at sampled frames, Blender's axes (z up), target a point 5 m ahead.
type Scene3DCameraPath struct {
	FPS     float64               `json:"fps"`
	Start   int                   `json:"start"`
	End     int                   `json:"end"`
	Camera  string                `json:"camera,omitempty"`
	Samples []Scene3DCameraSample `json:"samples"`
}

// Scene3DCameraSample is the pose at one frame.
type Scene3DCameraSample struct {
	Frame  int        `json:"frame"`
	Pos    [3]float64 `json:"pos"`
	Target [3]float64 `json:"target"`
	Fov    float64    `json:"fov"`
}

// Scene3DOutputs is one set of outputs as the page lists it.
type Scene3DOutputs struct {
	Key      string           `json:"key"`
	Status   string           `json:"status"`
	Error    string           `json:"error,omitempty"`
	Manifest *Scene3DManifest `json:"manifest,omitempty"`
}

var s3Outputs = map[string]*Scene3DOutputs{} // under s3Reg: outputs folder -> running or failed

func scene3dOutputsKey(hash string, sp Scene3DOutputsSpec) string {
	raw, _ := json.Marshal(sp)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|outputs|%s|%dx%d|%.0f", hash, raw, Scene3DFrameW, Scene3DFrameH, Scene3DOutputsFar)))
	return hex.EncodeToString(sum[:])[:20]
}

func (l *Library) scene3dOutputsDir(id, hash, key string) string {
	return filepath.Join(l.scene3dCache(id, hash), scene3dOutputsDir, key)
}

// Scene3DOutputsCheck normalises a spec: frames within the scene's range (at most a few, in order), known passes (beauty, clay,
// depth and normals by default), masks of objects the scene has.
func Scene3DOutputsCheck(sp Scene3DOutputsSpec, meta *Scene3DMeta) (Scene3DOutputsSpec, error) {
	cam, err := Scene3DNormCam(sp.Cam, meta)
	if err != nil {
		return sp, err
	}
	sp.Cam = cam
	if len(sp.Frames) == 0 {
		sp.Frames = []int{meta.Start, (meta.Start + meta.End) / 2, meta.End}
	}
	seen := map[int]bool{}
	var frames []int
	for _, f := range sp.Frames {
		if f < meta.Start || f > meta.End {
			return sp, deckErr(http400, "frames: within the scene's range", "frames: в пределах сцены")
		}
		if !seen[f] {
			seen[f] = true
			frames = append(frames, f)
		}
	}
	sort.Ints(frames)
	if len(frames) > Scene3DOutputsFrames {
		return sp, deckErr(http400, "At most "+strconv.Itoa(Scene3DOutputsFrames)+" key frames", "Не больше "+strconv.Itoa(Scene3DOutputsFrames)+" ключевых кадров")
	}
	sp.Frames = frames
	if len(sp.Passes) == 0 {
		sp.Passes = []string{"beauty", "clay", "depth", "normals"}
	}
	var passes []string
	for _, p := range []string{"beauty", "clay", "depth", "normals"} {
		for _, q := range sp.Passes {
			if q == p {
				passes = append(passes, p)
				break
			}
		}
	}
	for _, q := range sp.Passes {
		if !scene3dPasses[q] {
			return sp, deckErr(http400, "passes: beauty, clay, depth, normals", "passes: beauty, clay, depth, normals")
		}
	}
	sp.Passes = passes
	if len(sp.Masks) > Scene3DOutputsMasks {
		return sp, deckErr(http400, "At most "+strconv.Itoa(Scene3DOutputsMasks)+" masks", "Не больше "+strconv.Itoa(Scene3DOutputsMasks)+" масок")
	}
	var masks []string
	for _, m := range sp.Masks {
		known := false
		for _, o := range meta.Objects {
			known = known || (o.N == m && o.T == "MESH")
		}
		if !known {
			return sp, deckErr(http400, "masks: "+m+" is not a mesh of the scene", "masks: "+m+" — не меш сцены")
		}
		masks = append(masks, m)
	}
	sp.Masks = masks
	return sp, nil
}

// cameraPathOf is the camera's path over the scene's range from the model's description.
func cameraPathOf(meta *Scene3DMeta, cam Scene3DCam) Scene3DCameraPath {
	p := Scene3DCameraPath{FPS: meta.FPS, Start: meta.Start, End: meta.End, Camera: cam.N, Samples: []Scene3DCameraSample{}}
	if cam.N != "" {
		for _, c := range meta.Cameras {
			if c.N != cam.N {
				continue
			}
			for _, row := range c.Track {
				if len(row) != 8 {
					continue
				}
				p.Samples = append(p.Samples, Scene3DCameraSample{Frame: int(row[0]), Pos: [3]float64{row[1], row[2], row[3]}, Target: [3]float64{row[4], row[5], row[6]}, Fov: row[7]})
			}
		}
		return p
	}
	p.Samples = append(p.Samples, Scene3DCameraSample{Frame: meta.Start, Pos: cam.Pos, Target: cam.Target, Fov: cam.Fov})
	return p
}

// StartScene3DOutputs makes the outputs of a camera at some frames, one job at a time through the draft queue; answers at once,
// ready from the cache or going.
func (l *Library) StartScene3DOutputs(id string, sp Scene3DOutputsSpec, retry bool) (Scene3DOutputs, error) {
	if err := l.requireScene3D(id); err != nil {
		return Scene3DOutputs{}, err
	}
	_, blend, hash, err := l.scene3dTarget(id)
	if err != nil {
		return Scene3DOutputs{}, deckErr(http422, err.Error(), err.Error())
	}
	meta := readScene3DMeta(l.scene3dCache(id, hash))
	if meta == nil {
		return Scene3DOutputs{}, deckErr(http409, "The model is not built yet", "Модель ещё не собрана")
	}
	sp, err = Scene3DOutputsCheck(sp, meta)
	if err != nil {
		return Scene3DOutputs{}, err
	}
	key := scene3dOutputsKey(hash, sp)
	dir := l.scene3dOutputsDir(id, hash, key)
	if m, ok := readScene3DManifest(dir); ok {
		return Scene3DOutputs{Key: key, Status: Scene3DReady, Manifest: &m}, nil
	}
	s3Reg.Lock()
	defer s3Reg.Unlock()
	if j := s3Outputs[dir]; j != nil && !(retry && j.Status == Scene3DFailed) {
		return *j, nil
	}
	j := &Scene3DOutputs{Key: key, Status: Scene3DQueued}
	s3Outputs[dir] = j
	s3Touch()
	title := id
	if a := l.ReadArtifact(id); a != nil && a.Title != "" {
		title = a.Title
	}
	s3Wg.Add(1)
	go func() {
		defer s3Wg.Done()
		l.scene3dMakeOutputs(id, title, blend, hash, dir, sp, meta, j)
	}()
	return *j, nil
}

func readScene3DManifest(dir string) (Scene3DManifest, bool) {
	raw, err := readFileLimited(filepath.Join(dir, Scene3DManifestFile), 1<<20)
	if err != nil {
		return Scene3DManifest{}, false
	}
	var m Scene3DManifest
	if json.Unmarshal(raw, &m) != nil || m.V != 1 {
		return Scene3DManifest{}, false
	}
	return m, true
}

func (l *Library) scene3dMakeOutputs(id, title, blend, hash, dir string, sp Scene3DOutputsSpec, meta *Scene3DMeta, j *Scene3DOutputs) {
	set := func(f func(o *Scene3DOutputs)) {
		s3Reg.Lock()
		f(j)
		s3Touch()
		s3Reg.Unlock()
	}
	fail := func(err error) { set(func(o *Scene3DOutputs) { o.Status, o.Error = Scene3DFailed, err.Error() }) }
	scene3dSem <- struct{}{}
	defer func() { <-scene3dSem }()
	set(func(o *Scene3DOutputs) { o.Status = Scene3DBuilding })
	tmp := dir + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		fail(err)
		return
	}
	defer os.RemoveAll(tmp)
	spec, _ := json.Marshal(map[string]any{"dir": "out", "cam": sp.Cam, "frames": sp.Frames, "w": Scene3DFrameW, "h": Scene3DFrameH, "samples": 4,
		"passes": sp.Passes, "masks": sp.Masks, "far": Scene3DOutputsFar})
	timeout := scene3dFrameTimeout(blend) * time.Duration(len(sp.Frames)*(len(sp.Passes)+len(sp.Masks))+1)
	out, where, err := scene3dBlender(context.Background(), blenderRun{Title: "3D scene: outputs", Blend: blend, Script: scene3dOutputsPy, Params: []string{string(spec)},
		Outputs: []string{"out/*.png", "out/*.jpg", "out/frames.json"}, Timeout: timeout, SlotWait: Scene3DFrameSlotWait * time.Second * 4, Tool: "blender-outputs"}, tmp)
	if err != nil {
		if errors.Is(err, errBlenderBusy) {
			err = errors.New(locale3("The machine is busy with other work; try again in a minute", "Машина занята другой работой; попробуйте ещё раз через минуту"))
		}
		fail(fmt.Errorf("%w: %s", err, s3Tail(out, 5)))
		return
	}
	raw, err := os.ReadFile(filepath.Join(tmp, "out", "frames.json"))
	if err != nil {
		fail(fmt.Errorf("Blender wrote no list of pictures: %s", s3Tail(out, 5)))
		return
	}
	var rows []struct {
		Frame   int    `json:"frame"`
		Beauty  string `json:"beauty"`
		Clay    string `json:"clay"`
		Depth   string `json:"depth"`
		Normals string `json:"normals"`
		Mask    string `json:"mask"`
		Object  string `json:"object"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		fail(err)
		return
	}
	m := Scene3DManifest{V: 1, Camera: sp.Cam, FPS: meta.FPS, Range: [2]int{meta.Start, meta.End}, Size: [2]int{Scene3DFrameW, Scene3DFrameH}, Engine: "cycles",
		DepthFar: Scene3DOutputsFar, Passes: sp.Passes, CameraPath: Scene3DCameraPathFile, Keyframes: []Scene3DKeyframe{}, Masks: []Scene3DMask{},
		CreatedAt: time.Now().UTC().Format(time.RFC3339), Where: where}
	m.Scene.Project, m.Scene.Title, m.Scene.Hash = id, title, hash
	safe := regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	for _, r := range rows {
		for _, n := range []string{r.Beauty, r.Clay, r.Depth, r.Normals, r.Mask} {
			if n == "" {
				continue
			}
			if !safe.MatchString(n) || !isRealFile(filepath.Join(tmp, "out", n)) {
				fail(fmt.Errorf("Blender did not write %s", n))
				return
			}
			if err := os.Rename(filepath.Join(tmp, "out", n), filepath.Join(tmp, n)); err != nil {
				fail(err)
				return
			}
		}
		if r.Mask != "" {
			m.Masks = append(m.Masks, Scene3DMask{Object: r.Object, Frame: r.Frame, File: r.Mask})
			continue
		}
		m.Keyframes = append(m.Keyframes, Scene3DKeyframe{Frame: r.Frame, Seconds: timeOf3(meta, r.Frame), Beauty: r.Beauty, Clay: r.Clay, Depth: r.Depth, Normals: r.Normals})
	}
	if len(m.Keyframes) == 0 {
		fail(errors.New("Blender drew no key frames"))
		return
	}
	_ = os.RemoveAll(filepath.Join(tmp, "out"))
	_ = os.Remove(filepath.Join(tmp, "scene3d.py"))
	_ = os.Remove(filepath.Join(tmp, "scene.blend"))
	if err := os.WriteFile(filepath.Join(tmp, Scene3DCameraPathFile), deckJSON(cameraPathOf(meta, sp.Cam)), 0o600); err != nil {
		fail(err)
		return
	}
	if err := os.WriteFile(filepath.Join(tmp, Scene3DManifestFile), deckJSON(m), 0o600); err != nil {
		fail(err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		fail(err)
		return
	}
	if err := os.Rename(tmp, dir); err != nil && !isRealFile(filepath.Join(dir, Scene3DManifestFile)) {
		fail(err)
		return
	}
	s3Reg.Lock()
	delete(s3Outputs, dir)
	s3Touch()
	s3Reg.Unlock()
}

func timeOf3(meta *Scene3DMeta, frame int) float64 {
	return float64(frame-meta.Start) / max(meta.FPS, 1)
}

// scene3dOutputsList lists the outputs of the current model: the ready ones and those going or failed.
func (l *Library) scene3dOutputsList(id, hash string) []Scene3DOutputs {
	root := filepath.Join(l.scene3dCache(id, hash), scene3dOutputsDir)
	out := []Scene3DOutputs{}
	names, _ := readNames(root)
	for _, n := range names {
		if m, ok := readScene3DManifest(filepath.Join(root, n)); ok {
			m := m
			out = append(out, Scene3DOutputs{Key: n, Status: Scene3DReady, Manifest: &m})
		}
	}
	s3Reg.Lock()
	for dir, j := range s3Outputs {
		if filepath.Dir(dir) == root {
			out = append(out, *j)
		}
	}
	s3Reg.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		a, b := "", ""
		if out[i].Manifest != nil {
			a = out[i].Manifest.CreatedAt
		}
		if out[j].Manifest != nil {
			b = out[j].Manifest.CreatedAt
		}
		return a > b || (a == b && out[i].Key < out[j].Key)
	})
	return out
}

// Scene3DOutputsFolder is a ready set's folder, in the current model's cache or any older one.
func (l *Library) Scene3DOutputsFolder(id, key string) (string, Scene3DManifest, error) {
	if err := l.requireScene3D(id); err != nil {
		return "", Scene3DManifest{}, err
	}
	if !frameKeyRe.MatchString(key) {
		return "", Scene3DManifest{}, deckErr(http400, "Not an outputs key", "Это не ключ выходов")
	}
	root := l.deckFile(id, Scene3DCacheDir)
	names, _ := readNames(root)
	for _, n := range names {
		dir := filepath.Join(root, n, scene3dOutputsDir, key)
		if m, ok := readScene3DManifest(dir); ok {
			return dir, m, nil
		}
	}
	return "", Scene3DManifest{}, deckErr(http404, "No such outputs", "Нет таких выходов")
}

var outputNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,120}$`)

// Scene3DOutputFile is a file of a set (a picture, the manifest, the camera path) by its name.
func (l *Library) Scene3DOutputFile(id, key, name string) (string, error) {
	dir, _, err := l.Scene3DOutputsFolder(id, key)
	if err != nil {
		return "", err
	}
	if !outputNameRe.MatchString(name) || strings.Contains(name, "..") {
		return "", deckErr(http400, "Not a file name", "Это не имя файла")
	}
	f := filepath.Join(dir, name)
	if !isRealFile(f) {
		return "", deckErr(http404, "No such file", "Нет такого файла")
	}
	return f, nil
}

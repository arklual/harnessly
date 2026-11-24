package design

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/builders"
	"github.com/ly-solutions/harnessly/internal/heavy"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The 3D scene's model: the settings, the cached glTF and draft frames, the pins. The routes are in
// scene3d_routes.go, the shapes in scene3d_types.go.

//go:embed scene3d_export.py
var scene3dExportPy []byte

//go:embed scene3d_frame.py
var scene3dFramePy []byte

// IsScene3D: the project is a 3D scene. It keeps meta/scene3d.json from the moment it is made.
func (l *Library) IsScene3D(id string) bool {
	return IsLibraryID(id) && isRealFile(l.deckFile(id, Scene3DSettingsFile))
}

func (l *Library) requireScene3D(id string) error {
	if err := l.requireDeck(id); err != nil {
		return err
	}
	if !l.IsScene3D(id) {
		return deckErr(http404, "Not a 3D scene", "Это не 3D-сцена")
	}
	return nil
}

// ValidateScene3DSettings checks the paths: Root is an absolute folder, Blend a .blend inside it, Script a file inside it.
func ValidateScene3DSettings(s Scene3DSettings) error {
	if s.V != 1 {
		return deckErr(http400, "scene3d.v must be 1", "scene3d.v должен быть 1")
	}
	if !filepath.IsAbs(s.Root) {
		return deckErr(http400, "root: an absolute path of the blender-lab project", "root: абсолютный путь проекта blender-lab")
	}
	if _, err := scene3dInside(s.Root, s.Blend, ".blend"); err != nil {
		return err
	}
	if s.Script != "" {
		if _, err := scene3dInside(s.Root, s.Script, ".py"); err != nil {
			return err
		}
	}
	return nil
}

// scene3dInside is root/rel when rel is a file with the extension that lies inside root (links followed).
func scene3dInside(root, rel, ext string) (string, error) {
	bad := deckErr(http400, "a path inside the project, "+ext+" file", "путь внутри проекта, файл "+ext)
	if rel == "" || filepath.IsAbs(rel) || !strings.EqualFold(filepath.Ext(rel), ext) {
		return "", bad
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", deckErr(http400, "The project folder is not there", "Папки проекта нет")
	}
	file, err := filepath.EvalSymlinks(filepath.Join(realRoot, filepath.FromSlash(rel)))
	if err != nil {
		return "", deckErr(http404, "No such file: "+rel, "Нет файла: "+rel)
	}
	if r, err := filepath.Rel(realRoot, file); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", bad
	}
	if st, err := os.Stat(file); err != nil || !st.Mode().IsRegular() {
		return "", bad
	}
	return file, nil
}

// ReadScene3DSettings is meta/scene3d.json.
func (l *Library) ReadScene3DSettings(id string) (Scene3DSettings, error) {
	if !IsLibraryID(id) {
		return Scene3DSettings{}, deckErr(http404, "No such design", "Нет такого макета")
	}
	raw, err := readFileLimited(l.deckFile(id, Scene3DSettingsFile), maxDeckFileBytes)
	if errors.Is(err, os.ErrNotExist) {
		return Scene3DSettings{}, deckErr(http404, "Not a 3D scene", "Это не 3D-сцена")
	}
	if err != nil {
		return Scene3DSettings{}, err
	}
	var s Scene3DSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Scene3DSettings{}, fmt.Errorf("%s: %w", Scene3DSettingsFile, err)
	}
	return s, nil
}

// InitScene3D validates and writes the settings of a new 3D scene.
func (l *Library) InitScene3D(id string, s Scene3DSettings) error {
	s.V = 1
	if !IsLibraryID(id) {
		return deckErr(http404, "No such design", "Нет такого макета")
	}
	if err := ValidateScene3DSettings(s); err != nil {
		return err
	}
	if err := l.requireDeck(id); err != nil {
		return err
	}
	if err := l.ensureMetaDirs(id); err != nil {
		return err
	}
	_, err := writeIfChanged(l.deckFile(id, Scene3DSettingsFile), deckJSON(s))
	return err
}

// ---- the blender runner

// blenderRun is one run of headless Blender on the machine's shared capacity: the one function every render of a scene goes
// through (scene3dBlender), so a pool of builder machines (internal/builders) takes it where there is one and the machine's
// own slots take it where there is none. The run happens in workdir: the script is written there, the params name files
// relative to it, and the files in Outputs are there after the run.
type blenderRun struct {
	Title    string
	Blend    string // the scene's .blend, absolute; a builder gets a copy in the workdir
	Script   []byte
	Params   []string // after "--"
	Outputs  []string // files the run writes into the workdir
	Timeout  time.Duration
	SlotWait time.Duration
	Tool     string // the name the machine's heavy log counts it under
	// Builder is the builder the person picked, by id; "" lets the hub choose (and a machine out of a hub runs here).
	Builder string
}

// scene3dBlender runs Blender in workdir and answers the tail of its output and where it ran ("" here, else the builder's
// name); tests replace it.
var scene3dBlender = runBlender

// scene3dBuilders is the client of this machine's builder pool; nil (or a nil answer): no pool, the run is local.
var scene3dBuilders func() *builders.Client

// SetScene3DBuilders is how the runtime gives the 3D scene its pool of builders.
func SetScene3DBuilders(f func() *builders.Client) { scene3dBuilders = f }

var errBlenderBusy = errors.New("the machine is busy: every slot for heavy work is taken")

var (
	blenderOnce sync.Once
	blenderBin  string
)

// findBlender is the Blender of this machine: $HARNESSLY_BLENDER, the PATH, the usual places; "" when none.
func findBlender() string {
	if v := strings.TrimSpace(os.Getenv("HARNESSLY_BLENDER")); v != "" {
		return v
	}
	blenderOnce.Do(func() {
		if p, err := exec.LookPath("blender"); err == nil {
			blenderBin = p
			return
		}
		home, _ := os.UserHomeDir()
		for _, p := range []string{filepath.Join(home, ".local", "bin", "blender"), "/usr/local/bin/blender", "/usr/bin/blender", "/snap/bin/blender", "/opt/blender/blender"} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				blenderBin = p
				return
			}
		}
	})
	return blenderBin
}

// scene3dEngine is what draft frames are drawn with. Eevee needs a GL context (libEGL); a machine without one — the shared
// one has no GPU — draws with Cycles on the CPU, few samples, denoised.
func scene3dEngine() string {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("HARNESSLY_SCENE3D_ENGINE"))); v {
	case "eevee", "cycles":
		return v
	}
	for _, d := range []string{"/usr/lib/x86_64-linux-gnu", "/usr/lib64", "/usr/lib", "/usr/lib/aarch64-linux-gnu"} {
		if _, err := os.Stat(filepath.Join(d, "libEGL.so.1")); err == nil {
			return "eevee"
		}
	}
	return "cycles"
}

type tailBuf struct {
	b []byte
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 6000 {
		t.b = t.b[len(t.b)-4000:]
	}
	return len(p), nil
}

func runBlender(ctx context.Context, r blenderRun, workdir string) (string, string, error) {
	script := filepath.Join(workdir, "scene3d.py")
	if err := os.WriteFile(script, r.Script, 0o600); err != nil {
		return "", "", err
	}
	// A scene that reads files of its project (textures by relative paths) cannot be sent to a builder alone: its textures would
	// be missing there. Such a scene runs on this machine, where the project is; a self-contained .blend may go to the pool.
	if scene3dBuilders != nil && !scene3dNeedsProject(r.Blend) {
		if c := scene3dBuilders(); c != nil {
			return runOnPool(ctx, c, r, workdir)
		}
	}
	out, err := localBlender(ctx, r, workdir, r.Blend, script)
	return out, "", err
}

// runOnPool sends the run through the builders client: the hub places it on a builder with blender, or says why it goes
// to this machine's own slots (the client then runs it here under heavy.Hold, nice and a limit, with draft env).
// scene3dNeedsProject says the .blend lies in a project that keeps textures and assets beside it (blender-lab's assets/): the
// folder of the .blend and the two above it are looked at. A very big .blend is not sent either.
func scene3dNeedsProject(blend string) bool {
	if st, err := os.Stat(blend); err == nil && st.Size() > scene3dPoolMaxBlend {
		return true
	}
	dir := filepath.Dir(blend)
	for i := 0; i < 3; i++ {
		for _, n := range []string{"assets", "textures", "tex", "maps", "images"} {
			if st, err := os.Stat(filepath.Join(dir, n)); err == nil && st.IsDir() {
				return true
			}
		}
		dir = filepath.Dir(dir)
	}
	return false
}

func runOnPool(ctx context.Context, c *builders.Client, r blenderRun, workdir string) (string, string, error) {
	// A builder gets a folder to work in: the .blend (hard-linked where it can be, no copy of a big file), the script.
	if err := linkOrCopy(r.Blend, filepath.Join(workdir, "scene.blend")); err != nil {
		return "", "", err
	}
	argv := append([]string{"blender", "-b", "scene.blend", "--disable-autoexec", "-noaudio", "-P", "scene3d.py", "--"}, r.Params...)
	var log tailBuf
	res, err := c.Run(ctx, builders.Spec{Title: r.Title, Caps: []string{"blender"}, Builder: r.Builder, Argv: argv, InDir: workdir,
		Outputs: r.Outputs, Dest: workdir, Timeout: r.Timeout, Log: &log})
	where := ""
	if res.Job.Builder != nil && !res.Job.Local {
		where = res.Job.Builder.Name
	}
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("Blender exited with %d", res.ExitCode)
	}
	if err != nil && strings.Contains(err.Error(), "exit code -1") {
		err = fmt.Errorf("Blender was stopped on %s (the time limit of %s or a kill): %w", scene3dWhere(where), r.Timeout.Round(time.Second), err)
	}
	return string(log.b), where, err
}

func scene3dWhere(builder string) string {
	if builder == "" {
		return "this machine"
	}
	return "builder " + builder
}

func linkOrCopy(from, to string) error {
	_ = os.Remove(to)
	if err := os.Link(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func localBlender(ctx context.Context, r blenderRun, workdir, blend, script string) (string, error) {
	bin := findBlender()
	if bin == "" {
		return "", errors.New("Blender is not installed on this machine")
	}
	release, err := heavy.Hold(r.Tool, r.SlotWait)
	if errors.Is(err, heavy.ErrBusy) {
		return "", errBlenderBusy
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	args := append([]string{"-b", blend, "--disable-autoexec", "-noaudio", "-P", script, "--"}, r.Params...)
	argv := heavy.Gentle(append([]string{bin}, args...))
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "OMP_NUM_THREADS=2")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var out tailBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out.b), fmt.Errorf("Blender did not finish in %s and was stopped", r.Timeout.Round(time.Second))
	}
	return string(out.b), err
}

// ---- the scene's hash and the cache

type blendStat struct {
	mtime int64
	size  int64
	hash  string
}

var blendHashes sync.Map // abs path -> blendStat

// scene3dHash is the content address of the scene: the .blend's bytes and the exporter's version.
func scene3dHash(file string) (string, error) {
	st, err := os.Stat(file)
	if err != nil {
		return "", err
	}
	if v, ok := blendHashes.Load(file); ok {
		if b := v.(blendStat); b.mtime == st.ModTime().UnixNano() && b.size == st.Size() {
			return b.hash, nil
		}
	}
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	fmt.Fprintf(h, "|export%d", scene3dExportVersion)
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	blendHashes.Store(file, blendStat{st.ModTime().UnixNano(), st.Size(), sum})
	return sum, nil
}

func (l *Library) scene3dCache(id, hash string) string {
	return l.deckFile(id, Scene3DCacheDir+"/"+hash)
}

// scene3dTarget is the scene's .blend (absolute) and the cache folder of its current content.
func (l *Library) scene3dTarget(id string) (Scene3DSettings, string, string, error) {
	s, err := l.ReadScene3DSettings(id)
	if err != nil {
		return s, "", "", err
	}
	blend, err := scene3dInside(s.Root, s.Blend, ".blend")
	if err != nil {
		return s, "", "", err
	}
	hash, err := scene3dHash(blend)
	if err != nil {
		return s, "", "", err
	}
	return s, blend, hash, nil
}

// ---- jobs in this runtime

type s3Job struct {
	status string
	err    string
	frame  Scene3DFrame
}

var s3Reg = struct {
	sync.Mutex
	jobs  map[string]*s3Job // cache folder (builds) or the frame's file (frames) -> job
	moved int64             // ms of the last change of any job: a state's rev never goes under it
	// builder is the builder asked for a cache folder's build (set by the page's request, "" automatic).
	builder map[string]string
}{jobs: map[string]*s3Job{}, builder: map[string]string{}}

func s3Touch() { s3Reg.moved = time.Now().UnixMilli() }

var scene3dSem = make(chan struct{}, scene3dMaxJobs)

// s3Wg counts the background jobs, so a test can wait for them before it puts its fakes back.
var s3Wg sync.WaitGroup

// ---- the model

func readScene3DMeta(dir string) *Scene3DMeta {
	raw, err := readFileLimited(filepath.Join(dir, "meta.json"), 8<<20)
	if err != nil {
		return nil
	}
	var m Scene3DMeta
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return &m
}

// Scene3DModelOf is the model for the scene's current .blend; a missing one starts being built.
func (l *Library) Scene3DModelOf(id string) (Scene3DModel, error) {
	_, blend, hash, err := l.scene3dTarget(id)
	if err != nil {
		return Scene3DModel{Status: Scene3DFailed, Error: err.Error()}, nil
	}
	dir := l.scene3dCache(id, hash)
	if st, err := os.Stat(filepath.Join(dir, "model.glb")); err == nil {
		if meta := readScene3DMeta(dir); meta != nil {
			return Scene3DModel{Status: Scene3DReady, Hash: hash, Bytes: st.Size(), Meta: meta}, nil
		}
	}
	s3Reg.Lock()
	j := s3Reg.jobs[dir]
	s3Reg.Unlock()
	if j != nil {
		return Scene3DModel{Status: j.status, Hash: hash, Error: j.err}, nil
	}
	// No look for Blender here: it may be on a builder of the pool, not on this machine. The runner says so when it is nowhere.
	l.scene3dBuild(id, blend, hash, dir)
	return Scene3DModel{Status: Scene3DBuilding, Hash: hash}, nil
}

// Scene3DRebuild forgets a failed build and starts it again.
func (l *Library) Scene3DRebuild(id, builder string) (Scene3DModel, error) {
	_, _, hash, err := l.scene3dTarget(id)
	if err != nil {
		return Scene3DModel{}, err
	}
	dir := l.scene3dCache(id, hash)
	s3Reg.Lock()
	if j := s3Reg.jobs[dir]; j != nil && j.status == Scene3DFailed {
		delete(s3Reg.jobs, dir)
	}
	s3Reg.builder[dir] = builder
	s3Reg.Unlock()
	return l.Scene3DModelOf(id)
}

func locale3(en, ru string) string { return locale.Pick(en, ru) }

// scene3dBuild exports the model in the background: once per cache folder.
func (l *Library) scene3dBuild(id, blend, hash, dir string) {
	s3Reg.Lock()
	if s3Reg.jobs[dir] != nil {
		s3Reg.Unlock()
		return
	}
	j := &s3Job{status: Scene3DBuilding}
	s3Reg.jobs[dir] = j
	s3Touch()
	s3Reg.Unlock()
	s3Wg.Add(1)
	go func() {
		defer s3Wg.Done()
		err := l.scene3dExport(blend, dir)
		s3Reg.Lock()
		if err != nil {
			j.status, j.err = Scene3DFailed, err.Error()
		} else {
			delete(s3Reg.jobs, dir)
			l.scene3dPrune(id, hash)
		}
		s3Touch()
		s3Reg.Unlock()
	}()
}

func (l *Library) scene3dExport(blend, dir string) error {
	tmp := dir + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	out, _, err := scene3dBlender(context.Background(), blenderRun{
		Title: "3D scene: model", Blend: blend, Script: scene3dExportPy,
		Params:  []string{"model.glb", "meta.json", strconv.Itoa(Scene3DTriBudget), strconv.Itoa(Scene3DTexMax)},
		Outputs: []string{"model.glb", "meta.json"}, Timeout: Scene3DExportTimeout * time.Second, SlotWait: 20 * time.Minute, Tool: "blender-export",
		Builder: scene3dBuilderFor(dir),
	}, tmp)
	if err != nil {
		return fmt.Errorf("%w: %s", err, s3Tail(out, 6))
	}
	if st, e := os.Stat(filepath.Join(tmp, "model.glb")); e != nil || st.Size() == 0 {
		return fmt.Errorf("Blender wrote no model: %s", s3Tail(out, 6))
	}
	if readScene3DMeta(tmp) == nil {
		return errors.New("Blender wrote no scene description")
	}
	_ = os.Remove(filepath.Join(tmp, "scene3d.py"))
	_ = os.Remove(filepath.Join(tmp, "scene.blend"))
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, e := os.Stat(filepath.Join(dir, "model.glb")); e == nil {
			return nil // another runtime of the shared library was first
		}
		return err
	}
	return nil
}

// scene3dBuilderFor is the builder the person asked for this cache folder's build with; "" is automatic.
func scene3dBuilderFor(dir string) string {
	s3Reg.Lock()
	defer s3Reg.Unlock()
	return s3Reg.builder[dir]
}

func s3Tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// scene3dPrune keeps the current model and the two newest older ones.
func (l *Library) scene3dPrune(id, keep string) {
	root := l.deckFile(id, Scene3DCacheDir)
	names, _ := readNames(root)
	type e struct {
		n string
		t time.Time
	}
	var old []e
	for _, n := range names {
		if n == keep || n == "shots" || strings.Contains(n, ".tmp-") {
			continue
		}
		if st, err := os.Stat(filepath.Join(root, n)); err == nil && st.IsDir() {
			old = append(old, e{n, st.ModTime()})
		}
	}
	sort.Slice(old, func(i, j int) bool { return old[i].t.After(old[j].t) })
	for i := 2; i < len(old); i++ {
		_ = os.RemoveAll(filepath.Join(root, old[i].n))
	}
}

// Scene3DModelFile is the model's .glb for the hash h ("" the current one) and its hash.
func (l *Library) Scene3DModelFile(id, h string) (string, string, error) {
	if err := l.requireScene3D(id); err != nil {
		return "", "", err
	}
	if h == "" {
		_, _, hash, err := l.scene3dTarget(id)
		if err != nil {
			return "", "", err
		}
		h = hash
	}
	if !hashRe.MatchString(h) {
		return "", "", deckErr(http400, "Not a model hash", "Это не хэш модели")
	}
	f := filepath.Join(l.scene3dCache(id, h), "model.glb")
	if !isRealFile(f) {
		return "", "", deckErr(http404, "The model is not built", "Модель не собрана")
	}
	return f, h, nil
}

// ---- draft frames

// Scene3DNormCam checks a view and rounds it, so equal views share a cache key. A named camera needs only its name.
func Scene3DNormCam(c Scene3DCam, meta *Scene3DMeta) (Scene3DCam, error) {
	bad := func(en, ru string) (Scene3DCam, error) { return Scene3DCam{}, deckErr(http400, en, ru) }
	finite := func(v [3]float64) bool {
		for _, f := range v {
			if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1e6 {
				return false
			}
		}
		return true
	}
	n, _ := CleanText(c.N, 80)
	if n != "" {
		if meta != nil {
			ok := false
			for _, k := range meta.Cameras {
				ok = ok || k.N == n
			}
			if !ok {
				return bad("cam: no such camera in the scene", "cam: в сцене нет такой камеры")
			}
		}
		return Scene3DCam{N: n}, nil
	}
	if !finite(c.Pos) || !finite(c.Target) || math.IsNaN(c.Fov) || c.Fov < 1 || c.Fov > 170 {
		return bad("cam: pos and target are points, fov from 1 to 170", "cam: pos и target — точки, fov от 1 до 170")
	}
	r := func(f float64) float64 { return math.Round(f*1000) / 1000 }
	return Scene3DCam{Pos: [3]float64{r(c.Pos[0]), r(c.Pos[1]), r(c.Pos[2])}, Target: [3]float64{r(c.Target[0]), r(c.Target[1]), r(c.Target[2])}, Fov: r(c.Fov)}, nil
}

func scene3dFrameKey(hash, engine string, cam Scene3DCam, frame int) string {
	raw, _ := json.Marshal(cam)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%dx%d|%d|%d|%s", hash, engine, Scene3DFrameW, Scene3DFrameH, Scene3DFrameSamples, frame, raw)))
	return hex.EncodeToString(sum[:])[:20]
}

var hashRe = regexp.MustCompile(`^[0-9a-f]{16}$`)
var frameKeyRe = regexp.MustCompile(`^[0-9a-f]{20}$`)

// Scene3DFrameStart asks for the draft frame of a view: answered from the cache at once, else queued (one at a time,
// under a slot of the machine, nice, with a time limit). The same view asked again is the same key.
func (l *Library) Scene3DFrameStart(id string, cam Scene3DCam, frame int, retry bool, builder string) (Scene3DFrame, error) {
	if err := l.requireScene3D(id); err != nil {
		return Scene3DFrame{}, err
	}
	_, blend, hash, err := l.scene3dTarget(id)
	if err != nil {
		return Scene3DFrame{}, deckErr(http422, err.Error(), err.Error())
	}
	dir := l.scene3dCache(id, hash)
	meta := readScene3DMeta(dir)
	if meta == nil {
		return Scene3DFrame{}, deckErr(http409, "The model is not built yet", "Модель ещё не собрана")
	}
	if frame < meta.Start || frame > meta.End {
		return Scene3DFrame{}, deckErr(http400, "frame: from the scene's start to its end", "frame: от начала сцены до конца")
	}
	cam, err = Scene3DNormCam(cam, meta)
	if err != nil {
		return Scene3DFrame{}, err
	}
	engine := scene3dEngine()
	key := scene3dFrameKey(hash, engine, cam, frame)
	file := filepath.Join(dir, "frames", key+".jpg")
	info := Scene3DFrame{Key: key, Cam: &cam, Frame: frame, Engine: engine}
	// A finishing job leaves the registry after its file is published. Check
	// the file and reserve a new job under the same lock to avoid a second draw.
	s3Reg.Lock()
	if isRealFile(file) {
		s3Reg.Unlock()
		info.Status, info.Cached = Scene3DReady, true
		if raw, err := readFileLimited(strings.TrimSuffix(file, ".jpg")+".json", 16*1024); err == nil {
			var side Scene3DFrame
			if json.Unmarshal(raw, &side) == nil {
				info.Where = side.Where
			}
		}
		return info, nil
	}
	defer s3Reg.Unlock()
	if j := s3Reg.jobs[file]; j != nil && !(retry && j.status == Scene3DFailed) {
		info.Status, info.Error = j.status, j.err
		return info, nil
	}
	j := &s3Job{status: Scene3DQueued, frame: info}
	s3Reg.jobs[file] = j
	s3Touch()
	info.Status = Scene3DQueued
	s3Wg.Add(1)
	go func() {
		defer s3Wg.Done()
		l.scene3dDraw(blend, file, builder, j)
	}()
	return info, nil
}

func (l *Library) scene3dDraw(blend, file, builder string, j *s3Job) {
	scene3dSem <- struct{}{}
	defer func() { <-scene3dSem }()
	s3Reg.Lock()
	j.status = Scene3DBuilding
	s3Touch()
	s3Reg.Unlock()
	err := l.scene3dRender(blend, file, builder, j.frame)
	s3Reg.Lock()
	if err != nil {
		j.status, j.err = Scene3DFailed, err.Error()
	} else {
		delete(s3Reg.jobs, file)
		scene3dTrimFrames(filepath.Dir(file))
	}
	s3Touch()
	s3Reg.Unlock()
}

func (l *Library) scene3dRender(blend, file, builder string, info Scene3DFrame) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	work, err := os.MkdirTemp(filepath.Dir(file), "work-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	spec, _ := json.Marshal(map[string]any{"out": "frame.jpg", "engine": info.Engine, "frame": info.Frame, "w": Scene3DFrameW, "h": Scene3DFrameH,
		"samples": Scene3DFrameSamples, "cam": info.Cam})
	out, where, err := scene3dBlender(context.Background(), blenderRun{
		Title: "3D scene: draft frame", Blend: blend, Script: scene3dFramePy, Params: []string{string(spec)}, Outputs: []string{"frame.jpg"},
		Timeout: scene3dFrameTimeout(blend), SlotWait: Scene3DFrameSlotWait * time.Second, Tool: "blender-draft", Builder: builder,
	}, work)
	if err != nil {
		if errors.Is(err, errBlenderBusy) {
			return errors.New(scene3dBusyWords())
		}
		return fmt.Errorf("%w: %s", err, s3Tail(out, 5))
	}
	made := filepath.Join(work, "frame.jpg")
	if st, e := os.Stat(made); e != nil || st.Size() == 0 {
		return fmt.Errorf("Blender drew no frame: %s", s3Tail(out, 5))
	}
	// The JPEG is the ready marker used by FrameStart and FrameFile. Publish it
	// only after the corresponding complete metadata is available to list readers.
	info.Status, info.Where = Scene3DReady, where
	sidecar := strings.TrimSuffix(file, ".jpg") + ".json"
	staged := filepath.Join(work, "frame.json")
	if err := os.WriteFile(staged, deckJSON(info), 0o600); err != nil {
		return err
	}
	if err := os.Rename(staged, sidecar); err != nil {
		return err
	}
	if err := os.Rename(made, file); err != nil {
		_ = os.Remove(sidecar)
		return err
	}
	return nil
}

// scene3dTrimFrames drops the oldest frames when the folder is over the cap.
func scene3dTrimFrames(dir string) {
	names, _ := readNames(dir)
	type e struct {
		p string
		t time.Time
		n int64
	}
	var all []e
	var total int64
	for _, n := range names {
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil && st.Mode().IsRegular() {
			all = append(all, e{filepath.Join(dir, n), st.ModTime(), st.Size()})
			total += st.Size()
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
	for i := 0; total > Scene3DCacheMax && i < len(all); i++ {
		_ = os.Remove(all[i].p)
		total -= all[i].n
	}
}

// Scene3DFrameFile is a cached frame's JPEG.
func (l *Library) Scene3DFrameFile(id, key string) (string, error) {
	if err := l.requireScene3D(id); err != nil {
		return "", err
	}
	if !frameKeyRe.MatchString(key) {
		return "", deckErr(http400, "Not a frame key", "Это не ключ кадра")
	}
	_, _, hash, err := l.scene3dTarget(id)
	if err != nil {
		return "", err
	}
	f := filepath.Join(l.scene3dCache(id, hash), "frames", key+".jpg")
	if !isRealFile(f) {
		return "", deckErr(http404, "No such frame", "Нет такого кадра")
	}
	return f, nil
}

// scene3dFrames lists the frames of the current model: the cached ones and the jobs still going or failed.
func (l *Library) scene3dFrames(id, hash string) []Scene3DFrame {
	dir := filepath.Join(l.scene3dCache(id, hash), "frames")
	// A running job can publish its JPEG and leave the registry while we scan.
	// Snapshot jobs first and let durable records supersede them by frame key.
	frames := make(map[string]Scene3DFrame)
	s3Reg.Lock()
	for file, j := range s3Reg.jobs {
		if filepath.Dir(file) == dir && j.frame.Key != "" {
			f := j.frame
			f.Status, f.Error = j.status, j.err
			frames[f.Key] = f
		}
	}
	s3Reg.Unlock()
	names, _ := readNames(dir)
	for _, n := range names {
		if !strings.HasSuffix(n, ".json") {
			continue
		}
		raw, err := readFileLimited(filepath.Join(dir, n), 16*1024)
		var f Scene3DFrame
		if err != nil || json.Unmarshal(raw, &f) != nil || !frameKeyRe.MatchString(f.Key) || !isRealFile(filepath.Join(dir, f.Key+".jpg")) {
			continue
		}
		frames[f.Key] = f
	}
	out := make([]Scene3DFrame, 0, len(frames))
	for _, f := range frames {
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if len(out) > 60 {
		out = out[len(out)-60:]
	}
	return out
}

// ---- comments

func scene3dFiniteP(p *[3]float64) bool {
	for _, f := range p {
		if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1e6 {
			return false
		}
	}
	return true
}

// AddScene3DComment pins a comment on the model: the object, a point, the view and the frame; the viewport's picture
// (shot, a JPEG) is kept beside it.
func (l *Library) AddScene3DComment(id string, spec DeckCommentSpec, shot []byte) (DeckComment, error) {
	if err := l.requireScene3D(id); err != nil {
		return DeckComment{}, err
	}
	a := spec.Scene
	if a == nil {
		return DeckComment{}, deckErr(http400, "A pin needs its view of the scene", "Пину нужен ракурс сцены")
	}
	text := cleanCommentText(spec.Text)
	if text == "" || utf8.RuneCountInString(text) > maxDeckCommentRune {
		return DeckComment{}, deckErr(http400, "The comment is empty or over 2000 characters", "Комментарий пуст или длиннее 2000 знаков")
	}
	_, _, hash, terr := l.scene3dTarget(id)
	var meta *Scene3DMeta
	if terr == nil {
		meta = readScene3DMeta(l.scene3dCache(id, hash))
	}
	anchor := Scene3DAnchor{Frame: a.Frame}
	if meta != nil && (a.Frame < meta.Start || a.Frame > meta.End) {
		return DeckComment{}, deckErr(http400, "frame: from the scene's start to its end", "frame: от начала сцены до конца")
	}
	if a.Frame < 0 || a.Frame > 1_000_000 {
		return DeckComment{}, deckErr(http400, "frame: a frame number", "frame: номер кадра")
	}
	if a.Object != "" {
		o, _ := CleanText(a.Object, 128)
		if meta != nil {
			ok := false
			for _, k := range meta.Objects {
				ok = ok || k.N == o
			}
			if !ok {
				return DeckComment{}, deckErr(http400, "object: no such object in the scene", "object: в сцене нет такого объекта")
			}
		}
		anchor.Object = o
	}
	if a.P != nil {
		if !scene3dFiniteP(a.P) {
			return DeckComment{}, deckErr(http400, "p: three numbers", "p: три числа")
		}
		anchor.P = a.P
	}
	if a.Cam != nil {
		cam, err := Scene3DNormCam(*a.Cam, meta)
		if err != nil {
			return DeckComment{}, err
		}
		if cam.N != "" {
			// A pin keeps the camera as it was seen: the named camera and the view of it, for the viewer to go back to.
			cam = Scene3DCam{N: cam.N, Pos: a.Cam.Pos, Target: a.Cam.Target, Fov: a.Cam.Fov}
			if !scene3dFiniteP(&cam.Pos) || !scene3dFiniteP(&cam.Target) || math.IsNaN(cam.Fov) || cam.Fov < 0 || cam.Fov > 170 {
				return DeckComment{}, deckErr(http400, "cam: pos and target are points", "cam: pos и target — точки")
			}
		}
		anchor.Cam = &cam
	}
	if anchor.Object == "" && anchor.P == nil && anchor.Cam == nil {
		return DeckComment{}, deckErr(http400, "A pin needs an object, a point or a view", "Пину нужен объект, точка или ракурс")
	}
	if len(shot) > scene3dMaxShot || (len(shot) > 0 && !bytes.HasPrefix(shot, []byte{0xFF, 0xD8, 0xFF})) {
		return DeckComment{}, deckErr(http400, "shot: a JPEG under 400 KB", "shot: JPEG до 400 КБ")
	}
	frameFile := ""
	if a.Render != "" {
		if !frameKeyRe.MatchString(a.Render) {
			return DeckComment{}, deckErr(http400, "render: a frame key", "render: ключ кадра")
		}
		if terr == nil && isRealFile(filepath.Join(l.scene3dCache(id, hash), "frames", a.Render+".jpg")) {
			anchor.Render = a.Render
			frameFile = Scene3DCacheDir + "/" + hash + "/frames/" + a.Render + ".jpg"
		}
	}
	label, _ := CleanText(spec.Label, 80)
	if label == "" {
		label = anchor.Object
	}
	if existing, _ := l.ReadDeckComments(id); len(existing) >= maxDeckComments {
		return DeckComment{}, deckErr(http409, "Too many comments on the scene", "Слишком много комментариев к сцене")
	}
	by, err := l.author()
	if err != nil {
		return DeckComment{}, err
	}
	c := DeckComment{ID: NewPageID(), Label: label, Text: text, By: by, At: l.clock().UTC().Format(time.RFC3339),
		View: DeckCommentScene3D, Scene: &anchor, Frame: frameFile}
	if len(shot) > 0 {
		dir := l.deckFile(id, Scene3DCacheDir+"/shots")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return DeckComment{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, c.ID+".jpg"), shot, 0o600); err != nil {
			return DeckComment{}, err
		}
		c.Scene.Shot = true
	}
	if err := l.writeCommentFile(id, c.ID, c); err != nil {
		return DeckComment{}, err
	}
	return c, nil
}

// Scene3DShotFile is the viewport picture of a pin.
func (l *Library) Scene3DShotFile(id, comment string) (string, error) {
	if err := l.requireScene3D(id); err != nil {
		return "", err
	}
	if !deckCommentIDRe.MatchString(comment) {
		return "", deckErr(http400, "Not a comment id", "Это не номер комментария")
	}
	f := l.deckFile(id, Scene3DCacheDir+"/shots/"+comment+".jpg")
	if !isRealFile(f) {
		return "", deckErr(http404, "No such picture", "Нет такой картинки")
	}
	return f, nil
}

// ---- state

func (l *Library) scene3dRev(id string) int64 {
	var newest int64
	touch := func(p string) {
		if st, err := os.Lstat(p); err == nil && st.ModTime().UnixMilli() > newest {
			newest = st.ModTime().UnixMilli()
		}
	}
	touch(l.deckFile(id, Scene3DSettingsFile))
	dir := l.commentsDir(id)
	touch(dir)
	names, _ := readNames(dir)
	for _, n := range names {
		touch(filepath.Join(dir, n))
	}
	if s, blend, hash, err := l.scene3dTarget(id); err == nil {
		touch(blend)
		touch(l.scene3dCache(id, hash))
		touch(filepath.Join(l.scene3dCache(id, hash), "frames"))
		_ = s
	}
	s3Reg.Lock()
	if s3Reg.moved > newest {
		newest = s3Reg.moved
	}
	s3Reg.Unlock()
	return newest
}

// Scene3DStateOf is everything the 3D page reads in one go.
func (l *Library) Scene3DStateOf(id string) (Scene3DState, error) {
	if err := l.requireScene3D(id); err != nil {
		return Scene3DState{}, err
	}
	s, err := l.ReadScene3DSettings(id)
	if err != nil {
		return Scene3DState{}, err
	}
	model, err := l.Scene3DModelOf(id)
	if err != nil {
		return Scene3DState{}, err
	}
	comments, guests := l.ReadDeckComments(id)
	frames, clips, outputs := []Scene3DFrame{}, []Scene3DClip{}, []Scene3DOutputs{}
	if model.Hash != "" {
		frames, clips, outputs = l.scene3dFrames(id, model.Hash), l.scene3dClips(id, model.Hash), l.scene3dOutputsList(id, model.Hash)
	}
	title := id
	if a := l.ReadArtifact(id); a != nil && a.Title != "" {
		title = a.Title
	}
	return Scene3DState{Rev: l.scene3dRev(id), Title: title, Settings: s, Blender: findBlender() != "", Engine: scene3dEngine(), Model: model,
		Comments: comments, GuestResolved: guests, Frames: frames, Clips: clips, Finals: Scene3DFinals(id), Outputs: outputs, Gens: l.scene3dGens(id), Generator: scene3dGenerator != nil}, nil
}

// scene3dBusyWords is what a draft says when no slot of the machine freed up in time; a job that waits for its frames
// (a clip, the outputs) takes it for «ask again», not for a failure.
func scene3dBusyWords() string {
	return locale3("The machine is busy with other work; try the frame again in a minute", "Машина занята другой работой; попробуйте кадр ещё раз через минуту")
}

// scene3dFrameTimeout is the limit of one draft: the base, and a second more for each megabyte of the scene (a heavy scene with
// textures takes longer to load and to draw), at most ten minutes.
func scene3dFrameTimeout(blend string) time.Duration {
	d := Scene3DFrameTimeout * time.Second
	if st, err := os.Stat(blend); err == nil {
		d += time.Duration(st.Size()>>20) * time.Second
	}
	return min(d, 10*time.Minute)
}

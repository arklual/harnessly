package design

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The final render of a scene on a rented GPU. It costs money, so nothing here starts without the person: a plan is
// made first (the dry run of the cloud script and an estimate, no machine rented), and a render starts only from a plan the
// person confirmed, within a quarter of an hour of it. The agent's tools have no way to start one.

//go:embed scene3d_pack.py
var scene3dPackPy []byte

const (
	scene3dPlanTTL      = 15 * time.Minute
	scene3dFinalMax     = 1200 // frames
	scene3dFinalTimeout = 5 * time.Hour
	// The cloud script's own figures (blender-lab/gpu/render_selectel.py): setup hours, seconds a frame, roubles an hour.
	scene3dCloudSetupH = 0.4
	scene3dCloudSecPF  = 8.0
	scene3dCloudRubH   = 290.0
)

// Scene3DFinalPlan is what a final render would do and cost, for the person to confirm.
type Scene3DFinalPlan struct {
	ID          string           `json:"id"`
	Spec        Scene3DClipSpec  `json:"spec"`
	Frames      int              `json:"frames"`
	Seconds     float64          `json:"seconds"`
	Hours       float64          `json:"hours"`
	EstimateRub float64          `json:"estimateRub"`
	Dry         string           `json:"dry"` // the cloud script's dry run, or why there is none
	Warnings    []string         `json:"warnings"`
	Expires     string           `json:"expires"`
	Hash        string           `json:"hash"`
	Cam         Scene3DCam       `json:"cam"`
	Checks      []Scene3DWarning `json:"checks,omitempty"`
}

// Scene3DWarning is a fault of the scene the plan names before money is spent.
type Scene3DWarning struct {
	Code  string   `json:"code"`
	Count int      `json:"count"`
	Names []string `json:"names,omitempty"`
}

// Scene3DFinalJob is a final render going or done.
type Scene3DFinalJob struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // building, ready, failed
	Phase   string `json:"phase"`
	Error   string `json:"error,omitempty"`
	Clip    string `json:"clip,omitempty"` // the clip's key once ready
	Cloud   string `json:"cloud,omitempty"`
	Started string `json:"started"`
	Plan    Scene3DFinalPlan
}

var s3Final = struct {
	sync.Mutex
	plans map[string]struct {
		plan    Scene3DFinalPlan
		project string
		until   time.Time
	}
	jobs map[string]*Scene3DFinalJob
}{plans: map[string]struct {
	plan    Scene3DFinalPlan
	project string
	until   time.Time
}{}, jobs: map[string]*Scene3DFinalJob{}}

// scene3dCloudPlan is the dry run of the cloud script; scene3dCloudRender the real run. Tests replace both.
var (
	scene3dCloudPlan   = cloudPlan
	scene3dCloudRender = cloudRender
)

func (l *Library) scene3dBlend(id string) (Scene3DSettings, string, string, *Scene3DMeta, error) {
	s, blend, hash, err := l.scene3dTarget(id)
	if err != nil {
		return s, "", "", nil, deckErr(http422, err.Error(), err.Error())
	}
	meta := readScene3DMeta(l.scene3dCache(id, hash))
	if meta == nil {
		return s, "", "", nil, deckErr(http409, "The model is not built yet", "Модель ещё не собрана")
	}
	return s, blend, hash, meta, nil
}

// Scene3DPlanFinal makes the plan of a final render: the frames of a camera, one by one. Nothing is rented.
func (l *Library) Scene3DPlanFinal(ctx context.Context, id string, sp Scene3DClipSpec) (Scene3DFinalPlan, error) {
	if err := l.requireScene3D(id); err != nil {
		return Scene3DFinalPlan{}, err
	}
	s, _, hash, meta, err := l.scene3dBlend(id)
	if err != nil {
		return Scene3DFinalPlan{}, err
	}
	sp.Step = 1
	if sp.Start == 0 && sp.End == 0 {
		sp.Start, sp.End = meta.Start, meta.End
	}
	sp, err = Scene3DClipCheck(sp, meta, scene3dFinalMax)
	if err != nil {
		return Scene3DFinalPlan{}, err
	}
	n := sp.End - sp.Start + 1
	hours := scene3dCloudSetupH + float64(n)*scene3dCloudSecPF/3600
	plan := Scene3DFinalPlan{Spec: sp, Frames: n, Seconds: float64(n) / max(meta.FPS, 1), Hours: hours, EstimateRub: float64(int(hours*scene3dCloudRubH + 0.5)),
		Hash: hash, Cam: sp.Cam, Warnings: []string{}}
	for _, w := range []struct {
		code  string
		names []string
	}{{"missing-textures", meta.Stats.MissingImages}, {"scale-not-applied", meta.Stats.UnappliedScale}} {
		if len(w.names) > 0 {
			plan.Checks = append(plan.Checks, Scene3DWarning{Code: w.code, Count: len(w.names), Names: w.names[:min(len(w.names), 5)]})
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s: %d", w.code, len(w.names)))
		}
	}
	if sp.Cam.N == "" {
		plan.Warnings = append(plan.Warnings, "free view: the render will use a camera made from it")
	}
	dctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	dry, derr := scene3dCloudPlan(dctx, s.Root, n)
	if derr != nil {
		plan.Dry = "no dry run: " + derr.Error() + "\n" + s3Tail(dry, 6)
	} else {
		plan.Dry = s3Tail(dry, 12)
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	plan.ID = hex.EncodeToString(b[:])
	until := time.Now().Add(scene3dPlanTTL)
	plan.Expires = until.UTC().Format(time.RFC3339)
	s3Final.Lock()
	for k, p := range s3Final.plans {
		if time.Now().After(p.until) {
			delete(s3Final.plans, k)
		}
	}
	s3Final.plans[plan.ID] = struct {
		plan    Scene3DFinalPlan
		project string
		until   time.Time
	}{plan, id, until}
	s3Final.Unlock()
	return plan, nil
}

// StartScene3DFinal starts a render from a plan the person confirmed. confirm must be true, the plan this scene's own, not
// expired, and the scene unchanged since it was made.
func (l *Library) StartScene3DFinal(id, planID string, confirm bool) (Scene3DFinalJob, error) {
	if !confirm {
		return Scene3DFinalJob{}, deckErr(http409, "A final render rents a GPU and costs money: it starts only when the person confirms the plan", "Финальный рендер арендует GPU и стоит денег: он стартует только после подтверждения плана человеком")
	}
	if err := l.requireScene3D(id); err != nil {
		return Scene3DFinalJob{}, err
	}
	s3Final.Lock()
	p, ok := s3Final.plans[planID]
	delete(s3Final.plans, planID) // a plan is confirmed once
	s3Final.Unlock()
	if !ok || p.project != id || time.Now().After(p.until) {
		return Scene3DFinalJob{}, deckErr(http409, "No such plan, or it expired: make the plan again", "Нет такого плана или он устарел: составьте план заново")
	}
	s, blend, hash, _, err := l.scene3dBlend(id)
	if err != nil {
		return Scene3DFinalJob{}, err
	}
	if hash != p.plan.Hash {
		return Scene3DFinalJob{}, deckErr(http409, "The scene changed since the plan: make it again", "Сцена изменилась после плана: составьте его заново")
	}
	job := &Scene3DFinalJob{ID: p.plan.ID, Status: "building", Phase: "pack", Started: time.Now().UTC().Format(time.RFC3339), Plan: p.plan}
	s3Final.Lock()
	s3Final.jobs[job.ID] = job
	s3Final.Unlock()
	// The answer is copied before the work starts: from then on the job changes under s3Final only.
	started := *job
	s3Wg.Add(1)
	go func() {
		defer s3Wg.Done()
		l.scene3dFinal(id, s, blend, hash, job)
	}()
	return started, nil
}

// Scene3DFinals are the final renders of this runtime (newest first).
func Scene3DFinals(project string) []Scene3DFinalJob {
	s3Final.Lock()
	defer s3Final.Unlock()
	out := []Scene3DFinalJob{}
	for _, j := range s3Final.jobs {
		if j.Plan.Cam.N != "" || j.Plan.Frames > 0 {
			out = append(out, *j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Started > out[k].Started })
	return out
}

func (l *Library) scene3dFinal(id string, s Scene3DSettings, blend, hash string, job *Scene3DFinalJob) {
	set := func(f func(j *Scene3DFinalJob)) {
		s3Final.Lock()
		f(job)
		s3Final.Unlock()
		s3Reg.Lock()
		s3Touch()
		s3Reg.Unlock()
	}
	fail := func(format string, a ...any) {
		set(func(j *Scene3DFinalJob) { j.Status, j.Phase, j.Error = "failed", "", fmt.Sprintf(format, a...) })
	}
	ctx, cancel := context.WithTimeout(context.Background(), scene3dFinalTimeout)
	defer cancel()
	sp := job.Plan.Spec
	// Nothing is written into the person's project: the work is in the scene's cache folder.
	if err := os.MkdirAll(l.deckFile(id, Scene3DCacheDir), 0o700); err != nil {
		fail("%v", err)
		return
	}
	work, err := os.MkdirTemp(l.deckFile(id, Scene3DCacheDir), "final-")
	if err != nil {
		fail("%v", err)
		return
	}
	defer os.RemoveAll(work)
	// 1. one .blend with the camera made the scene's own and the textures packed, on a slot of this machine.
	spec := fmt.Sprintf(`{"cam": %s}`, string(deckJSON(sp.Cam)))
	out, _, err := scene3dBlender(ctx, blenderRun{Title: "3D scene: pack for the final render", Blend: blend, Script: scene3dPackPy,
		Params: []string{"pack.blend", spec}, Outputs: []string{"pack.blend"}, Timeout: 30 * time.Minute, SlotWait: 30 * time.Minute, Tool: "blender-pack"}, work)
	if err != nil {
		fail("packing the scene: %v: %s", err, s3Tail(out, 5))
		return
	}
	// 2. the render box: rent, render, download, delete.
	set(func(j *Scene3DFinalJob) { j.Phase = "render" })
	framesDir := filepath.Join(work, "frames")
	log, err := scene3dCloudRender(ctx, s.Root, filepath.Join(work, "pack.blend"), sp.Start, sp.End, framesDir)
	set(func(j *Scene3DFinalJob) { j.Cloud = s3Tail(log, 12) })
	if err != nil {
		fail("the GPU render: %v: %s", err, s3Tail(log, 6))
		return
	}
	// 3. the frames become a clip of the scene.
	set(func(j *Scene3DFinalJob) { j.Phase = "clip" })
	key, err := l.scene3dFinalClip(ctx, id, hash, sp, framesDir, job.Plan)
	if err != nil {
		fail("%v", err)
		return
	}
	set(func(j *Scene3DFinalJob) { j.Status, j.Phase, j.Clip = "ready", "", key })
}

// scene3dFinalClip makes the clip of a final render's PNG frames: the MP4 at the render's size, and JPEG frames of at most
// 960 px, at most 150 of them, for a video scene to draw from.
func (l *Library) scene3dFinalClip(ctx context.Context, id, hash string, sp Scene3DClipSpec, framesDir string, plan Scene3DFinalPlan) (string, error) {
	names, _ := readNames(framesDir)
	var pngs []string
	for _, n := range names {
		if strings.HasSuffix(strings.ToLower(n), ".png") {
			pngs = append(pngs, filepath.Join(framesDir, n))
		}
	}
	if len(pngs) == 0 {
		return "", errors.New("the render brought no frames")
	}
	meta := l.scene3dMeta(id)
	fps := 24.0
	if meta != nil {
		fps = meta.FPS
	}
	key := scene3dClipKey(hash, sp, "final")
	dir := l.scene3dClipDir(id, hash, key)
	tmp := dir + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	bin := DeckFFmpeg()
	if bin == "" {
		return "", errors.New("ffmpeg is not installed on this machine")
	}
	run := func(args ...string) error {
		argv := scene3dNice(append([]string{bin, "-y", "-hide_banner", "-loglevel", "error"}, args...))
		o, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg: %w: %s", err, s3Tail(string(o), 4))
		}
		return nil
	}
	// the numbered names ffmpeg reads: f0001.png in render order
	seq := filepath.Join(tmp, "png")
	if err := os.MkdirAll(seq, 0o700); err != nil {
		return "", err
	}
	for i, p := range pngs {
		if err := linkOrCopy(p, filepath.Join(seq, fmt.Sprintf("f%05d.png", i+1))); err != nil {
			return "", err
		}
	}
	if err := run("-framerate", strconv.FormatFloat(fps, 'f', 3, 64), "-i", filepath.Join(seq, "f%05d.png"), "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "18", "-movflags", "+faststart", filepath.Join(tmp, "clip.mp4")); err != nil {
		return "", err
	}
	stride := max(1, (len(pngs)+149)/150)
	played := 0
	for i := 0; i < len(pngs); i += stride {
		played++
		if err := run("-i", pngs[i], "-vf", "scale='min(960,iw)':-2", "-q:v", "4", filepath.Join(tmp, fmt.Sprintf("f%04d.jpg", played))); err != nil {
			return "", err
		}
	}
	_ = os.RemoveAll(seq)
	w, h := 1920, 1080
	if meta != nil {
		w, h = meta.Res[0], meta.Res[1]
	}
	title := id
	if a := l.ReadArtifact(id); a != nil && a.Title != "" {
		title = a.Title
	}
	origin := Scene3DOrigin{Project: id, Title: title, Hash: hash, Cam: sp.Cam, Start: sp.Start, End: sp.End, Step: stride, Kind: "final",
		Engine: "cycles (GPU)", Where: "Selectel GPU", Width: min(960, w), Height: min(h*min(960, w)/max(w, 1), h), CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	clip := Scene3DClip{Key: key, Status: Scene3DReady, Done: played, Total: played, Frames: played, FPS: fps / float64(stride), Seconds: float64(len(pngs)) / fps, Origin: origin}
	if err := os.WriteFile(filepath.Join(tmp, "clip.json"), deckJSON(clip), 0o600); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil && !isRealFile(filepath.Join(dir, "clip.json")) {
		return "", err
	}
	_ = plan
	return key, nil
}

// cloudPlan is the cloud script's dry run: `plan` only reads the provider's API. It needs the machine's cloud login; without
// it the answer says so and the estimate stands alone.
func cloudPlan(ctx context.Context, root string, frames int) (string, error) {
	if !isRealFile(filepath.Join(root, "gpu", "render_selectel.py")) {
		return "", errors.New("the project has no gpu/render_selectel.py")
	}
	cmd := exec.CommandContext(ctx, "bash", "-c", "source gpu/sel_auth.sh && python3 -I gpu/render_selectel.py plan --frames "+strconv.Itoa(frames))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// cloudRender rents the box, renders and deletes it again (render_selectel.py render: its `finally` cleans up, a watchdog backs
// that up after the time limit), then makes sure nothing is left alive. The frames land in dest.
func cloudRender(ctx context.Context, root, pack string, start, end int, dest string) (string, error) {
	script := "source gpu/sel_auth.sh && python3 -I -u gpu/render_selectel.py render --blend \"$HL_PACK\" --frames \"$HL_FRAMES\" --ttl-hours 5; code=$?; python3 -I gpu/render_selectel.py status; exit $code"
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "HL_PACK="+pack, "HL_FRAMES="+fmt.Sprintf("%d-%d", start, end))
	var out tailBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	// the script writes the frames to out/frames_gpu
	src := filepath.Join(root, "out", "frames_gpu")
	if err == nil {
		if mkErr := os.MkdirAll(dest, 0o700); mkErr != nil {
			return string(out.b), mkErr
		}
		names, _ := readNames(src)
		for _, n := range names {
			if strings.HasSuffix(strings.ToLower(n), ".png") {
				if mvErr := linkOrCopy(filepath.Join(src, n), filepath.Join(dest, n)); mvErr != nil {
					return string(out.b), mvErr
				}
			}
		}
	}
	return string(out.b), err
}

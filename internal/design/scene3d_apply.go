package design

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/heavy"
)

// Closing a pin by a change of the scene: the agent edits the scene's script (a plain file of the blender-lab
// project), then Scene3DApply builds the .blend again with it on this machine's slots, draws the pins' views from the scene
// before and after, and Scene3DResolve closes the pin with the two frames.

const (
	scene3dApplyTimeout  = 25 * time.Minute
	scene3dApplyPins     = 6
	scene3dModelWait     = 12 * time.Minute
	scene3dApplyKeepJobs = 8
)

// Scene3DApplyPin is one pin's before and after: the frames' keys and their files (for the agent to open).
type Scene3DApplyPin struct {
	ID         string `json:"id"`
	Object     string `json:"object,omitempty"`
	Text       string `json:"text"`
	BeforeKey  string `json:"beforeKey,omitempty"`
	Before     string `json:"before,omitempty"`
	AfterKey   string `json:"afterKey,omitempty"`
	After      string `json:"after,omitempty"`
	Error      string `json:"error,omitempty"`
	SameAsPrev bool   `json:"sameAsBefore,omitempty"`
}

// Scene3DApplyJob is a build of the scene from its script and the draw of the pins' frames around it.
type Scene3DApplyJob struct {
	ID      string            `json:"id"`
	Status  string            `json:"status"` // building, ready, failed
	Phase   string            `json:"phase"`
	Error   string            `json:"error,omitempty"`
	Output  string            `json:"output,omitempty"`
	Changed bool              `json:"changed"`
	Hash    string            `json:"hash,omitempty"`
	Pins    []Scene3DApplyPin `json:"pins"`
	Started string            `json:"started"`
}

var s3Apply = struct {
	sync.Mutex
	jobs map[string]*Scene3DApplyJob // by job id
	seq  int
}{jobs: map[string]*Scene3DApplyJob{}}

func (j *Scene3DApplyJob) snapshot() Scene3DApplyJob {
	c := *j
	c.Pins = append([]Scene3DApplyPin{}, j.Pins...)
	return c
}

// Scene3DApplyOf is a job of the apply, by id (a copy).
func Scene3DApplyOf(id string) (Scene3DApplyJob, bool) {
	s3Apply.Lock()
	defer s3Apply.Unlock()
	j := s3Apply.jobs[id]
	if j == nil {
		return Scene3DApplyJob{}, false
	}
	return j.snapshot(), true
}

// scene3dFrameWait draws a frame and waits for it: the frame, its key and the file.
func (l *Library) scene3dFrameWait(ctx context.Context, id string, cam Scene3DCam, frame int, wait time.Duration) (Scene3DFrame, string, error) {
	deadline := time.Now().Add(wait)
	retry := true
	for {
		f, err := l.Scene3DFrameStart(id, cam, frame, retry, "")
		if err != nil {
			return f, "", err
		}
		retry = false
		switch f.Status {
		case Scene3DReady:
			file, err := l.Scene3DFrameFile(id, f.Key)
			return f, file, err
		case Scene3DFailed:
			if f.Error == scene3dBusyWords() && time.Now().Before(deadline) {
				retry = true // the machine was busy: a job like this waits its turn, it does not fail for it
				break
			}
			return f, "", errors.New(f.Error)
		}
		if time.Now().After(deadline) {
			return f, "", errors.New("the frame did not finish in time")
		}
		select {
		case <-ctx.Done():
			return f, "", ctx.Err()
		case <-time.After(1200 * time.Millisecond):
		}
	}
}

// StartScene3DApply starts the apply as a job and answers it at once; read it again with Scene3DApplyOf. pinIDs empty: the
// open pins (at most a few).
func (l *Library) StartScene3DApply(id string, pinIDs []string) (Scene3DApplyJob, error) {
	if err := l.requireScene3D(id); err != nil {
		return Scene3DApplyJob{}, err
	}
	s, err := l.ReadScene3DSettings(id)
	if err != nil {
		return Scene3DApplyJob{}, err
	}
	if s.Script == "" {
		return Scene3DApplyJob{}, deckErr(http409, "The scene has no script set: it can only be viewed", "У сцены не задан скрипт: её можно только смотреть")
	}
	script, err := scene3dInside(s.Root, s.Script, ".py")
	if err != nil {
		return Scene3DApplyJob{}, err
	}
	if findBlender() == "" && os.Getenv("HARNESSLY_BLENDER") == "" {
		return Scene3DApplyJob{}, deckErr(http409, "Blender is not installed on this machine", "На машине нет Blender")
	}
	all, _ := l.ReadDeckComments(id)
	want := map[string]bool{}
	for _, p := range pinIDs {
		want[p] = true
	}
	var pins []Scene3DApplyPin
	var anchors []DeckComment
	for _, c := range all {
		if c.View != DeckCommentScene3D || c.Scene == nil || c.Scene.Cam == nil || (len(want) > 0 && !want[c.ID]) || (len(want) == 0 && c.Resolved) {
			continue
		}
		if len(pins) >= scene3dApplyPins {
			break
		}
		pins = append(pins, Scene3DApplyPin{ID: c.ID, Object: c.Scene.Object, Text: c.Text})
		anchors = append(anchors, c)
	}
	s3Apply.Lock()
	s3Apply.seq++
	job := &Scene3DApplyJob{ID: fmt.Sprintf("a%d-%d", time.Now().Unix(), s3Apply.seq), Status: "building", Phase: "before", Pins: pins,
		Started: time.Now().UTC().Format(time.RFC3339)}
	s3Apply.jobs[job.ID] = job
	if len(s3Apply.jobs) > scene3dApplyKeepJobs {
		ids := make([]string, 0, len(s3Apply.jobs))
		for k := range s3Apply.jobs {
			ids = append(ids, k)
		}
		sort.Strings(ids)
		delete(s3Apply.jobs, ids[0])
	}
	snap := job.snapshot()
	s3Apply.Unlock()
	s3Wg.Add(1)
	go func() {
		defer s3Wg.Done()
		l.scene3dApply(id, s, script, job, anchors)
	}()
	return snap, nil
}

func (l *Library) scene3dApply(id string, s Scene3DSettings, script string, job *Scene3DApplyJob, anchors []DeckComment) {
	ctx, cancel := context.WithTimeout(context.Background(), scene3dApplyTimeout+scene3dModelWait+20*time.Minute)
	defer cancel()
	set := func(f func(j *Scene3DApplyJob)) {
		s3Apply.Lock()
		f(job)
		s3Apply.Unlock()
		s3Reg.Lock()
		s3Touch()
		s3Reg.Unlock()
	}
	fail := func(format string, a ...any) {
		set(func(j *Scene3DApplyJob) { j.Status, j.Phase, j.Error = "failed", "", fmt.Sprintf(format, a...) })
	}
	// 1. the pins' views drawn from the scene as it is (a pin that kept its frame uses it).
	blend, err := scene3dInside(s.Root, s.Blend, ".blend")
	if err != nil {
		fail("%v", err)
		return
	}
	hashBefore, _ := scene3dHash(blend)
	if model, _ := l.Scene3DModelOf(id); model.Status != Scene3DReady {
		// the draw needs the scene's description (frames and cameras): wait for the model of the scene as it is.
		if err := l.scene3dWaitModel(ctx, id); err != nil {
			fail("the model of the scene did not build before the change: %v", err)
			return
		}
	}
	for i, c := range anchors {
		key, file := "", ""
		if r := c.Scene.Render; r != "" {
			if f, err := l.Scene3DFrameFile(id, r); err == nil {
				key, file = r, f
			}
		}
		if key == "" {
			f, p, err := l.scene3dFrameWait(ctx, id, *c.Scene.Cam, c.Scene.Frame, 6*time.Minute)
			if err != nil {
				set(func(j *Scene3DApplyJob) { j.Pins[i].Error = "before: " + err.Error() })
				continue
			}
			key, file = f.Key, p
		}
		set(func(j *Scene3DApplyJob) { j.Pins[i].BeforeKey, j.Pins[i].Before = key, file })
	}
	// 2. the scene built again from its script, here: the whole project is the script's, it cannot be sent to a builder.
	set(func(j *Scene3DApplyJob) { j.Phase = "build" })
	out, err := scene3dScript(ctx, s.Root, script)
	set(func(j *Scene3DApplyJob) { j.Output = s3Tail(out, 25) })
	if err != nil {
		fail("%v: %s", err, s3Tail(out, 8))
		return
	}
	hashAfter, err := scene3dHash(blend)
	if err != nil {
		fail("the script wrote no %s: %v", s.Blend, err)
		return
	}
	set(func(j *Scene3DApplyJob) { j.Changed, j.Hash, j.Phase = hashAfter != hashBefore, hashAfter, "model" })
	// 3. the model of the new scene, then the same views drawn from it.
	if err := l.scene3dWaitModel(ctx, id); err != nil {
		fail("the model of the changed scene did not build: %v", err)
		return
	}
	set(func(j *Scene3DApplyJob) { j.Phase = "after" })
	for i, c := range anchors {
		f, p, err := l.scene3dFrameWait(ctx, id, *c.Scene.Cam, c.Scene.Frame, 6*time.Minute)
		if err != nil {
			set(func(j *Scene3DApplyJob) { j.Pins[i].Error = "after: " + err.Error() })
			continue
		}
		set(func(j *Scene3DApplyJob) {
			j.Pins[i].AfterKey, j.Pins[i].After = f.Key, p
			j.Pins[i].SameAsPrev = f.Key == j.Pins[i].BeforeKey
		})
	}
	set(func(j *Scene3DApplyJob) { j.Status, j.Phase = "ready", "" })
}

func (l *Library) scene3dWaitModel(ctx context.Context, id string) error {
	deadline := time.Now().Add(scene3dModelWait)
	for {
		m, err := l.Scene3DModelOf(id)
		if err != nil {
			return err
		}
		switch m.Status {
		case Scene3DReady:
			return nil
		case Scene3DFailed:
			return errors.New(m.Error)
		}
		if time.Now().After(deadline) {
			return errors.New("timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// scene3dScript builds the scene from its script; tests replace it.
var scene3dScript = scene3dRunScript

// scene3dRunScript builds the scene: blender -b -y -P script, in the project's folder, on a slot of this machine, nice, with
// a time limit.
func scene3dRunScript(ctx context.Context, root, script string) (string, error) {
	bin := findBlender()
	if bin == "" {
		return "", errors.New("Blender is not installed on this machine")
	}
	release, err := heavy.Hold("blender-build", 20*time.Minute)
	if errors.Is(err, heavy.ErrBusy) {
		return "", errBlenderBusy
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, scene3dApplyTimeout)
	defer cancel()
	rel, err := filepath.Rel(root, script)
	if err != nil {
		rel = script
	}
	argv := heavy.Gentle([]string{bin, "-b", "-y", "-noaudio", "-P", rel})
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "OMP_NUM_THREADS=2")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var out tailBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out.b), fmt.Errorf("the script did not finish in %s and was stopped", scene3dApplyTimeout)
	}
	if err == nil && strings.Contains(string(out.b), "Traceback (most recent call last)") {
		err = errors.New("the script raised an error")
	}
	return string(out.b), err
}

// Scene3DResolve closes a pin with how it was fixed: the frames before and after and a word of the change.
func (l *Library) Scene3DResolve(id, commentID string, fix Scene3DFix) (DeckComment, error) {
	if err := l.requireScene3D(id); err != nil {
		return DeckComment{}, err
	}
	if !deckCommentIDRe.MatchString(commentID) {
		return DeckComment{}, deckErr(http400, "Not a comment id", "Это не номер комментария")
	}
	file := filepath.Join(l.commentsDir(id), commentID+".json")
	raw, err := readFileLimited(file, 64*1024)
	if err != nil {
		return DeckComment{}, deckErr(http404, "No such comment", "Нет такого комментария")
	}
	var c DeckComment
	if json.Unmarshal(raw, &c) != nil || c.ID != commentID || c.Scene == nil {
		return DeckComment{}, deckErr(http404, "No such pin", "Нет такого пина")
	}
	for _, k := range []string{fix.Before, fix.After} {
		if k != "" && !frameKeyRe.MatchString(k) {
			return DeckComment{}, deckErr(http400, "before and after are frame keys", "before и after — ключи кадров")
		}
	}
	note, _ := CleanText(fix.Note, 500)
	if utf8.RuneCountInString(note) == 0 && fix.After == "" {
		return DeckComment{}, deckErr(http400, "Say what was changed (note) or give the frame after", "Скажите, что изменено (note), или дайте кадр «после»")
	}
	by, err := l.author()
	if err != nil {
		return DeckComment{}, err
	}
	at := l.clock().UTC().Format(time.RFC3339)
	// The frames live in the cache folder of a scene's hash, which goes with the scene: the pin keeps its own copies.
	dir := l.deckFile(id, Scene3DCacheDir+"/fixes")
	for which, key := range map[string]string{"before": fix.Before, "after": fix.After} {
		if key == "" {
			continue
		}
		src := l.scene3dFindFrame(id, key)
		if src == "" {
			return DeckComment{}, deckErr(http404, "No such frame: "+key, "Нет такого кадра: "+key)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return DeckComment{}, err
		}
		if err := linkOrCopy(src, filepath.Join(dir, c.ID+"."+which+".jpg")); err != nil {
			return DeckComment{}, err
		}
	}
	c.Scene.Fix = &Scene3DFix{Before: fix.Before, After: fix.After, Note: note, At: at}
	c.Resolved, c.ResolvedBy, c.ResolvedAt = true, &by.Name, &at
	if err := l.writeCommentFile(id, c.ID, c); err != nil {
		return DeckComment{}, err
	}
	return c, nil
}

// scene3dFindFrame is the file of a cached frame in any of the scene's cache folders ("" when there is none).
func (l *Library) scene3dFindFrame(id, key string) string {
	if !frameKeyRe.MatchString(key) {
		return ""
	}
	root := l.deckFile(id, Scene3DCacheDir)
	names, _ := readNames(root)
	for _, n := range names {
		if f := filepath.Join(root, n, "frames", key+".jpg"); isRealFile(f) {
			return f
		}
	}
	return ""
}

// Scene3DFixFile is the picture of how a pin was fixed: which is "before" or "after".
func (l *Library) Scene3DFixFile(id, comment, which string) (string, error) {
	if err := l.requireScene3D(id); err != nil {
		return "", err
	}
	if !deckCommentIDRe.MatchString(comment) || (which != "before" && which != "after") {
		return "", deckErr(http400, "id and before|after are expected", "Нужны id и before|after")
	}
	f := l.deckFile(id, Scene3DCacheDir+"/fixes/"+comment+"."+which+".jpg")
	if !isRealFile(f) {
		return "", deckErr(http404, "No such picture", "Нет такой картинки")
	}
	return f, nil
}

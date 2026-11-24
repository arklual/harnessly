package design

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// scene3dRig is a library with one 3D scene over a fake blender-lab project, and a fake Blender that writes what the
// scripts would: the export's model and description, a frame's JPEG. calls counts its runs by kind.
type scene3dRig struct {
	l        *Library
	root     string // the fake blender-lab project
	blend    string
	export   atomic.Int32
	frame    atomic.Int32
	fail     atomic.Value // a failBox: the error runs answer
	builders atomic.Value // the builder the last run asked for
	packed   atomic.Int32
	outputs  atomic.Int32
}

type failBox struct{ err error }

var scene3dTestMeta = Scene3DMeta{Version: 1, FPS: 24, Start: 1, End: 240, Res: [2]int{1920, 1080}, Camera: "cam1",
	Objects: []Scene3DObject{{N: "car", T: "MESH", Tris: 1000}, {N: "wheel", T: "MESH", P: "car", Tris: 100}, {N: "cam1", T: "CAMERA"}},
	Bake:    map[string]Scene3DBake{"wheel": {M: "e", Rows: [][]float64{{1, 0, 0, 0, 0, 0, 0, 1, 1, 1}, {240, 0, 0, 0, 60, 0, 0, 1, 1, 1}}}},
	Cameras: []Scene3DCamera{{N: "cam1", Lens: 35, Fov: 40, Near: 0.1, Far: 100, Track: [][]float64{{1, 0, -5, 1, 0, 0, 1, 40}, {240, 10, -5, 1, 10, 0, 1, 40}}}}, Markers: []Scene3DMarker{}}

func newScene3DRig(t *testing.T) *scene3dRig {
	t.Helper()
	libRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(libRoot, "villa", ProjectDirname), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &scene3dRig{l: testLibrary(t, libRoot), root: t.TempDir()}
	// The rig's Blender is the fake below: the check for a Blender of the machine must not depend on the CI having one.
	t.Setenv("HARNESSLY_BLENDER", "/fake/blender")
	r.blend = filepath.Join(r.root, "out", "villa.blend")
	writeFile(t, r.blend, "BLENDER-v500 fake")
	writeFile(t, filepath.Join(r.root, "scenes", "villa.py"), "# scene")
	old := scene3dBlender
	scene3dBlender = func(ctx context.Context, run blenderRun, workdir string) (string, string, error) {
		if b, _ := r.fail.Load().(failBox); b.err != nil {
			return "", "", b.err
		}
		if run.Blend != r.blend {
			return "", "", errors.New("the wrong scene: " + run.Blend)
		}
		r.builders.Store(run.Builder)
		switch {
		case strings.Contains(strings.Join(run.Outputs, " "), "model.glb"):
			r.export.Add(1)
			raw, _ := json.Marshal(scene3dTestMeta)
			if err := os.WriteFile(filepath.Join(workdir, "model.glb"), []byte("glTF"), 0o600); err != nil {
				return "", "", err
			}
			return "", "", os.WriteFile(filepath.Join(workdir, "meta.json"), raw, 0o600)
		case strings.Contains(strings.Join(run.Outputs, " "), "frame.jpg"):
			r.frame.Add(1)
			return "", "farm-1", os.WriteFile(filepath.Join(workdir, "frame.jpg"), tinyJPEG(), 0o600)
		case strings.Contains(strings.Join(run.Outputs, " "), "frames.json"):
			r.outputs.Add(1)
			var sp struct {
				Frames []int
				Passes []string
				Masks  []string
			}
			_ = json.Unmarshal([]byte(run.Params[0]), &sp)
			if err := os.MkdirAll(filepath.Join(workdir, "out"), 0o700); err != nil {
				return "", "", err
			}
			put := func(name string) string {
				_ = os.WriteFile(filepath.Join(workdir, "out", name), tinyJPEG(), 0o600)
				return name
			}
			var rows []map[string]any
			for _, f := range sp.Frames {
				row := map[string]any{"frame": f}
				for _, p := range sp.Passes {
					ext := "png"
					if p == "beauty" || p == "clay" {
						ext = "jpg"
					}
					row[p] = put(fmt.Sprintf("%s_%04d.%s", p, f, ext))
				}
				rows = append(rows, row)
				for _, m := range sp.Masks {
					rows = append(rows, map[string]any{"frame": f, "object": m, "mask": put(fmt.Sprintf("mask_%s_%04d.png", m, f))})
				}
			}
			raw, _ := json.Marshal(rows)
			return "", "farm-2", os.WriteFile(filepath.Join(workdir, "out", "frames.json"), raw, 0o600)
		case strings.Contains(strings.Join(run.Outputs, " "), "pack.blend"):
			r.packed.Add(1)
			return "", "", os.WriteFile(filepath.Join(workdir, "pack.blend"), []byte("packed"), 0o600)
		}
		return "", "", errors.New("unexpected run")
	}
	t.Cleanup(func() {
		s3Wg.Wait()
		scene3dBlender = old
	})
	if err := r.l.InitScene3D("villa", Scene3DSettings{Root: r.root, Blend: "out/villa.blend", Script: "scenes/villa.py"}); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *scene3dRig) ready(t *testing.T) Scene3DState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := r.l.Scene3DStateOf("villa")
		if err != nil {
			t.Fatal(err)
		}
		if st.Model.Status == Scene3DReady {
			return st
		}
		if st.Model.Status == Scene3DFailed || time.Now().After(deadline) {
			t.Fatalf("model: %+v", st.Model)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *scene3dRig) frameDone(t *testing.T, cam Scene3DCam, frame int) Scene3DFrame {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := r.l.Scene3DFrameStart("villa", cam, frame, false, "")
		if err != nil {
			t.Fatal(err)
		}
		if f.Status == Scene3DReady || f.Status == Scene3DFailed || time.Now().After(deadline) {
			return f
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestScene3DSettingsAreChecked(t *testing.T) {
	r := newScene3DRig(t)
	if !r.l.IsScene3D("villa") || r.l.IsVideo("villa") || r.l.IsDeck("villa") {
		t.Fatal("kind")
	}
	outside := filepath.Join(t.TempDir(), "x.blend")
	writeFile(t, outside, "x")
	for name, s := range map[string]Scene3DSettings{
		"relative root": {V: 1, Root: "rel", Blend: "out/villa.blend"},
		"escape":        {V: 1, Root: r.root, Blend: "../" + filepath.Base(filepath.Dir(outside)) + "/x.blend"},
		"absolute":      {V: 1, Root: r.root, Blend: outside},
		"not a blend":   {V: 1, Root: r.root, Blend: "scenes/villa.py"},
		"no file":       {V: 1, Root: r.root, Blend: "out/none.blend"},
		"script escape": {V: 1, Root: r.root, Blend: "out/villa.blend", Script: "../x.py"},
		"wrong version": {V: 2, Root: r.root, Blend: "out/villa.blend"},
	} {
		if err := ValidateScene3DSettings(s); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// A link inside the project that leads out of it is refused.
	if err := os.Symlink(outside, filepath.Join(r.root, "out", "link.blend")); err == nil {
		if err := ValidateScene3DSettings(Scene3DSettings{V: 1, Root: r.root, Blend: "out/link.blend"}); err == nil {
			t.Error("a link out of the project accepted")
		}
	}
}

func TestScene3DModelIsBuiltOnceAndKeptByHash(t *testing.T) {
	r := newScene3DRig(t)
	st := r.ready(t)
	if st.Model.Meta == nil || len(st.Model.Meta.Objects) != 3 || st.Model.Bytes != 4 || st.Engine == "" || len(st.Model.Meta.Bake["wheel"].Rows) != 2 {
		t.Fatalf("%+v", st)
	}
	for i := 0; i < 3; i++ {
		if _, err := r.l.Scene3DStateOf("villa"); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.export.Load(); n != 1 {
		t.Errorf("exported %d times", n)
	}
	file, hash, err := r.l.Scene3DModelFile("villa", "")
	if err != nil || hash != st.Model.Hash || !strings.HasSuffix(file, "model.glb") {
		t.Fatalf("%s %s %v", file, hash, err)
	}
	if _, _, err := r.l.Scene3DModelFile("villa", "../../etc/passwd"); err == nil {
		t.Error("a path as a hash")
	}
	// The scene changed: a new hash, a new export; the old model stays for a moment.
	writeFile(t, r.blend, "BLENDER-v500 fake, edited")
	st2 := r.ready(t)
	if st2.Model.Hash == hash || r.export.Load() != 2 {
		t.Errorf("hash %s -> %s, %d exports", hash, st2.Model.Hash, r.export.Load())
	}
}

func TestScene3DFailedBuildSaysWhyAndRetries(t *testing.T) {
	r := newScene3DRig(t)
	r.fail.Store(failBox{errors.New("blender exploded")})
	var st Scene3DState
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		st, _ = r.l.Scene3DStateOf("villa")
		if st.Model.Status == Scene3DFailed {
			break
		}
	}
	if st.Model.Status != Scene3DFailed || !strings.Contains(st.Model.Error, "blender exploded") {
		t.Fatalf("%+v", st.Model)
	}
	r.fail.Store(failBox{})
	if _, err := r.l.Scene3DRebuild("villa", ""); err != nil {
		t.Fatal(err)
	}
	r.ready(t)
}

func TestScene3DFramePublicationRequiresMetadata(t *testing.T) {
	r := newScene3DRig(t)
	state := r.ready(t)
	cam := Scene3DCam{N: "cam1"}
	key := scene3dFrameKey(state.Model.Hash, scene3dEngine(), cam, 10)
	sidecar := filepath.Join(r.l.scene3dCache("villa", state.Model.Hash), "frames", key+".json")
	// A directory at the metadata destination deterministically rejects rename.
	if err := os.MkdirAll(sidecar, 0o700); err != nil {
		t.Fatal(err)
	}
	if f := r.frameDone(t, cam, 10); f.Status != Scene3DFailed || f.Error == "" {
		t.Fatalf("metadata failure reported as %+v", f)
	}
	if _, err := r.l.Scene3DFrameFile("villa", key); err == nil {
		t.Fatal("served an unpublished frame")
	}
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	if _, err := r.l.Scene3DFrameStart("villa", cam, 10, true, ""); err != nil {
		t.Fatal(err)
	}
	f := r.frameDone(t, cam, 10)
	if f.Status != Scene3DReady || f.Key != key {
		t.Fatalf("retry: %+v", f)
	}
	frames := r.l.scene3dFrames("villa", state.Model.Hash)
	if len(frames) != 1 || frames[0].Key != key || frames[0].Status != Scene3DReady {
		t.Fatalf("published frames: %+v", frames)
	}
}

func TestScene3DFrameIsCachedByViewAndFrame(t *testing.T) {
	r := newScene3DRig(t)
	r.ready(t)
	cam := Scene3DCam{N: "cam1"}
	f := r.frameDone(t, cam, 10)
	if f.Status != Scene3DReady || r.frame.Load() != 1 {
		t.Fatalf("first: %+v", f)
	}
	again, err := r.l.Scene3DFrameStart("villa", cam, 10, false, "")
	if err != nil || again.Key != f.Key || !again.Cached || again.Status != Scene3DReady {
		t.Fatalf("again: %+v %v", again, err)
	}
	if r.frame.Load() != 1 {
		t.Errorf("rendered %d times", r.frame.Load())
	}
	// A named camera needs no other numbers: they do not change the key.
	if same, _ := r.l.Scene3DFrameStart("villa", Scene3DCam{N: "cam1", Pos: [3]float64{9, 9, 9}, Fov: 3}, 10, false, ""); same.Key != f.Key {
		t.Error("a named camera's numbers moved the key")
	}
	if other := r.frameDone(t, cam, 11); other.Key == f.Key || r.frame.Load() != 2 {
		t.Errorf("another frame: %+v, %d renders", other, r.frame.Load())
	}
	free := Scene3DCam{Pos: [3]float64{4, -3, 1.0004}, Target: [3]float64{0, 0, 0.5}, Fov: 35}
	g := r.frameDone(t, free, 1)
	if g.Status != Scene3DReady || g.Cam == nil || g.Cam.Pos[2] != 1 {
		t.Fatalf("free view: %+v", g)
	}
	file, err := r.l.Scene3DFrameFile("villa", g.Key)
	if err != nil || !strings.HasPrefix(file, r.l.ArtifactDir("villa")) {
		t.Fatalf("%s %v", file, err)
	}
	st, _ := r.l.Scene3DStateOf("villa")
	if len(st.Frames) != 3 {
		t.Errorf("%d frames listed", len(st.Frames))
	}
	for name, c := range map[string]Scene3DCam{
		"no such camera": {N: "cam9"},
		"fov":            {Pos: [3]float64{1, 1, 1}, Fov: 400},
		"nan":            {Pos: [3]float64{1e9, 0, 0}, Fov: 40},
	} {
		if _, err := r.l.Scene3DFrameStart("villa", c, 1, false, ""); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := r.l.Scene3DFrameStart("villa", cam, 9999, false, ""); err == nil {
		t.Error("a frame past the end accepted")
	}
	if _, err := r.l.Scene3DFrameFile("villa", "../x"); err == nil {
		t.Error("a path as a key")
	}
}

func TestScene3DBusyMachineFailsTheFrameInWords(t *testing.T) {
	r := newScene3DRig(t)
	r.ready(t)
	r.fail.Store(failBox{errBlenderBusy})
	f := r.frameDone(t, Scene3DCam{N: "cam1"}, 5)
	if f.Status != Scene3DFailed || !strings.Contains(strings.ToLower(f.Error), "busy") && !strings.Contains(f.Error, "занята") {
		t.Fatalf("%+v", f)
	}
	r.fail.Store(failBox{})
	if again, _ := r.l.Scene3DFrameStart("villa", Scene3DCam{N: "cam1"}, 5, true, ""); again.Status == Scene3DFailed {
		t.Errorf("a retry stayed failed: %+v", again)
	}
	if f := r.frameDone(t, Scene3DCam{N: "cam1"}, 5); f.Status != Scene3DReady {
		t.Errorf("after retry: %+v", f)
	}
}

func TestScene3DPinKeepsObjectPointViewAndFrame(t *testing.T) {
	r := newScene3DRig(t)
	r.ready(t)
	f := r.frameDone(t, Scene3DCam{N: "cam1"}, 7)
	p := [3]float64{1.5, -2, 0.25}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 9, 9, 9}
	c, err := r.l.AddScene3DComment("villa", DeckCommentSpec{Text: "  Колесо слишком большое \r\n", Scene: &Scene3DAnchor{
		Object: "wheel", P: &p, Frame: 7, Render: f.Key,
		Cam: &Scene3DCam{N: "cam1", Pos: [3]float64{4, 4, 2}, Target: [3]float64{0, 0, 0.5}, Fov: 40}}}, jpeg)
	if err != nil {
		t.Fatal(err)
	}
	if c.View != DeckCommentScene3D || c.Label != "wheel" || c.Text != "Колесо слишком большое" || c.Scene == nil || !c.Scene.Shot ||
		c.Scene.Object != "wheel" || c.Scene.Frame != 7 || c.Scene.Cam.N != "cam1" || c.Scene.Render != f.Key || !strings.HasSuffix(c.Frame, f.Key+".jpg") {
		t.Fatalf("%+v %+v", c, c.Scene)
	}
	st, _ := r.l.Scene3DStateOf("villa")
	if len(st.Comments) != 1 || st.Comments[0].ID != c.ID || st.Comments[0].Scene.P == nil || st.Comments[0].Scene.P[0] != 1.5 {
		t.Fatalf("%+v", st.Comments)
	}
	if file, err := r.l.Scene3DShotFile("villa", c.ID); err != nil || !isRealFile(file) {
		t.Fatalf("shot: %s %v", file, err)
	}
	if _, err := r.l.ResolveDeckComment("villa", c.ID, true); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.l.Scene3DStateOf("villa"); !st.Comments[0].Resolved {
		t.Error("not resolved")
	}
	bad := map[string]*Scene3DAnchor{
		"no anchor":      {Frame: 1},
		"unknown object": {Object: "ghost", Frame: 1},
		"frame range":    {Object: "car", Frame: 9999},
		"nan point":      {Object: "car", P: &[3]float64{1e12, 0, 0}, Frame: 1},
		"bad fov":        {Object: "car", Frame: 1, Cam: &Scene3DCam{Pos: [3]float64{1, 1, 1}, Fov: 0}},
		"unknown camera": {Object: "car", Frame: 1, Cam: &Scene3DCam{N: "nope"}},
		"render key":     {Object: "car", Frame: 1, Render: "../../x"},
	}
	for name, a := range bad {
		if _, err := r.l.AddScene3DComment("villa", DeckCommentSpec{Text: "x", Scene: a}, nil); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := r.l.AddScene3DComment("villa", DeckCommentSpec{Text: " ", Scene: &Scene3DAnchor{Object: "car", Frame: 1}}, nil); err == nil {
		t.Error("an empty text accepted")
	}
	if _, err := r.l.AddScene3DComment("villa", DeckCommentSpec{Text: "x", Scene: &Scene3DAnchor{Object: "car", Frame: 1}}, []byte("not a jpeg")); err == nil {
		t.Error("a shot that is no JPEG accepted")
	}
	if _, err := r.l.AddScene3DComment("villa", DeckCommentSpec{Text: "x"}, nil); err == nil {
		t.Error("no anchor at all accepted")
	}
}

func TestScene3DEngineFollowsTheMachine(t *testing.T) {
	t.Setenv("HARNESSLY_SCENE3D_ENGINE", "eevee")
	if scene3dEngine() != "eevee" {
		t.Error("override")
	}
	t.Setenv("HARNESSLY_SCENE3D_ENGINE", "")
	if e := scene3dEngine(); e != "eevee" && e != "cycles" {
		t.Errorf("engine %q", e)
	}
}

func TestScene3DFrameKeepsTheBuilderAndWhereItRan(t *testing.T) {
	r := newScene3DRig(t)
	r.ready(t)
	deadline := time.Now().Add(5 * time.Second)
	var f Scene3DFrame
	for time.Now().Before(deadline) {
		var err error
		if f, err = r.l.Scene3DFrameStart("villa", Scene3DCam{N: "cam1"}, 3, false, "builder-7"); err != nil {
			t.Fatal(err)
		}
		if f.Status == Scene3DReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got, _ := r.builders.Load().(string); got != "builder-7" {
		t.Errorf("the run was asked for builder %q", got)
	}
	st, _ := r.l.Scene3DStateOf("villa")
	if len(st.Frames) != 1 || st.Frames[0].Where != "farm-1" {
		t.Errorf("frames: %+v", st.Frames)
	}
}

// A machine's own Blender runs in the workdir with the files named relative to it, and a missing one is said in words.
func TestScene3DLocalBlenderRunsInTheWorkdir(t *testing.T) {
	t.Setenv("HARNESSLY_HEAVY_DIR", t.TempDir())
	bin := filepath.Join(t.TempDir(), "blender")
	// A fake Blender: writes each file named after "--" into the current folder and prints what it was given.
	writeFile(t, bin, "#!/bin/sh\nwhile [ \"$1\" != \"--\" ]; do shift; done; shift\nfor f in \"$@\"; do echo x > \"$f\"; done\necho \"args: $*\"\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARNESSLY_BLENDER", bin)
	work := t.TempDir()
	blend := filepath.Join(t.TempDir(), "s.blend")
	writeFile(t, blend, "x")
	out, where, err := runBlender(context.Background(), blenderRun{Blend: blend, Script: []byte("#"), Params: []string{"made.txt"}, Outputs: []string{"made.txt"},
		Timeout: 20 * time.Second, SlotWait: 5 * time.Second, Tool: "blender-test"}, work)
	if err != nil || where != "" || !strings.Contains(out, "args: made.txt") {
		t.Fatalf("%q %q %v", out, where, err)
	}
	if !isRealFile(filepath.Join(work, "made.txt")) || !isRealFile(filepath.Join(work, "scene3d.py")) {
		t.Error("the run's files are not in the workdir")
	}
	t.Setenv("HARNESSLY_BLENDER", filepath.Join(t.TempDir(), "none"))
	if _, _, err := runBlender(context.Background(), blenderRun{Blend: blend, Script: []byte("#"), Timeout: time.Second, SlotWait: time.Second, Tool: "blender-test"}, work); err == nil {
		t.Error("a missing Blender ran")
	}
}

func (r *scene3dRig) pin(t *testing.T, text string) DeckComment {
	t.Helper()
	p := [3]float64{1, 2, 3}
	c, err := r.l.AddScene3DComment("villa", DeckCommentSpec{Text: text, Scene: &Scene3DAnchor{Object: "car", P: &p, Frame: 5,
		Cam: &Scene3DCam{Pos: [3]float64{4, 4, 2}, Target: [3]float64{0, 0, 1}, Fov: 40}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestScene3DApplyDrawsBeforeAndAfterAndTheFixClosesThePin(t *testing.T) {
	r := newScene3DRig(t)
	t.Setenv("HARNESSLY_BLENDER", "/fake/blender")
	r.ready(t)
	pin := r.pin(t, "Сделай машину ярче")
	done := r.pin(t, "Уже решено")
	if _, err := r.l.ResolveDeckComment("villa", done.ID, true); err != nil {
		t.Fatal(err)
	}
	var built atomic.Int32
	old := scene3dScript
	scene3dScript = func(ctx context.Context, root, script string) (string, error) {
		built.Add(1)
		if !strings.HasSuffix(script, "scenes/villa.py") || root != r.root {
			return "", errors.New("wrong script " + script)
		}
		return "built", os.WriteFile(r.blend, []byte("BLENDER-v500 changed by the script"), 0o600)
	}
	t.Cleanup(func() {
		s3Wg.Wait()
		scene3dScript = old
	})

	job, err := r.l.StartScene3DApply("villa", nil)
	if err != nil || len(job.Pins) != 1 || job.Pins[0].ID != pin.ID {
		t.Fatalf("%+v %v", job, err)
	}
	var got Scene3DApplyJob
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		got, _ = Scene3DApplyOf(job.ID)
		if got.Status != "building" {
			break
		}
	}
	if got.Status != "ready" || !got.Changed || built.Load() != 1 {
		t.Fatalf("%+v (%d builds)", got, built.Load())
	}
	p := got.Pins[0]
	if p.BeforeKey == "" || p.AfterKey == "" || p.BeforeKey == p.AfterKey || !isRealFile(p.Before) || !isRealFile(p.After) || p.Error != "" {
		t.Fatalf("pin %+v", p)
	}
	if _, err := r.l.Scene3DResolve("villa", pin.ID, Scene3DFix{}); err == nil {
		t.Error("a pin closed with nothing said")
	}
	if _, err := r.l.Scene3DResolve("villa", pin.ID, Scene3DFix{After: "../../x"}); err == nil {
		t.Error("a path as a frame key")
	}
	c, err := r.l.Scene3DResolve("villa", pin.ID, Scene3DFix{Before: p.BeforeKey, After: p.AfterKey, Note: "Краска ярче"})
	if err != nil || !c.Resolved || c.Scene.Fix == nil || c.Scene.Fix.After != p.AfterKey || c.Scene.Fix.Note != "Краска ярче" {
		t.Fatalf("%+v %v", c, err)
	}
	for _, which := range []string{"before", "after"} {
		if f, err := r.l.Scene3DFixFile("villa", pin.ID, which); err != nil || !isRealFile(f) {
			t.Errorf("%s: %s %v", which, f, err)
		}
	}
	if _, err := r.l.Scene3DFixFile("villa", pin.ID, "sideways"); err == nil {
		t.Error("an unknown picture")
	}
	// A scene without a script cannot be applied.
	if err := r.l.InitScene3D("villa", Scene3DSettings{Root: r.root, Blend: "out/villa.blend"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.l.StartScene3DApply("villa", nil); err == nil {
		t.Error("applied with no script")
	}
}

func TestSceneToolsServeOnlyTheirProject(t *testing.T) {
	r := newScene3DRig(t)
	r.ready(t)
	if (SceneTools{L: r.l}).Has("scene_document") || len((SceneTools{L: r.l}).Tools()) != 0 {
		t.Fatal("tools without a project")
	}
	tools := SceneTools{L: r.l, Project: "villa"}
	if got := r.l.SceneProjectOf(filepath.Join(r.l.Root(), "villa", "project")); got != "villa" {
		t.Errorf("project of a folder: %q", got)
	}
	if got := r.l.SceneProjectOf(t.TempDir()); got != "" {
		t.Errorf("a stranger's folder: %q", got)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools() {
		n, _ := tool.(*jsvalue.Object).Get("name")
		names[n.(string)] = true
	}
	for _, n := range []string{"scene_document", "scene_selection", "scene_comments", "scene_apply", "scene_view", "scene_render", "scene_clip", "scene_outputs", "scene_to_video"} {
		if !names[n] || !tools.Has(n) {
			t.Errorf("no tool %s", n)
		}
	}
	text := func(o *jsvalue.Object) string {
		c, _ := o.Get("content")
		first, _ := c.([]any)[0].(*jsvalue.Object).Get("text")
		return fmt.Sprint(first)
	}
	if doc := text(tools.Call(context.Background(), "scene_document", jsvalue.NewObject())); !strings.Contains(doc, "cam1") || !strings.Contains(doc, "wheel") {
		t.Errorf("document: %s", doc)
	}
	pin := r.pin(t, "Колесо больше")
	if list := text(tools.Call(context.Background(), "scene_comments", jsvalue.NewObject("open", true))); !strings.Contains(list, pin.ID) {
		t.Errorf("comments: %s", list)
	}
	// The viewer is not open: the agent is told so; once it reports, a command waits for it and is taken once.
	if msg := text(tools.Call(context.Background(), "scene_view", jsvalue.NewObject("object", "car"))); !strings.Contains(msg, "not have the scene open") {
		t.Errorf("view closed: %s", msg)
	}
	scene3dViewReport("villa", Scene3DViewReport{Object: "wheel", Frame: 7}, time.Now())
	if seen := text(tools.Call(context.Background(), "scene_selection", jsvalue.NewObject())); !strings.Contains(seen, "wheel") || !strings.Contains(seen, `true`) {
		t.Errorf("selection: %s", seen)
	}
	if msg := text(tools.Call(context.Background(), "scene_view", jsvalue.NewObject("pin", pin.ID))); !strings.Contains(msg, "Done") {
		t.Errorf("view open: %s", msg)
	}
	cmds := scene3dViewReport("villa", Scene3DViewReport{}, time.Now())
	if len(cmds) != 1 || cmds[0].Pin != pin.ID {
		t.Fatalf("cmds %+v", cmds)
	}
	if again := scene3dViewReport("villa", Scene3DViewReport{Acked: cmds[0].Seq}, time.Now()); len(again) != 0 {
		t.Errorf("a command taken came again: %+v", again)
	}
	if msg := text(tools.Call(context.Background(), "scene_view", jsvalue.NewObject())); !strings.Contains(msg, "give a camera") {
		t.Errorf("empty view: %s", msg)
	}
	img := text(tools.Call(context.Background(), "scene_render", jsvalue.NewObject("cam", "cam1", "frame", float64(3))))
	if !strings.Contains(img, ".jpg") {
		t.Errorf("render: %s", img)
	}
	if bad := text(tools.Call(context.Background(), "scene_render", jsvalue.NewObject("pos", []any{1.0, 2.0}))); !strings.Contains(bad, "give cam") {
		t.Errorf("bad render: %s", bad)
	}
}

func tinyImage() *stdimage.RGBA {
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 64, 36))
	for x := 0; x < 64; x++ {
		for y := 0; y < 36; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 7), 90, 255})
		}
	}
	return img
}

func tinyJPEG() []byte {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, tinyImage(), nil)
	return b.Bytes()
}

func needFFmpeg(t *testing.T) {
	t.Helper()
	if DeckFFmpeg() == "" {
		t.Skip("no ffmpeg on this machine")
	}
}

func (r *scene3dRig) clipDone(t *testing.T, sp Scene3DClipSpec) Scene3DClip {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		c, err := r.l.StartScene3DClip("villa", sp, false)
		if err != nil {
			t.Fatal(err)
		}
		if c.Status == Scene3DReady || c.Status == Scene3DFailed || time.Now().After(deadline) {
			return c
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestScene3DDraftClipIsCachedAndGoesIntoAVideo(t *testing.T) {
	needFFmpeg(t)
	r := newScene3DRig(t)
	r.ready(t)
	sp := Scene3DClipSpec{Cam: Scene3DCam{N: "cam1"}, Start: 1, End: 49, Step: 12}
	c := r.clipDone(t, sp)
	if c.Status != Scene3DReady || c.Frames != 5 || c.Origin.Kind != "draft" || c.Origin.Step != 12 || c.Origin.Cam.N != "cam1" || c.Origin.Where != "farm-1" {
		t.Fatalf("%+v", c)
	}
	if got := r.frame.Load(); got != 5 {
		t.Errorf("%d frames drawn", got)
	}
	dir, got, err := r.l.Scene3DClipDir("villa", c.Key)
	if err != nil || !isRealFile(filepath.Join(dir, "clip.mp4")) || !isRealFile(filepath.Join(dir, "f0005.jpg")) || got.Key != c.Key {
		t.Fatalf("%s %+v %v", dir, got, err)
	}
	// Again: from the cache, nothing drawn.
	if again, _ := r.l.StartScene3DClip("villa", sp, false); again.Status != Scene3DReady || r.frame.Load() != 5 {
		t.Errorf("again %+v, %d frames", again, r.frame.Load())
	}
	st, _ := r.l.Scene3DStateOf("villa")
	if len(st.Clips) != 1 || st.Clips[0].Key != c.Key {
		t.Errorf("clips in state: %+v", st.Clips)
	}
	// The step is chosen when none is given; too many frames for a draft are refused.
	if auto, err := r.l.StartScene3DClip("villa", Scene3DClipSpec{Cam: Scene3DCam{N: "cam1"}, Start: 1, End: 240}, false); err != nil || auto.Total > Scene3DClipMaxFrames || auto.Origin.Step < 2 {
		t.Errorf("auto step: %+v %v", auto, err)
	}
	if _, err := r.l.StartScene3DClip("villa", Scene3DClipSpec{Cam: Scene3DCam{N: "cam1"}, Start: 1, End: 240, Step: 1}, false); err == nil {
		t.Error("240 draft frames accepted")
	}
	if _, err := r.l.StartScene3DClip("villa", Scene3DClipSpec{Cam: Scene3DCam{N: "cam9"}, Start: 1, End: 9, Step: 4}, false); err == nil {
		t.Error("a camera the scene lacks accepted")
	}
	// Into a new video document: the frames, the MP4 and where they came from, and a scene that plays them.
	res, err := r.l.Scene3DClipToVideo("villa", c.Key, "", nil)
	if err != nil || !res.New || !r.l.IsVideo(res.Video) {
		t.Fatalf("%+v %v", res, err)
	}
	vdir := filepath.Join(r.l.ProjectDir(res.Video), "clips")
	subs, _ := os.ReadDir(vdir)
	if len(subs) != 1 {
		t.Fatalf("clips folder: %v", subs)
	}
	folder := filepath.Join(vdir, subs[0].Name())
	for _, f := range []string{"clip.mp4", "f0001.jpg", "f0005.jpg", "origin.json"} {
		if !isRealFile(filepath.Join(folder, f)) {
			t.Errorf("no %s in the video's clip folder", f)
		}
	}
	origin := readFile(t, filepath.Join(folder, "origin.json"))
	if !strings.Contains(origin, `"cam1"`) || !strings.Contains(origin, `"draft"`) || !strings.Contains(origin, `"frames": 5`) {
		t.Errorf("origin: %s", origin)
	}
	vs, err := r.l.VideoStateOf(res.Video)
	if err != nil || len(vs.Scenes) != 1 || vs.Scenes[0].Dur < VideoSceneDurMin {
		t.Fatalf("video scenes: %+v %v", vs.Scenes, err)
	}
	pages, _ := r.l.ReadModel(res.Video), 0
	board := readFile(t, filepath.Join(r.l.ProjectDir(res.Video), pageFileOf(pages.Pages[0].File)))
	if !strings.Contains(board, "tkVideo.register") || !strings.Contains(board, res.Folder) || strings.Contains(board, "<video") ||
		!strings.Contains(board, `<script src="./support.js">`) || !strings.Contains(board, "<x-dc>") || !strings.Contains(board, "data-dc-script") {
		t.Errorf("board: %s", board)
	}
	if _, err := r.l.Scene3DClipToVideo("villa", "../../x", "", nil); err == nil {
		t.Error("a path as a clip key")
	}
	if _, err := r.l.Scene3DClipToVideo("villa", c.Key, res.Video, func(string) bool { return false }); err == nil {
		t.Error("a video the person may not change taken")
	}
}

func TestScene3DFinalRenderStartsOnlyFromAConfirmedPlan(t *testing.T) {
	needFFmpeg(t)
	r := newScene3DRig(t)
	r.ready(t)
	var rented atomic.Int32
	oldPlan, oldRender := scene3dCloudPlan, scene3dCloudRender
	scene3dCloudPlan = func(ctx context.Context, root string, frames int) (string, error) {
		return fmt.Sprintf("dry run for %d frames at %s", frames, root), nil
	}
	scene3dCloudRender = func(ctx context.Context, root, pack string, start, end int, dest string) (string, error) {
		rented.Add(1)
		if !isRealFile(pack) {
			return "", errors.New("no packed scene")
		}
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return "", err
		}
		for i := start; i <= min(end, start+3); i++ {
			var b bytes.Buffer
			_ = png.Encode(&b, tinyImage())
			if err := os.WriteFile(filepath.Join(dest, fmt.Sprintf("f%04d.png", i)), b.Bytes(), 0o600); err != nil {
				return "", err
			}
		}
		return "LEFT: nothing", nil
	}
	t.Cleanup(func() {
		s3Wg.Wait()
		scene3dCloudPlan, scene3dCloudRender = oldPlan, oldRender
	})

	plan, err := r.l.Scene3DPlanFinal(context.Background(), "villa", Scene3DClipSpec{Cam: Scene3DCam{N: "cam1"}, Start: 1, End: 4})
	if err != nil || plan.Frames != 4 || plan.EstimateRub <= 0 || !strings.Contains(plan.Dry, "dry run for 4 frames") || plan.ID == "" {
		t.Fatalf("%+v %v", plan, err)
	}
	// 4 frames: 0.4 h of setup and 4 × 8 s, at 290 ₽ an hour.
	hours := 0.4 + 4*8.0/3600
	if want := float64(int(hours*290 + 0.5)); plan.EstimateRub != want {
		t.Errorf("estimate %v, want %v", plan.EstimateRub, want)
	}
	if rented.Load() != 0 {
		t.Fatal("a plan rented a GPU")
	}
	if _, err := r.l.StartScene3DFinal("villa", plan.ID, false); err == nil || rented.Load() != 0 {
		t.Fatalf("a render started without the person's yes: %v", err)
	}
	// An unconfirmed start did not use up the plan; a wrong plan and another scene's plan are refused.
	if _, err := r.l.StartScene3DFinal("villa", "deadbeef", true); err == nil {
		t.Error("an unknown plan accepted")
	}
	if _, err := r.l.Scene3DPlanFinal(context.Background(), "villa", Scene3DClipSpec{Cam: Scene3DCam{N: "cam1"}, Start: 1, End: 999}); err == nil {
		t.Error("frames past the scene's end planned")
	}
	job, err := r.l.StartScene3DFinal("villa", plan.ID, true)
	if err != nil || job.Status != "building" {
		t.Fatalf("%+v %v", job, err)
	}
	var done Scene3DFinalJob
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		for _, j := range Scene3DFinals("villa") {
			if j.ID == job.ID {
				done = j
			}
		}
		if done.Status != "" && done.Status != "building" {
			break
		}
	}
	if done.Status != "ready" || done.Clip == "" || rented.Load() != 1 || r.packed.Load() != 1 {
		t.Fatalf("%+v (rented %d, packed %d)", done, rented.Load(), r.packed.Load())
	}
	_, clip, err := r.l.Scene3DClipDir("villa", done.Clip)
	if err != nil || clip.Origin.Kind != "final" || clip.Origin.Where == "" || clip.Origin.Cam.N != "cam1" || clip.Origin.Start != 1 || clip.Origin.End != 4 {
		t.Fatalf("%+v %v", clip, err)
	}
	// A plan is confirmed once.
	if _, err := r.l.StartScene3DFinal("villa", plan.ID, true); err == nil || rented.Load() != 1 {
		t.Errorf("a plan used twice: %v (rented %d)", err, rented.Load())
	}
	// A scene that changed after the plan needs a new plan.
	p2, _ := r.l.Scene3DPlanFinal(context.Background(), "villa", Scene3DClipSpec{Cam: Scene3DCam{N: "cam1"}, Start: 1, End: 4})
	writeFile(t, r.blend, "BLENDER-v500 fake, edited after the plan")
	r.ready(t)
	if _, err := r.l.StartScene3DFinal("villa", p2.ID, true); err == nil || rented.Load() != 1 {
		t.Errorf("a stale plan started: %v (rented %d)", err, rented.Load())
	}
}

func TestScene3DOutputsMakeOneManifestForAGenerator(t *testing.T) {
	r := newScene3DRig(t)
	r.ready(t)
	sp := Scene3DOutputsSpec{Cam: Scene3DCam{N: "cam1"}, Frames: []int{240, 1, 120}, Masks: []string{"wheel"}}
	var o Scene3DOutputs
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		var err error
		if o, err = r.l.StartScene3DOutputs("villa", sp, false); err != nil {
			t.Fatal(err)
		}
		if o.Status == Scene3DReady || o.Status == Scene3DFailed {
			break
		}
	}
	if o.Status != Scene3DReady || o.Manifest == nil {
		t.Fatalf("%+v", o)
	}
	m := o.Manifest
	if m.V != 1 || m.Scene.Project != "villa" || m.Camera.N != "cam1" || len(m.Keyframes) != 3 || m.Keyframes[0].Frame != 1 || m.Keyframes[2].Frame != 240 ||
		len(m.Masks) != 3 || m.Masks[0].Object != "wheel" || m.Where != "farm-2" || m.DepthFar != Scene3DOutputsFar || m.CameraPath != Scene3DCameraPathFile {
		t.Fatalf("manifest %+v", m)
	}
	k := m.Keyframes[1]
	if k.Beauty != "beauty_0120.jpg" || k.Clay != "clay_0120.jpg" || k.Depth != "depth_0120.png" || k.Normals != "normals_0120.png" || k.Seconds < 4.9 || k.Seconds > 5.0 {
		t.Errorf("keyframe %+v", k)
	}
	dir, _, err := r.l.Scene3DOutputsFolder("villa", o.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{Scene3DManifestFile, Scene3DCameraPathFile, "beauty_0001.jpg", "depth_0240.png", "mask_wheel_0120.png"} {
		if !isRealFile(filepath.Join(dir, f)) {
			t.Errorf("no %s in the outputs", f)
		}
	}
	var path Scene3DCameraPath
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, Scene3DCameraPathFile))), &path); err != nil || len(path.Samples) != 2 || path.Samples[1].Frame != 240 || path.Camera != "cam1" {
		t.Errorf("camera path %+v %v", path, err)
	}
	if left, _ := os.ReadDir(dir); func() bool {
		for _, e := range left {
			if e.Name() == "out" || e.Name() == "scene3d.py" || e.Name() == "scene.blend" {
				return true
			}
		}
		return false
	}() {
		t.Error("the working files stayed in the outputs")
	}
	// Again: from the cache.
	if again, _ := r.l.StartScene3DOutputs("villa", sp, false); again.Status != Scene3DReady || r.outputs.Load() != 1 {
		t.Errorf("again %+v, %d runs", again, r.outputs.Load())
	}
	if st, _ := r.l.Scene3DStateOf("villa"); len(st.Outputs) != 1 {
		t.Errorf("outputs in state: %+v", st.Outputs)
	}
	if f, err := r.l.Scene3DOutputFile("villa", o.Key, "beauty_0001.jpg"); err != nil || !isRealFile(f) { // gitleaks:allow — an output file name, not a key
		t.Errorf("file: %s %v", f, err)
	}
	for _, bad := range []string{"../meta.json", "a/b.png", "..", ""} {
		if _, err := r.l.Scene3DOutputFile("villa", o.Key, bad); err == nil {
			t.Errorf("file %q served", bad)
		}
	}
	for name, s := range map[string]Scene3DOutputsSpec{
		"frame past the end": {Cam: Scene3DCam{N: "cam1"}, Frames: []int{999}},
		"five key frames":    {Cam: Scene3DCam{N: "cam1"}, Frames: []int{1, 2, 3, 4, 5}},
		"unknown pass":       {Cam: Scene3DCam{N: "cam1"}, Frames: []int{1}, Passes: []string{"magic"}},
		"mask of a camera":   {Cam: Scene3DCam{N: "cam1"}, Frames: []int{1}, Masks: []string{"cam1"}},
		"mask of nothing":    {Cam: Scene3DCam{N: "cam1"}, Frames: []int{1}, Masks: []string{"ghost"}},
		"no such camera":     {Cam: Scene3DCam{N: "cam9"}, Frames: []int{1}},
	} {
		if _, err := r.l.StartScene3DOutputs("villa", s, false); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

type fakeGen struct {
	mu       sync.Mutex
	started  []Scene3DGenRequest
	estimate float64
	video    string // an MP4 to put in CopyTo when the job is done
	done     bool
}

func (g *fakeGen) Estimate(ctx context.Context, r Scene3DGenRequest) (float64, string, error) {
	return g.estimate, "fake price", nil
}

func (g *fakeGen) Start(r Scene3DGenRequest) (Scene3DGenJob, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.started = append(g.started, r)
	return Scene3DGenJob{ID: "gen-1", State: "running", EstimateUSD: g.estimate}, nil
}

func (g *fakeGen) Get(id string) (Scene3DGenJob, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.started) == 0 {
		return Scene3DGenJob{}, false
	}
	if !g.done {
		return Scene3DGenJob{ID: id, State: "running"}, true
	}
	r := g.started[0]
	copied := filepath.Join(r.CopyTo, "clip-1.mp4")
	origin := copied + ".origin.json"
	_ = os.MkdirAll(r.CopyTo, 0o700)
	data, _ := os.ReadFile(g.video)
	_ = os.WriteFile(copied, data, 0o600)
	_ = os.WriteFile(origin, []byte(`{"provider":"higgsfield","model":"kling","costUsd":1.2,"requestId":"r-1"}`), 0o600)
	return Scene3DGenJob{ID: id, State: "done", EstimateUSD: g.estimate, Videos: []Scene3DGenVideo{{Path: copied, Copy: copied, OriginFile: origin}}}, true
}

func TestScene3DGenerationSpendsOnlyWhenConfirmedAndTakesTheClipIn(t *testing.T) {
	needFFmpeg(t)
	r := newScene3DRig(t)
	r.ready(t)
	var o Scene3DOutputs
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		o, _ = r.l.StartScene3DOutputs("villa", Scene3DOutputsSpec{Cam: Scene3DCam{N: "cam1"}, Frames: []int{1, 120, 240}}, false)
		if o.Status == Scene3DReady {
			break
		}
	}
	if o.Status != Scene3DReady {
		t.Fatalf("%+v", o)
	}
	params := Scene3DGenParams{Outputs: o.Key, Model: "kling", Prompt: "The car drives past", Seconds: 5}
	// No generator: said in words.
	if _, _, err := r.l.Scene3DGenEstimate(context.Background(), "villa", params); err == nil {
		t.Error("an estimate with no generator")
	}
	video := filepath.Join(t.TempDir(), "gen.mp4")
	if out, err := exec.Command(DeckFFmpeg(), "-y", "-f", "lavfi", "-i", "testsrc=size=160x90:rate=12:duration=1", "-pix_fmt", "yuv420p", video).CombinedOutput(); err != nil {
		t.Skipf("no way to make a test video: %v %s", err, out)
	}
	g := &fakeGen{estimate: 1.2, video: video}
	SetScene3DGenerator(g)
	t.Cleanup(func() {
		s3Wg.Wait()
		SetScene3DGenerator(nil)
	})
	usd, note, err := r.l.Scene3DGenEstimate(context.Background(), "villa", params)
	if err != nil || usd != 1.2 || note == "" || len(g.started) != 0 {
		t.Fatalf("%v %q %v (%d started)", usd, note, err, len(g.started))
	}
	if _, err := r.l.StartScene3DGen("villa", params, false); err == nil || len(g.started) != 0 {
		t.Fatalf("started without the person's yes: %v", err)
	}
	if _, err := r.l.StartScene3DGen("villa", Scene3DGenParams{Outputs: o.Key, Model: "kling"}, true); err == nil {
		t.Error("started with no prompt")
	}
	if _, err := r.l.StartScene3DGen("villa", Scene3DGenParams{Outputs: "ffffffffffffffffffff", Model: "kling", Prompt: "x"}, true); err == nil {
		t.Error("started on outputs there are none of")
	}
	gen, err := r.l.StartScene3DGen("villa", params, true)
	if err != nil || gen.ID != "gen-1" || len(g.started) != 1 {
		t.Fatalf("%+v %v", gen, err)
	}
	req := g.started[0]
	dir, _, _ := r.l.Scene3DOutputsFolder("villa", o.Key)
	if len(req.Frames) != 2 || !strings.HasSuffix(req.Frames[0], "beauty_0001.jpg") || !strings.HasSuffix(req.Frames[1], "beauty_0240.jpg") || req.Owner != "design:villa" ||
		req.CopyTo != filepath.Join(dir, "generated") || req.Seconds != 5 || req.Origin["scene"] != "villa" || req.Origin["outputs"] != o.Key {
		t.Fatalf("request %+v", req)
	}
	if !req.SaveOK(filepath.Join(req.CopyTo, "x.mp4")) || req.SaveOK(filepath.Join(dir, "..", "x.mp4")) || req.SaveOK("/etc/passwd") ||
		!req.FrameOK(req.Frames[0]) || req.FrameOK(filepath.Join(r.root, "out", "villa.blend")) {
		t.Error("the generator's file limits are wrong")
	}
	// While it runs the state says so; once done the clip is taken in, with the generator's own origin kept.
	if st, _ := r.l.Scene3DStateOf("villa"); len(st.Gens) != 1 || st.Gens[0].State != "running" || !st.Generator {
		t.Fatalf("running: %+v", st.Gens)
	}
	g.mu.Lock()
	g.done = true
	g.mu.Unlock()
	var clipKey string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		st, _ := r.l.Scene3DStateOf("villa")
		if len(st.Gens) == 1 && st.Gens[0].Clip != "" {
			clipKey = st.Gens[0].Clip
			break
		}
	}
	if clipKey == "" {
		t.Fatal("the generated clip was not taken in")
	}
	cdir, clip, err := r.l.Scene3DClipDir("villa", clipKey)
	if err != nil || clip.Origin.Kind != "generated" || clip.Frames < 10 || !strings.Contains(string(clip.Origin.Generator), `"higgsfield"`) || clip.Origin.Outputs != o.Key ||
		!isRealFile(filepath.Join(cdir, "clip.mp4")) || !isRealFile(filepath.Join(cdir, "f0001.jpg")) {
		t.Fatalf("%+v %v", clip, err)
	}
	// And it goes into a video like any clip, the generator's origin with it.
	res, err := r.l.Scene3DClipToVideo("villa", clipKey, "", nil)
	if err != nil || !r.l.IsVideo(res.Video) {
		t.Fatalf("%+v %v", res, err)
	}
	subs, _ := os.ReadDir(filepath.Join(r.l.ProjectDir(res.Video), "clips"))
	origin := readFile(t, filepath.Join(r.l.ProjectDir(res.Video), "clips", subs[0].Name(), "origin.json"))
	if len(subs) != 1 || !strings.Contains(origin, "generated") || !strings.Contains(origin, "higgsfield") {
		t.Errorf("origin: %s", origin)
	}
}

// A job that waits for its frames (a clip, scene_apply) is not failed by a busy machine: it waits its turn.
func TestScene3DFrameWaitOutlastsABusyMachine(t *testing.T) {
	r := newScene3DRig(t)
	r.ready(t)
	r.fail.Store(failBox{errBlenderBusy})
	go func() {
		time.Sleep(300 * time.Millisecond)
		r.fail.Store(failBox{})
	}()
	f, file, err := r.l.scene3dFrameWait(context.Background(), "villa", Scene3DCam{N: "cam1"}, 4, 20*time.Second)
	if err != nil || f.Status != Scene3DReady || !isRealFile(file) {
		t.Fatalf("%+v %s %v", f, file, err)
	}
	// With no end to the busyness it gives up at its own deadline, in the machine's words.
	r.fail.Store(failBox{errBlenderBusy})
	if _, _, err := r.l.scene3dFrameWait(context.Background(), "villa", Scene3DCam{N: "cam1"}, 5, 1500*time.Millisecond); err == nil {
		t.Error("a busy machine for ever ended well")
	}
}

func TestScene3DDiscoverFindsProjectsAroundTheLibrary(t *testing.T) {
	parent := t.TempDir()
	lib := filepath.Join(parent, "design-artifacts")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	l := testLibrary(t, lib)
	writeFile(t, filepath.Join(parent, "lab", "scenes", "car.py"), "# car")
	writeFile(t, filepath.Join(parent, "lab", "scenes", "notes.txt"), "x")
	writeFile(t, filepath.Join(parent, "lab", "out", "old.blend"), "old")
	writeFile(t, filepath.Join(parent, "lab", "out", "car.blend"), "new")
	writeFile(t, filepath.Join(parent, "lab", "out", "car.blend1"), "backup")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(parent, "lab", "out", "old.blend"), old, old); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(parent, "stray", "thing.blend"), "x")
	writeFile(t, filepath.Join(parent, "plain", "readme.md"), "x")
	writeFile(t, filepath.Join(parent, ".hidden", "scenes", "a.py"), "x")
	got, err := l.Scene3DDiscover("")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, p.Name)
	}
	if len(got) != 2 || got[0].Name != "lab" || got[1].Name != "stray" {
		t.Fatalf("found %v", names)
	}
	lab := got[0]
	if len(lab.Blends) != 2 || lab.Blends[0] != "out/car.blend" || lab.Blends[1] != "out/old.blend" || len(lab.Scripts) != 1 || lab.Scripts[0] != "scenes/car.py" || lab.Root != filepath.Join(parent, "lab") {
		t.Errorf("lab: %+v", lab)
	}
	if got[1].Blends[0] != "./thing.blend" && got[1].Blends[0] != "thing.blend" {
		t.Errorf("stray: %+v", got[1])
	}
	// A folder typed in: that one only; a relative path or a folder with nothing is refused in words.
	one, err := l.Scene3DDiscover(filepath.Join(parent, "lab"))
	if err != nil || len(one) != 1 || one[0].Name != "lab" {
		t.Fatalf("%+v %v", one, err)
	}
	for _, bad := range []string{"lab", filepath.Join(parent, "plain"), filepath.Join(parent, "none")} {
		if _, err := l.Scene3DDiscover(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	// A new scene from the pick: the document is a 3D scene, named after the .blend when no title is given.
	p, err := l.CreateScene3D("", "", Scene3DSettings{Root: lab.Root, Blend: lab.Blends[0], Script: lab.Scripts[0]})
	if err != nil || !l.IsScene3D(p.ID) || p.Title != "car" {
		t.Fatalf("%+v %v", p, err)
	}
	if a := l.ReadArtifact(p.ID); a == nil {
		t.Error("the new scene is not in the library listing")
	}
	if _, err := l.CreateScene3D("x", "", Scene3DSettings{Root: lab.Root, Blend: "out/none.blend"}); err == nil {
		t.Error("a scene over a missing .blend made")
	}
	if left := l.ListArtifacts(); len(left) != 1 {
		t.Errorf("a refused scene left a project: %d listed", len(left))
	}
}

// The agent's `scene3d add`: a rebuilt .blend is the same document, a project the brief made becomes the scene, a .blend from
// elsewhere is copied into the project.
func TestAddScene3DKeepsOneDocumentPerBlend(t *testing.T) {
	parent := t.TempDir()
	lib := filepath.Join(parent, "design-artifacts")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	l := testLibrary(t, lib)
	lab := filepath.Join(parent, "lab")
	writeFile(t, filepath.Join(lab, "scenes", "car.py"), "# car")
	writeFile(t, filepath.Join(lab, "out", "car.blend"), "v1")
	first, existed, err := l.AddScene3D("", Scene3DAdd{Root: lab, Blend: "out/car.blend"})
	if err != nil || existed || !l.IsScene3D(first.ID) {
		t.Fatalf("first: %+v %v %v", first, existed, err)
	}
	// Rebuilt: the same document, the script is learnt, an absolute path is the same file.
	again, existed, err := l.AddScene3D("", Scene3DAdd{Root: lab, Blend: filepath.Join(lab, "out", "car.blend"), Script: "scenes/car.py"})
	if err != nil || !existed || again.ID != first.ID || again.Title != first.Title {
		t.Fatalf("again: %+v %v %v", again, existed, err)
	}
	if got, _ := l.ReadScene3DSettings(first.ID); got.Script != "scenes/car.py" {
		t.Errorf("script not kept: %+v", got)
	}
	// The project the brief made becomes the scene.
	made, err := l.CreateProject("desert drive")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(lab, "out", "desert.blend"), "x")
	got, existed, err := l.AddScene3D("", Scene3DAdd{Root: lab, Blend: "out/desert.blend", Project: made.ID})
	if err != nil || existed || got.ID != made.ID || !l.IsScene3D(made.ID) {
		t.Fatalf("project: %+v %v %v", got, existed, err)
	}
	// An attachment: copied into out/, then the same file is the same document.
	att := filepath.Join(parent, "uploads", "villa.blend")
	writeFile(t, att, "villa")
	v, existed, err := l.AddScene3D("", Scene3DAdd{File: att, Root: lab})
	if err != nil || existed || !l.IsScene3D(v.ID) {
		t.Fatalf("file: %+v %v %v", v, existed, err)
	}
	if b, err := os.ReadFile(filepath.Join(lab, "out", "villa.blend")); err != nil || string(b) != "villa" {
		t.Errorf("not copied: %q %v", b, err)
	}
	if v2, existed, err := l.AddScene3D("", Scene3DAdd{File: att, Root: lab}); err != nil || !existed || v2.ID != v.ID {
		t.Errorf("file again: %+v %v %v", v2, existed, err)
	}
	for _, bad := range []Scene3DAdd{{File: filepath.Join(parent, "uploads", "m.glb"), Root: lab}, {File: "rel.blend", Root: lab}, {Root: lab, Blend: "out/none.blend"}} {
		if _, _, err := l.AddScene3D("", bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

// A scene that keeps textures beside it is run where its project is; a self-contained .blend may go to a builder.
func TestScene3DNeedsProjectWhenTexturesLieBesideIt(t *testing.T) {
	lab := t.TempDir()
	writeFile(t, filepath.Join(lab, "out", "a.blend"), "x")
	if scene3dNeedsProject(filepath.Join(lab, "out", "a.blend")) {
		t.Error("a lone .blend was kept at home")
	}
	if err := os.MkdirAll(filepath.Join(lab, "assets", "wood"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !scene3dNeedsProject(filepath.Join(lab, "out", "a.blend")) {
		t.Error("a .blend beside assets/ was sent away from its textures")
	}
	deep := t.TempDir()
	writeFile(t, filepath.Join(deep, "a", "b", "c", "x.blend"), "x")
	if scene3dNeedsProject(filepath.Join(deep, "a", "b", "c", "x.blend")) {
		t.Error("a far folder named nothing special kept at home")
	}
}

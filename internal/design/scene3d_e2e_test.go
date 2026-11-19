package design

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestScene3DWithRealBlender walks the agent's way through a real scene: a pin, the script edited, scene_apply (the scene
// built again by its script, the pin's view drawn before and after), the pin closed with both frames. It needs a real Blender
// and takes minutes, so it runs only when HARNESSLY_E2E_BLENDER names a copy of a blender-lab project that holds
// scenes/retro_convertible.py (it builds out/retro_convertible_clay.blend); the copy is copied again, nothing is changed in it.
func TestScene3DWithRealBlender(t *testing.T) {
	src := os.Getenv("HARNESSLY_E2E_BLENDER")
	if src == "" {
		t.Skip("HARNESSLY_E2E_BLENDER names a blender-lab project copy to run against a real Blender")
	}
	root := t.TempDir()
	if out, err := exec.Command("cp", "-a", src+"/.", root).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, out)
	}
	libRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(libRoot, "villa", ProjectDirname), 0o755); err != nil {
		t.Fatal(err)
	}
	l := testLibrary(t, libRoot)
	if err := l.InitScene3D("villa", Scene3DSettings{Root: root, Blend: "out/retro_convertible_clay.blend", Script: "scenes/retro_convertible.py"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s3Wg.Wait)
	var st Scene3DState
	for deadline := time.Now().Add(12 * time.Minute); time.Now().Before(deadline); time.Sleep(3 * time.Second) {
		st, _ = l.Scene3DStateOf("villa")
		if st.Model.Status == Scene3DReady || st.Model.Status == Scene3DFailed {
			break
		}
	}
	if st.Model.Status != Scene3DReady {
		t.Fatalf("model: %+v", st.Model)
	}
	t.Logf("model %d bytes, %d objects, %d tris out", st.Model.Bytes, len(st.Model.Meta.Objects), st.Model.Meta.Stats.TrisOut)
	p := [3]float64{0.5, 0, 1}
	pin, err := l.AddScene3DComment("villa", DeckCommentSpec{Text: "Кузов слишком светлый: сделай краску темнее", Scene: &Scene3DAnchor{Object: "body", P: &p, Frame: 30,
		Cam: &Scene3DCam{N: "cam1_hero"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The agent edits the script: the paint of the body.
	script := filepath.Join(root, "scenes", "retro_convertible.py")
	raw, _ := os.ReadFile(script)
	edited := strings.Replace(string(raw), `paint=K.mat("paint", 0.78, 0.32, coat=0.4)`, `paint=K.mat("paint", 0.12, 0.32, coat=0.4)`, 1)
	if edited == string(raw) {
		t.Fatal("the paint line is not in the script")
	}
	if err := os.WriteFile(script, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := l.StartScene3DApply("villa", nil)
	if err != nil || len(job.Pins) != 1 {
		t.Fatalf("%+v %v", job, err)
	}
	var got Scene3DApplyJob
	for deadline := time.Now().Add(25 * time.Minute); time.Now().Before(deadline); time.Sleep(3 * time.Second) {
		got, _ = Scene3DApplyOf(job.ID)
		if got.Status != "building" {
			break
		}
	}
	t.Logf("apply: %s changed=%v pins=%+v output=%q", got.Status, got.Changed, got.Pins, got.Output)
	if got.Status != "ready" || !got.Changed || got.Pins[0].Before == "" || got.Pins[0].After == "" || got.Pins[0].Error != "" {
		t.Fatalf("%+v", got)
	}
	b, _ := os.ReadFile(got.Pins[0].Before)
	a, _ := os.ReadFile(got.Pins[0].After)
	if string(a) == string(b) {
		t.Error("the frame after is the same as before: the edit did not show")
	}
	c, err := l.Scene3DResolve("villa", pin.ID, Scene3DFix{Before: got.Pins[0].BeforeKey, After: got.Pins[0].AfterKey, Note: "Краска кузова темнее"})
	if err != nil || !c.Resolved {
		t.Fatalf("%+v %v", c, err)
	}
	bf, _ := l.Scene3DFixFile("villa", pin.ID, "before")
	af, _ := l.Scene3DFixFile("villa", pin.ID, "after")
	t.Logf("BEFORE %s", bf)
	t.Logf("AFTER %s", af)
	if out := os.Getenv("HARNESSLY_E2E_KEEP"); out != "" {
		_ = exec.Command("cp", bf, filepath.Join(out, "before.jpg")).Run()
		_ = exec.Command("cp", af, filepath.Join(out, "after.jpg")).Run()
	}
}

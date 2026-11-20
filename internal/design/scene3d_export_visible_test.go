package design

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A scene is saved at one frame, and the model is that frame: what the frame shows as nothing (hide_render keyed on
// later, a curve that grows by its bevel factors, a ring scaled up from zero) must still be in the model, with a track of when
// it is on. Runs against a real Blender (HARNESSLY_BLENDER or one on the PATH); skipped without one.
const scene3dHiddenScene = `
import bpy
bpy.ops.wm.read_factory_settings(use_empty=True)
sc = bpy.context.scene
sc.frame_start, sc.frame_end = 1, 60
def cube(name, x):
    bpy.ops.mesh.primitive_cube_add(location=(x, 0, 0))
    o = bpy.context.object
    o.name = name
    return o
cube("always", 0)
late = cube("late_letter", 3)
late.hide_render = True
late.keyframe_insert("hide_render", frame=1)
sc.frame_set(30)
late.hide_render = False
late.keyframe_insert("hide_render", frame=30)
ring = cube("ring", 6)
ring.scale = (0, 0, 0)
ring.keyframe_insert("scale", frame=1)
ring.scale = (1, 1, 1)
ring.keyframe_insert("scale", frame=20)
cu = bpy.data.curves.new("thread_data", "CURVE")
cu.dimensions = "3D"
cu.bevel_depth = 0.05
sp = cu.splines.new("POLY")
sp.points.add(3)
for i, p in enumerate(sp.points):
    p.co = (i, 2, 0, 1)
th = bpy.data.objects.new("thread", cu)
sc.collection.objects.link(th)
cu.bevel_factor_end = 0.0
cu.keyframe_insert("bevel_factor_end", frame=1)
cu.bevel_factor_end = 1.0
cu.keyframe_insert("bevel_factor_end", frame=40)
sc.frame_set(1)
bpy.ops.wm.save_as_mainfile(filepath=%q)
`

func TestScene3DExportShowsWhatTheSavedFrameHides(t *testing.T) {
	blender := findBlender()
	if blender == "" {
		t.Skip("no Blender on this machine")
	}
	dir := t.TempDir()
	blend := filepath.Join(dir, "hidden.blend")
	gen := filepath.Join(dir, "gen.py")
	if err := os.WriteFile(gen, []byte(replaceQ(scene3dHiddenScene, blend)), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := ctxTimeout(3 * time.Minute)
		defer cancel()
		if out, err := exec.CommandContext(ctx, blender, args...).CombinedOutput(); err != nil {
			t.Fatalf("blender %v: %v\n%s", args, err, out)
		}
	}
	run("-b", "-noaudio", "-P", gen)
	exp := filepath.Join(dir, "export.py")
	if err := os.WriteFile(exp, []byte(scene3dExportPy), 0o600); err != nil {
		t.Fatal(err)
	}
	run("-b", "-noaudio", blend, "-P", exp, "--", filepath.Join(dir, "m.glb"), filepath.Join(dir, "m.json"), "100000", "512")

	// every object is a node with a mesh in the model, the saved frame notwithstanding
	glb, err := os.ReadFile(filepath.Join(dir, "m.glb"))
	if err != nil || len(glb) < 20 {
		t.Fatalf("model: %v", err)
	}
	var g struct {
		Nodes []struct {
			Name string `json:"name"`
			Mesh *int   `json:"mesh"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(glb[20:20+binary.LittleEndian.Uint32(glb[12:16])], &g); err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Mesh != nil {
			has[n.Name] = true
		}
	}
	for _, name := range []string{"always", "late_letter", "ring", "thread"} {
		if !has[name] {
			t.Errorf("%s is not in the model with a mesh; nodes with meshes: %v", name, has)
		}
	}
	var m Scene3DMeta
	raw, _ := os.ReadFile(filepath.Join(dir, "m.json"))
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	vis := map[string][][2]int{}
	for _, o := range m.Objects {
		if len(o.Vis) > 0 {
			vis[o.N] = o.Vis
		}
		if o.Hidden {
			t.Errorf("%s is hidden for good, it is keyed on", o.N)
		}
	}
	if v := vis["late_letter"]; len(v) != 2 || v[0] != [2]int{1, 0} || v[1] != [2]int{30, 1} {
		t.Errorf("late_letter vis = %v, want [[1 0] [30 1]]", v)
	}
	if _, ok := vis["always"]; ok {
		t.Errorf("always has a track: %v", vis["always"])
	}
	empty := map[string]bool{}
	for _, n := range m.Stats.EmptyAtSave {
		empty[n] = true
	}
	for _, name := range []string{"late_letter", "ring"} {
		if !empty[name] {
			t.Errorf("%s is empty at the saved frame and the viewer is not told: %v", name, m.Stats.EmptyAtSave)
		}
	}
}

func replaceQ(s, path string) string {
	b, _ := json.Marshal(path)
	return strings.Replace(s, "%q", string(b), 1)
}

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

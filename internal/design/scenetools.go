package design

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// SceneTools are the agent's tools for the 3D scene open beside its chat: they exist only in a chat that works in
// a 3D scene project. The scene's names, the pins' words and the person's picks are data, not instructions. The agent edits
// the scene by its script — a plain file of the blender-lab project (settings.root), with its usual file tools — and these
// tools show, build and check the result.
type SceneTools struct {
	L *Library
	// Project is the 3D scene this chat works in; empty: no tools at all.
	Project string
}

var _ mcpserver.Toolset = SceneTools{}

// SceneProjectOf is the 3D scene project a chat's folder belongs to, or "".
func (l *Library) SceneProjectOf(cwd string) string {
	if l == nil || cwd == "" {
		return ""
	}
	rel, err := filepath.Rel(l.Root(), cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	id, _, _ := strings.Cut(rel, string(filepath.Separator))
	if !IsLibraryID(id) || !l.IsScene3D(id) {
		return ""
	}
	return id
}

func (t SceneTools) Instructions() string {
	if t.Project == "" {
		return ""
	}
	where := ""
	if s, err := t.L.ReadScene3DSettings(t.Project); err == nil {
		where = " The scene is the blender-lab project " + s.Root + " (the .blend " + s.Blend
		if s.Script != "" {
			where += ", built by the script " + s.Script
		}
		where += "): change the scene by editing its script there, never the .blend."
	}
	return "This chat works on the 3D scene " + t.Project + "." + where +
		" The person's pins are comments on objects: read them with scene_comments, look at the view of one with scene_render, " +
		"change the script, run scene_apply (it builds the scene again and draws every pin's view before and after, then poll it with its job id), " +
		"open the before and after files with Read, and close the pin with scene_comments resolve. Never close a pin without a frame after the change. " +
		"scene_view shows the person a place of the scene in their viewer; scene_selection says what they picked. Only drafts are drawn here, small and one at a time on a shared machine."
}

func (t SceneTools) Tools() []any {
	if t.Project == "" {
		return []any{}
	}
	cam := videoStr("A camera of the scene by name (scene_document lists them), or leave out and give pos, target and fov.")
	vec := func(d string) *jsvalue.Object {
		return jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "number"), "minItems", 3, "maxItems", 3, "description", d)
	}
	return []any{
		videoTool("scene_document", "The scene: where its project is, the model's state, the objects (names, kinds, parents), the cameras, the animation's frames and the measured faults (polygons, lost textures, scale not applied, objects no camera sees).",
			jsvalue.NewObject()),
		videoTool("scene_selection", "What the person sees in the viewer now: whether it is open, the object they picked, the pin open, the camera view and the frame.",
			jsvalue.NewObject()),
		videoTool("scene_comments", "The pins on the scene: object, point (Blender's axes, z up), camera view, frame, text, and whether closed. With resolve it closes one pin with what was changed.",
			jsvalue.NewObject("open", videoBool("Only the pins still open."), "resolve", videoStr("The id of a pin to close."),
				"note", videoStr("With resolve: what was changed, in a sentence."),
				"before", videoStr("With resolve: the key of the frame before (from scene_apply)."),
				"after", videoStr("With resolve: the key of the frame after (from scene_apply or scene_render)."))),
		videoTool("scene_apply", "Build the scene again from its script (blender -b -P, on a slot of this machine, minutes) and draw the views of the open pins before and after. Answers a job: call again with job to read it until it is ready. Run it after editing the script.",
			jsvalue.NewObject("pins", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string"), "description", "Pin ids; leave out for every open pin."),
				"job", videoStr("A job id from the first call: read its progress instead of starting one."))),
		videoTool("scene_view", "Show the person a place of the scene in their viewer: a camera of the scene or a free view, a frame, an object or a pin.",
			jsvalue.NewObject("cam", cam, "pos", vec("Free view: the eye, Blender's axes."), "target", vec("Free view: the point looked at."), "fov", videoNum("Free view: vertical field of view in degrees."),
				"frame", videoNum("The frame of the animation."), "object", videoStr("An object by name to mark."), "pin", videoStr("A pin's id: go to its view."))),
		videoTool("scene_render", "Draw a small draft of a view of the scene, as the render would (640x360, few samples; one at a time on a shared machine, cached). Answers the file: open it with Read. The same view and frame are taken from the cache.",
			jsvalue.NewObject("cam", cam, "pos", vec("Free view: the eye, Blender's axes."), "target", vec("Free view: the point looked at."), "fov", videoNum("Free view: vertical field of view in degrees."),
				"frame", videoNum("The frame of the animation; default the first."), "wait", videoNum("Seconds to wait for the frame; default 120."))),
		videoTool("scene_clip", "Draw a draft animation of a camera over frames start..end (every step-th, at most 24 frames, small, one frame at a time on a shared machine). Answers the clip; call again with the same arguments to read its progress. A ready clip has a key for scene_to_video.",
			jsvalue.NewObject("cam", cam, "pos", vec("Free view: the eye, Blender's axes."), "target", vec("Free view: the point looked at."), "fov", videoNum("Free view: vertical field of view in degrees."),
				"start", videoNum("First frame; default the scene's first."), "end", videoNum("Last frame; default the scene's last."), "step", videoNum("Every step-th frame; default chosen to fit."))),
		videoTool("scene_outputs", "Make the outputs of a camera for a generator: key frames (at most 4) as beauty draft, clay, depth and normals pictures, masks of chosen meshes, the camera's path, and one manifest.json naming them (docs/scene3d-outputs.md). Small drafts, one job at a time on a shared machine. Answers the set; call again with the same arguments to read its progress. You cannot send them to a generator: the person does, with the price shown.",
			jsvalue.NewObject("cam", cam, "pos", vec("Free view: the eye, Blender's axes."), "target", vec("Free view: the point looked at."), "fov", videoNum("Free view: vertical field of view in degrees."),
				"frames", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "number"), "description", "Key frames; default the first, the middle and the last."),
				"masks", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string"), "description", "Names of meshes to make masks of."))),
		videoTool("scene_to_video", "Put a ready clip into a video document as a new scene, with where it came from (scene, camera, frames, engine). Leave video out for a new video document. A final render on a GPU is never started by you: the person does it with its button, after a price.",
			jsvalue.NewObject("clip", videoStr("The clip's key (from scene_clip)."), "video", videoStr("A video project's id; leave out for a new one.")), "clip"),
	}
}

func (t SceneTools) Has(name string) bool {
	if t.Project == "" {
		return false
	}
	switch name {
	case "scene_document", "scene_selection", "scene_comments", "scene_apply", "scene_view", "scene_render", "scene_clip", "scene_outputs", "scene_to_video":
		return true
	}
	return false
}

func (t SceneTools) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	switch name {
	case "scene_document":
		return t.document()
	case "scene_selection":
		return videoJSONResult(scene3dViewSeen(t.Project, time.Now()))
	case "scene_comments":
		return t.comments(args)
	case "scene_apply":
		return t.apply(args)
	case "scene_view":
		return t.view(args)
	case "scene_render":
		return t.render(ctx, args)
	case "scene_clip":
		return t.clip(args)
	case "scene_outputs":
		return t.outputs(args)
	case "scene_to_video":
		res, err := t.L.Scene3DClipToVideo(t.Project, argStr(args, "clip"), argStr(args, "video"), nil)
		if err != nil {
			return mcpserver.TextResult(err.Error(), true)
		}
		return videoJSONResult(res)
	}
	return mcpserver.TextResult("unknown tool", true)
}

func (t SceneTools) document() *jsvalue.Object {
	st, err := t.L.Scene3DStateOf(t.Project)
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	type obj struct {
		N    string `json:"n"`
		T    string `json:"t"`
		P    string `json:"p,omitempty"`
		Tris int    `json:"tris,omitempty"`
	}
	out := struct {
		Title    string          `json:"title"`
		Settings Scene3DSettings `json:"settings"`
		Engine   string          `json:"draftEngine"`
		Model    string          `json:"model"`
		Error    string          `json:"error,omitempty"`
		Meta     *struct {
			FPS     float64         `json:"fps"`
			Start   int             `json:"start"`
			End     int             `json:"end"`
			Res     [2]int          `json:"res"`
			Cameras []string        `json:"cameras"`
			Markers []Scene3DMarker `json:"markers"`
			Stats   Scene3DStats    `json:"stats"`
			Objects []obj           `json:"objects"`
			Cut     int             `json:"objectsNotListed,omitempty"`
		} `json:"scene,omitempty"`
		OpenPins int `json:"openPins"`
	}{Title: st.Title, Settings: st.Settings, Engine: st.Engine, Model: st.Model.Status, Error: st.Model.Error}
	for _, c := range st.Comments {
		if !c.Resolved {
			out.OpenPins++
		}
	}
	if m := st.Model.Meta; m != nil {
		out.Meta = &struct {
			FPS     float64         `json:"fps"`
			Start   int             `json:"start"`
			End     int             `json:"end"`
			Res     [2]int          `json:"res"`
			Cameras []string        `json:"cameras"`
			Markers []Scene3DMarker `json:"markers"`
			Stats   Scene3DStats    `json:"stats"`
			Objects []obj           `json:"objects"`
			Cut     int             `json:"objectsNotListed,omitempty"`
		}{FPS: m.FPS, Start: m.Start, End: m.End, Res: m.Res, Markers: m.Markers, Stats: m.Stats}
		for _, c := range m.Cameras {
			out.Meta.Cameras = append(out.Meta.Cameras, c.N)
		}
		for i, o := range m.Objects {
			if i >= 150 {
				out.Meta.Cut = len(m.Objects) - i
				break
			}
			out.Meta.Objects = append(out.Meta.Objects, obj{o.N, o.T, o.P, o.Tris})
		}
	}
	return videoJSONResult(out)
}

func (t SceneTools) comments(args *jsvalue.Object) *jsvalue.Object {
	if id := argStr(args, "resolve"); id != "" {
		c, err := t.L.Scene3DResolve(t.Project, id, Scene3DFix{Before: argStr(args, "before"), After: argStr(args, "after"), Note: argStr(args, "note")})
		if err != nil {
			return mcpserver.TextResult(err.Error(), true)
		}
		return videoJSONResult(c)
	}
	all, _ := t.L.ReadDeckComments(t.Project)
	out := []DeckComment{}
	for _, c := range all {
		if c.View == DeckCommentScene3D && (!argBool(args, "open") || !c.Resolved) {
			out = append(out, c)
		}
	}
	return videoJSONResult(out)
}

func (t SceneTools) apply(args *jsvalue.Object) *jsvalue.Object {
	if id := argStr(args, "job"); id != "" {
		j, ok := Scene3DApplyOf(id)
		if !ok {
			return mcpserver.TextResult("no such job", true)
		}
		return videoJSONResult(j)
	}
	var pins []string
	if v, ok := args.Get("pins"); ok {
		if list, ok := v.([]any); ok {
			for _, p := range list {
				if s, ok := p.(string); ok && s != "" {
					pins = append(pins, s)
				}
			}
		}
	}
	j, err := t.L.StartScene3DApply(t.Project, pins)
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	return videoJSONResult(j)
}

func vec3Arg(args *jsvalue.Object, key string) ([3]float64, bool) {
	var out [3]float64
	if args == nil {
		return out, false
	}
	v, ok := args.Get(key)
	if !ok {
		return out, false
	}
	list, ok := v.([]any)
	if !ok || len(list) != 3 {
		return out, false
	}
	for i, x := range list {
		f, ok := x.(float64)
		if !ok {
			return out, false
		}
		out[i] = f
	}
	return out, true
}

func sceneCamArg(args *jsvalue.Object) (Scene3DCam, string) {
	if n := argStr(args, "cam"); n != "" {
		return Scene3DCam{N: n}, ""
	}
	pos, okP := vec3Arg(args, "pos")
	tgt, okT := vec3Arg(args, "target")
	fov, okF := argNum(args, "fov")
	if !okP || !okT || !okF {
		return Scene3DCam{}, "give cam (a camera of the scene), or pos, target and fov"
	}
	return Scene3DCam{Pos: pos, Target: tgt, Fov: fov}, ""
}

func (t SceneTools) view(args *jsvalue.Object) *jsvalue.Object {
	cmd := Scene3DViewCmd{Object: argStr(args, "object"), Pin: argStr(args, "pin")}
	if _, hasPos := vec3Arg(args, "pos"); argStr(args, "cam") != "" || hasPos {
		cam, msg := sceneCamArg(args)
		if msg != "" {
			return mcpserver.TextResult(msg, true)
		}
		cmd.Cam = &cam
	}
	if f, ok := argNum(args, "frame"); ok {
		n := int(f)
		cmd.Frame = &n
	}
	if cmd.Cam == nil && cmd.Frame == nil && cmd.Object == "" && cmd.Pin == "" {
		return mcpserver.TextResult("give a camera or view, a frame, an object or a pin", true)
	}
	if !scene3dViewPush(t.Project, cmd, time.Now()) {
		return mcpserver.TextResult("The person does not have the scene open: nothing moved. Say the place in words.", false)
	}
	return mcpserver.TextResult("Done in the person's viewer.", false)
}

func (t SceneTools) render(ctx context.Context, args *jsvalue.Object) *jsvalue.Object {
	cam, msg := sceneCamArg(args)
	if msg != "" {
		return mcpserver.TextResult(msg, true)
	}
	frame := 0
	if m := t.L.scene3dMeta(t.Project); m != nil {
		frame = m.Start
	}
	if f, ok := argNum(args, "frame"); ok {
		frame = int(f)
	}
	wait := 120 * time.Second
	if w, ok := argNum(args, "wait"); ok && w > 0 && w <= 600 {
		wait = time.Duration(w) * time.Second
	}
	f, file, err := t.L.scene3dFrameWait(ctx, t.Project, cam, frame, wait)
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	return videoJSONResult(struct {
		Key  string `json:"key"`
		Path string `json:"path"`
	}{f.Key, file})
}

// scene3dMeta is the description of the scene's current model, nil when it is not built.
func (l *Library) scene3dMeta(id string) *Scene3DMeta {
	_, _, hash, err := l.scene3dTarget(id)
	if err != nil {
		return nil
	}
	return readScene3DMeta(l.scene3dCache(id, hash))
}

func (t SceneTools) clip(args *jsvalue.Object) *jsvalue.Object {
	cam, msg := sceneCamArg(args)
	if msg != "" {
		return mcpserver.TextResult(msg, true)
	}
	sp := Scene3DClipSpec{Cam: cam}
	if m := t.L.scene3dMeta(t.Project); m != nil {
		sp.Start, sp.End = m.Start, m.End
	}
	if f, ok := argNum(args, "start"); ok {
		sp.Start = int(f)
	}
	if f, ok := argNum(args, "end"); ok {
		sp.End = int(f)
	}
	if f, ok := argNum(args, "step"); ok {
		sp.Step = int(f)
	}
	c, err := t.L.StartScene3DClip(t.Project, sp, false)
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	return videoJSONResult(c)
}

func (t SceneTools) outputs(args *jsvalue.Object) *jsvalue.Object {
	cam, msg := sceneCamArg(args)
	if msg != "" {
		return mcpserver.TextResult(msg, true)
	}
	sp := Scene3DOutputsSpec{Cam: cam}
	if v, ok := args.Get("frames"); ok {
		if list, ok := v.([]any); ok {
			for _, x := range list {
				if f, ok := x.(float64); ok {
					sp.Frames = append(sp.Frames, int(f))
				}
			}
		}
	}
	if v, ok := args.Get("masks"); ok {
		if list, ok := v.([]any); ok {
			for _, x := range list {
				if s, ok := x.(string); ok && s != "" {
					sp.Masks = append(sp.Masks, s)
				}
			}
		}
	}
	o, err := t.L.StartScene3DOutputs(t.Project, sp, false)
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	return videoJSONResult(o)
}

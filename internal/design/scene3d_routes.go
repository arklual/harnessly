package design

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The 3D scene's routes: the state, the pins, the draft frames and the model for the viewer, and the agent's
// CLI over the same. The files and jobs they use are in scene3d.go.

// scene3dGuarded is deckGuarded for a project that must be a 3D scene.
func (a *API) scene3dGuarded(r *http.Request, min Role, methods ...string) (*jsvalue.Object, string, *Answer) {
	body, id, no := a.deckGuarded(r, min, methods...)
	if no != nil {
		return nil, "", no
	}
	if !a.L.IsScene3D(id) {
		ans := JSON(r, http.StatusNotFound, errorBody(locale.Pick("Not a 3D scene", "Это не 3D-сцена")), noStore())
		return nil, "", &ans
	}
	return body, id, nil
}

// Scene3D is GET ?artifact= (view): the Scene3DState.
func (a *API) Scene3D(r *http.Request) Answer {
	_, id, no := a.scene3dGuarded(r, RoleView, http.MethodGet)
	if no != nil {
		return *no
	}
	st, err := a.L.Scene3DStateOf(id)
	if err != nil {
		return a.deckFail(r, err, "read the 3D scene")
	}
	return JSON(r, http.StatusOK, st, noStore())
}

// Scene3DBuild is POST {artifact} (edit): builds the model again after a failure; {model: Scene3DModel}.
func (a *API) Scene3DBuild(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	var in struct {
		Builder string `json:"builder"`
	}
	_ = scene3dDecode(body, &in)
	m, err := a.L.Scene3DRebuild(id, in.Builder)
	if err != nil {
		return a.deckFail(r, err, "build the model")
	}
	return JSON(r, http.StatusOK, struct {
		Model Scene3DModel `json:"model"`
	}{m}, noStore())
}

type scene3dFrameBody struct {
	Cam   Scene3DCam `json:"cam"`
	Frame int        `json:"frame"`
	Retry bool       `json:"retry"`
	// Builder is the builder the person picked (its id); "" lets the hub choose.
	Builder string `json:"builder"`
}

func scene3dDecode(body *jsvalue.Object, into any) error {
	return json.Unmarshal(jsvalue.Stringify(body, ""), into)
}

// Scene3DFrame is POST {artifact, cam: {n}|{pos, target, fov}, frame, retry?} (comment): asks for the draft frame "as it
// will be" and answers its Scene3DFrame at once: ready from the cache, or queued/building to ask for again.
func (a *API) Scene3DFrame(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleComment, http.MethodPost)
	if no != nil {
		return *no
	}
	var in scene3dFrameBody
	if err := scene3dDecode(body, &in); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The body is not valid", "Тело запроса неверно")), noStore())
	}
	f, err := a.L.Scene3DFrameStart(id, in.Cam, in.Frame, in.Retry, in.Builder)
	if err != nil {
		return a.deckFail(r, err, "draw the frame")
	}
	return JSON(r, http.StatusOK, f, noStore())
}

type scene3dCommentBody struct {
	Text   string      `json:"text"`
	Label  string      `json:"label"`
	Object string      `json:"object"`
	P      *[3]float64 `json:"p"`
	Cam    *Scene3DCam `json:"cam"`
	Frame  int         `json:"frame"`
	Render string      `json:"render"`
	Shot   string      `json:"shot"` // a data:image/jpeg;base64 URL of the viewport
}

// Scene3DComments is POST {artifact, text, label?, object?, p?, cam?, frame, render?, shot?} (comment): the DeckComment of
// a pin; and PATCH {artifact, id, resolved} as the deck's (comment).
func (a *API) Scene3DComments(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleComment, http.MethodPost, http.MethodPatch)
	if no != nil {
		return *no
	}
	if r.Method == http.MethodPatch {
		cid, _ := strField(body, "id")
		resolved, isBool := Field(body, "resolved").(bool)
		if cid == "" || !isBool {
			return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("id and resolved are expected", "Нужны id и resolved")), noStore())
		}
		v, err := a.L.ResolveDeckComment(id, cid, resolved)
		if err != nil {
			return a.deckFail(r, err, "change the comment")
		}
		return JSON(r, http.StatusOK, v, noStore())
	}
	var in scene3dCommentBody
	if err := scene3dDecode(body, &in); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The body is not valid", "Тело запроса неверно")), noStore())
	}
	var shot []byte
	if in.Shot != "" {
		raw, ok := strings.CutPrefix(in.Shot, "data:image/jpeg;base64,")
		b, err := base64.StdEncoding.DecodeString(raw)
		if !ok || err != nil {
			return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("shot: a JPEG data URL", "shot: JPEG в виде data URL")), noStore())
		}
		shot = b
	}
	spec := DeckCommentSpec{Text: in.Text, Label: in.Label,
		Scene: &Scene3DAnchor{Object: in.Object, P: in.P, Cam: in.Cam, Frame: in.Frame, Render: in.Render}}
	c, err := a.L.AddScene3DComment(id, spec, shot)
	if err != nil {
		return a.deckFail(r, err, "save the comment")
	}
	return JSON(r, http.StatusOK, c, noStore())
}

// ---- body routes: the model and the pictures

func (a *API) sceneBodyGuard(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !a.imageGuard(w, r) {
		return "", false
	}
	id := QueryString(r, "artifact")
	if !IsLibraryID(id) {
		w.WriteHeader(http.StatusBadRequest)
		return "", false
	}
	if a.denyImage(w, r, id) {
		return "", false
	}
	return id, true
}

func (a *API) sceneBodyFile(w http.ResponseWriter, r *http.Request, file, ctype, cache string) {
	f, err := os.Open(file)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Cache-Control", cache)
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// Scene3DModel is GET ?artifact[&h=hash] (view, body route): the model as a binary glTF. By hash it never changes, so
// the browser keeps it.
func (a *API) Scene3DModel(w http.ResponseWriter, r *http.Request) {
	id, ok := a.sceneBodyGuard(w, r)
	if !ok {
		return
	}
	file, h, err := a.L.Scene3DModelFile(id, QueryString(r, "h"))
	if err != nil {
		writeAnswer(w, a.deckFail(r, err, "read the model"))
		return
	}
	cache := "private, no-cache"
	if QueryString(r, "h") == h {
		cache = "private, max-age=31536000, immutable"
	}
	a.sceneBodyFile(w, r, file, "model/gltf-binary", cache)
}

// Scene3DFrameImage is GET ?artifact&key (view, body route): a draft frame as image/jpeg. A key is its content's address.
func (a *API) Scene3DFrameImage(w http.ResponseWriter, r *http.Request) {
	id, ok := a.sceneBodyGuard(w, r)
	if !ok {
		return
	}
	file, err := a.L.Scene3DFrameFile(id, QueryString(r, "key"))
	if err != nil {
		writeAnswer(w, a.deckFail(r, err, "read the frame"))
		return
	}
	a.sceneBodyFile(w, r, file, "image/jpeg", "private, max-age=31536000, immutable")
}

// Scene3DShot is GET ?artifact&id (view, body route): the viewport picture of a pin as image/jpeg.
func (a *API) Scene3DShot(w http.ResponseWriter, r *http.Request) {
	id, ok := a.sceneBodyGuard(w, r)
	if !ok {
		return
	}
	file, err := a.L.Scene3DShotFile(id, QueryString(r, "id"))
	if err != nil {
		writeAnswer(w, a.deckFail(r, err, "read the picture"))
		return
	}
	a.sceneBodyFile(w, r, file, "image/jpeg", "private, max-age=31536000, immutable")
}

// ---- the agent's CLI

// CLIScene3D is GET ?project=&what=state|comments[&open=1] (view): {project, state: Scene3DState} or {project, comments}.
func (a *API) CLIScene3D(r *http.Request) Answer {
	if _, no := a.CLIGuard(r); no != nil {
		return *no
	}
	h := noStore()
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	project := QueryString(r, "project")
	if no := a.gate(r, project, RoleView); no != nil {
		return *no
	}
	if err := a.L.requireScene3D(project); err != nil {
		return a.deckFail(r, err, "read the 3D scene")
	}
	switch QueryString(r, "what") {
	case "state":
		st, err := a.L.Scene3DStateOf(project)
		if err != nil {
			return a.deckFail(r, err, "read the 3D scene")
		}
		return JSON(r, http.StatusOK, struct {
			Project string       `json:"project"`
			State   Scene3DState `json:"state"`
		}{project, st}, h)
	case "comments":
		all, _ := a.L.ReadDeckComments(project)
		out := []DeckComment{}
		for _, c := range all {
			if QueryString(r, "open") != "1" || !c.Resolved {
				out = append(out, c)
			}
		}
		return JSON(r, http.StatusOK, struct {
			Project  string        `json:"project"`
			Comments []DeckComment `json:"comments"`
		}{project, out}, h)
	}
	return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("what is state or comments", "what — state или comments")), h)
}

// CLIScene3DAdd is POST {title, root, blend, script?} (edit): a new 3D scene document over a blender-lab project folder
// of this machine; {id, title}. The settings are only written here, on the machine: a page cannot point at a path.
func (a *API) CLIScene3DAdd(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	body := Fields(raw)
	title, _ := strField(body, "title")
	root, _ := strField(body, "root")
	blend, _ := strField(body, "blend")
	script, _ := strField(body, "script")
	s := Scene3DSettings{V: 1, Root: root, Blend: blend, Script: script}
	if err := ValidateScene3DSettings(s); err != nil {
		return a.deckFail(r, err, "make the 3D scene")
	}
	p, err := a.L.CreateProject(title)
	if err != nil {
		return a.deckFail(r, err, "make the 3D scene")
	}
	if err := a.L.InitScene3D(p.ID, s); err != nil {
		return a.deckFail(r, err, "make the 3D scene")
	}
	a.L.AnnounceNewBy(p.ID, a.person(r))
	return JSON(r, http.StatusOK, struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}{p.ID, p.Title}, h)
}

// CLIScene3DFrame is POST {project, cam, frame, wait?: seconds} (view): the draft frame; with wait it holds until the frame is
// ready (or failed) and answers {frame, path} with the cached JPEG's absolute path for the agent to open.
func (a *API) CLIScene3DFrame(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	body := Fields(raw)
	project, _ := strField(body, "project")
	if no := a.gate(r, project, RoleComment); no != nil {
		return *no
	}
	var in struct {
		scene3dFrameBody
		Wait int `json:"wait"`
	}
	if err := scene3dDecode(body, &in); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The body is not valid", "Тело запроса неверно")), h)
	}
	f, err := a.L.Scene3DFrameStart(project, in.Cam, in.Frame, in.Retry, in.Builder)
	if err != nil {
		return a.deckFail(r, err, "draw the frame")
	}
	deadline := time.Now().Add(time.Duration(min(max(in.Wait, 0), 600)) * time.Second)
	for f.Status != Scene3DReady && f.Status != Scene3DFailed && time.Now().Before(deadline) {
		select {
		case <-r.Context().Done():
			return JSON(r, 499, errorBody("canceled"), h)
		case <-time.After(1500 * time.Millisecond):
		}
		if f, err = a.L.Scene3DFrameStart(project, in.Cam, in.Frame, false, in.Builder); err != nil {
			return a.deckFail(r, err, "draw the frame")
		}
	}
	path := ""
	if f.Status == Scene3DReady {
		if file, err := a.L.Scene3DFrameFile(project, f.Key); err == nil {
			path = file
		}
	}
	return JSON(r, http.StatusOK, struct {
		Frame Scene3DFrame `json:"frame"`
		Path  string       `json:"path,omitempty"`
	}{f, path}, h)
}

// Scene3DFix is GET ?artifact&id&which=before|after (view, body route): the frame a pin was closed with, as image/jpeg.
func (a *API) Scene3DFixImage(w http.ResponseWriter, r *http.Request) {
	id, ok := a.sceneBodyGuard(w, r)
	if !ok {
		return
	}
	file, err := a.L.Scene3DFixFile(id, QueryString(r, "id"), QueryString(r, "which"))
	if err != nil {
		writeAnswer(w, a.deckFail(r, err, "read the picture"))
		return
	}
	a.sceneBodyFile(w, r, file, "image/jpeg", "private, max-age=31536000, immutable")
}

// Scene3DClip is POST {artifact, cam, start, end, step?, retry?} (comment): asks for a draft animation of a camera over
// frames start..end taking every step-th (drawn small, one frame at a time); answers the Scene3DClip, ready from the cache or
// going.
func (a *API) Scene3DClip(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleComment, http.MethodPost)
	if no != nil {
		return *no
	}
	var in struct {
		Scene3DClipSpec
		Retry bool `json:"retry"`
	}
	if err := scene3dDecode(body, &in); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The body is not valid", "Тело запроса неверно")), noStore())
	}
	c, err := a.L.StartScene3DClip(id, in.Scene3DClipSpec, in.Retry)
	if err != nil {
		return a.deckFail(r, err, "draw the clip")
	}
	return JSON(r, http.StatusOK, c, noStore())
}

// Scene3DFinal is POST {artifact, action: "plan", cam, start, end} | {artifact, action: "start", plan, confirm: true} (edit):
// the plan of a final render on a rented GPU with its estimate (nothing rented), or the render of a plan the person confirmed.
func (a *API) Scene3DFinal(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	var in struct {
		Scene3DClipSpec
		Action  string `json:"action"`
		Plan    string `json:"plan"`
		Confirm bool   `json:"confirm"`
	}
	if err := scene3dDecode(body, &in); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The body is not valid", "Тело запроса неверно")), noStore())
	}
	switch in.Action {
	case "plan":
		p, err := a.L.Scene3DPlanFinal(r.Context(), id, in.Scene3DClipSpec)
		if err != nil {
			return a.deckFail(r, err, "plan the render")
		}
		return JSON(r, http.StatusOK, p, noStore())
	case "start":
		j, err := a.L.StartScene3DFinal(id, in.Plan, in.Confirm)
		if err != nil {
			return a.deckFail(r, err, "start the render")
		}
		return JSON(r, http.StatusOK, j, noStore())
	}
	return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("action is plan or start", "action — plan или start")), noStore())
}

// Scene3DToVideo is POST {artifact, clip, video?} (edit): the clip as a new scene of a video document (a new document when
// video is empty); {video, scene, page, folder, origin, new}.
func (a *API) Scene3DToVideo(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	clip, _ := strField(body, "clip")
	video, _ := strField(body, "video")
	if video != "" {
		if !IsLibraryID(video) {
			return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Unknown video", "Неизвестное видео")), noStore())
		}
		if gated := a.gate(r, video, RoleEdit); gated != nil {
			return *gated
		}
	}
	res, err := a.L.Scene3DClipToVideo(id, clip, video, nil)
	if err != nil {
		return a.deckFail(r, err, "put the clip into the video")
	}
	return JSON(r, http.StatusOK, res, noStore())
}

// Scene3DClipFile is GET ?artifact&key (view, body route): a ready clip's MP4.
func (a *API) Scene3DClipFile(w http.ResponseWriter, r *http.Request) {
	id, ok := a.sceneBodyGuard(w, r)
	if !ok {
		return
	}
	dir, _, err := a.L.Scene3DClipDir(id, QueryString(r, "key"))
	if err != nil {
		writeAnswer(w, a.deckFail(r, err, "read the clip"))
		return
	}
	a.sceneBodyFile(w, r, filepath.Join(dir, "clip.mp4"), "video/mp4", "private, max-age=31536000, immutable")
}

// Scene3DOutputsRoute is POST {artifact, cam, frames?, passes?, masks?, retry?} (comment): makes the outputs of a camera at a few
// frames for a generator (docs/scene3d-outputs.md); answers the Scene3DOutputs, ready from the cache or going.
func (a *API) Scene3DOutputsRoute(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleComment, http.MethodPost)
	if no != nil {
		return *no
	}
	var in struct {
		Scene3DOutputsSpec
		Retry bool `json:"retry"`
	}
	if err := scene3dDecode(body, &in); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The body is not valid", "Тело запроса неверно")), noStore())
	}
	o, err := a.L.StartScene3DOutputs(id, in.Scene3DOutputsSpec, in.Retry)
	if err != nil {
		return a.deckFail(r, err, "make the outputs")
	}
	return JSON(r, http.StatusOK, o, noStore())
}

// Scene3DGenerate is POST {artifact, action: "estimate"|"start", outputs, model, prompt, seconds, source, confirm?} (edit): the
// generator's price of the generation, or its start by a person who confirmed the price. Money is spent only by "start".
func (a *API) Scene3DGenerate(r *http.Request) Answer {
	body, id, no := a.scene3dGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	var in struct {
		Scene3DGenParams
		Action  string `json:"action"`
		Confirm bool   `json:"confirm"`
	}
	if err := scene3dDecode(body, &in); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The body is not valid", "Тело запроса неверно")), noStore())
	}
	switch in.Action {
	case "estimate":
		usd, note, err := a.L.Scene3DGenEstimate(r.Context(), id, in.Scene3DGenParams)
		if err != nil {
			return a.deckFail(r, err, "price the generation")
		}
		return JSON(r, http.StatusOK, struct {
			USD  float64 `json:"usd"`
			Note string  `json:"note"`
		}{usd, note}, noStore())
	case "start":
		g, err := a.L.StartScene3DGen(id, in.Scene3DGenParams, in.Confirm)
		if err != nil {
			return a.deckFail(r, err, "start the generation")
		}
		return JSON(r, http.StatusOK, g, noStore())
	}
	return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("action is estimate or start", "action — estimate или start")), noStore())
}

// Scene3DOutputFileRoute is GET ?artifact&key&name (view, body route): a file of a set of outputs (a picture, the manifest).
func (a *API) Scene3DOutputFileRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := a.sceneBodyGuard(w, r)
	if !ok {
		return
	}
	file, err := a.L.Scene3DOutputFile(id, QueryString(r, "key"), QueryString(r, "name"))
	if err != nil {
		writeAnswer(w, a.deckFail(r, err, "read the file"))
		return
	}
	ctype := "image/png"
	switch strings.ToLower(filepath.Ext(file)) {
	case ".jpg":
		ctype = "image/jpeg"
	case ".json":
		ctype = "application/json"
	}
	a.sceneBodyFile(w, r, file, ctype, "private, max-age=31536000, immutable")
}

package design

import "time"

// A 3D scene is a project folder with one more file: meta/scene3d.json. The document is a blender-lab project
// (scenes/*.py, kit/, assets/, out/*.blend): no format of its own. The settings only say where the project is and which
// .blend is the scene. What the page shows is made from it on the machine and cached under .media/scene3d/<hash>/: the
// .glb for the viewer (exported by headless Blender, decimated and with small textures) with a meta.json that names the
// objects, cameras and the animation, and the draft frames "as it will be" (one still each, rendered at a small size).
// Comments are the deck's (meta/comments/<id>.json) with a pin on the model: the object, a point on its surface, the
// camera it was seen from and the frame of the animation.

const (
	Scene3DSettingsFile = "meta/scene3d.json"
	// Scene3DCacheDir holds <hash>/model.glb, <hash>/meta.json and <hash>/frames/<key>.jpg.
	Scene3DCacheDir = ".media/scene3d"
	// DeckComment.View of a comment on a 3D scene.
	DeckCommentScene3D = "scene3d"
)

// The bounds of a 3D scene's work on the shared machine: only drafts run here, finals belong on a rented GPU.
const (
	Scene3DFrameW        = 640
	Scene3DFrameH        = 360
	Scene3DFrameSamples  = 12
	Scene3DFrameTimeout  = 100 // seconds, the draft render's limit
	Scene3DFrameSlotWait = 90  // seconds a draft waits for one of the machine's slots
	Scene3DExportTimeout = 600 // seconds, the export's limit
	Scene3DTriBudget     = 160000
	Scene3DTexMax        = 1024
	Scene3DCacheMax      = 1 << 30 // bytes of drafts kept per scene before the oldest are dropped
	scene3dMaxJobs       = 1       // drafts rendered at once by this runtime
	scene3dExportVersion = 3       // moves with scene3d_export.py: a new exporter makes new models
	scene3dMaxShot       = 400 << 10
)

// Scene3DSettings is meta/scene3d.json. Root is the blender-lab project's folder on the machine (absolute), Blend the
// scene's .blend under it, Script the .py that builds it (what an agent edits, stage 2) — all relative to Root.
type Scene3DSettings struct {
	V      int    `json:"v"`
	Root   string `json:"root"`
	Blend  string `json:"blend"`
	Script string `json:"script,omitempty"`
}

// Scene3DObject is one object of the scene as the viewer's tree shows it.
type Scene3DObject struct {
	N      string `json:"n"`
	T      string `json:"t"` // MESH, CURVE, EMPTY, CAMERA, LIGHT, ...
	P      string `json:"p,omitempty"`
	Tris   int    `json:"tris,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

// Scene3DCamera is a camera of the scene. Fov is the vertical field of view in degrees at the render's aspect.
type Scene3DCamera struct {
	N    string  `json:"n"`
	Lens float64 `json:"lens"`
	Fov  float64 `json:"fov"`
	Near float64 `json:"near"`
	Far  float64 `json:"far"`
	// Track is the camera's path: [frame, x, y, z, tx, ty, tz, fov] at up to 120 frames of the range, t* a point 5 m ahead
	// (Blender's axes), so the viewer shows the camera as the render has it.
	Track [][]float64 `json:"track,omitempty"`
}

// Scene3DMarker binds a camera to a frame of the animation (a timeline marker).
type Scene3DMarker struct {
	F   int    `json:"f"`
	N   string `json:"n"`
	Cam string `json:"cam,omitempty"`
}

// Scene3DStats is what the export measured: polygons and the scene's faults a check reports (stage 3).
type Scene3DStats struct {
	Tris           int      `json:"tris"`
	TrisOut        int      `json:"trisOut"`
	Objects        int      `json:"objects"`
	Animated       int      `json:"animated"`
	MissingImages  []string `json:"missingImages,omitempty"`
	UnappliedScale []string `json:"unappliedScale,omitempty"`
	OffCamera      []string `json:"offCamera,omitempty"`
}

// Scene3DMeta is <hash>/meta.json: what the exporter read from the .blend.
type Scene3DMeta struct {
	Version int             `json:"version"`
	FPS     float64         `json:"fps"`
	Start   int             `json:"start"`
	End     int             `json:"end"`
	Res     [2]int          `json:"res"`
	Camera  string          `json:"camera,omitempty"` // the scene's active camera
	Objects []Scene3DObject `json:"objects"`
	Cameras []Scene3DCamera `json:"cameras"`
	Markers []Scene3DMarker `json:"markers"`
	Bounds  [6]float64      `json:"bounds"` // min xyz, max xyz in Blender's axes
	Stats   Scene3DStats    `json:"stats"`
	// Bake is what the glTF's actions do not carry (drivers, constraints, NLA): an object's local transform at sampled frames, by name.
	// m "e": [frame, location xyz, XYZ euler, scale xyz]; m "q": [frame, location, quaternion wxyz, scale]. Blender's axes.
	Bake map[string]Scene3DBake `json:"bake,omitempty"`
}

// Scene3DBake is one baked object's samples.
type Scene3DBake struct {
	M    string      `json:"m"`
	Rows [][]float64 `json:"rows"`
}

// The statuses of the model and of a frame.
const (
	Scene3DNone     = "none"
	Scene3DBuilding = "building"
	Scene3DReady    = "ready"
	Scene3DFailed   = "failed"
	Scene3DQueued   = "queued"
)

// Scene3DModel is the viewer's model as the machine has it for the scene's current .blend.
type Scene3DModel struct {
	Status string       `json:"status"`
	Hash   string       `json:"hash,omitempty"`
	Bytes  int64        `json:"bytes,omitempty"`
	Error  string       `json:"error,omitempty"`
	Meta   *Scene3DMeta `json:"meta,omitempty"`
}

// Scene3DState is the answer of GET /design/scene3d. Rev moves when anything the page shows moves; the page polls it
// every 3 s while visible.
type Scene3DState struct {
	Rev           int64               `json:"rev"`
	Title         string              `json:"title"`
	Settings      Scene3DSettings     `json:"settings"`
	Blender       bool                `json:"blender"`
	Engine        string              `json:"engine"` // what draft frames are rendered with: eevee or cycles
	Model         Scene3DModel        `json:"model"`
	Comments      []DeckComment       `json:"comments"`
	GuestResolved []string            `json:"guestResolved"`
	Frames        []Scene3DFrame      `json:"frames"`
	Clips         []Scene3DClip       `json:"clips"`
	Finals        []Scene3DFinalJob   `json:"finals"`
	Outputs       []Scene3DOutputs    `json:"outputs"`
	Gens          []Scene3DGeneration `json:"generations"`
	Generator     bool                `json:"generator"`
}

// Scene3DCam is a view of the scene: a named camera of the scene (N, then the rest is the camera as it was at the frame),
// or a free one given by its position, target and vertical field of view, all in Blender's axes (z up).
type Scene3DCam struct {
	N      string     `json:"n,omitempty"`
	Pos    [3]float64 `json:"pos"`
	Target [3]float64 `json:"target"`
	Fov    float64    `json:"fov"`
}

// Scene3DAnchor is where a pin sits: the object (its name in Blender), the point on its surface, the camera it was set
// from and the frame of the animation.
type Scene3DAnchor struct {
	Object string      `json:"object,omitempty"`
	P      *[3]float64 `json:"p,omitempty"`
	Cam    *Scene3DCam `json:"cam,omitempty"`
	Frame  int         `json:"frame"`
	// Shot is the viewport as the person saw it (a small JPEG kept beside the comment); Render is the key of the draft
	// frame "as it will be" taken from this view, when there was one.
	Shot   bool   `json:"shot,omitempty"`
	Render string `json:"render,omitempty"`
	// Fix is how an agent closed the pin: the frame of the pin's view before and after the scene was changed.
	Fix *Scene3DFix `json:"fix,omitempty"`
}

// Scene3DFrame is one draft render: the key it is cached under, its state and the engine that drew it.
type Scene3DFrame struct {
	Key    string      `json:"key"`
	Status string      `json:"status"`
	Cam    *Scene3DCam `json:"cam,omitempty"`
	Frame  int         `json:"frame"`
	Engine string      `json:"engine,omitempty"`
	Error  string      `json:"error,omitempty"`
	Cached bool        `json:"cached,omitempty"`
	// Where is the builder that drew it; "" is this machine.
	Where string `json:"where,omitempty"`
}

// Scene3DFix is what closes a pin by a change of the scene: the draft frame of the pin's view (keys of frames, drawn from the
// scene before and after the change) and a word of what was changed.
type Scene3DFix struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Note   string `json:"note,omitempty"`
	At     string `json:"at"`
}

// Scene3DViewCmd is something the agent asks the person's viewer to do (a view of the scene, a frame, a pin).
type Scene3DViewCmd struct {
	Seq    int64       `json:"seq"`
	Cam    *Scene3DCam `json:"cam,omitempty"`
	Frame  *int        `json:"frame,omitempty"`
	Object string      `json:"object,omitempty"`
	Pin    string      `json:"pin,omitempty"`
	at     time.Time
}

// Scene3DViewReport is what the page says of itself: what it shows, what the person picked.
type Scene3DViewReport struct {
	Object  string      `json:"object"`
	Pin     string      `json:"pin"`
	Cam     *Scene3DCam `json:"cam,omitempty"`
	Frame   int         `json:"frame"`
	Playing bool        `json:"playing"`
	Acked   int64       `json:"acked"`
}

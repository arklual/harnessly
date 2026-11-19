# 3D scene outputs: the contract for generators

A 3D scene of Sinopia (a blender-lab project opened as a document) can make *outputs* for a camera: a few key frames as
pictures of what the scene is made of, the camera's path, and one `manifest.json` that names them. A generator (a video
model of a provider, an app) reads the manifest and nothing else; it never opens the `.blend`.

Make them with the **Outputs** section of the scene's *Frames* tab, the agent's tool `scene_outputs`, or
`POST <platform>/design/scene3d/outputs`. They are small drafts (640×360, a few samples) drawn on the machine's shared
capacity, one job at a time, and kept by the scene's content: the same camera, frames and passes of the same scene come
from the cache.

## Where they are

```
<project>/.media/scene3d/<scene hash>/outputs/<key>/
  manifest.json
  camera_path.json
  beauty_0001.jpg  clay_0001.jpg  depth_0001.png  normals_0001.png     one set per key frame
  mask_<object>_0001.png                                               one per chosen object and frame
  generated/                                                           what a generator brought back
```

Served by `GET <platform>/design/scene3d/output-file?artifact=<project>&key=<key>&name=<file>`.

## manifest.json (version 1)

```json
{
  "v": 1,
  "scene": { "project": "retro-convertible", "title": "Retro convertible", "hash": "c3a1f0…" },
  "camera": { "n": "cam1_hero", "pos": [0, 0, 0], "target": [0, 0, 0], "fov": 0 },
  "fps": 24,
  "range": [1, 240],
  "size": [640, 360],
  "engine": "cycles",
  "depthFar": 30,
  "passes": ["beauty", "clay", "depth", "normals"],
  "cameraPath": "camera_path.json",
  "keyframes": [
    { "frame": 1, "seconds": 0, "beauty": "beauty_0001.jpg", "clay": "clay_0001.jpg", "depth": "depth_0001.png", "normals": "normals_0001.png" }
  ],
  "masks": [{ "object": "body", "frame": 1, "file": "mask_body_0001.png" }],
  "createdAt": "2026-11-18T12:00:00Z",
  "where": ""
}
```

- Every file name is relative to the manifest's folder. A pass that was not asked for has no field.
- `camera` is a camera of the scene (`n`, then the rest is not used) or a free view: `pos` and `target` are points and `fov`
  the vertical field of view in degrees. All coordinates are Blender's: **z is up**, metres.
- **beauty**: the scene as the render has it (draft). **clay**: the same with a grey clay material over everything.
- **depth**: 8-bit grey, white is at the camera, black is `depthFar` metres away or farther, and black where nothing is.
- **normals**: world-space normals as colour, `rgb = n * 0.5 + 0.5`, black where nothing is.
- **masks**: a PNG whose **alpha** is the object (the colour is white); one per chosen mesh and key frame.
- `seconds` is the frame's time from the scene's start; `range` is the scene's first and last frame.

## camera_path.json

```json
{
  "fps": 24, "start": 1, "end": 240, "camera": "cam1_hero",
  "samples": [{ "frame": 1, "pos": [0, 0, 0], "target": [0, 0, 0], "fov": 40 }]
}
```

The camera's pose at sampled frames (about twenty, and at every cut of the timeline's markers); between two samples the
pose is the mix of both. `target` is a point 5 m ahead of the camera. A free view has one sample.

## Sending them to a generator

The *Send to a generator* panel of the **Outputs** section takes the first and the last key frame (beauty or clay) as the
first and the last frame of a video model, shows the provider's price, and starts only when the person confirms it.
The request goes to the machine's media service (`Owner: "design:<project>"`, `CopyTo` the set's `generated/` folder)
and carries an `origin` of the scene, the camera, the set and the frames. The clip that comes back is taken into the
scene's clips as a clip of kind `generated`, with the provider's own origin file kept in its `generator` field, and
*Add to video* puts it into a video document with `origin.json` beside it.

A generator never starts by itself and the agent's tools cannot start one: money is spent only by the person's button.

## What a generator must not assume

- Pictures are drafts: use them as references, not as the final frames.
- A set is made for one scene hash. When the scene changes (an agent edited its script) the sets of the old hash stay where
  they are and say which scene they came from; make new ones.

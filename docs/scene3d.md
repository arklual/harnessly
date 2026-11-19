# 3D scenes in Sinopia

A *3D scene* is a document type of Sinopia that opens a **blender-lab project** (a folder with `scenes/*.py`, `kit/`,
`assets/` and `out/*.blend`, built by headless Blender) for looking at and commenting on, and lets an agent close the
comments by editing the scene's script. It is a built-in type like `video`, not an app: a scene can be far over the 100 KB an
app document may keep (see [Sinopia: apps inside documents](build-apps/04-sinopia.md)), and the document has no format of its
own — only `meta/scene3d.json`, which says where the project is:

```json
{ "v": 1, "root": "/home/me/blender-lab", "blend": "out/retro_convertible.blend", "script": "scenes/retro_convertible.py" }
```

`root` is an absolute folder of the machine; `blend` and `script` are inside it. Only the machine's CLI writes this file.

```sh
harnessly design scene3d add --root /home/me/blender-lab --blend out/retro_convertible.blend --script scenes/retro_convertible.py
harnessly design scene3d get|comments|frame <project> …
```

Nobody fills a form for a scene. The person writes the scene in words in «Что нарисуем?» (the kind «3D-сцена»): the
runtime makes a project and starts an agent that builds it with the blender-lab skill, and `design scene3d add --project <id> …`
turns that project into the scene. An agent that saves a `.blend` in any chat, or gets one attached (`add --file <path>` copies it
into the project's `out/`), runs the same command: it opens the document in the right pane. The same `.blend` is the same
document, so a rebuild adds nothing and the viewer re-reads the file by itself.

## The viewer

The machine exports the `.blend` with headless Blender (modifiers applied, big meshes decimated, textures shrunk to JPEG) to a
`.glb` kept under `.media/scene3d/<hash of the .blend>/`, with `meta.json`: objects and their tree, the scene's cameras with
their sampled paths (markers, parents and constraints included), the animation's frames and the measured faults. The page
shows it with three.js: orbit, the scene's cameras as the render has them (with a guide of the render's frame), the tree of
objects, a timeline, and the looks material, clay and mesh. Objects that the render hides (cutters, helpers) are not shown.

## Pins

A pin is a comment on the model: the object, a point on its surface, the camera view and the frame, with a picture of what the
person saw. A click on a pin takes the camera back there. The button **Frame as it will be** draws a draft of the view with
Blender and keeps it by (scene, camera, frame).

Drafts are the only thing drawn on the shared machine: 640×360, a few samples, one at a time, under a slot of
the machine's heavy work (`internal/heavy`), nice, with a time limit. A machine without a GPU has no EGL, so the engine is
Cycles on the CPU with denoising (Eevee when the machine has EGL). Where the machine is part of a hub with a pool of builders,
the same job goes to a builder (`internal/builders`; the picker beside the button).

## The agent

In a chat that works in a scene the agent has `scene_document`, `scene_selection`, `scene_comments`, `scene_apply`,
`scene_view`, `scene_render`, `scene_clip`, `scene_outputs` and `scene_to_video`. It changes the scene by editing the script,
runs `scene_apply` (the scene is built again with `blender -b -y -P <script>`, the views of the open pins are drawn before and
after) and closes a pin with `scene_comments` `resolve`, with both frames. It cannot start anything that costs money.

## Clips, finals and the video

- **Draft animation**: every Nth frame of a camera, drawn small, joined by ffmpeg.
- **Final render**: the frames of a camera on a rented GPU through `gpu/render_selectel.py` of the project. The button shows a
  plan first — the estimate and the cloud script's dry run — and rents nothing until the person confirms the plan; a plan is
  confirmed once, within a quarter of an hour, for the scene as it was.
- **Add to video** puts a clip into a video document as a scene that plays its frames on the scene's time, with
  `origin.json` (scene, camera, frames, engine, where it was drawn) beside them.
- **Outputs for a generator** and **Send to a generator**: [scene3d-outputs.md](scene3d-outputs.md).

The checks of the scene (polygons, lost textures, scale not applied, objects no camera sees) are in the *Checks* tab, and the
plan of a final render names the ones that matter before money is spent.

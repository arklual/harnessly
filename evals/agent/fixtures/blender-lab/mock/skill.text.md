---
name: blender-lab
description: Build 3D scenes, models and motion clips in Blender by code (headless Blender 5.2) — car/product/motion-design shots, camera moves, realistic humans (MPFB2/MakeHuman) with facial expressions, photoreal environments from Poly Haven, soundtrack, and final renders on a rented GPU in Selectel (create → render → delete). Also clay previs for video-to-video (Kling Motion Control, FLF keyframes). Use when asked to "сделай в блендере", model or animate something in 3D, render a 3D clip, rent a GPU for rendering, or prepare previs for AI video generation.
---

# Blender lab

Workspace: `@LAB@`
Blender: `~/.local/bin/blender` (5.2.3 LTS, in `~/opt/blender`). Always headless: `blender -b -y -P scene.py -- <args>`
(`-y` = allow Python drivers). Do NOT pass `--factory-startup` when the scene uses MPFB (the extension must load).

## This machine (no GPU)
- No GPU, no libEGL: **only Cycles on CPU**. EEVEE/Workbench fail ("Couldn't open libEGL.so.1").
- Shared machine: `nice -n 10..15`, threads=8, long renders in the background.
- Measured: clay 960x540/20 spp ≈ 12 s/frame; realistic scene 1080p/64 spp ≈ 240 s/frame (≈ 2 h per second of video).
  → local = stills and previews only; finals go to a rented GPU (section below).

## Layout
- `kit/bl_kit.py` — helpers: reset, empty, key, interp, mat, loft, box, cylinder, sphere, torus, revolve, curve, subsurf,
  boolean, solidify, Surface (ray-cast details onto a body), align_z, driver, cycles.
- `kit/env_real.py` — photoreal desert highway: HDRI goegap_road + sun, wet asphalt with puddles, curbs, terrain,
  ~20k scattered Poly Haven rocks/shrubs/quiver trees, guard rail, poles. `env_real.build(sc, look, wet, intensity, sun_az, ...)`.
- `kit/mpfb_human.py` — realistic human via MPFB2: `build()` (skin SSS, eyes, brows, lashes, hair, clothes, default rig)
  and `set_face(h, frame, FACES[...])` with presets neutral/focus/frown/surprise/wow/smile (FACS expression units).
- `kit/character.py` — stylised toy driver (fallback, `--human toy`).
- `scenes/retro_convertible.py` — the reference scene: 1950s convertible, MPFB driver (IK arms to wheel/radio, lean,
  finger curl, faces, singing on the beat), radio with flickering dial, 9 cameras cut by timeline markers.
  Args: `--look clay|color`, `--human mpfb|toy`, `--stills 30,116`, `--anim`, `--res WxH`, `--samples N`.
- `tools/polyhaven.py` — CC0 assets: `python3 -I tools/polyhaven.py search palm | get <id> --res 2k` → `assets/<id>/`.
- `tools/mix_audio.py` — soundtrack (music + synthesized static/clicks/engine/wind) → `out/audio/mix.wav`, `beat.json`.
  Music: Kevin MacLeod "Sax, Rock, and Roll", CC BY 3.0 — credit required (assets/audio/LICENSES.md).
- `tools/sheet.sh` — contact sheet of stills (look at several shots in one image).
- `gpu/` — GPU rental (see below and gpu/README.md).

## Workflow that works
1. Build in code, save `out/<scene>.blend`, and right after EVERY save run `harnessly design scene3d add --root @LAB@ --blend out/<scene>.blend --script scenes/<scene>.py`
   (the scene opens in the right pane; see the Sinopia section). Render 3–5 low-res stills (`--res 640x360 --samples 16`) and LOOK at them (Read the PNG) before any animation.
2. Fix, re-render stills. Only then render the animation: locally (`--anim`, clay/preview) or on a rented GPU (final).
3. Encode: `ffmpeg -framerate 24 -i frames/f%04d.png -i out/audio/mix.wav -c:v libx264 -pix_fmt yuv420p -crf 18 -c:a aac -shortest clip.mp4`.
4. Show: the 3D scene is already in the pane (step 1); a video or a still: `harnessly open <file>`. Send to the team chat: see the announce-release skill (bot in the alerter pod;
   `sendMessage` via `app.tg`, a video needs a multipart `sendVideo` built inside the pod with `app.BOT_TOKEN`).

## The scene appears in Sinopia by itself
The person never fills a form. Every time a scene is saved as `out/<scene>.blend`, make its document and show it:

    harnessly design scene3d add --root @LAB@ \
        --blend out/<scene>.blend --script scenes/<scene>.py

It makes the «3D-сцена» document in the Sinopia library and opens it in the right pane of this chat. Run it again after
every rebuild: the same .blend is the same document (no duplicates), the viewer re-reads the file by itself. Started from
Sinopia's «3D-сцена» start, the first message names a document id: add `--project <id>` so that document becomes the scene.
Then look at it: `harnessly design scene3d get <id>`, `design scene3d frame <id> --cam NAME`, and read the person's pins with
`design scene3d comments <id> --open`.

A scene the person attaches to the chat (`.blend`): `harnessly design scene3d add --file <attachment path>` (it is copied into
`out/` of the project, the document opens). A `.glb`, `.fbx` or `.obj`: turn it into a .blend first —
`blender -b -P conv.py -- in.glb out/x.blend` where conv.py does `bpy.ops.wm.read_factory_settings(use_empty=True)`, then
`bpy.ops.import_scene.gltf(filepath=…)` / `bpy.ops.wm.obj_import(filepath=…)` / `bpy.ops.import_scene.fbx(filepath=…)`, then
`bpy.ops.wm.save_as_mainfile(filepath=…)` — and add that file the same way.

## Gotchas (each cost time once)
- Ray-cast placement: shoot from OUTSIDE inward (`Surface.hit` does it) — rays from inside hit wheel-arch/cockpit walls.
- MPFB: add the rig BEFORE hair/eyes/brows/clothes, otherwise they don't follow the head (floating hair, bulging eyes).
- MPFB purges orphan materials when building a human → set `use_fake_user=True` on your materials first.
- MPFB assets: `makehuman_system_assets_cc0.zip` from **files.makehumancommunity.org** (files2 mirror is ~16 KB/s),
  unzip into `~/.config/blender/5.2/extensions/.user/user_default/mpfb/data`. Install the extension from the zip
  (`blender -b -c extension install-file -r user_default --enable mpfb.zip`) — online sync is disabled.
- MPFB human stands facing -Y with hips at z≈0.874: rotate 90° about Z to face +X, drop hips onto the seat.
  Arm reach is short (shoulder→wrist ≈ 0.5 m): move the seat / lean the spine (spine01-03 rotation X) / aim the wrist ~8 cm short of the target.
- Blender objects can't hold Python attributes (`obj._x = ...` fails) — keep state in dicts.
- Keyframe interpolation: `K.interp("LINEAR"|"BEZIER"|"CONSTANT")` before inserting keys (Blender 5 slotted actions — don't touch `action.fcurves`).
- Packing for the GPU box: `gpu/pack.py` packs every existing image (Poly Haven model blends reference a few missing
  scan textures — skip them, they're unused); `pack_all` fails on them.

## Rendering on a rented GPU (Selectel) — read gpu/README.md before running
`source gpu/sel_auth.sh && python3 -I gpu/render_selectel.py plan` (dry run) → ask the person (it costs money) →
`... render --blend out/render_pack.blend --frames 1-348 --ttl-hours 4` → `... status` must print "nothing alive".
Measured on RTX PRO 6000 Blackwell (290 ₽/h): 15.4 s/frame for the realistic 1080p/64 spp scene (CPU-bound scene
sync per frame — bake animation to cut it), setup ≈ 5–10 min. 14.5 s clip ≈ 1.7 h ≈ 500 ₽.

## Previs for AI video (Higgsfield)
Clay renders are the input for video-to-video: Kling 3.0 Motion Control (draft clip + character image) or Kling Omni FLF
(first/last keyframes rendered here). Keep previs at 720p, low samples — the AI re-dresses it.

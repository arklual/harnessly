# One draft frame of a blender-lab scene (harnessly, internal/design/scene3d.go): small, few samples, one still.
# blender -b SCENE.blend -P scene3d_frame.py -- '{"out": "...jpg", "engine": "cycles", "frame": 1, "cam": {...}, ...}'
# The .blend is never saved.
import json
import math
import os
import sys

import bpy
from mathutils import Vector

spec = json.loads(sys.argv[sys.argv.index("--") + 1])
sc = bpy.context.scene
r = sc.render
r.resolution_x, r.resolution_y, r.resolution_percentage = spec["w"], spec["h"], 100
r.use_motion_blur = False
r.image_settings.file_format = "JPEG"
r.image_settings.quality = 85
# the name is relative to the folder the run started in: Blender does not resolve a bare name the way a shell does
r.filepath = os.path.abspath(spec["out"])
r.film_transparent = False

if spec["engine"] == "eevee":
    r.engine = "BLENDER_EEVEE"
    sc.eevee.taa_render_samples = spec["samples"]
else:
    # No GPU on the shared machine: Cycles on two threads, few samples, denoised, short light paths.
    r.engine = "CYCLES"
    c = sc.cycles
    c.device = "CPU"
    c.samples = spec["samples"]
    c.use_adaptive_sampling = True
    c.use_denoising = True
    c.max_bounces = 4
    c.diffuse_bounces = 2
    c.glossy_bounces = 2
    c.transmission_bounces = 4
    c.volume_bounces = 0
    c.caustics_reflective = False
    c.caustics_refractive = False
    c.sample_clamp_indirect = 8
    r.threads_mode = "FIXED"
    r.threads = 2

sc.frame_set(max(sc.frame_start, min(sc.frame_end, spec["frame"])) if sc.frame_end >= sc.frame_start else spec["frame"])

cam = spec.get("cam") or {}
if cam.get("n") and cam["n"] in bpy.data.objects and not cam.get("free"):
    # a camera of the scene, seen as it is at this frame; the timeline's switches must not take it back
    for m in sc.timeline_markers:
        m.camera = None
    sc.camera = bpy.data.objects[cam["n"]]
elif cam.get("pos"):
    for m in sc.timeline_markers:
        m.camera = None
    cd = bpy.data.cameras.new("hl_view")
    cd.sensor_fit = "VERTICAL"
    cd.sensor_height = 24.0
    cd.lens = 12.0 / math.tan(math.radians(max(1.0, min(170.0, cam.get("fov", 40.0)))) / 2)
    cd.clip_start, cd.clip_end = 0.05, 2000.0
    co = bpy.data.objects.new("hl_view", cd)
    sc.collection.objects.link(co)
    pos, tgt = Vector(cam["pos"]), Vector(cam["target"])
    d = tgt - pos
    if d.length < 1e-6:
        d = Vector((0, 1, 0))
    co.location = pos
    co.rotation_euler = d.to_track_quat("-Z", "Y").to_euler()
    sc.camera = co

bpy.ops.render.render(write_still=True)
print("HL_FRAME_OK")

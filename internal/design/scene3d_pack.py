# Prepares one .blend for a final render on a rented GPU (harnessly, internal/design/scene3d_final.go):
# blender -b SCENE.blend -P scene3d_pack.py -- OUT.blend '{"cam": {...}}'
# Makes the wanted camera the scene's own (the timeline's markers must not take it back) and packs the textures that
# exist on disk into the file, so the render box needs nothing else. The project's scene file is never saved over.
import json
import math
import os
import sys

import bpy
from mathutils import Vector

out, spec = sys.argv[sys.argv.index("--") + 1:][:2]
out = os.path.abspath(out)
cam = json.loads(spec).get("cam") or {}
sc = bpy.context.scene
if cam.get("n") and cam["n"] in bpy.data.objects:
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
    co = bpy.data.objects.new("hl_view", cd)
    sc.collection.objects.link(co)
    pos, tgt = Vector(cam["pos"]), Vector(cam["target"])
    d = tgt - pos if (tgt - pos).length > 1e-6 else Vector((0, 1, 0))
    co.location = pos
    co.rotation_euler = d.to_track_quat("-Z", "Y").to_euler()
    sc.camera = co
missing, packed = [], 0
for img in bpy.data.images:
    if img.source not in {"FILE", "SEQUENCE", "TILED"} or img.packed_file:
        continue
    p = bpy.path.abspath(img.filepath)
    if not os.path.exists(p):
        missing.append(img.name)
        continue
    img.pack()
    packed += 1
bpy.ops.wm.save_as_mainfile(filepath=out, compress=True, copy=True)
print("HL_PACKED", out, "images:", packed, "missing:", len(missing))

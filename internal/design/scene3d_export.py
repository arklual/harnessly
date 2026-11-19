# Exports a blender-lab scene for the browser viewer (harnessly, internal/design/scene3d.go).
# blender -b SCENE.blend -P scene3d_export.py -- OUT.glb OUT.json TRI_BUDGET TEX_MAX
# Writes a .glb (modifiers applied, big meshes decimated, textures shrunk to JPEG) and a meta.json naming the objects,
# cameras, timeline markers and the scene's faults. The .blend is never saved.
import json
import math
import os
import sys

import bpy
from mathutils import Vector

out_glb, out_meta, tri_budget, tex_max = sys.argv[sys.argv.index("--") + 1:][:4]
out_glb, out_meta = os.path.abspath(out_glb), os.path.abspath(out_meta)
tri_budget, tex_max = int(tri_budget), int(tex_max)
sc = bpy.context.scene
objs = list(sc.objects)
dg = bpy.context.evaluated_depsgraph_get()


def tris_of(o):
    if o.type not in ("MESH", "CURVE", "SURFACE", "FONT", "META"):
        return 0
    try:
        ev = o.evaluated_get(dg)
        me = ev.to_mesh()
        n = sum(max(1, len(p.vertices) - 2) for p in me.polygons)
        ev.to_mesh_clear()
        return n
    except Exception:
        return 0


tris = {o.name: tris_of(o) for o in objs}
total = sum(tris.values())

# --- decimate: only when the scene is over the budget, the biggest meshes first
out_total = total
if total > tri_budget:
    ratio = max(0.05, tri_budget / total)
    for o in objs:
        if o.type != "MESH" or tris[o.name] < 2000:
            continue
        if o.data.shape_keys or any(m.type in ("ARMATURE", "SKIN") for m in o.modifiers):
            continue
        m = o.modifiers.new("hl_decimate", "DECIMATE")
        m.ratio = ratio
        out_total -= int(tris[o.name] * (1 - ratio))

# --- textures: shrink the big ones, count the lost ones
missing = []
for img in bpy.data.images:
    if img.source != "FILE" or img.name in ("Render Result", "Viewer Node"):
        continue
    if not img.has_data:
        try:
            img.reload()
        except Exception:
            pass
    if not img.has_data:
        missing.append(img.name)
        continue
    w, h = img.size
    if max(w, h) > tex_max:
        k = tex_max / max(w, h)
        try:
            img.scale(max(1, int(w * k)), max(1, int(h * k)))
        except Exception:
            pass

# --- the scene's faults
unapplied = [o.name for o in objs if o.type == "MESH" and any(abs(s - 1.0) > 1e-3 for s in o.scale)
             and not o.parent]
bmin = Vector((1e9, 1e9, 1e9))
bmax = Vector((-1e9, -1e9, -1e9))
for o in objs:
    if o.type in ("MESH", "CURVE") and o.bound_box:
        for c in o.bound_box:
            w = o.matrix_world @ Vector(c)
            bmin = Vector((min(bmin[i], w[i]) for i in range(3)))
            bmax = Vector((max(bmax[i], w[i]) for i in range(3)))
if bmin[0] > bmax[0]:
    bmin, bmax = Vector((-5, -5, 0)), Vector((5, 5, 3))

# a mesh no camera of the scene sees at the marked frames is "off camera": checked at the markers' frames
cams = [o for o in objs if o.type == "CAMERA"]
off = []
try:
    from bpy_extras.object_utils import world_to_camera_view
    frames = sorted({m.frame for m in sc.timeline_markers if m.camera}) or [sc.frame_start]
    seen = set()
    for f in frames[:8]:
        sc.frame_set(f)
        cam = next((m.camera for m in sc.timeline_markers if m.frame == f and m.camera), sc.camera)
        if not cam:
            continue
        for o in objs:
            if o.type != "MESH" or o.name in seen or tris[o.name] == 0:
                continue
            p = world_to_camera_view(sc, cam, o.matrix_world.translation)
            if -0.1 <= p.x <= 1.1 and -0.1 <= p.y <= 1.1 and p.z > 0:
                seen.add(o.name)
    off = [o.name for o in objs if o.type == "MESH" and tris[o.name] > 0 and o.name not in seen and not o.hide_render]
    sc.frame_set(sc.frame_start)
except Exception:
    off = []

rx, ry = sc.render.resolution_x, sc.render.resolution_y


def fov_of(c):
    d = c.data
    aspect = rx / max(ry, 1)
    if d.sensor_fit == "VERTICAL":
        return math.degrees(2 * math.atan(d.sensor_height / (2 * d.lens)))
    h = 2 * math.atan(d.sensor_width / (2 * d.lens))  # across the longer side (AUTO) or the width
    if d.sensor_fit == "AUTO" and aspect < 1:
        return math.degrees(h)
    return math.degrees(2 * math.atan(math.tan(h / 2) / aspect))


# the cameras' paths: world position, a point 5 m ahead and the field of view, so the viewer shows a scene camera as the
# render has it (markers, parents, constraints and drivers included). A frame costs a full evaluation of the scene, so a
# camera nothing moves is one sample, and the others are sampled at ~20 frames and at the markers, within a time limit.
import time


def moving(o):
    while o:
        if (o.animation_data and (o.animation_data.action or o.animation_data.drivers)) or o.constraints:
            return True
        if o.data and getattr(o.data, "animation_data", None) and o.data.animation_data.action:
            return True
        o = o.parent
    return False


def sample(c, f):
    mw = c.matrix_world
    pos = mw.translation
    tgt = pos + (mw.to_quaternion() @ Vector((0, 0, -1))).normalized() * 5.0
    return [f, round(pos.x, 4), round(pos.y, 4), round(pos.z, 4), round(tgt.x, 4), round(tgt.y, 4), round(tgt.z, 4),
            round(fov_of(c), 3)]


tracks = {c.name: [] for c in cams}
try:
    sc.frame_set(sc.frame_start)
    dg.update()
    dyn = [c for c in cams if moving(c)]
    for c in cams:
        if c not in dyn:
            tracks[c.name].append(sample(c, sc.frame_start))
    if dyn:
        step = max(1, math.ceil((sc.frame_end - sc.frame_start + 1) / 20))
        fs = set(range(sc.frame_start, sc.frame_end + 1, step)) | {sc.frame_end}
        for m in sc.timeline_markers:
            fs |= {x for x in (m.frame - 1, m.frame) if sc.frame_start <= x <= sc.frame_end}
        t0 = time.time()
        for f in sorted(fs):
            if time.time() - t0 > 100:
                break
            sc.frame_set(f)
            dg.update()
            for c in dyn:
                tracks[c.name].append(sample(c, f))
    sc.frame_set(sc.frame_start)
except Exception as e:
    print("HL_TRACK_FAIL", e)

meta = {
    "version": 1,
    "fps": sc.render.fps / (sc.render.fps_base or 1),
    "start": sc.frame_start,
    "end": sc.frame_end,
    "res": [rx, ry],
    "camera": sc.camera.name if sc.camera else "",
    "objects": [
        {"n": o.name, "t": o.type, "p": o.parent.name if o.parent else "", "tris": tris[o.name],
         **({"hidden": True} if o.hide_render else {})}
        for o in objs
    ],
    "cameras": [
        {"n": c.name, "lens": round(c.data.lens, 3), "fov": round(fov_of(c), 3), "near": c.data.clip_start,
         "far": c.data.clip_end, "track": tracks.get(c.name, [])}
        for c in cams
    ],
    "markers": [{"f": m.frame, "n": m.name, "cam": m.camera.name if m.camera else ""} for m in sc.timeline_markers],
    "bounds": [round(v, 3) for v in (*bmin, *bmax)],
    "stats": {
        "tris": total, "trisOut": max(out_total, 0), "objects": len(objs),
        "animated": sum(1 for o in objs if o.animation_data and o.animation_data.action),
        "missingImages": missing[:50], "unappliedScale": unapplied[:50], "offCamera": off[:50],
    },
}
with open(out_meta, "w") as f:
    json.dump(meta, f)

# --- the glTF: only the options this Blender knows are passed
want = dict(
    filepath=out_glb, export_format="GLB", export_apply=True, export_cameras=True, export_lights=False,
    export_animations=True, export_frame_range=True, export_force_sampling=False, export_image_format="JPEG",
    export_jpeg_quality=72, export_yup=True, export_texcoords=True, export_normals=True, export_materials="EXPORT",
    export_draco_mesh_compression_enable=False, use_visible=True, use_renderable=True,
)
known = set(bpy.ops.export_scene.gltf.get_rna_type().properties.keys())
res = bpy.ops.export_scene.gltf(**{k: v for k, v in want.items() if k in known})
print("HL_EXPORT", res)

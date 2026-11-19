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

cams = [o for o in objs if o.type == "CAMERA"]

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
bake = {}  # objects the glTF's actions do not carry (drivers, constraints, NLA): their local transform at the sampled frames
off = []
BAKE_MAX = 400
try:
    from bpy_extras.object_utils import world_to_camera_view

    def baked(o):
        ad = o.animation_data
        return o.type != "CAMERA" and ((ad and (ad.drivers or ad.nla_tracks)) or len(o.constraints) > 0)

    sc.frame_set(sc.frame_start)
    dg.update()
    dyn = [c for c in cams if moving(c)]
    movers = [o for o in objs if baked(o)][:BAKE_MAX]
    for c in cams:
        if c not in dyn:
            tracks[c.name].append(sample(c, sc.frame_start))
    animated_any = bool(dyn) or bool(movers) or any(o.animation_data and o.animation_data.action for o in objs)
    step = max(1, math.ceil((sc.frame_end - sc.frame_start + 1) / 20))
    fs = {sc.frame_start}
    if animated_any:
        fs |= set(range(sc.frame_start, sc.frame_end + 1, step)) | {sc.frame_end}
        for m in sc.timeline_markers:
            fs |= {x for x in (m.frame - 1, m.frame) if sc.frame_start <= x <= sc.frame_end}
    meshes = [o for o in objs if o.type == "MESH" and tris[o.name] > 0 and not o.hide_render]
    seen = set()
    t0 = time.time()
    for f in sorted(fs):
        if time.time() - t0 > 100 and f != sc.frame_start:
            break
        sc.frame_set(f)
        dg.update()
        for c in dyn:
            tracks[c.name].append(sample(c, f))
        for o in movers:
            ev = o.evaluated_get(dg)
            pi = o.matrix_parent_inverse
            ident = all(abs(pi[i][j] - (1.0 if i == j else 0.0)) < 1e-6 for i in range(4) for j in range(4))
            euler = o.rotation_mode == "XYZ" and len(o.constraints) == 0 and ident
            if euler:
                row = [f, *ev.location, *ev.rotation_euler, *ev.scale]
            else:
                loc, rot, scl = ev.matrix_local.decompose()
                row = [f, *loc, rot.w, rot.x, rot.y, rot.z, *scl]
            bake.setdefault(o.name, {"m": "e" if euler else "q", "rows": []})["rows"].append([round(v, 5) for v in row])
        # what a camera sees: the corners of a mesh's box against the camera's frame, at this frame, for every camera
        for cam in cams:
            for o in meshes:
                if o.name in seen:
                    continue
                zs, xs, ys = [], [], []
                behind = False
                for corner in o.bound_box:
                    pnt = world_to_camera_view(sc, cam, o.matrix_world @ Vector(corner))
                    if pnt.z > 0:
                        xs.append(pnt.x)
                        ys.append(pnt.y)
                    else:
                        behind = True
                if not xs:
                    continue
                # some of the box behind the camera: it reaches the frame unless it lies wholly to one side
                if behind or (min(xs) <= 1 and max(xs) >= 0 and min(ys) <= 1 and max(ys) >= 0):
                    seen.add(o.name)
    off = [o.name for o in meshes if o.name not in seen]
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
    "bake": bake,
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

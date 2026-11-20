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

# --- what the saved frame does not show (HRN-1716). A scene is saved at one frame, and the glTF is that frame: an object whose
# hide_render is keyed on later, a curve that grows by its bevel factors, a ring scaled up from zero is empty there, and the viewer
# would show a scene without its letters and threads. The export shows such an object in full (the glTF carries the scale and
# the transform animation by itself); when it is visible goes to the viewer as a track of its own, meta "vis".
saved_frame = sc.frame_current
vis_of = lambda o: not (o.hide_render or o.hide_viewport)
anim = [o for o in sc.objects if (o.animation_data and (o.animation_data.action or o.animation_data.drivers))
        or (o.type == "CURVE" and o.data.animation_data and o.data.animation_data.action)]
vis_track = {}  # name -> [[frame, 1|0], ...] from the first sampled frame, one entry where it changes
reveal = []  # objects shown in full for the export
empty_at_save = []  # what the saved frame shows as nothing: hidden, or scaled to zero, and not later
if anim:
    import time as _t

    _t0 = _t.time()
    _step = max(1, math.ceil((sc.frame_end - sc.frame_start + 1) / 24))
    _fs = set(range(sc.frame_start, sc.frame_end + 1, _step)) | {sc.frame_end, saved_frame}
    _fs |= {m.frame for m in sc.timeline_markers if sc.frame_start <= m.frame <= sc.frame_end}
    _cache = {}

    def _state(f):
        if f not in _cache:
            sc.frame_set(f)
            _cache[f] = ({o.name: vis_of(o) for o in anim}, {o.name: max(o.matrix_world.to_scale()) > 1e-4 for o in anim})
        return _cache[f]

    for f in sorted(_fs):
        if _t.time() - _t0 > 60:
            break
        _state(f)
    _extra = [0]

    def _bisect(a, b):
        # between two sampled frames that differ, the frame it changes on: halving, within a budget
        if b - a <= 1 or _extra[0] > 80 or _t.time() - _t0 > 80:
            return
        if _state(a)[0] == _state(b)[0]:
            return
        m = (a + b) // 2
        _extra[0] += 1
        _state(m)
        _bisect(a, m)
        _bisect(m, b)

    _done = sorted(_cache)
    for a, b in zip(_done, _done[1:]):
        _bisect(a, b)
    _done = sorted(_cache)
    for o in anim:
        seq = [(f, _cache[f][0][o.name]) for f in _done]
        if all(v == seq[0][1] for _, v in seq):
            continue
        track = [[seq[0][0], int(seq[0][1])]]
        for f, v in seq[1:]:
            if int(v) != track[-1][1]:
                track.append([f, int(v)])
        vis_track[o.name] = track
    for o in anim:
        grows = o.type == "CURVE" and o.data.animation_data and o.data.animation_data.action
        if o.name in vis_track or grows:
            reveal.append(o)
        if saved_frame in _cache:
            hid = not _cache[saved_frame][0][o.name]
            flat = not _cache[saved_frame][1][o.name] and any(_cache[f][1][o.name] for f in _done)
            if (o.name in vis_track and hid) or flat:
                empty_at_save.append(o.name)
    sc.frame_set(saved_frame)


def fcurves_of(ad):
    """The F-curves of an ID's animation, whichever way this Blender keeps them (Blender 5: slots, layers, strips, channelbags)."""
    act = ad.action if ad else None
    if not act:
        return
    try:
        yield from act.fcurves
        return
    except AttributeError:
        pass
    slot = getattr(ad, "action_slot", None)
    for layer in act.layers:
        for strip in layer.strips:
            cb = strip.channelbag(slot) if slot else None
            if cb:
                yield from cb.fcurves


def show_in_full():
    """Visibility keys and the bevel keys of a growing curve are muted and the values set whole: the glTF exporter re-evaluates
    the animation on its own, and what it reads is the object as it is then."""
    for o in reveal:
        if o.name in vis_track:
            for fc in fcurves_of(o.animation_data):
                if fc.data_path in ("hide_render", "hide_viewport"):
                    fc.mute = True
            o.hide_render = False
            o.hide_viewport = False
        if o.type == "CURVE" and o.data.animation_data and o.data.animation_data.action:
            for fc in fcurves_of(o.data.animation_data):
                if fc.data_path in ("bevel_factor_start", "bevel_factor_end"):
                    fc.mute = True
            o.data.bevel_factor_start = 0.0
            o.data.bevel_factor_end = 1.0
    bpy.context.view_layer.update()


show_in_full()


def poly_tris(me):
    return sum(max(1, len(p.vertices) - 2) for p in me.polygons)


def tris_of(o):
    if o.type not in ("MESH", "CURVE", "SURFACE", "FONT", "META"):
        return 0
    try:
        if o.type == "MESH" and len(o.modifiers) == 0:
            return poly_tris(o.data)  # no evaluation for what has no modifiers: a scene of scattered plants is millions
        ev = o.evaluated_get(dg)
        me = ev.to_mesh()
        n = poly_tris(me)
        ev.to_mesh_clear()
        return n
    except Exception:
        return 0


tris = {o.name: tris_of(o) for o in objs}
total = sum(tris.values())

# --- decimate to the budget. A scene of scattered plants and rocks is tens of millions of triangles of which the browser can show
# a few hundred thousand: what lies near a camera keeps its detail, what lies far gets little. Objects that share a mesh share
# the decimated copy, so the file does not carry it once for each of them.
out_total = total
if total > tri_budget:
    import collections

    # where the cameras are, at a few frames of the animation, and how far each mesh is from the nearest
    cam_objs = [o for o in objs if o.type == "CAMERA"]
    probe = sorted({sc.frame_start, sc.frame_end} | {sc.frame_start + (sc.frame_end - sc.frame_start) * k // 4 for k in (1, 2, 3)})
    near = {}
    meshes_all = [o for o in objs if o.type == "MESH" and tris[o.name] > 0]
    for f in probe:
        sc.frame_set(f)
        dg.update()
        cams_at = [c.matrix_world.translation.copy() for c in cam_objs] or [Vector((0, 0, 0))]
        for o in meshes_all:
            pos = o.matrix_world.translation
            d = min((pos - c).length for c in cams_at)
            near[o.name] = min(near.get(o.name, 1e9), d)
    sc.frame_set(sc.frame_start)
    show_in_full()
    dg.update()
    weight = {o.name: tris[o.name] / (1.0 + (near.get(o.name, 1e9) / 8.0) ** 2) for o in meshes_all}
    wsum = sum(weight.values()) or 1.0
    FLOOR = 48  # a mesh keeps at least this many triangles: it is still a recognisable thing
    CAP = 30000
    work = bpy.data.scenes.new("hl_work")
    by_data = collections.defaultdict(list)
    for o in meshes_all:
        if o.data.shape_keys or any(m.type in ("ARMATURE", "SKIN") for m in o.modifiers):
            continue
        by_data[o.data].append(o)
    for data, users in by_data.items():
        n = max(tris[u.name] for u in users)
        want = max(weight[u.name] / wsum * tri_budget for u in users)  # the best-placed user's share
        # no single mesh needs more than CAP triangles in a browser, however near a camera it is
        target = min(n, CAP, max(FLOOR, int(want)))
        if target >= n:
            continue
        # The modifier is applied to a copy of the mesh on an object of its own: an object that is not visible in the view layer
        # (the scatter is hidden for the viewport) is not evaluated, and its Decimate would change nothing.
        # On a scene of its own, so that the evaluation sees one object, not the sixteen thousand of the real scene.
        tmp_mesh = data.copy()
        tmp = bpy.data.objects.new("hl_tmp", tmp_mesh)
        work.collection.objects.link(tmp)
        m = tmp.modifiers.new("hl_decimate", "DECIMATE")
        m.ratio = max(0.001, target / n)
        try:
            with bpy.context.temp_override(scene=work, view_layer=work.view_layers[0], active_object=tmp, object=tmp, selected_objects=[tmp]):
                bpy.ops.object.modifier_apply(modifier=m.name)
            done = tmp.data
        except Exception as e:
            print("HL_DECIMATE_FAIL", data.name, e)
            done = None
        work.collection.objects.unlink(tmp)
        bpy.data.objects.remove(tmp)
        if done is None:
            bpy.data.meshes.remove(tmp_mesh)
            continue
        for u in users:
            u.data = done
    out_total = sum(poly_tris(o.data) for o in meshes_all)
    dg = bpy.context.evaluated_depsgraph_get()

# --- textures: shrink the big ones to a small JPEG (or PNG where there is an alpha: leaves), count the lost ones. The exporter reads
# the file an image points at, not the pixels in memory, so a shrunk image is written to a file of its own and pointed at it.
import tempfile

tex_dir = tempfile.mkdtemp(prefix="hl-tex-")
missing = []
for img in bpy.data.images:
    if img.source != "FILE" or img.name in ("Render Result", "Viewer Node"):
        continue
    # An image is loaded when something asks for its size: has_data is False for one nobody has looked at, so it is not the test.
    w, h = img.size
    if w == 0 or h == 0:
        try:
            img.reload()
        except Exception:
            pass
        w, h = img.size
    if w == 0 or h == 0:
        missing.append(img.name)
        continue
    ext = os.path.splitext(img.filepath)[1].lower()
    if max(w, h) <= tex_max and ext in (".jpg", ".jpeg", ".png"):
        continue
    try:
        if max(w, h) > tex_max:
            k = tex_max / max(w, h)
            img.scale(max(1, int(w * k)), max(1, int(h * k)))
        alpha = img.depth in (32, 128) and ext in (".png", ".tga", ".tif", ".tiff")
        name = "".join(ch if ch.isalnum() else "_" for ch in img.name)[:80]
        path = os.path.join(tex_dir, name + (".png" if alpha else ".jpg"))
        img.filepath_raw = path
        img.file_format = "PNG" if alpha else "JPEG"
        if not alpha:
            img.save(quality=82)
        else:
            img.save()
    except Exception as e:
        print("HL_TEX_FAIL", img.name, e)

# an image whose file is gone has no size and the exporter refuses it: nothing is shown in its place, the rest goes through
gone = set(missing)
for mat in bpy.data.materials:
    if mat.use_nodes and mat.node_tree:
        for n in mat.node_tree.nodes:
            if n.type == "TEX_IMAGE" and n.image and n.image.name in gone:
                n.image = None

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

sc.frame_set(saved_frame)
show_in_full()

meta = {
    "version": 1,
    "fps": sc.render.fps / (sc.render.fps_base or 1),
    "start": sc.frame_start,
    "end": sc.frame_end,
    "res": [rx, ry],
    "camera": sc.camera.name if sc.camera else "",
    "objects": [
        {"n": o.name, "t": o.type, "p": o.parent.name if o.parent else "", "tris": tris[o.name],
         **({"hidden": True} if o.hide_render and o.name not in vis_track else {}),
         **({"vis": vis_track[o.name]} if o.name in vis_track else {})}
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
        "missingImages": missing[:50], "emptyAtSave": empty_at_save[:50], "unappliedScale": unapplied[:50], "offCamera": off[:50],
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
args = {k: v for k, v in want.items() if k in known}
try:
    res = bpy.ops.export_scene.gltf(**args)
except Exception as e:  # a texture the exporter cannot take must not cost the whole model: once more with no materials
    print("HL_EXPORT_RETRY", e)
    args["export_materials"] = "NONE"
    res = bpy.ops.export_scene.gltf(**args)
print("HL_EXPORT", res)

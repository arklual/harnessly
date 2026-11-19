# The outputs of a scene for a generator (harnessly, internal/design/scene3d_outputs.go): for a few frames of one camera, small
# pictures of what the scene is made of — the beauty keyframe, clay, depth, normals and masks of chosen objects.
# blender -b SCENE.blend -P scene3d_outputs.py -- '{"dir": "...", "cam": {...}, "frames": [1, 60], "w": 640, "h": 360,
#   "samples": 4, "passes": ["beauty", "clay", "depth", "normals"], "masks": ["body"]}'
# Every picture is drawn by Cycles with a material put over the whole scene, so no compositor is needed. The .blend is never saved.
import json
import math
import os
import sys

import bpy
from mathutils import Vector

spec = json.loads(sys.argv[sys.argv.index("--") + 1])
out = os.path.abspath(spec["dir"])
os.makedirs(out, exist_ok=True)
sc = bpy.context.scene
r = sc.render
r.resolution_x, r.resolution_y, r.resolution_percentage = spec["w"], spec["h"], 100
r.use_motion_blur = False
r.film_transparent = False
r.engine = "CYCLES"
c = sc.cycles
c.device = "CPU"
c.samples = spec["samples"]
c.use_denoising = False
c.max_bounces = 2
r.threads_mode = "FIXED"
r.threads = 2

cam = spec.get("cam") or {}
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
    cd.clip_start, cd.clip_end = 0.05, 2000.0
    co = bpy.data.objects.new("hl_view", cd)
    sc.collection.objects.link(co)
    pos, tgt = Vector(cam["pos"]), Vector(cam["target"])
    d = tgt - pos if (tgt - pos).length > 1e-6 else Vector((0, 1, 0))
    co.location = pos
    co.rotation_euler = d.to_track_quat("-Z", "Y").to_euler()
    sc.camera = co
camobj = sc.camera


def emission_material(name, build):
    mat = bpy.data.materials.new(name)
    mat.use_nodes = True
    nt = mat.node_tree
    nt.nodes.clear()
    out_node = nt.nodes.new("ShaderNodeOutputMaterial")
    em = nt.nodes.new("ShaderNodeEmission")
    em.inputs["Strength"].default_value = 1.0
    nt.links.new(build(nt), em.inputs["Color"])
    nt.links.new(em.outputs["Emission"], out_node.inputs["Surface"])
    return mat


def depth_color(nt):
    far = float(spec.get("far", 30.0))
    geo = nt.nodes.new("ShaderNodeNewGeometry")
    dist = nt.nodes.new("ShaderNodeVectorMath")
    dist.operation = "DISTANCE"
    dist.inputs[1].default_value = camobj.matrix_world.translation
    nt.links.new(geo.outputs["Position"], dist.inputs[0])
    rng = nt.nodes.new("ShaderNodeMapRange")
    rng.inputs["From Min"].default_value = 0.0
    rng.inputs["From Max"].default_value = far
    rng.inputs["To Min"].default_value = 1.0  # near is white, far is black
    rng.inputs["To Max"].default_value = 0.0
    rng.clamp = True
    nt.links.new(dist.outputs["Value"], rng.inputs["Value"])
    comb = nt.nodes.new("ShaderNodeCombineColor")
    for k in ("Red", "Green", "Blue"):
        nt.links.new(rng.outputs["Result"], comb.inputs[k])
    return comb.outputs["Color"]


def normal_color(nt):
    geo = nt.nodes.new("ShaderNodeNewGeometry")
    mul = nt.nodes.new("ShaderNodeVectorMath")
    mul.operation = "MULTIPLY_ADD"
    mul.inputs[1].default_value = (0.5, 0.5, 0.5)
    mul.inputs[2].default_value = (0.5, 0.5, 0.5)
    nt.links.new(geo.outputs["Normal"], mul.inputs[0])
    return mul.outputs["Vector"]


def flat(color):
    def build(nt):
        n = nt.nodes.new("ShaderNodeRGB")
        n.outputs[0].default_value = color
        return n.outputs[0]
    return build


def clay_material():
    mat = bpy.data.materials.new("hl_clay")
    mat.use_nodes = True
    bsdf = mat.node_tree.nodes.get("Principled BSDF")
    bsdf.inputs["Base Color"].default_value = (0.78, 0.78, 0.8, 1)
    bsdf.inputs["Roughness"].default_value = 0.85
    return mat


def shoot(path, material=None, jpg=False, data=False):
    sc.view_layers[0].material_override = material
    # data passes: nothing behind the scene is black (far, no surface), not the sky
    r.film_transparent = data
    r.image_settings.file_format = "JPEG" if jpg else "PNG"
    if jpg:
        r.image_settings.quality = 88
    else:
        r.image_settings.color_mode = "RGB"
        r.image_settings.color_depth = "8"
    r.filepath = path
    bpy.ops.render.render(write_still=True)


# Standard view transform: the values written are the values drawn (depth and normals are data, not a photograph).
vs = sc.view_settings
raw = (vs.view_transform, vs.look)
passes = spec.get("passes") or ["beauty"]
mats = {
    "clay": clay_material,
    "depth": lambda: emission_material("hl_depth", depth_color),
    "normals": lambda: emission_material("hl_normals", normal_color),
}
files = []
for f in spec["frames"]:
    sc.frame_set(int(f))
    tag = "%04d" % int(f)
    row = {"frame": int(f)}
    if "beauty" in passes:
        vs.view_transform, vs.look = raw
        name = "beauty_%s.jpg" % tag
        shoot(os.path.join(out, name), None, jpg=True)
        row["beauty"] = name
    for p in ("clay", "depth", "normals"):
        if p in passes:
            vs.view_transform = "Standard" if p != "clay" else raw[0]
            vs.look = "None" if p != "clay" else raw[1]
            name = "%s_%s.%s" % (p, tag, "jpg" if p == "clay" else "png")
            shoot(os.path.join(out, name), mats[p](), jpg=(p == "clay"), data=p in ("depth", "normals"))
            row[p] = name
    files.append(row)
    # the masks: the object alone, white, on a transparent film; the mask is the picture's alpha
    for obj_name in spec.get("masks") or []:
        ob = bpy.data.objects.get(obj_name)
        if not ob or ob.type != "MESH":
            continue
        vs.view_transform, vs.look = "Standard", "None"
        white = emission_material("hl_white", flat((1, 1, 1, 1)))
        saved = [s.material for s in ob.material_slots]
        if not ob.material_slots:
            ob.data.materials.append(white)
        for s in ob.material_slots:
            s.material = white
        hidden = []
        for o in sc.objects:
            if o != ob and o.type in {"MESH", "CURVE", "SURFACE", "FONT"} and not o.hide_render:
                o.hide_render = True
                hidden.append(o)
        sc.view_layers[0].material_override = None
        r.film_transparent = True
        r.image_settings.file_format = "PNG"
        r.image_settings.color_mode = "RGBA"
        name = "mask_%s_%s.png" % ("".join(ch if ch.isalnum() else "-" for ch in obj_name)[:40], tag)
        r.filepath = os.path.join(out, name)
        bpy.ops.render.render(write_still=True)
        r.film_transparent = False
        r.image_settings.color_mode = "RGB"
        for o in hidden:
            o.hide_render = False
        for s, m in zip(ob.material_slots, saved):
            s.material = m
        files.append({"frame": int(f), "mask": name, "object": obj_name})
with open(os.path.join(out, "frames.json"), "w") as fh:
    json.dump(files, fh)
print("HL_OUTPUTS_OK", len(files))

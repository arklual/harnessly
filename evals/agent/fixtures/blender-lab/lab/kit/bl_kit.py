"""Small helper kit for code-driven Blender scenes (Blender 5.2, headless).

Import from a scene script:
    import sys; sys.path.insert(0, "<blender-lab>/kit"); import bl_kit as K
"""
import math
import sys

import bmesh
import bpy
from mathutils import Matrix, Vector
from mathutils.bvhtree import BVHTree


# ---------- scene ----------

def reset():
    bpy.ops.wm.read_factory_settings(use_empty=True)
    sc = bpy.context.scene
    sc.render.fps = 24
    return sc


def args():
    """Arguments after `--` on the blender command line."""
    a = sys.argv
    return a[a.index("--") + 1:] if "--" in a else []


def link(obj, parent=None):
    bpy.context.scene.collection.objects.link(obj)
    if parent is not None:
        obj.parent = parent
    return obj


def empty(name, loc=(0, 0, 0), parent=None, size=0.3):
    e = bpy.data.objects.new(name, None)
    e.empty_display_size = size
    e.location = loc
    return link(e, parent)


def interp(kind):
    """Interpolation for keyframes inserted from now on: LINEAR / BEZIER / CONSTANT."""
    bpy.context.preferences.edit.keyframe_new_interpolation_type = kind


def key(obj, path, frame, value):
    setattr(obj, path, value)
    obj.keyframe_insert(data_path=path, frame=frame)


# ---------- materials ----------

def mat(name, color=0.8, rough=0.5, metal=0.0, transmission=0.0, coat=0.0, emission=0.0):
    m = bpy.data.materials.new(name)
    m.use_nodes = True
    b = m.node_tree.nodes.get("Principled BSDF")
    c = color if isinstance(color, (tuple, list)) else (color, color, color)
    b.inputs["Base Color"].default_value = (*c, 1)
    b.inputs["Roughness"].default_value = rough
    b.inputs["Metallic"].default_value = metal
    b.inputs["Transmission Weight"].default_value = transmission
    b.inputs["Coat Weight"].default_value = coat
    if emission:
        b.inputs["Emission Color"].default_value = (*c, 1)
        b.inputs["Emission Strength"].default_value = emission
    return m


def set_mat(obj, m):
    obj.data.materials.clear()
    obj.data.materials.append(m)
    return obj


# ---------- mesh building ----------

def mesh_obj(name, bm, parent=None, smooth=True):
    me = bpy.data.meshes.new(name)
    bm.to_mesh(me)
    bm.free()
    if smooth:
        me.shade_smooth()
    return link(bpy.data.objects.new(name, me), parent)


def loft(name, rings, closed=True, cap=True, parent=None):
    """Skin a list of rings (lists of Vector, equal length) into a surface."""
    bm = bmesh.new()
    vr = [[bm.verts.new(p) for p in r] for r in rings]
    n = len(rings[0])
    span = n if closed else n - 1
    for a, b in zip(vr, vr[1:]):
        for j in range(span):
            k = (j + 1) % n
            bm.faces.new((a[j], a[k], b[k], b[j]))
    if cap and closed:
        for r, flip in ((vr[0], True), (vr[-1], False)):
            c = bm.verts.new(sum((v.co for v in r), Vector()) / n)
            for j in range(n):
                f = (r[j], r[(j + 1) % n], c)
                bm.faces.new(f[::-1] if flip else f)
    bmesh.ops.recalc_face_normals(bm, faces=bm.faces)
    return mesh_obj(name, bm, parent)


def box(name, size, loc=(0, 0, 0), rot=(0, 0, 0), bevel=0.0, segs=3, parent=None, smooth=True):
    bm = bmesh.new()
    bmesh.ops.create_cube(bm, size=1.0)
    bmesh.ops.scale(bm, vec=Vector(size), verts=bm.verts)
    o = mesh_obj(name, bm, parent, smooth)
    o.location, o.rotation_euler = loc, rot
    if bevel:
        m = o.modifiers.new("bevel", "BEVEL")
        m.width, m.segments, m.limit_method = bevel, segs, "NONE"
    return o


def cylinder(name, r, depth, loc=(0, 0, 0), rot=(0, 0, 0), segs=48, parent=None):
    bm = bmesh.new()
    bmesh.ops.create_cone(bm, cap_ends=True, segments=segs, radius1=r, radius2=r, depth=depth)
    o = mesh_obj(name, bm, parent)
    o.location, o.rotation_euler = loc, rot
    return o


def sphere(name, r, loc=(0, 0, 0), scale=(1, 1, 1), parent=None, segs=32):
    bm = bmesh.new()
    bmesh.ops.create_uvsphere(bm, u_segments=segs, v_segments=segs // 2, radius=r)
    o = mesh_obj(name, bm, parent)
    o.location, o.scale = loc, scale
    return o


def torus(name, R, r, loc=(0, 0, 0), rot=(0, 0, 0), segs=64, rsegs=16, parent=None):
    bm = bmesh.new()
    rings = []
    for i in range(segs):
        a = 2 * math.pi * i / segs
        ring = []
        for j in range(rsegs):
            b = 2 * math.pi * j / rsegs
            d = R + r * math.cos(b)
            ring.append(bm.verts.new((d * math.cos(a), d * math.sin(a), r * math.sin(b))))
        rings.append(ring)
    for i in range(segs):
        a, c = rings[i], rings[(i + 1) % segs]
        for j in range(rsegs):
            k = (j + 1) % rsegs
            bm.faces.new((a[j], c[j], c[k], a[k]))
    o = mesh_obj(name, bm, parent)
    o.location, o.rotation_euler = loc, rot
    return o


def revolve(name, profile, segs=96, parent=None, tweak=None):
    """Revolve a (radius, y) profile around the Y axis. tweak(i, j, r, y) -> (r, y)."""
    bm = bmesh.new()
    rings = []
    for i in range(segs):
        a = 2 * math.pi * i / segs
        ring = []
        for j, (r, y) in enumerate(profile):
            if tweak:
                r, y = tweak(i, j, r, y)
            ring.append(bm.verts.new((r * math.cos(a), y, r * math.sin(a))))
        rings.append(ring)
    n = len(profile)
    for i in range(segs):
        a, c = rings[i], rings[(i + 1) % segs]
        for j in range(n - 1):
            bm.faces.new((a[j], a[j + 1], c[j + 1], c[j]))
    bmesh.ops.recalc_face_normals(bm, faces=bm.faces)
    return mesh_obj(name, bm, parent)


def curve(name, pts, bevel=0.02, cyclic=False, parent=None, radii=None, res=6):
    cu = bpy.data.curves.new(name, "CURVE")
    cu.dimensions = "3D"
    cu.bevel_depth, cu.bevel_resolution = bevel, res
    cu.use_fill_caps = True
    sp = cu.splines.new("POLY")
    sp.points.add(len(pts) - 1)
    for i, p in enumerate(pts):
        sp.points[i].co = (*p, 1)
        if radii:
            sp.points[i].radius = radii[i]
    sp.use_cyclic_u = cyclic
    sp.use_smooth = True
    return link(bpy.data.objects.new(name, cu), parent)


# ---------- modifiers ----------

def subsurf(o, lvl=2):
    m = o.modifiers.new("subsurf", "SUBSURF")
    m.levels = m.render_levels = lvl
    return m


def boolean(o, cutter, op="DIFFERENCE"):
    m = o.modifiers.new("bool_" + cutter.name, "BOOLEAN")
    m.object, m.operation, m.solver = cutter, op, "EXACT"
    cutter.hide_render = True
    cutter.display_type = "WIRE"
    cutter.visible_camera = cutter.visible_shadow = False
    return m


def solidify(o, t, offset=-1):
    m = o.modifiers.new("solidify", "SOLIDIFY")
    m.thickness, m.offset = t, offset
    return m


# ---------- surface placement ----------

class Surface:
    """Ray-cast against an object's evaluated mesh (world space) to stick details onto it."""

    def __init__(self, obj):
        dg = bpy.context.evaluated_depsgraph_get()
        ev = obj.evaluated_get(dg)
        me = ev.to_mesh()
        mw = obj.matrix_world
        verts = [mw @ v.co for v in me.vertices]
        polys = [tuple(p.vertices) for p in me.polygons]
        self.bvh = BVHTree.FromPolygons(verts, polys)
        ev.to_mesh_clear()

    def hit(self, origin, direction, reach=20.0):
        """Outer surface point seen from `origin` looking along `direction`.

        The ray is shot from far outside (origin + direction*reach) back towards origin, so
        cavities (wheel arches, cockpit) behind the outer skin never catch it.
        """
        d = Vector(direction).normalized()
        start = Vector(origin) + d * reach
        loc, nor, _i, _d = self.bvh.ray_cast(start, -d, reach * 2)
        return (loc, nor) if loc is not None else (None, None)


def align_z(normal):
    """Rotation that points local +Z along `normal`."""
    return Vector((0, 0, 1)).rotation_difference(Vector(normal)).to_euler()


# ---------- drivers ----------

def driver(obj, path, index, expr, vars_=()):
    """vars_: (name, target_obj, 'LOC_X'|..., 'WORLD_SPACE'|'LOCAL_SPACE')"""
    fc = obj.driver_add(path, index)
    d = fc.driver
    d.type, d.expression = "SCRIPTED", expr
    for name, tgt, chan, space in vars_:
        v = d.variables.new()
        v.name, v.type = name, "TRANSFORMS"
        v.targets[0].id = tgt
        v.targets[0].transform_type = chan
        v.targets[0].transform_space = space
    return fc


# ---------- render ----------

def cycles(sc, w=960, h=540, samples=32, threads=8, blur=True):
    sc.render.engine = "CYCLES"
    sc.cycles.device = "CPU"
    sc.cycles.samples = samples
    sc.cycles.use_adaptive_sampling = True
    sc.cycles.use_denoising = True
    sc.cycles.max_bounces = 4
    sc.cycles.diffuse_bounces = 2
    sc.cycles.glossy_bounces = 2
    sc.cycles.transmission_bounces = 4
    sc.render.resolution_x, sc.render.resolution_y = w, h
    sc.render.threads_mode, sc.render.threads = "FIXED", threads
    sc.render.use_motion_blur = blur
    sc.render.motion_blur_shutter = 0.5
    sc.view_settings.view_transform = "AgX"
    try:
        sc.view_settings.look = "AgX - Medium High Contrast"
    except TypeError:
        pass

// What the 3D view works out from the scene's description, apart from the screen: Blender's axes and three.js's, the
// camera the timeline's markers pick at a frame, the tree of objects, the time of a frame and the scene's faults.
import type {
  IScene3dCamera,
  IScene3dMarker,
  IScene3dMeta,
  IScene3dObject,
  TVec3,
} from '@/next/features/designly/scene3d/types';

/** Blender is z up and looks along y; glTF (and so three.js) is y up: x stays, y becomes -z, z becomes y. */
export const blenderToThree = (v: TVec3): TVec3 => [v[0], v[2], -v[1]];
export const threeToBlender = (v: TVec3): TVec3 => [v[0], -v[2], v[1]];

const round = (n: number, digits = 3): number => {
  const k = 10 ** digits;
  return Math.round(n * k) / k;
};
export const roundVec = (v: TVec3, digits = 3): TVec3 => [
  round(v[0], digits),
  round(v[1], digits),
  round(v[2], digits),
];

/** The camera of the scene's timeline at a frame: the last marker at or before it that binds one; else the scene's own. */
export const cameraAt = (meta: IScene3dMeta, frame: number): string => {
  let name = '';
  let at = -Infinity;
  for (const m of meta.markers) {
    if (!m.cam || m.f > frame || m.f < at) continue;
    name = m.cam;
    at = m.f;
  }
  return name || meta.camera || meta.cameras[0]?.n || '';
};

/** Seconds from the scene's start to a frame, and back (to a whole frame within the scene). */
export const timeOf = (meta: IScene3dMeta, frame: number): number => (frame - meta.start) / (meta.fps || 24);
export const frameOf = (meta: IScene3dMeta, seconds: number): number =>
  Math.min(meta.end, Math.max(meta.start, Math.round(meta.start + seconds * (meta.fps || 24))));

export const durationOf = (meta: IScene3dMeta): number => Math.max(0, timeOf(meta, meta.end));

/** «0:07.5» for the timeline. */
export const clock = (seconds: number): string => {
  const tenths = Math.round(Math.max(0, seconds) * 10);
  const m = Math.floor(tenths / 600);
  const rest = (tenths % 600) / 10;
  return `${m}:${rest < 10 ? '0' : ''}${rest.toFixed(1)}`;
};

export interface ITreeNode {
  object: IScene3dObject;
  children: ITreeNode[];
}

/** The objects as a forest by parent; an object whose parent is not there is a root. */
export const treeOf = (objects: readonly IScene3dObject[]): ITreeNode[] => {
  const nodes = new Map<string, ITreeNode>();
  for (const o of objects) nodes.set(o.n, { object: o, children: [] });
  const roots: ITreeNode[] = [];
  for (const o of objects) {
    const node = nodes.get(o.n);
    if (!node) continue;
    const parent = o.p ? nodes.get(o.p) : undefined;
    if (parent && parent !== node) parent.children.push(node);
    else roots.push(node);
  }
  return roots;
};

/** The names from an object up to its root (the object last): what the tree opens to show it. */
export const pathTo = (objects: readonly IScene3dObject[], name: string): string[] => {
  const by = new Map(objects.map((o) => [o.n, o]));
  const out: string[] = [];
  for (let cur = by.get(name); cur && out.length < 64; cur = cur.p ? by.get(cur.p) : undefined)
    out.unshift(cur.n);
  return out;
};

/** The name Blender gave an object, the glTF's node name: dots are kept; a name three.js made unique has a «_1» tail. */
export const sceneName = (n: string): string => n;

export const CHECK = {
  tris: 'tris',
  textures: 'textures',
  scale: 'scale',
  offCamera: 'offCamera',
  emptyAtSave: 'emptyAtSave',
} as const;
export type TCheck = (typeof CHECK)[keyof typeof CHECK];
export interface ISceneCheck {
  code: TCheck;
  /** The objects or files the finding is about, at most a few. */
  names: string[];
  count: number;
}

/** The scene's faults the export measured: too many polygons for the browser, lost textures, scale not applied, objects
 *  no camera of the scene sees. */
export const checksOf = (meta: IScene3dMeta, triLimit = 2_000_000): ISceneCheck[] => {
  const out: ISceneCheck[] = [];
  const put = (code: TCheck, names: readonly string[] | undefined) => {
    if (names && names.length > 0) out.push({ code, names: names.slice(0, 5), count: names.length });
  };
  if (meta.stats.tris > triLimit) out.push({ code: CHECK.tris, names: [], count: meta.stats.tris });
  put(CHECK.textures, meta.stats.missingImages);
  put(CHECK.scale, meta.stats.unappliedScale);
  put(CHECK.offCamera, meta.stats.offCamera);
  put(CHECK.emptyAtSave, meta.stats.emptyAtSave);
  return out;
};

/** A marker list in frame order, for the timeline's ticks. */
export const markersOf = (meta: IScene3dMeta): IScene3dMarker[] =>
  [...meta.markers].sort((a, b) => a.f - b.f);

/** The horizontal field of view for a vertical one at an aspect (degrees), for the viewer's camera. */
export const hFov = (vfov: number, aspect: number): number =>
  (2 * Math.atan(Math.tan((vfov * Math.PI) / 360) * aspect) * 180) / Math.PI;

/** Whether an object with a visibility track is on at a frame: the last entry at or before it; before the first, the first. */
export const visibleAt = (vis: readonly (readonly number[])[], frame: number): boolean => {
  let on = (vis[0]?.[1] ?? 1) !== 0;
  for (const e of vis) {
    if ((e[0] ?? 0) > frame) break;
    on = (e[1] ?? 1) !== 0;
  }
  return on;
};

/** The two samples a frame lies between in a baked track and how far along (0..1) it is; before the first and after the last the
 *  nearest one holds. */
export const bakeSpan = (
  rows: readonly (readonly number[])[],
  frame: number,
): { a: readonly number[]; b: readonly number[]; k: number } | null => {
  if (rows.length === 0) return null;
  let i = 0;
  while (i < rows.length - 1 && (rows[i + 1]?.[0] ?? 0) <= frame) i++;
  const a = rows[i] as readonly number[];
  const b = (rows[Math.min(i + 1, rows.length - 1)] ?? a) as readonly number[];
  const span = (b[0] ?? 0) - (a[0] ?? 0);
  return { a, b, k: span <= 0 ? 0 : Math.min(1, Math.max(0, (frame - (a[0] ?? 0)) / span)) };
};

export interface ICamPose {
  pos: TVec3;
  target: TVec3;
  fov: number;
}

const lerp = (a: number, b: number, k: number): number => a + (b - a) * k;

/** A scene camera's pose at a frame from its sampled path (Blender's axes): between two samples it is the mix of both; before
 *  the first and after the last it holds. A camera without a path is where its description says, looking along +y. */
export const poseAt = (camera: IScene3dCamera, frame: number): ICamPose => {
  const t = camera.track;
  const row = (r: readonly number[]): ICamPose => ({
    pos: [r[1] ?? 0, r[2] ?? 0, r[3] ?? 0],
    target: [r[4] ?? 0, r[5] ?? 0, r[6] ?? 0],
    fov: r[7] ?? camera.fov,
  });
  const first = t[0];
  const last = t[t.length - 1];
  if (!first || !last) return { pos: [0, 0, 1.5], target: [0, 5, 1.5], fov: camera.fov };
  if (frame <= (first[0] ?? 0)) return row(first);
  if (frame >= (last[0] ?? 0)) return row(last);
  let i = 1;
  while (i < t.length - 1 && (t[i]?.[0] ?? 0) < frame) i++;
  const a = t[i - 1] as readonly number[];
  const b = t[i] as readonly number[];
  const span = (b[0] ?? 0) - (a[0] ?? 0);
  // Two samples one frame apart across a cut are not a move: the later one holds from its frame.
  const k = span <= 0 ? 1 : (frame - (a[0] ?? 0)) / span;
  const mix = (n: number) => lerp(a[n] ?? 0, b[n] ?? 0, k);
  return { pos: [mix(1), mix(2), mix(3)], target: [mix(4), mix(5), mix(6)], fov: mix(7) };
};

/** The vertical field of view of the canvas's camera so that the frame guide (the render's aspect, fitted in the canvas)
 *  shows what the render with `vfov` shows. */
export const canvasFov = (vfov: number, renderAspect: number, canvasAspect: number): number => {
  if (canvasAspect >= renderAspect) return vfov;
  const guideH = canvasAspect / renderAspect; // the guide's height as a part of the canvas's, when it is width-limited
  return (2 * Math.atan(Math.tan((vfov * Math.PI) / 360) / guideH) * 180) / Math.PI;
};

// The 3D scene's answers, read as the page uses them: what is not in the shape is dropped, an answer that is not a scene
// is null. Looser than the video's reader on purpose: the runtime and the page ship together.
import {
  FRAME_STATUS,
  MODEL_STATUS,
  type IScene3dAnchor,
  type IScene3dCam,
  type IScene3dClip,
  type IScene3dFinal,
  type IScene3dGeneration,
  type IScene3dOutputs,
  type IScene3dFrame,
  type IScene3dMeta,
  type IScene3dPin,
  type IScene3dState,
  type IScene3dViewCmd,
  type TFrameStatus,
  type TModelStatus,
  type TVec3,
} from '@/next/features/designly/scene3d/types';

type TRec = Record<string, unknown>;

const rec = (v: unknown): TRec | null =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as TRec) : null;
const str = (v: unknown, d = ''): string => (typeof v === 'string' ? v : d);
const num = (v: unknown, d = 0): number => (typeof v === 'number' && Number.isFinite(v) ? v : d);
const arr = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

const vec = (v: unknown): TVec3 | null => {
  const a = arr(v);
  if (a.length !== 3 || a.some((x) => typeof x !== 'number' || !Number.isFinite(x))) return null;
  return [a[0] as number, a[1] as number, a[2] as number];
};

export const readCam = (v: unknown): IScene3dCam | null => {
  const r = rec(v);
  if (!r) return null;
  return {
    ...(typeof r.n === 'string' && r.n ? { n: r.n } : {}),
    pos: vec(r.pos) ?? [0, 0, 0],
    target: vec(r.target) ?? [0, 0, 0],
    fov: num(r.fov),
  };
};

const readAnchor = (v: unknown): IScene3dAnchor | null => {
  const r = rec(v);
  if (!r) return null;
  const p = vec(r.p);
  const cam = readCam(r.cam);
  const f = rec(r.fix);
  const fix = f
    ? {
        ...(typeof f.before === 'string' && f.before ? { before: f.before } : {}),
        ...(typeof f.after === 'string' && f.after ? { after: f.after } : {}),
        ...(typeof f.note === 'string' && f.note ? { note: f.note } : {}),
        at: str(f.at),
      }
    : null;
  return {
    ...(typeof r.object === 'string' && r.object ? { object: r.object } : {}),
    ...(p ? { p } : {}),
    ...(cam ? { cam } : {}),
    frame: num(r.frame),
    ...(r.shot === true ? { shot: true } : {}),
    ...(typeof r.render === 'string' && r.render ? { render: r.render } : {}),
    ...(fix ? { fix } : {}),
  };
};

export const readViewCmds = (v: unknown): IScene3dViewCmd[] =>
  arr(rec(v)?.cmds).flatMap((c) => {
    const r = rec(c);
    if (!r || typeof r.seq !== 'number') return [];
    const cam = readCam(r.cam);
    return [
      {
        seq: r.seq,
        ...(cam ? { cam } : {}),
        ...(typeof r.frame === 'number' ? { frame: r.frame } : {}),
        ...(typeof r.object === 'string' && r.object ? { object: r.object } : {}),
        ...(typeof r.pin === 'string' && r.pin ? { pin: r.pin } : {}),
      },
    ];
  });

export const readClip = (v: unknown): IScene3dClip | null => {
  const r = rec(v);
  const o = rec(r?.origin);
  if (!r || !o || typeof r.key !== 'string') return null;
  const cam = readCam(o.cam);
  return {
    key: r.key,
    status: (['queued', 'building', 'ready', 'failed'].includes(str(r.status))
      ? r.status
      : 'failed') as IScene3dClip['status'],
    done: num(r.done),
    total: num(r.total),
    ...(typeof r.frames === 'number' ? { frames: r.frames } : {}),
    ...(typeof r.fps === 'number' ? { fps: r.fps } : {}),
    ...(typeof r.seconds === 'number' ? { seconds: r.seconds } : {}),
    ...(typeof r.error === 'string' && r.error ? { error: r.error } : {}),
    origin: {
      project: str(o.project),
      title: str(o.title),
      hash: str(o.hash),
      cam: cam ?? { pos: [0, 0, 0], target: [0, 0, 0], fov: 0 },
      start: num(o.start),
      end: num(o.end),
      step: num(o.step, 1),
      kind: o.kind === 'final' ? 'final' : o.kind === 'generated' ? 'generated' : 'draft',
      ...(typeof o.engine === 'string' ? { engine: o.engine } : {}),
      ...(typeof o.where === 'string' && o.where ? { where: o.where } : {}),
      width: num(o.width),
      height: num(o.height),
      createdAt: str(o.createdAt),
    },
  };
};

export const readPin = (v: unknown): IScene3dPin | null => {
  const r = rec(v);
  const scene = r ? readAnchor(r.scene) : null;
  if (!r || !scene || r.view !== 'scene3d' || typeof r.id !== 'string') return null;
  const by = rec(r.by);
  return {
    id: r.id,
    label: str(r.label),
    text: str(r.text),
    by: { name: str(by?.name), email: str(by?.email) },
    at: str(r.at),
    resolved: r.resolved === true,
    scene,
    frame: str(r.frame),
  };
};

const MODEL_STATUSES: readonly string[] = Object.values(MODEL_STATUS);
const FRAME_STATUSES: readonly string[] = Object.values(FRAME_STATUS);

export const readFrame = (v: unknown): IScene3dFrame | null => {
  const r = rec(v);
  if (!r || typeof r.key !== 'string' || !FRAME_STATUSES.includes(str(r.status))) return null;
  const cam = readCam(r.cam);
  return {
    key: r.key,
    status: r.status as TFrameStatus,
    ...(cam ? { cam } : {}),
    frame: num(r.frame),
    ...(typeof r.engine === 'string' ? { engine: r.engine } : {}),
    ...(typeof r.error === 'string' && r.error ? { error: r.error } : {}),
    ...(r.cached === true ? { cached: true } : {}),
    ...(typeof r.where === 'string' && r.where ? { where: r.where } : {}),
  };
};

const readBake = (v: unknown): IScene3dMeta['bake'] | null => {
  const r = rec(v);
  if (!r) return null;
  const out: NonNullable<IScene3dMeta['bake']> = {};
  for (const [name, t] of Object.entries(r)) {
    const track = rec(t);
    if (!track || (track.m !== 'e' && track.m !== 'q')) continue;
    const width = track.m === 'e' ? 10 : 11;
    const rows = arr(track.rows).flatMap((row) => {
      const x = arr(row);
      return x.length === width && x.every((n) => typeof n === 'number' && Number.isFinite(n))
        ? [x as number[]]
        : [];
    });
    if (rows.length > 0) out[name] = { m: track.m, rows };
  }
  return Object.keys(out).length > 0 ? out : null;
};

const readMeta = (v: unknown): IScene3dMeta | null => {
  const r = rec(v);
  if (!r) return null;
  const b = arr(r.bounds).map((x) => num(x));
  const res = arr(r.res).map((x) => num(x));
  const stats = rec(r.stats) ?? {};
  const bake = readBake(r.bake);
  return {
    version: num(r.version),
    fps: num(r.fps, 24),
    start: num(r.start, 1),
    end: num(r.end, 1),
    res: [res[0] || 1920, res[1] || 1080],
    ...(typeof r.camera === 'string' && r.camera ? { camera: r.camera } : {}),
    objects: arr(r.objects).flatMap((o) => {
      const x = rec(o);
      return x && typeof x.n === 'string'
        ? [
            {
              n: x.n,
              t: str(x.t),
              ...(x.p ? { p: str(x.p) } : {}),
              tris: num(x.tris),
              ...(x.hidden === true ? { hidden: true } : {}),
            },
          ]
        : [];
    }),
    cameras: arr(r.cameras).flatMap((o) => {
      const x = rec(o);
      return x && typeof x.n === 'string'
        ? [
            {
              n: x.n,
              lens: num(x.lens, 35),
              fov: num(x.fov, 40),
              near: num(x.near, 0.1),
              far: num(x.far, 1000),
              track: arr(x.track).flatMap((row) => {
                const r = arr(row);
                return r.length === 8 && r.every((v) => typeof v === 'number' && Number.isFinite(v))
                  ? [r as number[]]
                  : [];
              }),
            },
          ]
        : [];
    }),
    markers: arr(r.markers).flatMap((o) => {
      const x = rec(o);
      return x && typeof x.n === 'string'
        ? [{ f: num(x.f), n: x.n, ...(x.cam ? { cam: str(x.cam) } : {}) }]
        : [];
    }),
    bounds: [b[0] ?? -5, b[1] ?? -5, b[2] ?? 0, b[3] ?? 5, b[4] ?? 5, b[5] ?? 3],
    ...(bake ? { bake } : {}),
    stats: {
      tris: num(stats.tris),
      trisOut: num(stats.trisOut),
      objects: num(stats.objects),
      animated: num(stats.animated),
      missingImages: arr(stats.missingImages).map((x) => str(x)),
      unappliedScale: arr(stats.unappliedScale).map((x) => str(x)),
      offCamera: arr(stats.offCamera).map((x) => str(x)),
    },
  };
};

export const readScene3dState = (v: unknown): IScene3dState | null => {
  const r = rec(v);
  const model = rec(r?.model);
  const settings = rec(r?.settings);
  if (!r || !model || !settings || !MODEL_STATUSES.includes(str(model.status))) return null;
  const meta = readMeta(model.meta);
  return {
    rev: num(r.rev),
    title: str(r.title),
    settings: {
      root: str(settings.root),
      blend: str(settings.blend),
      ...(settings.script ? { script: str(settings.script) } : {}),
    },
    blender: r.blender === true,
    engine: str(r.engine),
    model: {
      status: model.status as TModelStatus,
      ...(typeof model.hash === 'string' ? { hash: model.hash } : {}),
      ...(typeof model.bytes === 'number' ? { bytes: model.bytes } : {}),
      ...(typeof model.error === 'string' && model.error ? { error: model.error } : {}),
      ...(meta ? { meta } : {}),
    },
    pins: arr(r.comments).flatMap((c) => readPin(c) ?? []),
    guestResolved: arr(r.guestResolved).map((x) => str(x)),
    frames: arr(r.frames).flatMap((f) => readFrame(f) ?? []),
    clips: arr(r.clips).flatMap((c) => readClip(c) ?? []),
    outputs: arr(r.outputs).flatMap((o) =>
      rec(o) && typeof rec(o)?.key === 'string' ? [o as IScene3dOutputs] : [],
    ),
    generations: arr(r.generations).flatMap((g) =>
      rec(g) && typeof rec(g)?.id === 'string' ? [g as IScene3dGeneration] : [],
    ),
    generator: r.generator === true,
    finals: arr(r.finals).flatMap((f) =>
      rec(f) && typeof rec(f)?.id === 'string' ? [f as IScene3dFinal] : [],
    ),
  };
};

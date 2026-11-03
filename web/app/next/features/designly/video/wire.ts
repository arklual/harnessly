// The video routes' answers read as data, by the rules of deck/wire.ts: every field is checked, a Go slice that was
// empty may come as null and reads as [], an unknown enum value falls back to the default, and a malformed item of a
// list is left out. The readers never throw; a top level that is not what was asked reads as null. No React here.
import { readDeckComment, readDeckSettings } from '@/next/features/designly/deck/wire';
import { DECK_JOB } from '@/next/features/designly/deck/types';
import {
  VIDEO_BUILD_IN,
  VIDEO_FORMAT,
  VIDEO_FORMATS,
  VIDEO_OVERLAY_ANCHORS,
  VIDEO_OVERLAY_ENTERS,
  VIDEO_OVERLAY_KINDS,
  VIDEO_OVERLAY_LEAVES,
  VIDEO_OVERLAY_THEMES,
  VIDEO_TRANSITION,
  type IVideoComment,
  type IVideoCommentAt,
  type IVideoMoment,
  type IVideoOverlay,
  type IVideoOverlayPlace,
  type IVideoRenderInfo,
  type IVideoRenderJob,
  type IVideoScene,
  type IVideoSettings,
  type IVideoState,
  type IVideoTransition,
  type TVideoFormat,
} from '@/next/features/designly/video/types';

type TRecord = Record<string, unknown>;

const isRecord = (v: unknown): v is TRecord => !!v && typeof v === 'object' && !Array.isArray(v);
const num = (v: unknown, fallback = 0) => (typeof v === 'number' && Number.isFinite(v) ? v : fallback);
const int = (v: unknown): number | null => (typeof v === 'number' && Number.isInteger(v) ? v : null);
const text = (v: unknown, max = 20_000) => (typeof v === 'string' ? v.slice(0, max) : '');
const textOrNull = (v: unknown, max = 20_000) => (typeof v === 'string' ? v.slice(0, max) : null);
const bool = (v: unknown) => v === true;
const records = (v: unknown): TRecord[] => (Array.isArray(v) ? v.filter(isRecord) : []);
const strings = (v: unknown, max = 200): string[] =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string').map((x) => x.slice(0, max)) : [];
const oneOf = <T extends string>(all: Record<string, T>, v: unknown, fallback: T): T =>
  typeof v === 'string' && (Object.values(all) as string[]).includes(v) ? (v as T) : fallback;

/** A format, or null for anything else. */
export const readVideoFormat = (v: unknown): TVideoFormat | null =>
  VIDEO_FORMATS.find((f) => f === v) ?? null;

/** The formats in their fixed order, each once; none read is 16:9 (a video always has a format). */
const readFormats = (v: unknown): TVideoFormat[] => {
  const have = new Set(Array.isArray(v) ? v.flatMap((x) => readVideoFormat(x) ?? []) : []);
  const out = VIDEO_FORMATS.filter((f) => have.has(f));
  return out.length > 0 ? out : [VIDEO_FORMAT.landscape];
};

export const readVideoSettings = (v: unknown): IVideoSettings => {
  const r = isRecord(v) ? v : {};
  return {
    v: num(r.v, 1),
    formats: readFormats(r.formats),
    fps: num(r.fps, 30),
    seconds: int(r.seconds),
    voice: 'none',
    subs: r.subs !== false,
  };
};

const readTransition = (v: unknown): IVideoTransition => {
  const r = isRecord(v) ? v : {};
  return {
    kind: oneOf(VIDEO_TRANSITION, r.kind, VIDEO_TRANSITION.none),
    ms: Math.min(2000, Math.max(0, num(r.ms))),
  };
};

const readMoment = (r: TRecord): IVideoMoment | null => {
  const at = int(r.at);
  if (at === null || at < 0) return null;
  const buildIn = Object.values(VIDEO_BUILD_IN).find((b) => b === r.buildIn) ?? '';
  return {
    block: text(r.block, 120),
    tpl: int(r.tpl),
    at,
    out: int(r.out),
    buildIn,
    derived: bool(r.derived),
  };
};

const numOrNull = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);
const pick = <T extends string>(all: readonly T[], v: unknown): T | null => all.find((x) => x === v) ?? null;

const readOverlayPlace = (v: unknown): IVideoOverlayPlace | null => {
  if (!isRecord(v)) return null;
  const x = numOrNull(v.x);
  const y = numOrNull(v.y);
  const w = numOrNull(v.w);
  const h = numOrNull(v.h);
  if (x === null || y === null || w === null || h === null) return null;
  return { x, y, w, h, tx: numOrNull(v.tx), ty: numOrNull(v.ty) };
};

/** An overlay of a scene; a malformed one reads as null and is left out. */
export const readVideoOverlay = (v: unknown): IVideoOverlay | null => {
  if (!isRecord(v)) return null;
  const id = text(v.id, 64);
  const kind = pick(VIDEO_OVERLAY_KINDS, v.kind);
  const place = readOverlayPlace(v);
  const at = int(v.at);
  if (id === '' || kind === null || place === null || at === null || at < 0) return null;
  return {
    id,
    kind,
    text: text(v.text, 400),
    at,
    out: int(v.out),
    ...place,
    portrait: readOverlayPlace(v.portrait),
    anchor: pick(VIDEO_OVERLAY_ANCHORS, v.anchor) ?? 'center',
    enter: pick(VIDEO_OVERLAY_ENTERS, v.enter) ?? 'fade',
    leave: pick(VIDEO_OVERLAY_LEAVES, v.leave) ?? 'fade',
    theme: pick(VIDEO_OVERLAY_THEMES, v.theme) ?? 'auto',
    value: numOrNull(v.value),
    from: num(v.from),
    decimals: Math.min(3, Math.max(0, num(v.decimals))),
    author: v.author === 'agent' ? 'agent' : 'human',
    updatedAt: text(v.updatedAt, 40),
  };
};

export const readVideoOverlays = (v: unknown): IVideoOverlay[] =>
  (Array.isArray(v) ? v : []).flatMap((x) => readVideoOverlay(x) ?? []);

const readKeys = (v: unknown): IVideoScene['keys'] => {
  const out: IVideoScene['keys'] = {};
  if (!isRecord(v)) return out;
  for (const f of VIDEO_FORMATS) {
    const key = textOrNull(v[f], 128);
    if (key !== null && key !== '') out[f] = key;
  }
  return out;
};

export const readVideoScene = (v: unknown): IVideoScene | null => {
  if (!isRecord(v)) return null;
  const id = text(v.id, 64);
  if (id === '') return null;
  return {
    id,
    path: text(v.path, 400),
    title: text(v.title, 200),
    start: Math.max(0, num(v.start)),
    dur: Math.max(0, num(v.dur)),
    auto: bool(v.auto),
    transition: readTransition(v.transition),
    moments: records(v.moments).flatMap((x) => readMoment(x) ?? []),
    overlays: readVideoOverlays(v.overlays),
    words: Math.max(0, num(v.words)),
    notes: text(v.notes),
    version: text(v.version, 80),
    blank: bool(v.blank),
    keys: readKeys(v.keys),
  };
};

/** A comment's video part; null for a comment made on the board, not on a time. */
const readCommentAt = (r: TRecord): IVideoCommentAt | null => {
  const t0 = int(r.t0);
  const format = readVideoFormat(r.format);
  if (r.view !== 'video' || t0 === null || t0 < 0 || format === null) return null;
  const t1 = int(r.t1);
  return {
    t0,
    t1: t1 !== null && t1 >= t0 ? t1 : null,
    format,
    block: text(r.block, 120),
    frame: text(r.frame, 400),
  };
};

export const readVideoComment = (v: unknown): IVideoComment | null => {
  const base = readDeckComment(v);
  if (base === null || !isRecord(v)) return null;
  return { ...base, video: readCommentAt(v) };
};

const readRenderInfo = (r: TRecord): IVideoRenderInfo | null => {
  const format = readVideoFormat(r.format);
  if (format === null) return null;
  const durMs = int(r.durMs);
  return {
    format,
    published: bool(r.published),
    key: textOrNull(r.key, 128),
    currentKey: text(r.currentKey, 128),
    fresh: bool(r.fresh),
    durMs,
    at: textOrNull(r.at, 40),
  };
};

/** POST/GET /design/video/render; null when the answer is not a job. */
export const readVideoRenderJob = (v: unknown): IVideoRenderJob | null => {
  if (!isRecord(v)) return null;
  const id = text(v.id, 64);
  const format = readVideoFormat(v.format);
  if (id === '' || format === null) return null;
  return {
    id,
    format,
    scene: textOrNull(v.scene, 64),
    draft: bool(v.draft),
    state: oneOf(DECK_JOB, v.state, DECK_JOB.failed),
    progress: Math.min(1, Math.max(0, num(v.progress))),
    file: textOrNull(v.file, 200),
    error: textOrNull(v.error, 400),
    at: text(v.at, 40),
  };
};

/** GET /design/video; null when the answer is not a video. */
export const readVideoState = (v: unknown): IVideoState | null => {
  if (!isRecord(v) || !isRecord(v.settings)) return null;
  return {
    rev: num(v.rev),
    settings: readVideoSettings(v.settings),
    deck: readDeckSettings(v.deck),
    chrome: bool(v.chrome),
    ffmpeg: bool(v.ffmpeg),
    durMs: Math.max(0, num(v.durMs)),
    scenes: (Array.isArray(v.scenes) ? v.scenes : []).flatMap((x) => readVideoScene(x) ?? []),
    comments: (Array.isArray(v.comments) ? v.comments : []).flatMap((x) => readVideoComment(x) ?? []),
    guestResolved: strings(v.guestResolved, 80),
    renders: records(v.renders).flatMap((x) => readRenderInfo(x) ?? []),
    job: readVideoRenderJob(v.job),
  };
};

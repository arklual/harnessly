// The overlay layer as the player edits it (HRN-653): where an overlay stands in a format, which are on screen at a
// time, how a drag moves or resizes one (with a snap to the frame's middle and safe zone), how a strip on the timeline
// moves, and the words the runtime draws from. Pure; the runtime's rules (internal/design/video_overlays.go) are the
// truth and this only mirrors the ones the editor needs. No React here.
import {
  VIDEO_FORMAT,
  VIDEO_OVERLAY_MIN_MS,
  type IVideoOverlay,
  type IVideoOverlayPlace,
  type TVideoFormat,
} from '@/next/features/designly/video/types';

/** The smallest a box gets when it is resized, a fraction of the frame. */
export const OVERLAY_MIN_SIZE = 0.04;
/** The snap reaches this far, a fraction of the frame. */
export const SNAP = 0.012;
/** How long a new overlay stays, ms. */
export const NEW_OVERLAY_MS = 3000;

interface ISafe {
  l: number;
  t: number;
  r: number;
  b: number;
}

/** A format's safe zone: the margins that text keeps clear of (the runtime's own table). */
export const safeZone = (format: TVideoFormat): ISafe =>
  format === VIDEO_FORMAT.portrait
    ? { l: 0.06, t: 0.08, r: 0.06, b: 0.17 }
    : { l: 0.04, t: 0.06, r: 0.04, b: 0.06 };

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

const basePlace = (o: IVideoOverlay): IVideoOverlayPlace => ({
  x: o.x,
  y: o.y,
  w: o.w,
  h: o.h,
  tx: o.tx,
  ty: o.ty,
});

/** Where the overlay stands in a format: 16:9 its own place; 9:16 its own place or the base one moved into the safe zone. */
export const placeOf = (o: IVideoOverlay, format: TVideoFormat): IVideoOverlayPlace => {
  const base = basePlace(o);
  if (format !== VIDEO_FORMAT.portrait) return base;
  if (o.portrait !== null) return o.portrait;
  if (o.kind === 'highlight') return base;
  const z = safeZone(format);
  const w = Math.min(base.w, 1 - z.l - z.r);
  const h = Math.min(base.h, 1 - z.t - z.b);
  return {
    ...base,
    w,
    h,
    x: Math.min(Math.max(base.x, z.l), 1 - z.r - w),
    y: Math.min(Math.max(base.y, z.t), 1 - z.b - h),
  };
};

/** The overlay with a place laid into the format: the base place for 16:9, the portrait one for 9:16. */
export const withPlace = (o: IVideoOverlay, format: TVideoFormat, p: IVideoOverlayPlace): IVideoOverlay =>
  format === VIDEO_FORMAT.portrait
    ? { ...o, portrait: p }
    : { ...o, x: p.x, y: p.y, w: p.w, h: p.h, tx: p.tx, ty: p.ty };

/** The fields the overlay route takes for a place in a format. */
export const placePatch = (format: TVideoFormat, p: IVideoOverlayPlace): Record<string, unknown> => {
  const place: Record<string, unknown> = { x: p.x, y: p.y, w: p.w, h: p.h };
  if (p.tx !== null && p.ty !== null) {
    place.tx = p.tx;
    place.ty = p.ty;
  }
  return format === VIDEO_FORMAT.portrait ? { portrait: place } : place;
};

/** On screen at `local` ms of its scene (the end is the scene's when `out` is null). */
export const visibleAt = (o: IVideoOverlay, local: number, dur: number): boolean =>
  local >= o.at && local < (o.out ?? dur);

/** The overlays on screen at `local`, in the layer's order. */
export const visibleOverlays = (list: readonly IVideoOverlay[], local: number, dur: number) =>
  list.filter((o) => visibleAt(o, local, dur));

// ---- a drag on the picture

export const DRAG = { move: 'move', resize: 'resize', target: 'target' } as const;
export type TDrag = (typeof DRAG)[keyof typeof DRAG];

const snapTo = (v: number, marks: readonly number[]): number => {
  let best = v;
  let gap = SNAP;
  for (const m of marks) {
    const d = Math.abs(m - v);
    if (d <= gap) {
      best = m;
      gap = d;
    }
  }
  return best;
};

/** One axis of a move: the start of the box snaps by its own edge, its middle or its far edge. */
const snapAxis = (start: number, size: number, lo: number, hi: number): number => {
  const marks = [lo, 0.5 - size / 2, hi - size];
  return clamp(snapTo(start, marks), 0, 1 - size);
};

/** The place after a drag of (dx, dy) fractions of the frame from `from`. */
export const dragPlace = (
  from: IVideoOverlayPlace,
  mode: TDrag,
  dx: number,
  dy: number,
  format: TVideoFormat,
): IVideoOverlayPlace => {
  const z = safeZone(format);
  switch (mode) {
    case DRAG.move: {
      const x = snapAxis(from.x + dx, from.w, z.l, 1 - z.r);
      const y = snapAxis(from.y + dy, from.h, z.t, 1 - z.b);
      const tx = from.tx === null ? null : clamp(from.tx + (x - from.x), 0, 1);
      const ty = from.ty === null ? null : clamp(from.ty + (y - from.y), 0, 1);
      return { ...from, x, y, tx, ty };
    }
    case DRAG.resize: {
      const w = clamp(from.w + dx, OVERLAY_MIN_SIZE, 1 - from.x);
      const h = clamp(from.h + dy, OVERLAY_MIN_SIZE, 1 - from.y);
      return { ...from, w, h };
    }
    case DRAG.target: {
      if (from.tx === null || from.ty === null) return from;
      return { ...from, tx: clamp(from.tx + dx, 0, 1), ty: clamp(from.ty + dy, 0, 1) };
    }
  }
};

/** A place rounded to a thousandth of the frame: what is stored. */
export const roundPlace = (p: IVideoOverlayPlace): IVideoOverlayPlace => {
  const r = (v: number) => Math.round(v * 1000) / 1000;
  return {
    x: r(p.x),
    y: r(p.y),
    w: r(p.w),
    h: r(p.h),
    tx: p.tx === null ? null : r(p.tx),
    ty: p.ty === null ? null : r(p.ty),
  };
};

// ---- a strip on the timeline

export const STRIP = { move: 'move', start: 'start', end: 'end' } as const;
export type TStrip = (typeof STRIP)[keyof typeof STRIP];

/** The time of a strip after its edge or its middle moved by `delta` ms; whole 10 ms, inside the scene. */
export const stripSpan = (
  from: { at: number; out: number | null },
  mode: TStrip,
  delta: number,
  dur: number,
): { at: number; out: number | null } => {
  const end = from.out ?? dur;
  const step = (v: number) => Math.round(v / 10) * 10;
  switch (mode) {
    case STRIP.move: {
      const len = end - from.at;
      const at = clamp(step(from.at + delta), 0, Math.max(0, dur - len));
      return { at, out: from.out === null && at + len >= dur ? null : at + len };
    }
    case STRIP.start:
      return { at: clamp(step(from.at + delta), 0, end - VIDEO_OVERLAY_MIN_MS), out: from.out };
    case STRIP.end: {
      const out = clamp(step(end + delta), from.at + VIDEO_OVERLAY_MIN_MS, dur);
      return { at: from.at, out: out >= dur ? null : out };
    }
  }
};

// ---- what the runtime draws from, and what a new overlay is

/** The overlay as the runtime reads it: absent optionals are left out. */
export const overlayWire = (o: IVideoOverlay): Record<string, unknown> => {
  const place = (p: IVideoOverlayPlace) => ({
    x: p.x,
    y: p.y,
    w: p.w,
    h: p.h,
    ...(p.tx !== null && p.ty !== null ? { tx: p.tx, ty: p.ty } : {}),
  });
  return {
    id: o.id,
    kind: o.kind,
    text: o.text,
    at: o.at,
    out: o.out,
    ...place(basePlace(o)),
    ...(o.portrait !== null ? { portrait: place(o.portrait) } : {}),
    anchor: o.anchor,
    enter: o.enter,
    leave: o.leave,
    theme: o.theme,
    ...(o.value !== null ? { value: o.value } : {}),
    from: o.from,
    decimals: o.decimals,
    author: o.author,
  };
};

/** A new caption at `local` ms: the center of the lower third is the kind's own default place. */
export const newCaptionPatch = (local: number, dur: number, text: string): Record<string, unknown> => {
  const at = clamp(Math.round(local / 10) * 10, 0, Math.max(0, dur - 1000));
  const end = at + NEW_OVERLAY_MS;
  return { kind: 'caption', text, at, out: end >= dur ? null : end };
};

/** What a change of kind needs besides the kind: a callout's arrow point, a counter's number. */
export const kindPatch = (
  o: IVideoOverlay,
  kind: IVideoOverlay['kind'],
  format: TVideoFormat,
): Record<string, unknown> => {
  const patch: Record<string, unknown> = { kind };
  const place = placeOf(o, format);
  if (kind === 'callout' && (place.tx === null || place.ty === null)) {
    const tx = clamp(place.x + place.w / 2, 0, 1);
    const ty = clamp(place.y + place.h + 0.12, 0, 1);
    Object.assign(patch, placePatch(format, { ...place, tx, ty }));
  }
  if (kind === 'counter' && o.value === null) patch.value = 100;
  if (kind !== 'highlight' && o.text.trim() === '' && !(kind === 'counter')) patch.text = '…';
  return patch;
};

/** A lane for each overlay so that the strips of the ones that overlap in time do not lie over each other. */
export const stripLanes = (list: readonly IVideoOverlay[], dur: number): Map<string, number> => {
  const ends: number[] = [];
  const lanes = new Map<string, number>();
  for (const o of [...list].sort((a, b) => a.at - b.at)) {
    let lane = ends.findIndex((end) => end <= o.at);
    if (lane < 0) lane = ends.length;
    ends[lane] = o.out ?? dur;
    lanes.set(o.id, lane);
  }
  return lanes;
};

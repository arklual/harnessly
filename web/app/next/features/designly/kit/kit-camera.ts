// The kit canvas's camera: a point of the world shows at `world * s + (x, y)` on the area. Zooming keeps the point
// under the cursor in place (as site-zoom does for a design); flying to a node fits its rectangle with a margin. Pure.
import type { IRect } from '@/next/features/designly/kit/kit-world';

export interface ICamera {
  s: number;
  x: number;
  y: number;
}

export const CAMERA_ZOOM = { min: 0.01, max: 4 } as const;

const clamp = (low: number, high: number, v: number) => Math.max(low, Math.min(high, v));

export const zoomAround = (c: ICamera, at: { x: number; y: number }, factor: number): ICamera => {
  const s = clamp(CAMERA_ZOOM.min, CAMERA_ZOOM.max, c.s * factor);
  const k = s / c.s;
  return { s, x: at.x - (at.x - c.x) * k, y: at.y - (at.y - c.y) * k };
};

export const panCamera = (c: ICamera, dx: number, dy: number): ICamera => ({
  s: c.s,
  x: c.x + dx,
  y: c.y + dy,
});

/** The camera that shows a rectangle of the world whole in an area, centred, with a margin; never above `max`. */
export const fitRect = (r: IRect, area: { w: number; h: number }, margin = 48, max = 1.5): ICamera => {
  const w = Math.max(1, area.w - margin * 2);
  const h = Math.max(1, area.h - margin * 2);
  const s = clamp(CAMERA_ZOOM.min, max, Math.min(w / Math.max(1, r.w), h / Math.max(1, r.h)));
  return { s, x: area.w / 2 - (r.x + r.w / 2) * s, y: area.h / 2 - (r.y + r.h / 2) * s };
};

/** The rectangle of the world the area shows. */
export const viewRect = (c: ICamera, area: { w: number; h: number }): IRect => ({
  x: -c.x / c.s,
  y: -c.y / c.s,
  w: area.w / c.s,
  h: area.h / c.s,
});

export const toWorld = (c: ICamera, p: { x: number; y: number }) => ({
  x: (p.x - c.x) / c.s,
  y: (p.y - c.y) / c.s,
});

/** A step between two cameras: the scale moves evenly in its logarithm, the point at the centre follows it. */
export const lerpCamera = (a: ICamera, b: ICamera, t: number, area: { w: number; h: number }): ICamera => {
  const e = t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2;
  const s = Math.exp(Math.log(a.s) + (Math.log(b.s) - Math.log(a.s)) * e);
  const ca = toWorld(a, { x: area.w / 2, y: area.h / 2 });
  const cb = toWorld(b, { x: area.w / 2, y: area.h / 2 });
  const cx = ca.x + (cb.x - ca.x) * e;
  const cy = ca.y + (cb.y - ca.y) * e;
  return { s, x: area.w / 2 - cx * s, y: area.h / 2 - cy * s };
};

/** The level of detail of a cell by its width on screen: far shows the atlas, middle the picture, near the live page. */
export const LOD = { far: 'far', middle: 'middle', near: 'near' } as const;
export type TLod = (typeof LOD)[keyof typeof LOD];

export const lodOf = (screenW: number, cellAtlasW: number): TLod => {
  if (screenW >= 520) return LOD.near;
  if (screenW > cellAtlasW * 1.15) return LOD.middle;
  return LOD.far;
};
